using BbsClient.Api;
using BbsClient.Storage;
using BbsClient.Util;
using System.Diagnostics;
using System.Globalization;
using System.Text;
using System.Text.Json;
using Spectre.Console;

namespace BbsClient.Ui;

public static class InteractiveUi
{
    private const int DefaultPageSize = 50;
    private static UiLocalizer _loc = UiLocalizer.Create("auto");
    private static TimeZoneInfo _uiTimeZone = TimeZoneInfo.Utc;
    private static string _uiTimeZoneLabel = "UTC";

    private static string L(string text) => _loc.L(text);
    private static string ML(string text) => Markup.Escape(L(text));
    private static string F(string format, params object[] args) => _loc.F(format, args);
    private static void SetLanguage(string? languageSetting) => _loc = UiLocalizer.Create(languageSetting);
    private static void SetTimeZone(string? timeZoneSetting) => (_uiTimeZone, _uiTimeZoneLabel) = ResolveTimeZone(timeZoneSetting);

    public static async Task<int> RunAsync(
        ClientConfigStore configStore,
        ClientConfig initialConfig,
        BackendLauncher launcher,
        CancellationToken ct
    )
    {
        var cfg = initialConfig.Normalize();
        SetLanguage(cfg.UiLanguage);
        SetTimeZone(cfg.UiTimeZone);

        if (Console.IsInputRedirected || Console.IsOutputRedirected)
        {
            Console.Error.WriteLine(L("ui requires an interactive terminal (no redirection)."));
            return 2;
        }

        AppLog.Init(cfg.DataDir);

        using var http = new HttpClient();
        var api = new BbsApiClient(http, cfg.BackendBaseUrl);
        var keys = new KeyStore(Path.Combine(cfg.DataDir, "keys.json"));
        var blocked = new BlockedStore(Path.Combine(cfg.DataDir, "blockedPubKeys.json"));

        string? backendStartError = null;
        try
        {
            var bbsNodePath = cfg.BbsNodePath ?? BbsNodePathResolver.Resolve();
            await AnsiConsole.Status()
                .Spinner(Spinner.Known.Dots)
                .StartAsync(L("Ensuring backend is running..."), async _ =>
                {
                    await launcher.EnsureRunningAsync(cfg.BackendBaseUrl, cfg.StartBackend, bbsNodePath, BbsNodeArgsBuilder.Build(cfg), ct);
                });
        }
        catch (Exception ex)
        {
            backendStartError = ex.Message;
            AppLog.Error("backend start failed", ex);
        }

        while (true)
        {
            var healthy = await BackendLauncher.IsHealthyAsync(cfg.BackendBaseUrl, ct);

            AnsiConsole.Clear();
            AnsiConsole.Write(new Rule($"[bold]{ML("Flex BBS Client")}[/]").LeftJustified());
            AnsiConsole.MarkupLine($"[grey]{ML("Backend")}:[/] {Markup.Escape(cfg.BackendBaseUrl)} [{(healthy ? "green" : "red")}]{ML(healthy ? "up" : "down")}[/]");
            AnsiConsole.MarkupLine($"[grey]{ML("Backend role (managed)")}:[/] {Markup.Escape(cfg.BackendRole)}");
            AnsiConsole.MarkupLine($"[grey]{ML("Data dir")}:[/] {Markup.Escape(cfg.DataDir)}");
            if (!string.IsNullOrWhiteSpace(backendStartError) && !healthy)
            {
                AnsiConsole.MarkupLine($"[red]{ML("Backend")}:[/] {Markup.Escape(backendStartError)}");
            }
            AnsiConsole.WriteLine();

            var choice = AnsiConsole.Prompt(
                new SelectionPrompt<string>()
                    .Title(L("Main menu"))
                    .AddChoices("Browse boards", "Search", "Keys", "Blocked", "Settings", "Quit")
                    .UseConverter(L)
            );

            try
            {
                switch (choice)
                {
                    case "Browse boards":
                        await BrowseBoardsAsync(api, cfg, keys, blocked, ct);
                        break;
                    case "Search":
                        await SearchAsync(api, keys, blocked, ct);
                        break;
                    case "Keys":
                        await KeysMenuAsync(keys, ct);
                        break;
                    case "Blocked":
                        await BlockedMenuAsync(blocked, ct);
                        break;
                    case "Settings":
                    {
                        var updated = await SettingsMenuAsync(configStore, cfg, launcher, ct);
                        if (updated != cfg)
                        {
                            cfg = updated.Normalize();
                            SetLanguage(cfg.UiLanguage);
                            SetTimeZone(cfg.UiTimeZone);
                            api = new BbsApiClient(http, cfg.BackendBaseUrl);
                            keys = new KeyStore(Path.Combine(cfg.DataDir, "keys.json"));
                            blocked = new BlockedStore(Path.Combine(cfg.DataDir, "blockedPubKeys.json"));
                        }
                        backendStartError = null;
                        break;
                    }
                    case "Quit":
                        return 0;
                }
            }
            catch (OperationCanceledException)
            {
                return 1;
            }
            catch (Exception ex)
            {
                ShowError(ex);
                Pause();
            }
        }
    }

    private static async Task BrowseBoardsAsync(BbsApiClient api, ClientConfig cfg, KeyStore keys, BlockedStore blocked, CancellationToken ct)
    {
        while (true)
        {
            AnsiConsole.Clear();
            AnsiConsole.Write(new Rule($"[bold]{ML("Boards")}[/]").LeftJustified());

            List<BoardItem> boards;
            try
            {
                boards = await api.GetBoardsAsync(ct);
            }
            catch (Exception ex)
            {
                ShowError(ex);
                Pause();
                return;
            }

            boards = boards
                .OrderBy(b => b.Board.BoardId, StringComparer.OrdinalIgnoreCase)
                .ToList();

            var table = new Table().Border(TableBorder.Rounded);
            table.Expand = true;
            table.AddColumn(L("BoardID"));
            table.AddColumn(L("Title"));
            table.AddColumn(L("MetaCID"));
            foreach (var b in boards)
            {
                table.AddRow(
                    Markup.Escape(b.Board.BoardId),
                    Markup.Escape(b.Board.Title),
                    Markup.Escape(b.BoardMetaCid)
                );
            }
            AnsiConsole.Write(table);
            AnsiConsole.WriteLine();

            if (boards.Count == 0)
            {
                AnsiConsole.MarkupLine($"[grey]{ML("No boards found.")}[/]");
                AnsiConsole.WriteLine();
            }

            var actions = new List<string>
            {
                "Open board",
                "Create board",
                "Add board",
                "Refresh",
                "Back",
            };
            var action = AnsiConsole.Prompt(new SelectionPrompt<string>().Title(L("Action")).AddChoices(actions).UseConverter(L));

            switch (action)
            {
                case "Open board":
                {
                    if (boards.Count == 0)
                    {
                        AnsiConsole.MarkupLine($"[grey]{ML("No boards to open.")}[/]");
                        Pause();
                        break;
                    }

                    var selected = PromptWithBack(
                        L("Select board"),
                        boards,
                        b => $"{Markup.Escape(b.Board.BoardId)}  {Markup.Escape(b.Board.Title)}  [grey]{Markup.Escape(Short(b.BoardMetaCid, 24))}[/]",
                        moreChoicesText: $"[grey]{ML("(move up and down to reveal more boards)")}[/]"
                    );
                    if (selected == null)
                    {
                        break;
                    }

                    await BrowseThreadsAsync(api, keys, blocked, selected.Board.BoardId, selected.Board.Title, ct);
                    break;
                }
                case "Create board":
                    await CreateBoardFlowAsync(cfg, keys, ct);
                    break;
                case "Add board":
                    await AddBoardFlowAsync(cfg, ct);
                    break;
                case "Refresh":
                    break;
                case "Back":
                    return;
            }
        }
    }

    private static async Task CreateBoardFlowAsync(ClientConfig cfg, KeyStore keys, CancellationToken ct)
    {
        var bbsNodePath = cfg.BbsNodePath ?? BbsNodePathResolver.Resolve();
        if (string.IsNullOrWhiteSpace(bbsNodePath))
        {
            AnsiConsole.MarkupLine($"[red]{ML("bbs-node not found.")}[/] {ML("Set it in Settings → Client / Backend.")}");
            Pause();
            return;
        }

        var key = await PromptKeyAsync(keys, ct);
        if (key == null)
        {
            return;
        }

        var boardId = AnsiConsole.Ask<string>(L("Board ID (e.g. bbs.general)"));
        if (string.IsNullOrWhiteSpace(boardId))
        {
            AnsiConsole.MarkupLine($"[yellow]{ML("Board ID is empty. Canceled.")}[/]");
            Pause();
            return;
        }
        boardId = boardId.Trim();

        var title = AnsiConsole.Ask<string>(L("Title"));
        if (string.IsNullOrWhiteSpace(title))
        {
            AnsiConsole.MarkupLine($"[yellow]{ML("Title is empty. Canceled.")}[/]");
            Pause();
            return;
        }
        title = title.Trim();

        var description = EmptyToNull(AnsiConsole.Ask(L("Description (optional)"), ""));

        AnsiConsole.WriteLine();
        AnsiConsole.MarkupLine($"[grey]{ML("BoardID")}:[/] {Markup.Escape(boardId)}");
        AnsiConsole.MarkupLine($"[grey]{ML("Title")}:[/] {Markup.Escape(title)}");
        if (description != null)
        {
            AnsiConsole.MarkupLine($"[grey]{ML("Description")}:[/] {Markup.Escape(description)}");
        }
        AnsiConsole.MarkupLine($"[grey]{ML("Author key")}:[/] {Markup.Escape(key.Name)}  [grey]{Markup.Escape(Short(key.Pub, 32))}[/]");
        AnsiConsole.WriteLine();

        if (!AnsiConsole.Confirm(L("Create this board and register it locally (boards.json)?"), true))
        {
            return;
        }

        var args = new List<string>
        {
            "init-board",
            "--board-id", boardId,
            "--title", title,
            "--author-priv-key", key.Priv,
            "--flexipfs-base-url", cfg.FlexIpfsBaseUrl,
            $"--autostart-flexipfs={cfg.AutostartFlexIpfs.ToString().ToLowerInvariant()}",
            $"--flexipfs-mdns={cfg.FlexIpfsMdns.ToString().ToLowerInvariant()}",
            "--flexipfs-mdns-timeout", $"{cfg.FlexIpfsMdnsTimeoutSeconds}s",
            "--data-dir", cfg.DataDir,
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
        if (!string.IsNullOrWhiteSpace(description))
        {
            args.Add("--description");
            args.Add(description);
        }

        try
        {
            var output = await AnsiConsole.Status()
                .Spinner(Spinner.Known.Dots)
                .StartAsync(L("Creating board..."), async _ => await RunProcessCaptureAsync(bbsNodePath, args, ct));
            var cid = TryExtractCid(output.StdOut) ?? "";
            if (cid == "")
            {
                AnsiConsole.MarkupLine("[green]ok[/]");
            }
            else
            {
                AnsiConsole.MarkupLine($"[green]ok[/] boardMetaCid={Markup.Escape(cid)}");
            }
        }
        catch (Exception ex)
        {
            ShowError(ex);
        }

        Pause();
    }

    private static async Task AddBoardFlowAsync(ClientConfig cfg, CancellationToken ct)
    {
        var bbsNodePath = cfg.BbsNodePath ?? BbsNodePathResolver.Resolve();
        if (string.IsNullOrWhiteSpace(bbsNodePath))
        {
            AnsiConsole.MarkupLine($"[red]{ML("bbs-node not found.")}[/] {ML("Set it in Settings → Client / Backend.")}");
            Pause();
            return;
        }

        var boardMetaCid = AnsiConsole.Ask<string>(L("BoardMeta CID"));
        if (string.IsNullOrWhiteSpace(boardMetaCid))
        {
            AnsiConsole.MarkupLine($"[yellow]{ML("BoardMeta CID is empty. Canceled.")}[/]");
            Pause();
            return;
        }
        boardMetaCid = boardMetaCid.Trim();

        if (!AnsiConsole.Confirm(L("Register this board locally (boards.json)?"), true))
        {
            return;
        }

        var args = new List<string>
        {
            "add-board",
            "--board-meta-cid", boardMetaCid,
            "--flexipfs-base-url", cfg.FlexIpfsBaseUrl,
            $"--autostart-flexipfs={cfg.AutostartFlexIpfs.ToString().ToLowerInvariant()}",
            $"--flexipfs-mdns={cfg.FlexIpfsMdns.ToString().ToLowerInvariant()}",
            "--flexipfs-mdns-timeout", $"{cfg.FlexIpfsMdnsTimeoutSeconds}s",
            "--data-dir", cfg.DataDir,
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

        try
        {
            var output = await AnsiConsole.Status()
                .Spinner(Spinner.Known.Dots)
                .StartAsync(L("Registering board..."), async _ => await RunProcessCaptureAsync(bbsNodePath, args, ct));
            var msg = output.StdOut.Trim();
            AnsiConsole.MarkupLine(msg == "" ? "[green]ok[/]" : $"[green]{Markup.Escape(msg)}[/]");
        }
        catch (Exception ex)
        {
            ShowError(ex);
        }
        Pause();
    }

    private static async Task BrowseThreadsAsync(
        BbsApiClient api,
        KeyStore keys,
        BlockedStore blocked,
        string boardId,
        string boardTitle,
        CancellationToken ct
    )
    {
        var offset = 0;

        while (true)
        {
            AnsiConsole.Clear();
            AnsiConsole.Write(new Rule($"[bold]{ML("Threads")}[/] [grey]{Markup.Escape(boardId)}[/]").LeftJustified());
            if (!string.IsNullOrWhiteSpace(boardTitle))
            {
                AnsiConsole.MarkupLine($"[grey]{Markup.Escape(boardTitle)}[/]");
            }
            AnsiConsole.WriteLine();

            List<ThreadItem> threads;
            try
            {
                threads = await api.GetThreadsAsync(boardId, DefaultPageSize, offset, ct);
            }
            catch (Exception ex)
            {
                ShowError(ex);
                Pause();
                return;
            }

            var table = new Table().Border(TableBorder.Rounded);
            table.Expand = true;
            table.AddColumn("#");
            table.AddColumn(L("ThreadID"));
            table.AddColumn(L("Title"));
            table.AddColumn(L("CreatedAt"));
            for (var i = 0; i < threads.Count; i++)
            {
                var t = threads[i];
            table.AddRow(
                    (offset + i + 1).ToString(),
                    Markup.Escape(t.ThreadId),
                    Markup.Escape(t.Thread.Title),
                    Markup.Escape(FormatTimestamp(t.Thread.CreatedAt))
                );
            }
            AnsiConsole.Write(table);
            AnsiConsole.WriteLine();

            var actions = new List<string>
            {
                "Open thread",
                "Create thread",
                "Refresh",
            };
            if (offset > 0)
            {
                actions.Add("Prev page");
            }
            if (threads.Count == DefaultPageSize)
            {
                actions.Add("Next page");
            }
            actions.Add("Back");

            var action = AnsiConsole.Prompt(
                new SelectionPrompt<string>()
                    .Title(L("Action"))
                    .AddChoices(actions)
                    .UseConverter(L)
            );

            switch (action)
            {
                case "Open thread":
                {
                    if (threads.Count == 0)
                    {
                        AnsiConsole.MarkupLine($"[grey]{ML("No threads in this page.")}[/]");
                        Pause();
                        break;
                    }

                    var thread = PromptWithBack(
                        L("Select thread"),
                        threads,
                        t => $"{Markup.Escape(t.Thread.Title)}  [grey]{Markup.Escape(Short(t.ThreadId, 24))}[/]",
                        moreChoicesText: $"[grey]{ML("(move up and down to reveal more threads)")}[/]"
                    );
                    if (thread == null)
                    {
                        break;
                    }

                    await BrowseThreadAsync(api, keys, blocked, thread.ThreadId, ct);
                    break;
                }
                case "Create thread":
                {
                    await CreateThreadFlowAsync(api, keys, boardId, ct);
                    offset = 0;
                    break;
                }
                case "Refresh":
                    break;
                case "Prev page":
                    offset = Math.Max(0, offset - DefaultPageSize);
                    break;
                case "Next page":
                    offset += DefaultPageSize;
                    break;
                case "Back":
                    return;
            }
        }
    }

    private static async Task BrowseThreadAsync(BbsApiClient api, KeyStore keys, BlockedStore blocked, string threadId, CancellationToken ct)
    {
        while (true)
        {
            ThreadResponse tr;
            HashSet<string> blockedKeys;
            try
            {
                blockedKeys = await blocked.LoadAsync(ct);
                tr = await api.GetThreadAsync(threadId, ct);
            }
            catch (Exception ex)
            {
                ShowError(ex);
                Pause();
                return;
            }

            AnsiConsole.Clear();
            AnsiConsole.Write(new Rule($"[bold]{ML("Thread")}[/]").LeftJustified());
            AnsiConsole.MarkupLine($"[grey]{ML("Board")}:[/] {Markup.Escape(tr.ThreadMeta.BoardId)}");
            AnsiConsole.MarkupLine($"[grey]{ML("Title")}:[/] {Markup.Escape(tr.ThreadMeta.Title)}");
            AnsiConsole.MarkupLine($"[grey]{ML("ThreadID")}:[/] {Markup.Escape(tr.ThreadMeta.ThreadId)}");
            AnsiConsole.WriteLine();

            var visiblePosts = tr.Posts.Where(p => !blockedKeys.Contains(p.Post.AuthorPubKey)).ToList();
            var hiddenCount = tr.Posts.Count - visiblePosts.Count;
            if (hiddenCount > 0)
            {
                AnsiConsole.MarkupLine($"[grey]{Markup.Escape(F("{0} post(s) hidden (blocked authors).", hiddenCount))}[/]");
                AnsiConsole.WriteLine();
            }

            for (var i = 0; i < visiblePosts.Count; i++)
            {
                var p = visiblePosts[i];

                var meta = $"[bold]#{i + 1}[/] {Markup.Escape(p.Post.DisplayName)} [grey]{Markup.Escape(FormatTimestamp(p.Post.CreatedAt))}[/]";
                if (!string.IsNullOrWhiteSpace(p.Post.EditedAt))
                {
                    meta += $" [grey]({ML("edited")} {Markup.Escape(FormatTimestamp(p.Post.EditedAt))})[/]";
                }

                var cidLine = KeyValueMarkupAuto(L("CID"), p.Cid);
                var authorLine = KeyValueMarkupAuto(L("Author"), p.Post.AuthorPubKey);
                var parentLine = string.IsNullOrWhiteSpace(p.Post.ParentPostCid)
                    ? null
                    : KeyValueMarkupAuto(L("Parent"), p.Post.ParentPostCid);

                var body = p.Tombstoned
                    ? $"[red]{Markup.Escape($"[{L("tombstoned")}]")}[/]\n{Markup.Escape(p.TombstoneReason ?? "")}".TrimEnd()
                    : Markup.Escape(p.Post.Body.Content);

                var lines = new List<string> { meta, cidLine, authorLine };
                if (parentLine != null)
                {
                    lines.Add(parentLine);
                }
                lines.Add("");
                lines.Add(body);

                var panel = new Panel(new Markup(string.Join("\n", lines)))
                    .BorderColor(Color.Grey)
                    .Border(BoxBorder.Rounded);
                panel.Expand = true;

                AnsiConsole.Write(panel);
            }

            AnsiConsole.WriteLine();

            var actions = new List<string>
            {
                "Reply",
                "Edit post",
                "Tombstone post",
                "Block author",
                "Refresh",
                "Back",
            };

            var action = AnsiConsole.Prompt(new SelectionPrompt<string>().Title(L("Action")).AddChoices(actions).UseConverter(L));
            switch (action)
            {
                case "Reply":
                    await ReplyFlowAsync(api, keys, threadId, visiblePosts, ct);
                    break;
                case "Edit post":
                    await EditPostFlowAsync(api, keys, visiblePosts, ct);
                    break;
                case "Tombstone post":
                    await TombstonePostFlowAsync(api, keys, visiblePosts, ct);
                    break;
                case "Block author":
                    await BlockAuthorFlowAsync(blocked, visiblePosts, ct);
                    break;
                case "Refresh":
                    break;
                case "Back":
                    return;
            }
        }
    }

    private static async Task ReplyFlowAsync(
        BbsApiClient api,
        KeyStore keys,
        string threadId,
        List<ThreadPostItem> visiblePosts,
        CancellationToken ct
    )
    {
        var key = await PromptKeyAsync(keys, ct);
        if (key == null)
        {
            return;
        }

        var displayName = AnsiConsole.Ask(L("Display name (optional)"), "");

        string? parent = null;
        if (visiblePosts.Count > 0 && AnsiConsole.Confirm(L("Reply to a specific post?"), false))
        {
            var selected = PromptWithBack(
                L("Select parent post"),
                visiblePosts,
                p => $"{Markup.Escape(p.Post.DisplayName)}  [grey]{Markup.Escape(Short(p.Cid, 24))}[/]",
                moreChoicesText: $"[grey]{ML("(move up and down to reveal more posts)")}[/]"
            );
            if (selected == null)
            {
                return;
            }
            parent = selected.Cid;
        }

        var body = ReadMultiline(L("Body"));
        if (string.IsNullOrWhiteSpace(body))
        {
            AnsiConsole.MarkupLine($"[yellow]{ML("Body is empty. Canceled.")}[/]");
            Pause();
            return;
        }

        try
        {
            var resp = await api.AddPostAsync(new AddPostRequest
            {
                ThreadId = threadId,
                ParentPostCid = parent,
                DisplayName = displayName,
                Body = new PostBody { Format = "markdown", Content = body },
                AuthorPrivKey = key.Priv,
            }, ct);

            AnsiConsole.MarkupLine($"[green]ok[/] postCid={Markup.Escape(resp.PostCid)}");
            Pause();
        }
        catch (Exception ex)
        {
            ShowError(ex);
            Pause();
        }
    }

    private static async Task CreateThreadFlowAsync(BbsApiClient api, KeyStore keys, string boardId, CancellationToken ct)
    {
        var key = await PromptKeyAsync(keys, ct);
        if (key == null)
        {
            return;
        }

        var title = AnsiConsole.Ask<string>(L("Title"));
        if (string.IsNullOrWhiteSpace(title))
        {
            AnsiConsole.MarkupLine($"[yellow]{ML("Title is empty. Canceled.")}[/]");
            Pause();
            return;
        }

        var displayName = AnsiConsole.Ask(L("Display name (optional)"), "");

        var body = ReadMultiline(L("Body"));
        if (string.IsNullOrWhiteSpace(body))
        {
            AnsiConsole.MarkupLine($"[yellow]{ML("Body is empty. Canceled.")}[/]");
            Pause();
            return;
        }

        try
        {
            var resp = await api.CreateThreadAsync(new CreateThreadRequest
            {
                BoardId = boardId,
                Title = title,
                DisplayName = displayName,
                Body = new PostBody { Format = "markdown", Content = body },
                AuthorPrivKey = key.Priv,
            }, ct);

            AnsiConsole.MarkupLine($"[green]ok[/] threadId={Markup.Escape(resp.ThreadId)}");
            Pause();
        }
        catch (Exception ex)
        {
            ShowError(ex);
            Pause();
        }
    }

    private static async Task EditPostFlowAsync(BbsApiClient api, KeyStore keys, List<ThreadPostItem> visiblePosts, CancellationToken ct)
    {
        if (visiblePosts.Count == 0)
        {
            AnsiConsole.MarkupLine($"[grey]{ML("No posts to edit.")}[/]");
            Pause();
            return;
        }

        var key = await PromptKeyAsync(keys, ct);
        if (key == null)
        {
            return;
        }

        var selected = PromptWithBack(
            L("Select post to edit"),
            visiblePosts,
            p => $"{Markup.Escape(p.Post.DisplayName)}  [grey]{Markup.Escape(Short(p.Cid, 24))}[/]",
            moreChoicesText: $"[grey]{ML("(move up and down to reveal more posts)")}[/]"
        );
        if (selected == null)
        {
            return;
        }

        var displayName = EmptyToNull(AnsiConsole.Ask(L("Display name (optional, blank = keep)"), ""));

        var body = ReadMultiline(L("Body"));
        if (string.IsNullOrWhiteSpace(body))
        {
            AnsiConsole.MarkupLine($"[yellow]{ML("Body is empty. Canceled.")}[/]");
            Pause();
            return;
        }

        try
        {
            var resp = await api.EditPostAsync(selected.Cid, new EditPostRequest
            {
                Body = new PostBody { Format = "markdown", Content = body },
                DisplayName = displayName,
                AuthorPrivKey = key.Priv,
            }, ct);

            AnsiConsole.MarkupLine($"[green]ok[/] newPostCid={Markup.Escape(resp.NewPostCid)}");
            Pause();
        }
        catch (Exception ex)
        {
            ShowError(ex);
            Pause();
        }
    }

    private static async Task TombstonePostFlowAsync(BbsApiClient api, KeyStore keys, List<ThreadPostItem> visiblePosts, CancellationToken ct)
    {
        if (visiblePosts.Count == 0)
        {
            AnsiConsole.MarkupLine($"[grey]{ML("No posts to tombstone.")}[/]");
            Pause();
            return;
        }

        var key = await PromptKeyAsync(keys, ct);
        if (key == null)
        {
            return;
        }

        var selected = PromptWithBack(
            L("Select post to tombstone"),
            visiblePosts,
            p => $"{Markup.Escape(p.Post.DisplayName)}  [grey]{Markup.Escape(Short(p.Cid, 24))}[/]",
            moreChoicesText: $"[grey]{ML("(move up and down to reveal more posts)")}[/]"
        );
        if (selected == null)
        {
            return;
        }

        var reason = EmptyToNull(AnsiConsole.Ask(L("Reason (optional)"), ""));

        try
        {
            var resp = await api.TombstonePostAsync(selected.Cid, new TombstonePostRequest
            {
                Reason = reason,
                AuthorPrivKey = key.Priv,
            }, ct);

            AnsiConsole.MarkupLine($"[green]ok[/] tombstoned {Markup.Escape(resp.TargetPostCid)}");
            Pause();
        }
        catch (Exception ex)
        {
            ShowError(ex);
            Pause();
        }
    }

    private static async Task SearchAsync(BbsApiClient api, KeyStore keys, BlockedStore blocked, CancellationToken ct)
    {
        var q = AnsiConsole.Ask<string>(L("Query (q)"));
        var boardId = EmptyToNull(AnsiConsole.Ask(L("Board ID (optional)"), ""));
        var author = EmptyToNull(AnsiConsole.Ask(L("Author pubKey (optional)"), ""));
        var since = EmptyToNull(AnsiConsole.Ask(L("Since (optional, RFC3339)"), ""));
        var until = EmptyToNull(AnsiConsole.Ask(L("Until (optional, RFC3339)"), ""));

        const int resultsLimit = 10;
        var postsOffset = 0;
        while (true)
        {
            List<BoardItem> boards;
            List<ThreadItem> threads;
            List<SearchPostResult> results;
            try
            {
                boards = await api.SearchBoardsAsync(q, resultsLimit, 0, ct);
                threads = await api.SearchThreadsAsync(q, boardId, resultsLimit, 0, ct);
                results = await api.SearchPostsAsync(q, boardId, author, since, until, DefaultPageSize, postsOffset, ct);
            }
            catch (Exception ex)
            {
                ShowError(ex);
                Pause();
                return;
            }

            AnsiConsole.Clear();
            AnsiConsole.Write(new Rule($"[bold]{ML("Search")}[/]").LeftJustified());
            AnsiConsole.MarkupLine($"[grey]q:[/] {Markup.Escape(q)}");
            if (boardId != null) AnsiConsole.MarkupLine($"[grey]boardId:[/] {Markup.Escape(boardId)}");
            if (author != null) AnsiConsole.MarkupLine($"[grey]author:[/] {Markup.Escape(author)}");
            AnsiConsole.WriteLine();

            AnsiConsole.MarkupLine($"[bold]{ML("Boards")}[/]");
            var boardsTable = new Table().Border(TableBorder.Rounded);
            boardsTable.Expand = true;
            boardsTable.AddColumn("#");
            boardsTable.AddColumn(L("BoardID"));
            boardsTable.AddColumn(L("Title"));
            boardsTable.AddColumn(L("MetaCID"));
            for (var i = 0; i < boards.Count; i++)
            {
                var b = boards[i];
                boardsTable.AddRow(
                    (i + 1).ToString(),
                    Markup.Escape(b.Board.BoardId),
                    Markup.Escape(b.Board.Title),
                    Markup.Escape(b.BoardMetaCid)
                );
            }
            AnsiConsole.Write(boardsTable);
            AnsiConsole.WriteLine();

            AnsiConsole.MarkupLine($"[bold]{ML("Threads")}[/]");
            var threadsTable = new Table().Border(TableBorder.Rounded);
            threadsTable.Expand = true;
            threadsTable.AddColumn("#");
            threadsTable.AddColumn(L("Board"));
            threadsTable.AddColumn(L("Title"));
            threadsTable.AddColumn(L("ThreadID"));
            threadsTable.AddColumn(L("CreatedAt"));
            for (var i = 0; i < threads.Count; i++)
            {
                var t = threads[i];
                threadsTable.AddRow(
                    (i + 1).ToString(),
                    Markup.Escape(t.Thread.BoardId),
                    Markup.Escape(t.Thread.Title),
                    Markup.Escape(t.ThreadId),
                    Markup.Escape(FormatTimestamp(t.Thread.CreatedAt))
                );
            }
            AnsiConsole.Write(threadsTable);
            AnsiConsole.WriteLine();

            AnsiConsole.MarkupLine($"[bold]{ML("Post")}[/]");
            var table = new Table().Border(TableBorder.Rounded);
            table.Expand = true;
            table.AddColumn("#");
            table.AddColumn(L("Board"));
            table.AddColumn(L("Thread"));
            table.AddColumn(L("Post"));
            table.AddColumn(L("Name"));
            table.AddColumn(L("CreatedAt"));
            for (var i = 0; i < results.Count; i++)
            {
                var r = results[i];
            table.AddRow(
                    (postsOffset + i + 1).ToString(),
                    Markup.Escape(r.BoardId),
                    Markup.Escape(r.ThreadId),
                    Markup.Escape(r.PostCid),
                    Markup.Escape(r.DisplayName),
                    Markup.Escape(FormatTimestamp(r.CreatedAt))
                );
            }
            AnsiConsole.Write(table);
            AnsiConsole.WriteLine();

            var actions = new List<string>
            {
                "Open board",
                "Open thread",
                "Open post",
                "Block author",
                "New search",
            };
            if (postsOffset > 0)
            {
                actions.Add("Prev page");
            }
            if (results.Count == DefaultPageSize)
            {
                actions.Add("Next page");
            }
            actions.Add("Back");

            var action = AnsiConsole.Prompt(new SelectionPrompt<string>().Title(L("Action")).AddChoices(actions).UseConverter(L));
            switch (action)
            {
                case "Open board":
                {
                    if (boards.Count == 0)
                    {
                        AnsiConsole.MarkupLine($"[grey]{ML("No results in this page.")}[/]");
                        Pause();
                        break;
                    }

                    var selected = PromptWithBack(
                        L("Select board"),
                        boards,
                        b => $"{Markup.Escape(b.Board.BoardId)}  {Markup.Escape(b.Board.Title)}  [grey]{Markup.Escape(b.BoardMetaCid)}[/]",
                        moreChoicesText: $"[grey]{ML("(move up and down to reveal more results)")}[/]"
                    );
                    if (selected == null)
                    {
                        break;
                    }

                    await BrowseThreadsAsync(api, keys, blocked, selected.Board.BoardId, selected.Board.Title, ct);
                    break;
                }
                case "Open thread":
                {
                    if (threads.Count == 0)
                    {
                        AnsiConsole.MarkupLine($"[grey]{ML("No results in this page.")}[/]");
                        Pause();
                        break;
                    }

                    var selected = PromptWithBack(
                        L("Select thread"),
                        threads,
                        t => $"{Markup.Escape(t.Thread.Title)}  [grey]{Markup.Escape(t.Thread.BoardId)} {Markup.Escape(t.ThreadId)}[/]",
                        moreChoicesText: $"[grey]{ML("(move up and down to reveal more results)")}[/]"
                    );
                    if (selected == null)
                    {
                        break;
                    }

                    await BrowseThreadAsync(api, keys, blocked, selected.ThreadId, ct);
                    break;
                }
                case "Open post":
                {
                    if (results.Count == 0)
                    {
                        AnsiConsole.MarkupLine($"[grey]{ML("No results in this page.")}[/]");
                        Pause();
                        break;
                    }

                    var selected = PromptWithBack(
                        L("Select result"),
                        results,
                        r => $"{Markup.Escape(r.DisplayName)}  [grey]{Markup.Escape(r.BoardId)} {Markup.Escape(r.ThreadId)} {Markup.Escape(r.PostCid)}[/]",
                        moreChoicesText: $"[grey]{ML("(move up and down to reveal more results)")}[/]"
                    );
                    if (selected == null)
                    {
                        break;
                    }

                    await BrowseThreadAsync(api, keys, blocked, selected.ThreadId, ct);
                    break;
                }
                case "Block author":
                {
                    if (results.Count == 0)
                    {
                        AnsiConsole.MarkupLine($"[grey]{ML("No results in this page.")}[/]");
                        Pause();
                        break;
                    }

                    var selected = PromptWithBack(
                        L("Select author to block"),
                        results,
                        r => $"{Markup.Escape(r.DisplayName)}  [grey]{Markup.Escape(Short(r.AuthorPubKey, 24))}[/]",
                        moreChoicesText: $"[grey]{ML("(move up and down to reveal more results)")}[/]"
                    );
                    if (selected == null)
                    {
                        break;
                    }

                    await blocked.AddAsync(selected.AuthorPubKey, ct);
                    AnsiConsole.MarkupLine($"[green]ok[/] blocked {Markup.Escape(Short(selected.AuthorPubKey, 24))}");
                    Pause();
                    break;
                }
                case "New search":
                    return;
                case "Prev page":
                    postsOffset = Math.Max(0, postsOffset - DefaultPageSize);
                    break;
                case "Next page":
                    postsOffset += DefaultPageSize;
                    break;
                case "Back":
                    return;
            }
        }
    }

    private static async Task KeysMenuAsync(KeyStore keys, CancellationToken ct)
    {
        while (true)
        {
            var list = await keys.LoadAsync(ct);
            list = list.OrderBy(k => k.Name, StringComparer.OrdinalIgnoreCase).ToList();

            AnsiConsole.Clear();
            AnsiConsole.Write(new Rule($"[bold]{ML("Keys")}[/]").LeftJustified());

            var table = new Table().Border(TableBorder.Rounded);
            table.Expand = true;
            table.AddColumn(L("Name"));
            table.AddColumn(L("Public key"));
            table.AddColumn(L("Password"));
            foreach (var k in list)
            {
                table.AddRow(
                    Markup.Escape(k.Name),
                    Markup.Escape(k.Pub),
                    k.IsProtected ? $"[green]{ML("protected")}[/]" : $"[grey]{ML("none")}[/]"
                );
            }
            AnsiConsole.Write(table);
            AnsiConsole.WriteLine();

            var action = AnsiConsole.Prompt(
                new SelectionPrompt<string>()
                    .Title(L("Action"))
                    .AddChoices("Generate", "Set password", "Remove password", "Delete", "Back")
                    .UseConverter(L)
            );

            switch (action)
            {
                case "Generate":
                {
                    var name = AnsiConsole.Ask(L("Key name"), "default");
                    string? password = null;
                    if (AnsiConsole.Confirm(L("Set a password now?"), false))
                    {
                        var p1 = AnsiConsole.Prompt(new TextPrompt<string>(L("New password")).Secret());
                        if (string.IsNullOrWhiteSpace(p1))
                        {
                            break;
                        }
                        var p2 = AnsiConsole.Prompt(new TextPrompt<string>(L("Confirm password")).Secret());
                        if (p1 != p2)
                        {
                            ShowError(new InvalidOperationException(L("Passwords do not match.")));
                            Pause();
                            break;
                        }
                        password = p1;
                    }

                    try
                    {
                        var created = await keys.GenerateAsync(name, password, ct);
                        AnsiConsole.MarkupLine($"[green]ok[/] {Markup.Escape(created.Name)} {Markup.Escape(created.Pub)}");
                    }
                    catch (Exception ex)
                    {
                        ShowError(ex);
                    }
                    Pause();
                    break;
                }
                case "Set password":
                {
                    if (list.Count == 0)
                    {
                        AnsiConsole.MarkupLine($"[grey]{ML("No keys found.")}[/]");
                        Pause();
                        break;
                    }

                    var selected = PromptWithBack(
                        L("Select key"),
                        list,
                        k => k.IsProtected
                            ? $"{Markup.Escape(k.Name)}  [green]{ML("protected")}[/]"
                            : $"{Markup.Escape(k.Name)}  [grey]{ML("none")}[/]"
                    );
                    if (selected == null)
                    {
                        break;
                    }

                    try
                    {
                        if (selected.IsProtected)
                        {
                            var current = AnsiConsole.Prompt(new TextPrompt<string>(L("Current password")).Secret());
                            var next = AnsiConsole.Prompt(new TextPrompt<string>(L("New password")).Secret());
                            if (string.IsNullOrWhiteSpace(next))
                            {
                                break;
                            }
                            var confirm = AnsiConsole.Prompt(new TextPrompt<string>(L("Confirm password")).Secret());
                            if (next != confirm)
                            {
                                throw new InvalidOperationException(L("Passwords do not match."));
                            }
                            await keys.ChangePasswordAsync(selected.Name, current, next, ct);
                            AnsiConsole.MarkupLine("[green]ok[/]");
                        }
                        else
                        {
                            var next = AnsiConsole.Prompt(new TextPrompt<string>(L("New password")).Secret());
                            if (string.IsNullOrWhiteSpace(next))
                            {
                                break;
                            }
                            var confirm = AnsiConsole.Prompt(new TextPrompt<string>(L("Confirm password")).Secret());
                            if (next != confirm)
                            {
                                throw new InvalidOperationException(L("Passwords do not match."));
                            }
                            await keys.ProtectAsync(selected.Name, next, ct);
                            AnsiConsole.MarkupLine("[green]ok[/]");
                        }
                    }
                    catch (Exception ex)
                    {
                        ShowError(ex);
                    }

                    Pause();
                    break;
                }
                case "Remove password":
                {
                    var protectedKeys = list.Where(k => k.IsProtected).ToList();
                    if (protectedKeys.Count == 0)
                    {
                        AnsiConsole.MarkupLine($"[grey]{ML("No password-protected keys.")}[/]");
                        Pause();
                        break;
                    }

                    var selected = PromptWithBack(
                        L("Select key"),
                        protectedKeys,
                        k => $"{Markup.Escape(k.Name)}  [green]{ML("protected")}[/]"
                    );
                    if (selected == null)
                    {
                        break;
                    }

                    try
                    {
                        var password = AnsiConsole.Prompt(new TextPrompt<string>(L("Password")).Secret());
                        if (string.IsNullOrWhiteSpace(password))
                        {
                            break;
                        }
                        await keys.UnprotectAsync(selected.Name, password, ct);
                        AnsiConsole.MarkupLine("[green]ok[/]");
                    }
                    catch (Exception ex)
                    {
                        ShowError(ex);
                    }

                    Pause();
                    break;
                }
                case "Delete":
                {
                    if (list.Count == 0)
                    {
                        AnsiConsole.MarkupLine($"[grey]{ML("No keys to delete.")}[/]");
                        Pause();
                        break;
                    }
                    var selected = PromptWithBack(
                        L("Select key to delete"),
                        list,
                        k => $"{Markup.Escape(k.Name)}  [grey]{Markup.Escape(Short(k.Pub, 24))}[/]"
                    );
                    if (selected == null)
                    {
                        break;
                    }
                    if (!AnsiConsole.Confirm(F("Delete '{0}'?", selected.Name), false))
                    {
                        break;
                    }
                    try
                    {
                        await keys.DeleteAsync(selected.Name, ct);
                        AnsiConsole.MarkupLine("[green]ok[/]");
                    }
                    catch (Exception ex)
                    {
                        ShowError(ex);
                    }
                    Pause();
                    break;
                }
                case "Back":
                    return;
            }
        }
    }

    private static async Task BlockedMenuAsync(BlockedStore blocked, CancellationToken ct)
    {
        while (true)
        {
            var set = await blocked.LoadAsync(ct);
            var list = set.Order(StringComparer.Ordinal).ToList();

            AnsiConsole.Clear();
            AnsiConsole.Write(new Rule($"[bold]{ML("Blocked authors")}[/]").LeftJustified());

            var table = new Table().Border(TableBorder.Rounded);
            table.Expand = true;
            table.AddColumn(L("Public key"));
            foreach (var k in list)
            {
                table.AddRow(Markup.Escape(k));
            }
            AnsiConsole.Write(table);
            AnsiConsole.WriteLine();

            var action = AnsiConsole.Prompt(
                new SelectionPrompt<string>()
                    .Title(L("Action"))
                    .AddChoices("Add", "Remove", "Back")
                    .UseConverter(L)
            );

            switch (action)
            {
                case "Add":
                {
                    var pub = AnsiConsole.Ask<string>(L("Public key to block"));
                    if (string.IsNullOrWhiteSpace(pub))
                    {
                        break;
                    }
                    await blocked.AddAsync(pub.Trim(), ct);
                    AnsiConsole.MarkupLine("[green]ok[/]");
                    Pause();
                    break;
                }
                case "Remove":
                {
                    if (list.Count == 0)
                    {
                        AnsiConsole.MarkupLine($"[grey]{ML("No blocked authors to remove.")}[/]");
                        Pause();
                        break;
                    }
                    var selected = PromptWithBack(
                        L("Select key to remove"),
                        list,
                        Markup.Escape
                    );
                    if (selected == null)
                    {
                        break;
                    }
                    await blocked.RemoveAsync(selected, ct);
                    AnsiConsole.MarkupLine("[green]ok[/]");
                    Pause();
                    break;
                }
                case "Back":
                    return;
            }
        }
    }

    private static async Task BlockAuthorFlowAsync(BlockedStore blocked, List<ThreadPostItem> visiblePosts, CancellationToken ct)
    {
        if (visiblePosts.Count == 0)
        {
            AnsiConsole.MarkupLine($"[grey]{ML("No posts to select.")}[/]");
            Pause();
            return;
        }

        var selected = PromptWithBack(
            L("Select post"),
            visiblePosts,
            p => $"{Markup.Escape(p.Post.DisplayName)}  [grey]{Markup.Escape(Short(p.Post.AuthorPubKey, 24))}[/]",
            moreChoicesText: $"[grey]{ML("(move up and down to reveal more posts)")}[/]"
        );
        if (selected == null)
        {
            return;
        }

        await blocked.AddAsync(selected.Post.AuthorPubKey, ct);
        AnsiConsole.MarkupLine($"[green]ok[/] blocked {Markup.Escape(Short(selected.Post.AuthorPubKey, 24))}");
        Pause();
    }

    private static async Task<KeyEntry?> PromptKeyAsync(KeyStore keys, CancellationToken ct)
    {
        var list = await keys.LoadAsync(ct);
        list = list.OrderBy(k => k.Name, StringComparer.OrdinalIgnoreCase).ToList();

        if (list.Count == 0)
        {
            AnsiConsole.MarkupLine($"[yellow]{ML("No keys found.")}[/]");
            if (!AnsiConsole.Confirm(L("Generate a key now?"), true))
            {
                return null;
            }
            var name = AnsiConsole.Ask(L("Key name"), "default");
            try
            {
                string? password = null;
                if (AnsiConsole.Confirm(L("Set a password now?"), false))
                {
                    var p1 = AnsiConsole.Prompt(new TextPrompt<string>(L("New password")).Secret());
                    if (string.IsNullOrWhiteSpace(p1))
                    {
                        return null;
                    }
                    var p2 = AnsiConsole.Prompt(new TextPrompt<string>(L("Confirm password")).Secret());
                    if (p1 != p2)
                    {
                        throw new InvalidOperationException(L("Passwords do not match."));
                    }
                    password = p1;
                }

                return await keys.GenerateAsync(name, password, ct);
            }
            catch (Exception ex)
            {
                ShowError(ex);
                Pause();
                return null;
            }
        }

        var selected = PromptWithBack(
            L("Select key"),
            list,
            k => k.IsProtected
                ? $"{Markup.Escape(k.Name)}  [green]{ML("protected")}[/] [grey]{Markup.Escape(Short(k.Pub, 32))}[/]"
                : $"{Markup.Escape(k.Name)}  [grey]{Markup.Escape(Short(k.Pub, 32))}[/]",
            moreChoicesText: $"[grey]{ML("(move up and down to reveal more keys)")}[/]"
        );
        if (selected == null)
        {
            return null;
        }

        if (!selected.IsProtected)
        {
            return selected;
        }

        for (var attempt = 1; attempt <= 3; attempt++)
        {
            var pw = AnsiConsole.Prompt(new TextPrompt<string>(L("Password")).Secret());
            if (string.IsNullOrWhiteSpace(pw))
            {
                return null;
            }
            try
            {
                var priv = KeyStore.DecryptPriv(selected, pw);
                return selected with { Priv = priv };
            }
            catch (Exception ex)
            {
                if (attempt >= 3)
                {
                    ShowError(ex);
                    Pause();
                    return null;
                }
                AnsiConsole.MarkupLine($"[red]{ML("Error")}:[/] {Markup.Escape(ex.Message)}");
            }
        }
        return null;
    }

    private static async Task<ClientConfig> SettingsMenuAsync(
        ClientConfigStore configStore,
        ClientConfig cfg,
        BackendLauncher launcher,
        CancellationToken ct
    )
    {
        cfg = cfg.Normalize();

        while (true)
        {
            var healthy = await BackendLauncher.IsHealthyAsync(cfg.BackendBaseUrl, ct);

            AnsiConsole.Clear();
            AnsiConsole.Write(new Rule($"[bold]{ML("Settings")}[/]").LeftJustified());
            AnsiConsole.MarkupLine($"[grey]{ML("Config")}:[/] {Markup.Escape(configStore.ConfigPath)}");
            AnsiConsole.MarkupLine($"[grey]{ML("Backend")}:[/] {Markup.Escape(cfg.BackendBaseUrl)} [{(healthy ? "green" : "red")}]{ML(healthy ? "up" : "down")}[/]");
            AnsiConsole.MarkupLine($"[grey]{ML("Backend listen (managed)")}:[/] {Markup.Escape(BbsNodeArgsBuilder.ResolveListenHostPort(cfg))}");
            AnsiConsole.MarkupLine($"[grey]{ML("Backend role (managed)")}:[/] {Markup.Escape(cfg.BackendRole)}");
            AnsiConsole.MarkupLine($"[grey]{ML("UI language")}:[/] {Markup.Escape(L(cfg.UiLanguage))}");
            AnsiConsole.MarkupLine($"[grey]{ML("Time zone")}:[/] {Markup.Escape(cfg.UiTimeZone.ToUpperInvariant())}");
            AnsiConsole.MarkupLine($"[grey]{ML("Auto-start backend")}:[/] {cfg.StartBackend}");
            AnsiConsole.MarkupLine($"[grey]{ML("bbs-node path")}:[/] {Markup.Escape(cfg.BbsNodePath ?? L("<auto>"))}");
            AnsiConsole.MarkupLine($"[grey]{ML("Data dir")}:[/] {Markup.Escape(cfg.DataDir)}");
            AnsiConsole.MarkupLine($"[grey]{ML("Flex-IPFS base URL")}:[/] {Markup.Escape(cfg.FlexIpfsBaseUrl)}");
            AnsiConsole.MarkupLine($"[grey]{ML("Flex-IPFS base dir")}:[/] {Markup.Escape(cfg.FlexIpfsBaseDir ?? L("<auto>"))}");
            AnsiConsole.MarkupLine($"[grey]{ML("Flex-IPFS GW endpoint override")}:[/] {Markup.Escape(cfg.FlexIpfsGwEndpoint ?? L("<none>"))}");
            AnsiConsole.MarkupLine($"[grey]{ML("Flex-IPFS mDNS")}:[/] {cfg.FlexIpfsMdns}");
            AnsiConsole.MarkupLine($"[grey]{ML("Flex-IPFS mDNS timeout")}:[/] {cfg.FlexIpfsMdnsTimeoutSeconds}s");
            AnsiConsole.MarkupLine($"[grey]{ML("Autostart flex-ipfs")}:[/] {cfg.AutostartFlexIpfs}");
            AnsiConsole.WriteLine();

            var choice = AnsiConsole.Prompt(
                new SelectionPrompt<string>()
                    .Title(L("Settings menu"))
                    .AddChoices("Client / Backend", "Flexible-IPFS", "Trusted indexers", "Language", "Time zone", "kadrtt.properties", "Backend control", "Back")
                    .UseConverter(L)
            );

            switch (choice)
            {
                case "Client / Backend":
                {
                    var updated = PromptClientBackendSettings(cfg);
                    cfg = await ApplyConfigAsync(configStore, launcher, cfg, updated, ct);
                    break;
                }
                case "Flexible-IPFS":
                {
                    var updated = PromptFlexIpfsSettings(cfg);
                    cfg = await ApplyConfigAsync(configStore, launcher, cfg, updated, ct);
                    break;
                }
                case "Trusted indexers":
                    await TrustedIndexersMenuAsync(cfg, ct);
                    break;
                case "Language":
                {
                    var langs = new[] { cfg.UiLanguage, "auto", "en", "ja" }
                        .Distinct(StringComparer.OrdinalIgnoreCase)
                        .ToList();

                    var selected = AnsiConsole.Prompt(
                        new SelectionPrompt<string>()
                            .Title(L("UI language"))
                            .AddChoices(langs)
                            .UseConverter(L)
                    );
                    var updated = cfg with { UiLanguage = selected };
                    cfg = await ApplyConfigAsync(configStore, launcher, cfg, updated, ct);
                    SetLanguage(cfg.UiLanguage);
                    break;
                }
                case "Time zone":
                {
                    var zones = new[] { cfg.UiTimeZone, "utc", "jst" }
                        .Distinct(StringComparer.OrdinalIgnoreCase)
                        .ToList();

                    var selected = AnsiConsole.Prompt(
                        new SelectionPrompt<string>()
                            .Title(L("Time zone"))
                            .AddChoices(zones)
                            .UseConverter(z => z.ToUpperInvariant())
                    );
                    var updated = cfg with { UiTimeZone = selected };
                    cfg = await ApplyConfigAsync(configStore, launcher, cfg, updated, ct);
                    SetTimeZone(cfg.UiTimeZone);
                    break;
                }
                case "kadrtt.properties":
                {
                    var changed = await KadrttPropertiesMenuAsync(cfg, launcher, ct);
                    if (changed)
                    {
                        await RestartBackendForFlexIpfsAsync(cfg, launcher, ct);
                        Pause();
                    }
                    break;
                }
                case "Backend control":
                    await BackendControlMenuAsync(cfg, launcher, ct);
                    break;
                case "Back":
                    return cfg;
            }
        }
    }

    private static async Task TrustedIndexersMenuAsync(ClientConfig cfg, CancellationToken ct)
    {
        cfg = cfg.Normalize();

        var bbsNodePath = cfg.BbsNodePath ?? BbsNodePathResolver.Resolve();
        if (string.IsNullOrWhiteSpace(bbsNodePath))
        {
            AnsiConsole.MarkupLine($"[red]{ML("bbs-node not found.")}[/] {ML("Set it in Settings → Client / Backend.")}");
            Pause();
            return;
        }

        while (true)
        {
            List<string> indexers;
            try
            {
                var output = await RunProcessCaptureAsync(
                    bbsNodePath,
                    new[] { "list-trusted-indexers", "--data-dir", cfg.DataDir },
                    ct
                );
                indexers = JsonSerializer.Deserialize<List<string>>(output.StdOut.Trim()) ?? [];
            }
            catch (Exception ex)
            {
                ShowError(ex);
                Pause();
                indexers = [];
            }

            AnsiConsole.Clear();
            AnsiConsole.Write(new Rule($"[bold]{ML("Trusted indexers")}[/]").LeftJustified());
            AnsiConsole.MarkupLine($"[grey]{ML("Data dir")}:[/] {Markup.Escape(cfg.DataDir)}");
            AnsiConsole.WriteLine();

            var table = new Table().Border(TableBorder.Rounded);
            table.Expand = true;
            table.AddColumn("#");
            table.AddColumn(L("Base URL"));
            if (indexers.Count == 0)
            {
                table.AddRow("-", $"[grey]{ML("<none>")}[/]");
            }
            else
            {
                for (var i = 0; i < indexers.Count; i++)
                {
                    table.AddRow($"{i + 1}", Markup.Escape(indexers[i]));
                }
            }
            AnsiConsole.Write(table);
            AnsiConsole.WriteLine();

            var action = AnsiConsole.Prompt(
                new SelectionPrompt<string>()
                    .Title(L("Action"))
                    .AddChoices("Add", "Remove", "Import from bootstrap", "Refresh", "Back")
                    .UseConverter(L)
            );

            switch (action)
            {
                case "Add":
                {
                    var url = AnsiConsole.Ask<string>(L("Indexer base URL"));
                    if (string.IsNullOrWhiteSpace(url))
                    {
                        break;
                    }
                    url = url.Trim();

                    if (!TryValidateHttpUrl(url, out var err))
                    {
                        AnsiConsole.MarkupLine($"[red]{ML("Invalid backend URL")}:[/] {Markup.Escape(err)}");
                        Pause();
                        break;
                    }

                    try
                    {
                        var output = await RunProcessCaptureAsync(
                            bbsNodePath,
                            new[] { "add-trusted-indexer", "--base-url", url, "--data-dir", cfg.DataDir },
                            ct
                        );
                        var msg = output.StdOut.Trim();
                        AnsiConsole.MarkupLine(msg == "" ? "[green]ok[/]" : $"[green]{Markup.Escape(msg)}[/]");
                    }
                    catch (Exception ex)
                    {
                        ShowError(ex);
                    }
                    Pause();
                    break;
                }
                case "Remove":
                {
                    if (indexers.Count == 0)
                    {
                        AnsiConsole.MarkupLine($"[grey]{ML("No results to select.")}[/]");
                        Pause();
                        break;
                    }
                    var selected = PromptWithBack(
                        L("Select trusted indexer"),
                        indexers,
                        Markup.Escape,
                        moreChoicesText: $"[grey]{ML("(move up and down to reveal more)")}[/]"
                    );
                    if (selected == null)
                    {
                        break;
                    }
                    if (!AnsiConsole.Confirm(F("Remove '{0}'?", selected), false))
                    {
                        break;
                    }

                    try
                    {
                        var output = await RunProcessCaptureAsync(
                            bbsNodePath,
                            new[] { "remove-trusted-indexer", "--base-url", selected, "--data-dir", cfg.DataDir },
                            ct
                        );
                        var msg = output.StdOut.Trim();
                        AnsiConsole.MarkupLine(msg == "" ? "[green]ok[/]" : $"[green]{Markup.Escape(msg)}[/]");
                    }
                    catch (Exception ex)
                    {
                        ShowError(ex);
                    }
                    Pause();
                    break;
                }
                case "Import from bootstrap":
                {
                    var url = AnsiConsole.Ask<string>(L("Bootstrap indexer base URL"));
                    if (string.IsNullOrWhiteSpace(url))
                    {
                        break;
                    }
                    url = url.Trim();

                    if (!TryValidateHttpUrl(url, out var err))
                    {
                        AnsiConsole.MarkupLine($"[red]{ML("Invalid backend URL")}:[/] {Markup.Escape(err)}");
                        Pause();
                        break;
                    }

                    try
                    {
                        var output = await RunProcessCaptureAsync(
                            bbsNodePath,
                            new[] { "sync-trusted-indexers", "--bootstrap-url", url, "--data-dir", cfg.DataDir },
                            ct
                        );
                        var msg = output.StdOut.Trim();
                        AnsiConsole.MarkupLine(msg == "" ? "[green]ok[/]" : $"[green]{Markup.Escape(msg)}[/]");
                    }
                    catch (Exception ex)
                    {
                        ShowError(ex);
                    }
                    Pause();
                    break;
                }
                case "Refresh":
                    break;
                case "Back":
                    return;
            }
        }
    }

    private static ClientConfig PromptClientBackendSettings(ClientConfig cfg)
    {
        var backend = AnsiConsole.Ask(L("Backend base URL"), cfg.BackendBaseUrl);
        var derivedListen = "";
        try
        {
            var u = new Uri(backend);
            derivedListen = $"{u.Host}:{u.Port}";
        }
        catch
        {
        }

        var currentListen = string.IsNullOrWhiteSpace(cfg.BackendListenHostPort) ? derivedListen : cfg.BackendListenHostPort!;
        var listenInput = AnsiConsole.Prompt(
            new TextPrompt<string>(
                    F(
                        "Backend listen address (host:port, blank = derived) [grey](current: {0})[/]",
                        EscapePrompt(string.IsNullOrWhiteSpace(currentListen) ? L("<empty>") : currentListen)
                    )
                )
                .AllowEmpty()
        );
        var listen = string.IsNullOrWhiteSpace(listenInput) ? null : listenInput.Trim();
        if (listen != null && derivedListen != "" && string.Equals(listen, derivedListen, StringComparison.OrdinalIgnoreCase))
        {
            // Keep config minimal; store null to mean "derive from Backend base URL".
            listen = null;
        }

        var roles = new[] { cfg.BackendRole, "client", "indexer", "archiver", "full" }
            .Distinct(StringComparer.OrdinalIgnoreCase)
            .ToList();
        var backendRole = AnsiConsole.Prompt(
            new SelectionPrompt<string>()
                .Title(L("Backend role (managed)"))
                .AddChoices(roles)
        );
        var dataDir = AnsiConsole.Ask(L("Data dir"), cfg.DataDir);
        var startBackend = AnsiConsole.Confirm(L("Auto-start backend (manage local bbs-node)?"), cfg.StartBackend);

        var currentPath = cfg.BbsNodePath ?? L("<auto>");
        var bbsNodePathInput = AnsiConsole.Prompt(
            new TextPrompt<string>(F("bbs-node path (blank = auto) [grey](current: {0})[/]", EscapePrompt(currentPath)))
                .AllowEmpty()
        );
        var bbsNodePath = string.IsNullOrWhiteSpace(bbsNodePathInput) ? null : bbsNodePathInput.Trim();

        return cfg with
        {
            BackendBaseUrl = backend,
            BackendListenHostPort = listen,
            BackendRole = backendRole,
            DataDir = dataDir,
            StartBackend = startBackend,
            BbsNodePath = bbsNodePath,
        };
    }

    private static ClientConfig PromptFlexIpfsSettings(ClientConfig cfg)
    {
        var flexBaseUrl = AnsiConsole.Ask(L("Flexible-IPFS HTTP API base URL"), cfg.FlexIpfsBaseUrl);
        var autostartFlexIpfs = AnsiConsole.Confirm(L("Autostart Flexible-IPFS (when managed by bbs-node)?"), cfg.AutostartFlexIpfs);
        var flexIpfsMdns = AnsiConsole.Confirm(L("Use mDNS on LAN to discover flex-ipfs gw endpoint?"), cfg.FlexIpfsMdns);
        var flexIpfsMdnsTimeoutSeconds = AnsiConsole.Prompt(
            new TextPrompt<int>(L("mDNS discovery timeout (seconds)"))
                .DefaultValue(cfg.FlexIpfsMdnsTimeoutSeconds)
                .Validate(v => v >= 1 ? ValidationResult.Success() : ValidationResult.Error(L("timeout must be >= 1")))
        );

        var currentBaseDir = cfg.FlexIpfsBaseDir ?? L("<auto>");
        var baseDirInput = AnsiConsole.Prompt(
            new TextPrompt<string>(F("flexible-ipfs-base dir (blank = auto) [grey](current: {0})[/]", EscapePrompt(currentBaseDir)))
                .AllowEmpty()
        );
        var baseDir = string.IsNullOrWhiteSpace(baseDirInput) ? null : baseDirInput.Trim();

        var currentGw = cfg.FlexIpfsGwEndpoint ?? L("<none>");
        var gwInput = AnsiConsole.Prompt(
            new TextPrompt<string>(F("ipfs.endpoint override (blank = none) [grey](e.g. /ip4/192.168.0.10/tcp/4001/ipfs/<PeerID>, current: {0})[/]", EscapePrompt(currentGw)))
                .AllowEmpty()
        );
        var gw = string.IsNullOrWhiteSpace(gwInput) ? null : gwInput.Trim();

        return cfg with
        {
            FlexIpfsBaseUrl = flexBaseUrl,
            AutostartFlexIpfs = autostartFlexIpfs,
            FlexIpfsMdns = flexIpfsMdns,
            FlexIpfsMdnsTimeoutSeconds = flexIpfsMdnsTimeoutSeconds,
            FlexIpfsBaseDir = baseDir,
            FlexIpfsGwEndpoint = gw,
        };
    }

    private static async Task<ClientConfig> ApplyConfigAsync(
        ClientConfigStore configStore,
        BackendLauncher launcher,
        ClientConfig oldCfg,
        ClientConfig newCfg,
        CancellationToken ct
    )
    {
        oldCfg = oldCfg.Normalize();
        newCfg = newCfg.Normalize();

        if (!TryValidateHttpUrl(newCfg.BackendBaseUrl, out var backendErr))
        {
            AnsiConsole.MarkupLine($"[red]{ML("Invalid backend URL")}:[/] {Markup.Escape(backendErr)}");
            Pause();
            return oldCfg;
        }
        if (newCfg.StartBackend)
        {
            var listenHostPort = BbsNodeArgsBuilder.ResolveListenHostPort(newCfg);
            if (!TryValidateHostPort(listenHostPort, out var listenErr))
            {
                AnsiConsole.MarkupLine($"[red]{ML("Invalid backend listen address")}:[/] {Markup.Escape(listenErr)}");
                Pause();
                return oldCfg;
            }
            try
            {
                var backendUri = new Uri(newCfg.BackendBaseUrl);
                var listenUri = new Uri("http://" + listenHostPort);
                if (backendUri.Port != listenUri.Port)
                {
                    AnsiConsole.MarkupLine($"[red]{ML("Backend port mismatch")}:[/] {Markup.Escape($"{backendUri.Port} != {listenUri.Port}")}");
                    Pause();
                    return oldCfg;
                }
            }
            catch
            {
                // ignore; validated above
            }
        }
        if (!TryValidateHttpUrl(newCfg.FlexIpfsBaseUrl, out var flexErr))
        {
            AnsiConsole.MarkupLine($"[red]{ML("Invalid Flexible-IPFS base URL")}:[/] {Markup.Escape(flexErr)}");
            Pause();
            return oldCfg;
        }
        if (string.IsNullOrWhiteSpace(newCfg.DataDir))
        {
            AnsiConsole.MarkupLine($"[red]{ML("Data dir is required.")}[/]");
            Pause();
            return oldCfg;
        }

        if (newCfg == oldCfg)
        {
            return oldCfg;
        }

        try
        {
            await configStore.SaveAsync(newCfg, ct);
            AnsiConsole.MarkupLine($"[green]ok[/] {ML("saved")}");
        }
        catch (Exception ex)
        {
            ShowError(ex);
            Pause();
            return oldCfg;
        }

        if (!newCfg.StartBackend && oldCfg.StartBackend && launcher.IsManagingProcess)
        {
            launcher.StopManaged();
        }

        var restartNeeded =
            !string.Equals(oldCfg.BackendBaseUrl, newCfg.BackendBaseUrl, StringComparison.Ordinal) ||
            !string.Equals(oldCfg.BackendListenHostPort, newCfg.BackendListenHostPort, StringComparison.Ordinal) ||
            !string.Equals(oldCfg.BackendRole, newCfg.BackendRole, StringComparison.Ordinal) ||
            !string.Equals(oldCfg.DataDir, newCfg.DataDir, StringComparison.Ordinal) ||
            !string.Equals(oldCfg.FlexIpfsBaseUrl, newCfg.FlexIpfsBaseUrl, StringComparison.Ordinal) ||
            !string.Equals(oldCfg.FlexIpfsBaseDir, newCfg.FlexIpfsBaseDir, StringComparison.Ordinal) ||
            !string.Equals(oldCfg.FlexIpfsGwEndpoint, newCfg.FlexIpfsGwEndpoint, StringComparison.Ordinal) ||
            oldCfg.FlexIpfsMdns != newCfg.FlexIpfsMdns ||
            oldCfg.FlexIpfsMdnsTimeoutSeconds != newCfg.FlexIpfsMdnsTimeoutSeconds ||
            oldCfg.AutostartFlexIpfs != newCfg.AutostartFlexIpfs;

        if (restartNeeded)
        {
            AppLog.Init(newCfg.DataDir);
            await RestartBackendForFlexIpfsAsync(newCfg, launcher, ct);
        }
        else if (newCfg.StartBackend && !await BackendLauncher.IsHealthyAsync(newCfg.BackendBaseUrl, ct))
        {
            try
            {
                var bbsNodePath = newCfg.BbsNodePath ?? BbsNodePathResolver.Resolve();
                await AnsiConsole.Status()
                    .Spinner(Spinner.Known.Dots)
                    .StartAsync(L("Starting backend..."), async _ =>
                    {
                        await launcher.EnsureRunningAsync(newCfg.BackendBaseUrl, newCfg.StartBackend, bbsNodePath, BbsNodeArgsBuilder.Build(newCfg), ct);
                    });
                AnsiConsole.MarkupLine($"[green]ok[/] {ML("started")}");
            }
            catch (Exception ex)
            {
                ShowError(ex);
            }
        }

        Pause();
        return newCfg;
    }

    private static async Task BackendControlMenuAsync(ClientConfig cfg, BackendLauncher launcher, CancellationToken ct)
    {
        while (true)
        {
            var healthy = await BackendLauncher.IsHealthyAsync(cfg.BackendBaseUrl, ct);
            var managed = launcher.IsManagingProcess;

            AnsiConsole.Clear();
            AnsiConsole.Write(new Rule($"[bold]{ML("Backend control")}[/]").LeftJustified());
            AnsiConsole.MarkupLine($"[grey]{ML("Backend")}:[/] {Markup.Escape(cfg.BackendBaseUrl)} [{(healthy ? "green" : "red")}]{ML(healthy ? "up" : "down")}[/]");
            AnsiConsole.MarkupLine($"[grey]{ML("Managed by this client")}:[/] {managed} {(launcher.ManagedPid is int pid ? $"(pid={pid})" : "")}".TrimEnd());
            AnsiConsole.MarkupLine($"[grey]{ML("Backend role (managed)")}:[/] {Markup.Escape(cfg.BackendRole)}");
            AnsiConsole.MarkupLine($"[grey]{ML("Auto-start backend")}:[/] {cfg.StartBackend}");
            AnsiConsole.WriteLine();

            var actions = new List<string>();
            if (!healthy)
            {
                actions.Add("Start");
            }
            if (managed)
            {
                actions.Add("Restart");
                actions.Add("Stop");
            }
            actions.Add("Back");

            var action = AnsiConsole.Prompt(new SelectionPrompt<string>().Title(L("Action")).AddChoices(actions).UseConverter(L));
            switch (action)
            {
                case "Start":
                    try
                    {
                        var bbsNodePath = cfg.BbsNodePath ?? BbsNodePathResolver.Resolve();
                        await AnsiConsole.Status()
                            .Spinner(Spinner.Known.Dots)
                            .StartAsync(L("Starting backend..."), async _ =>
                            {
                                await launcher.EnsureRunningAsync(cfg.BackendBaseUrl, cfg.StartBackend, bbsNodePath, BbsNodeArgsBuilder.Build(cfg), ct);
                            });
                        AnsiConsole.MarkupLine("[green]ok[/]");
                    }
                    catch (Exception ex)
                    {
                        ShowError(ex);
                    }
                    Pause();
                    break;
                case "Restart":
                    await RestartBackendForFlexIpfsAsync(cfg, launcher, ct);
                    Pause();
                    break;
                case "Stop":
                    launcher.StopManaged();
                    AnsiConsole.MarkupLine("[green]ok[/]");
                    Pause();
                    break;
                case "Back":
                    return;
            }
        }
    }

    private static async Task RestartBackendForFlexIpfsAsync(ClientConfig cfg, BackendLauncher launcher, CancellationToken ct)
    {
        cfg = cfg.Normalize();

        if (!cfg.StartBackend)
        {
            AnsiConsole.MarkupLine($"[yellow]{ML("Auto-start is disabled; restart skipped.")}[/]");
            return;
        }

        var bbsNodePath = cfg.BbsNodePath ?? BbsNodePathResolver.Resolve();
        var bbsNodeArgs = BbsNodeArgsBuilder.Build(cfg);

        try
        {
            var restarted = await AnsiConsole.Status()
                .Spinner(Spinner.Known.Dots)
                .StartAsync(L("Restarting backend..."), async _ =>
                {
                    return await launcher.RestartManagedAsync(cfg.BackendBaseUrl, cfg.StartBackend, bbsNodePath, bbsNodeArgs, ct);
                });
            if (restarted)
            {
                AnsiConsole.MarkupLine($"[green]ok[/] {ML("restarted")}");
                return;
            }

            if (!await BackendLauncher.IsHealthyAsync(cfg.BackendBaseUrl, ct))
            {
                await AnsiConsole.Status()
                    .Spinner(Spinner.Known.Dots)
                    .StartAsync(L("Starting backend..."), async _ =>
                    {
                        await launcher.EnsureRunningAsync(cfg.BackendBaseUrl, cfg.StartBackend, bbsNodePath, bbsNodeArgs, ct);
                    });
                AnsiConsole.MarkupLine($"[green]ok[/] {ML("started")}");
                return;
            }

            AnsiConsole.MarkupLine($"[yellow]{ML("Backend is running but not managed by this client; please restart it manually to apply settings.")}[/]");
        }
        catch (Exception ex)
        {
            ShowError(ex);
        }
    }

    private static async Task<bool> KadrttPropertiesMenuAsync(ClientConfig cfg, BackendLauncher launcher, CancellationToken ct)
    {
        _ = launcher;

        var baseDir = ResolveFlexIpfsBaseDir(cfg);
        if (string.IsNullOrWhiteSpace(baseDir))
        {
            AnsiConsole.MarkupLine($"[red]{ML("flexible-ipfs-base dir not found. Set it in Flexible-IPFS settings.")}[/]");
            Pause();
            return false;
        }

        var propsPath = Path.Combine(baseDir, "kadrtt.properties");
        if (!File.Exists(propsPath))
        {
            AnsiConsole.MarkupLine($"[red]{ML("kadrtt.properties not found")}:[/] {Markup.Escape(propsPath)}");
            Pause();
            return false;
        }

        var changed = false;

        while (true)
        {
            string content;
            try
            {
                content = await File.ReadAllTextAsync(propsPath, ct);
            }
            catch (Exception ex)
            {
                ShowError(ex);
                Pause();
                return changed;
            }

            var entries = ParseJavaProperties(content);

            AnsiConsole.Clear();
            AnsiConsole.Write(new Rule("[bold]kadrtt.properties[/]").LeftJustified());
            AnsiConsole.MarkupLine($"[grey]{Markup.Escape(propsPath)}[/]");
            if (!string.IsNullOrWhiteSpace(cfg.FlexIpfsGwEndpoint))
            {
                AnsiConsole.MarkupLine($"[yellow]{ML("Note")}:[/] {ML("ipfs.endpoint override is set; it will be applied on autostart/restart.")}");
            }
            AnsiConsole.WriteLine();

            var table = new Table().Border(TableBorder.Rounded);
            table.Expand = true;
            table.AddColumn(L("Key"));
            table.AddColumn(L("Value"));
            foreach (var kv in entries.OrderBy(e => e.Key, StringComparer.OrdinalIgnoreCase))
            {
                table.AddRow(Markup.Escape(kv.Key), Markup.Escape(Short(kv.Value, 60)));
            }
            AnsiConsole.Write(table);
            AnsiConsole.WriteLine();

            var action = AnsiConsole.Prompt(
                new SelectionPrompt<string>()
                    .Title(L("Action"))
                    .AddChoices("Edit", "Add", "Remove", "Back")
                    .UseConverter(L)
            );

            switch (action)
            {
                case "Edit":
                {
                    if (entries.Count == 0)
                    {
                        AnsiConsole.MarkupLine($"[grey]{ML("No properties found.")}[/]");
                        Pause();
                        break;
                    }
                    var key = AnsiConsole.Prompt(
                        new SelectionPrompt<string>()
                            .Title(L("Select key"))
                            .PageSize(12)
                            .MoreChoicesText($"[grey]{ML("(move up and down to reveal more)")}[/]")
                            .AddChoices(entries.Keys.Order(StringComparer.OrdinalIgnoreCase))
                            .UseConverter(Markup.Escape)
                    );
                    var current = entries.GetValueOrDefault(key, "");
                    AnsiConsole.Clear();
                    AnsiConsole.Write(new Rule($"[bold]{ML("Edit property")}[/]").LeftJustified());
                    AnsiConsole.MarkupLine($"[grey]{ML("Key")}:[/] {Markup.Escape(key)}");
                    AnsiConsole.MarkupLine($"[grey]{ML("Current value")}:[/]");
                    if (string.IsNullOrEmpty(current))
                    {
                        AnsiConsole.MarkupLine($"[grey]{ML("<empty>")}[/]");
                    }
                    else
                    {
                        var chunkSize = Math.Clamp(AnsiConsole.Profile.Width - 8, 24, 200);
                        var currentPanel = new Panel(new Markup(Markup.Escape(WrapToken(current, chunkSize))))
                            .BorderColor(Color.Grey)
                            .Border(BoxBorder.Rounded);
                        currentPanel.Expand = true;
                        AnsiConsole.Write(currentPanel);
                    }
                    AnsiConsole.WriteLine();

                    var value = AnsiConsole.Prompt(new TextPrompt<string>(L("New value (single line, blank = empty)")).AllowEmpty());
                    if (value.Contains('\n') || value.Contains('\r'))
                    {
                        AnsiConsole.MarkupLine($"[red]{ML("Value must be a single line.")}[/]");
                        Pause();
                        break;
                    }
                    if (TryUpsertJavaProperty(content, key, value, out var updatedContent))
                    {
                        await File.WriteAllTextAsync(propsPath, updatedContent, ct);
                        changed = true;
                        AnsiConsole.MarkupLine("[green]ok[/]");
                        Pause();
                    }
                    break;
                }
                case "Add":
                {
                    var key = AnsiConsole.Ask<string>(L("Key"));
                    key = key.Trim();
                    if (string.IsNullOrWhiteSpace(key) || key.Contains('=') || key.Contains(':') || key.Contains('\n') || key.Contains('\r'))
                    {
                        AnsiConsole.MarkupLine($"[red]{ML("Invalid key.")}[/]");
                        Pause();
                        break;
                    }
                    var value = AnsiConsole.Prompt(new TextPrompt<string>(L("Value")).AllowEmpty());
                    if (value.Contains('\n') || value.Contains('\r'))
                    {
                        AnsiConsole.MarkupLine($"[red]{ML("Value must be a single line.")}[/]");
                        Pause();
                        break;
                    }
                    if (TryUpsertJavaProperty(content, key, value, out var updatedContent))
                    {
                        await File.WriteAllTextAsync(propsPath, updatedContent, ct);
                        changed = true;
                        AnsiConsole.MarkupLine("[green]ok[/]");
                        Pause();
                    }
                    break;
                }
                case "Remove":
                {
                    if (entries.Count == 0)
                    {
                        AnsiConsole.MarkupLine($"[grey]{ML("No properties to remove.")}[/]");
                        Pause();
                        break;
                    }
                    var key = AnsiConsole.Prompt(
                        new SelectionPrompt<string>()
                            .Title(L("Select key to remove"))
                            .PageSize(12)
                            .MoreChoicesText($"[grey]{ML("(move up and down to reveal more)")}[/]")
                            .AddChoices(entries.Keys.Order(StringComparer.OrdinalIgnoreCase))
                            .UseConverter(Markup.Escape)
                    );
                    if (!AnsiConsole.Confirm(F("Remove '{0}'?", key), false))
                    {
                        break;
                    }
                    if (TryRemoveJavaProperty(content, key, out var updatedContent))
                    {
                        await File.WriteAllTextAsync(propsPath, updatedContent, ct);
                        changed = true;
                        AnsiConsole.MarkupLine("[green]ok[/]");
                        Pause();
                    }
                    break;
                }
                case "Back":
                    return changed;
            }
        }
    }

    private static string? ResolveFlexIpfsBaseDir(ClientConfig cfg)
    {
        cfg = cfg.Normalize();
        if (!string.IsNullOrWhiteSpace(cfg.FlexIpfsBaseDir))
        {
            return cfg.FlexIpfsBaseDir;
        }

        var candidates = new List<string>
        {
            Path.Combine(AppContext.BaseDirectory, "flexible-ipfs-base"),
            Path.Combine(AppContext.BaseDirectory, "..", "flexible-ipfs-base"),
            Path.Combine(Environment.CurrentDirectory, "flexible-ipfs-base"),
        };
        foreach (var c in candidates)
        {
            if (Directory.Exists(c))
            {
                return Path.GetFullPath(c);
            }
        }
        return null;
    }

    private static SortedDictionary<string, string> ParseJavaProperties(string content)
    {
        var dict = new SortedDictionary<string, string>(StringComparer.OrdinalIgnoreCase);
        var lines = content.Replace("\r\n", "\n").Split('\n');
        foreach (var raw in lines)
        {
            var line = raw.TrimEnd('\r');
            var trimLeft = line.TrimStart(' ', '\t');
            if (string.IsNullOrWhiteSpace(trimLeft) || trimLeft.StartsWith("#", StringComparison.Ordinal) || trimLeft.StartsWith("!", StringComparison.Ordinal))
            {
                continue;
            }
            var sepIndex = trimLeft.IndexOfAny(['=', ':']);
            if (sepIndex <= 0)
            {
                continue;
            }
            var key = trimLeft[..sepIndex].Trim();
            var value = trimLeft[(sepIndex + 1)..].Trim();
            if (key.Length == 0)
            {
                continue;
            }
            dict[key] = value;
        }
        return dict;
    }

    private static bool TryUpsertJavaProperty(string original, string key, string value, out string updated)
    {
        updated = original;
        key = key.Trim();
        if (string.IsNullOrWhiteSpace(key) || key.IndexOfAny(['\r', '\n']) >= 0)
        {
            return false;
        }
        if (value.IndexOfAny(['\r', '\n']) >= 0)
        {
            return false;
        }

        var lineSep = original.Contains("\r\n", StringComparison.Ordinal) ? "\r\n" : "\n";
        var hadTrailingNewline = original.EndsWith(lineSep, StringComparison.Ordinal);
        var lines = original.Split(lineSep);

        var replaced = false;
        for (var i = 0; i < lines.Length; i++)
        {
            var line = lines[i];
            var trimLeft = line.TrimStart(' ', '\t');
            if (trimLeft.StartsWith("#", StringComparison.Ordinal) || trimLeft.StartsWith("!", StringComparison.Ordinal))
            {
                continue;
            }
            var sepIndex = trimLeft.IndexOfAny(['=', ':']);
            if (sepIndex <= 0)
            {
                continue;
            }
            var k = trimLeft[..sepIndex].Trim();
            if (!string.Equals(k, key, StringComparison.OrdinalIgnoreCase))
            {
                continue;
            }

            var indentLen = line.Length - trimLeft.Length;
            var indent = indentLen > 0 ? line[..indentLen] : "";
            var sep = trimLeft[sepIndex];
            lines[i] = $"{indent}{key}{sep}{value}";
            replaced = true;
        }

        if (!replaced)
        {
            var list = lines.ToList();
            if (list.Count > 0 && list[^1].Length != 0)
            {
                list.Add("");
            }
            list.Add($"{key}={value}");
            lines = list.ToArray();
        }

        updated = string.Join(lineSep, lines);
        if (hadTrailingNewline && !updated.EndsWith(lineSep, StringComparison.Ordinal))
        {
            updated += lineSep;
        }
        return true;
    }

    private static bool TryRemoveJavaProperty(string original, string key, out string updated)
    {
        updated = original;
        key = key.Trim();
        if (string.IsNullOrWhiteSpace(key) || key.IndexOfAny(['\r', '\n']) >= 0)
        {
            return false;
        }

        var lineSep = original.Contains("\r\n", StringComparison.Ordinal) ? "\r\n" : "\n";
        var hadTrailingNewline = original.EndsWith(lineSep, StringComparison.Ordinal);
        var lines = original.Split(lineSep);

        var changed = false;
        var kept = new List<string>(lines.Length);
        foreach (var line in lines)
        {
            var trimLeft = line.TrimStart(' ', '\t');
            if (trimLeft.StartsWith("#", StringComparison.Ordinal) || trimLeft.StartsWith("!", StringComparison.Ordinal))
            {
                kept.Add(line);
                continue;
            }
            var sepIndex = trimLeft.IndexOfAny(['=', ':']);
            if (sepIndex <= 0)
            {
                kept.Add(line);
                continue;
            }
            var k = trimLeft[..sepIndex].Trim();
            if (string.Equals(k, key, StringComparison.OrdinalIgnoreCase))
            {
                changed = true;
                continue;
            }
            kept.Add(line);
        }

        if (!changed)
        {
            return false;
        }

        updated = string.Join(lineSep, kept);
        if (hadTrailingNewline && !updated.EndsWith(lineSep, StringComparison.Ordinal))
        {
            updated += lineSep;
        }
        return true;
    }

    private static bool TryValidateHttpUrl(string url, out string error)
    {
        error = "";
        if (!Uri.TryCreate(url, UriKind.Absolute, out var u))
        {
            error = L("not a valid absolute URL");
            return false;
        }
        if (u.Scheme is not ("http" or "https"))
        {
            error = L("scheme must be http or https");
            return false;
        }
        return true;
    }

    private static bool TryValidateHostPort(string hostPort, out string error)
    {
        error = "";
        hostPort = hostPort.Trim();
        if (string.IsNullOrWhiteSpace(hostPort))
        {
            error = L("host:port is empty");
            return false;
        }

        // Require an explicit port.
        if (hostPort.StartsWith("[", StringComparison.Ordinal))
        {
            if (!hostPort.Contains("]:", StringComparison.Ordinal))
            {
                error = L("IPv6 must be in [::1]:port form");
                return false;
            }
        }
        else
        {
            var idx = hostPort.LastIndexOf(':');
            if (idx <= 0 || idx >= hostPort.Length - 1)
            {
                error = L("must be in host:port form");
                return false;
            }
        }

        if (!Uri.TryCreate("http://" + hostPort, UriKind.Absolute, out var u))
        {
            error = L("not a valid host:port");
            return false;
        }
        if (string.IsNullOrWhiteSpace(u.Host))
        {
            error = L("host is empty");
            return false;
        }
        if (u.Port is < 1 or > 65535)
        {
            error = L("port must be 1-65535");
            return false;
        }
        return true;
    }

    private static string EscapePrompt(string value)
    {
        return value.Replace("[", "[[").Replace("]", "]]");
    }

    private static string ReadMultiline(string label)
    {
        AnsiConsole.MarkupLine($"[grey]{Markup.Escape(label)}[/] {Markup.Escape(L("(finish with a single '.' line):"))}");
        var lines = new List<string>();
        while (true)
        {
            var line = Console.ReadLine();
            if (line == null || line == ".")
            {
                break;
            }
            lines.Add(line);
        }
        return string.Join("\n", lines).TrimEnd();
    }

    private static string Short(string value, int maxLen)
    {
        if (string.IsNullOrEmpty(value))
        {
            return value;
        }
        return value.Length <= maxLen ? value : value[..Math.Max(0, maxLen - 3)] + "...";
    }

    private static string WrapToken(string value, int chunkSize)
    {
        if (string.IsNullOrEmpty(value) || chunkSize <= 0)
        {
            return value;
        }
        if (value.Length <= chunkSize)
        {
            return value;
        }

        var sb = new StringBuilder(value.Length + (value.Length / chunkSize) + 8);
        for (var i = 0; i < value.Length; i += chunkSize)
        {
            if (i > 0)
            {
                sb.Append('\n');
            }
            sb.Append(value, i, Math.Min(chunkSize, value.Length - i));
        }
        return sb.ToString();
    }

    private static string KeyValueMarkupAuto(string key, string value)
    {
        return KeyValueMarkup(key, value, GetAdaptiveKeyValueChunkSize(key));
    }

    private static int GetAdaptiveKeyValueChunkSize(string key)
    {
        key = key.Trim();

        var width = AnsiConsole.Profile.Width;
        if (width <= 0)
        {
            return 48;
        }

        // Rough budget: panel borders/padding + a little breathing room.
        var reserved = key.Length + 2 + 12;
        return Math.Clamp(width - reserved, 24, 200);
    }

    private static string KeyValueMarkup(string key, string value, int chunkSize)
    {
        key = key.Trim();
        value ??= "";

        var wrapped = WrapToken(value, chunkSize);
        var parts = wrapped.Split('\n');

        if (parts.Length == 0)
        {
            return $"[grey]{Markup.Escape(key)}:[/]";
        }

        var indent = new string(' ', key.Length + 2);
        var sb = new StringBuilder();
        sb.Append($"[grey]{Markup.Escape(key)}:[/] {Markup.Escape(parts[0])}");
        for (var i = 1; i < parts.Length; i++)
        {
            sb.Append('\n');
            sb.Append(indent);
            sb.Append(Markup.Escape(parts[i]));
        }
        return sb.ToString();
    }

    private static string? EmptyToNull(string value)
    {
        return string.IsNullOrWhiteSpace(value) ? null : value.Trim();
    }

    private static void ShowError(Exception ex)
    {
        AnsiConsole.MarkupLine($"[red]{ML("Error")}:[/] {Markup.Escape(ex.Message)}");
        AppLog.Error(ex.Message, ex);
    }

    private static void Pause()
    {
        AnsiConsole.MarkupLine($"[grey]{ML("Press Enter to continue...")}[/]");
        Console.ReadLine();
    }

    private sealed record ProcessOutput(string StdOut, string StdErr, int ExitCode);

    private static async Task<ProcessOutput> RunProcessCaptureAsync(string fileName, IEnumerable<string> args, CancellationToken ct)
    {
        var stdout = new TailBuffer(200);
        var stderr = new TailBuffer(200);

        var psi = new ProcessStartInfo
        {
            FileName = fileName,
            UseShellExecute = false,
            RedirectStandardOutput = true,
            RedirectStandardError = true,
            RedirectStandardInput = true,
            WorkingDirectory = AppContext.BaseDirectory,
        };
        foreach (var a in args)
        {
            psi.ArgumentList.Add(a);
        }

        using var p = Process.Start(psi) ?? throw new InvalidOperationException($"failed to start: {Path.GetFileName(fileName)}");
        try
        {
            p.StandardInput.Close();
        }
        catch
        {
        }

        var stdoutTask = PumpLinesAsync(p.StandardOutput, stdout);
        var stderrTask = PumpLinesAsync(p.StandardError, stderr);

        try
        {
            await p.WaitForExitAsync(ct);
        }
        catch (OperationCanceledException)
        {
            try
            {
                if (!p.HasExited)
                {
                    p.Kill(entireProcessTree: true);
                }
            }
            catch
            {
            }
            throw;
        }

        var pumps = Task.WhenAll(stdoutTask, stderrTask);
        if (await Task.WhenAny(pumps, Task.Delay(250)) != pumps)
        {
            // Some child processes (e.g. flex-ipfs) may inherit stdout/stderr and keep pipes open
            // even after bbs-node exits. Close our side so the pump tasks can finish.
            try
            {
                p.StandardOutput.Close();
            }
            catch
            {
            }
            try
            {
                p.StandardError.Close();
            }
            catch
            {
            }
            _ = await Task.WhenAny(pumps, Task.Delay(1000));
        }

        if (p.ExitCode != 0)
        {
            var msg = string.IsNullOrWhiteSpace(stderr.ToString()) ? stdout.ToString() : stderr.ToString();
            msg = string.IsNullOrWhiteSpace(msg) ? $"process exited with code {p.ExitCode}" : msg.Trim();
            throw new InvalidOperationException(msg);
        }

        return new ProcessOutput(stdout.ToString(), stderr.ToString(), p.ExitCode);
    }

    private static async Task PumpLinesAsync(StreamReader reader, TailBuffer buffer)
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
                if (line.Contains("Received: NCL_PING", StringComparison.Ordinal))
                {
                    continue;
                }
                buffer.Add(line);
            }
        }
        catch
        {
        }
    }

    private sealed class TailBuffer(int maxLines)
    {
        private readonly Queue<string> _lines = new();

        public void Add(string line)
        {
            _lines.Enqueue(line);
            while (_lines.Count > maxLines)
            {
                _lines.Dequeue();
            }
        }

        public override string ToString()
        {
            return string.Join("\n", _lines);
        }
    }

    private static string? TryExtractCid(string text)
    {
        if (string.IsNullOrWhiteSpace(text))
        {
            return null;
        }

        var tokens = text.Split(
            new[] { ' ', '\t', '\r', '\n', '"', '\'' },
            StringSplitOptions.RemoveEmptyEntries | StringSplitOptions.TrimEntries
        );

        string? last = null;
        foreach (var token in tokens)
        {
            if (token.Length >= 20 && token.StartsWith("baf", StringComparison.OrdinalIgnoreCase))
            {
                last = token;
                continue;
            }
            if (token.Length >= 40 && token.StartsWith("Qm", StringComparison.Ordinal))
            {
                last = token;
            }
        }
        return last;
    }

    private sealed record Choice<T>(string Label, T? Value, bool IsBack) where T : class;

    private static T? PromptWithBack<T>(
        string title,
        IReadOnlyList<T> items,
        Func<T, string> render,
        string? moreChoicesText = null,
        int pageSize = 12
    ) where T : class
    {
        var choices = items.Select(i => new Choice<T>(render(i), i, false)).ToList();
        choices.Add(new Choice<T>($"[grey]{ML("Back")}[/]", null, true));

        var prompt = new SelectionPrompt<Choice<T>>()
            .Title(title)
            .PageSize(pageSize)
            .AddChoices(choices)
            .UseConverter(c => c.Label);
        if (!string.IsNullOrWhiteSpace(moreChoicesText))
        {
            prompt.MoreChoicesText(moreChoicesText);
        }

        var selected = AnsiConsole.Prompt(prompt);
        return selected.IsBack ? null : selected.Value;
    }

    private static string FormatTimestamp(string? rfc3339)
    {
        if (string.IsNullOrWhiteSpace(rfc3339))
        {
            return rfc3339 ?? "";
        }

        if (!DateTimeOffset.TryParse(rfc3339, CultureInfo.InvariantCulture, DateTimeStyles.RoundtripKind, out var dto))
        {
            return rfc3339;
        }

        var converted = TimeZoneInfo.ConvertTime(dto, _uiTimeZone);
        return converted.ToString("yyyy-MM-dd HH:mm:ss", CultureInfo.InvariantCulture) + " " + _uiTimeZoneLabel;
    }

    private static (TimeZoneInfo TimeZone, string Label) ResolveTimeZone(string? timeZoneSetting)
    {
        var tz = string.IsNullOrWhiteSpace(timeZoneSetting) ? "utc" : timeZoneSetting.Trim().ToLowerInvariant();
        return tz switch
        {
            "jst" => (ResolveJstTimeZone(), "JST"),
            _ => (TimeZoneInfo.Utc, "UTC"),
        };
    }

    private static TimeZoneInfo ResolveJstTimeZone()
    {
        foreach (var id in new[] { "Asia/Tokyo", "Tokyo Standard Time" })
        {
            try
            {
                return TimeZoneInfo.FindSystemTimeZoneById(id);
            }
            catch
            {
            }
        }
        return TimeZoneInfo.CreateCustomTimeZone("JST", TimeSpan.FromHours(9), "JST", "JST");
    }
}
