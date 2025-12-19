package main

import "sync"

// --- In-memory store for boards -> threads listing (Issue #16) ---
//
// NOTE:
// 本来は永続ストレージ/FlexIPFS などから取得する想定。
// 合体(本実装)時に、このストア自体を消すか、getBoardThreadSummaries の実装を差し替える。

var (
	boardThreadsMu sync.RWMutex
	boardThreads   = map[string][]threadSummary{}
)

// getBoardThreadSummaries returns thread summaries for a board.
// It returns (nil, false) when the board does not exist.
func getBoardThreadSummaries(boardID string) ([]threadSummary, bool) {
	boardThreadsMu.RLock()
	threads, ok := boardThreads[boardID]
	boardThreadsMu.RUnlock()
	if !ok {
		return nil, false
	}
	// defensive copy to avoid callers mutating shared slice
	out := make([]threadSummary, len(threads))
	copy(out, threads)
	return out, true
}

// setBoardThreadSummaries is a small helper for tests and temporary wiring.
func setBoardThreadSummaries(boardID string, threads []threadSummary) {
	boardThreadsMu.Lock()
	defer boardThreadsMu.Unlock()
	cp := make([]threadSummary, len(threads))
	copy(cp, threads)
	boardThreads[boardID] = cp
}

// resetBoardThreads clears the in-memory store (used by tests).
func resetBoardThreads() {
	boardThreadsMu.Lock()
	defer boardThreadsMu.Unlock()
	boardThreads = map[string][]threadSummary{}
}
