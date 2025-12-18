package main

import "testing"

func TestResolveRole(t *testing.T) {
	cases := []struct {
		name     string
		input    string
		wantRole roleType
	}{
		{"client", "client", roleClient},
		{"indexer", "indexer", roleIndexer},
		{"archiver", "archiver", roleArchiver},
		{"full", "full", roleFull},
		{"unknown -> client", "unknown", roleClient},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := resolveRole(c.input)
			if got != c.wantRole {
				t.Fatalf("resolveRole(%q) = %q, want %q", c.input, got, c.wantRole)
			}
		})
	}
}

func TestFeaturesForRole(t *testing.T) {
	cases := []struct {
		name   string
		role   roleType
		client bool
		indexer bool
		archiver bool
	}{
		{"client", roleClient, true, false, false},
		{"indexer", roleIndexer, false, true, false},
		{"archiver", roleArchiver, false, false, true},
		{"full", roleFull, true, true, true},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := featuresForRole(c.role)
			if f.enableClient != c.client || f.enableIndexer != c.indexer || f.enableArchiver != c.archiver {
				t.Fatalf("featuresForRole(%q) = %+v, want client=%t indexer=%t archiver=%t", c.role, f, c.client, c.indexer, c.archiver)
			}
		})
	}
}
