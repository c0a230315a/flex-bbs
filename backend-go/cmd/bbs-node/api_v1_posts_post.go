package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
)

// --- API: POST /api/v1/posts (thread post) ---

// createPostRequest は POST /api/v1/posts の入力。
// NOTE: meta/attachments などが来ても無視してOK (署名対象外の想定)。
type createPostRequest struct {
	Version       int     `json:"version"`
	ThreadID      string  `json:"threadId"`
	ParentPostCid *string `json:"parentPostCid,omitempty"`
	AuthorPubKey  string  `json:"authorPubKey"`
	DisplayName   string  `json:"displayName"`
	Body          struct {
		Format  string `json:"format"`
		Content string `json:"content"`
	} `json:"body"`
	CreatedAt string `json:"createdAt"`

	// Signature は payload(PostSignPayload) を Ed25519 署名した base64(StdEncoding) 文字列。
	// 互換性のため signatureBase64 も受け付ける。
	Signature       string `json:"signature"`
	SignatureBase64 string `json:"signatureBase64"`

	// 差し替えメモ:
	// 合体(本実装)で API スキーマを共通パッケージ/モデルに寄せる場合は、この struct を移動/統合する。
}

// createPostResponse は POST /api/v1/posts の出力(暫定)。
// NOTE: 合体(本実装)時に CID 生成/永続化の仕様に合わせて変更する。
type createPostResponse struct {
	PostCid string `json:"postCid"`
}

// postStoreItem は暫定のインメモリ保存用。
type postStoreItem struct {
	PostCid string
	Req     createPostRequest
}

var (
	postsMu       sync.RWMutex
	postsByThread = map[string][]postStoreItem{}
)

func init() {
	// main.go を触らずに DefaultServeMux にルーティングを登録する。
	//
	// 差し替えメモ:
	// 合体(本実装)でルーター構成を整理する場合は、ここでの HandleFunc 登録を
	// 新しいルーティング層へ移す。
	http.HandleFunc("/api/v1/posts", handleCreatePost)
}

func handleCreatePost(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	var req createPostRequest
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}

	// 追加のJSONが続くのを防ぐ。
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		writeJSONError(w, http.StatusBadRequest, "invalid_json", "unexpected trailing JSON")
		return
	}

	req.ThreadID = strings.TrimSpace(req.ThreadID)
	req.AuthorPubKey = strings.TrimSpace(req.AuthorPubKey)
	req.DisplayName = strings.TrimSpace(req.DisplayName)
	req.Body.Format = strings.TrimSpace(req.Body.Format)
	// Body.Content は空白も意味を持つ可能性があるので TrimSpace しない。
	req.CreatedAt = strings.TrimSpace(req.CreatedAt)
	req.Signature = strings.TrimSpace(req.Signature)
	req.SignatureBase64 = strings.TrimSpace(req.SignatureBase64)

	if err := validateCreatePostRequest(req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	sig := req.Signature
	if sig == "" {
		sig = req.SignatureBase64
	}

	payload := PostSignPayload(
		req.Version,
		req.ThreadID,
		req.ParentPostCid,
		req.AuthorPubKey,
		req.DisplayName,
		req.Body.Format,
		req.Body.Content,
		req.CreatedAt,
	)

	ok, err := VerifyPayloadEd25519(req.AuthorPubKey, payload, sig)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid_signature", err.Error())
		return
	}
	if !ok {
		writeJSONError(w, http.StatusUnauthorized, "invalid_signature", "signature verification failed")
		return
	}

	// 差し替えメモ:
	// 合体(本実装)のタイミングで、ここは「本物のCID生成・永続化」に差し替える。
	// いまはとりあえず動かすため、署名ペイロードのSHA256を暫定CIDとして返す。
	sum := sha256.Sum256([]byte(payload))
	postCid := "sha256:" + hex.EncodeToString(sum[:])

	// 差し替えメモ:
	// 合体(本実装)のタイミングで、このインメモリ保存はストレージ層/FlexIPFS/DB に置き換える。
	postsMu.Lock()
	postsByThread[req.ThreadID] = append(postsByThread[req.ThreadID], postStoreItem{PostCid: postCid, Req: req})
	postsMu.Unlock()

	writeJSON(w, http.StatusCreated, createPostResponse{PostCid: postCid})
}

func validateCreatePostRequest(req createPostRequest) error {
	// 差し替えメモ:
	// 合体(本実装)で共通のバリデーション層を作る場合は、ここを共通関数へ寄せる。
	if req.Version <= 0 {
		return errors.New("version must be > 0")
	}
	if req.ThreadID == "" {
		return errors.New("threadId is required")
	}
	if req.AuthorPubKey == "" {
		return errors.New("authorPubKey is required")
	}
	if req.Body.Format == "" {
		return errors.New("body.format is required")
	}
	if req.Body.Content == "" {
		return errors.New("body.content is required")
	}
	if req.CreatedAt == "" {
		return errors.New("createdAt is required")
	}
	if !isRFC3339OrNano(req.CreatedAt) {
		return errors.New("createdAt must be RFC3339 or RFC3339Nano")
	}
	if req.Signature == "" && req.SignatureBase64 == "" {
		return errors.New("signature is required")
	}
	return nil
}

func isRFC3339OrNano(s string) bool {
	// 差し替えメモ:
	// 合体(本実装)で createdAt の仕様を厳密化/変更する場合は、ここ(許可するフォーマット)を調整する。
	if _, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return true
	}
	if _, err := time.Parse(time.RFC3339, s); err == nil {
		return true
	}
	return false
}

// resetPostsStore clears the in-memory posts store (used by tests).
//
// 差し替えメモ:
// 合体(本実装)でテスト無し運用なら、この関数自体を削除してOK。
func resetPostsStore() {
	postsMu.Lock()
	postsByThread = map[string][]postStoreItem{}
	postsMu.Unlock()
}

// --- small JSON helpers (local to this file) ---

type jsonErrorResponse struct {
	Error string `json:"error"`
	Code  string `json:"code"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("error encoding json: %v", err)
	}
}

func writeJSONError(w http.ResponseWriter, status int, code string, message string) {
	writeJSON(w, status, jsonErrorResponse{Error: message, Code: code})
}
