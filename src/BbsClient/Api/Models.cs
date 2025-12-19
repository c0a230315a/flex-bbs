using System.Text.Json.Serialization;

namespace BbsClient.Api;

public sealed class PostBody
{
    [JsonPropertyName("format")]
    public string Format { get; set; } = "";

    [JsonPropertyName("content")]
    public string Content { get; set; } = "";
}

public sealed class Attachment
{
    [JsonPropertyName("cid")]
    public string CID { get; set; } = "";

    [JsonPropertyName("mime")]
    public string Mime { get; set; } = "";
}

public sealed class Post
{
    [JsonPropertyName("version")]
    public int Version { get; set; }

    [JsonPropertyName("type")]
    public string Type { get; set; } = "";

    [JsonPropertyName("postCid")]
    public string? PostCID { get; set; }

    [JsonPropertyName("threadId")]
    public string ThreadID { get; set; } = "";

    [JsonPropertyName("parentPostCid")]
    public string? ParentPostCID { get; set; }

    [JsonPropertyName("authorPubKey")]
    public string AuthorPubKey { get; set; } = "";

    [JsonPropertyName("displayName")]
    public string DisplayName { get; set; } = "";

    [JsonPropertyName("body")]
    public PostBody Body { get; set; } = new();

    [JsonPropertyName("attachments")]
    public List<Attachment> Attachments { get; set; } = [];

    [JsonPropertyName("createdAt")]
    public string CreatedAt { get; set; } = "";

    [JsonPropertyName("editedAt")]
    public string? EditedAt { get; set; }

    [JsonPropertyName("meta")]
    public Dictionary<string, object?> Meta { get; set; } = new();

    [JsonPropertyName("signature")]
    public string Signature { get; set; } = "";
}

public sealed class ThreadMeta
{
    [JsonPropertyName("version")]
    public int Version { get; set; }

    [JsonPropertyName("type")]
    public string Type { get; set; } = "";

    [JsonPropertyName("threadId")]
    public string ThreadID { get; set; } = "";

    [JsonPropertyName("boardId")]
    public string BoardID { get; set; } = "";

    [JsonPropertyName("title")]
    public string Title { get; set; } = "";

    [JsonPropertyName("rootPostCid")]
    public string RootPostCID { get; set; } = "";

    [JsonPropertyName("createdAt")]
    public string CreatedAt { get; set; } = "";

    [JsonPropertyName("createdBy")]
    public string CreatedBy { get; set; } = "";

    [JsonPropertyName("meta")]
    public Dictionary<string, object?> Meta { get; set; } = new();

    [JsonPropertyName("signature")]
    public string Signature { get; set; } = "";
}

public sealed class BoardMeta
{
    [JsonPropertyName("version")]
    public int Version { get; set; }

    [JsonPropertyName("type")]
    public string Type { get; set; } = "";

    [JsonPropertyName("boardId")]
    public string BoardID { get; set; } = "";

    [JsonPropertyName("title")]
    public string Title { get; set; } = "";

    [JsonPropertyName("description")]
    public string Description { get; set; } = "";

    [JsonPropertyName("logHeadCid")]
    public string? LogHeadCID { get; set; }

    [JsonPropertyName("createdAt")]
    public string CreatedAt { get; set; } = "";

    [JsonPropertyName("createdBy")]
    public string CreatedBy { get; set; } = "";

    [JsonPropertyName("signature")]
    public string Signature { get; set; } = "";
}

public sealed class BoardItem
{
    [JsonPropertyName("boardMetaCid")]
    public string BoardMetaCID { get; set; } = "";

    [JsonPropertyName("board")]
    public BoardMeta Board { get; set; } = new();
}

public sealed class ThreadItem
{
    [JsonPropertyName("threadId")]
    public string ThreadID { get; set; } = "";

    [JsonPropertyName("threadMetaCid")]
    public string ThreadMetaCID { get; set; } = "";

    [JsonPropertyName("thread")]
    public ThreadMeta Thread { get; set; } = new();
}

public sealed class ThreadPostItem
{
    [JsonPropertyName("cid")]
    public string CID { get; set; } = "";

    [JsonPropertyName("post")]
    public Post Post { get; set; } = new();

    [JsonPropertyName("tombstoned")]
    public bool Tombstoned { get; set; }

    [JsonPropertyName("tombstoneReason")]
    public string? TombstoneReason { get; set; }
}

public sealed class ThreadResponse
{
    [JsonPropertyName("threadMetaCid")]
    public string ThreadMetaCID { get; set; } = "";

    [JsonPropertyName("threadMeta")]
    public ThreadMeta ThreadMeta { get; set; } = new();

    [JsonPropertyName("posts")]
    public List<ThreadPostItem> Posts { get; set; } = [];
}

public sealed class CreateThreadRequest
{
    [JsonPropertyName("boardId")]
    public string BoardID { get; set; } = "";

    [JsonPropertyName("title")]
    public string Title { get; set; } = "";

    [JsonPropertyName("displayName")]
    public string DisplayName { get; set; } = "";

    [JsonPropertyName("body")]
    public PostBody Body { get; set; } = new();

    [JsonPropertyName("attachments")]
    public List<Attachment> Attachments { get; set; } = [];

    [JsonPropertyName("threadMeta")]
    public Dictionary<string, object?> ThreadMeta { get; set; } = new();

    [JsonPropertyName("postMeta")]
    public Dictionary<string, object?> PostMeta { get; set; } = new();

    [JsonPropertyName("authorPrivKey")]
    public string AuthorPrivKey { get; set; } = "";
}

public sealed class CreateThreadResponse
{
    [JsonPropertyName("threadId")]
    public string ThreadID { get; set; } = "";

    [JsonPropertyName("rootPostCid")]
    public string RootPostCID { get; set; } = "";

    [JsonPropertyName("boardLogCid")]
    public string BoardLogCID { get; set; } = "";

    [JsonPropertyName("boardMetaCid")]
    public string BoardMetaCID { get; set; } = "";

    [JsonPropertyName("threadMeta")]
    public ThreadMeta ThreadMeta { get; set; } = new();
}

public sealed class AddPostRequest
{
    [JsonPropertyName("threadId")]
    public string ThreadID { get; set; } = "";

    [JsonPropertyName("parentPostCid")]
    public string? ParentPostCID { get; set; }

    [JsonPropertyName("displayName")]
    public string DisplayName { get; set; } = "";

    [JsonPropertyName("body")]
    public PostBody Body { get; set; } = new();

    [JsonPropertyName("attachments")]
    public List<Attachment> Attachments { get; set; } = [];

    [JsonPropertyName("meta")]
    public Dictionary<string, object?> Meta { get; set; } = new();

    [JsonPropertyName("authorPrivKey")]
    public string AuthorPrivKey { get; set; } = "";
}

public sealed class AddPostResponse
{
    [JsonPropertyName("postCid")]
    public string PostCID { get; set; } = "";

    [JsonPropertyName("boardLogCid")]
    public string BoardLogCID { get; set; } = "";

    [JsonPropertyName("boardMetaCid")]
    public string BoardMetaCID { get; set; } = "";
}

public sealed class EditPostRequest
{
    [JsonPropertyName("body")]
    public PostBody Body { get; set; } = new();

    [JsonPropertyName("displayName")]
    public string? DisplayName { get; set; }

    [JsonPropertyName("authorPrivKey")]
    public string AuthorPrivKey { get; set; } = "";
}

public sealed class EditPostResponse
{
    [JsonPropertyName("oldPostCid")]
    public string OldPostCID { get; set; } = "";

    [JsonPropertyName("newPostCid")]
    public string NewPostCID { get; set; } = "";

    [JsonPropertyName("boardLogCid")]
    public string BoardLogCID { get; set; } = "";

    [JsonPropertyName("boardMetaCid")]
    public string BoardMetaCID { get; set; } = "";
}

public sealed class TombstonePostRequest
{
    [JsonPropertyName("reason")]
    public string? Reason { get; set; }

    [JsonPropertyName("authorPrivKey")]
    public string AuthorPrivKey { get; set; } = "";
}

public sealed class TombstonePostResponse
{
    [JsonPropertyName("targetPostCid")]
    public string TargetPostCID { get; set; } = "";

    [JsonPropertyName("boardLogCid")]
    public string BoardLogCID { get; set; } = "";

    [JsonPropertyName("boardMetaCid")]
    public string BoardMetaCID { get; set; } = "";
}

public sealed class SearchPostResult
{
    [JsonPropertyName("postCid")]
    public string PostCID { get; set; } = "";

    [JsonPropertyName("threadId")]
    public string ThreadID { get; set; } = "";

    [JsonPropertyName("boardId")]
    public string BoardID { get; set; } = "";

    [JsonPropertyName("authorPubKey")]
    public string AuthorPubKey { get; set; } = "";

    [JsonPropertyName("displayName")]
    public string DisplayName { get; set; } = "";

    [JsonPropertyName("bodyFormat")]
    public string BodyFormat { get; set; } = "";

    [JsonPropertyName("bodyContent")]
    public string BodyContent { get; set; } = "";

    [JsonPropertyName("createdAt")]
    public string CreatedAt { get; set; } = "";

    [JsonPropertyName("editedAt")]
    public string? EditedAt { get; set; }
}

