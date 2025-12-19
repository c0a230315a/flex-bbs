package main

import (
    "encoding/json"
    "net/http"
    "net/http/httptest"
    "testing"
)

// 単一BoardMetaの取得API (GET /api/v1/boards/{boardId}) のテスト群。
// boards_api.go の registerBoardsHTTP で登録されるハンドラーを、
// テスト用HTTPサーバー経由で叩いて期待どおり動くかを検証する。
func TestBoardsAPI_GetSingleBoard_Success(t *testing.T) {
    // 1件だけ存在する板一覧を準備
    boards := []BoardMeta{
        {
            Version:     1,
            Type:        "boardMeta",
            BoardID:     "bbs.test",
            Title:       "テスト板",
            Description: "テスト用の板",
            CreatedBy:   "tester",
        },
    }

    // テスト用のServeMuxに boards API を登録
    mux := http.NewServeMux()
    registerBoardsHTTP(mux, boards)

    // httptest.NewServer で実サーバー相当を起動
    srv := httptest.NewServer(mux)
    defer srv.Close()

    // 事前に用意した boardId を指定してGET
    resp, err := http.Get(srv.URL + "/api/v1/boards/bbs.test")
    if err != nil {
        t.Fatalf("GET /api/v1/boards/bbs.test: %v", err)
    }
    defer resp.Body.Close()

    if resp.StatusCode != http.StatusOK {
        t.Fatalf("status = %d, want 200", resp.StatusCode)
    }

    // レスポンスJSONを BoardMeta としてパース
    var got BoardMeta
    if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
        t.Fatalf("decode: %v", err)
    }

    // 期待したBoardMetaが返ってきているかを検証
    if got.BoardID != "bbs.test" || got.Title != "テスト板" || got.Description != "テスト用の板" {
        t.Fatalf("unexpected body: %+v", got)
    }
}

func TestBoardsAPI_GetSingleBoard_NotFound(t *testing.T) {
    // 板一覧を空にしておく → どのIDでも存在しない状態
    boards := []BoardMeta{}

    // boards API を登録
    mux := http.NewServeMux()
    registerBoardsHTTP(mux, boards)

    // テスト用HTTPサーバーを起動
    srv := httptest.NewServer(mux)
    defer srv.Close()

    // 存在しない boardId を指定してGET
    resp, err := http.Get(srv.URL + "/api/v1/boards/unknown")
    if err != nil {
        t.Fatalf("GET /api/v1/boards/unknown: %v", err)
    }
    defer resp.Body.Close()

    if resp.StatusCode != http.StatusNotFound {
        t.Fatalf("status = %d, want 404", resp.StatusCode)
    }
}

func TestBoardsAPI_GetSingleBoard_EmptyID(t *testing.T) {
    // 1件だけ存在する板を用意するが、今回はID空のリクエストを投げる
    boards := []BoardMeta{
        {
            Version:     1,
            Type:        "boardMeta",
            BoardID:     "bbs.test",
            Title:       "テスト板",
            Description: "テスト用の板",
            CreatedBy:   "tester",
        },
    }

    // boards API を登録
    mux := http.NewServeMux()
    registerBoardsHTTP(mux, boards)

    // テスト用HTTPサーバーを起動
    srv := httptest.NewServer(mux)
    defer srv.Close()

    // 末尾のスラッシュのみで boardId が空 → 実装上は404になるはず
    resp, err := http.Get(srv.URL + "/api/v1/boards/")
    if err != nil {
        t.Fatalf("GET /api/v1/boards/: %v", err)
    }
    defer resp.Body.Close()

    if resp.StatusCode != http.StatusNotFound {
        t.Fatalf("status = %d, want 404", resp.StatusCode)
    }
}

func TestBoardsAPI_GetSingleBoard_MethodNotAllowed(t *testing.T) {
    // 正常に存在する板を1件用意
    boards := []BoardMeta{
        {
            Version:     1,
            Type:        "boardMeta",
            BoardID:     "bbs.test",
            Title:       "テスト板",
            Description: "テスト用の板",
            CreatedBy:   "tester",
        },
    }

    // boards API を登録
    mux := http.NewServeMux()
    registerBoardsHTTP(mux, boards)

    // テスト用HTTPサーバーを起動
    srv := httptest.NewServer(mux)
    defer srv.Close()

    // GETではなくPOSTで叩く → 405(Method Not Allowed)になることを確認したい
    req, err := http.NewRequest(http.MethodPost, srv.URL+"/api/v1/boards/bbs.test", nil)
    if err != nil {
        t.Fatalf("NewRequest: %v", err)
    }

    resp, err := http.DefaultClient.Do(req)
    if err != nil {
        t.Fatalf("POST /api/v1/boards/bbs.test: %v", err)
    }
    defer resp.Body.Close()

    if resp.StatusCode != http.StatusMethodNotAllowed {
        t.Fatalf("status = %d, want 405", resp.StatusCode)
    }
}
