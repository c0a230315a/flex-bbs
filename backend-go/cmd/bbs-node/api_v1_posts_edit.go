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

// --- API: POST /api/v1/posts/{postCid}/edit (#20: edit post) ---
//
// 差し替えメモ:
// 合体時にテストを使わない(リポジトリに残さない)運用なら、以下を削除してOK。
// - cmd/bbs-node/api_v1_posts_edit_test.go
// - このファイル内の seedPostForTests

// editPostRequest は POST /api/v1/posts/{postCid}/edit の入力。
// NOTE: meta/attachments などが来ても無視してOK (署名対象外の想定)。
type editPostRequest struct {
	Version      int    `json:"version"`
	ThreadID     string `json:"threadId"`
	AuthorPubKey string `json:"authorPubKey"`
	Body         struct {
		Format  string `json:"format"`
		Content string `json:"content"`
	} `json:"body"`
	EditedAt string `json:"editedAt"`

	// Signature は PostEditSignPayload を Ed25519 署名した base64(StdEncoding) 文字列。
	// 互換性のため signatureBase64 も受け付ける。
	Signature       string `json:"signature"`
	SignatureBase64 string `json:"signatureBase64"`

	// 差し替えメモ:
	// 合体(本実装)で API スキーマを共通パッケージ/モデルに寄せる場合は、この struct を移動/統合する。
}

// editPostResponse は POST /api/v1/posts/{postCid}/edit の出力(暫定)。
// NOTE: 合体(本実装)時に CID/履歴/ログ等の仕様に合わせて変更する。
type editPostResponse struct {
	OldPostCid string `json:"oldPostCid"`
	NewPostCid string `json:"newPostCid"`
}

type storedPost struct {
	PostCid      string
	ThreadID     string
	AuthorPubKey string
	BodyFormat   string
	BodyContent  string
	CreatedAt    string
	EditedAt     string
}

var (
	postsStoreMu sync.RWMutex
	postsStore   = map[string]storedPost{}
)

func init() {
	// main.go を触らずに DefaultServeMux にルーティングを登録する。
	//
	// 差し替えメモ:
	// 合体(本実装)でルーター構成を整理する場合は、ここでの HandleFunc 登録を
	// 新しいルーティング層へ移す。
	http.HandleFunc("/api/v1/posts/", handleEditPost)
}

func handleEditPost(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	postCid, ok := parsePostEditPath(r.URL.Path)
	if !ok {
		http.NotFound(w, r)
		return
	}

	var req editPostRequest
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
	req.Body.Format = strings.TrimSpace(req.Body.Format)
	// Body.Content は空白も意味を持つ可能性があるので TrimSpace しない。
	req.EditedAt = strings.TrimSpace(req.EditedAt)
	req.Signature = strings.TrimSpace(req.Signature)
	req.SignatureBase64 = strings.TrimSpace(req.SignatureBase64)

	if err := validateEditPostRequest(req); err != nil {
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

	// 編集ルール(暫定): 投稿者のみ編集可。
	if old.AuthorPubKey != req.AuthorPubKey {
		writeJSONError(w, http.StatusForbidden, "forbidden", "authorPubKey does not match")
		return
	}
	// 編集ルール(暫定): threadId は変更不可。
	if old.ThreadID != req.ThreadID {
		writeJSONError(w, http.StatusBadRequest, "invalid_request", "threadId mismatch")
		return
	}

	// 編集ルール(暫定): editedAt は createdAt より前にしない。
	if t0, err0 := time.Parse(time.RFC3339Nano, old.CreatedAt); err0 == nil {
		if t1, err1 := time.Parse(time.RFC3339Nano, req.EditedAt); err1 == nil {
			if t1.Before(t0) {
				writeJSONError(w, http.StatusBadRequest, "invalid_request", "editedAt must be >= createdAt")
				return
			}
		}
	}

	sig := req.Signature
	if sig == "" {
		sig = req.SignatureBase64
	}

	payload := PostEditSignPayload(
		req.Version,
		req.ThreadID,
		postCid,
		req.AuthorPubKey,
		req.Body.Format,
		req.Body.Content,
		req.EditedAt,
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
		BodyFormat:   req.Body.Format,
		BodyContent:  req.Body.Content,
		CreatedAt:    old.CreatedAt,
		EditedAt:     req.EditedAt,
	}
	postsStoreMu.Unlock()

	writeJSON(w, http.StatusOK, editPostResponse{OldPostCid: postCid, NewPostCid: newPostCid})
}

func parsePostEditPath(path string) (string, bool) {
	const prefix = "/api/v1/posts/"
	if !strings.HasPrefix(path, prefix) {
		return "", false
	}
	rest := strings.TrimPrefix(path, prefix)
	segments := strings.Split(strings.Trim(rest, "/"), "/")
	if len(segments) != 2 {
		return "", false
	}
	postCid := segments[0]
	if postCid == "" {
		return "", false
	}
	if segments[1] != "edit" {
		return "", false
	}
	return postCid, true
}

func validateEditPostRequest(req editPostRequest) error {
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
	if req.EditedAt == "" {
		return errors.New("editedAt is required")
	}
	if !isRFC3339OrNano(req.EditedAt) {
		return errors.New("editedAt must be RFC3339 or RFC3339Nano")
	}
	if req.Signature == "" && req.SignatureBase64 == "" {
		return errors.New("signature is required")
	}
	return nil
}

// PostEditSignPayload は Post(編集)用の署名ペイロードを作る。
//
// 差し替えメモ:
// 合体(本実装)のタイミングで、仕様が決まったら key.go 側へ移して共通化してもOK。
func PostEditSignPayload(
	version int,
	threadID string,
	postCid string,
	authorPubKey string,
	bodyFormat string,
	bodyContent string,
	editedAt string,
) string {
	return BuildSignPayload([][2]string{
		{"type", "postEdit"},
		{"version", strconv.Itoa(version)},
		{"threadId", threadID},
		{"postCid", postCid},
		{"authorPubKey", authorPubKey},
		{"body.format", bodyFormat},
		{"body.content", bodyContent},
		{"editedAt", editedAt},
	})
}

// seedPostForTests はテスト用に in-memory store に投稿を登録する。
//
// 差し替えメモ:
// 合体(本実装)でテスト無し運用なら、この関数自体を削除してOK。
func seedPostForTests(p storedPost) {
	postsStoreMu.Lock()
	postsStore[p.PostCid] = p
	postsStoreMu.Unlock()
}

func resetPostsStoreForTests() {
	postsStoreMu.Lock()
	postsStore = map[string]storedPost{}
	postsStoreMu.Unlock()
}
