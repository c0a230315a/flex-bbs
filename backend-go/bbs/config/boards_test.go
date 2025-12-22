package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestBoardsStore_Load_ReloadsFromDisk(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "boards.json")

	s := NewBoardsStore(path)
	if err := s.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := s.List(); len(got) != 0 {
		t.Fatalf("expected empty boards, got=%v", got)
	}

	// Simulate another process updating the file.
	f := BoardsFile{
		Boards: []BoardRef{
			{BoardID: "bbs.test", BoardMetaCID: "baf_test"},
		},
	}
	b, err := json.MarshalIndent(&f, "", "  ")
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if err := s.Load(); err != nil {
		t.Fatalf("Load (reload): %v", err)
	}
	got := s.List()
	if len(got) != 1 || got[0].BoardID != "bbs.test" || got[0].BoardMetaCID != "baf_test" {
		t.Fatalf("reload mismatch: %#v", got)
	}

	// And reflect removals too.
	f = BoardsFile{Boards: nil}
	b, err = json.MarshalIndent(&f, "", "  ")
	if err != nil {
		t.Fatalf("Marshal (empty): %v", err)
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatalf("WriteFile (empty): %v", err)
	}
	if err := s.Load(); err != nil {
		t.Fatalf("Load (reload empty): %v", err)
	}
	if got := s.List(); len(got) != 0 {
		t.Fatalf("expected empty after reload, got=%v", got)
	}
}

