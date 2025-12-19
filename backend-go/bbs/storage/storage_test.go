package storage

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"

	"flex-bbs/backend-go/bbs/flexipfs"
	"flex-bbs/backend-go/bbs/types"
)

func TestSaveThreadMeta_DoublePutTags(t *testing.T) {
	type putCall struct {
		attrs string
		tags  string
		value string
	}

	var (
		mu    sync.Mutex
		calls []putCall
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v0/dht/putvaluewithattr":
			q := r.URL.Query()
			mu.Lock()
			calls = append(calls, putCall{
				attrs: q.Get("attrs"),
				tags:  q.Get("tags"),
				value: q.Get("value"),
			})
			mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]any{"CID_file": "baf_thread"})
		case "/api/v0/dht/getvalue":
			w.WriteHeader(http.StatusNotFound)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)

	fc := flexipfs.New(srv.URL + "/api/v0")
	st := New(fc)
	tm := &types.ThreadMeta{
		Version:     1,
		Type:        types.TypeThreadMeta,
		ThreadID:    "",
		BoardID:     "bbs.general",
		Title:       "hello",
		RootPostCID: "baf_root",
		CreatedAt:   "2025-01-01T00:00:00Z",
		CreatedBy:   "ed25519:pub",
		Meta:        map[string]any{},
		Signature:   "sig",
	}
	cid, err := st.SaveThreadMeta(context.Background(), tm)
	if err != nil {
		t.Fatalf("SaveThreadMeta: %v", err)
	}
	if cid != "baf_thread" {
		t.Fatalf("cid mismatch: %q", cid)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(calls) != 2 {
		t.Fatalf("put call count: %d", len(calls))
	}

	if calls[0].attrs != "objtype_threadmeta_version_1" || calls[1].attrs != "objtype_threadmeta_version_1" {
		t.Fatalf("attrs mismatch: %#v", calls)
	}
	if calls[0].tags != "board_bbs.general" {
		t.Fatalf("first tags mismatch: %q", calls[0].tags)
	}
	if calls[1].tags != "board_bbs.general-thread_baf_thread" {
		t.Fatalf("second tags mismatch: %q", calls[1].tags)
	}
	if calls[0].value != calls[1].value {
		t.Fatalf("value differs between puts")
	}
	if _, err := url.ParseQuery("value=" + url.QueryEscape(calls[0].value)); err != nil {
		t.Fatalf("value not encodable: %v", err)
	}
}
