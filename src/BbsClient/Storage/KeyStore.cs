using System.Text.Json;
using System.Security.Cryptography;
using Chaos.NaCl;
using System.Text;

namespace BbsClient.Storage;

public sealed class KeyStore
{
    private static readonly JsonSerializerOptions JsonOptions = new()
    {
        PropertyNamingPolicy = JsonNamingPolicy.CamelCase,
        WriteIndented = true,
    };

    private readonly string _path;
    private const int EncryptionVersion = 1;
    private const int DefaultKdfIterations = 200_000;

    public KeyStore(string path)
    {
        _path = path;
    }

    public async Task<List<KeyEntry>> LoadAsync(CancellationToken ct)
    {
        if (!File.Exists(_path))
        {
            return [];
        }
        await using var s = File.OpenRead(_path);
        var keys = await JsonSerializer.DeserializeAsync<List<KeyEntry>>(s, JsonOptions, ct);
        return keys ?? [];
    }

    public async Task SaveAsync(List<KeyEntry> keys, CancellationToken ct)
    {
        Directory.CreateDirectory(Path.GetDirectoryName(_path)!);
        await using var s = File.Create(_path);
        await JsonSerializer.SerializeAsync(s, keys, JsonOptions, ct);
    }

    public async Task<KeyEntry?> FindAsync(string name, CancellationToken ct)
    {
        var keys = await LoadAsync(ct);
        return keys.FirstOrDefault(k => string.Equals(k.Name, name, StringComparison.OrdinalIgnoreCase));
    }

    public async Task<KeyEntry> GenerateAsync(string name, CancellationToken ct)
    {
        return await GenerateAsync(name, password: null, ct);
    }

    public async Task<KeyEntry> GenerateAsync(string name, string? password, CancellationToken ct)
    {
        var seed = RandomNumberGenerator.GetBytes(32);
        var pub = new byte[32];
        var privExpanded = new byte[64];
        Ed25519.KeyPairFromSeed(pub, privExpanded, seed);

        var pubStr = $"ed25519:{ToBase64Raw(pub)}";
        var privStr = $"ed25519:{ToBase64Raw(seed)}";
        KeyEntry entry = new(name, pubStr, privStr);
        if (!string.IsNullOrWhiteSpace(password))
        {
            entry = entry with
            {
                Priv = "",
                Encryption = EncryptPriv(privStr, password),
            };
        }

        var keys = await LoadAsync(ct);
        if (keys.Any(k => string.Equals(k.Name, name, StringComparison.OrdinalIgnoreCase)))
        {
            throw new InvalidOperationException($"Key '{name}' already exists.");
        }
        keys.Add(entry);
        await SaveAsync(keys, ct);
        return entry;
    }

    public async Task DeleteAsync(string name, CancellationToken ct)
    {
        var keys = await LoadAsync(ct);
        var removed = keys.RemoveAll(k => string.Equals(k.Name, name, StringComparison.OrdinalIgnoreCase));
        if (removed == 0)
        {
            throw new InvalidOperationException($"Key '{name}' not found.");
        }
        await SaveAsync(keys, ct);
    }

    private static string ToBase64Raw(byte[] bytes)
    {
        // Match Go's base64.RawStdEncoding (no padding).
        return Convert.ToBase64String(bytes).TrimEnd('=');
    }

    public async Task ProtectAsync(string name, string password, CancellationToken ct)
    {
        if (string.IsNullOrWhiteSpace(password))
        {
            throw new InvalidOperationException("Password is empty.");
        }

        var keys = await LoadAsync(ct);
        var idx = keys.FindIndex(k => string.Equals(k.Name, name, StringComparison.OrdinalIgnoreCase));
        if (idx < 0)
        {
            throw new InvalidOperationException($"Key '{name}' not found.");
        }

        var k = keys[idx];
        if (k.IsProtected)
        {
            throw new InvalidOperationException($"Key '{name}' is already password-protected.");
        }
        if (string.IsNullOrWhiteSpace(k.Priv))
        {
            throw new InvalidOperationException($"Key '{name}' has no private key.");
        }

        keys[idx] = k with
        {
            Priv = "",
            Encryption = EncryptPriv(k.Priv, password),
        };
        await SaveAsync(keys, ct);
    }

    public async Task ChangePasswordAsync(string name, string currentPassword, string newPassword, CancellationToken ct)
    {
        if (string.IsNullOrWhiteSpace(newPassword))
        {
            throw new InvalidOperationException("New password is empty.");
        }

        var keys = await LoadAsync(ct);
        var idx = keys.FindIndex(k => string.Equals(k.Name, name, StringComparison.OrdinalIgnoreCase));
        if (idx < 0)
        {
            throw new InvalidOperationException($"Key '{name}' not found.");
        }

        var k = keys[idx];
        if (!k.IsProtected)
        {
            throw new InvalidOperationException($"Key '{name}' is not password-protected.");
        }

        var priv = DecryptPriv(k, currentPassword);
        keys[idx] = k with
        {
            Priv = "",
            Encryption = EncryptPriv(priv, newPassword),
        };
        await SaveAsync(keys, ct);
    }

    public async Task UnprotectAsync(string name, string password, CancellationToken ct)
    {
        var keys = await LoadAsync(ct);
        var idx = keys.FindIndex(k => string.Equals(k.Name, name, StringComparison.OrdinalIgnoreCase));
        if (idx < 0)
        {
            throw new InvalidOperationException($"Key '{name}' not found.");
        }

        var k = keys[idx];
        if (!k.IsProtected)
        {
            return;
        }

        var priv = DecryptPriv(k, password);
        keys[idx] = k with
        {
            Priv = priv,
            Encryption = null,
        };
        await SaveAsync(keys, ct);
    }

    public static string DecryptPriv(KeyEntry entry, string password)
    {
        if (entry.Encryption == null)
        {
            if (string.IsNullOrWhiteSpace(entry.Priv))
            {
                throw new InvalidOperationException("Key has no private key.");
            }
            return entry.Priv;
        }
        if (entry.Encryption.Version != EncryptionVersion)
        {
            throw new InvalidOperationException($"Unsupported encryption version: {entry.Encryption.Version}.");
        }
        if (!string.Equals(entry.Encryption.Kdf, "pbkdf2-sha256/aes-256-gcm", StringComparison.Ordinal))
        {
            throw new InvalidOperationException($"Unsupported KDF/cipher: {entry.Encryption.Kdf}.");
        }

        var salt = Convert.FromBase64String(entry.Encryption.Salt);
        var nonce = Convert.FromBase64String(entry.Encryption.Nonce);
        var tag = Convert.FromBase64String(entry.Encryption.Tag);
        var ciphertext = Convert.FromBase64String(entry.Encryption.CipherText);

        var key = DeriveKey(password, salt, entry.Encryption.Iterations);
        try
        {
            var plaintext = new byte[ciphertext.Length];
            using var aes = new AesGcm(key, tagSizeInBytes: 16);
            aes.Decrypt(nonce, ciphertext, tag, plaintext, null);
            return Encoding.UTF8.GetString(plaintext);
        }
        catch (CryptographicException)
        {
            throw new InvalidOperationException("Invalid password.");
        }
    }

    private static KeyEncryption EncryptPriv(string priv, string password)
    {
        var salt = RandomNumberGenerator.GetBytes(16);
        var nonce = RandomNumberGenerator.GetBytes(12);
        var key = DeriveKey(password, salt, DefaultKdfIterations);

        var plaintext = Encoding.UTF8.GetBytes(priv);
        var ciphertext = new byte[plaintext.Length];
        var tag = new byte[16];
        using var aes = new AesGcm(key, tagSizeInBytes: 16);
        aes.Encrypt(nonce, plaintext, ciphertext, tag, null);

        return new KeyEncryption(
            Version: EncryptionVersion,
            Kdf: "pbkdf2-sha256/aes-256-gcm",
            Iterations: DefaultKdfIterations,
            Salt: Convert.ToBase64String(salt),
            Nonce: Convert.ToBase64String(nonce),
            Tag: Convert.ToBase64String(tag),
            CipherText: Convert.ToBase64String(ciphertext)
        );
    }

    private static byte[] DeriveKey(string password, byte[] salt, int iterations)
    {
        using var kdf = new Rfc2898DeriveBytes(password, salt, iterations, HashAlgorithmName.SHA256);
        return kdf.GetBytes(32);
    }
}

public sealed record KeyEntry(string Name, string Pub, string Priv)
{
    public KeyEncryption? Encryption { get; init; }
    public bool IsProtected => Encryption != null;
}

public sealed record KeyEncryption(
    int Version,
    string Kdf,
    int Iterations,
    string Salt,
    string Nonce,
    string Tag,
    string CipherText
);
