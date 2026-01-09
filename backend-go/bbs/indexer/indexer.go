package indexer

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	bbslog "flex-bbs/backend-go/bbs/log"
	"flex-bbs/backend-go/bbs/storage"
	"flex-bbs/backend-go/bbs/types"

	_ "modernc.org/sqlite"
)

var ErrIndexerClosed = errors.New("indexer closed")

type Indexer struct {
	db      *sql.DB
	storage *storage.Storage
}

func Open(path string, st *storage.Storage) (*Indexer, error) {
	if st == nil || st.Flex == nil {
		return nil, fmt.Errorf("storage is required")
	}
	if path != ":memory:" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return nil, err
		}
	}

	dsn := path
	if path != ":memory:" {
		dsn = fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)", path)
	}

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)

	ix := &Indexer{db: db, storage: st}
	if err := ix.migrate(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}
	return ix, nil
}

func (i *Indexer) Close() error {
	if i.db == nil {
		return nil
	}
	err := i.db.Close()
	i.db = nil
	return err
}

func (i *Indexer) SyncBoardByMetaCID(ctx context.Context, boardMetaCID string) error {
	if i.db == nil {
		return ErrIndexerClosed
	}

	bm, err := i.storage.LoadBoardMeta(ctx, boardMetaCID)
	if err != nil {
		return err
	}
	if !bbslog.VerifyBoardMeta(bm) {
		return fmt.Errorf("invalid boardMeta signature cid=%s", boardMetaCID)
	}

	if err := i.upsertBoard(ctx, boardMetaCID, bm); err != nil {
		return err
	}
	if bm.LogHeadCID == nil || *bm.LogHeadCID == "" {
		return nil
	}
	headCID := *bm.LogHeadCID
	processed, err := i.isLogProcessed(ctx, headCID)
	if err != nil {
		return err
	}
	if processed {
		return nil
	}

	newCIDs, err := i.collectUnprocessedLogCIDs(ctx, headCID, 50_000)
	if err != nil {
		return err
	}
	for _, cid := range newCIDs {
		if err := i.applyLog(ctx, cid); err != nil {
			return err
		}
	}

	if err := i.setBoardLogHead(ctx, bm.BoardID, headCID); err != nil {
		return err
	}
	return nil
}

func (i *Indexer) collectUnprocessedLogCIDs(ctx context.Context, headCID string, maxDepth int) ([]string, error) {
	visited := make(map[string]struct{})
	var newestFirst []string

	current := headCID
	for current != "" {
		if _, ok := visited[current]; ok {
			break
		}
		if len(newestFirst) >= maxDepth {
			return nil, bbslog.ErrLogTooDeep
		}
		visited[current] = struct{}{}

		processed, err := i.isLogProcessed(ctx, current)
		if err != nil {
			return nil, err
		}
		if processed {
			break
		}

		e, err := i.storage.LoadBoardLogEntry(ctx, current)
		if err != nil {
			return nil, err
		}
		newestFirst = append(newestFirst, current)

		if e.PrevLogCID == nil || *e.PrevLogCID == "" {
			break
		}
		current = *e.PrevLogCID
	}

	for i, j := 0, len(newestFirst)-1; i < j; i, j = i+1, j-1 {
		newestFirst[i], newestFirst[j] = newestFirst[j], newestFirst[i]
	}
	return newestFirst, nil
}

func (i *Indexer) applyLog(ctx context.Context, logCID string) error {
	e, err := i.storage.LoadBoardLogEntry(ctx, logCID)
	if err != nil {
		return err
	}
	validSig := bbslog.VerifyBoardLogEntry(e)

	if err := i.insertProcessedLog(ctx, logCID, e, validSig); err != nil {
		return err
	}
	if !validSig {
		return nil
	}

	switch e.Op {
	case types.OpCreateThread:
		return i.applyCreateThread(ctx, e)
	case types.OpAddPost:
		return i.applyAddPost(ctx, e)
	case types.OpEditPost:
		return i.applyEditPost(ctx, e)
	case types.OpTombstonePost:
		return i.applyTombstone(ctx, e)
	default:
		return fmt.Errorf("unknown op: %s", e.Op)
	}
}

func (i *Indexer) applyCreateThread(ctx context.Context, e *types.BoardLogEntry) error {
	if e.PostCID == nil || *e.PostCID == "" {
		return nil
	}
	threadCID := e.ThreadID
	tm, err := i.storage.LoadThreadMeta(ctx, threadCID)
	if err != nil {
		return err
	}
	if !bbslog.VerifyThreadMeta(tm) {
		return nil
	}
	if tm.BoardID != e.BoardID {
		return nil
	}
	tmCopy := *tm
	if tmCopy.RootPostCID == "" {
		tmCopy.RootPostCID = *e.PostCID
	}
	if err := i.upsertThread(ctx, threadCID, &tmCopy); err != nil {
		return err
	}
	return i.appendPost(ctx, e.BoardID, threadCID, *e.PostCID)
}

func (i *Indexer) applyAddPost(ctx context.Context, e *types.BoardLogEntry) error {
	if e.PostCID == nil || *e.PostCID == "" {
		return nil
	}
	return i.appendPost(ctx, e.BoardID, e.ThreadID, *e.PostCID)
}

func (i *Indexer) applyEditPost(ctx context.Context, e *types.BoardLogEntry) error {
	if e.OldPostCID == nil || *e.OldPostCID == "" || e.NewPostCID == nil || *e.NewPostCID == "" {
		return nil
	}
	oldCID := *e.OldPostCID
	newCID := *e.NewPostCID

	oldP, err := i.storage.LoadPost(ctx, oldCID)
	if err != nil {
		return err
	}
	if !bbslog.VerifyPost(oldP) {
		return nil
	}
	newP, err := i.storage.LoadPost(ctx, newCID)
	if err != nil {
		return err
	}
	if !bbslog.VerifyPost(newP) {
		return nil
	}
	if e.AuthorPubKey != oldP.AuthorPubKey || e.AuthorPubKey != newP.AuthorPubKey {
		return nil
	}

	if err := i.upsertPost(ctx, newCID, newP); err != nil {
		return err
	}
	_, err = i.db.ExecContext(ctx, `
		UPDATE thread_posts
		SET post_cid = ?
		WHERE thread_id = ? AND post_cid = ?
	`, newCID, e.ThreadID, oldCID)
	return err
}

func (i *Indexer) applyTombstone(ctx context.Context, e *types.BoardLogEntry) error {
	if e.TargetPostCID == nil || *e.TargetPostCID == "" {
		return nil
	}
	targetCID := *e.TargetPostCID
	p, err := i.storage.LoadPost(ctx, targetCID)
	if err != nil {
		return err
	}
	if !bbslog.VerifyPost(p) {
		return nil
	}
	if e.AuthorPubKey != p.AuthorPubKey {
		return nil
	}
	_, err = i.db.ExecContext(ctx, `
		UPDATE thread_posts
		SET tombstoned = 1, tombstone_reason = ?, tombstone_created_at = ?, tombstone_author_pubkey = ?
		WHERE thread_id = ? AND post_cid = ?
	`, strPtrOrEmpty(e.Reason), e.CreatedAt, e.AuthorPubKey, e.ThreadID, targetCID)
	return err
}

func (i *Indexer) appendPost(ctx context.Context, boardID, threadID, postCID string) error {
	p, err := i.storage.LoadPost(ctx, postCID)
	if err != nil {
		return err
	}
	if !bbslog.VerifyPost(p) {
		return nil
	}
	if p.ThreadID != threadID {
		return nil
	}
	if err := i.upsertPost(ctx, postCID, p); err != nil {
		return err
	}

	var nextOrdinal int
	if err := i.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(ordinal), -1) + 1 FROM thread_posts WHERE thread_id = ?`, threadID).Scan(&nextOrdinal); err != nil {
		return err
	}
	_, err = i.db.ExecContext(ctx, `
		INSERT INTO thread_posts (thread_id, ordinal, post_cid, tombstoned)
		VALUES (?, ?, ?, 0)
		ON CONFLICT(thread_id, ordinal) DO NOTHING
	`, threadID, nextOrdinal, postCID)
	if err != nil {
		return err
	}

	return nil
}

func strPtrOrEmpty(s *string) any {
	if s == nil {
		return nil
	}
	return *s
}

func (i *Indexer) migrate(ctx context.Context) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS boards (
			board_id TEXT PRIMARY KEY,
			board_meta_cid TEXT NOT NULL,
			title TEXT NOT NULL,
			description TEXT NOT NULL,
			created_at TEXT NOT NULL,
			created_by TEXT NOT NULL,
			signature TEXT NOT NULL,
			log_head_cid TEXT
		);`,
		`CREATE TABLE IF NOT EXISTS threads (
			thread_id TEXT PRIMARY KEY,
			board_id TEXT NOT NULL,
			title TEXT NOT NULL,
			root_post_cid TEXT NOT NULL,
			created_at TEXT NOT NULL,
			created_by TEXT NOT NULL,
			signature TEXT NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS posts (
			post_cid TEXT PRIMARY KEY,
			thread_id TEXT NOT NULL,
			parent_post_cid TEXT,
			author_pubkey TEXT NOT NULL,
			display_name TEXT NOT NULL,
			body_format TEXT NOT NULL,
			body_content TEXT NOT NULL,
			created_at TEXT NOT NULL,
			edited_at TEXT,
			signature TEXT NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS thread_posts (
			thread_id TEXT NOT NULL,
			ordinal INTEGER NOT NULL,
			post_cid TEXT NOT NULL,
			tombstoned INTEGER NOT NULL DEFAULT 0,
			tombstone_reason TEXT,
			tombstone_created_at TEXT,
			tombstone_author_pubkey TEXT,
			PRIMARY KEY(thread_id, ordinal)
		);`,
		`CREATE TABLE IF NOT EXISTS processed_logs (
			log_cid TEXT PRIMARY KEY,
			board_id TEXT NOT NULL,
			thread_id TEXT NOT NULL,
			op TEXT NOT NULL,
			created_at TEXT NOT NULL,
			author_pubkey TEXT NOT NULL,
			prev_log_cid TEXT,
			valid_sig INTEGER NOT NULL
		);`,
		`CREATE INDEX IF NOT EXISTS idx_threads_board ON threads(board_id);`,
		`CREATE INDEX IF NOT EXISTS idx_posts_thread ON posts(thread_id);`,
		`CREATE INDEX IF NOT EXISTS idx_posts_author ON posts(author_pubkey);`,
		`CREATE INDEX IF NOT EXISTS idx_posts_created ON posts(created_at);`,
		`CREATE INDEX IF NOT EXISTS idx_thread_posts_post ON thread_posts(thread_id, post_cid);`,
	}

	for _, s := range stmts {
		if _, err := i.db.ExecContext(ctx, s); err != nil {
			return err
		}
	}
	return nil
}

func (i *Indexer) upsertBoard(ctx context.Context, cid string, bm *types.BoardMeta) error {
	_, err := i.db.ExecContext(ctx, `
		INSERT INTO boards(board_id, board_meta_cid, title, description, created_at, created_by, signature, log_head_cid)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(board_id) DO UPDATE SET
			board_meta_cid=excluded.board_meta_cid,
			title=excluded.title,
			description=excluded.description,
			created_at=excluded.created_at,
			created_by=excluded.created_by,
			signature=excluded.signature,
			log_head_cid=excluded.log_head_cid
	`, bm.BoardID, cid, bm.Title, bm.Description, bm.CreatedAt, bm.CreatedBy, bm.Signature, strPtrOrEmpty(bm.LogHeadCID))
	return err
}

func (i *Indexer) setBoardLogHead(ctx context.Context, boardID, headCID string) error {
	_, err := i.db.ExecContext(ctx, `UPDATE boards SET log_head_cid = ? WHERE board_id = ?`, headCID, boardID)
	return err
}

func (i *Indexer) upsertThread(ctx context.Context, threadCID string, tm *types.ThreadMeta) error {
	_, err := i.db.ExecContext(ctx, `
		INSERT INTO threads(thread_id, board_id, title, root_post_cid, created_at, created_by, signature)
		VALUES(?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(thread_id) DO UPDATE SET
			board_id=excluded.board_id,
			title=excluded.title,
			root_post_cid=excluded.root_post_cid,
			created_at=excluded.created_at,
			created_by=excluded.created_by,
			signature=excluded.signature
	`, threadCID, tm.BoardID, tm.Title, tm.RootPostCID, tm.CreatedAt, tm.CreatedBy, tm.Signature)
	return err
}

func (i *Indexer) upsertPost(ctx context.Context, postCID string, p *types.Post) error {
	_, err := i.db.ExecContext(ctx, `
		INSERT INTO posts(post_cid, thread_id, parent_post_cid, author_pubkey, display_name, body_format, body_content, created_at, edited_at, signature)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(post_cid) DO UPDATE SET
			thread_id=excluded.thread_id,
			parent_post_cid=excluded.parent_post_cid,
			author_pubkey=excluded.author_pubkey,
			display_name=excluded.display_name,
			body_format=excluded.body_format,
			body_content=excluded.body_content,
			created_at=excluded.created_at,
			edited_at=excluded.edited_at,
			signature=excluded.signature
	`, postCID, p.ThreadID, strPtrOrEmpty(p.ParentPostCID), p.AuthorPubKey, p.DisplayName, p.Body.Format, p.Body.Content, p.CreatedAt, strPtrOrEmpty(p.EditedAt), p.Signature)
	return err
}

func (i *Indexer) isLogProcessed(ctx context.Context, cid string) (bool, error) {
	var n int
	if err := i.db.QueryRowContext(ctx, `SELECT COUNT(1) FROM processed_logs WHERE log_cid = ?`, cid).Scan(&n); err != nil {
		return false, err
	}
	return n > 0, nil
}

func (i *Indexer) insertProcessedLog(ctx context.Context, cid string, e *types.BoardLogEntry, validSig bool) error {
	v := 0
	if validSig {
		v = 1
	}
	_, err := i.db.ExecContext(ctx, `
		INSERT INTO processed_logs(log_cid, board_id, thread_id, op, created_at, author_pubkey, prev_log_cid, valid_sig)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(log_cid) DO NOTHING
	`, cid, e.BoardID, e.ThreadID, e.Op, e.CreatedAt, e.AuthorPubKey, strPtrOrEmpty(e.PrevLogCID), v)
	return err
}

func (i *Indexer) PruneOlderThan(ctx context.Context, ttl time.Duration) error {
	if ttl <= 0 {
		return nil
	}
	cutoff := time.Now().Add(-ttl).UTC().Format(time.RFC3339)
	_, err := i.db.ExecContext(ctx, `DELETE FROM processed_logs WHERE created_at < ?`, cutoff)
	return err
}
