package main

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// ============================================================================
// Test Data Fixtures
// ============================================================================

const (
	testAuthorPubKey    = "ed25519:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	testCreatorPubKey   = "ed25519:fedcba9876543210fedcba9876543210fedcba9876543210fedcba9876543210"
	testThreadCID       = "bafyreithqq5r5cdyjlpqrh43s76qixevqfm4cbvhm7zx6p3abw75lviq4"
	testBoardID         = "bbs.general"
	testDisplayName     = "conecone"
)

// ============================================================================
// Post Tests
// ============================================================================

func TestNewPost(t *testing.T) {
	post := NewPost(testThreadCID, testAuthorPubKey, testDisplayName, "markdown", "こんにちは、テスト投稿です。")
	
	if post.Version != 1 {
		t.Errorf("Expected Version=1, got %d", post.Version)
	}
	if post.Type != "post" {
		t.Errorf("Expected Type='post', got '%s'", post.Type)
	}
	if post.ThreadID != testThreadCID {
		t.Errorf("Expected ThreadID=%s, got %s", testThreadCID, post.ThreadID)
	}
	if post.AuthorPubKey != testAuthorPubKey {
		t.Errorf("Expected AuthorPubKey=%s, got %s", testAuthorPubKey, post.AuthorPubKey)
	}
	if post.DisplayName != testDisplayName {
		t.Errorf("Expected DisplayName=%s, got %s", testDisplayName, post.DisplayName)
	}
	if post.Body.Format != "markdown" {
		t.Errorf("Expected Body.Format='markdown', got '%s'", post.Body.Format)
	}
}

func TestPostValidation(t *testing.T) {
	tests := []struct {
		name      string
		setupFn   func() *Post
		shouldErr bool
	}{
		{
			name: "Valid post",
			setupFn: func() *Post {
				return NewPost(testThreadCID, testAuthorPubKey, testDisplayName, "markdown", "content")
			},
			shouldErr: false,
		},
		{
			name: "Missing ThreadID",
			setupFn: func() *Post {
				p := NewPost(testThreadCID, testAuthorPubKey, testDisplayName, "markdown", "content")
				p.ThreadID = ""
				return p
			},
			shouldErr: true,
		},
		{
			name: "Missing AuthorPubKey",
			setupFn: func() *Post {
				p := NewPost(testThreadCID, testAuthorPubKey, testDisplayName, "markdown", "content")
				p.AuthorPubKey = ""
				return p
			},
			shouldErr: true,
		},
		{
			name: "Invalid PubKey format",
			setupFn: func() *Post {
				p := NewPost(testThreadCID, testAuthorPubKey, testDisplayName, "markdown", "content")
				p.AuthorPubKey = "invalid"
				return p
			},
			shouldErr: true,
		},
		{
			name: "Missing DisplayName",
			setupFn: func() *Post {
				p := NewPost(testThreadCID, testAuthorPubKey, testDisplayName, "markdown", "content")
				p.DisplayName = ""
				return p
			},
			shouldErr: true,
		},
		{
			name: "Missing Body.Format",
			setupFn: func() *Post {
				p := NewPost(testThreadCID, testAuthorPubKey, testDisplayName, "markdown", "content")
				p.Body.Format = ""
				return p
			},
			shouldErr: true,
		},
		{
			name: "Missing Body.Content",
			setupFn: func() *Post {
				p := NewPost(testThreadCID, testAuthorPubKey, testDisplayName, "markdown", "content")
				p.Body.Content = ""
				return p
			},
			shouldErr: true,
		},
		{
			name: "Invalid CreatedAt timestamp",
			setupFn: func() *Post {
				p := NewPost(testThreadCID, testAuthorPubKey, testDisplayName, "markdown", "content")
				p.CreatedAt = "invalid-timestamp"
				return p
			},
			shouldErr: true,
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			post := tt.setupFn()
			err := post.Validate()
			if (err != nil) != tt.shouldErr {
				t.Errorf("Expected error=%v, got error=%v (%v)", tt.shouldErr, err != nil, err)
			}
		})
	}
}

func TestPostJSON(t *testing.T) {
	post := NewPost(testThreadCID, testAuthorPubKey, testDisplayName, "markdown", "テスト本文")
	
	// Test ToJSON
	jsonBytes, err := post.ToJSON()
	if err != nil {
		t.Fatalf("ToJSON failed: %v", err)
	}
	
	// Test FromJSON
	post2, err := PostFromJSON(jsonBytes)
	if err != nil {
		t.Fatalf("PostFromJSON failed: %v", err)
	}
	
	if post2.Type != post.Type || post2.ThreadID != post.ThreadID || post2.DisplayName != post.DisplayName {
		t.Error("Deserialized Post does not match original")
	}
}

func TestPostCanonicalSignPayload(t *testing.T) {
	post := NewPost(testThreadCID, testAuthorPubKey, testDisplayName, "markdown", "content")
	
	payload := post.CanonicalSignPayload()
	
	// Check that payload starts with correct fields
	lines := strings.Split(payload, "\n")
	if len(lines) < 9 {
		t.Errorf("Expected at least 9 lines in payload, got %d", len(lines))
	}
	
	if lines[0] != "type=post" {
		t.Errorf("Expected first line 'type=post', got '%s'", lines[0])
	}
	if lines[1] != "version=1" {
		t.Errorf("Expected second line 'version=1', got '%s'", lines[1])
	}
	if !strings.HasPrefix(lines[2], "threadId=") {
		t.Errorf("Expected threadId field, got '%s'", lines[2])
	}
	
	// Verify payload is deterministic
	payload2 := post.CanonicalSignPayload()
	if payload != payload2 {
		t.Error("Canonical payload is not deterministic")
	}
}

// ============================================================================
// ThreadMeta Tests
// ============================================================================

func TestNewThreadMeta(t *testing.T) {
	threadID := "bafyreithqq5r5cdyjlpqrh43s76qixevqfm4cbvhm7zx6p3abw75lv123"
	rootPostCid := "bafyreithqq5r5cdyjlpqrh43s76qixevqfm4cbvhm7zx6p3abw75lv456"
	
	threadMeta := NewThreadMeta(threadID, testBoardID, "はじめてのスレッド", rootPostCid, testCreatorPubKey)
	
	if threadMeta.Version != 1 {
		t.Errorf("Expected Version=1, got %d", threadMeta.Version)
	}
	if threadMeta.Type != "threadMeta" {
		t.Errorf("Expected Type='threadMeta', got '%s'", threadMeta.Type)
	}
	if threadMeta.ThreadID != threadID {
		t.Errorf("Expected ThreadID=%s, got %s", threadID, threadMeta.ThreadID)
	}
	if threadMeta.BoardID != testBoardID {
		t.Errorf("Expected BoardID=%s, got %s", testBoardID, threadMeta.BoardID)
	}
}

func TestThreadMetaValidation(t *testing.T) {
	threadID := "bafyreithqq5r5cdyjlpqrh43s76qixevqfm4cbvhm7zx6p3abw75lv123"
	rootPostCid := "bafyreithqq5r5cdyjlpqrh43s76qixevqfm4cbvhm7zx6p3abw75lv456"
	
	tests := []struct {
		name      string
		setupFn   func() *ThreadMeta
		shouldErr bool
	}{
		{
			name: "Valid ThreadMeta",
			setupFn: func() *ThreadMeta {
				return NewThreadMeta(threadID, testBoardID, "Title", rootPostCid, testCreatorPubKey)
			},
			shouldErr: false,
		},
		{
			name: "Missing ThreadID",
			setupFn: func() *ThreadMeta {
				t := NewThreadMeta(threadID, testBoardID, "Title", rootPostCid, testCreatorPubKey)
				t.ThreadID = ""
				return t
			},
			shouldErr: true,
		},
		{
			name: "Missing BoardID",
			setupFn: func() *ThreadMeta {
				t := NewThreadMeta(threadID, testBoardID, "Title", rootPostCid, testCreatorPubKey)
				t.BoardID = ""
				return t
			},
			shouldErr: true,
		},
		{
			name: "Invalid CreatedBy format",
			setupFn: func() *ThreadMeta {
				t := NewThreadMeta(threadID, testBoardID, "Title", rootPostCid, testCreatorPubKey)
				t.CreatedBy = "invalid"
				return t
			},
			shouldErr: true,
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tm := tt.setupFn()
			err := tm.Validate()
			if (err != nil) != tt.shouldErr {
				t.Errorf("Expected error=%v, got error=%v (%v)", tt.shouldErr, err != nil, err)
			}
		})
	}
}

func TestThreadMetaJSON(t *testing.T) {
	threadID := "bafyreithqq5r5cdyjlpqrh43s76qixevqfm4cbvhm7zx6p3abw75lv123"
	rootPostCid := "bafyreithqq5r5cdyjlpqrh43s76qixevqfm4cbvhm7zx6p3abw75lv456"
	
	tm := NewThreadMeta(threadID, testBoardID, "Title", rootPostCid, testCreatorPubKey)
	
	// Test ToJSON
	jsonBytes, err := tm.ToJSON()
	if err != nil {
		t.Fatalf("ToJSON failed: %v", err)
	}
	
	// Test FromJSON
	tm2, err := ThreadMetaFromJSON(jsonBytes)
	if err != nil {
		t.Fatalf("ThreadMetaFromJSON failed: %v", err)
	}
	
	if tm2.Type != tm.Type || tm2.ThreadID != tm.ThreadID || tm2.BoardID != tm.BoardID {
		t.Error("Deserialized ThreadMeta does not match original")
	}
}

func TestThreadMetaCanonicalSignPayload(t *testing.T) {
	threadID := "bafyreithqq5r5cdyjlpqrh43s76qixevqfm4cbvhm7zx6p3abw75lv123"
	rootPostCid := "bafyreithqq5r5cdyjlpqrh43s76qixevqfm4cbvhm7zx6p3abw75lv456"
	
	tm := NewThreadMeta(threadID, testBoardID, "Title", rootPostCid, testCreatorPubKey)
	
	payload := tm.CanonicalSignPayload()
	
	lines := strings.Split(payload, "\n")
	if len(lines) < 8 {
		t.Errorf("Expected at least 8 lines in payload, got %d", len(lines))
	}
	
	if lines[0] != "type=threadMeta" {
		t.Errorf("Expected first line 'type=threadMeta', got '%s'", lines[0])
	}
	if lines[1] != "version=1" {
		t.Errorf("Expected second line 'version=1', got '%s'", lines[1])
	}
}

// ============================================================================
// BoardMeta Tests
// ============================================================================

func TestNewBoardMeta(t *testing.T) {
	boardMeta := NewBoardMeta(testBoardID, "雑談板", "実験用の雑談板", testCreatorPubKey)
	
	if boardMeta.Version != 1 {
		t.Errorf("Expected Version=1, got %d", boardMeta.Version)
	}
	if boardMeta.Type != "boardMeta" {
		t.Errorf("Expected Type='boardMeta', got '%s'", boardMeta.Type)
	}
	if boardMeta.BoardID != testBoardID {
		t.Errorf("Expected BoardID=%s, got %s", testBoardID, boardMeta.BoardID)
	}
	if boardMeta.LogHeadCid != nil {
		t.Errorf("Expected LogHeadCid=nil, got %v", boardMeta.LogHeadCid)
	}
}

func TestBoardMetaValidation(t *testing.T) {
	tests := []struct {
		name      string
		setupFn   func() *BoardMeta
		shouldErr bool
	}{
		{
			name: "Valid BoardMeta",
			setupFn: func() *BoardMeta {
				return NewBoardMeta(testBoardID, "Title", "Description", testCreatorPubKey)
			},
			shouldErr: false,
		},
		{
			name: "Missing BoardID",
			setupFn: func() *BoardMeta {
				b := NewBoardMeta(testBoardID, "Title", "Description", testCreatorPubKey)
				b.BoardID = ""
				return b
			},
			shouldErr: true,
		},
		{
			name: "Missing Title",
			setupFn: func() *BoardMeta {
				b := NewBoardMeta(testBoardID, "Title", "Description", testCreatorPubKey)
				b.Title = ""
				return b
			},
			shouldErr: true,
		},
		{
			name: "Invalid CreatedBy format",
			setupFn: func() *BoardMeta {
				b := NewBoardMeta(testBoardID, "Title", "Description", testCreatorPubKey)
				b.CreatedBy = "invalid"
				return b
			},
			shouldErr: true,
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bm := tt.setupFn()
			err := bm.Validate()
			if (err != nil) != tt.shouldErr {
				t.Errorf("Expected error=%v, got error=%v (%v)", tt.shouldErr, err != nil, err)
			}
		})
	}
}

func TestBoardMetaJSON(t *testing.T) {
	bm := NewBoardMeta(testBoardID, "Title", "Description", testCreatorPubKey)
	
	// Test ToJSON
	jsonBytes, err := bm.ToJSON()
	if err != nil {
		t.Fatalf("ToJSON failed: %v", err)
	}
	
	// Test FromJSON
	bm2, err := BoardMetaFromJSON(jsonBytes)
	if err != nil {
		t.Fatalf("BoardMetaFromJSON failed: %v", err)
	}
	
	if bm2.Type != bm.Type || bm2.BoardID != bm.BoardID || bm2.Title != bm.Title {
		t.Error("Deserialized BoardMeta does not match original")
	}
}

func TestBoardMetaCanonicalSignPayload(t *testing.T) {
	bm := NewBoardMeta(testBoardID, "Title", "Description", testCreatorPubKey)
	
	payload := bm.CanonicalSignPayload()
	
	lines := strings.Split(payload, "\n")
	if len(lines) < 8 {
		t.Errorf("Expected at least 8 lines in payload, got %d", len(lines))
	}
	
	if lines[0] != "type=boardMeta" {
		t.Errorf("Expected first line 'type=boardMeta', got '%s'", lines[0])
	}
}

// ============================================================================
// BoardLogEntry Tests
// ============================================================================

func TestNewBoardLogEntry(t *testing.T) {
	logEntry := NewBoardLogEntry(testBoardID, "createThread", testAuthorPubKey)
	
	if logEntry.Version != 1 {
		t.Errorf("Expected Version=1, got %d", logEntry.Version)
	}
	if logEntry.Type != "boardLogEntry" {
		t.Errorf("Expected Type='boardLogEntry', got '%s'", logEntry.Type)
	}
	if logEntry.BoardID != testBoardID {
		t.Errorf("Expected BoardID=%s, got %s", testBoardID, logEntry.BoardID)
	}
	if logEntry.Op != "createThread" {
		t.Errorf("Expected Op='createThread', got '%s'", logEntry.Op)
	}
}

func TestBoardLogEntryValidation(t *testing.T) {
	tests := []struct {
		name      string
		setupFn   func() *BoardLogEntry
		shouldErr bool
	}{
		{
			name: "Valid BoardLogEntry",
			setupFn: func() *BoardLogEntry {
				return NewBoardLogEntry(testBoardID, "createThread", testAuthorPubKey)
			},
			shouldErr: false,
		},
		{
			name: "Missing BoardID",
			setupFn: func() *BoardLogEntry {
				e := NewBoardLogEntry(testBoardID, "createThread", testAuthorPubKey)
				e.BoardID = ""
				return e
			},
			shouldErr: true,
		},
		{
			name: "Missing Op",
			setupFn: func() *BoardLogEntry {
				e := NewBoardLogEntry(testBoardID, "createThread", testAuthorPubKey)
				e.Op = ""
				return e
			},
			shouldErr: true,
		},
		{
			name: "Invalid Op",
			setupFn: func() *BoardLogEntry {
				e := NewBoardLogEntry(testBoardID, "createThread", testAuthorPubKey)
				e.Op = "invalidOp"
				return e
			},
			shouldErr: true,
		},
		{
			name: "Invalid AuthorPubKey format",
			setupFn: func() *BoardLogEntry {
				e := NewBoardLogEntry(testBoardID, "createThread", testAuthorPubKey)
				e.AuthorPubKey = "invalid"
				return e
			},
			shouldErr: true,
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ble := tt.setupFn()
			err := ble.Validate()
			if (err != nil) != tt.shouldErr {
				t.Errorf("Expected error=%v, got error=%v (%v)", tt.shouldErr, err != nil, err)
			}
		})
	}
}

func TestBoardLogEntryJSON(t *testing.T) {
	ble := NewBoardLogEntry(testBoardID, "createThread", testAuthorPubKey)
	
	// Test ToJSON
	jsonBytes, err := ble.ToJSON()
	if err != nil {
		t.Fatalf("ToJSON failed: %v", err)
	}
	
	// Test FromJSON
	ble2, err := BoardLogEntryFromJSON(jsonBytes)
	if err != nil {
		t.Fatalf("BoardLogEntryFromJSON failed: %v", err)
	}
	
	if ble2.Type != ble.Type || ble2.BoardID != ble.BoardID || ble2.Op != ble.Op {
		t.Error("Deserialized BoardLogEntry does not match original")
	}
}

func TestBoardLogEntryCanonicalSignPayload(t *testing.T) {
	ble := NewBoardLogEntry(testBoardID, "createThread", testAuthorPubKey)
	
	payload := ble.CanonicalSignPayload()
	
	lines := strings.Split(payload, "\n")
	if len(lines) < 13 {
		t.Errorf("Expected at least 13 lines in payload, got %d", len(lines))
	}
	
	if lines[0] != "type=boardLogEntry" {
		t.Errorf("Expected first line 'type=boardLogEntry', got '%s'", lines[0])
	}
}

// ============================================================================
// Integration Tests
// ============================================================================

func TestFullPostWorkflow(t *testing.T) {
	// Create a post
	post := NewPost(testThreadCID, testAuthorPubKey, testDisplayName, "markdown", "Test content")
	
	// Validate
	if err := post.Validate(); err != nil {
		t.Fatalf("Post validation failed: %v", err)
	}
	
	// Serialize to JSON
	jsonStr, err := post.ToJSONString()
	if err != nil {
		t.Fatalf("Post ToJSONString failed: %v", err)
	}
	
	// Deserialize from JSON
	post2, err := PostFromJSONString(jsonStr)
	if err != nil {
		t.Fatalf("PostFromJSONString failed: %v", err)
	}
	
	// Validate deserialized post
	if err := post2.Validate(); err != nil {
		t.Fatalf("Deserialized Post validation failed: %v", err)
	}
	
	// Generate canonical payload
	payload := post2.CanonicalSignPayload()
	if payload == "" {
		t.Error("Canonical payload is empty")
	}
}

func TestJSONStructure(t *testing.T) {
	post := NewPost(testThreadCID, testAuthorPubKey, testDisplayName, "markdown", "content")
	
	jsonBytes, _ := post.ToJSON()
	var m map[string]interface{}
	if err := json.Unmarshal(jsonBytes, &m); err != nil {
		t.Fatalf("Failed to unmarshal JSON: %v", err)
	}
	
	expectedFields := []string{"version", "type", "threadId", "authorPubKey", "displayName", "body", "createdAt"}
	for _, field := range expectedFields {
		if _, ok := m[field]; !ok {
			t.Errorf("Expected field '%s' not found in JSON", field)
		}
	}
}

func TestNullableFields(t *testing.T) {
	post := NewPost(testThreadCID, testAuthorPubKey, testDisplayName, "markdown", "content")
	
	// Set optional fields to nil
	post.PostCid = nil
	post.ParentPostCid = nil
	post.EditedAt = nil
	
	jsonBytes, err := post.ToJSON()
	if err != nil {
		t.Fatalf("ToJSON failed: %v", err)
	}
	
	var m map[string]interface{}
	json.Unmarshal(jsonBytes, &m)
	
	// Verify null fields are present
	if m["postCid"] != nil {
		t.Error("postCid should be null")
	}
	if m["parentPostCid"] != nil {
		t.Error("parentPostCid should be null")
	}
}

func TestTimestampFormat(t *testing.T) {
	post := NewPost(testThreadCID, testAuthorPubKey, testDisplayName, "markdown", "content")
	
	// Verify timestamp is RFC3339Nano format
	_, err := time.Parse(time.RFC3339Nano, post.CreatedAt)
	if err != nil {
		t.Errorf("CreatedAt is not in RFC3339Nano format: %v", err)
	}
}
