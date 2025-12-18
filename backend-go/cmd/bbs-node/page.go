package main

import (
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"strings"
)

// --- API: GET /api/v1/boards/{boardId}/threads (paging) ---

// threadSummary はスレッド一覧で返す最小のJSON形。
// NOTE: 合体(本実装)のタイミングで必要なフィールドを増やす。
type threadSummary struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

// boardThreadsResponse はスレッド一覧APIのレスポンス。
type boardThreadsResponse struct {
	Threads []threadSummary `json:"threads"`
	Page    int             `json:"page"`
	Limit   int             `json:"limit"`
	Total   int             `json:"total"`
}

func init() {
	// main.go を触らずに DefaultServeMux にルーティングを登録する。
	http.HandleFunc("/api/v1/boards/", handleBoardThreads)
}

// handleBoardThreads は GET /api/v1/boards/{boardId}/threads を処理する。
// クエリ: page(1-based), limit
func handleBoardThreads(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	boardID, ok := parseBoardThreadsPath(r.URL.Path)
	if !ok {
		http.NotFound(w, r)
		return
	}

	// page/limit は不正ならデフォルトにフォールバック。
	page := parsePositiveInt(r.URL.Query().Get("page"), 1, 1_000_000)
	limit := parsePositiveInt(r.URL.Query().Get("limit"), 10, 1_000)

	// NOTE: ここは本来ストレージ層に置き換える。
	// とりあえず「APIが実行できる」ことを優先し、暫定で空リストを返す。
	all := listThreadsForBoard(boardID)

	// ページング適用。
	start := (page - 1) * limit
	if start < 0 {
		start = 0
	}
	if start > len(all) {
		start = len(all)
	}
	end := start + limit
	if end > len(all) {
		end = len(all)
	}
	paged := all[start:end]

	resp := boardThreadsResponse{
		Threads: paged,
		Page:    page,
		Limit:   limit,
		Total:   len(all),
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		log.Printf("error encoding threads response: %v", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
}

// parseBoardThreadsPath は /api/v1/boards/{boardId}/threads 形式から boardId を抽出する。
func parseBoardThreadsPath(path string) (string, bool) {
	const prefix = "/api/v1/boards/"
	if !strings.HasPrefix(path, prefix) {
		return "", false
	}
	rest := strings.TrimPrefix(path, prefix)
	segments := strings.Split(strings.Trim(rest, "/"), "/")
	if len(segments) != 2 {
		return "", false
	}
	if segments[0] == "" {
		return "", false
	}
	if segments[1] != "threads" {
		return "", false
	}
	return segments[0], true
}

// parsePositiveInt は正の整数をパースして返す。失敗したら defaultValue。
// maxValue を超える値は maxValue に丸める。
func parsePositiveInt(s string, defaultValue int, maxValue int) int {
	if s == "" {
		return defaultValue
	}
	v, err := strconv.Atoi(s)
	if err != nil || v <= 0 {
		return defaultValue
	}
	if v > maxValue {
		return maxValue
	}
	return v
}

// listThreadsForBoard は boardID に紐づく全スレッドを返す。
//
// 差し替えメモ:
// 合体(本実装)のタイミングで、この関数の中身を
// 「ストレージ層/FlexIPFS/DB」などの本物に差し替える。
func listThreadsForBoard(boardID string) []threadSummary {
	if threads, ok := getBoardThreadSummaries(boardID); ok {
		return threads
	}
	return []threadSummary{}
}
