package model

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)


// =========================
// Common helpers
// =========================


func parseRFC3339(s string) error {
	_, err := time.Parse(time.RFC3339, s)
	return err
}


func joinLines(lines ...string) string {
	return strings.Join(lines, "\n")
}


// =========================
// Post (投稿データ構造)
// =========================


type PostBody struct {
	Format string `json:"format"`
	Content string `json:"content"`
}


type Post struct {
	Version int `json:"version"`
	Type string `json:"type"`
	ThreadID string `json:"threadId"`
	ParentPostID *string `json:"parentPostCid"`
	AuthorPubKey string `json:"authorPubKey"`
	DisplayName string `json:"displayName"`
	Body PostBody `json:"body"`
	CreatedAt string `json:"createdAt"`
	Signature string `json:"signature"`
}

func (p *Post) Validate() error {
	if p.Version != 1 || p.Type != "post" {
		return errors.New("invalid post header")
	}
	if p.ThreadID == "" || p.AuthorPubKey == "" {
		return errors.New("missing required post field")
	}
	if p.Body.Format == "" || p.Body.Content == "" {
		return errors.New("invalid post body")
	}
	return parseRFC3339(p.CreatedAt)
}


// SignaturePayload defines the exact signing payload (spec-defined)
func (p *Post) SignaturePayload() string {
	parent := ""
	if p.ParentPostID != nil {
		parent = *p.ParentPostID
	}
	return joinLines(
		"type=post",
		"version=1",
		"threadId="+p.ThreadID,
		"parentPostCid="+parent,
		"authorPubKey="+p.AuthorPubKey,
		"displayName="+p.DisplayName,
		"body.format="+p.Body.Format,
		"body.content="+p.Body.Content,
		"createdAt="+p.CreatedAt,
	)
}

type ThreadMeta struct {
	Type string `json:"type"`
	ThreadID string `json:"threadId"`
	BoardID string `json:"boardId"`
	Title string `json:"title"`
	RootPostCID string `json:"rootPostCid"`
	CreatedAt string `json:"createdAt"`
	CreatedBy string `json:"createdBy"`
	Signature string `json:"signature"`
}


func (t *ThreadMeta) Validate() error {
	if t.Version != 1 || t.Type != "threadMeta" {
		return errors.New("invalid threadMeta header")
	}
	if t.ThreadID == "" || t.BoardID == "" {
		return errors.New("missing threadMeta id")
	}
	return parseRFC3339(t.CreatedAt)
}


// SignaturePayload defines signing payload for ThreadMeta (extended spec)
func (t *ThreadMeta) SignaturePayload() string {
	return joinLines(
		"type=threadMeta",
		"version=1",
		"threadId="+t.ThreadID,
		"boardId="+t.BoardID,
		"title="+t.Title,
		"rootPostCid="+t.RootPostCID,
		"createdAt="+t.CreatedAt,
		"createdBy="+t.CreatedBy,
	)
}


func (t *ThreadMeta) Validate() error {() error {
	if t.Version != 1 || t.Type != "threadMeta" {
		return errors.New("invalid threadMeta header")
	}
	if t.ThreadID == "" || t.BoardID == "" {
		return errors.New("missing threadMeta id")
	}
	return parseRFC3339(t.CreatedAt)
}

// =========================
// BoardMeta
// =========================

type BoardMeta struct {
	Version int `json:"version"`
	Type string `json:"type"`
	BoardID string `json:"boardId"`
	Title string `json:"title"`
	Desc string `json:"description"`
	LogHeadCID string `json:"logHeadCid"`
	CreatedAt string `json:"createdAt"`
	CreatedBy string `json:"createdBy"`
	Signature string `json:"signature"`
}


func (b *BoardMeta) Validate() error {
	if b.Version != 1 || b.Type != "boardMeta" {
		return errors.New("invalid boardMeta header")
	}
	if b.BoardID == "" {
		return errors.New("missing boardId")
	}
	return parseRFC3339(b.CreatedAt)
}


// SignaturePayload defines signing payload for BoardMeta (extended spec)
func (b *BoardMeta) SignaturePayload() string {
	return joinLines(
		"type=boardMeta",
		"version=1",
		"boardId="+b.BoardID,
		"title="+b.Title,
		"description="+b.Desc,
		"logHeadCid="+b.LogHeadCID,
		"createdAt="+b.CreatedAt,
		"createdBy="+b.CreatedBy,
	)
}


func (b *BoardMeta) Validate() error {() error {
	if b.Version != 1 || b.Type != "boardMeta" {
		return errors.New("invalid boardMeta header")
	}
	if b.BoardID == "" {
		return errors.New("missing boardId")
	}
	return parseRFC3339(b.CreatedAt)
}

// =========================
// BoardLogEntry (操作ログ)
// =========================

type BoardLogEntry struct {
	Version int `json:"version"`
	Type string `json:"type"`
	BoardID string `json:"boardId"`
	Op string `json:"op"`
	ThreadID string `json:"threadId"`
	PostCID *string `json:"postCid"`
	CreatedAt string `json:"createdAt"`
	AuthorPubKey string `json:"authorPubKey"`
	PrevLogCID *string `json:"prevLogCid"`
	Signature string `json:"signature"`
}


func (e *BoardLogEntry) Validate() error {
	if e.Version != 1 || e.Type != "boardLogEntry" {
		return errors.New("invalid boardLogEntry header")
	}
	if e.BoardID == "" || e.Op == "" {
		return errors.New("missing log fields")
	}
	return parseRFC3339(e.CreatedAt)
}


func (e *BoardLogEntry) SignaturePayload() string {
	post := ""
	prev := ""
	if e.PostCID != nil {
		post = *e.PostCID
	}
	if e.PrevLogCID != nil {
		prev = *e.PrevLogCID
	}
	return joinLines(
		"type=boardLogEntry",
		"version=1",
		"boardId="+e.BoardID,
		"op="+e.Op,
		"threadId="+e.ThreadID,
		"postCid="+post,
		"createdAt="+e.CreatedAt,
		"authorPubKey="+e.AuthorPubKey,
		"prevLogCid="+prev,
	)
}


// =========================
// JSON helpers
// =========================


func ToJSON(v any) ([]byte, error) {
	return json.MarshalIndent(v, "", " ")
}


func FromJSON[T any](b []byte, out *T) error {
	return json.Unmarshal(b, out)
}


// =========================
// Signing helpers (Post / BoardLogEntry only)
// =========================


func Sign(priv ed25519.PrivateKey, payload string) string {
	sig := ed25519.Sign(priv, []byte(payload))
	return base64.StdEncoding.EncodeToString(sig)
}


func Verify(pub ed25519.PublicKey, payload, sigB64 string) bool {
	sig, err := base64.StdEncoding.DecodeString(sigB64)
	if err != nil {
		return false
	}
	return ed25519.Verify(pub, []byte(payload), sig)
}