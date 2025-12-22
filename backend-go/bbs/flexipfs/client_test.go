package flexipfs

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestPutValueWithAttr_URLencodesValue(t *testing.T) {
	var got url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v0/dht/peerlist":
			_, _ = w.Write([]byte(`"peer1"`))
		case "/api/v0/dht/putvaluewithattr":
			got = r.URL.Query()
			_ = json.NewEncoder(w).Encode(map[string]any{
				"CID_file": "baf_test",
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)

	c := New(srv.URL + "/api/v0")
	value := `{"a":"x y","b":"1&2"}`
	cid, err := c.PutValueWithAttr(context.Background(), value, []string{"post_1"}, []string{"board_bbs.general"})
	if err != nil {
		t.Fatalf("PutValueWithAttr: %v", err)
	}
	if cid != "baf_test" {
		t.Fatalf("cid mismatch: %q", cid)
	}
	if got.Get("value") != value {
		t.Fatalf("value query mismatch: got=%q want=%q", got.Get("value"), value)
	}
	if got.Get("attrs") != "post_1" {
		t.Fatalf("attrs mismatch: %q", got.Get("attrs"))
	}
	if got.Get("tags") != "board_bbs.general" {
		t.Fatalf("tags mismatch: %q", got.Get("tags"))
	}
}

func TestGetValue_UnwrapsJSONString(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`"{\"hello\":\"world\"}"`))
	}))
	t.Cleanup(srv.Close)

	c := New(srv.URL + "/api/v0")
	b, err := c.GetValue(context.Background(), "baf")
	if err != nil {
		t.Fatalf("GetValue: %v", err)
	}
	if strings.TrimSpace(string(b)) != `{"hello":"world"}` {
		t.Fatalf("unwrap mismatch: %q", string(b))
	}
}

func TestPutValueWithAttr_PeerListEmpty_FailsFast(t *testing.T) {
	var putCalled bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v0/dht/peerlist":
			_, _ = w.Write([]byte(`""`))
		case "/api/v0/dht/putvaluewithattr":
			putCalled = true
			_ = json.NewEncoder(w).Encode(map[string]any{"CID_file": "baf_should_not_happen"})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)

	c := New(srv.URL + "/api/v0")
	_, err := c.PutValueWithAttr(context.Background(), "v", []string{"post_1"}, []string{"board_bbs.general"})
	if err == nil {
		t.Fatalf("expected error")
	}
	if putCalled {
		t.Fatalf("putvaluewithattr should not be called when peerlist is empty")
	}
}

func TestPutValueWithAttr_RetriesOnHTTP400EmptyBody(t *testing.T) {
	var putCalls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v0/dht/peerlist":
			_, _ = w.Write([]byte(`"peer1"`))
		case "/api/v0/dht/putvaluewithattr":
			putCalls++
			if putCalls < 3 {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"CID_file": "baf_test",
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)

	c := New(srv.URL + "/api/v0")
	cid, err := c.PutValueWithAttr(context.Background(), "v", []string{"post_1"}, []string{"board_bbs.general"})
	if err != nil {
		t.Fatalf("PutValueWithAttr: %v", err)
	}
	if cid != "baf_test" {
		t.Fatalf("cid mismatch: %q", cid)
	}
	if putCalls != 3 {
		t.Fatalf("expected 3 put attempts, got %d", putCalls)
	}
}

func TestHttpError_UsesTrailerKeysAsMessage(t *testing.T) {
	trailer := http.Header{}
	trailer["No+target+node+found.%0A"] = nil
	err := httpError(http.StatusBadRequest, nil, http.Header{}, trailer)
	if err == nil {
		t.Fatalf("expected error")
	}
	if !strings.Contains(err.Error(), "No target node found.") {
		t.Fatalf("unexpected error message: %v", err)
	}
}
