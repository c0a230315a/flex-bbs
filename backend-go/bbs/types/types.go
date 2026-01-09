package types

import "time"

const (
	Version1 = 1

	TypePost          = "post"
	TypeThreadMeta    = "threadMeta"
	TypeBoardMeta     = "boardMeta"
	TypeBoardLogEntry = "boardLogEntry"

	OpCreateThread  = "createThread"
	OpAddPost       = "addPost"
	OpEditPost      = "editPost"
	OpTombstonePost = "tombstonePost"
)

type PostBody struct {
	Format  string `json:"format"`
	Content string `json:"content"`
}

type Attachment struct {
	CID  string `json:"cid"`
	Mime string `json:"mime"`
}

type Post struct {
	Version       int            `json:"version"`
	Type          string         `json:"type"`
	PostCID       *string        `json:"postCid"`
	ThreadID      string         `json:"threadId"`
	ParentPostCID *string        `json:"parentPostCid"`
	AuthorPubKey  string         `json:"authorPubKey"`
	DisplayName   string         `json:"displayName"`
	Body          PostBody       `json:"body"`
	Attachments   []Attachment   `json:"attachments"`
	CreatedAt     string         `json:"createdAt"`
	EditedAt      *string        `json:"editedAt"`
	Meta          map[string]any `json:"meta"`
	Signature     string         `json:"signature"`
}

type ThreadMeta struct {
	Version     int            `json:"version"`
	Type        string         `json:"type"`
	ThreadID    string         `json:"threadId"`
	BoardID     string         `json:"boardId"`
	Title       string         `json:"title"`
	RootPostCID string         `json:"rootPostCid"`
	CreatedAt   string         `json:"createdAt"`
	CreatedBy   string         `json:"createdBy"`
	Meta        map[string]any `json:"meta"`
	Signature   string         `json:"signature"`
}

type BoardMeta struct {
	Version     int     `json:"version"`
	Type        string  `json:"type"`
	BoardID     string  `json:"boardId"`
	Title       string  `json:"title"`
	Description string  `json:"description"`
	LogHeadCID  *string `json:"logHeadCid"`
	CreatedAt   string  `json:"createdAt"`
	CreatedBy   string  `json:"createdBy"`
	Signature   string  `json:"signature"`
}

type BoardLogEntry struct {
	Version       int     `json:"version"`
	Type          string  `json:"type"`
	BoardID       string  `json:"boardId"`
	Op            string  `json:"op"`
	ThreadID      string  `json:"threadId"`
	PostCID       *string `json:"postCid"`
	OldPostCID    *string `json:"oldPostCid"`
	NewPostCID    *string `json:"newPostCid"`
	TargetPostCID *string `json:"targetPostCid"`
	Reason        *string `json:"reason"`
	CreatedAt     string  `json:"createdAt"`
	AuthorPubKey  string  `json:"authorPubKey"`
	PrevLogCID    *string `json:"prevLogCid"`
	Signature     string  `json:"signature"`
}

func NowUTC() string {
	return time.Now().UTC().Format(time.RFC3339)
}
