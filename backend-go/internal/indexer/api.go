package indexer

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
)

// APIHandler はindexerの検索APIを処理します。
type APIHandler struct {
	db DB
}

// NewAPIHandler は新しいAPIハンドラーを作成します。
func NewAPIHandler(db DB) *APIHandler {
	return &APIHandler{db: db}
}

// RegisterRoutes はHTTPルーティングを登録します。
func (h *APIHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/v1/search/posts", h.handleSearchPosts)
	mux.HandleFunc("/api/v1/search/threads", h.handleSearchThreads)

	// boards
	mux.HandleFunc("/api/v1/boards", h.handleListBoards)
	mux.HandleFunc("/api/v1/boards/", func(w http.ResponseWriter, r *http.Request) {
		// /api/v1/boards/{boardID} or /api/v1/boards/{boardID}/threads
		path := strings.TrimPrefix(r.URL.Path, "/api/v1/boards/")
		parts := strings.Split(strings.Trim(path, "/"), "/")
		if len(parts) == 1 {
			h.handleBoardDetail(w, r.WithContext(context.WithValue(r.Context(), ctxKeyBoardID, parts[0])))
			return
		}
		if len(parts) == 2 && parts[1] == "threads" {
			h.handleBoardThreads(w, r.WithContext(context.WithValue(r.Context(), ctxKeyBoardID, parts[0])))
			return
		}
		http.NotFound(w, r)
	})

	// threads
	mux.HandleFunc("/api/v1/threads/", func(w http.ResponseWriter, r *http.Request) {
		// /api/v1/threads/{threadID} or /api/v1/threads/{threadID}/posts
		path := strings.TrimPrefix(r.URL.Path, "/api/v1/threads/")
		parts := strings.Split(strings.Trim(path, "/"), "/")
		if len(parts) == 1 {
			h.handleThreadDetail(w, r.WithContext(context.WithValue(r.Context(), ctxKeyThreadID, parts[0])))
			return
		}
		if len(parts) == 2 && parts[1] == "posts" {
			h.handleThreadPosts(w, r.WithContext(context.WithValue(r.Context(), ctxKeyThreadID, parts[0])))
			return
		}
		http.NotFound(w, r)
	})

	// posts
	mux.HandleFunc("/api/v1/posts/", func(w http.ResponseWriter, r *http.Request) {
		// /api/v1/posts/{postID}
		path := strings.TrimPrefix(r.URL.Path, "/api/v1/posts/")
		id := strings.Trim(path, "/")
		if id == "" {
			http.NotFound(w, r)
			return
		}
		h.handlePostDetail(w, r.WithContext(context.WithValue(r.Context(), ctxKeyPostID, id)))
	})
}

type ctxKey string

const (
	ctxKeyBoardID  ctxKey = "board_id"
	ctxKeyThreadID ctxKey = "thread_id"
	ctxKeyPostID   ctxKey = "post_id"
)

// 共通レスポンスヘルパー
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]any{
		"error": msg,
	})
}

// handleSearchPosts は投稿検索エンドポイントを処理します。
// GET /api/v1/search/posts?query=...&board_id=...&thread_id=...&author_id=...&limit=&offset=
func (h *APIHandler) handleSearchPosts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	q := r.URL.Query()
	req := &SearchPostsRequest{
		Query:    q.Get("query"),
		BoardID:  q.Get("board_id"),
		ThreadID: q.Get("thread_id"),
		AuthorID: q.Get("author_id"),
	}

	if limitStr := q.Get("limit"); limitStr != "" {
		if v, err := strconv.Atoi(limitStr); err == nil {
			req.Limit = v
		}
	}
	if offsetStr := q.Get("offset"); offsetStr != "" {
		if v, err := strconv.Atoi(offsetStr); err == nil {
			req.Offset = v
		}
	}

	resp, err := h.db.SearchPosts(r.Context(), req)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// handleSearchThreads はスレッド検索エンドポイントを処理します。
// GET /api/v1/search/threads?query=...&board_id=...&limit=&offset=
func (h *APIHandler) handleSearchThreads(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	q := r.URL.Query()
	req := &SearchThreadsRequest{
		Query:   q.Get("query"),
		BoardID: q.Get("board_id"),
	}
	if limitStr := q.Get("limit"); limitStr != "" {
		if v, err := strconv.Atoi(limitStr); err == nil {
			req.Limit = v
		}
	}
	if offsetStr := q.Get("offset"); offsetStr != "" {
		if v, err := strconv.Atoi(offsetStr); err == nil {
			req.Offset = v
		}
	}

	resp, err := h.db.SearchThreads(r.Context(), req)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// handleListBoards は掲示板リスト取得エンドポイントを処理します。
// GET /api/v1/boards
func (h *APIHandler) handleListBoards(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	boards, err := h.db.ListBoards(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"boards": boards,
	})
}

// handleBoardDetail は掲示板詳細エンドポイントを処理します。
// GET /api/v1/boards/{boardID}
func (h *APIHandler) handleBoardDetail(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	boardID, _ := r.Context().Value(ctxKeyBoardID).(string)
	if boardID == "" {
		writeError(w, http.StatusBadRequest, "missing board_id")
		return
	}
	b, err := h.db.GetBoard(r.Context(), boardID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if b == nil {
		writeError(w, http.StatusNotFound, "board not found")
		return
	}
	writeJSON(w, http.StatusOK, b)
}

// handleBoardThreads は掲示板のスレッドリストを返します。
// GET /api/v1/boards/{boardID}/threads
func (h *APIHandler) handleBoardThreads(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	boardID, _ := r.Context().Value(ctxKeyBoardID).(string)
	if boardID == "" {
		writeError(w, http.StatusBadRequest, "missing board_id")
		return
	}
	threads, err := h.db.ListThreadsByBoard(r.Context(), boardID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"threads": threads,
	})
}

// handleThreadDetail はスレッド詳細エンドポイントを処理します。
// GET /api/v1/threads/{threadID}
func (h *APIHandler) handleThreadDetail(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	threadID, _ := r.Context().Value(ctxKeyThreadID).(string)
	if threadID == "" {
		writeError(w, http.StatusBadRequest, "missing thread_id")
		return
	}
	th, err := h.db.GetThread(r.Context(), threadID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if th == nil {
		writeError(w, http.StatusNotFound, "thread not found")
		return
	}
	writeJSON(w, http.StatusOK, th)
}

// handleThreadPosts はスレッドの投稿リストを返します。
// GET /api/v1/threads/{threadID}/posts
func (h *APIHandler) handleThreadPosts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	threadID, _ := r.Context().Value(ctxKeyThreadID).(string)
	if threadID == "" {
		writeError(w, http.StatusBadRequest, "missing thread_id")
		return
	}
	posts, err := h.db.ListPostsByThread(r.Context(), threadID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"posts": posts,
	})
}

// handlePostDetail は投稿詳細エンドポイントを処理します。
// GET /api/v1/posts/{postID}
func (h *APIHandler) handlePostDetail(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	postID, _ := r.Context().Value(ctxKeyPostID).(string)
	if postID == "" {
		writeError(w, http.StatusBadRequest, "missing post_id")
		return
	}
	p, err := h.db.GetPost(r.Context(), postID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if p == nil {
		writeError(w, http.StatusNotFound, "post not found")
		return
	}
	writeJSON(w, http.StatusOK, p)
}
