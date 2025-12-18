package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
)

// --- API: GET /api/v1/threads/{threadId} ---

// ErrThreadNotFound は対象のスレッドが存在しないときに使う。
var ErrThreadNotFound = errors.New("thread not found")

// ThreadDetail はスレッドの詳細情報(最低限 threadId を返す)。
// TODO: 仕様確定後に title/createdAt などを追加する。
type ThreadDetail struct {
	ThreadID string `json:"threadId"`
}

// ThreadLogEntry はスレッド内の変更履歴を表す(最低限 op と createdAt)。
// TODO: 仕様確定後に cid/postCid/old/new などを追加する。
type ThreadLogEntry struct {
	Op        string `json:"op"`
	CreatedAt string `json:"createdAt"`
}

// PostView はスレッド取得時に返す投稿ビュー(最低限 cid を返す)。
// TODO: 仕様確定後に author/body などを追加する。
type PostView struct {
	CID string `json:"cid"`
}

// GetThreadResponse は GET /api/v1/threads/{threadId} のレスポンス。
type GetThreadResponse struct {
	Thread    ThreadDetail     `json:"thread"`
	ThreadLog []ThreadLogEntry `json:"threadLog"`
	Posts     []PostView       `json:"posts"`
}

// ThreadGetter は threadId から Thread詳細・ThreadLog・Posts を返す責務。
// 実装は今後増える想定なので interface にして差し替え可能にしている。
type ThreadGetter interface {
	GetThread(ctx context.Context, threadID string) (GetThreadResponse, error)
}

// threadGetter はハンドラが使う実装(テストで差し替え可能)。
//
// 差し替えメモ:
// 合体(本実装)のタイミングで、ここを実データ版 ThreadGetter に差し替える。
// 例: var threadGetter ThreadGetter = flexIPFSThreadGetter{...}
// あるいは別ファイルの init() で `threadGetter = flexIPFSThreadGetter{...}` を実行。
//
// まずは「APIが200で返る」ことを優先して、デフォルトは空のデータを返す。
var threadGetter ThreadGetter = defaultThreadGetter{}

type defaultThreadGetter struct{}

func (defaultThreadGetter) GetThread(ctx context.Context, threadID string) (GetThreadResponse, error) {
	return GetThreadResponse{
		Thread: ThreadDetail{ThreadID: threadID},
		// TODO: 実データソースが決まり次第、ThreadLog/Posts を組み立てる。
		ThreadLog: []ThreadLogEntry{},
		Posts:     []PostView{},
	}, nil
}

// init は main.go を触らずに DefaultServeMux にルーティングを登録する。
func init() {
	// main.go を触らずに DefaultServeMux にルーティングを足す。
	// 末尾スラッシュ付きで prefix マッチさせる。
	http.HandleFunc("/api/v1/threads/", handleGetThread)
}

// handleGetThread は GET /api/v1/threads/{threadId} を処理して JSON を返す。
func handleGetThread(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	threadID, ok := parseThreadIDFromPath(r.URL.Path)
	if !ok {
		writeJSONError(w, http.StatusNotFound, "not found")
		return
	}

	resp, err := threadGetter.GetThread(r.Context(), threadID)
	if err != nil {
		if errors.Is(err, ErrThreadNotFound) {
			writeJSONError(w, http.StatusNotFound, "thread not found")
			return
		}
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, resp)
}

// parseThreadIDFromPath は URL パスから threadId を抽出する。
// /api/v1/threads/{id} 以外は false を返す。
func parseThreadIDFromPath(path string) (string, bool) {
	const prefix = "/api/v1/threads/"
	if !strings.HasPrefix(path, prefix) {
		return "", false
	}
	rest := strings.TrimPrefix(path, prefix)
	if rest == "" {
		return "", false
	}
	// 追加セグメントは許可しない: /api/v1/threads/{id} のみ。
	if strings.Contains(rest, "/") {
		return "", false
	}
	return rest, true
}

// jsonError は JSON エラーレスポンスの形。
type jsonError struct {
	Error string `json:"error"`
}

// writeJSON は任意の値を JSON で返す共通ヘルパー。
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// writeJSONError はエラー用の JSON を返す共通ヘルパー。
func writeJSONError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, jsonError{Error: msg})
}
