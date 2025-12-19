package storage

import (
	"context"
	"encoding/json"
	"fmt"

	"flex-bbs/backend-go/bbs/flexipfs"
	"flex-bbs/backend-go/bbs/types"
)

const (
	AttrObjTypeBoardMeta     = "objtype_boardmeta"
	AttrObjTypeThreadMeta    = "objtype_threadmeta"
	AttrObjTypeBoardLogEntry = "objtype_boardlogentry"
	AttrObjTypePost          = "objtype_post"
	AttrVersion1             = "version_1"
)

type Storage struct {
	Flex *flexipfs.Client
}

func New(flex *flexipfs.Client) *Storage {
	return &Storage{Flex: flex}
}

func TagBoard(boardID string) string {
	return fmt.Sprintf("board_%s", boardID)
}

func TagBoardThread(boardID, threadID string) string {
	return fmt.Sprintf("board_%s-thread_%s", boardID, threadID)
}

func (s *Storage) SavePost(ctx context.Context, boardID string, p *types.Post) (string, error) {
	return s.saveJSON(ctx, []string{AttrObjTypePost, AttrVersion1}, []string{TagBoardThread(boardID, p.ThreadID)}, p)
}

func (s *Storage) LoadPost(ctx context.Context, cid string) (*types.Post, error) {
	var p types.Post
	if err := s.loadJSON(ctx, cid, &p); err != nil {
		return nil, err
	}
	return &p, nil
}

func (s *Storage) SaveThreadMeta(ctx context.Context, m *types.ThreadMeta) (string, error) {
	cid, err := s.saveJSON(ctx, []string{AttrObjTypeThreadMeta, AttrVersion1}, []string{TagBoard(m.BoardID)}, m)
	if err != nil {
		return "", err
	}
	_, _ = s.saveJSON(ctx, []string{AttrObjTypeThreadMeta, AttrVersion1}, []string{TagBoardThread(m.BoardID, cid)}, m)
	return cid, nil
}

func (s *Storage) LoadThreadMeta(ctx context.Context, cid string) (*types.ThreadMeta, error) {
	var m types.ThreadMeta
	if err := s.loadJSON(ctx, cid, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

func (s *Storage) SaveBoardMeta(ctx context.Context, m *types.BoardMeta) (string, error) {
	return s.saveJSON(ctx, []string{AttrObjTypeBoardMeta, AttrVersion1}, []string{TagBoard(m.BoardID)}, m)
}

func (s *Storage) LoadBoardMeta(ctx context.Context, cid string) (*types.BoardMeta, error) {
	var m types.BoardMeta
	if err := s.loadJSON(ctx, cid, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

func (s *Storage) SaveBoardLogEntry(ctx context.Context, e *types.BoardLogEntry) (string, error) {
	return s.saveJSON(ctx, []string{AttrObjTypeBoardLogEntry, AttrVersion1}, []string{TagBoardThread(e.BoardID, e.ThreadID)}, e)
}

func (s *Storage) LoadBoardLogEntry(ctx context.Context, cid string) (*types.BoardLogEntry, error) {
	var e types.BoardLogEntry
	if err := s.loadJSON(ctx, cid, &e); err != nil {
		return nil, err
	}
	return &e, nil
}

func (s *Storage) saveJSON(ctx context.Context, attrs []string, tags []string, v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return s.Flex.PutValueWithAttr(ctx, string(b), attrs, tags)
}

func (s *Storage) loadJSON(ctx context.Context, cid string, out any) error {
	b, err := s.Flex.GetValue(ctx, cid)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(b, out); err != nil {
		return fmt.Errorf("unmarshal cid=%s: %w", cid, err)
	}
	return nil
}
