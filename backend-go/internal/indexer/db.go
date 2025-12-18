package indexer

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// DB は indexer 用の永続化層インターフェースです。
type DB interface {
	// トランザクション実行（必要に応じて内部で BEGIN/COMMIT）
	WithTx(ctx context.Context, fn func(tx DB) error) error

	// ログシーケンス管理
	GetLastSequence(ctx context.Context) (int64, error)
	SetLastSequence(ctx context.Context, seq int64) error

	// Board 操作
	CreateBoard(ctx context.Context, b *Board) error
	UpdateBoard(ctx context.Context, b *Board) error
	GetBoard(ctx context.Context, id string) (*Board, error)
	ListBoards(ctx context.Context) ([]Board, error)

	// Thread 操作
	CreateThread(ctx context.Context, t *Thread) error
	UpdateThread(ctx context.Context, t *Thread) error
	GetThread(ctx context.Context, id string) (*Thread, error)
	ListThreadsByBoard(ctx context.Context, boardID string) ([]Thread, error)
	CloseThread(ctx context.Context, threadID string) error

	// Post 操作
	CreatePost(ctx context.Context, p *Post) error
	UpdatePost(ctx context.Context, p *Post) error
	GetPost(ctx context.Context, id string) (*Post, error)
	ListPostsByThread(ctx context.Context, threadID string) ([]Post, error)
	DeletePost(ctx context.Context, postID string) error

	// 検索
	SearchPosts(ctx context.Context, req *SearchPostsRequest) (*SearchPostsResponse, error)
	SearchThreads(ctx context.Context, req *SearchThreadsRequest) (*SearchThreadsResponse, error)

	// 終了処理
	Close() error
}

// sqliteDB は SQLite ベースの DB 実装です。
type sqliteDB struct {
	db *sql.DB
}

// sqliteTx はトランザクション中の DB 実装です。
type sqliteTx struct {
	tx *sql.Tx
}

// NewSQLiteDB は SQLite を利用した新しい DB を作成します。
// dsn には ":memory:" も利用できます。
func NewSQLiteDB(dsn string) (DB, error) {
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	// 外部キー制約などの設定
	if _, err := db.Exec(`PRAGMA foreign_keys = ON;`); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("enable foreign_keys: %w", err)
	}

	s := &sqliteDB{db: db}
	if err := s.initSchema(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

// スキーマ定義
func (s *sqliteDB) initSchema() error {
	schema := []string{
		`CREATE TABLE IF NOT EXISTS boards (
            id TEXT PRIMARY KEY,
            name TEXT NOT NULL,
            description TEXT NOT NULL,
            created_at TIMESTAMP NOT NULL,
            updated_at TIMESTAMP NOT NULL,
            thread_count INTEGER NOT NULL DEFAULT 0
        );`,
		`CREATE TABLE IF NOT EXISTS threads (
            id TEXT PRIMARY KEY,
            board_id TEXT NOT NULL,
            title TEXT NOT NULL,
            author_id TEXT NOT NULL,
            created_at TIMESTAMP NOT NULL,
            updated_at TIMESTAMP NOT NULL,
            post_count INTEGER NOT NULL DEFAULT 0,
            is_closed INTEGER NOT NULL DEFAULT 0,
            FOREIGN KEY(board_id) REFERENCES boards(id) ON DELETE CASCADE
        );`,
		`CREATE TABLE IF NOT EXISTS posts (
            id TEXT PRIMARY KEY,
            thread_id TEXT NOT NULL,
            board_id TEXT NOT NULL,
            author_id TEXT NOT NULL,
            content TEXT NOT NULL,
            created_at TIMESTAMP NOT NULL,
            updated_at TIMESTAMP NOT NULL,
            is_deleted INTEGER NOT NULL DEFAULT 0,
            reply_to TEXT,
            FOREIGN KEY(thread_id) REFERENCES threads(id) ON DELETE CASCADE,
            FOREIGN KEY(board_id) REFERENCES boards(id) ON DELETE CASCADE
        );`,
		`CREATE TABLE IF NOT EXISTS log_state (
            id INTEGER PRIMARY KEY CHECK (id = 1),
            last_seq INTEGER NOT NULL
        );`,
		`CREATE INDEX IF NOT EXISTS idx_posts_thread_id_created_at ON posts(thread_id, created_at);`,
		`CREATE INDEX IF NOT EXISTS idx_posts_board_id_created_at ON posts(board_id, created_at);`,
		`CREATE INDEX IF NOT EXISTS idx_posts_author_id ON posts(author_id);`,
		`CREATE INDEX IF NOT EXISTS idx_threads_board_id_created_at ON threads(board_id, created_at);`,
	}

	for _, q := range schema {
		if _, err := s.db.Exec(q); err != nil {
			return fmt.Errorf("init schema: %w", err)
		}
	}
	return nil
}

// ========================================
// トランザクション管理
// ========================================

func (s *sqliteDB) WithTx(ctx context.Context, fn func(tx DB) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	w := &sqliteTx{tx: tx}
	if err := fn(w); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}
	return nil
}

// トランザクションの中で更に WithTx が呼ばれた場合は、そのまま同じ tx を使う。
func (t *sqliteTx) WithTx(ctx context.Context, fn func(tx DB) error) error {
	return fn(t)
}

// ========================================
// ログシーケンス管理
// ========================================

func (s *sqliteDB) GetLastSequence(ctx context.Context) (int64, error) {
	var seq int64
	err := s.db.QueryRowContext(ctx, `SELECT last_seq FROM log_state WHERE id = 1`).Scan(&seq)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("get last_seq: %w", err)
	}
	return seq, nil
}

func (s *sqliteDB) SetLastSequence(ctx context.Context, seq int64) error {
	_, err := s.db.ExecContext(ctx, `
        INSERT INTO log_state (id, last_seq) VALUES (1, ?)
        ON CONFLICT(id) DO UPDATE SET last_seq = excluded.last_seq
    `, seq)
	if err != nil {
		return fmt.Errorf("set last_seq: %w", err)
	}
	return nil
}

func (t *sqliteTx) GetLastSequence(ctx context.Context) (int64, error) {
	var seq int64
	err := t.tx.QueryRowContext(ctx, `SELECT last_seq FROM log_state WHERE id = 1`).Scan(&seq)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("get last_seq(tx): %w", err)
	}
	return seq, nil
}

func (t *sqliteTx) SetLastSequence(ctx context.Context, seq int64) error {
	_, err := t.tx.ExecContext(ctx, `
        INSERT INTO log_state (id, last_seq) VALUES (1, ?)
        ON CONFLICT(id) DO UPDATE SET last_seq = excluded.last_seq
    `, seq)
	if err != nil {
		return fmt.Errorf("set last_seq(tx): %w", err)
	}
	return nil
}

// ========================================
// Board 操作
// ========================================

func (s *sqliteDB) CreateBoard(ctx context.Context, b *Board) error {
	now := time.Now().UTC()
	if b.CreatedAt.IsZero() {
		b.CreatedAt = now
	}
	if b.UpdatedAt.IsZero() {
		b.UpdatedAt = now
	}
	_, err := s.db.ExecContext(ctx, `
        INSERT INTO boards (id, name, description, created_at, updated_at, thread_count)
        VALUES (?, ?, ?, ?, ?, ?)
    `, b.ID, b.Name, b.Description, b.CreatedAt, b.UpdatedAt, b.ThreadCount)
	if err != nil {
		return fmt.Errorf("create board: %w", err)
	}
	return nil
}

func (t *sqliteTx) CreateBoard(ctx context.Context, b *Board) error {
	now := time.Now().UTC()
	if b.CreatedAt.IsZero() {
		b.CreatedAt = now
	}
	if b.UpdatedAt.IsZero() {
		b.UpdatedAt = now
	}
	_, err := t.tx.ExecContext(ctx, `
        INSERT INTO boards (id, name, description, created_at, updated_at, thread_count)
        VALUES (?, ?, ?, ?, ?, ?)
    `, b.ID, b.Name, b.Description, b.CreatedAt, b.UpdatedAt, b.ThreadCount)
	if err != nil {
		return fmt.Errorf("create board(tx): %w", err)
	}
	return nil
}

func (s *sqliteDB) UpdateBoard(ctx context.Context, b *Board) error {
	b.UpdatedAt = time.Now().UTC()
	_, err := s.db.ExecContext(ctx, `
        UPDATE boards
        SET name = ?, description = ?, updated_at = ?, thread_count = ?
        WHERE id = ?
    `, b.Name, b.Description, b.UpdatedAt, b.ThreadCount, b.ID)
	if err != nil {
		return fmt.Errorf("update board: %w", err)
	}
	return nil
}

func (t *sqliteTx) UpdateBoard(ctx context.Context, b *Board) error {
	b.UpdatedAt = time.Now().UTC()
	_, err := t.tx.ExecContext(ctx, `
        UPDATE boards
        SET name = ?, description = ?, updated_at = ?, thread_count = ?
        WHERE id = ?
    `, b.Name, b.Description, b.UpdatedAt, b.ThreadCount, b.ID)
	if err != nil {
		return fmt.Errorf("update board(tx): %w", err)
	}
	return nil
}

func (s *sqliteDB) GetBoard(ctx context.Context, id string) (*Board, error) {
	row := s.db.QueryRowContext(ctx, `
        SELECT id, name, description, created_at, updated_at, thread_count
        FROM boards WHERE id = ?
    `, id)
	var b Board
	if err := row.Scan(&b.ID, &b.Name, &b.Description, &b.CreatedAt, &b.UpdatedAt, &b.ThreadCount); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get board: %w", err)
	}
	return &b, nil
}

func (t *sqliteTx) GetBoard(ctx context.Context, id string) (*Board, error) {
	row := t.tx.QueryRowContext(ctx, `
        SELECT id, name, description, created_at, updated_at, thread_count
        FROM boards WHERE id = ?
    `, id)
	var b Board
	if err := row.Scan(&b.ID, &b.Name, &b.Description, &b.CreatedAt, &b.UpdatedAt, &b.ThreadCount); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get board(tx): %w", err)
	}
	return &b, nil
}

func (s *sqliteDB) ListBoards(ctx context.Context) ([]Board, error) {
	rows, err := s.db.QueryContext(ctx, `
        SELECT id, name, description, created_at, updated_at, thread_count
        FROM boards
        ORDER BY created_at ASC
    `)
	if err != nil {
		return nil, fmt.Errorf("list boards: %w", err)
	}
	defer rows.Close()

	var res []Board
	for rows.Next() {
		var b Board
		if err := rows.Scan(&b.ID, &b.Name, &b.Description, &b.CreatedAt, &b.UpdatedAt, &b.ThreadCount); err != nil {
			return nil, fmt.Errorf("list boards scan: %w", err)
		}
		res = append(res, b)
	}
	return res, nil
}

func (t *sqliteTx) ListBoards(ctx context.Context) ([]Board, error) {
	rows, err := t.tx.QueryContext(ctx, `
        SELECT id, name, description, created_at, updated_at, thread_count
        FROM boards
        ORDER BY created_at ASC
    `)
	if err != nil {
		return nil, fmt.Errorf("list boards(tx): %w", err)
	}
	defer rows.Close()

	var res []Board
	for rows.Next() {
		var b Board
		if err := rows.Scan(&b.ID, &b.Name, &b.Description, &b.CreatedAt, &b.UpdatedAt, &b.ThreadCount); err != nil {
			return nil, fmt.Errorf("list boards scan(tx): %w", err)
		}
		res = append(res, b)
	}
	return res, nil
}

// ========================================
// Thread 操作
// ========================================

func (s *sqliteDB) CreateThread(ctx context.Context, t *Thread) error {
	now := time.Now().UTC()
	if t.CreatedAt.IsZero() {
		t.CreatedAt = now
	}
	if t.UpdatedAt.IsZero() {
		t.UpdatedAt = now
	}
	_, err := s.db.ExecContext(ctx, `
        INSERT INTO threads (id, board_id, title, author_id, created_at, updated_at, post_count, is_closed)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?)
    `, t.ID, t.BoardID, t.Title, t.AuthorID, t.CreatedAt, t.UpdatedAt, t.PostCount, boolToInt(t.IsClosed))
	if err != nil {
		return fmt.Errorf("create thread: %w", err)
	}
	// 対応する board のスレッド数を+1
	_, err = s.db.ExecContext(ctx, `
        UPDATE boards SET thread_count = thread_count + 1, updated_at = ?
        WHERE id = ?
    `, now, t.BoardID)
	if err != nil {
		return fmt.Errorf("increment board.thread_count: %w", err)
	}
	return nil
}

func (t *sqliteTx) CreateThread(ctx context.Context, th *Thread) error {
	now := time.Now().UTC()
	if th.CreatedAt.IsZero() {
		th.CreatedAt = now
	}
	if th.UpdatedAt.IsZero() {
		th.UpdatedAt = now
	}
	_, err := t.tx.ExecContext(ctx, `
        INSERT INTO threads (id, board_id, title, author_id, created_at, updated_at, post_count, is_closed)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?)
    `, th.ID, th.BoardID, th.Title, th.AuthorID, th.CreatedAt, th.UpdatedAt, th.PostCount, boolToInt(th.IsClosed))
	if err != nil {
		return fmt.Errorf("create thread(tx): %w", err)
	}
	_, err = t.tx.ExecContext(ctx, `
        UPDATE boards SET thread_count = thread_count + 1, updated_at = ?
        WHERE id = ?
    `, now, th.BoardID)
	if err != nil {
		return fmt.Errorf("increment board.thread_count(tx): %w", err)
	}
	return nil
}

func (s *sqliteDB) UpdateThread(ctx context.Context, t *Thread) error {
	t.UpdatedAt = time.Now().UTC()
	_, err := s.db.ExecContext(ctx, `
        UPDATE threads
        SET title = ?, author_id = ?, updated_at = ?, post_count = ?, is_closed = ?
        WHERE id = ?
    `, t.Title, t.AuthorID, t.UpdatedAt, t.PostCount, boolToInt(t.IsClosed), t.ID)
	if err != nil {
		return fmt.Errorf("update thread: %w", err)
	}
	return nil
}

func (t *sqliteTx) UpdateThread(ctx context.Context, th *Thread) error {
	th.UpdatedAt = time.Now().UTC()
	_, err := t.tx.ExecContext(ctx, `
        UPDATE threads
        SET title = ?, author_id = ?, updated_at = ?, post_count = ?, is_closed = ?
        WHERE id = ?
    `, th.Title, th.AuthorID, th.UpdatedAt, th.PostCount, boolToInt(th.IsClosed), th.ID)
	if err != nil {
		return fmt.Errorf("update thread(tx): %w", err)
	}
	return nil
}

func (s *sqliteDB) GetThread(ctx context.Context, id string) (*Thread, error) {
	row := s.db.QueryRowContext(ctx, `
        SELECT id, board_id, title, author_id, created_at, updated_at, post_count, is_closed
        FROM threads WHERE id = ?
    `, id)
	var t Thread
	var closed int
	if err := row.Scan(&t.ID, &t.BoardID, &t.Title, &t.AuthorID, &t.CreatedAt, &t.UpdatedAt, &t.PostCount, &closed); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get thread: %w", err)
	}
	t.IsClosed = closed != 0
	return &t, nil
}

func (t *sqliteTx) GetThread(ctx context.Context, id string) (*Thread, error) {
	row := t.tx.QueryRowContext(ctx, `
        SELECT id, board_id, title, author_id, created_at, updated_at, post_count, is_closed
        FROM threads WHERE id = ?
    `, id)
	var th Thread
	var closed int
	if err := row.Scan(&th.ID, &th.BoardID, &th.Title, &th.AuthorID, &th.CreatedAt, &th.UpdatedAt, &th.PostCount, &closed); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get thread(tx): %w", err)
	}
	th.IsClosed = closed != 0
	return &th, nil
}

func (s *sqliteDB) ListThreadsByBoard(ctx context.Context, boardID string) ([]Thread, error) {
	rows, err := s.db.QueryContext(ctx, `
        SELECT id, board_id, title, author_id, created_at, updated_at, post_count, is_closed
        FROM threads
        WHERE board_id = ?
        ORDER BY created_at ASC
    `, boardID)
	if err != nil {
		return nil, fmt.Errorf("list threads: %w", err)
	}
	defer rows.Close()

	var res []Thread
	for rows.Next() {
		var t Thread
		var closed int
		if err := rows.Scan(&t.ID, &t.BoardID, &t.Title, &t.AuthorID, &t.CreatedAt, &t.UpdatedAt, &t.PostCount, &closed); err != nil {
			return nil, fmt.Errorf("list threads scan: %w", err)
		}
		t.IsClosed = closed != 0
		res = append(res, t)
	}
	return res, nil
}

func (t *sqliteTx) ListThreadsByBoard(ctx context.Context, boardID string) ([]Thread, error) {
	rows, err := t.tx.QueryContext(ctx, `
        SELECT id, board_id, title, author_id, created_at, updated_at, post_count, is_closed
        FROM threads
        WHERE board_id = ?
        ORDER BY created_at ASC
    `, boardID)
	if err != nil {
		return nil, fmt.Errorf("list threads(tx): %w", err)
	}
	defer rows.Close()

	var res []Thread
	for rows.Next() {
		var th Thread
		var closed int
		if err := rows.Scan(&th.ID, &th.BoardID, &th.Title, &th.AuthorID, &th.CreatedAt, &th.UpdatedAt, &th.PostCount, &closed); err != nil {
			return nil, fmt.Errorf("list threads scan(tx): %w", err)
		}
		th.IsClosed = closed != 0
		res = append(res, th)
	}
	return res, nil
}

func (s *sqliteDB) CloseThread(ctx context.Context, threadID string) error {
	_, err := s.db.ExecContext(ctx, `
        UPDATE threads SET is_closed = 1, updated_at = ?
        WHERE id = ?
    `, time.Now().UTC(), threadID)
	if err != nil {
		return fmt.Errorf("close thread: %w", err)
	}
	return nil
}

func (t *sqliteTx) CloseThread(ctx context.Context, threadID string) error {
	_, err := t.tx.ExecContext(ctx, `
        UPDATE threads SET is_closed = 1, updated_at = ?
        WHERE id = ?
    `, time.Now().UTC(), threadID)
	if err != nil {
		return fmt.Errorf("close thread(tx): %w", err)
	}
	return nil
}

// ========================================
// Post 操作
// ========================================

func (s *sqliteDB) CreatePost(ctx context.Context, p *Post) error {
	now := time.Now().UTC()
	if p.CreatedAt.IsZero() {
		p.CreatedAt = now
	}
	if p.UpdatedAt.IsZero() {
		p.UpdatedAt = now
	}
	_, err := s.db.ExecContext(ctx, `
        INSERT INTO posts (id, thread_id, board_id, author_id, content, created_at, updated_at, is_deleted, reply_to)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
    `, p.ID, p.ThreadID, p.BoardID, p.AuthorID, p.Content, p.CreatedAt, p.UpdatedAt, boolToInt(p.IsDeleted), nullIfEmpty(p.ReplyTo))
	if err != nil {
		return fmt.Errorf("create post: %w", err)
	}
	_, err = s.db.ExecContext(ctx, `
        UPDATE threads SET post_count = post_count + 1, updated_at = ?
        WHERE id = ?
    `, now, p.ThreadID)
	if err != nil {
		return fmt.Errorf("increment thread.post_count: %w", err)
	}
	return nil
}

func (t *sqliteTx) CreatePost(ctx context.Context, p *Post) error {
	now := time.Now().UTC()
	if p.CreatedAt.IsZero() {
		p.CreatedAt = now
	}
	if p.UpdatedAt.IsZero() {
		p.UpdatedAt = now
	}
	_, err := t.tx.ExecContext(ctx, `
        INSERT INTO posts (id, thread_id, board_id, author_id, content, created_at, updated_at, is_deleted, reply_to)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
    `, p.ID, p.ThreadID, p.BoardID, p.AuthorID, p.Content, p.CreatedAt, p.UpdatedAt, boolToInt(p.IsDeleted), nullIfEmpty(p.ReplyTo))
	if err != nil {
		return fmt.Errorf("create post(tx): %w", err)
	}
	_, err = t.tx.ExecContext(ctx, `
        UPDATE threads SET post_count = post_count + 1, updated_at = ?
        WHERE id = ?
    `, now, p.ThreadID)
	if err != nil {
		return fmt.Errorf("increment thread.post_count(tx): %w", err)
	}
	return nil
}

func (s *sqliteDB) UpdatePost(ctx context.Context, p *Post) error {
	p.UpdatedAt = time.Now().UTC()
	_, err := s.db.ExecContext(ctx, `
        UPDATE posts
        SET content = ?, updated_at = ?, is_deleted = ?, reply_to = ?
        WHERE id = ?
    `, p.Content, p.UpdatedAt, boolToInt(p.IsDeleted), nullIfEmpty(p.ReplyTo), p.ID)
	if err != nil {
		return fmt.Errorf("update post: %w", err)
	}
	return nil
}

func (t *sqliteTx) UpdatePost(ctx context.Context, p *Post) error {
	p.UpdatedAt = time.Now().UTC()
	_, err := t.tx.ExecContext(ctx, `
        UPDATE posts
        SET content = ?, updated_at = ?, is_deleted = ?, reply_to = ?
        WHERE id = ?
    `, p.Content, p.UpdatedAt, boolToInt(p.IsDeleted), nullIfEmpty(p.ReplyTo), p.ID)
	if err != nil {
		return fmt.Errorf("update post(tx): %w", err)
	}
	return nil
}

func (s *sqliteDB) GetPost(ctx context.Context, id string) (*Post, error) {
	row := s.db.QueryRowContext(ctx, `
        SELECT id, thread_id, board_id, author_id, content, created_at, updated_at, is_deleted, reply_to
        FROM posts WHERE id = ?
    `, id)
	var p Post
	var deleted int
	var replyTo sql.NullString
	if err := row.Scan(&p.ID, &p.ThreadID, &p.BoardID, &p.AuthorID, &p.Content, &p.CreatedAt, &p.UpdatedAt, &deleted, &replyTo); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get post: %w", err)
	}
	p.IsDeleted = deleted != 0
	if replyTo.Valid {
		p.ReplyTo = replyTo.String
	}
	return &p, nil
}

func (t *sqliteTx) GetPost(ctx context.Context, id string) (*Post, error) {
	row := t.tx.QueryRowContext(ctx, `
        SELECT id, thread_id, board_id, author_id, content, created_at, updated_at, is_deleted, reply_to
        FROM posts WHERE id = ?
    `, id)
	var p Post
	var deleted int
	var replyTo sql.NullString
	if err := row.Scan(&p.ID, &p.ThreadID, &p.BoardID, &p.AuthorID, &p.Content, &p.CreatedAt, &p.UpdatedAt, &deleted, &replyTo); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get post(tx): %w", err)
	}
	p.IsDeleted = deleted != 0
	if replyTo.Valid {
		p.ReplyTo = replyTo.String
	}
	return &p, nil
}

func (s *sqliteDB) ListPostsByThread(ctx context.Context, threadID string) ([]Post, error) {
	rows, err := s.db.QueryContext(ctx, `
        SELECT id, thread_id, board_id, author_id, content, created_at, updated_at, is_deleted, reply_to
        FROM posts
        WHERE thread_id = ?
        ORDER BY created_at ASC
    `, threadID)
	if err != nil {
		return nil, fmt.Errorf("list posts: %w", err)
	}
	defer rows.Close()

	var res []Post
	for rows.Next() {
		var p Post
		var deleted int
		var replyTo sql.NullString
		if err := rows.Scan(&p.ID, &p.ThreadID, &p.BoardID, &p.AuthorID, &p.Content, &p.CreatedAt, &p.UpdatedAt, &deleted, &replyTo); err != nil {
			return nil, fmt.Errorf("list posts scan: %w", err)
		}
		p.IsDeleted = deleted != 0
		if replyTo.Valid {
			p.ReplyTo = replyTo.String
		}
		res = append(res, p)
	}
	return res, nil
}

func (t *sqliteTx) ListPostsByThread(ctx context.Context, threadID string) ([]Post, error) {
	rows, err := t.tx.QueryContext(ctx, `
        SELECT id, thread_id, board_id, author_id, content, created_at, updated_at, is_deleted, reply_to
        FROM posts
        WHERE thread_id = ?
        ORDER BY created_at ASC
    `, threadID)
	if err != nil {
		return nil, fmt.Errorf("list posts(tx): %w", err)
	}
	defer rows.Close()

	var res []Post
	for rows.Next() {
		var p Post
		var deleted int
		var replyTo sql.NullString
		if err := rows.Scan(&p.ID, &p.ThreadID, &p.BoardID, &p.AuthorID, &p.Content, &p.CreatedAt, &p.UpdatedAt, &deleted, &replyTo); err != nil {
			return nil, fmt.Errorf("list posts scan(tx): %w", err)
		}
		p.IsDeleted = deleted != 0
		if replyTo.Valid {
			p.ReplyTo = replyTo.String
		}
		res = append(res, p)
	}
	return res, nil
}

func (s *sqliteDB) DeletePost(ctx context.Context, postID string) error {
	// post 情報取得（thread_id 用）
	var threadID string
	err := s.db.QueryRowContext(ctx, `SELECT thread_id FROM posts WHERE id = ?`, postID).Scan(&threadID)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return fmt.Errorf("delete post get thread: %w", err)
	}

	_, err = s.db.ExecContext(ctx, `
        UPDATE posts SET is_deleted = 1, updated_at = ?
        WHERE id = ?
    `, time.Now().UTC(), postID)
	if err != nil {
		return fmt.Errorf("delete post: %w", err)
	}
	_, err = s.db.ExecContext(ctx, `
        UPDATE threads SET post_count = CASE WHEN post_count > 0 THEN post_count - 1 ELSE 0 END, updated_at = ?
        WHERE id = ?
    `, time.Now().UTC(), threadID)
	if err != nil {
		return fmt.Errorf("decrement thread.post_count: %w", err)
	}
	return nil
}

func (t *sqliteTx) DeletePost(ctx context.Context, postID string) error {
	var threadID string
	err := t.tx.QueryRowContext(ctx, `SELECT thread_id FROM posts WHERE id = ?`, postID).Scan(&threadID)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return fmt.Errorf("delete post get thread(tx): %w", err)
	}

	_, err = t.tx.ExecContext(ctx, `
        UPDATE posts SET is_deleted = 1, updated_at = ?
        WHERE id = ?
    `, time.Now().UTC(), postID)
	if err != nil {
		return fmt.Errorf("delete post(tx): %w", err)
	}
	_, err = t.tx.ExecContext(ctx, `
        UPDATE threads SET post_count = CASE WHEN post_count > 0 THEN post_count - 1 ELSE 0 END, updated_at = ?
        WHERE id = ?
    `, time.Now().UTC(), threadID)
	if err != nil {
		return fmt.Errorf("decrement thread.post_count(tx): %w", err)
	}
	return nil
}

// ========================================
// 検索系
// ========================================

func (s *sqliteDB) SearchPosts(ctx context.Context, req *SearchPostsRequest) (*SearchPostsResponse, error) {
	if req.Limit <= 0 {
		req.Limit = 20
	}
	if req.Offset < 0 {
		req.Offset = 0
	}

	where := []string{"is_deleted = 0"}
	args := []any{}

	if req.Query != "" {
		where = append(where, "content LIKE ?")
		args = append(args, "%"+req.Query+"%")
	}
	if req.BoardID != "" {
		where = append(where, "board_id = ?")
		args = append(args, req.BoardID)
	}
	if req.ThreadID != "" {
		where = append(where, "thread_id = ?")
		args = append(args, req.ThreadID)
	}
	if req.AuthorID != "" {
		where = append(where, "author_id = ?")
		args = append(args, req.AuthorID)
	}
	whereSQL := strings.Join(where, " AND ")

	// カウント
	countQuery := `SELECT COUNT(*) FROM posts WHERE ` + whereSQL
	var total int
	if err := s.db.QueryRowContext(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, fmt.Errorf("search posts count: %w", err)
	}

	// データ取得
	query := `
        SELECT id, thread_id, board_id, author_id, content, created_at, updated_at, is_deleted, reply_to
        FROM posts
        WHERE ` + whereSQL + `
        ORDER BY created_at DESC
        LIMIT ? OFFSET ?
    `
	argsWithLimit := append(args, req.Limit, req.Offset)
	rows, err := s.db.QueryContext(ctx, query, argsWithLimit...)
	if err != nil {
		return nil, fmt.Errorf("search posts query: %w", err)
	}
	defer rows.Close()

	var posts []Post
	for rows.Next() {
		var p Post
		var deleted int
		var replyTo sql.NullString
		if err := rows.Scan(&p.ID, &p.ThreadID, &p.BoardID, &p.AuthorID, &p.Content,
			&p.CreatedAt, &p.UpdatedAt, &deleted, &replyTo); err != nil {
			return nil, fmt.Errorf("search posts scan: %w", err)
		}
		p.IsDeleted = deleted != 0
		if replyTo.Valid {
			p.ReplyTo = replyTo.String
		}
		posts = append(posts, p)
	}

	return &SearchPostsResponse{
		Posts:      posts,
		TotalCount: total,
		Limit:      req.Limit,
		Offset:     req.Offset,
	}, nil
}

func (t *sqliteTx) SearchPosts(ctx context.Context, req *SearchPostsRequest) (*SearchPostsResponse, error) {
	// トランザクション内でも特に変わらないので、db 実装とほぼ同じ
	if req.Limit <= 0 {
		req.Limit = 20
	}
	if req.Offset < 0 {
		req.Offset = 0
	}

	where := []string{"is_deleted = 0"}
	args := []any{}

	if req.Query != "" {
		where = append(where, "content LIKE ?")
		args = append(args, "%"+req.Query+"%")
	}
	if req.BoardID != "" {
		where = append(where, "board_id = ?")
		args = append(args, req.BoardID)
	}
	if req.ThreadID != "" {
		where = append(where, "thread_id = ?")
		args = append(args, req.ThreadID)
	}
	if req.AuthorID != "" {
		where = append(where, "author_id = ?")
		args = append(args, req.AuthorID)
	}
	whereSQL := strings.Join(where, " AND ")

	countQuery := `SELECT COUNT(*) FROM posts WHERE ` + whereSQL
	var total int
	if err := t.tx.QueryRowContext(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, fmt.Errorf("search posts count(tx): %w", err)
	}

	query := `
        SELECT id, thread_id, board_id, author_id, content, created_at, updated_at, is_deleted, reply_to
        FROM posts
        WHERE ` + whereSQL + `
        ORDER BY created_at DESC
        LIMIT ? OFFSET ?
    `
	argsWithLimit := append(args, req.Limit, req.Offset)
	rows, err := t.tx.QueryContext(ctx, query, argsWithLimit...)
	if err != nil {
		return nil, fmt.Errorf("search posts query(tx): %w", err)
	}
	defer rows.Close()

	var posts []Post
	for rows.Next() {
		var p Post
		var deleted int
		var replyTo sql.NullString
		if err := rows.Scan(&p.ID, &p.ThreadID, &p.BoardID, &p.AuthorID, &p.Content,
			&p.CreatedAt, &p.UpdatedAt, &deleted, &replyTo); err != nil {
			return nil, fmt.Errorf("search posts scan(tx): %w", err)
		}
		p.IsDeleted = deleted != 0
		if replyTo.Valid {
			p.ReplyTo = replyTo.String
		}
		posts = append(posts, p)
	}

	return &SearchPostsResponse{
		Posts:      posts,
		TotalCount: total,
		Limit:      req.Limit,
		Offset:     req.Offset,
	}, nil
}

func (s *sqliteDB) SearchThreads(ctx context.Context, req *SearchThreadsRequest) (*SearchThreadsResponse, error) {
	if req.Limit <= 0 {
		req.Limit = 20
	}
	if req.Offset < 0 {
		req.Offset = 0
	}

	where := []string{"1=1"}
	args := []any{}

	if req.Query != "" {
		where = append(where, "title LIKE ?")
		args = append(args, "%"+req.Query+"%")
	}
	if req.BoardID != "" {
		where = append(where, "board_id = ?")
		args = append(args, req.BoardID)
	}
	whereSQL := strings.Join(where, " AND ")

	countQuery := `SELECT COUNT(*) FROM threads WHERE ` + whereSQL
	var total int
	if err := s.db.QueryRowContext(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, fmt.Errorf("search threads count: %w", err)
	}

	query := `
        SELECT id, board_id, title, author_id, created_at, updated_at, post_count, is_closed
        FROM threads
        WHERE ` + whereSQL + `
        ORDER BY created_at DESC
        LIMIT ? OFFSET ?
    `
	argsWithLimit := append(args, req.Limit, req.Offset)
	rows, err := s.db.QueryContext(ctx, query, argsWithLimit...)
	if err != nil {
		return nil, fmt.Errorf("search threads query: %w", err)
	}
	defer rows.Close()

	var threads []Thread
	for rows.Next() {
		var th Thread
		var closed int
		if err := rows.Scan(&th.ID, &th.BoardID, &th.Title, &th.AuthorID,
			&th.CreatedAt, &th.UpdatedAt, &th.PostCount, &closed); err != nil {
			return nil, fmt.Errorf("search threads scan: %w", err)
		}
		th.IsClosed = closed != 0
		threads = append(threads, th)
	}

	return &SearchThreadsResponse{
		Threads:    threads,
		TotalCount: total,
		Limit:      req.Limit,
		Offset:     req.Offset,
	}, nil
}

func (t *sqliteTx) SearchThreads(ctx context.Context, req *SearchThreadsRequest) (*SearchThreadsResponse, error) {
	if req.Limit <= 0 {
		req.Limit = 20
	}
	if req.Offset < 0 {
		req.Offset = 0
	}

	where := []string{"1=1"}
	args := []any{}

	if req.Query != "" {
		where = append(where, "title LIKE ?")
		args = append(args, "%"+req.Query+"%")
	}
	if req.BoardID != "" {
		where = append(where, "board_id = ?")
		args = append(args, req.BoardID)
	}
	whereSQL := strings.Join(where, " AND ")

	countQuery := `SELECT COUNT(*) FROM threads WHERE ` + whereSQL
	var total int
	if err := t.tx.QueryRowContext(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, fmt.Errorf("search threads count(tx): %w", err)
	}

	query := `
        SELECT id, board_id, title, author_id, created_at, updated_at, post_count, is_closed
        FROM threads
        WHERE ` + whereSQL + `
        ORDER BY created_at DESC
        LIMIT ? OFFSET ?
    `
	argsWithLimit := append(args, req.Limit, req.Offset)
	rows, err := t.tx.QueryContext(ctx, query, argsWithLimit...)
	if err != nil {
		return nil, fmt.Errorf("search threads query(tx): %w", err)
	}
	defer rows.Close()

	var threads []Thread
	for rows.Next() {
		var th Thread
		var closed int
		if err := rows.Scan(&th.ID, &th.BoardID, &th.Title, &th.AuthorID,
			&th.CreatedAt, &th.UpdatedAt, &th.PostCount, &closed); err != nil {
			return nil, fmt.Errorf("search threads scan(tx): %w", err)
		}
		th.IsClosed = closed != 0
		threads = append(threads, th)
	}

	return &SearchThreadsResponse{
		Threads:    threads,
		TotalCount: total,
		Limit:      req.Limit,
		Offset:     req.Offset,
	}, nil
}

// ========================================
// ユーティリティ
// ========================================

func (s *sqliteDB) Close() error {
	return s.db.Close()
}

func (t *sqliteTx) Close() error {
	// トランザクションのライフサイクルは WithTx で管理するので、ここでは何もしない。
	return nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}
