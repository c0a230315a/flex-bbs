package indexer

import (
	"context"
	"testing"

	"flex-bbs/backend-go/bbs/flexipfs"
	"flex-bbs/backend-go/bbs/storage"
)

func TestGetBoardByID_ReturnsRow(t *testing.T) {
	st := storage.New(flexipfs.New("http://127.0.0.1:5001/api/v0"))
	ix, err := Open(":memory:", st)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = ix.Close() })

	_, err = ix.db.ExecContext(context.Background(), `
		INSERT INTO boards(board_id, board_meta_cid, title, description, created_at, created_by, signature, log_head_cid)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?)
	`, "bbs.general", "baf_boardmeta", "General", "Desc", "2025-01-01T00:00:00Z", "me", "sig", nil)
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	got, err := ix.GetBoardByID(context.Background(), "bbs.general")
	if err != nil {
		t.Fatalf("GetBoardByID: %v", err)
	}
	if got == nil {
		t.Fatalf("expected result")
	}
	if got.BoardID != "bbs.general" || got.Title != "General" || got.BoardMetaCID != "baf_boardmeta" {
		t.Fatalf("unexpected result: %#v", got)
	}

	missing, err := ix.GetBoardByID(context.Background(), "bbs.missing")
	if err != nil {
		t.Fatalf("GetBoardByID missing: %v", err)
	}
	if missing != nil {
		t.Fatalf("expected nil, got: %#v", missing)
	}
}
