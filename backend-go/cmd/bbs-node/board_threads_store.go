package main

import "sync"

// --- In-memory store for boards -> threads listing ---
//
// NOTE: 合体(本実装)のタイミングでストレージ層へ置き換える。

// threadSummary is the minimal shape for thread listings.
type threadSummary struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

var (
	boardThreadsMu sync.RWMutex
	boardThreads   = map[string][]threadSummary{}
)

func resetBoardThreadsForTests() {
	boardThreadsMu.Lock()
	boardThreads = map[string][]threadSummary{}
	boardThreadsMu.Unlock()
}
