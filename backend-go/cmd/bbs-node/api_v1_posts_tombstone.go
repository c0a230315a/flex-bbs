package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// --- API: POST /api/v1/posts/{postCid}/tombstone (#21: logical delete) ---
//
// 差し替えメモ:
// 合体(本実装)でルーター構成を整理する場合は、init() の HandleFunc 登録を
// 新しいルーティング層へ移す。

// tombstonePostRequest は POST /api/v1/posts/{postCid}/tombstone の入力。
// NOTE: meta/attachments などが来ても無視してOK (署名対象外の想定)。
type tombstonePostRequest struct {
	Version      int    `json:"version"`
	ThreadID     string `json:"threadId"`
	AuthorPubKey string `json:"authorPubKey"`
	TombstonedAt string `json:"tombstonedAt"`

	// Signature は PostTombstoneSignPayload を Ed25519 署名した base64(StdEncoding) 文字列。
	// 互換性のため signatureBase64 も受け付ける。
	Signature       string `json:"signature"`
	SignatureBase64 string `json:"signatureBase64"`
}

// tombstonePostResponse は POST /api/v1/posts/{postCid}/tombstone の出力(暫定)。
type tombstonePostResponse struct {
	OldPostCid string `json:"oldPostCid"`
	NewPostCid string `json:"newPostCid"`
}

type storedPost struct {
	PostCid       string
	ThreadID      string
	AuthorPubKey  string
	BodyFormat    string
	BodyContent   string
	CreatedAt     string
	EditedAt      string
	TombstonedAt  string
	IsTombstoned  bool
	OriginalPost  string
	OriginalTitle string
}

var (
	postsStoreMu sync.RWMutex
	postsStore   = map[string]storedPost{}
)

func init() {
	// main.go を触らずに DefaultServeMux にルーティングを登録する。
	http.HandleFunc("/api/v1/posts/", handlePostActions)
}

// handlePostActions は /api/v1/posts/{postCid}/... 系をまとめて受ける(暫定)。
//
// 差し替えメモ:
// 合体(本実装)で edit/tombstone を分割するなら、ここをルーター層に分離する。
func handlePostActions(w http.ResponseWriter, r *http.Request) {
	postCid, action, ok := parsePostActionPath(r.URL.Path)
	if !ok {
		http.NotFound(w, r)
		return
	}

	switch action {
	case "tombstone":
		handleTombstonePost(w, r, postCid)
	default:
		http.NotFound(w, r)
	}
}

func handleTombstonePost(w http.ResponseWriter, r *http.Request, postCid string) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	var req tombstonePostRequest
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		writeJSONError(w, http.StatusBadRequest, "invalid_json", "unexpected trailing JSON")
		return
	}

	req.ThreadID = strings.TrimSpace(req.ThreadID)
	req.AuthorPubKey = strings.TrimSpace(req.AuthorPubKey)
	req.TombstonedAt = strings.TrimSpace(req.TombstonedAt)
	req.Signature = strings.TrimSpace(req.Signature)
	req.SignatureBase64 = strings.TrimSpace(req.SignatureBase64)

	if err := validateTombstonePostRequest(req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	postsStoreMu.RLock()
	old, found := postsStore[postCid]
	postsStoreMu.RUnlock()
	if !found {
		writeJSONError(w, http.StatusNotFound, "not_found", "post not found")
		return
	}

	// 削除ルール(暫定): 投稿者のみ tombstone 可。
	if old.AuthorPubKey != req.AuthorPubKey {
		writeJSONError(w, http.StatusForbidden, "forbidden", "authorPubKey does not match")
		return
	}

	// 削除ルール(暫定): threadId は変更不可。
	if old.ThreadID != req.ThreadID {
		writeJSONError(w, http.StatusBadRequest, "invalid_request", "threadId mismatch")
		return
	}

	// 削除ルール(暫定): 既に tombstone 済みなら弾く。
	if old.IsTombstoned {
		writeJSONError(w, http.StatusConflict, "already_tombstoned", "post already tombstoned")
		return
	}

	// 削除ルール(暫定): tombstonedAt は createdAt より前にしない。
	if t0, err0 := time.Parse(time.RFC3339Nano, old.CreatedAt); err0 == nil {
		if t1, err1 := time.Parse(time.RFC3339Nano, req.TombstonedAt); err1 == nil {
			if t1.Before(t0) {
				writeJSONError(w, http.StatusBadRequest, "invalid_request", "tombstonedAt must be >= createdAt")
				return
			}
		}
	}

	sig := req.Signature
	if sig == "" {
		sig = req.SignatureBase64
	}

	payload := PostTombstoneSignPayload(
		req.Version,
		req.ThreadID,
		postCid,
		req.AuthorPubKey,
		req.TombstonedAt,
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
	newPostCid := "sha256:" + hex.EncodeToString(sum[:])

	// 差し替えメモ:
	// 合体(本実装)のタイミングで、このインメモリ保存はストレージ層/FlexIPFS/DB に置き換える。
	postsStoreMu.Lock()
	postsStore[newPostCid] = storedPost{
		PostCid:      newPostCid,
		ThreadID:     old.ThreadID,
		AuthorPubKey: old.AuthorPubKey,
		BodyFormat:   old.BodyFormat,
		BodyContent:  "",
		CreatedAt:    old.CreatedAt,
		EditedAt:     old.EditedAt,
		TombstonedAt: req.TombstonedAt,
		IsTombstoned: true,
	}
	postsStoreMu.Unlock()

	writeJSON(w, http.StatusOK, tombstonePostResponse{OldPostCid: postCid, NewPostCid: newPostCid})
}

func parsePostActionPath(path string) (postCid string, action string, ok bool) {
	const prefix = "/api/v1/posts/"
	if !strings.HasPrefix(path, prefix) {
		return "", "", false
	}
	rest := strings.TrimPrefix(path, prefix)
	segments := strings.Split(strings.Trim(rest, "/"), "/")
	if len(segments) != 2 {
		return "", "", false
	}
	if segments[0] == "" || segments[1] == "" {
		return "", "", false
	}
	return segments[0], segments[1], true
}

func validateTombstonePostRequest(req tombstonePostRequest) error {
	if req.Version <= 0 {
		return errors.New("version must be > 0")
	}
	if req.ThreadID == "" {
		return errors.New("threadId is required")
	}
	if req.AuthorPubKey == "" {
		return errors.New("authorPubKey is required")
	}
	if req.TombstonedAt == "" {
		return errors.New("tombstonedAt is required")
	}
	if !isRFC3339OrNano(req.TombstonedAt) {
		return errors.New("tombstonedAt must be RFC3339 or RFC3339Nano")
	}
	if req.Signature == "" && req.SignatureBase64 == "" {
		return errors.New("signature is required")
	}
	return nil
}

// PostTombstoneSignPayload は Post(tombstone)用の署名ペイロードを作る。
//
// 差し替えメモ:
// 合体(本実装)のタイミングで、仕様が決まったら key.go 側へ移して共通化してもOK。
func PostTombstoneSignPayload(
	version int,
	threadID string,
	postCid string,
	authorPubKey string,
	tombstonedAt string,
) string {
	return BuildSignPayload([][2]string{
		{"type", "postTombstone"},
		{"version", strconv.Itoa(version)},
		{"threadId", threadID},
		{"postCid", postCid},
		{"authorPubKey", authorPubKey},
		{"tombstonedAt", tombstonedAt},
	})
}

// seedPostForTests はテスト用に in-memory store に投稿を登録する。
//
// 差し替えメモ:
// 合体(本実装)でテスト無し運用なら、
// - cmd/bbs-node/api_v1_posts_tombstone_test.go
// - この関数(seedPostForTests)
// を削除してOK。
func seedPostForTests(p storedPost) {
	postsStoreMu.Lock()
	postsStore[p.PostCid] = p
	postsStoreMu.Unlock()
}
