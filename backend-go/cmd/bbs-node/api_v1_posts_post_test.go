package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type createPostReqBody struct {
	Version      int     `json:"version"`
	ThreadID     string  `json:"threadId"`
	ParentPostID *string `json:"parentPostCid,omitempty"`
	AuthorPubKey string  `json:"authorPubKey"`
	DisplayName  string  `json:"displayName"`
	Body         struct {
		Format  string `json:"format"`
		Content string `json:"content"`
	} `json:"body"`
	CreatedAt  string `json:"createdAt"`
	Signature  string `json:"signature"`
	SignatureB string `json:"signatureBase64,omitempty"`
}

func TestCreatePost_OK(t *testing.T) {
	resetPostsStore()

	kp, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair: %v", err)
	}

	var req createPostReqBody
	req.Version = 1
	req.ThreadID = "thread-1"
	req.AuthorPubKey = kp.Public
	req.DisplayName = "alice"
	req.Body.Format = "md"
	req.Body.Content = "hello"
	req.CreatedAt = "2025-01-01T00:00:00Z"

	payload := PostSignPayload(req.Version, req.ThreadID, nil, req.AuthorPubKey, req.DisplayName, req.Body.Format, req.Body.Content, req.CreatedAt)
	sig, err := SignPayloadEd25519(kp.Private, payload)
	if err != nil {
		t.Fatalf("SignPayloadEd25519: %v", err)
	}
	req.Signature = sig

	b, _ := json.Marshal(req)
	r := httptest.NewRequest(http.MethodPost, "/api/v1/posts", bytes.NewReader(b))
	w := httptest.NewRecorder()

	handleCreatePost(w, r)
	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status=%d body=%s", resp.StatusCode, w.Body.String())
	}

	var out createPostResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.PostCid == "" || !strings.HasPrefix(out.PostCid, "sha256:") {
		t.Fatalf("postCid=%q", out.PostCid)
	}
}

func TestCreatePost_InvalidSignature_Unauthorized(t *testing.T) {
	resetPostsStore()

	kpAuthor, _ := GenerateKeyPair()
	kpOther, _ := GenerateKeyPair()

	var req createPostReqBody
	req.Version = 1
	req.ThreadID = "thread-1"
	req.AuthorPubKey = kpAuthor.Public
	req.DisplayName = "alice"
	req.Body.Format = "md"
	req.Body.Content = "hello"
	req.CreatedAt = "2025-01-01T00:00:00Z"

	payload := PostSignPayload(req.Version, req.ThreadID, nil, req.AuthorPubKey, req.DisplayName, req.Body.Format, req.Body.Content, req.CreatedAt)
	sig, _ := SignPayloadEd25519(kpOther.Private, payload)
	req.Signature = sig

	b, _ := json.Marshal(req)
	r := httptest.NewRequest(http.MethodPost, "/api/v1/posts", bytes.NewReader(b))
	w := httptest.NewRecorder()

	handleCreatePost(w, r)
	if w.Result().StatusCode != http.StatusUnauthorized {
		t.Fatalf("status=%d body=%s", w.Result().StatusCode, w.Body.String())
	}
}

func TestCreatePost_BadRequest_MissingFields(t *testing.T) {
	resetPostsStore()

	b := []byte(`{"version":1}`)
	r := httptest.NewRequest(http.MethodPost, "/api/v1/posts", bytes.NewReader(b))
	w := httptest.NewRecorder()

	handleCreatePost(w, r)
	if w.Result().StatusCode != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", w.Result().StatusCode, w.Body.String())
	}
}

func TestCreatePost_MethodNotAllowed(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/api/v1/posts", nil)
	w := httptest.NewRecorder()
	handleCreatePost(w, r)
	if w.Result().StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("status=%d", w.Result().StatusCode)
	}
}
