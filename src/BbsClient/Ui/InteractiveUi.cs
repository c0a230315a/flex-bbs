using BbsClient.Api;
using BbsClient.Storage;
using Spectre.Console;

namespace BbsClient.Ui;

public static class InteractiveUi
{
    private const int DefaultPageSize = 50;

    public static async Task<int> RunAsync(
        BbsApiClient api,
        KeyStore keys,
        BlockedStore blocked,
        string backendBaseUrl,
        string dataDir,
        CancellationToken ct
    )
    {
        if (Console.IsInputRedirected || Console.IsOutputRedirected)
        {
            Console.Error.WriteLine("ui requires an interactive terminal (no redirection).");
            return 2;
        }

        while (true)
        {
            AnsiConsole.Clear();
            AnsiConsole.Write(new Rule("[bold]Flex BBS Client[/]").LeftJustified());
            AnsiConsole.MarkupLine($"[grey]Backend:[/] {Markup.Escape(backendBaseUrl)}");
            AnsiConsole.MarkupLine($"[grey]Data dir:[/] {Markup.Escape(dataDir)}");
            AnsiConsole.WriteLine();

            var choice = AnsiConsole.Prompt(
                new SelectionPrompt<string>()
                    .Title("Main menu")
                    .AddChoices("Browse boards", "Search posts", "Keys", "Blocked", "Quit")
            );

            try
            {
                switch (choice)
                {
                    case "Browse boards":
                        await BrowseBoardsAsync(api, keys, blocked, ct);
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

    private static async Task BrowseBoardsAsync(BbsApiClient api, KeyStore keys, BlockedStore blocked, CancellationToken ct)
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
                .OrderBy(b => b.Board.BoardID, StringComparer.OrdinalIgnoreCase)
                .ToList();

            var table = new Table().Border(TableBorder.Rounded);
            table.AddColumn("BoardID");
            table.AddColumn("Title");
            foreach (var b in boards)
            {
                table.AddRow(
                    Markup.Escape(b.Board.BoardID),
                    Markup.Escape(b.Board.Title)
                );
            }
            AnsiConsole.Write(table);
            AnsiConsole.WriteLine();

            if (boards.Count == 0)
            {
                AnsiConsole.MarkupLine("[grey]No boards found.[/]");
                Pause();
                return;
            }

            var choices = new List<Choice<BoardItem>>
            {
                new("Back", null),
            };
            choices.AddRange(boards.Select(b => new Choice<BoardItem>($"{b.Board.BoardID}  {b.Board.Title}", b)));

            var selected = AnsiConsole.Prompt(
                new SelectionPrompt<Choice<BoardItem>>()
                    .Title("Select board")
                    .PageSize(12)
                    .MoreChoicesText("[grey](move up and down to reveal more boards)[/]")
                    .AddChoices(choices)
                    .UseConverter(c => Markup.Escape(c.Label))
            );

            if (selected.Value == null)
            {
                return;
            }

            await BrowseThreadsAsync(api, keys, blocked, selected.Value.Board.BoardID, selected.Value.Board.Title, ct);
        }
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
                    Markup.Escape(Short(t.ThreadID, 24)),
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
                            .UseConverter(t => $"{Markup.Escape(t.Thread.Title)}  [grey]{Markup.Escape(t.ThreadID)}[/]")
                    );

                    await BrowseThreadAsync(api, keys, blocked, thread.ThreadID, ct);
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
            AnsiConsole.MarkupLine($"[grey]Board:[/] {Markup.Escape(tr.ThreadMeta.BoardID)}");
            AnsiConsole.MarkupLine($"[grey]Title:[/] {Markup.Escape(tr.ThreadMeta.Title)}");
            AnsiConsole.MarkupLine($"[grey]ThreadID:[/] {Markup.Escape(tr.ThreadMeta.ThreadID)}");
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

                var cidLine = $"[grey]CID:[/] {Markup.Escape(p.CID)}";
                var authorLine = $"[grey]Author:[/] {Markup.Escape(Short(p.Post.AuthorPubKey, 24))}";
                var parentLine = string.IsNullOrWhiteSpace(p.Post.ParentPostCID)
                    ? null
                    : $"[grey]Parent:[/] {Markup.Escape(Short(p.Post.ParentPostCID, 24))}";

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
                    .UseConverter(p => $"{Markup.Escape(p.Post.DisplayName)}  [grey]{Markup.Escape(Short(p.CID, 24))}[/]")
            );
            parent = selected.CID;
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
                ThreadID = threadId,
                ParentPostCID = parent,
                DisplayName = displayName,
                Body = new PostBody { Format = "markdown", Content = body },
                AuthorPrivKey = key.Priv,
            }, ct);

            AnsiConsole.MarkupLine($"[green]ok[/] postCid={Markup.Escape(resp.PostCID)}");
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
                BoardID = boardId,
                Title = title,
                DisplayName = displayName,
                Body = new PostBody { Format = "markdown", Content = body },
                AuthorPrivKey = key.Priv,
            }, ct);

            AnsiConsole.MarkupLine($"[green]ok[/] threadId={Markup.Escape(resp.ThreadID)}");
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
                .UseConverter(p => $"{Markup.Escape(p.Post.DisplayName)}  [grey]{Markup.Escape(Short(p.CID, 24))}[/]")
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
            var resp = await api.EditPostAsync(selected.CID, new EditPostRequest
            {
                Body = new PostBody { Format = "markdown", Content = body },
                DisplayName = displayName,
                AuthorPrivKey = key.Priv,
            }, ct);

            AnsiConsole.MarkupLine($"[green]ok[/] newPostCid={Markup.Escape(resp.NewPostCID)}");
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
                .UseConverter(p => $"{Markup.Escape(p.Post.DisplayName)}  [grey]{Markup.Escape(Short(p.CID, 24))}[/]")
        );

        var reason = EmptyToNull(AnsiConsole.Ask("Reason (optional)", ""));

        try
        {
            var resp = await api.TombstonePostAsync(selected.CID, new TombstonePostRequest
            {
                Reason = reason,
                AuthorPrivKey = key.Priv,
            }, ct);

            AnsiConsole.MarkupLine($"[green]ok[/] tombstoned {Markup.Escape(resp.TargetPostCID)}");
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
                    Markup.Escape(r.BoardID),
                    Markup.Escape(Short(r.ThreadID, 16)),
                    Markup.Escape(Short(r.PostCID, 16)),
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
                                $"{Markup.Escape(r.DisplayName)}  [grey]{Markup.Escape(r.BoardID)} {Markup.Escape(Short(r.ThreadID, 16))} {Markup.Escape(Short(r.PostCID, 16))}[/]"
                            )
                    );

                    await BrowseThreadAsync(api, keys, blocked, selected.ThreadID, ct);
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
    }

    private static void Pause()
    {
        AnsiConsole.MarkupLine("[grey]Press Enter to continue...[/]");
        Console.ReadLine();
    }

    private sealed record Choice<T>(string Label, T? Value) where T : class;
}
