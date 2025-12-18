package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// 差し替えメモ:
// 合体時にテストを使わない(リポジトリに残さない)運用なら、このファイルは削除してOK。

func TestTombstonePost_OK(t *testing.T) {
	postsStoreMu.Lock()
	postsStore = map[string]storedPost{}
	postsStoreMu.Unlock()

	kp, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair: %v", err)
	}

	oldCid := "sha256:old"
	seedPostForTests(storedPost{
		PostCid:      oldCid,
		ThreadID:     "thread-1",
		AuthorPubKey: kp.Public,
		BodyFormat:   "text/plain",
		BodyContent:  "hello",
		CreatedAt:    "2025-12-19T00:00:00Z",
	})

	tombstonedAt := "2025-12-19T01:00:00Z"
	payload := PostTombstoneSignPayload(1, "thread-1", oldCid, kp.Public, tombstonedAt)
	sig, err := SignPayloadEd25519(kp.Private, payload)
	if err != nil {
		t.Fatalf("SignPayloadEd25519: %v", err)
	}

	reqBody := map[string]any{
		"version":      1,
		"threadId":     "thread-1",
		"authorPubKey": kp.Public,
		"tombstonedAt": tombstonedAt,
		"signature":    sig,
		"meta":         map[string]any{"ignored": true},
	}
	b, _ := json.Marshal(reqBody)

	r := httptest.NewRequest(http.MethodPost, "/api/v1/posts/"+oldCid+"/tombstone", bytes.NewReader(b))
	w := httptest.NewRecorder()
	handlePostActions(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}

	var resp tombstonePostResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp.OldPostCid != oldCid {
		t.Fatalf("oldPostCid=%q", resp.OldPostCid)
	}
	if resp.NewPostCid == "" {
		t.Fatalf("expected newPostCid")
	}

	postsStoreMu.RLock()
	p, ok := postsStore[resp.NewPostCid]
	postsStoreMu.RUnlock()
	if !ok {
		t.Fatalf("expected new post saved")
	}
	if !p.IsTombstoned {
		t.Fatalf("expected IsTombstoned")
	}
}

func TestTombstonePost_InvalidSignature(t *testing.T) {
	postsStoreMu.Lock()
	postsStore = map[string]storedPost{}
	postsStoreMu.Unlock()

	kp, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair: %v", err)
	}

	oldCid := "sha256:old"
	seedPostForTests(storedPost{
		PostCid:      oldCid,
		ThreadID:     "thread-1",
		AuthorPubKey: kp.Public,
		BodyFormat:   "text/plain",
		BodyContent:  "hello",
		CreatedAt:    "2025-12-19T00:00:00Z",
	})

	tombstonedAt := "2025-12-19T01:00:00Z"
	payload := PostTombstoneSignPayload(1, "thread-1", oldCid, kp.Public, tombstonedAt)
	sig, err := SignPayloadEd25519(kp.Private, payload)
	if err != nil {
		t.Fatalf("SignPayloadEd25519: %v", err)
	}

	// tombstonedAt を変えて署名不一致にする。
	reqBody := map[string]any{
		"version":      1,
		"threadId":     "thread-1",
		"authorPubKey": kp.Public,
		"tombstonedAt": "2025-12-19T02:00:00Z",
		"signature":    sig,
	}
	b, _ := json.Marshal(reqBody)

	r := httptest.NewRequest(http.MethodPost, "/api/v1/posts/"+oldCid+"/tombstone", bytes.NewReader(b))
	w := httptest.NewRecorder()
	handlePostActions(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestTombstonePost_NotFound(t *testing.T) {
	postsStoreMu.Lock()
	postsStore = map[string]storedPost{}
	postsStoreMu.Unlock()

	reqBody := map[string]any{
		"version":      1,
		"threadId":     "thread-1",
		"authorPubKey": "ed25519:xxx",
		"tombstonedAt": "2025-12-19T01:00:00Z",
		"signature":    "x",
	}
	b, _ := json.Marshal(reqBody)

	r := httptest.NewRequest(http.MethodPost, "/api/v1/posts/sha256:nope/tombstone", bytes.NewReader(b))
	w := httptest.NewRecorder()
	handlePostActions(w, r)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}
