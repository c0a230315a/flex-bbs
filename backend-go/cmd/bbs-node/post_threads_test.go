package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

type createThreadReqBody struct {
	Version      int    `json:"version"`
	BoardID      string `json:"boardId"`
	Title        string `json:"title"`
	CreatedAt    string `json:"createdAt"`
	AuthorPubKey string `json:"authorPubKey"`
	Signature    string `json:"signature"`
}

func TestCreateThread_OK(t *testing.T) {
	resetBoardThreadsForTests()
	defer resetBoardThreadsForTests()

	kp, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair: %v", err)
	}

	var req createThreadReqBody
	req.Version = 1
	req.BoardID = "board-1"
	req.Title = "hello"
	req.CreatedAt = "2025-01-01T00:00:00Z"
	req.AuthorPubKey = kp.Public

	payload := ThreadSignPayload(req.Version, req.BoardID, req.Title, req.CreatedAt, req.AuthorPubKey)
	sig, err := SignPayloadEd25519(kp.Private, payload)
	if err != nil {
		t.Fatalf("SignPayloadEd25519: %v", err)
	}
	req.Signature = sig

	b, _ := json.Marshal(req)
	r := httptest.NewRequest(http.MethodPost, "/api/v1/threads", bytes.NewReader(b))
	w := httptest.NewRecorder()
	handleCreateThread(w, r)

	resp := w.Result()
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status=%d body=%s", resp.StatusCode, w.Body.String())
	}

	var out createThreadResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.ThreadID == "" {
		t.Fatalf("threadId empty")
	}

	boardThreadsMu.RLock()
	threads := boardThreads[req.BoardID]
	boardThreadsMu.RUnlock()
	if len(threads) != 1 {
		t.Fatalf("threads len=%d", len(threads))
	}
	if threads[0].ID != out.ThreadID {
		t.Fatalf("stored id=%q resp id=%q", threads[0].ID, out.ThreadID)
	}
	if threads[0].Title != req.Title {
		t.Fatalf("stored title=%q", threads[0].Title)
	}
}

func TestCreateThread_InvalidSignature_Unauthorized(t *testing.T) {
	resetBoardThreadsForTests()
	defer resetBoardThreadsForTests()

	kpAuthor, _ := GenerateKeyPair()
	kpOther, _ := GenerateKeyPair()

	var req createThreadReqBody
	req.Version = 1
	req.BoardID = "board-1"
	req.Title = "hello"
	req.CreatedAt = "2025-01-01T00:00:00Z"
	req.AuthorPubKey = kpAuthor.Public

	payload := ThreadSignPayload(req.Version, req.BoardID, req.Title, req.CreatedAt, req.AuthorPubKey)
	sig, _ := SignPayloadEd25519(kpOther.Private, payload)
	req.Signature = sig

	b, _ := json.Marshal(req)
	r := httptest.NewRequest(http.MethodPost, "/api/v1/threads", bytes.NewReader(b))
	w := httptest.NewRecorder()
	handleCreateThread(w, r)

	if w.Result().StatusCode != http.StatusUnauthorized {
		t.Fatalf("status=%d body=%s", w.Result().StatusCode, w.Body.String())
	}
}

func TestCreateThread_BadRequest_MissingFields(t *testing.T) {
	b := []byte(`{"version":1}`)
	r := httptest.NewRequest(http.MethodPost, "/api/v1/threads", bytes.NewReader(b))
	w := httptest.NewRecorder()
	handleCreateThread(w, r)
	if w.Result().StatusCode != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", w.Result().StatusCode, w.Body.String())
	}
}

func TestCreateThread_MethodNotAllowed(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/api/v1/threads", nil)
	w := httptest.NewRecorder()
	handleCreateThread(w, r)
	if w.Result().StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("status=%d", w.Result().StatusCode)
	}
}
