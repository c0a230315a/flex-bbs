namespace BbsClient.Util;

public static class BbsNodePathResolver
{
    public static string? Resolve()
    {
        try
        {
            var rid = System.Runtime.InteropServices.RuntimeInformation.RuntimeIdentifier;
            var exe = OperatingSystem.IsWindows() ? "bbs-node.exe" : "bbs-node";

            var candidates = new List<string>
            {
                Path.Combine(AppContext.BaseDirectory, "runtimes", rid, "bbs-node", exe),
                Path.Combine(Environment.CurrentDirectory, "backend-go", exe),
                Path.Combine(AppContext.BaseDirectory, "backend-go", exe),
            };

            if (OperatingSystem.IsLinux())
            {
                candidates.Add(Path.Combine(Environment.CurrentDirectory, "bbs-node-linux-amd64"));
                candidates.Add(Path.Combine(AppContext.BaseDirectory, "bbs-node-linux-amd64"));
            }
            else if (OperatingSystem.IsMacOS())
            {
                candidates.Add(Path.Combine(Environment.CurrentDirectory, "bbs-node-darwin-amd64"));
                candidates.Add(Path.Combine(AppContext.BaseDirectory, "bbs-node-darwin-amd64"));
            }
            else if (OperatingSystem.IsWindows())
            {
                candidates.Add(Path.Combine(Environment.CurrentDirectory, "bbs-node-windows-amd64.exe"));
                candidates.Add(Path.Combine(AppContext.BaseDirectory, "bbs-node-windows-amd64.exe"));
            }

            foreach (var c in candidates)
            {
                if (File.Exists(c))
                {
                    return c;
                }
            }

            return null;
        }
        catch
        {
            return null;
        }
    }
}

