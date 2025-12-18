package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadBoardMetaConfig_Default(t *testing.T) {
	boards, err := loadBoardMetaConfig("")
	if err != nil {
		t.Fatalf("loadBoardMetaConfig(""): %v", err)
	}
	if len(boards) == 0 {
		t.Fatalf("expected default boards, got 0")
	}
}

func TestLoadBoardMetaConfig_FromFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "boards.json")

	cfg := boardMetaConfigFile{
		Boards: []BoardMeta{
			{
				Version:     1,
				Type:        "boardMeta",
				BoardID:     "bbs.test",
				Title:       "テスト板",
				Description: "テスト用",
				CreatedAt:   time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC),
				CreatedBy:   "tester",
			},
		},
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("Create config: %v", err)
	}
	if err := json.NewEncoder(f).Encode(&cfg); err != nil {
		f.Close()
		t.Fatalf("Encode config: %v", err)
	}
	f.Close()

	boards, err := loadBoardMetaConfig(path)
	if err != nil {
		t.Fatalf("loadBoardMetaConfig: %v", err)
	}
	if len(boards) != 1 || boards[0].BoardID != "bbs.test" {
		t.Fatalf("unexpected boards: %+v", boards)
	}
}

func TestBoardsAPI_ListAndDetail(t *testing.T) {
	boards := []BoardMeta{
		{
			Version:     1,
			Type:        "boardMeta",
			BoardID:     "bbs.general",
			Title:       "雑談板",
			Description: "汎用雑談",
			CreatedAt:   time.Now().UTC(),
			CreatedBy:   "tester",
		},
	}

	mux := http.NewServeMux()
	registerBoardsHTTP(mux, boards)

	ts := httptest.NewServer(mux)
	defer ts.Close()

	// 一覧
	resp, err := http.Get(ts.URL + "/api/v1/boards")
	if err != nil {
		t.Fatalf("GET /api/v1/boards: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var list struct {
		Boards []BoardMeta `json:"boards"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(list.Boards) != 1 || list.Boards[0].BoardID != "bbs.general" {
		t.Fatalf("unexpected list: %+v", list.Boards)
	}

	// 詳細
	resp2, err := http.Get(ts.URL + "/api/v1/boards/bbs.general")
	if err != nil {
		t.Fatalf("GET /api/v1/boards/bbs.general: %v", err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp2.StatusCode)
	}
	var detail BoardMeta
	if err := json.NewDecoder(resp2.Body).Decode(&detail); err != nil {
		t.Fatalf("decode detail: %v", err)
	}
	if detail.BoardID != "bbs.general" {
		t.Fatalf("detail.BoardID = %q, want %q", detail.BoardID, "bbs.general")
	}

	// 存在しない板
	resp3, err := http.Get(ts.URL + "/api/v1/boards/unknown")
	if err != nil {
		t.Fatalf("GET /api/v1/boards/unknown: %v", err)
	}
	defer resp3.Body.Close()
	if resp3.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp3.StatusCode)
	}
}
