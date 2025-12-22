using System.Diagnostics;

namespace BbsClient.Util;

public sealed class BackendLauncher : IDisposable
{
    private Process? _process;
    private StreamWriter? _logWriter;
    private string? _logFilePath;
    private readonly object _logLock = new();
    private readonly Queue<string> _recentLogs = new();
    private const int MaxRecentLogs = 200;

    public bool IsManagingProcess => _process != null && !_process.HasExited;

    public int? ManagedPid => _process is { HasExited: false } p ? p.Id : null;

    public string? LogFilePath => _logFilePath;

    public async Task EnsureRunningAsync(string backendBaseUrl, bool startBackend, string? bbsNodePath, string[] bbsNodeArgs, CancellationToken ct)
    {
        if (await IsHealthyAsync(backendBaseUrl, ct))
        {
            return;
        }
        if (!startBackend)
        {
            AppLog.Warn($"backend not healthy at {backendBaseUrl} (auto-start disabled)");
            return;
        }
        if (string.IsNullOrWhiteSpace(bbsNodePath))
        {
            throw new InvalidOperationException("bbs-node not found. Set --bbs-node-path, or disable auto-start with --no-start-backend.");
        }
        Start(bbsNodePath, bbsNodeArgs);
        await WaitHealthyWithDiagnosticsAsync(backendBaseUrl, ComputeStartupTimeout(bbsNodeArgs), ct);
    }

    public void StopManaged()
    {
        if (_process == null)
        {
            return;
        }
        AppLog.Info("stopping managed bbs-node");
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
        try
        {
            _process.Dispose();
        }
        catch
        {
        }
        _process = null;
        CloseLog();
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
        await WaitHealthyWithDiagnosticsAsync(backendBaseUrl, ComputeStartupTimeout(bbsNodeArgs), ct);
        return true;
    }

    private void Start(string bbsNodePath, string[] bbsNodeArgs)
    {
        if (_process != null && !_process.HasExited)
        {
            return;
        }

        CloseLog();
        var dataDir = TryGetArgValue(bbsNodeArgs, "--data-dir");
        if (string.IsNullOrWhiteSpace(dataDir))
        {
            dataDir = ConfigPaths.DefaultAppDir();
        }
        try
        {
            var logsDir = Path.Combine(dataDir, "logs");
            Directory.CreateDirectory(logsDir);
            _logFilePath = Path.Combine(logsDir, "bbs-node.log");
            _logWriter = new StreamWriter(File.Open(_logFilePath, FileMode.Append, FileAccess.Write, FileShare.ReadWrite))
            {
                AutoFlush = true,
            };
            _logWriter.WriteLine($"----- bbs-node start {DateTimeOffset.Now:O} -----");
        }
        catch
        {
            _logFilePath = null;
            _logWriter = null;
        }

        var psi = new ProcessStartInfo
        {
            FileName = bbsNodePath,
            UseShellExecute = false,
            RedirectStandardOutput = true,
            RedirectStandardError = true,
            RedirectStandardInput = true,
            WorkingDirectory = AppContext.BaseDirectory,
        };
        foreach (var a in bbsNodeArgs)
        {
            psi.ArgumentList.Add(a);
        }
        AppLog.Info($"starting bbs-node: {bbsNodePath} {string.Join(" ", bbsNodeArgs)}");
        _process = Process.Start(psi) ?? throw new InvalidOperationException("failed to start bbs-node");
        AppLog.Info($"bbs-node started pid={_process.Id} log={_logFilePath ?? "<none>"}");

        try
        {
            _process.StandardInput.Close();
        }
        catch
        {
        }

        StartLogPump(_process.StandardOutput, "OUT");
        StartLogPump(_process.StandardError, "ERR");
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

    private async Task WaitHealthyWithDiagnosticsAsync(string backendBaseUrl, TimeSpan timeout, CancellationToken ct)
    {
        try
        {
            await WaitHealthyAsync(backendBaseUrl, timeout, ct);
        }
        catch (TimeoutException ex)
        {
            var hint = _logFilePath == null ? "" : $" (see {_logFilePath})";
            var tail = string.Join("\n", GetRecentLogs(30));
            if (!string.IsNullOrWhiteSpace(tail))
            {
                AppLog.Error($"backend did not become healthy in time{hint}", ex);
                throw new TimeoutException($"backend did not become healthy in time{hint}\n{tail}", ex);
            }
            AppLog.Error($"backend did not become healthy in time{hint}", ex);
            throw new TimeoutException($"backend did not become healthy in time{hint}", ex);
        }
    }

    private static TimeSpan ComputeStartupTimeout(string[] bbsNodeArgs)
    {
        // bbs-node may block startup while waiting for flex-ipfs autostart (up to ~20s).
        var autostartFlex = GetBoolArg(bbsNodeArgs, "--autostart-flexipfs");
        var flexBaseUrl = TryGetArgValue(bbsNodeArgs, "--flexipfs-base-url");
        if (autostartFlex == true && BbsNodeArgsBuilder.IsLocalBaseUrl(flexBaseUrl ?? ""))
        {
            return TimeSpan.FromSeconds(45);
        }
        return TimeSpan.FromSeconds(15);
    }

    private static bool? GetBoolArg(string[] args, string name)
    {
        for (var i = 0; i < args.Length; i++)
        {
            var a = args[i];
            if (string.Equals(a, name, StringComparison.Ordinal))
            {
                return true;
            }
            if (!a.StartsWith(name + "=", StringComparison.Ordinal))
            {
                continue;
            }
            var v = a[(name.Length + 1)..].Trim();
            if (bool.TryParse(v, out var b))
            {
                return b;
            }
            if (string.Equals(v, "1", StringComparison.Ordinal))
            {
                return true;
            }
            if (string.Equals(v, "0", StringComparison.Ordinal))
            {
                return false;
            }
        }
        return null;
    }

    private static string? TryGetArgValue(string[] args, string name)
    {
        for (var i = 0; i < args.Length; i++)
        {
            var a = args[i];
            if (string.Equals(a, name, StringComparison.Ordinal) && i + 1 < args.Length)
            {
                return args[i + 1];
            }
            if (a.StartsWith(name + "=", StringComparison.Ordinal))
            {
                return a[(name.Length + 1)..];
            }
        }
        return null;
    }

    private void StartLogPump(StreamReader reader, string stream)
    {
        _ = Task.Run(async () =>
        {
            try
            {
                while (true)
                {
                    var line = await reader.ReadLineAsync().ConfigureAwait(false);
                    if (line == null)
                    {
                        break;
                    }
                    AppendLog(line, stream);
                }
            }
            catch
            {
            }
        });
    }

    private void AppendLog(string line, string stream)
    {
        if (line.Contains("Received: NCL_PING", StringComparison.Ordinal))
        {
            return;
        }

        var entry = $"{DateTimeOffset.Now:O} [{stream}] {line}";
        lock (_logLock)
        {
            try
            {
                _logWriter?.WriteLine(entry);
            }
            catch
            {
            }

            _recentLogs.Enqueue(entry);
            while (_recentLogs.Count > MaxRecentLogs)
            {
                _recentLogs.Dequeue();
            }
        }

    }

    private IEnumerable<string> GetRecentLogs(int maxLines)
    {
        lock (_logLock)
        {
            var skip = Math.Max(0, _recentLogs.Count - Math.Max(0, maxLines));
            return _recentLogs.Skip(skip).ToArray();
        }
    }

    private void CloseLog()
    {
        lock (_logLock)
        {
            if (_logWriter == null)
            {
                _logFilePath = null;
                _recentLogs.Clear();
                return;
            }
            try
            {
                _logWriter.WriteLine($"----- bbs-node stop {DateTimeOffset.Now:O} -----");
            }
            catch
            {
            }
            try
            {
                _logWriter.Dispose();
            }
            catch
            {
            }
            _logWriter = null;
            _logFilePath = null;
            _recentLogs.Clear();
        }
    }

    public void Dispose()
    {
        StopManaged();
    }
}
