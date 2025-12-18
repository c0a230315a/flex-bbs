package indexer

import "time"

// Board は掲示板を表すモデルです。
type Board struct {
	ID          string    `db:"id" json:"id"`                     // 掲示板ID
	Name        string    `db:"name" json:"name"`                 // 掲示板名
	Description string    `db:"description" json:"description"`   // 説明
	CreatedAt   time.Time `db:"created_at" json:"created_at"`     // 作成日時
	UpdatedAt   time.Time `db:"updated_at" json:"updated_at"`     // 更新日時
	ThreadCount int       `db:"thread_count" json:"thread_count"` // スレッド数
}

// Thread はスレッドを表すモデルです。
type Thread struct {
	ID        string    `db:"id" json:"id"`                 // スレッドID
	BoardID   string    `db:"board_id" json:"board_id"`     // 所属掲示板ID
	Title     string    `db:"title" json:"title"`           // スレッドタイトル
	AuthorID  string    `db:"author_id" json:"author_id"`   // 作成者ID
	CreatedAt time.Time `db:"created_at" json:"created_at"` // 作成日時
	UpdatedAt time.Time `db:"updated_at" json:"updated_at"` // 更新日時
	PostCount int       `db:"post_count" json:"post_count"` // 投稿数
	IsClosed  bool      `db:"is_closed" json:"is_closed"`   // クローズ済みか
}

// Post は投稿を表すモデルです。
type Post struct {
	ID        string    `db:"id" json:"id"`                 // 投稿ID
	ThreadID  string    `db:"thread_id" json:"thread_id"`   // 所属スレッドID
	BoardID   string    `db:"board_id" json:"board_id"`     // 所属掲示板ID
	AuthorID  string    `db:"author_id" json:"author_id"`   // 投稿者ID
	Content   string    `db:"content" json:"content"`       // 投稿内容
	CreatedAt time.Time `db:"created_at" json:"created_at"` // 投稿日時
	UpdatedAt time.Time `db:"updated_at" json:"updated_at"` // 更新日時
	IsDeleted bool      `db:"is_deleted" json:"is_deleted"` // 削除済みか
	ReplyTo   string    `db:"reply_to" json:"reply_to"`     // 返信先投稿ID（空文字列の場合はなし）
}

// BoardLogEntry は掲示板の操作ログを表すエントリです。
type BoardLogEntry struct {
	SeqNum    int64     `json:"seq_num"`    // シーケンス番号
	Timestamp time.Time `json:"timestamp"`  // タイムスタンプ
	Operation string    `json:"operation"`  // 操作種別（create_board, create_thread, create_post等）
	EntityID  string    `json:"entity_id"`  // 対象エンティティID
	Data      string    `json:"data"`       // JSONエンコードされたデータ
	Signature string    `json:"signature"`  // 署名（将来の拡張用）
}

// SearchPostsRequest は投稿検索のリクエストです。
type SearchPostsRequest struct {
	Query    string `json:"query"`              // 検索クエリ
	BoardID  string `json:"board_id,omitempty"` // 掲示板IDでフィルタ
	ThreadID string `json:"thread_id,omitempty"`// スレッドIDでフィルタ
	AuthorID string `json:"author_id,omitempty"`// 投稿者IDでフィルタ
	Limit    int    `json:"limit,omitempty"`    // 取得件数（デフォルト20）
	Offset   int    `json:"offset,omitempty"`   // オフセット
}

// SearchPostsResponse は投稿検索のレスポンスです。
type SearchPostsResponse struct {
	Posts      []Post `json:"posts"`       // 検索結果の投稿リスト
	TotalCount int    `json:"total_count"` // 総件数
	Limit      int    `json:"limit"`       // 取得件数
	Offset     int    `json:"offset"`      // オフセット
}

// SearchThreadsRequest はスレッド検索のリクエストです。
type SearchThreadsRequest struct {
	Query   string `json:"query"`              // 検索クエリ
	BoardID string `json:"board_id,omitempty"` // 掲示板IDでフィルタ
	Limit   int    `json:"limit,omitempty"`    // 取得件数（デフォルト20）
	Offset  int    `json:"offset,omitempty"`   // オフセット
}

// SearchThreadsResponse はスレッド検索のレスポンスです。
type SearchThreadsResponse struct {
	Threads    []Thread `json:"threads"`     // 検索結果のスレッドリスト
	TotalCount int      `json:"total_count"` // 総件数
	Limit      int      `json:"limit"`       // 取得件数
	Offset     int      `json:"offset"`      // オフセット
}
