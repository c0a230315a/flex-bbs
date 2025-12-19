using BbsClient.Api;
using BbsClient.Storage;
using BbsClient.Ui;
using BbsClient.Util;

var ct = CancellationToken.None;

if (args.Any(a => a is "-h" or "--help"))
{
    PrintHelp();
    return 0;
}

var configStore = new ClientConfigStore(ClientConfigStore.DefaultPath());
ClientConfig persistedConfig;
try
{
    persistedConfig = await configStore.LoadAsync(ct);
}
catch (Exception ex)
{
    Console.Error.WriteLine($"config load error: {ex.Message}");
    persistedConfig = new ClientConfig().Normalize();
}

var backend = GetOption(args, "--backend") ?? persistedConfig.BackendBaseUrl;
var dataDir = GetOption(args, "--data-dir") ?? persistedConfig.DataDir;
var startBackend = GetBoolOption(args, "--start-backend")
                   ?? (HasFlag(args, "--no-start-backend") ? false : persistedConfig.StartBackend);
var bbsNodePath = GetOption(args, "--bbs-node-path") ?? persistedConfig.BbsNodePath;

var flexIpfsBaseUrl = GetOption(args, "--flexipfs-base-url") ?? persistedConfig.FlexIpfsBaseUrl;
var flexIpfsBaseDir = GetOption(args, "--flexipfs-base-dir") ?? persistedConfig.FlexIpfsBaseDir;
var flexIpfsGwEndpoint = GetOption(args, "--flexipfs-gw-endpoint") ?? persistedConfig.FlexIpfsGwEndpoint;
var autostartFlexIpfs = GetBoolOption(args, "--autostart-flexipfs")
                        ?? (HasFlag(args, "--no-autostart-flexipfs") ? false : persistedConfig.AutostartFlexIpfs);

var effectiveConfig = persistedConfig with
{
    BackendBaseUrl = backend,
    DataDir = dataDir,
    StartBackend = startBackend,
    BbsNodePath = bbsNodePath,
    FlexIpfsBaseUrl = flexIpfsBaseUrl,
    FlexIpfsBaseDir = flexIpfsBaseDir,
    FlexIpfsGwEndpoint = flexIpfsGwEndpoint,
    AutostartFlexIpfs = autostartFlexIpfs,
};
effectiveConfig = effectiveConfig.Normalize();
backend = effectiveConfig.BackendBaseUrl;
dataDir = effectiveConfig.DataDir;
startBackend = effectiveConfig.StartBackend;
bbsNodePath = effectiveConfig.BbsNodePath;

using var launcher = new BackendLauncher();
var cmdIndex = FindCommandIndex(args);
var command = cmdIndex < 0 ? "ui" : args[cmdIndex];
var rest = cmdIndex < 0 ? [] : args.Skip(cmdIndex + 1).ToArray();

if (command == "ui")
{
    return await InteractiveUi.RunAsync(configStore, effectiveConfig, launcher, ct);
}

var keysPath = Path.Combine(dataDir, "keys.json");
var blockedPath = Path.Combine(dataDir, "blockedPubKeys.json");
var keyStore = new KeyStore(keysPath);
var blockedStore = new BlockedStore(blockedPath);

if (command == "keys")
{
    return await HandleKeys(keyStore, rest, ct);
}
if (command == "blocked")
{
    return await HandleBlocked(blockedStore, rest, ct);
}

var needsBackend = command is "boards" or "threads" or "thread"
    or "create-thread" or "add-post" or "edit-post" or "tombstone-post";

if (!needsBackend)
{
    Console.Error.WriteLine($"Unknown command: {command}");
    PrintHelp();
    return 2;
}

try
{
    var bbsNodeArgs = BbsNodeArgsBuilder.Build(effectiveConfig);
    await launcher.EnsureRunningAsync(
        backend,
        startBackend,
        bbsNodePath ?? BbsNodePathResolver.Resolve(),
        bbsNodeArgs,
        ct
    );
}
catch (Exception ex)
{
    Console.Error.WriteLine(ex.Message);
    return ex is InvalidOperationException or UriFormatException ? 2 : 1;
}

using var http = new HttpClient();
var api = new BbsApiClient(http, backend);

try
{
    switch (command)
    {
        case "boards":
            return await HandleBoards(api, rest, ct);

        case "threads":
            return await HandleThreads(api, rest, ct);

        case "thread":
            return await HandleThread(api, blockedStore, rest, ct);

        case "create-thread":
            return await HandleCreateThread(api, keyStore, rest, ct);

        case "add-post":
            return await HandleAddPost(api, keyStore, rest, ct);

        case "edit-post":
            return await HandleEditPost(api, keyStore, rest, ct);

        case "tombstone-post":
            return await HandleTombstonePost(api, keyStore, rest, ct);

        default:
            Console.Error.WriteLine($"Unknown command: {command}");
            PrintHelp();
            return 2;
    }
}
catch (Exception ex)
{
    Console.Error.WriteLine(ex.Message);
    return 1;
}

static async Task<int> HandleKeys(KeyStore store, string[] args, CancellationToken ct)
{
    if (args.Length == 0)
    {
        Console.Error.WriteLine("keys: list|generate|delete");
        return 2;
    }
    switch (args[0])
    {
        case "list":
        {
            var keys = await store.LoadAsync(ct);
            foreach (var k in keys.OrderBy(k => k.Name, StringComparer.OrdinalIgnoreCase))
            {
                Console.WriteLine($"{k.Name}\t{k.Pub}");
            }
            return 0;
        }
        case "generate":
        {
            var name = GetOption(args, "--name") ?? "default";
            var k = await store.GenerateAsync(name, ct);
            Console.WriteLine($"{k.Name}\t{k.Pub}");
            return 0;
        }
        case "delete":
        {
            var name = GetOption(args, "--name") ?? "";
            if (string.IsNullOrWhiteSpace(name))
            {
                Console.Error.WriteLine("keys delete: --name is required");
                return 2;
            }
            await store.DeleteAsync(name, ct);
            Console.WriteLine("ok");
            return 0;
        }
        default:
            Console.Error.WriteLine("keys: list|generate|delete");
            return 2;
    }
}

static async Task<int> HandleBlocked(BlockedStore store, string[] args, CancellationToken ct)
{
    if (args.Length == 0)
    {
        Console.Error.WriteLine("blocked: list|add|remove");
        return 2;
    }
    switch (args[0])
    {
        case "list":
        {
            var keys = await store.LoadAsync(ct);
            foreach (var k in keys.Order(StringComparer.Ordinal))
            {
                Console.WriteLine(k);
            }
            return 0;
        }
        case "add":
        {
            var pub = GetOption(args, "--pub") ?? "";
            if (string.IsNullOrWhiteSpace(pub))
            {
                Console.Error.WriteLine("blocked add: --pub is required");
                return 2;
            }
            await store.AddAsync(pub, ct);
            Console.WriteLine("ok");
            return 0;
        }
        case "remove":
        {
            var pub = GetOption(args, "--pub") ?? "";
            if (string.IsNullOrWhiteSpace(pub))
            {
                Console.Error.WriteLine("blocked remove: --pub is required");
                return 2;
            }
            await store.RemoveAsync(pub, ct);
            Console.WriteLine("ok");
            return 0;
        }
        default:
            Console.Error.WriteLine("blocked: list|add|remove");
            return 2;
    }
}

static async Task<int> HandleBoards(BbsApiClient api, string[] args, CancellationToken ct)
{
    _ = args;
    var boards = await api.GetBoardsAsync(ct);
    foreach (var b in boards)
    {
        Console.WriteLine($"{b.Board.BoardID}\t{b.Board.Title}\t{b.BoardMetaCID}");
    }
    return 0;
}

static async Task<int> HandleThreads(BbsApiClient api, string[] args, CancellationToken ct)
{
    if (args.Length == 0)
    {
        Console.Error.WriteLine("threads: <boardId> [--limit N] [--offset N]");
        return 2;
    }
    var boardId = args[0];
    var limit = int.TryParse(GetOption(args, "--limit"), out var l) ? l : 50;
    var offset = int.TryParse(GetOption(args, "--offset"), out var o) ? o : 0;
    var threads = await api.GetThreadsAsync(boardId, limit, offset, ct);
    foreach (var t in threads)
    {
        Console.WriteLine($"{t.ThreadID}\t{t.Thread.Title}\t{t.Thread.CreatedAt}");
    }
    return 0;
}

static async Task<int> HandleThread(BbsApiClient api, BlockedStore blocked, string[] args, CancellationToken ct)
{
    if (args.Length == 0)
    {
        Console.Error.WriteLine("thread: <threadId>");
        return 2;
    }
    var threadId = args[0];
    var blockedKeys = await blocked.LoadAsync(ct);
    var tr = await api.GetThreadAsync(threadId, ct);

    Console.WriteLine($"[{tr.ThreadMeta.BoardID}] {tr.ThreadMeta.Title}");
    foreach (var p in tr.Posts)
    {
        if (blockedKeys.Contains(p.Post.AuthorPubKey))
        {
            continue;
        }

        var head = $"{p.CID}\t{p.Post.DisplayName}\t{p.Post.AuthorPubKey}\t{p.Post.CreatedAt}";
        if (!string.IsNullOrWhiteSpace(p.Post.EditedAt))
        {
            head += $"\t(edited {p.Post.EditedAt})";
        }
        Console.WriteLine(head);

        if (p.Tombstoned)
        {
            Console.WriteLine($"  [tombstoned] {p.TombstoneReason ?? ""}".TrimEnd());
            continue;
        }
        Console.WriteLine($"  {p.Post.Body.Content.Replace("\n", "\n  ")}");
    }
    return 0;
}

static async Task<int> HandleCreateThread(BbsApiClient api, KeyStore keys, string[] args, CancellationToken ct)
{
    var boardId = GetOption(args, "--board") ?? "";
    var title = GetOption(args, "--title") ?? "";
    var body = GetOption(args, "--body") ?? "";
    var keyName = GetOption(args, "--key") ?? "default";
    var displayName = GetOption(args, "--name") ?? "";

    if (string.IsNullOrWhiteSpace(boardId) || string.IsNullOrWhiteSpace(title) || string.IsNullOrWhiteSpace(body))
    {
        Console.Error.WriteLine("create-thread: --board --title --body are required");
        return 2;
    }

    var key = await keys.FindAsync(keyName, ct);
    if (key == null)
    {
        Console.Error.WriteLine($"key not found: {keyName}");
        return 2;
    }

    var resp = await api.CreateThreadAsync(new CreateThreadRequest
    {
        BoardID = boardId,
        Title = title,
        DisplayName = displayName,
        Body = new PostBody { Format = "markdown", Content = body },
        AuthorPrivKey = key.Priv,
    }, ct);

    Console.WriteLine(resp.ThreadID);
    return 0;
}

static async Task<int> HandleAddPost(BbsApiClient api, KeyStore keys, string[] args, CancellationToken ct)
{
    var threadId = GetOption(args, "--thread") ?? "";
    var body = GetOption(args, "--body") ?? "";
    var keyName = GetOption(args, "--key") ?? "default";
    var displayName = GetOption(args, "--name") ?? "";
    var parent = GetOption(args, "--parent");

    if (string.IsNullOrWhiteSpace(threadId) || string.IsNullOrWhiteSpace(body))
    {
        Console.Error.WriteLine("add-post: --thread --body are required");
        return 2;
    }

    var key = await keys.FindAsync(keyName, ct);
    if (key == null)
    {
        Console.Error.WriteLine($"key not found: {keyName}");
        return 2;
    }

    var resp = await api.AddPostAsync(new AddPostRequest
    {
        ThreadID = threadId,
        ParentPostCID = string.IsNullOrWhiteSpace(parent) ? null : parent,
        DisplayName = displayName,
        Body = new PostBody { Format = "markdown", Content = body },
        AuthorPrivKey = key.Priv,
    }, ct);
    Console.WriteLine(resp.PostCID);
    return 0;
}

static async Task<int> HandleEditPost(BbsApiClient api, KeyStore keys, string[] args, CancellationToken ct)
{
    var postCid = GetOption(args, "--post") ?? "";
    var body = GetOption(args, "--body") ?? "";
    var keyName = GetOption(args, "--key") ?? "default";
    var displayName = GetOption(args, "--name");

    if (string.IsNullOrWhiteSpace(postCid) || string.IsNullOrWhiteSpace(body))
    {
        Console.Error.WriteLine("edit-post: --post --body are required");
        return 2;
    }

    var key = await keys.FindAsync(keyName, ct);
    if (key == null)
    {
        Console.Error.WriteLine($"key not found: {keyName}");
        return 2;
    }

    var resp = await api.EditPostAsync(postCid, new EditPostRequest
    {
        Body = new PostBody { Format = "markdown", Content = body },
        DisplayName = displayName,
        AuthorPrivKey = key.Priv,
    }, ct);
    Console.WriteLine(resp.NewPostCID);
    return 0;
}

static async Task<int> HandleTombstonePost(BbsApiClient api, KeyStore keys, string[] args, CancellationToken ct)
{
    var postCid = GetOption(args, "--post") ?? "";
    var reason = GetOption(args, "--reason");
    var keyName = GetOption(args, "--key") ?? "default";

    if (string.IsNullOrWhiteSpace(postCid))
    {
        Console.Error.WriteLine("tombstone-post: --post is required");
        return 2;
    }

    var key = await keys.FindAsync(keyName, ct);
    if (key == null)
    {
        Console.Error.WriteLine($"key not found: {keyName}");
        return 2;
    }

    var resp = await api.TombstonePostAsync(postCid, new TombstonePostRequest
    {
        Reason = string.IsNullOrWhiteSpace(reason) ? null : reason,
        AuthorPrivKey = key.Priv,
    }, ct);
    Console.WriteLine(resp.TargetPostCID);
    return 0;
}

static string? GetOption(string[] args, string name)
{
    for (var i = 0; i < args.Length; i++)
    {
        var a = args[i];
        if (a == name && i + 1 < args.Length)
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

static bool HasFlag(string[] args, string name)
{
    return args.Any(a => string.Equals(a, name, StringComparison.Ordinal));
}

static bool? GetBoolOption(string[] args, string name)
{
    for (var i = 0; i < args.Length; i++)
    {
        var a = args[i];
        if (a == name)
        {
            return true;
        }
        if (a.StartsWith(name + "=", StringComparison.Ordinal))
        {
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
    }
    return null;
}

static int FindCommandIndex(string[] args)
{
    for (var i = 0; i < args.Length; i++)
    {
        var a = args[i];
        if (a == "--")
        {
            return i + 1 < args.Length ? i + 1 : -1;
        }
        if (!a.StartsWith("-", StringComparison.Ordinal))
        {
            return i;
        }
        if (a.Contains('=', StringComparison.Ordinal))
        {
            continue;
        }
        if (OptionTakesValue(a) && i + 1 < args.Length && !args[i + 1].StartsWith("-", StringComparison.Ordinal))
        {
            i++;
        }
    }
    return -1;
}

static bool OptionTakesValue(string name)
{
    return name is "--backend" or "--data-dir" or "--bbs-node-path"
        or "--flexipfs-base-url" or "--flexipfs-base-dir" or "--flexipfs-gw-endpoint";
}

static void PrintHelp()
{
    Console.WriteLine("BbsClient");
    Console.WriteLine();
    Console.WriteLine("Global options:");
    Console.WriteLine("  --backend <url>      (default: http://127.0.0.1:8080)");
    Console.WriteLine("  --data-dir <path>    (default: OS app data dir)");
    Console.WriteLine("  --start-backend      (default: true; start bbs-node if not running)");
    Console.WriteLine("  --no-start-backend   (disable auto-start)");
    Console.WriteLine("  --bbs-node-path <p>  (path to bbs-node)");
    Console.WriteLine("  --flexipfs-base-url <url>     (default: http://127.0.0.1:5001/api/v0)");
    Console.WriteLine("  --flexipfs-base-dir <path>    (default: auto-detect)");
    Console.WriteLine("  --flexipfs-gw-endpoint <val>  (override ipfs.endpoint on autostart)");
    Console.WriteLine("  --autostart-flexipfs=<bool>   (default: true)");
    Console.WriteLine("  --no-autostart-flexipfs       (disable flex-ipfs autostart)");
    Console.WriteLine();
    Console.WriteLine("Commands:");
    Console.WriteLine("  ui  (interactive TUI, default)");
    Console.WriteLine("  keys list|generate --name <name>|delete --name <name>");
    Console.WriteLine("  blocked list|add --pub <pubKey>|remove --pub <pubKey>");
    Console.WriteLine("  boards");
    Console.WriteLine("  threads <boardId> [--limit N] [--offset N]");
    Console.WriteLine("  thread <threadId>");
    Console.WriteLine("  create-thread --board <boardId> --title <title> --body <text> [--key <name>] [--name <displayName>]");
    Console.WriteLine("  add-post --thread <threadId> --body <text> [--parent <postCid>] [--key <name>] [--name <displayName>]");
    Console.WriteLine("  edit-post --post <postCid> --body <text> [--key <name>] [--name <displayName>]");
    Console.WriteLine("  tombstone-post --post <postCid> [--reason <text>] [--key <name>]");
}
