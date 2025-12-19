package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type editPostReqBody struct {
	Version      int    `json:"version"`
	ThreadID     string `json:"threadId"`
	AuthorPubKey string `json:"authorPubKey"`
	Body         struct {
		Format  string `json:"format"`
		Content string `json:"content"`
	} `json:"body"`
	EditedAt  string `json:"editedAt"`
	Signature string `json:"signature"`
}

func TestEditPost_OK(t *testing.T) {
	resetPostsStoreForTests()

	kp, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair: %v", err)
	}

	oldCid := "oldcid"
	threadID := "thread-1"
	seedPostForTests(storedPost{
		PostCid:      oldCid,
		ThreadID:     threadID,
		AuthorPubKey: kp.Public,
		BodyFormat:   "md",
		BodyContent:  "hello",
		CreatedAt:    "2025-01-01T00:00:00Z",
	})

	var req editPostReqBody
	req.Version = 1
	req.ThreadID = threadID
	req.AuthorPubKey = kp.Public
	req.Body.Format = "md"
	req.Body.Content = "hello edited"
	req.EditedAt = "2025-01-02T00:00:00Z"

	payload := PostEditSignPayload(req.Version, req.ThreadID, oldCid, req.AuthorPubKey, req.Body.Format, req.Body.Content, req.EditedAt)
	sig, err := SignPayloadEd25519(kp.Private, payload)
	if err != nil {
		t.Fatalf("SignPayloadEd25519: %v", err)
	}
	req.Signature = sig

	b, _ := json.Marshal(req)
	r := httptest.NewRequest(http.MethodPost, "/api/v1/posts/"+oldCid+"/edit", bytes.NewReader(b))
	w := httptest.NewRecorder()

	handleEditPost(w, r)
	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d body=%s", resp.StatusCode, w.Body.String())
	}
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Fatalf("Content-Type=%q", ct)
	}

	var out editPostResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if out.OldPostCid != oldCid {
		t.Fatalf("OldPostCid=%q", out.OldPostCid)
	}
	if out.NewPostCid == "" || !strings.HasPrefix(out.NewPostCid, "sha256:") {
		t.Fatalf("NewPostCid=%q", out.NewPostCid)
	}

	postsStoreMu.RLock()
	p, ok := postsStore[out.NewPostCid]
	postsStoreMu.RUnlock()
	if !ok {
		t.Fatalf("new post not stored")
	}
	if p.BodyContent != req.Body.Content {
		t.Fatalf("stored content=%q", p.BodyContent)
	}
	if p.EditedAt != req.EditedAt {
		t.Fatalf("stored editedAt=%q", p.EditedAt)
	}
}

func TestEditPost_AuthorMismatch_Forbidden(t *testing.T) {
	resetPostsStoreForTests()

	kpAuthor, _ := GenerateKeyPair()
	kpOther, _ := GenerateKeyPair()

	oldCid := "oldcid"
	threadID := "thread-1"
	seedPostForTests(storedPost{
		PostCid:      oldCid,
		ThreadID:     threadID,
		AuthorPubKey: kpAuthor.Public,
		BodyFormat:   "md",
		BodyContent:  "hello",
		CreatedAt:    "2025-01-01T00:00:00Z",
	})

	var req editPostReqBody
	req.Version = 1
	req.ThreadID = threadID
	req.AuthorPubKey = kpOther.Public
	req.Body.Format = "md"
	req.Body.Content = "hello edited"
	req.EditedAt = "2025-01-02T00:00:00Z"

	payload := PostEditSignPayload(req.Version, req.ThreadID, oldCid, req.AuthorPubKey, req.Body.Format, req.Body.Content, req.EditedAt)
	sig, _ := SignPayloadEd25519(kpOther.Private, payload)
	req.Signature = sig

	b, _ := json.Marshal(req)
	r := httptest.NewRequest(http.MethodPost, "/api/v1/posts/"+oldCid+"/edit", bytes.NewReader(b))
	w := httptest.NewRecorder()

	handleEditPost(w, r)
	if w.Result().StatusCode != http.StatusForbidden {
		t.Fatalf("status=%d body=%s", w.Result().StatusCode, w.Body.String())
	}
}

func TestEditPost_InvalidSignature_Unauthorized(t *testing.T) {
	resetPostsStoreForTests()

	kpAuthor, _ := GenerateKeyPair()
	kpOther, _ := GenerateKeyPair()

	oldCid := "oldcid"
	threadID := "thread-1"
	seedPostForTests(storedPost{
		PostCid:      oldCid,
		ThreadID:     threadID,
		AuthorPubKey: kpAuthor.Public,
		BodyFormat:   "md",
		BodyContent:  "hello",
		CreatedAt:    "2025-01-01T00:00:00Z",
	})

	var req editPostReqBody
	req.Version = 1
	req.ThreadID = threadID
	req.AuthorPubKey = kpAuthor.Public
	req.Body.Format = "md"
	req.Body.Content = "hello edited"
	req.EditedAt = "2025-01-02T00:00:00Z"

	payload := PostEditSignPayload(req.Version, req.ThreadID, oldCid, req.AuthorPubKey, req.Body.Format, req.Body.Content, req.EditedAt)
	sig, _ := SignPayloadEd25519(kpOther.Private, payload)
	req.Signature = sig

	b, _ := json.Marshal(req)
	r := httptest.NewRequest(http.MethodPost, "/api/v1/posts/"+oldCid+"/edit", bytes.NewReader(b))
	w := httptest.NewRecorder()

	handleEditPost(w, r)
	if w.Result().StatusCode != http.StatusUnauthorized {
		t.Fatalf("status=%d body=%s", w.Result().StatusCode, w.Body.String())
	}
}

func TestEditPost_NotFound(t *testing.T) {
	resetPostsStoreForTests()

	kp, _ := GenerateKeyPair()

	oldCid := "missing"
	threadID := "thread-1"

	var req editPostReqBody
	req.Version = 1
	req.ThreadID = threadID
	req.AuthorPubKey = kp.Public
	req.Body.Format = "md"
	req.Body.Content = "hello edited"
	req.EditedAt = "2025-01-02T00:00:00Z"

	payload := PostEditSignPayload(req.Version, req.ThreadID, oldCid, req.AuthorPubKey, req.Body.Format, req.Body.Content, req.EditedAt)
	sig, _ := SignPayloadEd25519(kp.Private, payload)
	req.Signature = sig

	b, _ := json.Marshal(req)
	r := httptest.NewRequest(http.MethodPost, "/api/v1/posts/"+oldCid+"/edit", bytes.NewReader(b))
	w := httptest.NewRecorder()

	handleEditPost(w, r)
	if w.Result().StatusCode != http.StatusNotFound {
		t.Fatalf("status=%d body=%s", w.Result().StatusCode, w.Body.String())
	}
}
