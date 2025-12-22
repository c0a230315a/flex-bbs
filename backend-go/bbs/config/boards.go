package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"sync"
)

type BoardRef struct {
	BoardID      string `json:"boardId"`
	BoardMetaCID string `json:"boardMetaCid"`
}

type BoardsFile struct {
	Boards []BoardRef `json:"boards"`
}

type BoardsStore struct {
	path string

	mu     sync.Mutex
	byID   map[string]string
}

func NewBoardsStore(path string) *BoardsStore {
	return &BoardsStore{
		path: path,
		byID: make(map[string]string),
	}
}

func (s *BoardsStore) Load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}

	b, err := os.ReadFile(s.path)
	if os.IsNotExist(err) {
		s.byID = make(map[string]string)
		return s.saveLocked()
	}
	if err != nil {
		return err
	}

	// Always re-load from disk (boards.json can be updated by other processes,
	// e.g. `bbs-node init-board` from the TUI).
	s.byID = make(map[string]string)
	if len(b) == 0 {
		return nil
	}

	var f BoardsFile
	if err := json.Unmarshal(b, &f); err != nil {
		return err
	}
	for _, br := range f.Boards {
		if br.BoardID == "" || br.BoardMetaCID == "" {
			continue
		}
		s.byID[br.BoardID] = br.BoardMetaCID
	}
	return nil
}

func (s *BoardsStore) List() []BoardRef {
	s.mu.Lock()
	defer s.mu.Unlock()

	out := make([]BoardRef, 0, len(s.byID))
	for id, cid := range s.byID {
		out = append(out, BoardRef{BoardID: id, BoardMetaCID: cid})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].BoardID < out[j].BoardID })
	return out
}

func (s *BoardsStore) Get(boardID string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cid, ok := s.byID[boardID]
	return cid, ok
}

func (s *BoardsStore) Upsert(boardID, boardMetaCID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.byID[boardID] = boardMetaCID
	return s.saveLocked()
}

func (s *BoardsStore) saveLocked() error {
	f := BoardsFile{Boards: make([]BoardRef, 0, len(s.byID))}
	for id, cid := range s.byID {
		f.Boards = append(f.Boards, BoardRef{BoardID: id, BoardMetaCID: cid})
	}
	sort.Slice(f.Boards, func(i, j int) bool { return f.Boards[i].BoardID < f.Boards[j].BoardID })

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
