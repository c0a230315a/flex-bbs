using System.Diagnostics;
using System.Net.Http;

namespace BbsClient.Util;

public sealed class BackendLauncher : IDisposable
{
    private Process? _process;

    public bool IsManagingProcess => _process != null && !_process.HasExited;

    public int? ManagedPid => _process is { HasExited: false } p ? p.Id : null;

    public async Task EnsureRunningAsync(string backendBaseUrl, bool startBackend, string? bbsNodePath, string[] bbsNodeArgs, CancellationToken ct)
    {
        if (await IsHealthyAsync(backendBaseUrl, ct))
        {
            return;
        }
        if (!startBackend)
        {
            return;
        }
        if (string.IsNullOrWhiteSpace(bbsNodePath))
        {
            throw new InvalidOperationException("bbs-node not found. Set --bbs-node-path, or disable auto-start with --no-start-backend.");
        }
        Start(bbsNodePath, bbsNodeArgs);
        await WaitHealthyAsync(backendBaseUrl, TimeSpan.FromSeconds(15), ct);
    }

    public void StopManaged()
    {
        if (_process == null)
        {
            return;
        }
        try
        {
            if (!_process.HasExited)
            {
                _process.Kill(entireProcessTree: true);
                _process.WaitForExit(milliseconds: 5000);
            }
        }
        catch
        {
        }
        _process = null;
    }

    public async Task<bool> RestartManagedAsync(string backendBaseUrl, bool startBackend, string? bbsNodePath, string[] bbsNodeArgs, CancellationToken ct)
    {
        if (!startBackend)
        {
            return false;
        }
        if (!IsManagingProcess)
        {
            return false;
        }
        if (string.IsNullOrWhiteSpace(bbsNodePath))
        {
            throw new InvalidOperationException("bbs-node not found. Set --bbs-node-path, or disable auto-start with --no-start-backend.");
        }

        StopManaged();
        Start(bbsNodePath, bbsNodeArgs);
        await WaitHealthyAsync(backendBaseUrl, TimeSpan.FromSeconds(15), ct);
        return true;
    }

    private void Start(string bbsNodePath, string[] bbsNodeArgs)
    {
        if (_process != null && !_process.HasExited)
        {
            return;
        }
        var psi = new ProcessStartInfo
        {
            FileName = bbsNodePath,
            UseShellExecute = false,
            RedirectStandardOutput = false,
            RedirectStandardError = false,
            Arguments = string.Join(" ", bbsNodeArgs.Select(QuoteArg)),
            WorkingDirectory = AppContext.BaseDirectory,
        };
        _process = Process.Start(psi) ?? throw new InvalidOperationException("failed to start bbs-node");
    }

    public static async Task<bool> IsHealthyAsync(string backendBaseUrl, CancellationToken ct)
    {
        try
        {
            using var http = new HttpClient { Timeout = TimeSpan.FromSeconds(2) };
            var resp = await http.GetAsync(backendBaseUrl.TrimEnd('/') + "/healthz", ct);
            return resp.IsSuccessStatusCode;
        }
        catch
        {
            return false;
        }
    }

    private static async Task WaitHealthyAsync(string backendBaseUrl, TimeSpan timeout, CancellationToken ct)
    {
        var start = DateTimeOffset.UtcNow;
        while (DateTimeOffset.UtcNow - start < timeout)
        {
            if (await IsHealthyAsync(backendBaseUrl, ct))
            {
                return;
            }
            await Task.Delay(300, ct);
        }
        throw new TimeoutException("backend did not become healthy in time");
    }

    private static string QuoteArg(string arg)
    {
        if (arg.Length == 0)
        {
            return "\"\"";
        }
        if (arg.Any(char.IsWhiteSpace) || arg.Contains('"'))
        {
            return "\"" + arg.Replace("\"", "\\\"") + "\"";
        }
        return arg;
    }

    public void Dispose()
    {
        if (_process == null)
        {
            return;
        }
        try
        {
            if (!_process.HasExited)
            {
                _process.Kill(entireProcessTree: true);
            }
        }
        catch
        {
        }
    }
}
