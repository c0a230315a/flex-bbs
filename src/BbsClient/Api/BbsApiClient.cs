using System.Net.Http.Json;
using System.Diagnostics;
using System.Text.Json;
using BbsClient.Util;

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
        var sw = Stopwatch.StartNew();
        using var resp = await SendAsync(HttpMethod.Get, url, ct);
        await EnsureSuccess("GET", url, resp, ct, sw);
        return await resp.Content.ReadFromJsonAsync<List<BoardItem>>(JsonOptions, ct) ?? [];
    }

    public async Task<BoardItem> GetBoardAsync(string boardId, CancellationToken ct)
    {
        var url = $"{_baseUrl}/api/v1/boards/{Uri.EscapeDataString(boardId)}";
        var sw = Stopwatch.StartNew();
        using var resp = await SendAsync(HttpMethod.Get, url, ct);
        await EnsureSuccess("GET", url, resp, ct, sw);
        return await resp.Content.ReadFromJsonAsync<BoardItem>(JsonOptions, ct) ?? throw new InvalidOperationException("empty response");
    }

    public async Task<List<ThreadItem>> GetThreadsAsync(string boardId, int limit, int offset, CancellationToken ct)
    {
        var url = $"{_baseUrl}/api/v1/boards/{Uri.EscapeDataString(boardId)}/threads?limit={limit}&offset={offset}";
        var sw = Stopwatch.StartNew();
        using var resp = await SendAsync(HttpMethod.Get, url, ct);
        await EnsureSuccess("GET", url, resp, ct, sw);
        return await resp.Content.ReadFromJsonAsync<List<ThreadItem>>(JsonOptions, ct) ?? [];
    }

    public async Task<ThreadResponse> GetThreadAsync(string threadId, CancellationToken ct)
    {
        var url = $"{_baseUrl}/api/v1/threads/{Uri.EscapeDataString(threadId)}";
        var sw = Stopwatch.StartNew();
        using var resp = await SendAsync(HttpMethod.Get, url, ct);
        await EnsureSuccess("GET", url, resp, ct, sw);
        return await resp.Content.ReadFromJsonAsync<ThreadResponse>(JsonOptions, ct) ?? throw new InvalidOperationException("empty response");
    }

    public async Task<CreateThreadResponse> CreateThreadAsync(CreateThreadRequest req, CancellationToken ct)
    {
        var url = $"{_baseUrl}/api/v1/threads";
        var sw = Stopwatch.StartNew();
        using var resp = await _http.PostAsJsonAsync(url, req, JsonOptions, ct);
        await EnsureSuccess("POST", url, resp, ct, sw);
        return (await resp.Content.ReadFromJsonAsync<CreateThreadResponse>(JsonOptions, ct)) ?? throw new InvalidOperationException("empty response");
    }

    public async Task<AddPostResponse> AddPostAsync(AddPostRequest req, CancellationToken ct)
    {
        var url = $"{_baseUrl}/api/v1/posts";
        var sw = Stopwatch.StartNew();
        using var resp = await _http.PostAsJsonAsync(url, req, JsonOptions, ct);
        await EnsureSuccess("POST", url, resp, ct, sw);
        return (await resp.Content.ReadFromJsonAsync<AddPostResponse>(JsonOptions, ct)) ?? throw new InvalidOperationException("empty response");
    }

    public async Task<EditPostResponse> EditPostAsync(string postCid, EditPostRequest req, CancellationToken ct)
    {
        var url = $"{_baseUrl}/api/v1/posts/{Uri.EscapeDataString(postCid)}/edit";
        var sw = Stopwatch.StartNew();
        using var resp = await _http.PostAsJsonAsync(url, req, JsonOptions, ct);
        await EnsureSuccess("POST", url, resp, ct, sw);
        return (await resp.Content.ReadFromJsonAsync<EditPostResponse>(JsonOptions, ct)) ?? throw new InvalidOperationException("empty response");
    }

    public async Task<TombstonePostResponse> TombstonePostAsync(string postCid, TombstonePostRequest req, CancellationToken ct)
    {
        var url = $"{_baseUrl}/api/v1/posts/{Uri.EscapeDataString(postCid)}/tombstone";
        var sw = Stopwatch.StartNew();
        using var resp = await _http.PostAsJsonAsync(url, req, JsonOptions, ct);
        await EnsureSuccess("POST", url, resp, ct, sw);
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
        var sw = Stopwatch.StartNew();
        using var resp = await SendAsync(HttpMethod.Get, url, ct);
        await EnsureSuccess("GET", url, resp, ct, sw);
        return (await resp.Content.ReadFromJsonAsync<List<SearchPostResult>>(JsonOptions, ct)) ?? [];
    }

    private async Task<HttpResponseMessage> SendAsync(HttpMethod method, string url, CancellationToken ct)
    {
        try
        {
            var req = new HttpRequestMessage(method, url);
            return await _http.SendAsync(req, ct);
        }
        catch (Exception ex)
        {
            AppLog.Error($"{method.Method} {url} failed", ex);
            throw;
        }
    }

    private static async Task EnsureSuccess(string method, string url, HttpResponseMessage resp, CancellationToken ct, Stopwatch? sw = null)
    {
        if (resp.IsSuccessStatusCode)
        {
            return;
        }
        var body = await ReadBodySafe(resp, ct);
        if (sw != null)
        {
            AppLog.Http(method, url, (int)resp.StatusCode, sw.Elapsed, body);
        }
        throw new HttpRequestException($"HTTP {(int)resp.StatusCode}: {body}");
    }

    private static async Task<string> ReadBodySafe(HttpResponseMessage resp, CancellationToken ct)
    {
        try
        {
            var body = await resp.Content.ReadAsStringAsync(ct);
            body = body.Trim();
            if (body.Length > 2048)
            {
                body = body[..2048] + "...(truncated)";
            }
            return body;
        }
        catch
        {
            return "<failed to read body>";
        }
    }
}
