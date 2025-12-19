using System.Text.Json;
using BbsClient.Util;

namespace BbsClient.Storage;

public sealed class ClientConfigStore
{
    private static readonly JsonSerializerOptions JsonOptions = new()
    {
        PropertyNamingPolicy = JsonNamingPolicy.CamelCase,
        WriteIndented = true,
    };

    private readonly string _path;

    public ClientConfigStore(string path)
    {
        _path = path;
    }

    public string ConfigPath => _path;

    public static string DefaultPath()
    {
        return System.IO.Path.Combine(ConfigPaths.DefaultAppDir(), "client-config.json");
    }

    public async Task<ClientConfig> LoadAsync(CancellationToken ct)
    {
        if (!File.Exists(_path))
        {
            return new ClientConfig().Normalize();
        }

        await using var s = File.OpenRead(_path);
        var cfg = await JsonSerializer.DeserializeAsync<ClientConfig>(s, JsonOptions, ct);
        return (cfg ?? new ClientConfig()).Normalize();
    }

    public async Task SaveAsync(ClientConfig cfg, CancellationToken ct)
    {
        cfg = cfg.Normalize();
        Directory.CreateDirectory(System.IO.Path.GetDirectoryName(_path)!);
        await using var s = File.Create(_path);
        await JsonSerializer.SerializeAsync(s, cfg, JsonOptions, ct);
    }
}

public sealed record ClientConfig
{
    public string BackendBaseUrl { get; init; } = "http://127.0.0.1:8080";
    public string DataDir { get; init; } = ConfigPaths.DefaultAppDir();
    public bool StartBackend { get; init; } = true;
    public string? BbsNodePath { get; init; }

    public string FlexIpfsBaseUrl { get; init; } = "http://127.0.0.1:5001/api/v0";
    public string? FlexIpfsBaseDir { get; init; }
    public string? FlexIpfsGwEndpoint { get; init; }
    public bool AutostartFlexIpfs { get; init; } = true;

    public ClientConfig Normalize()
    {
        var backendBaseUrl = string.IsNullOrWhiteSpace(BackendBaseUrl) ? "http://127.0.0.1:8080" : BackendBaseUrl.Trim();
        var dataDir = string.IsNullOrWhiteSpace(DataDir) ? ConfigPaths.DefaultAppDir() : DataDir.Trim();
        var bbsNodePath = string.IsNullOrWhiteSpace(BbsNodePath) ? null : BbsNodePath.Trim();

        var flexIpfsBaseUrl = string.IsNullOrWhiteSpace(FlexIpfsBaseUrl) ? "http://127.0.0.1:5001/api/v0" : FlexIpfsBaseUrl.Trim();
        var flexIpfsBaseDir = string.IsNullOrWhiteSpace(FlexIpfsBaseDir) ? null : FlexIpfsBaseDir.Trim();
        var flexIpfsGwEndpoint = string.IsNullOrWhiteSpace(FlexIpfsGwEndpoint) ? null : FlexIpfsGwEndpoint.Trim();

        return this with
        {
            BackendBaseUrl = backendBaseUrl,
            DataDir = dataDir,
            BbsNodePath = bbsNodePath,
            FlexIpfsBaseUrl = flexIpfsBaseUrl,
            FlexIpfsBaseDir = flexIpfsBaseDir,
            FlexIpfsGwEndpoint = flexIpfsGwEndpoint,
        };
    }
}
