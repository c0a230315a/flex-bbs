package indexer

import (
    "context"
    "testing"
    "time"
)

func TestSQLiteDB_BasicCRUDAndSearch(t *testing.T) {
    ctx := context.Background()
    db, err := NewSQLiteDB(":memory:")
    if err != nil {
        t.Fatalf("NewSQLiteDB: %v", err)
    }
    defer db.Close()

    // Board
    b := &Board{
        ID:          "board1",
        Name:        "Test Board",
        Description: "desc",
    }
    if err := db.CreateBoard(ctx, b); err != nil {
        t.Fatalf("CreateBoard: %v", err)
    }

    // Thread
    th := &Thread{
        ID:       "thread1",
        BoardID:  "board1",
        Title:    "Hello",
        AuthorID: "user1",
    }
    if err := db.CreateThread(ctx, th); err != nil {
        t.Fatalf("CreateThread: %v", err)
    }

    // Post
    p1 := &Post{
        ID:       "post1",
        ThreadID: "thread1",
        BoardID:  "board1",
        AuthorID: "user1",
        Content:  "hello world",
    }
    if err := db.CreatePost(ctx, p1); err != nil {
        t.Fatalf("CreatePost: %v", err)
    }
    p2 := &Post{
        ID:       "post2",
        ThreadID: "thread1",
        BoardID:  "board1",
        AuthorID: "user2",
        Content:  "another message",
    }
    if err := db.CreatePost(ctx, p2); err != nil {
        t.Fatalf("CreatePost#2: %v", err)
    }

    // SearchPosts
    resp, err := db.SearchPosts(ctx, &SearchPostsRequest{
        Query: "hello",
    })
    if err != nil {
        t.Fatalf("SearchPosts: %v", err)
    }
    if resp.TotalCount != 1 {
        t.Fatalf("SearchPosts TotalCount = %d, want 1", resp.TotalCount)
    }
    if len(resp.Posts) != 1 || resp.Posts[0].ID != "post1" {
        t.Fatalf("SearchPosts Posts mismatch: %+v", resp.Posts)
    }

    // SearchThreads
    thResp, err := db.SearchThreads(ctx, &SearchThreadsRequest{
        Query: "Hello",
    })
    if err != nil {
        t.Fatalf("SearchThreads: %v", err)
    }
    if thResp.TotalCount != 1 || thResp.Threads[0].ID != "thread1" {
        t.Fatalf("SearchThreads mismatch: %+v", thResp.Threads)
    }
}

func TestSQLiteDB_LogSequence(t *testing.T) {
    ctx := context.Background()
    db, err := NewSQLiteDB(":memory:")
    if err != nil {
        t.Fatalf("NewSQLiteDB: %v", err)
    }
    defer db.Close()

    seq, err := db.GetLastSequence(ctx)
    if err != nil {
        t.Fatalf("GetLastSequence: %v", err)
    }
    if seq != 0 {
        t.Fatalf("initial last_seq = %d, want 0", seq)
    }

    if err := db.SetLastSequence(ctx, 42); err != nil {
        t.Fatalf("SetLastSequence: %v", err)
    }
    seq, err = db.GetLastSequence(ctx)
    if err != nil {
        t.Fatalf("GetLastSequence(2): %v", err)
    }
    if seq != 42 {
        t.Fatalf("last_seq = %d, want 42", seq)
    }
}