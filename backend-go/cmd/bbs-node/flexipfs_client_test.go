package main

import (
    "context"
    "encoding/json"
    "net/http"
    "net/http/httptest"
    "testing"
)

// HTTPベースのクライアント(httpFlexIPFSClient)の基本動作を確認するテスト
func TestHTTPFlexIPFSClient_GetValue_JSONResponse(t *testing.T) {
    // /dht/getvalue に対してJSONレスポンスを返すテスト用サーバー
    mux := http.NewServeMux()
    mux.HandleFunc("/dht/getvalue", func(w http.ResponseWriter, r *http.Request) {
        if r.Method != http.MethodPost {
            t.Fatalf("unexpected method: %s", r.Method)
        }

        resp := FlexGetValueResponse{
            Value: []byte("hello"),
            Attrs: map[string]string{"type": "test-type"},
            Tags:  []string{"tag1"},
        }
        w.Header().Set("Content-Type", "application/json")
        if err := json.NewEncoder(w).Encode(&resp); err != nil {
            t.Fatalf("encode: %v", err)
        }
    })

    srv := httptest.NewServer(mux)
    defer srv.Close()

    // テストサーバーに向けたクライアントを作成
    client := NewFlexIPFSClientWithHTTPClient(srv.URL, srv.Client())

    ctx := context.Background()
    got, err := client.GetValue(ctx, "test-key")
    if err != nil {
        t.Fatalf("GetValue: %v", err)
    }

    if string(got.Value) != "hello" {
        t.Fatalf("Value = %q, want %q", string(got.Value), "hello")
    }
    if got.Type != "test-type" {
        t.Fatalf("Type = %q, want %q", got.Type, "test-type")
    }
    if got.Attrs["type"] != "test-type" {
        t.Fatalf("Attrs[type] = %q, want %q", got.Attrs["type"], "test-type")
    }
}

// GetValue が非JSONレスポンスに対してフォールバック動作をすることを確認
func TestHTTPFlexIPFSClient_GetValue_NonJSONFallback(t *testing.T) {
    mux := http.NewServeMux()
    mux.HandleFunc("/dht/getvalue", func(w http.ResponseWriter, r *http.Request) {
        w.WriteHeader(http.StatusOK)
        _, _ = w.Write([]byte("raw-bytes"))
    })

    srv := httptest.NewServer(mux)
    defer srv.Close()

    client := NewFlexIPFSClientWithHTTPClient(srv.URL, srv.Client())

    ctx := context.Background()
    got, err := client.GetValue(ctx, "test-key")
    if err != nil {
        t.Fatalf("GetValue: %v", err)
    }

    if string(got.Value) != "raw-bytes" {
        t.Fatalf("Value = %q, want %q", string(got.Value), "raw-bytes")
    }
    if got.Type != "" {
        t.Fatalf("Type = %q, want empty", got.Type)
    }
}

// エラー時に Flexible-IPFS 由来のJSONエラーがラップされることを確認
func TestHTTPFlexIPFSClient_PutValueWithAttr_JSONError(t *testing.T) {
    mux := http.NewServeMux()
    mux.HandleFunc("/dht/putvaluewithattr", func(w http.ResponseWriter, r *http.Request) {
        w.WriteHeader(http.StatusInternalServerError)
        _, _ = w.Write([]byte(`{"message":"oops","code":"ERR_INTERNAL"}`))
    })

    srv := httptest.NewServer(mux)
    defer srv.Close()

    client := NewFlexIPFSClientWithHTTPClient(srv.URL, srv.Client())

    ctx := context.Background()
    err := client.PutValueWithAttr(ctx, "key", []byte("value"), nil, nil)
    if err == nil {
        t.Fatalf("PutValueWithAttr expected error, got nil")
    }

    if _, ok := err.(*FlexErrorResponse); !ok {
        t.Fatalf("error type = %T, want *FlexErrorResponse", err)
    }
}

// モッククライアント(MockFlexIPFSClient)の基本的なPut/Get動作を確認
func TestMockFlexIPFSClient_PutAndGet(t *testing.T) {
    mock := NewMockFlexIPFSClient("http://example")

    ctx := context.Background()
    attrs := map[string]string{"type": "my-type"}
    tags := []string{"tag1", "tag2"}

    if err := mock.PutValueWithAttr(ctx, "k1", []byte("data"), attrs, tags); err != nil {
        t.Fatalf("PutValueWithAttr: %v", err)
    }

    res, err := mock.GetValue(ctx, "k1")
    if err != nil {
        t.Fatalf("GetValue: %v", err)
    }

    if string(res.Value) != "data" {
        t.Fatalf("Value = %q, want %q", string(res.Value), "data")
    }
    if res.Type != "my-type" {
        t.Fatalf("Type = %q, want %q", res.Type, "my-type")
    }

    // Deep copy されていることの簡易チェック（元attrsを書き換えてもres.Attrsは変わらない）
    attrs["type"] = "changed"
    if res.Attrs["type"] != "my-type" {
        t.Fatalf("Attrs[type] was modified unexpectedly: %q", res.Attrs["type"])
    }

    keys := mock.GetStorageKeys()
    if len(keys) != 1 || keys[0] != "k1" {
        t.Fatalf("GetStorageKeys = %v, want [k1]", keys)
    }

    mock.ClearStorage()
    keys = mock.GetStorageKeys()
    if len(keys) != 0 {
        t.Fatalf("GetStorageKeys after ClearStorage = %v, want []", keys)
    }
}
