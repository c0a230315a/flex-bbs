package storage

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf16"

	"flex-bbs/backend-go/bbs/flexipfs"
	"flex-bbs/backend-go/bbs/types"
)

const (
	// Flexible-IPFS expects attrs in the form "name_<int>" (name_value).
	// Values with multiple '_' (e.g. "objtype_boardmeta_version_1") cause server-side parse errors.
	AttrObjTypeBoardMetaV1     = "boardmeta_1"
	AttrObjTypeThreadMetaV1    = "threadmeta_1"
	AttrObjTypeBoardLogEntryV1 = "boardlogentry_1"
	AttrObjTypePostV1          = "post_1"
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
	return s.saveJSON(ctx, []string{AttrObjTypePostV1}, []string{TagBoardThread(boardID, p.ThreadID)}, p)
}

func (s *Storage) LoadPost(ctx context.Context, cid string) (*types.Post, error) {
	var p types.Post
	if err := s.loadJSON(ctx, cid, &p); err != nil {
		return nil, err
	}
	return &p, nil
}

func (s *Storage) SaveThreadMeta(ctx context.Context, m *types.ThreadMeta) (string, error) {
	cid, err := s.saveJSON(ctx, []string{AttrObjTypeThreadMetaV1}, []string{TagBoard(m.BoardID)}, m)
	if err != nil {
		return "", err
	}
	_, _ = s.saveJSON(ctx, []string{AttrObjTypeThreadMetaV1}, []string{TagBoardThread(m.BoardID, cid)}, m)
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
	return s.saveJSON(ctx, []string{AttrObjTypeBoardMetaV1}, []string{TagBoard(m.BoardID)}, m)
}

func (s *Storage) LoadBoardMeta(ctx context.Context, cid string) (*types.BoardMeta, error) {
	var m types.BoardMeta
	if err := s.loadJSON(ctx, cid, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

func (s *Storage) SaveBoardLogEntry(ctx context.Context, e *types.BoardLogEntry) (string, error) {
	return s.saveJSON(ctx, []string{AttrObjTypeBoardLogEntryV1}, []string{TagBoardThread(e.BoardID, e.ThreadID)}, e)
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
	b = escapeJSONNonASCII(b)
	return s.Flex.PutValueWithAttr(ctx, string(b), attrs, tags)
}

func (s *Storage) loadJSON(ctx context.Context, cid string, out any) error {
	b, err := s.Flex.GetValue(ctx, cid)
	if err != nil {
		return err
	}

	tryUnmarshal := func(payload []byte) error {
		if err := json.Unmarshal(payload, out); err != nil {
			trim := bytes.TrimSpace(payload)
			// Only include a short preview when the payload clearly isn't JSON (e.g. "Not Found"),
			// to avoid leaking post bodies into logs/errors.
			if len(trim) > 0 && trim[0] != '{' && trim[0] != '[' {
				preview := trim
				if len(preview) > 160 {
					preview = preview[:160]
				}
				preview = bytes.ReplaceAll(preview, []byte{'\r'}, []byte{' '})
				preview = bytes.ReplaceAll(preview, []byte{'\n'}, []byte{' '})
				preview = bytes.ReplaceAll(preview, []byte{'\t'}, []byte{' '})
				return fmt.Errorf("unmarshal cid=%s: %w (value_preview=%q)", cid, err, string(preview))
			}
			return fmt.Errorf("unmarshal cid=%s: %w", cid, err)
		}
		return nil
	}

	if err := tryUnmarshal(b); err != nil {
		// Flexible-IPFS may write a corrupted/incomplete getdata cache file and refuse to overwrite it.
		// Retry once by forcing a fresh fetch (invalidates any existing <cid>.txt cache file first).
		fresh, ferr := s.Flex.GetValueFresh(ctx, cid)
		if ferr != nil {
			return fmt.Errorf("%v (fresh fetch failed: %w)", err, ferr)
		}
		uerr := tryUnmarshal(fresh)
		if uerr == nil {
			return nil
		}
		return fmt.Errorf("%v (after fresh fetch: %v)", err, uerr)
	}
	return nil
}

func escapeJSONNonASCII(b []byte) []byte {
	for _, c := range b {
		if c >= 0x80 {
			return escapeJSONNonASCIIImpl(b)
		}
	}
	return b
}

func escapeJSONNonASCIIImpl(b []byte) []byte {
	var sb strings.Builder
	sb.Grow(len(b))

	for _, r := range string(b) {
		if r <= 0x7F {
			sb.WriteByte(byte(r))
			continue
		}
		if r <= 0xFFFF {
			sb.WriteString(`\u`)
			appendHex4(&sb, uint16(r))
			continue
		}
		hi, lo := utf16.EncodeRune(r)
		sb.WriteString(`\u`)
		appendHex4(&sb, uint16(hi))
		sb.WriteString(`\u`)
		appendHex4(&sb, uint16(lo))
	}
	return []byte(sb.String())
}

func appendHex4(sb *strings.Builder, v uint16) {
	const hex = "0123456789ABCDEF"
	sb.WriteByte(hex[(v>>12)&0xF])
	sb.WriteByte(hex[(v>>8)&0xF])
	sb.WriteByte(hex[(v>>4)&0xF])
	sb.WriteByte(hex[v&0xF])
}
