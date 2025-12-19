package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

type testThreadGetter struct {
	resp GetThreadResponse
	err  error
}

func (g testThreadGetter) GetThread(ctx context.Context, threadID string) (GetThreadResponse, error) {
	if g.err != nil {
		return GetThreadResponse{}, g.err
	}
	if g.resp.Thread.ThreadID == "" {
		g.resp.Thread.ThreadID = threadID
	}
	return g.resp, nil
}

type jsonErr struct {
	Error string `json:"error"`
}

func TestGetThread_OK_DefaultGetter(t *testing.T) {
	orig := threadGetter
	defer func() { threadGetter = orig }()
	threadGetter = defaultThreadGetter{}

	r := httptest.NewRequest(http.MethodGet, "/api/v1/threads/thread-123", nil)
	w := httptest.NewRecorder()
	handleGetThread(w, r)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d body=%s", resp.StatusCode, w.Body.String())
	}

	var out GetThreadResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.Thread.ThreadID != "thread-123" {
		t.Fatalf("threadId=%q", out.Thread.ThreadID)
	}
}

func TestGetThread_NotFound_InvalidPath(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/api/v1/threads/", nil)
	w := httptest.NewRecorder()
	handleGetThread(w, r)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status=%d body=%s", resp.StatusCode, w.Body.String())
	}

	var out jsonErr
	_ = json.NewDecoder(resp.Body).Decode(&out)
	if out.Error == "" {
		t.Fatalf("expected error body")
	}
}

func TestGetThread_NotFound_ExtraSegment(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/api/v1/threads/a/b", nil)
	w := httptest.NewRecorder()
	handleGetThread(w, r)

	if w.Result().StatusCode != http.StatusNotFound {
		t.Fatalf("status=%d body=%s", w.Result().StatusCode, w.Body.String())
	}
}

func TestGetThread_MethodNotAllowed(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/api/v1/threads/thread-123", nil)
	w := httptest.NewRecorder()
	handleGetThread(w, r)

	if w.Result().StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("status=%d body=%s", w.Result().StatusCode, w.Body.String())
	}
}

func TestGetThread_ThreadNotFound_FromGetter(t *testing.T) {
	orig := threadGetter
	defer func() { threadGetter = orig }()
	threadGetter = testThreadGetter{err: ErrThreadNotFound}

	r := httptest.NewRequest(http.MethodGet, "/api/v1/threads/thread-404", nil)
	w := httptest.NewRecorder()
	handleGetThread(w, r)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status=%d body=%s", resp.StatusCode, w.Body.String())
	}
}

func TestGetThread_InternalError_FromGetter(t *testing.T) {
	orig := threadGetter
	defer func() { threadGetter = orig }()
	threadGetter = testThreadGetter{err: errors.New("boom")}

	r := httptest.NewRequest(http.MethodGet, "/api/v1/threads/thread-500", nil)
	w := httptest.NewRecorder()
	handleGetThread(w, r)

	if w.Result().StatusCode != http.StatusInternalServerError {
		t.Fatalf("status=%d body=%s", w.Result().StatusCode, w.Body.String())
	}
}
