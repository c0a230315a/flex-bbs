package config

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

type TrustedIndexersFile struct {
	TrustedIndexers []string `json:"trustedIndexers"`
}

type TrustedIndexersStore struct {
	path string

	mu  sync.Mutex
	set map[string]struct{}
}

func NewTrustedIndexersStore(path string) *TrustedIndexersStore {
	return &TrustedIndexersStore{
		path: path,
		set:  make(map[string]struct{}),
	}
}

func NormalizeBaseURL(baseURL string) (string, error) {
	baseURL = strings.TrimSpace(baseURL)
	if baseURL == "" {
		return "", fmt.Errorf("base URL is empty")
	}
	u, err := url.Parse(baseURL)
	if err != nil {
		return "", err
	}
	if !u.IsAbs() {
		return "", fmt.Errorf("base URL must be an absolute URL")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("scheme must be http or https")
	}
	if u.Host == "" {
		return "", fmt.Errorf("host is empty")
	}

	// Base URL should be stable.
	u.Host = strings.ToLower(u.Host)
	u.RawQuery = ""
	u.ForceQuery = false
	u.Fragment = ""
	u.RawFragment = ""
	u.Path = strings.TrimRight(u.Path, "/")

	normalized := u.String()
	normalized = strings.TrimRight(normalized, "/")
	return normalized, nil
}

func (s *TrustedIndexersStore) Load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}

	b, err := os.ReadFile(s.path)
	if os.IsNotExist(err) {
		s.set = make(map[string]struct{})
		return s.saveLocked()
	}
	if err != nil {
		return err
	}

	// Always re-load from disk (trusted_indexers.json can be updated by other processes,
	// e.g. `bbs-node add-trusted-indexer` from the TUI).
	s.set = make(map[string]struct{})
	if len(b) == 0 {
		return nil
	}

	var f TrustedIndexersFile
	if err := json.Unmarshal(b, &f); err != nil {
		return err
	}
	for _, raw := range f.TrustedIndexers {
		n, err := NormalizeBaseURL(raw)
		if err != nil {
			continue
		}
		s.set[n] = struct{}{}
	}
	return nil
}

func (s *TrustedIndexersStore) List() []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	out := make([]string, 0, len(s.set))
	for u := range s.set {
		out = append(out, u)
	}
	sort.Strings(out)
	return out
}

func (s *TrustedIndexersStore) Add(baseURL string) (bool, error) {
	n, err := NormalizeBaseURL(baseURL)
	if err != nil {
		return false, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.set[n]; ok {
		return false, nil
	}
	s.set[n] = struct{}{}
	return true, s.saveLocked()
}

func (s *TrustedIndexersStore) Remove(baseURL string) (bool, error) {
	n, err := NormalizeBaseURL(baseURL)
	if err != nil {
		return false, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.set[n]; !ok {
		return false, nil
	}
	delete(s.set, n)
	return true, s.saveLocked()
}

func (s *TrustedIndexersStore) saveLocked() error {
	list := make([]string, 0, len(s.set))
	for u := range s.set {
		list = append(list, u)
	}
	sort.Strings(list)

	f := TrustedIndexersFile{TrustedIndexers: list}
	b, err := json.MarshalIndent(&f, "", "  ")
	if err != nil {
		return err
	}

	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}
