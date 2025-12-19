using System.Net.Http.Json;
using System.Text.Json;

namespace BbsClient.Api;

public sealed class BbsApiClient
{
    private static readonly JsonSerializerOptions JsonOptions = new()
    {
        PropertyNameCaseInsensitive = true,
    };

    private readonly HttpClient _http;
    private readonly string _baseUrl;

    public BbsApiClient(HttpClient httpClient, string baseUrl)
    {
        _http = httpClient;
        _baseUrl = baseUrl.TrimEnd('/');
    }

    public async Task<List<BoardItem>> GetBoardsAsync(CancellationToken ct)
    {
        var url = $"{_baseUrl}/api/v1/boards";
        return await _http.GetFromJsonAsync<List<BoardItem>>(url, JsonOptions, ct) ?? [];
    }

    public async Task<BoardItem> GetBoardAsync(string boardId, CancellationToken ct)
    {
        var url = $"{_baseUrl}/api/v1/boards/{Uri.EscapeDataString(boardId)}";
        return await _http.GetFromJsonAsync<BoardItem>(url, JsonOptions, ct) ?? throw new InvalidOperationException("empty response");
    }

    public async Task<List<ThreadItem>> GetThreadsAsync(string boardId, int limit, int offset, CancellationToken ct)
    {
        var url = $"{_baseUrl}/api/v1/boards/{Uri.EscapeDataString(boardId)}/threads?limit={limit}&offset={offset}";
        return await _http.GetFromJsonAsync<List<ThreadItem>>(url, JsonOptions, ct) ?? [];
    }

    public async Task<ThreadResponse> GetThreadAsync(string threadId, CancellationToken ct)
    {
        var url = $"{_baseUrl}/api/v1/threads/{Uri.EscapeDataString(threadId)}";
        return await _http.GetFromJsonAsync<ThreadResponse>(url, JsonOptions, ct) ?? throw new InvalidOperationException("empty response");
    }

    public async Task<CreateThreadResponse> CreateThreadAsync(CreateThreadRequest req, CancellationToken ct)
    {
        var url = $"{_baseUrl}/api/v1/threads";
        var resp = await _http.PostAsJsonAsync(url, req, JsonOptions, ct);
        await EnsureSuccess(resp);
        return (await resp.Content.ReadFromJsonAsync<CreateThreadResponse>(JsonOptions, ct)) ?? throw new InvalidOperationException("empty response");
    }

    public async Task<AddPostResponse> AddPostAsync(AddPostRequest req, CancellationToken ct)
    {
        var url = $"{_baseUrl}/api/v1/posts";
        var resp = await _http.PostAsJsonAsync(url, req, JsonOptions, ct);
        await EnsureSuccess(resp);
        return (await resp.Content.ReadFromJsonAsync<AddPostResponse>(JsonOptions, ct)) ?? throw new InvalidOperationException("empty response");
    }

    public async Task<EditPostResponse> EditPostAsync(string postCid, EditPostRequest req, CancellationToken ct)
    {
        var url = $"{_baseUrl}/api/v1/posts/{Uri.EscapeDataString(postCid)}/edit";
        var resp = await _http.PostAsJsonAsync(url, req, JsonOptions, ct);
        await EnsureSuccess(resp);
        return (await resp.Content.ReadFromJsonAsync<EditPostResponse>(JsonOptions, ct)) ?? throw new InvalidOperationException("empty response");
    }

    public async Task<TombstonePostResponse> TombstonePostAsync(string postCid, TombstonePostRequest req, CancellationToken ct)
    {
        var url = $"{_baseUrl}/api/v1/posts/{Uri.EscapeDataString(postCid)}/tombstone";
        var resp = await _http.PostAsJsonAsync(url, req, JsonOptions, ct);
        await EnsureSuccess(resp);
        return (await resp.Content.ReadFromJsonAsync<TombstonePostResponse>(JsonOptions, ct)) ?? throw new InvalidOperationException("empty response");
    }

    public async Task<List<SearchPostResult>> SearchPostsAsync(string q, string? boardId, string? author, string? since, string? until, int limit, int offset, CancellationToken ct)
    {
        var query = new List<string>();
        if (!string.IsNullOrWhiteSpace(q)) query.Add($"q={Uri.EscapeDataString(q)}");
        if (!string.IsNullOrWhiteSpace(boardId)) query.Add($"boardId={Uri.EscapeDataString(boardId)}");
        if (!string.IsNullOrWhiteSpace(author)) query.Add($"author={Uri.EscapeDataString(author)}");
        if (!string.IsNullOrWhiteSpace(since)) query.Add($"since={Uri.EscapeDataString(since)}");
        if (!string.IsNullOrWhiteSpace(until)) query.Add($"until={Uri.EscapeDataString(until)}");
        query.Add($"limit={limit}");
        query.Add($"offset={offset}");
        var url = $"{_baseUrl}/api/v1/search/posts?{string.Join("&", query)}";
        var resp = await _http.GetAsync(url, ct);
        await EnsureSuccess(resp);
        return (await resp.Content.ReadFromJsonAsync<List<SearchPostResult>>(JsonOptions, ct)) ?? [];
    }

    private static async Task EnsureSuccess(HttpResponseMessage resp)
    {
        if (resp.IsSuccessStatusCode)
        {
            return;
        }
        var body = await resp.Content.ReadAsStringAsync();
        throw new HttpRequestException($"HTTP {(int)resp.StatusCode}: {body}");
    }
}
