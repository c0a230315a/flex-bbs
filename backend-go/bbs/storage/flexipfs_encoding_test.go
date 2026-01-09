package storage

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"flex-bbs/backend-go/bbs/flexipfs"
	bbslog "flex-bbs/backend-go/bbs/log"
	"flex-bbs/backend-go/bbs/signature"
	"flex-bbs/backend-go/bbs/types"
)

func TestFlexIPFS_QueryDecoding_NonASCIIValueRoundTrip(t *testing.T) {
	t.Parallel()

	type stored struct {
		mu    sync.Mutex
		byCID map[string]string
	}
	state := &stored{byCID: map[string]string{}}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v0/dht/peerlist":
			_ = json.NewEncoder(w).Encode("peer1")
		case "/api/v0/dht/putvaluewithattr":
			value, ok := javaURIQueryGet(r.URL.RawQuery, "value")
			if !ok {
				http.Error(w, "missing value", http.StatusBadRequest)
				return
			}
			cid := "baf_test"
			state.mu.Lock()
			state.byCID[cid] = value
			state.mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]any{"CID_file": cid})
		case "/api/v0/dht/getvalue":
			cid := r.URL.Query().Get("cid")
			if cid == "" {
				http.Error(w, "missing cid", http.StatusBadRequest)
				return
			}
			state.mu.Lock()
			value, ok := state.byCID[cid]
			state.mu.Unlock()
			if !ok {
				http.NotFound(w, r)
				return
			}
			_ = json.NewEncoder(w).Encode(value)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	fc := flexipfs.New(srv.URL + "/api/v0")
	st := New(fc)

	_, priv, err := signature.GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair: %v", err)
	}

	origTitle := "日本語のタイトル"
	origDescription := "日本語の説明"
	bm := &types.BoardMeta{
		BoardID:     "test",
		Title:       origTitle,
		Description: origDescription,
		CreatedAt:   "2025-01-01T00:00:00Z",
	}
	if err := signature.SignBoardMeta(priv, bm); err != nil {
		t.Fatalf("SignBoardMeta: %v", err)
	}

	cid, err := st.SaveBoardMeta(context.Background(), bm)
	if err != nil {
		t.Fatalf("SaveBoardMeta: %v", err)
	}

	loaded, err := st.LoadBoardMeta(context.Background(), cid)
	if err != nil {
		t.Fatalf("LoadBoardMeta: %v", err)
	}

	if !bbslog.VerifyBoardMeta(loaded) {
		t.Fatalf("VerifyBoardMeta failed after round-trip")
	}
	if loaded.Title != origTitle {
		t.Fatalf("title mismatch: got %q want %q", loaded.Title, origTitle)
	}
	if loaded.Description != origDescription {
		t.Fatalf("description mismatch: got %q want %q", loaded.Description, origDescription)
	}
}

func javaURIQueryGet(rawQuery, key string) (string, bool) {
	for _, part := range strings.Split(rawQuery, "&") {
		if part == "" {
			continue
		}
		kv := strings.SplitN(part, "=", 2)
		k := javaURIDecodeComponent(kv[0])
		if k != key {
			continue
		}
		if len(kv) == 1 {
			return "", true
		}
		return javaURIDecodeComponent(kv[1]), true
	}
	return "", false
}

func javaURIDecodeComponent(s string) string {
	var out []byte
	for i := 0; i < len(s); i++ {
		if s[i] == '%' && i+2 < len(s) {
			if b, err := hex.DecodeString(s[i+1 : i+3]); err == nil {
				out = append(out, b[0])
				i += 2
				continue
			}
		}
		out = append(out, s[i])
	}

	runes := make([]rune, len(out))
	for i, b := range out {
		runes[i] = rune(b)
	}
	return string(runes)
}
