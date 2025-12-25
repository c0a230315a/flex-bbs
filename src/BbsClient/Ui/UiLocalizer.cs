using System.Globalization;

namespace BbsClient.Ui;

public sealed class UiLocalizer
{
    private readonly IReadOnlyDictionary<string, string> _map;

    private UiLocalizer(string language, IReadOnlyDictionary<string, string> map)
    {
        Language = language;
        _map = map;
    }

    public string Language { get; }

    public static UiLocalizer Create(string? languageSetting)
    {
        var normalized = NormalizeLanguage(languageSetting);
        var language = normalized == "auto" ? ResolveAutoLanguage() : normalized;
        return language == "ja" ? new UiLocalizer("ja", Ja) : new UiLocalizer("en", En);
    }

    public static string NormalizeLanguage(string? language)
    {
        language = string.IsNullOrWhiteSpace(language) ? "auto" : language.Trim().ToLowerInvariant();
        return language switch
        {
            "auto" => language,
            "en" => language,
            "ja" => language,
            "jp" => "ja",
            _ => "auto",
        };
    }

    private static string ResolveAutoLanguage()
    {
        try
        {
            return string.Equals(CultureInfo.CurrentUICulture.TwoLetterISOLanguageName, "ja", StringComparison.OrdinalIgnoreCase)
                ? "ja"
                : "en";
        }
        catch
        {
            return "en";
        }
    }

    public string L(string text)
    {
        if (string.IsNullOrEmpty(text))
        {
            return text;
        }
        return _map.TryGetValue(text, out var translated) ? translated : text;
    }

    public string F(string format, params object[] args)
    {
        return string.Format(CultureInfo.CurrentCulture, L(format), args);
    }

    private static readonly IReadOnlyDictionary<string, string> En = new Dictionary<string, string>
    {
    };

    private static readonly IReadOnlyDictionary<string, string> Ja = new Dictionary<string, string>
    {
        ["auto"] = "自動",
        ["en"] = "English",
        ["ja"] = "日本語",

        ["Flex BBS Client"] = "Flex BBS クライアント",
        ["ui requires an interactive terminal (no redirection)."] = "ui は対話可能なターミナルが必要です（リダイレクト不可）。",
        ["Ensuring backend is running..."] = "バックエンドの起動状態を確認しています...",

        ["Main menu"] = "メインメニュー",
        ["Browse boards"] = "板一覧",
        ["Search"] = "検索",
        ["Search posts"] = "投稿検索",
        ["Keys"] = "鍵",
        ["Blocked"] = "ブロック",
        ["Settings"] = "設定",
        ["Quit"] = "終了",

        ["Boards"] = "板",
        ["Threads"] = "スレッド",
        ["Thread"] = "スレッド",

        ["Action"] = "操作",
        ["Back"] = "戻る",
        ["Refresh"] = "更新",
        ["Prev page"] = "前のページ",
        ["Next page"] = "次のページ",
        ["Add"] = "追加",
        ["Remove"] = "削除",
        ["Start"] = "開始",
        ["Restart"] = "再起動",
        ["Stop"] = "停止",

        ["Open board"] = "板を開く",
        ["Create board"] = "板を作成",
        ["Add board"] = "板を追加",

        ["Open thread"] = "スレッドを開く",
        ["Open post"] = "投稿を開く",
        ["Create thread"] = "スレッドを作成",

        ["Reply"] = "返信",
        ["Edit post"] = "投稿を編集",
        ["Tombstone post"] = "投稿を削除（墓石）",
        ["Block author"] = "投稿者をブロック",
        ["New search"] = "新しい検索",

        ["No boards found."] = "板が見つかりません。",
        ["No boards to open."] = "開ける板がありません。",
        ["No threads in this page."] = "このページにはスレッドがありません。",
        ["No results in this page."] = "このページには結果がありません。",
        ["No results to select."] = "選択できる結果がありません。",
        ["No posts to select."] = "選択できる投稿がありません。",
        ["No posts to edit."] = "編集できる投稿がありません。",
        ["No posts to tombstone."] = "削除（墓石）できる投稿がありません。",
        ["No keys found."] = "鍵がありません。",
        ["No keys to delete."] = "削除できる鍵がありません。",
        ["No blocked authors to remove."] = "解除できるブロックがありません。",

        ["Select board"] = "板を選択",
        ["Select thread"] = "スレッドを選択",
        ["Select result"] = "結果を選択",
        ["Select parent post"] = "親投稿を選択",
        ["Select post"] = "投稿を選択",
        ["Select post to edit"] = "編集する投稿を選択",
        ["Select post to tombstone"] = "削除（墓石）する投稿を選択",
        ["Select author to block"] = "ブロックする投稿者を選択",
        ["Select key"] = "鍵を選択",
        ["Select key to delete"] = "削除する鍵を選択",
        ["Select key to remove"] = "削除するキーを選択",

        ["(move up and down to reveal more boards)"] = "（上下キーで移動して、さらに表示します）",
        ["(move up and down to reveal more threads)"] = "（上下キーで移動して、さらに表示します）",
        ["(move up and down to reveal more posts)"] = "（上下キーで移動して、さらに表示します）",
        ["(move up and down to reveal more results)"] = "（上下キーで移動して、さらに表示します）",
        ["(move up and down to reveal more keys)"] = "（上下キーで移動して、さらに表示します）",
        ["(move up and down to reveal more)"] = "（上下キーで移動して、さらに表示します）",

        ["Backend"] = "バックエンド",
        ["Backend listen (managed)"] = "バックエンド listen（管理対象）",
        ["Backend role (managed)"] = "バックエンド role（管理対象）",
        ["Backend base URL"] = "バックエンド base URL",
        ["Backend listen address (host:port, blank = derived) [grey](current: {0})[/]"] = "バックエンド listen アドレス（host:port、空欄=自動） [grey](現在: {0})[/]",
        ["Auto-start backend"] = "バックエンド自動起動",
        ["Auto-start backend (manage local bbs-node)?"] = "バックエンドを自動起動して管理しますか（ローカル bbs-node）？",
        ["bbs-node path"] = "bbs-node パス",
        ["Data dir"] = "データディレクトリ",
        ["Config"] = "設定ファイル",
        ["Managed by this client"] = "このクライアントが管理",
        ["Invalid backend listen address"] = "バックエンド listen アドレスが不正です",
        ["Backend port mismatch"] = "バックエンドのポート不一致",
        ["host:port is empty"] = "host:port が空です",
        ["IPv6 must be in [::1]:port form"] = "IPv6 は [::1]:port 形式で指定してください",
        ["must be in host:port form"] = "host:port 形式で指定してください",
        ["not a valid host:port"] = "host:port として不正です",
        ["host is empty"] = "host が空です",
        ["port must be 1-65535"] = "port は 1〜65535 で指定してください",

        ["Flex-IPFS base URL"] = "Flex-IPFS base URL",
        ["Flex-IPFS base dir"] = "Flex-IPFS base dir",
        ["Flex-IPFS GW endpoint override"] = "Flex-IPFS GW endpoint 上書き",
        ["Flex-IPFS mDNS"] = "Flex-IPFS mDNS",
        ["Flex-IPFS mDNS timeout"] = "Flex-IPFS mDNS タイムアウト",
        ["Autostart flex-ipfs"] = "flex-ipfs 自動起動",

        ["<auto>"] = "<自動>",
        ["<none>"] = "<なし>",
        ["<empty>"] = "<空>",

        ["Board ID"] = "Board ID",
        ["Board ID (e.g. bbs.general)"] = "Board ID（例: bbs.general）",
        ["BoardID"] = "BoardID",
        ["BoardMeta CID"] = "BoardMeta CID",
        ["MetaCID"] = "MetaCID",
        ["ThreadID"] = "ThreadID",
        ["Title"] = "タイトル",
        ["Description"] = "説明",
        ["Description (optional)"] = "説明（任意）",
        ["Display name (optional)"] = "表示名（任意）",
        ["Display name (optional, blank = keep)"] = "表示名（任意、空欄で維持）",
        ["Body"] = "本文",
        ["Reason (optional)"] = "理由（任意）",
        ["Author key"] = "投稿者鍵",

        ["Board ID / CID is empty. Canceled."] = "Board ID / CID が空です。キャンセルしました。",
        ["BoardMeta CID is empty. Canceled."] = "BoardMeta CID が空です。キャンセルしました。",
        ["Board ID is empty. Canceled."] = "Board ID が空です。キャンセルしました。",
        ["Title is empty. Canceled."] = "タイトルが空です。キャンセルしました。",
        ["Body is empty. Canceled."] = "本文が空です。キャンセルしました。",
        ["Reply to a specific post?"] = "特定の投稿に返信しますか？",

        ["Press Enter to continue..."] = "Enter を押して続行...",
        ["Error"] = "エラー",

        ["up"] = "稼働",
        ["down"] = "停止",

        ["Settings menu"] = "設定メニュー",
        ["Client / Backend"] = "クライアント / バックエンド",
        ["Backend control"] = "バックエンド操作",
        ["Trusted indexers"] = "信頼する Indexer",
        ["Select trusted indexer"] = "信頼する Indexer を選択",
        ["Indexer base URL"] = "Indexer base URL",
        ["Bootstrap indexer base URL"] = "Bootstrap Indexer base URL",
        ["Import from bootstrap"] = "bootstrap から取得",
        ["Base URL"] = "Base URL",

        ["Language"] = "言語",
        ["UI language"] = "UI 言語",
        ["Time zone"] = "タイムゾーン",

        ["Name"] = "名前",
        ["Key name"] = "鍵の名前",
        ["Public key"] = "公開鍵",
        ["Password"] = "パスワード",
        ["protected"] = "あり",
        ["none"] = "なし",
        ["Generate"] = "生成",
        ["Generate a key now?"] = "鍵を生成しますか？",
        ["Set a password now?"] = "今パスワードを設定しますか？",
        ["Set password"] = "パスワード設定",
        ["Remove password"] = "パスワード解除",
        ["Current password"] = "現在のパスワード",
        ["New password"] = "新しいパスワード",
        ["Confirm password"] = "パスワード確認",
        ["Passwords do not match."] = "パスワードが一致しません。",
        ["Password is empty."] = "パスワードが空です。",
        ["No password-protected keys."] = "パスワード付きの鍵がありません。",
        ["Delete"] = "削除",
        ["Delete '{0}'?"] = "「{0}」を削除しますか？",
        ["Remove '{0}'?"] = "「{0}」を削除しますか？",
        ["Blocked authors"] = "ブロックした投稿者",
        ["Public key to block"] = "ブロックする公開鍵",

        ["Query (q)"] = "クエリ（q）",
        ["Board ID (optional)"] = "Board ID（任意）",
        ["Author pubKey (optional)"] = "投稿者 pubKey（任意）",
        ["Since (optional, RFC3339)"] = "Since（任意、RFC3339）",
        ["Until (optional, RFC3339)"] = "Until（任意、RFC3339）",

        ["Board"] = "板",
        ["Post"] = "投稿",
        ["CreatedAt"] = "作成日時",
        ["Author"] = "投稿者",
        ["CID"] = "CID",
        ["Parent"] = "親",

        ["{0} post(s) hidden (blocked authors)."] = "{0} 件の投稿を非表示（ブロック済みの投稿者）",
        ["edited"] = "編集",
        ["tombstoned"] = "削除済み",

        ["Register this board locally (boards.json)?"] = "この板をローカル（boards.json）に登録しますか？",
        ["Registering board..."] = "板を登録しています...",
        ["Create this board and register it locally (boards.json)?"] = "この板を作成してローカル（boards.json）に登録しますか？",
        ["Creating board..."] = "板を作成しています...",
        ["bbs-node not found."] = "bbs-node が見つかりません。",
        ["Set it in Settings → Client / Backend."] = "Settings → Client / Backend で設定してください。",

        ["Invalid backend URL"] = "バックエンド URL が不正です",
        ["Invalid Flexible-IPFS base URL"] = "Flexible-IPFS base URL が不正です",
        ["Data dir is required."] = "データディレクトリは必須です。",
        ["saved"] = "保存しました",
        ["Starting backend..."] = "バックエンドを起動しています...",
        ["started"] = "起動しました",
        ["Restarting backend..."] = "バックエンドを再起動しています...",
        ["restarted"] = "再起動しました",
        ["Auto-start is disabled; restart skipped."] = "自動起動が無効のため、再起動をスキップしました。",
        ["Backend is running but not managed by this client; please restart it manually to apply settings."] = "バックエンドは稼働中ですが、このクライアント管理外です。設定を反映するには手動で再起動してください。",

        ["not a valid absolute URL"] = "URL が不正です（絶対 URL が必要です）",
        ["scheme must be http or https"] = "scheme は http または https にしてください",

        ["Flexible-IPFS HTTP API base URL"] = "Flexible-IPFS HTTP API base URL",
        ["Autostart Flexible-IPFS (when managed by bbs-node)?"] = "Flexible-IPFS を自動起動しますか（bbs-node が管理する場合）？",
        ["Use mDNS on LAN to discover flex-ipfs gw endpoint?"] = "LAN の mDNS で flex-ipfs gw endpoint を発見しますか？",
        ["mDNS discovery timeout (seconds)"] = "mDNS 発見タイムアウト（秒）",
        ["timeout must be >= 1"] = "タイムアウトは 1 以上にしてください",
        ["bbs-node path (blank = auto) [grey](current: {0})[/]"] = "bbs-node パス（空欄で自動） [grey](現在: {0})[/]",
        ["flexible-ipfs-base dir (blank = auto) [grey](current: {0})[/]"] = "flexible-ipfs-base ディレクトリ（空欄で自動） [grey](現在: {0})[/]",
        ["ipfs.endpoint override (blank = none) [grey](e.g. /ip4/192.168.0.10/tcp/4001/ipfs/<PeerID>, current: {0})[/]"] =
            "ipfs.endpoint override（空欄でなし） [grey](例: /ip4/192.168.0.10/tcp/4001/ipfs/<PeerID>, 現在: {0})[/]",

        ["flexible-ipfs-base dir not found. Set it in Flexible-IPFS settings."] = "flexible-ipfs-base ディレクトリが見つかりません。Flexible-IPFS 設定で指定してください。",
        ["kadrtt.properties not found"] = "kadrtt.properties が見つかりません",
        ["Note"] = "注意",
        ["ipfs.endpoint override is set; it will be applied on autostart/restart."] = "ipfs.endpoint override が設定されています。自動起動/再起動時に適用されます。",
        ["No properties found."] = "プロパティが見つかりません。",
        ["Key"] = "キー",
        ["Value"] = "値",
        ["Current value"] = "現在の値",
        ["Edit property"] = "プロパティ編集",
        ["New value (single line, blank = empty)"] = "新しい値（1 行、空欄で空）",
        ["Value must be a single line."] = "値は 1 行にしてください。",
        ["Invalid key."] = "キーが不正です。",
        ["No properties to remove."] = "削除できるプロパティがありません。",
        ["(finish with a single '.' line):"] = "（単独の「.」行で終了）:",
    };
}
