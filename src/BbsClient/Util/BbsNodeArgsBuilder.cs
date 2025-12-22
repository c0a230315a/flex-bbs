using BbsClient.Storage;

namespace BbsClient.Util;

public static class BbsNodeArgsBuilder
{
    public static string[] Build(ClientConfig cfg)
    {
        cfg = cfg.Normalize();

        var uri = new Uri(cfg.BackendBaseUrl);
        var hostPort = $"{uri.Host}:{uri.Port}";

        var args = new List<string>
        {
            $"--role={cfg.BackendRole}",
            "--http", hostPort,
            "--data-dir", cfg.DataDir,
            "--flexipfs-base-url", cfg.FlexIpfsBaseUrl,
            $"--autostart-flexipfs={cfg.AutostartFlexIpfs.ToString().ToLowerInvariant()}",
            $"--flexipfs-mdns={cfg.FlexIpfsMdns.ToString().ToLowerInvariant()}",
        };

        if (!string.IsNullOrWhiteSpace(cfg.FlexIpfsBaseDir))
        {
            args.Add("--flexipfs-base-dir");
            args.Add(cfg.FlexIpfsBaseDir);
        }

        if (!string.IsNullOrWhiteSpace(cfg.FlexIpfsGwEndpoint))
        {
            args.Add("--flexipfs-gw-endpoint");
            args.Add(cfg.FlexIpfsGwEndpoint);
        }

        return args.ToArray();
    }

    public static bool IsLocalBaseUrl(string baseUrl)
    {
        if (!Uri.TryCreate(baseUrl, UriKind.Absolute, out var u))
        {
            return true;
        }
        var host = u.Host.Trim().ToLowerInvariant();
        return host is "" or "127.0.0.1" or "localhost" || host.StartsWith("0.0.0.0", StringComparison.Ordinal);
    }
}
