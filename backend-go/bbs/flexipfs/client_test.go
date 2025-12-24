package flexipfs

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
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

func TestPutValueWithAttr_EncodesSpacesAsPercent20(t *testing.T) {
	var rawQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v0/dht/peerlist":
			_, _ = w.Write([]byte(`"peer1"`))
		case "/api/v0/dht/putvaluewithattr":
			rawQuery = r.URL.RawQuery
			_ = json.NewEncoder(w).Encode(map[string]any{"CID_file": "baf_test"})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)

	c := New(srv.URL + "/api/v0")
	_, err := c.PutValueWithAttr(context.Background(), `{"title":"CI Board"}`, []string{"post_1"}, []string{"board_bbs.general"})
	if err != nil {
		t.Fatalf("PutValueWithAttr: %v", err)
	}
	if strings.Contains(rawQuery, "+") {
		t.Fatalf("raw query must not contain '+': %q", rawQuery)
	}
	if !strings.Contains(rawQuery, "%20") {
		t.Fatalf("raw query should contain '%%20' for spaces: %q", rawQuery)
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

func TestGetValue_ReadsFromGetDataFile_OnDownloadingPlaceholder(t *testing.T) {
	baseDir := t.TempDir()
	getDataDir := filepath.Join(baseDir, "getdata")
	if err := os.MkdirAll(getDataDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v0/dht/getvalue" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		cid := r.URL.Query().Get("cid")
		if cid == "" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		if err := os.WriteFile(filepath.Join(getDataDir, cid+".txt"), []byte("Bhi"), 0o644); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_, _ = w.Write([]byte(strconv.Quote("Downloading chunks for CID:" + cid)))
	}))
	t.Cleanup(srv.Close)

	c := New(srv.URL + "/api/v0")
	c.BaseDir = baseDir
	b, err := c.GetValue(context.Background(), "baf_test")
	if err != nil {
		t.Fatalf("GetValue: %v", err)
	}
	if string(b) != "hi" {
		t.Fatalf("value mismatch: %q", string(b))
	}
}

func TestDecodeGetDataValue_StripsYLengthPrefix(t *testing.T) {
	b := []byte{'Y', 0x00, 0x02, 'h', 'i'}
	if got := string(decodeGetDataValue(b)); got != "hi" {
		t.Fatalf("decode mismatch: %q", got)
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
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	t.Cleanup(cancel)
	_, err := c.PutValueWithAttr(ctx, "v", []string{"post_1"}, []string{"board_bbs.general"})
	if err == nil {
		t.Fatalf("expected error")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected deadline exceeded, got: %v", err)
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

func TestPutValueWithAttr_FallsBackWithoutAttrs_OnUnknownMultihashType(t *testing.T) {
	var (
		putCalls int
		gotAttrs []string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v0/dht/peerlist":
			_, _ = w.Write([]byte(`"peer1"`))
		case "/api/v0/dht/putvaluewithattr":
			putCalls++
			gotAttrs = append(gotAttrs, r.URL.Query().Get("attrs"))
			if putCalls == 1 {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte("Unknown Multihash type: 0xffffffff"))
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
	if putCalls != 2 {
		t.Fatalf("expected 2 put attempts, got %d", putCalls)
	}
	if len(gotAttrs) != 2 || gotAttrs[0] == "" || gotAttrs[1] != "" {
		t.Fatalf("attrs should be sent first then omitted: %#v", gotAttrs)
	}
}

func TestPutValueWithAttr_FallsBackWithoutAttrs_OnEOF(t *testing.T) {
	var (
		putCalls int
		gotAttrs []string
	)
	errCh := make(chan error, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v0/dht/peerlist":
			_, _ = w.Write([]byte(`"peer1"`))
		case "/api/v0/dht/putvaluewithattr":
			putCalls++
			attrs := r.URL.Query().Get("attrs")
			gotAttrs = append(gotAttrs, attrs)
			if attrs != "" {
				hj, ok := w.(http.Hijacker)
				if !ok {
					select {
					case errCh <- errors.New("ResponseWriter does not implement Hijacker"):
					default:
					}
					w.WriteHeader(http.StatusInternalServerError)
					return
				}
				conn, _, err := hj.Hijack()
				if err != nil {
					select {
					case errCh <- err:
					default:
					}
					w.WriteHeader(http.StatusInternalServerError)
					return
				}
				_ = conn.Close()
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
	select {
	case err := <-errCh:
		t.Fatalf("handler error: %v", err)
	default:
	}
	if cid != "baf_test" {
		t.Fatalf("cid mismatch: %q", cid)
	}
	if putCalls != 2 {
		t.Fatalf("expected 2 put attempts, got %d", putCalls)
	}
	if len(gotAttrs) != 2 || gotAttrs[0] == "" || gotAttrs[1] != "" {
		t.Fatalf("attrs should be sent first then omitted: %#v", gotAttrs)
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
