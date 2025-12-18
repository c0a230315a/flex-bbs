package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// --- API: POST /api/v1/threads (create thread) ---

// createThreadRequest は POST /api/v1/threads の入力。
// NOTE: 将来フィールドが増えても、署名対象外のものは無視できる想定。
type createThreadRequest struct {
	Version      int    `json:"version"`
	BoardID      string `json:"boardId"`
	Title        string `json:"title"`
	CreatedAt    string `json:"createdAt"`
	AuthorPubKey string `json:"authorPubKey"`

	// Signature は ThreadSignPayload を Ed25519 署名した base64(StdEncoding) 文字列。
	// 互換性のため signatureBase64 も受け付ける。
	Signature       string `json:"signature"`
	SignatureBase64 string `json:"signatureBase64"`

	// 差し替えメモ:
	// 合体(本実装)で API スキーマを共通パッケージ/モデルに寄せる場合は、この struct を移動/統合する。
}

// createThreadResponse は POST /api/v1/threads の出力(暫定)。
// NOTE: 合体(本実装)時に CID/スレッドログ等の仕様に合わせて変更する。
type createThreadResponse struct {
	ThreadID string `json:"threadId"`
}

func init() {
	// main.go を触らずに DefaultServeMux にルーティングを登録する。
	//
	// 差し替えメモ:
	// 合体(本実装)でルーター構成を整理する場合は、ここでの HandleFunc 登録を
	// 新しいルーティング層へ移す。
	http.HandleFunc("/api/v1/threads", handleCreateThread)
}

func handleCreateThread(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	var req createThreadRequest
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

	req.BoardID = strings.TrimSpace(req.BoardID)
	req.Title = strings.TrimSpace(req.Title)
	req.CreatedAt = strings.TrimSpace(req.CreatedAt)
	req.AuthorPubKey = strings.TrimSpace(req.AuthorPubKey)
	req.Signature = strings.TrimSpace(req.Signature)
	req.SignatureBase64 = strings.TrimSpace(req.SignatureBase64)

	if err := validateCreateThreadRequest(req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	sig := req.Signature
	if sig == "" {
		sig = req.SignatureBase64
	}

	payload := ThreadSignPayload(
		req.Version,
		req.BoardID,
		req.Title,
		req.CreatedAt,
		req.AuthorPubKey,
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
	// いまはとりあえず動かすため、署名ペイロードのSHA256から暫定threadIdを作る。
	sum := sha256.Sum256([]byte(payload))
	threadID := "thread-" + hex.EncodeToString(sum[:8])

	// BBSデータ(暫定): board_threads_store.go の in-memory へ追加。
	//
	// 差し替えメモ:
	// 合体(本実装)のタイミングで、ここはストレージ層/FlexIPFS/DB に置き換える。
	boardThreadsMu.Lock()
	boardThreads[req.BoardID] = append(boardThreads[req.BoardID], threadSummary{ID: threadID, Title: req.Title})
	boardThreadsMu.Unlock()

	writeJSON(w, http.StatusCreated, createThreadResponse{ThreadID: threadID})
}

func validateCreateThreadRequest(req createThreadRequest) error {
	// 差し替えメモ:
	// 合体(本実装)で共通のバリデーション層を作る場合は、ここを共通関数へ寄せる。
	if req.Version <= 0 {
		return errors.New("version must be > 0")
	}
	if req.BoardID == "" {
		return errors.New("boardId is required")
	}
	if req.Title == "" {
		return errors.New("title is required")
	}
	if req.AuthorPubKey == "" {
		return errors.New("authorPubKey is required")
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

// ThreadSignPayload は Thread(新規作成)用の署名ペイロードを作る。
//
// 差し替えメモ:
// 合体(本実装)のタイミングで、仕様が決まったら key.go 側へ移して共通化してもOK。
func ThreadSignPayload(
	version int,
	boardID string,
	title string,
	createdAt string,
	authorPubKey string,
) string {
	return BuildSignPayload([][2]string{
		{"type", "thread"},
		{"version", strconv.Itoa(version)},
		{"boardId", boardID},
		{"title", title},
		{"createdAt", createdAt},
		{"authorPubKey", authorPubKey},
	})
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
