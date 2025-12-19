using System.Text.Json;
using NSec.Cryptography;

namespace BbsClient.Storage;

public sealed class KeyStore
{
    private static readonly JsonSerializerOptions JsonOptions = new()
    {
        PropertyNamingPolicy = JsonNamingPolicy.CamelCase,
        WriteIndented = true,
    };

    private readonly string _path;

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
        var alg = SignatureAlgorithm.Ed25519;
        using var key = Key.Create(alg, new KeyCreationParameters
        {
            ExportPolicy = KeyExportPolicies.AllowPlaintextExport,
        });

        var pub = key.Export(KeyBlobFormat.RawPublicKey);
        var priv = key.Export(KeyBlobFormat.RawPrivateKey);

        var entry = new KeyEntry(
            name,
            $"ed25519:{Convert.ToBase64String(pub)}",
            $"ed25519:{Convert.ToBase64String(priv)}"
        );

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
}

public sealed record KeyEntry(string Name, string Pub, string Priv);

