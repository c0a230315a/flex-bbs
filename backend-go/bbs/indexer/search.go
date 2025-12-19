package indexer

import (
	"context"
	"database/sql"
	"strings"
)

type SearchPostsParams struct {
	Query        string
	BoardID      string
	AuthorPubKey string
	Since        string
	Until        string
	Limit        int
	Offset       int
}

type SearchPostResult struct {
	PostCID      string  `json:"postCid"`
	ThreadID     string  `json:"threadId"`
	BoardID      string  `json:"boardId"`
	AuthorPubKey string  `json:"authorPubKey"`
	DisplayName  string  `json:"displayName"`
	BodyFormat   string  `json:"bodyFormat"`
	BodyContent  string  `json:"bodyContent"`
	CreatedAt    string  `json:"createdAt"`
	EditedAt     *string `json:"editedAt"`
}

func (i *Indexer) SearchPosts(ctx context.Context, p SearchPostsParams) ([]SearchPostResult, error) {
	if i.db == nil {
		return nil, ErrIndexerClosed
	}

	if p.Limit <= 0 {
		p.Limit = 50
	}
	if p.Limit > 200 {
		p.Limit = 200
	}
	if p.Offset < 0 {
		p.Offset = 0
	}

	var (
		where []string
		args  []any
	)
	where = append(where, "tp.tombstoned = 0")
	if p.BoardID != "" {
		where = append(where, "t.board_id = ?")
		args = append(args, p.BoardID)
	}
	if p.AuthorPubKey != "" {
		where = append(where, "p.author_pubkey = ?")
		args = append(args, p.AuthorPubKey)
	}
	if p.Since != "" {
		where = append(where, "p.created_at >= ?")
		args = append(args, p.Since)
	}
	if p.Until != "" {
		where = append(where, "p.created_at <= ?")
		args = append(args, p.Until)
	}
	if p.Query != "" {
		where = append(where, "p.body_content LIKE ?")
		args = append(args, "%"+p.Query+"%")
	}

	q := `
		SELECT
			p.post_cid,
			p.thread_id,
			t.board_id,
			p.author_pubkey,
			p.display_name,
			p.body_format,
			p.body_content,
			p.created_at,
			p.edited_at
		FROM posts p
		JOIN thread_posts tp ON tp.thread_id = p.thread_id AND tp.post_cid = p.post_cid
		JOIN threads t ON t.thread_id = p.thread_id
	`
	if len(where) > 0 {
		q += " WHERE " + strings.Join(where, " AND ")
	}
	q += " ORDER BY p.created_at DESC LIMIT ? OFFSET ?"
	args = append(args, p.Limit, p.Offset)

	rows, err := i.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []SearchPostResult
	for rows.Next() {
		var r SearchPostResult
		var edited sql.NullString
		if err := rows.Scan(
			&r.PostCID,
			&r.ThreadID,
			&r.BoardID,
			&r.AuthorPubKey,
			&r.DisplayName,
			&r.BodyFormat,
			&r.BodyContent,
			&r.CreatedAt,
			&edited,
		); err != nil {
			return nil, err
		}
		if edited.Valid {
			r.EditedAt = &edited.String
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
