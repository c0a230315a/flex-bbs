package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// ============================================================================
// JSON Serialization / Deserialization
// ============================================================================

// NewPost creates a new Post with current timestamp
func NewPost(threadID string, authorPubKey string, displayName string, format string, content string) *Post {
	return &Post{
		Version:      1,
		Type:         "post",
		PostCid:      nil,
		ThreadID:     threadID,
		ParentPostCid: nil,
		AuthorPubKey: authorPubKey,
		DisplayName:  displayName,
		Body: PostBody{
			Format:  format,
			Content: content,
		},
		Attachments: []Attachment{},
		CreatedAt:   time.Now().UTC().Format(time.RFC3339Nano),
		EditedAt:    nil,
		Meta: PostMeta{
			Tags:   []string{},
			Client: "",
		},
		Signature: "",
	}
}

// NewThreadMeta creates a new ThreadMeta
func NewThreadMeta(threadID string, boardID string, title string, rootPostCid string, createdBy string) *ThreadMeta {
	return &ThreadMeta{
		Version:     1,
		Type:        "threadMeta",
		ThreadID:    threadID,
		BoardID:     boardID,
		Title:       title,
		RootPostCid: rootPostCid,
		CreatedAt:   time.Now().UTC().Format(time.RFC3339Nano),
		CreatedBy:   createdBy,
		Meta: ThreadMeta_Meta{
			Tags: []string{},
		},
		Signature: "",
	}
}

// NewBoardMeta creates a new BoardMeta
func NewBoardMeta(boardID string, title string, description string, createdBy string) *BoardMeta {
	return &BoardMeta{
		Version:     1,
		Type:        "boardMeta",
		BoardID:     boardID,
		Title:       title,
		Description: description,
		LogHeadCid:  nil,
		CreatedAt:   time.Now().UTC().Format(time.RFC3339Nano),
		CreatedBy:   createdBy,
		Signature:   "",
	}
}

// NewBoardLogEntry creates a new BoardLogEntry
func NewBoardLogEntry(boardID string, op string, authorPubKey string) *BoardLogEntry {
	return &BoardLogEntry{
		Version:       1,
		Type:          "boardLogEntry",
		BoardID:       boardID,
		Op:            op,
		ThreadID:      nil,
		PostCid:       nil,
		OldPostCid:    nil,
		NewPostCid:    nil,
		TargetPostCid: nil,
		Reason:        nil,
		CreatedAt:     time.Now().UTC().Format(time.RFC3339Nano),
		AuthorPubKey:  authorPubKey,
		PrevLogCid:    nil,
		Signature:     "",
	}
}

// ToJSON serializes the Post to JSON bytes
func (p *Post) ToJSON() ([]byte, error) {
	if p == nil {
		return nil, errors.New("cannot serialize nil Post")
	}
	return json.Marshal(p)
}

// ToJSONString serializes the Post to JSON string
func (p *Post) ToJSONString() (string, error) {
	b, err := p.ToJSON()
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// FromJSON deserializes Post from JSON bytes
func (p *Post) FromJSON(data []byte) error {
	if p == nil {
		return errors.New("cannot deserialize into nil Post")
	}
	if err := json.Unmarshal(data, p); err != nil {
		return fmt.Errorf("failed to unmarshal Post: %w", err)
	}
	return nil
}

// FromJSONString deserializes Post from JSON string
func (p *Post) FromJSONString(jsonStr string) error {
	return p.FromJSON([]byte(jsonStr))
}

// PostFromJSON creates a new Post from JSON bytes
func PostFromJSON(data []byte) (*Post, error) {
	var p Post
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("failed to unmarshal Post: %w", err)
	}
	return &p, nil
}

// PostFromJSONString creates a new Post from JSON string
func PostFromJSONString(jsonStr string) (*Post, error) {
	return PostFromJSON([]byte(jsonStr))
}

// ToJSON serializes ThreadMeta to JSON bytes
func (t *ThreadMeta) ToJSON() ([]byte, error) {
	if t == nil {
		return nil, errors.New("cannot serialize nil ThreadMeta")
	}
	return json.Marshal(t)
}

// ToJSONString serializes ThreadMeta to JSON string
func (t *ThreadMeta) ToJSONString() (string, error) {
	b, err := t.ToJSON()
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// FromJSON deserializes ThreadMeta from JSON bytes
func (t *ThreadMeta) FromJSON(data []byte) error {
	if t == nil {
		return errors.New("cannot deserialize into nil ThreadMeta")
	}
	if err := json.Unmarshal(data, t); err != nil {
		return fmt.Errorf("failed to unmarshal ThreadMeta: %w", err)
	}
	return nil
}

// FromJSONString deserializes ThreadMeta from JSON string
func (t *ThreadMeta) FromJSONString(jsonStr string) error {
	return t.FromJSON([]byte(jsonStr))
}

// ThreadMetaFromJSON creates a new ThreadMeta from JSON bytes
func ThreadMetaFromJSON(data []byte) (*ThreadMeta, error) {
	var t ThreadMeta
	if err := json.Unmarshal(data, &t); err != nil {
		return nil, fmt.Errorf("failed to unmarshal ThreadMeta: %w", err)
	}
	return &t, nil
}

// ThreadMetaFromJSONString creates a new ThreadMeta from JSON string
func ThreadMetaFromJSONString(jsonStr string) (*ThreadMeta, error) {
	return ThreadMetaFromJSON([]byte(jsonStr))
}

// ToJSON serializes BoardMeta to JSON bytes
func (b *BoardMeta) ToJSON() ([]byte, error) {
	if b == nil {
		return nil, errors.New("cannot serialize nil BoardMeta")
	}
	return json.Marshal(b)
}

// ToJSONString serializes BoardMeta to JSON string
func (b *BoardMeta) ToJSONString() (string, error) {
	bytes, err := b.ToJSON()
	if err != nil {
		return "", err
	}
	return string(bytes), nil
}

// FromJSON deserializes BoardMeta from JSON bytes
func (b *BoardMeta) FromJSON(data []byte) error {
	if b == nil {
		return errors.New("cannot deserialize into nil BoardMeta")
	}
	if err := json.Unmarshal(data, b); err != nil {
		return fmt.Errorf("failed to unmarshal BoardMeta: %w", err)
	}
	return nil
}

// FromJSONString deserializes BoardMeta from JSON string
func (b *BoardMeta) FromJSONString(jsonStr string) error {
	return b.FromJSON([]byte(jsonStr))
}

// BoardMetaFromJSON creates a new BoardMeta from JSON bytes
func BoardMetaFromJSON(data []byte) (*BoardMeta, error) {
	var b BoardMeta
	if err := json.Unmarshal(data, &b); err != nil {
		return nil, fmt.Errorf("failed to unmarshal BoardMeta: %w", err)
	}
	return &b, nil
}

// BoardMetaFromJSONString creates a new BoardMeta from JSON string
func BoardMetaFromJSONString(jsonStr string) (*BoardMeta, error) {
	return BoardMetaFromJSON([]byte(jsonStr))
}

// ToJSON serializes BoardLogEntry to JSON bytes
func (e *BoardLogEntry) ToJSON() ([]byte, error) {
	if e == nil {
		return nil, errors.New("cannot serialize nil BoardLogEntry")
	}
	return json.Marshal(e)
}

// ToJSONString serializes BoardLogEntry to JSON string
func (e *BoardLogEntry) ToJSONString() (string, error) {
	b, err := e.ToJSON()
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// FromJSON deserializes BoardLogEntry from JSON bytes
func (e *BoardLogEntry) FromJSON(data []byte) error {
	if e == nil {
		return errors.New("cannot deserialize into nil BoardLogEntry")
	}
	if err := json.Unmarshal(data, e); err != nil {
		return fmt.Errorf("failed to unmarshal BoardLogEntry: %w", err)
	}
	return nil
}

// FromJSONString deserializes BoardLogEntry from JSON string
func (e *BoardLogEntry) FromJSONString(jsonStr string) error {
	return e.FromJSON([]byte(jsonStr))
}

// BoardLogEntryFromJSON creates a new BoardLogEntry from JSON bytes
func BoardLogEntryFromJSON(data []byte) (*BoardLogEntry, error) {
	var e BoardLogEntry
	if err := json.Unmarshal(data, &e); err != nil {
		return nil, fmt.Errorf("failed to unmarshal BoardLogEntry: %w", err)
	}
	return &e, nil
}

// BoardLogEntryFromJSONString creates a new BoardLogEntry from JSON string
func BoardLogEntryFromJSONString(jsonStr string) (*BoardLogEntry, error) {
	return BoardLogEntryFromJSON([]byte(jsonStr))
}

// ============================================================================
// Formatted JSON Output (Pretty Print)
// ============================================================================

// ToFormattedJSON returns indented JSON string
func (p *Post) ToFormattedJSON() (string, error) {
	b, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// ToFormattedJSON returns indented JSON string
func (t *ThreadMeta) ToFormattedJSON() (string, error) {
	b, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// ToFormattedJSON returns indented JSON string
func (b *BoardMeta) ToFormattedJSON() (string, error) {
	bytes, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		return "", err
	}
	return string(bytes), nil
}

// ToFormattedJSON returns indented JSON string
func (e *BoardLogEntry) ToFormattedJSON() (string, error) {
	b, err := json.MarshalIndent(e, "", "  ")
	if err != nil {
		return "", err
	}
	return string(b), nil
}
