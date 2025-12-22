using BbsClient.Api;
using BbsClient.Storage;
using BbsClient.Util;
using System.Diagnostics;
using Spectre.Console;

namespace BbsClient.Ui;

public static class InteractiveUi
{
    private const int DefaultPageSize = 50;

    public static async Task<int> RunAsync(
        ClientConfigStore configStore,
        ClientConfig initialConfig,
        BackendLauncher launcher,
        CancellationToken ct
    )
    {
        if (Console.IsInputRedirected || Console.IsOutputRedirected)
        {
            Console.Error.WriteLine("ui requires an interactive terminal (no redirection).");
            return 2;
        }

        var cfg = initialConfig.Normalize();
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
                .StartAsync("Ensuring backend is running...", async _ =>
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
            AnsiConsole.Write(new Rule("[bold]Flex BBS Client[/]").LeftJustified());
            AnsiConsole.MarkupLine($"[grey]Backend:[/] {Markup.Escape(cfg.BackendBaseUrl)} [{(healthy ? "green" : "red")}]{(healthy ? "up" : "down")}[/]");
            AnsiConsole.MarkupLine($"[grey]Backend role (managed):[/] {Markup.Escape(cfg.BackendRole)}");
            AnsiConsole.MarkupLine($"[grey]Data dir:[/] {Markup.Escape(cfg.DataDir)}");
            if (!string.IsNullOrWhiteSpace(backendStartError) && !healthy)
            {
                AnsiConsole.MarkupLine($"[red]Backend:[/] {Markup.Escape(backendStartError)}");
            }
            AnsiConsole.WriteLine();

            var choice = AnsiConsole.Prompt(
                new SelectionPrompt<string>()
                    .Title("Main menu")
                    .AddChoices("Browse boards", "Search posts", "Keys", "Blocked", "Settings", "Quit")
            );

            try
            {
                switch (choice)
                {
                    case "Browse boards":
                        await BrowseBoardsAsync(api, cfg, keys, blocked, ct);
                        break;
                    case "Search posts":
                        await SearchPostsAsync(api, keys, blocked, ct);
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
            AnsiConsole.Write(new Rule("[bold]Boards[/]").LeftJustified());

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
            table.AddColumn("BoardID");
            table.AddColumn("Title");
            table.AddColumn("MetaCID");
            foreach (var b in boards)
            {
                table.AddRow(
                    Markup.Escape(b.Board.BoardId),
                    Markup.Escape(b.Board.Title),
                    Markup.Escape(Short(b.BoardMetaCid, 24))
                );
            }
            AnsiConsole.Write(table);
            AnsiConsole.WriteLine();

            if (boards.Count == 0)
            {
                AnsiConsole.MarkupLine("[grey]No boards found.[/]");
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
            var action = AnsiConsole.Prompt(new SelectionPrompt<string>().Title("Action").AddChoices(actions));

            switch (action)
            {
                case "Open board":
                {
                    if (boards.Count == 0)
                    {
                        AnsiConsole.MarkupLine("[grey]No boards to open.[/]");
                        Pause();
                        break;
                    }

                    var selected = AnsiConsole.Prompt(
                        new SelectionPrompt<BoardItem>()
                            .Title("Select board")
                            .PageSize(12)
                            .MoreChoicesText("[grey](move up and down to reveal more boards)[/]")
                            .AddChoices(boards)
                            .UseConverter(b => $"{Markup.Escape(b.Board.BoardId)}  {Markup.Escape(b.Board.Title)}  [grey]{Markup.Escape(Short(b.BoardMetaCid, 24))}[/]")
                    );

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
            AnsiConsole.MarkupLine("[red]bbs-node not found.[/] Set it in Settings → Client / Backend.");
            Pause();
            return;
        }

        var key = await PromptKeyAsync(keys, ct);
        if (key == null)
        {
            return;
        }

        var boardId = AnsiConsole.Ask<string>("Board ID (e.g. bbs.general)");
        if (string.IsNullOrWhiteSpace(boardId))
        {
            AnsiConsole.MarkupLine("[yellow]Board ID is empty. Canceled.[/]");
            Pause();
            return;
        }
        boardId = boardId.Trim();

        var title = AnsiConsole.Ask<string>("Title");
        if (string.IsNullOrWhiteSpace(title))
        {
            AnsiConsole.MarkupLine("[yellow]Title is empty. Canceled.[/]");
            Pause();
            return;
        }
        title = title.Trim();

        var description = EmptyToNull(AnsiConsole.Ask("Description (optional)", ""));

        AnsiConsole.WriteLine();
        AnsiConsole.MarkupLine($"[grey]BoardID:[/] {Markup.Escape(boardId)}");
        AnsiConsole.MarkupLine($"[grey]Title:[/] {Markup.Escape(title)}");
        if (description != null)
        {
            AnsiConsole.MarkupLine($"[grey]Description:[/] {Markup.Escape(description)}");
        }
        AnsiConsole.MarkupLine($"[grey]Author key:[/] {Markup.Escape(key.Name)}  [grey]{Markup.Escape(Short(key.Pub, 32))}[/]");
        AnsiConsole.WriteLine();

        if (!AnsiConsole.Confirm("Create this board and register it locally (boards.json)?", true))
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
                .StartAsync("Creating board...", async _ => await RunProcessCaptureAsync(bbsNodePath, args, ct));
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
            AnsiConsole.MarkupLine("[red]bbs-node not found.[/] Set it in Settings → Client / Backend.");
            Pause();
            return;
        }

        var boardId = AnsiConsole.Ask<string>("Board ID");
        var boardMetaCid = AnsiConsole.Ask<string>("BoardMeta CID");
        if (string.IsNullOrWhiteSpace(boardId) || string.IsNullOrWhiteSpace(boardMetaCid))
        {
            AnsiConsole.MarkupLine("[yellow]Board ID / CID is empty. Canceled.[/]");
            Pause();
            return;
        }
        boardId = boardId.Trim();
        boardMetaCid = boardMetaCid.Trim();

        if (!AnsiConsole.Confirm("Register this board locally (boards.json)?", true))
        {
            return;
        }

        var args = new List<string>
        {
            "add-board",
            "--board-id", boardId,
            "--board-meta-cid", boardMetaCid,
            "--data-dir", cfg.DataDir,
        };

        try
        {
            _ = await AnsiConsole.Status()
                .Spinner(Spinner.Known.Dots)
                .StartAsync("Registering board...", async _ => await RunProcessCaptureAsync(bbsNodePath, args, ct));
            AnsiConsole.MarkupLine("[green]ok[/]");
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
            AnsiConsole.Write(new Rule($"[bold]Threads[/] [grey]{Markup.Escape(boardId)}[/]").LeftJustified());
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
            table.AddColumn("#");
            table.AddColumn("ThreadID");
            table.AddColumn("Title");
            table.AddColumn("CreatedAt");
            for (var i = 0; i < threads.Count; i++)
            {
                var t = threads[i];
                table.AddRow(
                    (offset + i + 1).ToString(),
                    Markup.Escape(Short(t.ThreadId, 24)),
                    Markup.Escape(t.Thread.Title),
                    Markup.Escape(t.Thread.CreatedAt)
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
                    .Title("Action")
                    .AddChoices(actions)
            );

            switch (action)
            {
                case "Open thread":
                {
                    if (threads.Count == 0)
                    {
                        AnsiConsole.MarkupLine("[grey]No threads in this page.[/]");
                        Pause();
                        break;
                    }

                    var thread = AnsiConsole.Prompt(
                        new SelectionPrompt<ThreadItem>()
                            .Title("Select thread")
                            .PageSize(12)
                            .MoreChoicesText("[grey](move up and down to reveal more threads)[/]")
                            .AddChoices(threads)
                            .UseConverter(t => $"{Markup.Escape(t.Thread.Title)}  [grey]{Markup.Escape(t.ThreadId)}[/]")
                    );

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
            AnsiConsole.Write(new Rule("[bold]Thread[/]").LeftJustified());
            AnsiConsole.MarkupLine($"[grey]Board:[/] {Markup.Escape(tr.ThreadMeta.BoardId)}");
            AnsiConsole.MarkupLine($"[grey]Title:[/] {Markup.Escape(tr.ThreadMeta.Title)}");
            AnsiConsole.MarkupLine($"[grey]ThreadID:[/] {Markup.Escape(tr.ThreadMeta.ThreadId)}");
            AnsiConsole.WriteLine();

            var visiblePosts = tr.Posts.Where(p => !blockedKeys.Contains(p.Post.AuthorPubKey)).ToList();
            var hiddenCount = tr.Posts.Count - visiblePosts.Count;
            if (hiddenCount > 0)
            {
                AnsiConsole.MarkupLine($"[grey]{hiddenCount} post(s) hidden (blocked authors).[/]");
                AnsiConsole.WriteLine();
            }

            for (var i = 0; i < visiblePosts.Count; i++)
            {
                var p = visiblePosts[i];

                var meta = $"[bold]#{i + 1}[/] {Markup.Escape(p.Post.DisplayName)} [grey]{Markup.Escape(p.Post.CreatedAt)}[/]";
                if (!string.IsNullOrWhiteSpace(p.Post.EditedAt))
                {
                    meta += $" [grey](edited {Markup.Escape(p.Post.EditedAt)})[/]";
                }

                var cidLine = $"[grey]CID:[/] {Markup.Escape(p.Cid)}";
                var authorLine = $"[grey]Author:[/] {Markup.Escape(Short(p.Post.AuthorPubKey, 24))}";
                var parentLine = string.IsNullOrWhiteSpace(p.Post.ParentPostCid)
                    ? null
                    : $"[grey]Parent:[/] {Markup.Escape(Short(p.Post.ParentPostCid, 24))}";

                var body = p.Tombstoned
                    ? $"[red][tombstoned][/]\n{Markup.Escape(p.TombstoneReason ?? "")}".TrimEnd()
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

            var action = AnsiConsole.Prompt(new SelectionPrompt<string>().Title("Action").AddChoices(actions));
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

        var displayName = AnsiConsole.Ask("Display name (optional)", "");

        string? parent = null;
        if (visiblePosts.Count > 0 && AnsiConsole.Confirm("Reply to a specific post?", false))
        {
            var selected = AnsiConsole.Prompt(
                new SelectionPrompt<ThreadPostItem>()
                    .Title("Select parent post")
                    .PageSize(12)
                    .MoreChoicesText("[grey](move up and down to reveal more posts)[/]")
                    .AddChoices(visiblePosts)
                    .UseConverter(p => $"{Markup.Escape(p.Post.DisplayName)}  [grey]{Markup.Escape(Short(p.Cid, 24))}[/]")
            );
            parent = selected.Cid;
        }

        var body = ReadMultiline("Body");
        if (string.IsNullOrWhiteSpace(body))
        {
            AnsiConsole.MarkupLine("[yellow]Body is empty. Canceled.[/]");
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

        var title = AnsiConsole.Ask<string>("Title");
        if (string.IsNullOrWhiteSpace(title))
        {
            AnsiConsole.MarkupLine("[yellow]Title is empty. Canceled.[/]");
            Pause();
            return;
        }

        var displayName = AnsiConsole.Ask("Display name (optional)", "");

        var body = ReadMultiline("Body");
        if (string.IsNullOrWhiteSpace(body))
        {
            AnsiConsole.MarkupLine("[yellow]Body is empty. Canceled.[/]");
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
            AnsiConsole.MarkupLine("[grey]No posts to edit.[/]");
            Pause();
            return;
        }

        var key = await PromptKeyAsync(keys, ct);
        if (key == null)
        {
            return;
        }

        var selected = AnsiConsole.Prompt(
            new SelectionPrompt<ThreadPostItem>()
                .Title("Select post to edit")
                .PageSize(12)
                .MoreChoicesText("[grey](move up and down to reveal more posts)[/]")
                .AddChoices(visiblePosts)
                .UseConverter(p => $"{Markup.Escape(p.Post.DisplayName)}  [grey]{Markup.Escape(Short(p.Cid, 24))}[/]")
        );

        var displayName = EmptyToNull(AnsiConsole.Ask("Display name (optional, blank = keep)", ""));

        var body = ReadMultiline("Body");
        if (string.IsNullOrWhiteSpace(body))
        {
            AnsiConsole.MarkupLine("[yellow]Body is empty. Canceled.[/]");
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
            AnsiConsole.MarkupLine("[grey]No posts to tombstone.[/]");
            Pause();
            return;
        }

        var key = await PromptKeyAsync(keys, ct);
        if (key == null)
        {
            return;
        }

        var selected = AnsiConsole.Prompt(
            new SelectionPrompt<ThreadPostItem>()
                .Title("Select post to tombstone")
                .PageSize(12)
                .MoreChoicesText("[grey](move up and down to reveal more posts)[/]")
                .AddChoices(visiblePosts)
                .UseConverter(p => $"{Markup.Escape(p.Post.DisplayName)}  [grey]{Markup.Escape(Short(p.Cid, 24))}[/]")
        );

        var reason = EmptyToNull(AnsiConsole.Ask("Reason (optional)", ""));

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

    private static async Task SearchPostsAsync(BbsApiClient api, KeyStore keys, BlockedStore blocked, CancellationToken ct)
    {
        var q = AnsiConsole.Ask<string>("Query (q)");
        var boardId = EmptyToNull(AnsiConsole.Ask("Board ID (optional)", ""));
        var author = EmptyToNull(AnsiConsole.Ask("Author pubKey (optional)", ""));
        var since = EmptyToNull(AnsiConsole.Ask("Since (optional, RFC3339)", ""));
        var until = EmptyToNull(AnsiConsole.Ask("Until (optional, RFC3339)", ""));

        var offset = 0;
        while (true)
        {
            List<SearchPostResult> results;
            try
            {
                results = await api.SearchPostsAsync(q, boardId, author, since, until, DefaultPageSize, offset, ct);
            }
            catch (Exception ex)
            {
                ShowError(ex);
                Pause();
                return;
            }

            AnsiConsole.Clear();
            AnsiConsole.Write(new Rule("[bold]Search posts[/]").LeftJustified());
            AnsiConsole.MarkupLine($"[grey]q:[/] {Markup.Escape(q)}");
            if (boardId != null) AnsiConsole.MarkupLine($"[grey]boardId:[/] {Markup.Escape(boardId)}");
            if (author != null) AnsiConsole.MarkupLine($"[grey]author:[/] {Markup.Escape(Short(author, 24))}");
            AnsiConsole.WriteLine();

            var table = new Table().Border(TableBorder.Rounded);
            table.AddColumn("#");
            table.AddColumn("Board");
            table.AddColumn("Thread");
            table.AddColumn("Post");
            table.AddColumn("Name");
            table.AddColumn("CreatedAt");
            for (var i = 0; i < results.Count; i++)
            {
                var r = results[i];
                table.AddRow(
                    (offset + i + 1).ToString(),
                    Markup.Escape(r.BoardId),
                    Markup.Escape(Short(r.ThreadId, 16)),
                    Markup.Escape(Short(r.PostCid, 16)),
                    Markup.Escape(r.DisplayName),
                    Markup.Escape(r.CreatedAt)
                );
            }
            AnsiConsole.Write(table);
            AnsiConsole.WriteLine();

            var actions = new List<string>
            {
                "Open thread",
                "Block author",
                "New search",
            };
            if (offset > 0)
            {
                actions.Add("Prev page");
            }
            if (results.Count == DefaultPageSize)
            {
                actions.Add("Next page");
            }
            actions.Add("Back");

            var action = AnsiConsole.Prompt(new SelectionPrompt<string>().Title("Action").AddChoices(actions));
            switch (action)
            {
                case "Open thread":
                {
                    if (results.Count == 0)
                    {
                        AnsiConsole.MarkupLine("[grey]No results in this page.[/]");
                        Pause();
                        break;
                    }

                    var selected = AnsiConsole.Prompt(
                        new SelectionPrompt<SearchPostResult>()
                            .Title("Select result")
                            .PageSize(12)
                            .MoreChoicesText("[grey](move up and down to reveal more results)[/]")
                            .AddChoices(results)
                            .UseConverter(r =>
                                $"{Markup.Escape(r.DisplayName)}  [grey]{Markup.Escape(r.BoardId)} {Markup.Escape(Short(r.ThreadId, 16))} {Markup.Escape(Short(r.PostCid, 16))}[/]"
                            )
                    );

                    await BrowseThreadAsync(api, keys, blocked, selected.ThreadId, ct);
                    break;
                }
                case "Block author":
                {
                    if (results.Count == 0)
                    {
                        AnsiConsole.MarkupLine("[grey]No results in this page.[/]");
                        Pause();
                        break;
                    }

                    var selected = AnsiConsole.Prompt(
                        new SelectionPrompt<SearchPostResult>()
                            .Title("Select author to block")
                            .PageSize(12)
                            .MoreChoicesText("[grey](move up and down to reveal more results)[/]")
                            .AddChoices(results)
                            .UseConverter(r => $"{Markup.Escape(r.DisplayName)}  [grey]{Markup.Escape(Short(r.AuthorPubKey, 24))}[/]")
                    );

                    await blocked.AddAsync(selected.AuthorPubKey, ct);
                    AnsiConsole.MarkupLine($"[green]ok[/] blocked {Markup.Escape(Short(selected.AuthorPubKey, 24))}");
                    Pause();
                    break;
                }
                case "New search":
                    return;
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

    private static async Task KeysMenuAsync(KeyStore keys, CancellationToken ct)
    {
        while (true)
        {
            var list = await keys.LoadAsync(ct);
            list = list.OrderBy(k => k.Name, StringComparer.OrdinalIgnoreCase).ToList();

            AnsiConsole.Clear();
            AnsiConsole.Write(new Rule("[bold]Keys[/]").LeftJustified());

            var table = new Table().Border(TableBorder.Rounded);
            table.AddColumn("Name");
            table.AddColumn("Public key");
            foreach (var k in list)
            {
                table.AddRow(Markup.Escape(k.Name), Markup.Escape(Short(k.Pub, 48)));
            }
            AnsiConsole.Write(table);
            AnsiConsole.WriteLine();

            var action = AnsiConsole.Prompt(
                new SelectionPrompt<string>()
                    .Title("Action")
                    .AddChoices("Generate", "Delete", "Back")
            );

            switch (action)
            {
                case "Generate":
                {
                    var name = AnsiConsole.Ask("Key name", "default");
                    try
                    {
                        var created = await keys.GenerateAsync(name, ct);
                        AnsiConsole.MarkupLine($"[green]ok[/] {Markup.Escape(created.Name)} {Markup.Escape(created.Pub)}");
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
                        AnsiConsole.MarkupLine("[grey]No keys to delete.[/]");
                        Pause();
                        break;
                    }
                    var selected = AnsiConsole.Prompt(
                        new SelectionPrompt<KeyEntry>()
                            .Title("Select key to delete")
                            .PageSize(12)
                            .AddChoices(list)
                            .UseConverter(k => $"{Markup.Escape(k.Name)}  [grey]{Markup.Escape(Short(k.Pub, 24))}[/]")
                    );
                    if (!AnsiConsole.Confirm($"Delete '{selected.Name}'?", false))
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
            AnsiConsole.Write(new Rule("[bold]Blocked authors[/]").LeftJustified());

            var table = new Table().Border(TableBorder.Rounded);
            table.AddColumn("Public key");
            foreach (var k in list)
            {
                table.AddRow(Markup.Escape(k));
            }
            AnsiConsole.Write(table);
            AnsiConsole.WriteLine();

            var action = AnsiConsole.Prompt(
                new SelectionPrompt<string>()
                    .Title("Action")
                    .AddChoices("Add", "Remove", "Back")
            );

            switch (action)
            {
                case "Add":
                {
                    var pub = AnsiConsole.Ask<string>("Public key to block");
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
                        AnsiConsole.MarkupLine("[grey]No blocked authors to remove.[/]");
                        Pause();
                        break;
                    }
                    var selected = AnsiConsole.Prompt(
                        new SelectionPrompt<string>()
                            .Title("Select key to remove")
                            .PageSize(12)
                            .AddChoices(list)
                            .UseConverter(Markup.Escape)
                    );
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
            AnsiConsole.MarkupLine("[grey]No posts to select.[/]");
            Pause();
            return;
        }

        var selected = AnsiConsole.Prompt(
            new SelectionPrompt<ThreadPostItem>()
                .Title("Select post")
                .PageSize(12)
                .MoreChoicesText("[grey](move up and down to reveal more posts)[/]")
                .AddChoices(visiblePosts)
                .UseConverter(p => $"{Markup.Escape(p.Post.DisplayName)}  [grey]{Markup.Escape(Short(p.Post.AuthorPubKey, 24))}[/]")
        );

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
            AnsiConsole.MarkupLine("[yellow]No keys found.[/]");
            if (!AnsiConsole.Confirm("Generate a key now?", true))
            {
                return null;
            }
            var name = AnsiConsole.Ask("Key name", "default");
            try
            {
                return await keys.GenerateAsync(name, ct);
            }
            catch (Exception ex)
            {
                ShowError(ex);
                Pause();
                return null;
            }
        }

        return AnsiConsole.Prompt(
            new SelectionPrompt<KeyEntry>()
                .Title("Select key")
                .PageSize(12)
                .MoreChoicesText("[grey](move up and down to reveal more keys)[/]")
                .AddChoices(list)
                .UseConverter(k => $"{Markup.Escape(k.Name)}  [grey]{Markup.Escape(Short(k.Pub, 32))}[/]")
        );
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
            AnsiConsole.Write(new Rule("[bold]Settings[/]").LeftJustified());
            AnsiConsole.MarkupLine($"[grey]Config:[/] {Markup.Escape(configStore.ConfigPath)}");
            AnsiConsole.MarkupLine($"[grey]Backend:[/] {Markup.Escape(cfg.BackendBaseUrl)} [{(healthy ? "green" : "red")}]{(healthy ? "up" : "down")}[/]");
            AnsiConsole.MarkupLine($"[grey]Backend role (managed):[/] {Markup.Escape(cfg.BackendRole)}");
            AnsiConsole.MarkupLine($"[grey]Auto-start backend:[/] {cfg.StartBackend}");
            AnsiConsole.MarkupLine($"[grey]bbs-node path:[/] {Markup.Escape(cfg.BbsNodePath ?? "<auto>")}");
            AnsiConsole.MarkupLine($"[grey]Data dir:[/] {Markup.Escape(cfg.DataDir)}");
            AnsiConsole.MarkupLine($"[grey]Flex-IPFS base URL:[/] {Markup.Escape(cfg.FlexIpfsBaseUrl)}");
            AnsiConsole.MarkupLine($"[grey]Flex-IPFS base dir:[/] {Markup.Escape(cfg.FlexIpfsBaseDir ?? "<auto>")}");
            AnsiConsole.MarkupLine($"[grey]Flex-IPFS GW endpoint override:[/] {Markup.Escape(cfg.FlexIpfsGwEndpoint ?? "<none>")}");
            AnsiConsole.MarkupLine($"[grey]Autostart flex-ipfs:[/] {cfg.AutostartFlexIpfs}");
            AnsiConsole.WriteLine();

            var choice = AnsiConsole.Prompt(
                new SelectionPrompt<string>()
                    .Title("Settings menu")
                    .AddChoices("Client / Backend", "Flexible-IPFS", "kadrtt.properties", "Backend control", "Back")
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

    private static ClientConfig PromptClientBackendSettings(ClientConfig cfg)
    {
        var backend = AnsiConsole.Ask("Backend base URL", cfg.BackendBaseUrl);
        var roles = new[] { cfg.BackendRole, "client", "indexer", "archiver", "full" }
            .Distinct(StringComparer.OrdinalIgnoreCase)
            .ToList();
        var backendRole = AnsiConsole.Prompt(
            new SelectionPrompt<string>()
                .Title("Backend role (managed)")
                .AddChoices(roles)
        );
        var dataDir = AnsiConsole.Ask("Data dir", cfg.DataDir);
        var startBackend = AnsiConsole.Confirm("Auto-start backend (manage local bbs-node)?", cfg.StartBackend);

        var currentPath = cfg.BbsNodePath ?? "<auto>";
        var bbsNodePathInput = AnsiConsole.Prompt(
            new TextPrompt<string>($"bbs-node path (blank = auto) [grey](current: {EscapePrompt(currentPath)})[/]")
                .AllowEmpty()
        );
        var bbsNodePath = string.IsNullOrWhiteSpace(bbsNodePathInput) ? null : bbsNodePathInput.Trim();

        return cfg with
        {
            BackendBaseUrl = backend,
            BackendRole = backendRole,
            DataDir = dataDir,
            StartBackend = startBackend,
            BbsNodePath = bbsNodePath,
        };
    }

    private static ClientConfig PromptFlexIpfsSettings(ClientConfig cfg)
    {
        var flexBaseUrl = AnsiConsole.Ask("Flexible-IPFS HTTP API base URL", cfg.FlexIpfsBaseUrl);
        var autostartFlexIpfs = AnsiConsole.Confirm("Autostart Flexible-IPFS (when managed by bbs-node)?", cfg.AutostartFlexIpfs);
        var flexIpfsMdns = AnsiConsole.Confirm("Use mDNS on LAN to discover flex-ipfs gw endpoint?", cfg.FlexIpfsMdns);

        var currentBaseDir = cfg.FlexIpfsBaseDir ?? "<auto>";
        var baseDirInput = AnsiConsole.Prompt(
            new TextPrompt<string>($"flexible-ipfs-base dir (blank = auto) [grey](current: {EscapePrompt(currentBaseDir)})[/]")
                .AllowEmpty()
        );
        var baseDir = string.IsNullOrWhiteSpace(baseDirInput) ? null : baseDirInput.Trim();

        var currentGw = cfg.FlexIpfsGwEndpoint ?? "<none>";
        var gwInput = AnsiConsole.Prompt(
            new TextPrompt<string>($"ipfs.endpoint override (blank = none) [grey](current: {EscapePrompt(currentGw)})[/]")
                .AllowEmpty()
        );
        var gw = string.IsNullOrWhiteSpace(gwInput) ? null : gwInput.Trim();

        return cfg with
        {
            FlexIpfsBaseUrl = flexBaseUrl,
            AutostartFlexIpfs = autostartFlexIpfs,
            FlexIpfsMdns = flexIpfsMdns,
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
            AnsiConsole.MarkupLine($"[red]Invalid backend URL:[/] {Markup.Escape(backendErr)}");
            Pause();
            return oldCfg;
        }
        if (!TryValidateHttpUrl(newCfg.FlexIpfsBaseUrl, out var flexErr))
        {
            AnsiConsole.MarkupLine($"[red]Invalid Flexible-IPFS base URL:[/] {Markup.Escape(flexErr)}");
            Pause();
            return oldCfg;
        }
        if (string.IsNullOrWhiteSpace(newCfg.DataDir))
        {
            AnsiConsole.MarkupLine("[red]Data dir is required.[/]");
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
            AnsiConsole.MarkupLine("[green]ok[/] saved");
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
            !string.Equals(oldCfg.BackendRole, newCfg.BackendRole, StringComparison.Ordinal) ||
            !string.Equals(oldCfg.DataDir, newCfg.DataDir, StringComparison.Ordinal) ||
            !string.Equals(oldCfg.FlexIpfsBaseUrl, newCfg.FlexIpfsBaseUrl, StringComparison.Ordinal) ||
            !string.Equals(oldCfg.FlexIpfsBaseDir, newCfg.FlexIpfsBaseDir, StringComparison.Ordinal) ||
            !string.Equals(oldCfg.FlexIpfsGwEndpoint, newCfg.FlexIpfsGwEndpoint, StringComparison.Ordinal) ||
            oldCfg.FlexIpfsMdns != newCfg.FlexIpfsMdns ||
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
                    .StartAsync("Starting backend...", async _ =>
                    {
                        await launcher.EnsureRunningAsync(newCfg.BackendBaseUrl, newCfg.StartBackend, bbsNodePath, BbsNodeArgsBuilder.Build(newCfg), ct);
                    });
                AnsiConsole.MarkupLine("[green]ok[/] started");
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
            AnsiConsole.Write(new Rule("[bold]Backend control[/]").LeftJustified());
            AnsiConsole.MarkupLine($"[grey]Backend:[/] {Markup.Escape(cfg.BackendBaseUrl)} [{(healthy ? "green" : "red")}]{(healthy ? "up" : "down")}[/]");
            AnsiConsole.MarkupLine($"[grey]Managed by this client:[/] {managed} {(launcher.ManagedPid is int pid ? $"(pid={pid})" : "")}".TrimEnd());
            AnsiConsole.MarkupLine($"[grey]Backend role (managed):[/] {Markup.Escape(cfg.BackendRole)}");
            AnsiConsole.MarkupLine($"[grey]Auto-start backend:[/] {cfg.StartBackend}");
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

            var action = AnsiConsole.Prompt(new SelectionPrompt<string>().Title("Action").AddChoices(actions));
            switch (action)
            {
                case "Start":
                    try
                    {
                        var bbsNodePath = cfg.BbsNodePath ?? BbsNodePathResolver.Resolve();
                        await AnsiConsole.Status()
                            .Spinner(Spinner.Known.Dots)
                            .StartAsync("Starting backend...", async _ =>
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
            AnsiConsole.MarkupLine("[yellow]Auto-start is disabled; restart skipped.[/]");
            return;
        }

        var bbsNodePath = cfg.BbsNodePath ?? BbsNodePathResolver.Resolve();
        var bbsNodeArgs = BbsNodeArgsBuilder.Build(cfg);

        try
        {
            var restarted = await AnsiConsole.Status()
                .Spinner(Spinner.Known.Dots)
                .StartAsync("Restarting backend...", async _ =>
                {
                    return await launcher.RestartManagedAsync(cfg.BackendBaseUrl, cfg.StartBackend, bbsNodePath, bbsNodeArgs, ct);
                });
            if (restarted)
            {
                AnsiConsole.MarkupLine("[green]ok[/] restarted");
                return;
            }

            if (!await BackendLauncher.IsHealthyAsync(cfg.BackendBaseUrl, ct))
            {
                await AnsiConsole.Status()
                    .Spinner(Spinner.Known.Dots)
                    .StartAsync("Starting backend...", async _ =>
                    {
                        await launcher.EnsureRunningAsync(cfg.BackendBaseUrl, cfg.StartBackend, bbsNodePath, bbsNodeArgs, ct);
                    });
                AnsiConsole.MarkupLine("[green]ok[/] started");
                return;
            }

            AnsiConsole.MarkupLine("[yellow]Backend is running but not managed by this client; please restart it manually to apply settings.[/]");
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
            AnsiConsole.MarkupLine("[red]flexible-ipfs-base dir not found. Set it in Flexible-IPFS settings.[/]");
            Pause();
            return false;
        }

        var propsPath = Path.Combine(baseDir, "kadrtt.properties");
        if (!File.Exists(propsPath))
        {
            AnsiConsole.MarkupLine($"[red]kadrtt.properties not found:[/] {Markup.Escape(propsPath)}");
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
                AnsiConsole.MarkupLine("[yellow]Note:[/] ipfs.endpoint override is set; it will be applied on autostart/restart.");
            }
            AnsiConsole.WriteLine();

            var table = new Table().Border(TableBorder.Rounded);
            table.AddColumn("Key");
            table.AddColumn("Value");
            foreach (var kv in entries.OrderBy(e => e.Key, StringComparer.OrdinalIgnoreCase))
            {
                table.AddRow(Markup.Escape(kv.Key), Markup.Escape(Short(kv.Value, 60)));
            }
            AnsiConsole.Write(table);
            AnsiConsole.WriteLine();

            var action = AnsiConsole.Prompt(
                new SelectionPrompt<string>()
                    .Title("Action")
                    .AddChoices("Edit", "Add", "Remove", "Back")
            );

            switch (action)
            {
                case "Edit":
                {
                    if (entries.Count == 0)
                    {
                        AnsiConsole.MarkupLine("[grey]No properties found.[/]");
                        Pause();
                        break;
                    }
                    var key = AnsiConsole.Prompt(
                        new SelectionPrompt<string>()
                            .Title("Select key")
                            .PageSize(12)
                            .MoreChoicesText("[grey](move up and down to reveal more)[/]")
                            .AddChoices(entries.Keys.Order(StringComparer.OrdinalIgnoreCase))
                            .UseConverter(Markup.Escape)
                    );
                    var current = entries.GetValueOrDefault(key, "");
                    var value = AnsiConsole.Prompt(
                        new TextPrompt<string>($"Value for '{EscapePrompt(key)}' [grey](current: {EscapePrompt(Short(current, 60))})[/]")
                            .AllowEmpty()
                    );
                    if (value.Contains('\n') || value.Contains('\r'))
                    {
                        AnsiConsole.MarkupLine("[red]Value must be a single line.[/]");
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
                    var key = AnsiConsole.Ask<string>("Key");
                    key = key.Trim();
                    if (string.IsNullOrWhiteSpace(key) || key.Contains('=') || key.Contains(':') || key.Contains('\n') || key.Contains('\r'))
                    {
                        AnsiConsole.MarkupLine("[red]Invalid key.[/]");
                        Pause();
                        break;
                    }
                    var value = AnsiConsole.Prompt(new TextPrompt<string>("Value").AllowEmpty());
                    if (value.Contains('\n') || value.Contains('\r'))
                    {
                        AnsiConsole.MarkupLine("[red]Value must be a single line.[/]");
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
                        AnsiConsole.MarkupLine("[grey]No properties to remove.[/]");
                        Pause();
                        break;
                    }
                    var key = AnsiConsole.Prompt(
                        new SelectionPrompt<string>()
                            .Title("Select key to remove")
                            .PageSize(12)
                            .MoreChoicesText("[grey](move up and down to reveal more)[/]")
                            .AddChoices(entries.Keys.Order(StringComparer.OrdinalIgnoreCase))
                            .UseConverter(Markup.Escape)
                    );
                    if (!AnsiConsole.Confirm($"Remove '{key}'?", false))
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
            error = "not a valid absolute URL";
            return false;
        }
        if (u.Scheme is not ("http" or "https"))
        {
            error = "scheme must be http or https";
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
        AnsiConsole.MarkupLine($"[grey]{Markup.Escape(label)}[/] (finish with a single '.' line):");
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

    private static string? EmptyToNull(string value)
    {
        return string.IsNullOrWhiteSpace(value) ? null : value.Trim();
    }

    private static void ShowError(Exception ex)
    {
        AnsiConsole.MarkupLine($"[red]Error:[/] {Markup.Escape(ex.Message)}");
        AppLog.Error(ex.Message, ex);
    }

    private static void Pause()
    {
        AnsiConsole.MarkupLine("[grey]Press Enter to continue...[/]");
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

    private sealed record Choice<T>(string Label, T? Value) where T : class;
}
