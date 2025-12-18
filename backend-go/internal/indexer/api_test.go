package indexer

import (
    "context"
    "encoding/json"
    "net/http"
    "net/http/httptest"
    "testing"
)

func TestAPI_SearchPosts(t *testing.T) {
    ctx := context.Background()
    db, err := NewSQLiteDB(":memory:")
    if err != nil {
        t.Fatalf("NewSQLiteDB: %v", err)
    }
    defer db.Close()

    // 事前データ投入
    if err := db.CreateBoard(ctx, &Board{ID: "b1", Name: "B", Description: "desc"}); err != nil {
        t.Fatalf("CreateBoard: %v", err)
    }
    if err := db.CreateThread(ctx, &Thread{ID: "t1", BoardID: "b1", Title: "T", AuthorID: "u1"}); err != nil {
        t.Fatalf("CreateThread: %v", err)
    }
    if err := db.CreatePost(ctx, &Post{
        ID:       "p1",
        ThreadID: "t1",
        BoardID:  "b1",
        AuthorID: "u1",
        Content:  "hello api test",
    }); err != nil {
        t.Fatalf("CreatePost: %v", err)
    }

    h := NewAPIHandler(db)
    mux := http.NewServeMux()
    h.RegisterRoutes(mux)

    srv := httptest.NewServer(mux)
    defer srv.Close()

    resp, err := http.Get(srv.URL + "/api/v1/search/posts?query=hello")
    if err != nil {
        t.Fatalf("GET /search/posts: %v", err)
    }
    defer resp.Body.Close()

    if resp.StatusCode != http.StatusOK {
        t.Fatalf("status = %d, want 200", resp.StatusCode)
    }

    var body SearchPostsResponse
    if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
        t.Fatalf("decode: %v", err)
    }
    if body.TotalCount != 1 || len(body.Posts) != 1 || body.Posts[0].ID != "p1" {
        t.Fatalf("unexpected body: %+v", body)
    }
}