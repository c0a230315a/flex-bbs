package main

import (
	"bytes"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ============================================================================
// Data Models based on Flexible-IPFS BBS specification v0.1
// ============================================================================

// Post: 投稿データモデル
// 1 Post = 1 JSON = 1 CID
// 署名対象フィールド（順序厳格）:
//   type, version, threadId, parentPostCid, authorPubKey, displayName,
//   body.format, body.content, createdAt
type Post struct {
	Version      int        `json:"version"`              // Currently 1
	Type         string     `json:"type"`                 // "post"
	PostCid      *string    `json:"postCid"`              // null initially
	ThreadID     string     `json:"threadId"`             // ThreadMeta CID
	ParentPostCid *string   `json:"parentPostCid"`        // null or parent Post CID
	AuthorPubKey string     `json:"authorPubKey"`         // "ed25519:xxxx"
	DisplayName  string     `json:"displayName"`          // Author display name
	Body         PostBody   `json:"body"`
	Attachments  []Attachment `json:"attachments"`        // May be omitted if empty
	CreatedAt    string     `json:"createdAt"`            // RFC3339Nano format
	EditedAt     *string    `json:"editedAt"`             // null if not edited
	Meta         PostMeta   `json:"meta"`                 // Not signed
	Signature    string     `json:"signature"`            // base64(ed25519-signature)
}

// PostBody: 投稿本体
type PostBody struct {
	Format  string `json:"format"`  // "markdown", "plain", etc.
	Content string `json:"content"` // The actual message
}

// PostMeta: 投稿メタデータ（署名対象外）
type PostMeta struct {
	Tags   []string `json:"tags,omitempty"`
	Client string   `json:"client,omitempty"` // e.g., "bbs-csharp/0.1.0"
}

// Attachment: ファイル添付情報
type Attachment struct {
	CID  string `json:"cid"`
	MIME string `json:"mime"` // e.g., "image/png"
}

// ThreadMeta: スレッド情報モデル
// 署名対象フィールド（順序厳格）:
//   type, version, threadId, boardId, title, rootPostCid, createdAt, createdBy
type ThreadMeta struct {
	Version      int       `json:"version"`         // Currently 1
	Type         string    `json:"type"`            // "threadMeta"
	ThreadID     string    `json:"threadId"`        // This object's CID
	BoardID      string    `json:"boardId"`         // Board identifier
	Title        string    `json:"title"`           // Thread title
	RootPostCid  string    `json:"rootPostCid"`     // Root Post CID
	CreatedAt    string    `json:"createdAt"`       // RFC3339Nano format
	CreatedBy    string    `json:"createdBy"`       // Author public key
	Meta         ThreadMeta_Meta `json:"meta"`      // Not signed
	Signature    string    `json:"signature"`       // base64(ed25519-signature)
}

// ThreadMeta_Meta: スレッドメタデータ（署名対象外）
type ThreadMeta_Meta struct {
	Tags []string `json:"tags,omitempty"`
}

// BoardMeta: 掲示板情報モデル
// 署名対象フィールド（順序厳格）:
//   type, version, boardId, title, description, logHeadCid, createdAt, createdBy
type BoardMeta struct {
	Version      int       `json:"version"`         // Currently 1
	Type         string    `json:"type"`            // "boardMeta"
	BoardID      string    `json:"boardId"`         // Board identifier
	Title        string    `json:"title"`           // Board title
	Description  string    `json:"description"`     // Board description
	LogHeadCid   *string   `json:"logHeadCid"`      // Latest BoardLogEntry CID or null
	CreatedAt    string    `json:"createdAt"`       // RFC3339Nano format
	CreatedBy    string    `json:"createdBy"`       // Creator public key
	Signature    string    `json:"signature"`       // base64(ed25519-signature)
}

// BoardLogEntry: 操作ログエントリモデル
// 片方向リスト: latest → ... → prevLogCid
// 署名対象フィールド（順序厳格）:
//   type, version, boardId, op, threadId, postCid, oldPostCid, newPostCid, targetPostCid,
//   reason, createdAt, authorPubKey, prevLogCid
type BoardLogEntry struct {
	Version       int       `json:"version"`         // Currently 1
	Type          string    `json:"type"`            // "boardLogEntry"
	BoardID       string    `json:"boardId"`         // Board identifier
	Op            string    `json:"op"`              // "createThread", "addPost", "editPost", "tombstonePost"
	ThreadID      *string   `json:"threadId"`        // ThreadMeta CID or null
	PostCid       *string   `json:"postCid"`         // Post CID or null
	OldPostCid    *string   `json:"oldPostCid"`      // For editPost: old Post CID or null
	NewPostCid    *string   `json:"newPostCid"`      // For editPost: new Post CID or null
	TargetPostCid *string   `json:"targetPostCid"`   // For tombstonePost: deleted Post CID or null
	Reason        *string   `json:"reason"`          // Optional reason for tombstone
	CreatedAt     string    `json:"createdAt"`       // RFC3339Nano format
	AuthorPubKey  string    `json:"authorPubKey"`    // Operator public key
	PrevLogCid    *string   `json:"prevLogCid"`      // Previous log entry CID or null
	Signature     string    `json:"signature"`       // base64(ed25519-signature)
}

// ============================================================================
// Validation
// ============================================================================

var (
	ErrInvalidVersion    = errors.New("invalid version")
	ErrInvalidType       = errors.New("invalid type")
	ErrMissingField      = errors.New("missing required field")
	ErrInvalidCID        = errors.New("invalid CID format")
	ErrInvalidPubKey     = errors.New("invalid public key format")
	ErrInvalidTimestamp  = errors.New("invalid timestamp format")
)

// ValidatePost validates Post data structure
func (p *Post) Validate() error {
	if p == nil {
		return errors.New("Post is nil")
	}
	if p.Version != 1 {
		return fmt.Errorf("Post.Version: %w (got %d)", ErrInvalidVersion, p.Version)
	}
	if p.Type != "post" {
		return fmt.Errorf("Post.Type: %w (expected 'post', got '%s')", ErrInvalidType, p.Type)
	}
	if p.ThreadID == "" {
		return fmt.Errorf("Post.ThreadID: %w", ErrMissingField)
	}
	if p.AuthorPubKey == "" {
		return fmt.Errorf("Post.AuthorPubKey: %w", ErrMissingField)
	}
	if !strings.HasPrefix(p.AuthorPubKey, "ed25519:") {
		return fmt.Errorf("Post.AuthorPubKey: %w", ErrInvalidPubKey)
	}
	if p.DisplayName == "" {
		return fmt.Errorf("Post.DisplayName: %w", ErrMissingField)
	}
	if p.Body.Format == "" {
		return fmt.Errorf("Post.Body.Format: %w", ErrMissingField)
	}
	if p.Body.Content == "" {
		return fmt.Errorf("Post.Body.Content: %w", ErrMissingField)
	}
	if p.CreatedAt == "" {
		return fmt.Errorf("Post.CreatedAt: %w", ErrMissingField)
	}
	if _, err := time.Parse(time.RFC3339Nano, p.CreatedAt); err != nil {
		return fmt.Errorf("Post.CreatedAt: %w (%v)", ErrInvalidTimestamp, err)
	}
	return nil
}

// ValidateThreadMeta validates ThreadMeta data structure
func (t *ThreadMeta) Validate() error {
	if t == nil {
		return errors.New("ThreadMeta is nil")
	}
	if t.Version != 1 {
		return fmt.Errorf("ThreadMeta.Version: %w (got %d)", ErrInvalidVersion, t.Version)
	}
	if t.Type != "threadMeta" {
		return fmt.Errorf("ThreadMeta.Type: %w (expected 'threadMeta', got '%s')", ErrInvalidType, t.Type)
	}
	if t.ThreadID == "" {
		return fmt.Errorf("ThreadMeta.ThreadID: %w", ErrMissingField)
	}
	if t.BoardID == "" {
		return fmt.Errorf("ThreadMeta.BoardID: %w", ErrMissingField)
	}
	if t.Title == "" {
		return fmt.Errorf("ThreadMeta.Title: %w", ErrMissingField)
	}
	if t.RootPostCid == "" {
		return fmt.Errorf("ThreadMeta.RootPostCid: %w", ErrMissingField)
	}
	if t.CreatedAt == "" {
		return fmt.Errorf("ThreadMeta.CreatedAt: %w", ErrMissingField)
	}
	if _, err := time.Parse(time.RFC3339Nano, t.CreatedAt); err != nil {
		return fmt.Errorf("ThreadMeta.CreatedAt: %w (%v)", ErrInvalidTimestamp, err)
	}
	if t.CreatedBy == "" {
		return fmt.Errorf("ThreadMeta.CreatedBy: %w", ErrMissingField)
	}
	if !strings.HasPrefix(t.CreatedBy, "ed25519:") {
		return fmt.Errorf("ThreadMeta.CreatedBy: %w", ErrInvalidPubKey)
	}
	return nil
}

// ValidateBoardMeta validates BoardMeta data structure
func (b *BoardMeta) Validate() error {
	if b == nil {
		return errors.New("BoardMeta is nil")
	}
	if b.Version != 1 {
		return fmt.Errorf("BoardMeta.Version: %w (got %d)", ErrInvalidVersion, b.Version)
	}
	if b.Type != "boardMeta" {
		return fmt.Errorf("BoardMeta.Type: %w (expected 'boardMeta', got '%s')", ErrInvalidType, b.Type)
	}
	if b.BoardID == "" {
		return fmt.Errorf("BoardMeta.BoardID: %w", ErrMissingField)
	}
	if b.Title == "" {
		return fmt.Errorf("BoardMeta.Title: %w", ErrMissingField)
	}
	if b.CreatedAt == "" {
		return fmt.Errorf("BoardMeta.CreatedAt: %w", ErrMissingField)
	}
	if _, err := time.Parse(time.RFC3339Nano, b.CreatedAt); err != nil {
		return fmt.Errorf("BoardMeta.CreatedAt: %w (%v)", ErrInvalidTimestamp, err)
	}
	if b.CreatedBy == "" {
		return fmt.Errorf("BoardMeta.CreatedBy: %w", ErrMissingField)
	}
	if !strings.HasPrefix(b.CreatedBy, "ed25519:") {
		return fmt.Errorf("BoardMeta.CreatedBy: %w", ErrInvalidPubKey)
	}
	return nil
}

// ValidateBoardLogEntry validates BoardLogEntry data structure
func (e *BoardLogEntry) Validate() error {
	if e == nil {
		return errors.New("BoardLogEntry is nil")
	}
	if e.Version != 1 {
		return fmt.Errorf("BoardLogEntry.Version: %w (got %d)", ErrInvalidVersion, e.Version)
	}
	if e.Type != "boardLogEntry" {
		return fmt.Errorf("BoardLogEntry.Type: %w (expected 'boardLogEntry', got '%s')", ErrInvalidType, e.Type)
	}
	if e.BoardID == "" {
		return fmt.Errorf("BoardLogEntry.BoardID: %w", ErrMissingField)
	}
	if e.Op == "" {
		return fmt.Errorf("BoardLogEntry.Op: %w", ErrMissingField)
	}
	validOps := map[string]bool{"createThread": true, "addPost": true, "editPost": true, "tombstonePost": true}
	if !validOps[e.Op] {
		return fmt.Errorf("BoardLogEntry.Op: %w (got '%s')", ErrInvalidType, e.Op)
	}
	if e.CreatedAt == "" {
		return fmt.Errorf("BoardLogEntry.CreatedAt: %w", ErrMissingField)
	}
	if _, err := time.Parse(time.RFC3339Nano, e.CreatedAt); err != nil {
		return fmt.Errorf("BoardLogEntry.CreatedAt: %w (%v)", ErrInvalidTimestamp, err)
	}
	if e.AuthorPubKey == "" {
		return fmt.Errorf("BoardLogEntry.AuthorPubKey: %w", ErrMissingField)
	}
	if !strings.HasPrefix(e.AuthorPubKey, "ed25519:") {
		return fmt.Errorf("BoardLogEntry.AuthorPubKey: %w", ErrInvalidPubKey)
	}
	return nil
}


// ============================================================================
// Signing Payload Generation (Deterministic)
// ============================================================================

// CanonicalSignPayload generates the deterministic signing payload for a Post.
// Signature fields (in strict order):
//   type, version, threadId, parentPostCid, authorPubKey, displayName,
//   body.format, body.content, createdAt
// Returns: text/plain with key=value pairs separated by newlines.
func (p *Post) CanonicalSignPayload() string {
	var buf bytes.Buffer
	buf.WriteString("type=post\n")
	buf.WriteString(fmt.Sprintf("version=%d\n", p.Version))
	buf.WriteString(fmt.Sprintf("threadId=%s\n", p.ThreadID))
	
	// parentPostCid: null -> empty string
	parentCid := ""
	if p.ParentPostCid != nil {
		parentCid = *p.ParentPostCid
	}
	buf.WriteString(fmt.Sprintf("parentPostCid=%s\n", parentCid))
	
	buf.WriteString(fmt.Sprintf("authorPubKey=%s\n", p.AuthorPubKey))
	buf.WriteString(fmt.Sprintf("displayName=%s\n", p.DisplayName))
	buf.WriteString(fmt.Sprintf("body.format=%s\n", p.Body.Format))
	buf.WriteString(fmt.Sprintf("body.content=%s\n", p.Body.Content))
	buf.WriteString(fmt.Sprintf("createdAt=%s", p.CreatedAt)) // No newline at end
	
	return buf.String()
}

// CanonicalSignPayload generates the deterministic signing payload for ThreadMeta.
// Signature fields (in strict order):
//   type, version, threadId, boardId, title, rootPostCid, createdAt, createdBy
func (t *ThreadMeta) CanonicalSignPayload() string {
	var buf bytes.Buffer
	buf.WriteString("type=threadMeta\n")
	buf.WriteString(fmt.Sprintf("version=%d\n", t.Version))
	buf.WriteString(fmt.Sprintf("threadId=%s\n", t.ThreadID))
	buf.WriteString(fmt.Sprintf("boardId=%s\n", t.BoardID))
	buf.WriteString(fmt.Sprintf("title=%s\n", t.Title))
	buf.WriteString(fmt.Sprintf("rootPostCid=%s\n", t.RootPostCid))
	buf.WriteString(fmt.Sprintf("createdAt=%s\n", t.CreatedAt))
	buf.WriteString(fmt.Sprintf("createdBy=%s", t.CreatedBy)) // No newline at end
	
	return buf.String()
}

// CanonicalSignPayload generates the deterministic signing payload for BoardMeta.
// Signature fields (in strict order):
//   type, version, boardId, title, description, logHeadCid, createdAt, createdBy
func (b *BoardMeta) CanonicalSignPayload() string {
	var buf bytes.Buffer
	buf.WriteString("type=boardMeta\n")
	buf.WriteString(fmt.Sprintf("version=%d\n", b.Version))
	buf.WriteString(fmt.Sprintf("boardId=%s\n", b.BoardID))
	buf.WriteString(fmt.Sprintf("title=%s\n", b.Title))
	buf.WriteString(fmt.Sprintf("description=%s\n", b.Description))
	
	// logHeadCid: null -> empty string
	logCid := ""
	if b.LogHeadCid != nil {
		logCid = *b.LogHeadCid
	}
	buf.WriteString(fmt.Sprintf("logHeadCid=%s\n", logCid))
	
	buf.WriteString(fmt.Sprintf("createdAt=%s\n", b.CreatedAt))
	buf.WriteString(fmt.Sprintf("createdBy=%s", b.CreatedBy)) // No newline at end
	
	return buf.String()
}

// CanonicalSignPayload generates the deterministic signing payload for BoardLogEntry.
// Signature fields (in strict order):
//   type, version, boardId, op, threadId, postCid, oldPostCid, newPostCid, targetPostCid,
//   reason, createdAt, authorPubKey, prevLogCid
func (e *BoardLogEntry) CanonicalSignPayload() string {
	var buf bytes.Buffer
	buf.WriteString("type=boardLogEntry\n")
	buf.WriteString(fmt.Sprintf("version=%d\n", e.Version))
	buf.WriteString(fmt.Sprintf("boardId=%s\n", e.BoardID))
	buf.WriteString(fmt.Sprintf("op=%s\n", e.Op))
	
	// Optional fields: null -> empty string
	threadId := ""
	if e.ThreadID != nil {
		threadId = *e.ThreadID
	}
	buf.WriteString(fmt.Sprintf("threadId=%s\n", threadId))
	
	postCid := ""
	if e.PostCid != nil {
		postCid = *e.PostCid
	}
	buf.WriteString(fmt.Sprintf("postCid=%s\n", postCid))
	
	oldPostCid := ""
	if e.OldPostCid != nil {
		oldPostCid = *e.OldPostCid
	}
	buf.WriteString(fmt.Sprintf("oldPostCid=%s\n", oldPostCid))
	
	newPostCid := ""
	if e.NewPostCid != nil {
		newPostCid = *e.NewPostCid
	}
	buf.WriteString(fmt.Sprintf("newPostCid=%s\n", newPostCid))
	
	targetPostCid := ""
	if e.TargetPostCid != nil {
		targetPostCid = *e.TargetPostCid
	}
	buf.WriteString(fmt.Sprintf("targetPostCid=%s\n", targetPostCid))
	
	reason := ""
	if e.Reason != nil {
		reason = *e.Reason
	}
	buf.WriteString(fmt.Sprintf("reason=%s\n", reason))
	
	buf.WriteString(fmt.Sprintf("createdAt=%s\n", e.CreatedAt))
	buf.WriteString(fmt.Sprintf("authorPubKey=%s\n", e.AuthorPubKey))
	
	prevLogCid := ""
	if e.PrevLogCid != nil {
		prevLogCid = *e.PrevLogCid
	}
	buf.WriteString(fmt.Sprintf("prevLogCid=%s", prevLogCid)) // No newline at end
	
	return buf.String()
}

// ============================================================================
// Signature Verification
// ============================================================================

// VerifySignature verifies an ed25519 signature against the canonical payload.
// pubKeyHex should be in format "ed25519:hexstring" or "ed25519:base64string"
// For now we support hex format.
func VerifySignature(pubKeyStr string, signatureBase64 string, payload string) (bool, error) {
	// Parse public key
	if !strings.HasPrefix(pubKeyStr, "ed25519:") {
		return false, fmt.Errorf("invalid public key format: must start with 'ed25519:'")
	}
	
	keyHex := strings.TrimPrefix(pubKeyStr, "ed25519:")
	
	// Decode hex to bytes (ed25519 public keys are 32 bytes)
	var pubKeyBytes [32]byte
	n, err := fmt.Sscanf(keyHex, "%32x", &pubKeyBytes)
	if err != nil || n != 1 {
		return false, fmt.Errorf("failed to parse public key hex: %w", err)
	}
	
	pubKey := ed25519.PublicKey(pubKeyBytes[:])
	
	// Decode signature from base64
	sigBytes := make([]byte, 0, ed25519.SignatureSize)
	// Try base64 decode (Note: ed25519 signatures are 64 bytes)
	// For now, we'll assume signature is hex-encoded as well for simplicity
	// In production, use proper base64 decoding
	var sigArray [64]byte
	n, err = fmt.Sscanf(signatureBase64, "%64x", &sigArray)
	if err != nil || n != 1 {
		return false, fmt.Errorf("failed to parse signature hex: %w", err)
	}
	sigBytes = sigArray[:]
	
	// Verify
	return ed25519.Verify(pubKey, []byte(payload), sigBytes), nil
}
