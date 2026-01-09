using System.Text.Json;

namespace BbsClient.Storage;

public sealed class BlockedStore
{
    private static readonly JsonSerializerOptions JsonOptions = new()
    {
        PropertyNamingPolicy = JsonNamingPolicy.CamelCase,
        WriteIndented = true,
    };

    private readonly string _path;

    public BlockedStore(string path)
    {
        _path = path;
    }

    public async Task<HashSet<string>> LoadAsync(CancellationToken ct)
    {
        if (!File.Exists(_path))
        {
            return new HashSet<string>(StringComparer.Ordinal);
        }
        await using var s = File.OpenRead(_path);
        var keys = await JsonSerializer.DeserializeAsync<List<string>>(s, JsonOptions, ct) ?? [];
        return new HashSet<string>(keys.Where(k => !string.IsNullOrWhiteSpace(k)), StringComparer.Ordinal);
    }

    public async Task SaveAsync(HashSet<string> keys, CancellationToken ct)
    {
        Directory.CreateDirectory(Path.GetDirectoryName(_path)!);
        await using var s = File.Create(_path);
        await JsonSerializer.SerializeAsync(s, keys.Order(StringComparer.Ordinal).ToList(), JsonOptions, ct);
    }

    public async Task AddAsync(string pubKey, CancellationToken ct)
    {
        var keys = await LoadAsync(ct);
        keys.Add(pubKey);
        await SaveAsync(keys, ct);
    }

    public async Task RemoveAsync(string pubKey, CancellationToken ct)
    {
        var keys = await LoadAsync(ct);
        keys.Remove(pubKey);
        await SaveAsync(keys, ct);
    }
}

