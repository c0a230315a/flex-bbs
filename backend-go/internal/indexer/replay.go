package indexer

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"
)

// LogReplayer はBoardLogEntryを処理してDBを更新します。
type LogReplayer struct {
	db DB
}

// NewLogReplayer は新しいログリプレイヤーを作成します。
func NewLogReplayer(db DB) *LogReplayer {
	return &LogReplayer{db: db}
}

// ReplayEntry は単一のBoardLogEntryを処理します。
func (r *LogReplayer) ReplayEntry(ctx context.Context, entry *BoardLogEntry) error {
	return r.db.WithTx(ctx, func(tx DB) error {
		lastSeq, err := tx.GetLastSequence(ctx)
		if err != nil {
			return fmt.Errorf("get last seq: %w", err)
		}
		// すでに処理済みのエントリはスキップ
		if entry.SeqNum <= lastSeq {
			return nil
		}

		var opErr error
		switch entry.Operation {
		case "create_board":
			opErr = r.replayCreateBoard(ctx, tx, entry)
		case "update_board":
			opErr = r.replayUpdateBoard(ctx, tx, entry)
		case "create_thread":
			opErr = r.replayCreateThread(ctx, tx, entry)
		case "close_thread":
			opErr = r.replayCloseThread(ctx, tx, entry)
		case "create_post":
			opErr = r.replayCreatePost(ctx, tx, entry)
		case "delete_post":
			opErr = r.replayDeletePost(ctx, tx, entry)
		default:
			log.Printf("LogReplayer: unknown operation %q seq=%d", entry.Operation, entry.SeqNum)
			// 未知の操作は無視してシーケンスだけ進める
		}
		if opErr != nil {
			return fmt.Errorf("replay %s: %w", entry.Operation, opErr)
		}

		if err := tx.SetLastSequence(ctx, entry.SeqNum); err != nil {
			return fmt.Errorf("update last seq: %w", err)
		}
		return nil
	})
}

// ReplayEntries は複数のBoardLogEntryを一括処理します。
func (r *LogReplayer) ReplayEntries(ctx context.Context, entries []BoardLogEntry) error {
	for i := range entries {
		if err := r.ReplayEntry(ctx, &entries[i]); err != nil {
			return err
		}
	}
	return nil
}

// replayCreateBoard は create_board操作を処理します。
func (r *LogReplayer) replayCreateBoard(ctx context.Context, tx DB, entry *BoardLogEntry) error {
	var b Board
	if err := json.Unmarshal([]byte(entry.Data), &b); err != nil {
		return fmt.Errorf("unmarshal board: %w", err)
	}
	if b.ID == "" {
		b.ID = entry.EntityID
	}
	if b.CreatedAt.IsZero() {
		b.CreatedAt = entry.Timestamp
	}
	if b.UpdatedAt.IsZero() {
		b.UpdatedAt = entry.Timestamp
	}
	return tx.CreateBoard(ctx, &b)
}

// replayCreateThread は create_thread操作を処理します。
func (r *LogReplayer) replayCreateThread(ctx context.Context, tx DB, entry *BoardLogEntry) error {
	var t Thread
	if err := json.Unmarshal([]byte(entry.Data), &t); err != nil {
		return fmt.Errorf("unmarshal thread: %w", err)
	}
	if t.ID == "" {
		t.ID = entry.EntityID
	}
	if t.CreatedAt.IsZero() {
		t.CreatedAt = entry.Timestamp
	}
	if t.UpdatedAt.IsZero() {
		t.UpdatedAt = entry.Timestamp
	}
	return tx.CreateThread(ctx, &t)
}

// replayCreatePost は create_post操作を処理します。
func (r *LogReplayer) replayCreatePost(ctx context.Context, tx DB, entry *BoardLogEntry) error {
	var p Post
	if err := json.Unmarshal([]byte(entry.Data), &p); err != nil {
		return fmt.Errorf("unmarshal post: %w", err)
	}
	if p.ID == "" {
		p.ID = entry.EntityID
	}
	if p.CreatedAt.IsZero() {
		p.CreatedAt = entry.Timestamp
	}
	if p.UpdatedAt.IsZero() {
		p.UpdatedAt = entry.Timestamp
	}
	return tx.CreatePost(ctx, &p)
}

// replayDeletePost は delete_post操作を処理します。
func (r *LogReplayer) replayDeletePost(ctx context.Context, tx DB, entry *BoardLogEntry) error {
	// Data は空で、EntityID に post ID が入っている想定
	if entry.EntityID == "" {
		return fmt.Errorf("delete_post: empty entity_id")
	}
	return tx.DeletePost(ctx, entry.EntityID)
}

// replayCloseThread は close_thread操作を処理します。
func (r *LogReplayer) replayCloseThread(ctx context.Context, tx DB, entry *BoardLogEntry) error {
	if entry.EntityID == "" {
		return fmt.Errorf("close_thread: empty entity_id")
	}
	// スレッドの is_closed を true にする
	if err := tx.CloseThread(ctx, entry.EntityID); err != nil {
		return err
	}
	// updated_at をログのタイムスタンプに合わせたい場合は Thread 更新を取得してもよいが、
	// CloseThread 側で updated_at を更新しているのでここでは何もしない。
	return nil
}

// replayUpdateBoard は update_board操作を処理します。
func (r *LogReplayer) replayUpdateBoard(ctx context.Context, tx DB, entry *BoardLogEntry) error {
	var incoming Board
	if err := json.Unmarshal([]byte(entry.Data), &incoming); err != nil {
		return fmt.Errorf("unmarshal board(update): %w", err)
	}
	id := incoming.ID
	if id == "" {
		id = entry.EntityID
	}
	if id == "" {
		return fmt.Errorf("update_board: empty entity_id")
	}

	existing, err := tx.GetBoard(ctx, id)
	if err != nil {
		return err
	}
	if existing == nil {
		// まだ存在しなければ create 扱い
		if incoming.ID == "" {
			incoming.ID = id
		}
		if incoming.CreatedAt.IsZero() {
			incoming.CreatedAt = entry.Timestamp
		}
		if incoming.UpdatedAt.IsZero() {
			incoming.UpdatedAt = entry.Timestamp
		}
		return tx.CreateBoard(ctx, &incoming)
	}

	// name / description だけ上書き（thread_count は維持）
	if incoming.Name != "" {
		existing.Name = incoming.Name
	}
	if incoming.Description != "" {
		existing.Description = incoming.Description
	}
	existing.UpdatedAt = time.Now().UTC()
	return tx.UpdateBoard(ctx, existing)
}
