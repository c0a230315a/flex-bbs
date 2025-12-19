package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// page_test.go は Issue #16 の
// GET /api/v1/boards/{boardId}/threads のページング挙動を固定するためのテスト。
//
// ここで確認していること:
// - デフォルト値: page=1, limit=10
// - page/limit 指定での切り出し範囲
// - 端数ページ(最後のページ)の件数
// - パス不正は 404、メソッド不正は 405

func TestBoardThreads_DefaultPaging(t *testing.T) {
	resetBoardThreads()
	defer resetBoardThreads()

	boardID := "board-1"
	var all []threadSummary
	for i := 1; i <= 25; i++ {
		all = append(all, threadSummary{ID: "t" + itoa(i), Title: "title"})
	}
	setBoardThreadSummaries(boardID, all)

	r := httptest.NewRequest(http.MethodGet, "/api/v1/boards/"+boardID+"/threads", nil)
	w := httptest.NewRecorder()
	handleBoardThreads(w, r)

	resp := w.Result()
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d body=%s", resp.StatusCode, w.Body.String())
	}

	var out boardThreadsResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.Page != 1 || out.Limit != 10 || out.Total != 25 {
		t.Fatalf("page/limit/total=%d/%d/%d", out.Page, out.Limit, out.Total)
	}
	if len(out.Threads) != 10 {
		t.Fatalf("threads len=%d", len(out.Threads))
	}
	if out.Threads[0].ID != "t1" || out.Threads[9].ID != "t10" {
		t.Fatalf("range=%s..%s", out.Threads[0].ID, out.Threads[9].ID)
	}
}

func TestBoardThreads_Page2Limit10(t *testing.T) {
	resetBoardThreads()
	defer resetBoardThreads()

	boardID := "board-1"
	var all []threadSummary
	for i := 1; i <= 25; i++ {
		all = append(all, threadSummary{ID: "t" + itoa(i), Title: "title"})
	}
	setBoardThreadSummaries(boardID, all)

	r := httptest.NewRequest(http.MethodGet, "/api/v1/boards/"+boardID+"/threads?page=2&limit=10", nil)
	w := httptest.NewRecorder()
	handleBoardThreads(w, r)

	resp := w.Result()
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d body=%s", resp.StatusCode, w.Body.String())
	}

	var out boardThreadsResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.Page != 2 || out.Limit != 10 || out.Total != 25 {
		t.Fatalf("page/limit/total=%d/%d/%d", out.Page, out.Limit, out.Total)
	}
	if len(out.Threads) != 10 {
		t.Fatalf("threads len=%d", len(out.Threads))
	}
	if out.Threads[0].ID != "t11" || out.Threads[9].ID != "t20" {
		t.Fatalf("range=%s..%s", out.Threads[0].ID, out.Threads[9].ID)
	}
}

func TestBoardThreads_Page3Limit10_Remainder(t *testing.T) {
	resetBoardThreads()
	defer resetBoardThreads()

	boardID := "board-1"
	var all []threadSummary
	for i := 1; i <= 25; i++ {
		all = append(all, threadSummary{ID: "t" + itoa(i), Title: "title"})
	}
	setBoardThreadSummaries(boardID, all)

	r := httptest.NewRequest(http.MethodGet, "/api/v1/boards/"+boardID+"/threads?page=3&limit=10", nil)
	w := httptest.NewRecorder()
	handleBoardThreads(w, r)

	resp := w.Result()
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d body=%s", resp.StatusCode, w.Body.String())
	}

	var out boardThreadsResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out.Threads) != 5 {
		t.Fatalf("threads len=%d", len(out.Threads))
	}
	if out.Threads[0].ID != "t21" || out.Threads[4].ID != "t25" {
		t.Fatalf("range=%s..%s", out.Threads[0].ID, out.Threads[4].ID)
	}
}

func TestBoardThreads_InvalidPath_NotFound(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/api/v1/boards/board-1/threads/extra", nil)
	w := httptest.NewRecorder()
	handleBoardThreads(w, r)
	if w.Result().StatusCode != http.StatusNotFound {
		t.Fatalf("status=%d", w.Result().StatusCode)
	}
}

func TestBoardThreads_MethodNotAllowed(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/api/v1/boards/board-1/threads", nil)
	w := httptest.NewRecorder()
	handleBoardThreads(w, r)
	if w.Result().StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("status=%d", w.Result().StatusCode)
	}
}

func itoa(i int) string {
	// itoa はテストデータ生成用の最小ユーティリティ。
	// fmt を import せずに "t1".."t25" のようなIDを作るために使う。
	if i == 0 {
		return "0"
	}
	var b [20]byte
	n := 0
	for i > 0 {
		d := i % 10
		b[n] = byte('0' + d)
		n++
		i /= 10
	}
	// reverse
	for j := 0; j < n/2; j++ {
		b[j], b[n-1-j] = b[n-1-j], b[j]
	}
	return string(b[:n])
}
