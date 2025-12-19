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
		got = r.URL.Query()
		_ = json.NewEncoder(w).Encode(map[string]any{
			"CID_file": "baf_test",
		})
	}))
	t.Cleanup(srv.Close)

	c := New(srv.URL + "/api/v0")
	value := `{"a":"x y","b":"1&2"}`
	cid, err := c.PutValueWithAttr(context.Background(), value, []string{"objtype_post", "version_1"}, []string{"board_bbs.general"})
	if err != nil {
		t.Fatalf("PutValueWithAttr: %v", err)
	}
	if cid != "baf_test" {
		t.Fatalf("cid mismatch: %q", cid)
	}
	if got.Get("value") != value {
		t.Fatalf("value query mismatch: got=%q want=%q", got.Get("value"), value)
	}
	if got.Get("attrs") != "objtype_post,version_1" {
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
