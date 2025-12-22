using System.Text;

namespace BbsClient.Util;

public static class AppLog
{
    private static readonly object Gate = new();
    private static StreamWriter? _writer;

    public static string? LogFilePath { get; private set; }

    public static void Init(string dataDir)
    {
        dataDir = (dataDir ?? "").Trim();
        if (string.IsNullOrWhiteSpace(dataDir))
        {
            dataDir = ConfigPaths.DefaultAppDir();
        }

        var logsDir = Path.Combine(dataDir, "logs");
        var nextPath = Path.Combine(logsDir, "bbs-client.log");

        lock (Gate)
        {
            if (_writer != null && string.Equals(LogFilePath, nextPath, StringComparison.Ordinal))
            {
                return;
            }

            CloseLocked();

            try
            {
                Directory.CreateDirectory(logsDir);
                var fs = File.Open(nextPath, FileMode.Append, FileAccess.Write, FileShare.ReadWrite);
                _writer = new StreamWriter(fs, new UTF8Encoding(encoderShouldEmitUTF8Identifier: false))
                {
                    AutoFlush = true,
                };
                LogFilePath = nextPath;
                _writer.WriteLine($"----- bbs-client start {DateTimeOffset.Now:O} -----");
            }
            catch
            {
                _writer = null;
                LogFilePath = null;
            }
        }
    }

    public static void Info(string message) => Write("INFO", message, null);

    public static void Warn(string message) => Write("WARN", message, null);

    public static void Error(string message, Exception? ex = null) => Write("ERROR", message, ex);

    public static void Http(string method, string url, int statusCode, TimeSpan elapsed, string? detail = null)
    {
        var msg = $"{method} {url} -> {statusCode} ({elapsed.TotalMilliseconds:0}ms)";
        if (!string.IsNullOrWhiteSpace(detail))
        {
            msg += $" :: {detail}";
        }
        Write("HTTP", msg, null);
    }

    private static void Write(string level, string message, Exception? ex)
    {
        message = (message ?? "").Replace("\r", "\\r").Replace("\n", "\\n");
        var line = $"{DateTimeOffset.Now:O} [{level}] {message}";
        lock (Gate)
        {
            try
            {
                _writer?.WriteLine(line);
                if (ex != null)
                {
                    _writer?.WriteLine(ex.ToString());
                }
            }
            catch
            {
            }
        }
    }

    public static void Close()
    {
        lock (Gate)
        {
            CloseLocked();
        }
    }

    private static void CloseLocked()
    {
        if (_writer == null)
        {
            LogFilePath = null;
            return;
        }

        try
        {
            _writer.WriteLine($"----- bbs-client stop {DateTimeOffset.Now:O} -----");
        }
        catch
        {
        }
        try
        {
            _writer.Dispose();
        }
        catch
        {
        }
        _writer = null;
        LogFilePath = null;
    }
}

