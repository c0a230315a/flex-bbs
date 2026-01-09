package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestTrustedIndexersStore_Load_ReloadsFromDisk(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "trusted_indexers.json")

	s := NewTrustedIndexersStore(path)
	if err := s.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := s.List(); len(got) != 0 {
		t.Fatalf("expected empty, got=%v", got)
	}

	// Simulate another process updating the file.
	f := TrustedIndexersFile{
		TrustedIndexers: []string{
			"http://example.com:8080/",
			"https://EXAMPLE.com:8443/api",
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
	if len(got) != 2 || got[0] != "http://example.com:8080" || got[1] != "https://example.com:8443/api" {
		t.Fatalf("reload mismatch: %#v", got)
	}

	// And reflect removals too.
	f = TrustedIndexersFile{TrustedIndexers: nil}
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
