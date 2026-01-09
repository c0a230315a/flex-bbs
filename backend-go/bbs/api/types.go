package api

import "flex-bbs/backend-go/bbs/types"

type BoardItem struct {
	BoardMetaCID string          `json:"boardMetaCid"`
	Board        types.BoardMeta `json:"board"`
}

type ThreadItem struct {
	ThreadID      string           `json:"threadId"`
	ThreadMetaCID string           `json:"threadMetaCid"`
	Thread        types.ThreadMeta `json:"thread"`
}

type ThreadPostItem struct {
	CID             string     `json:"cid"`
	Post            types.Post `json:"post"`
	Tombstoned      bool       `json:"tombstoned"`
	TombstoneReason *string    `json:"tombstoneReason"`
}

type ThreadResponse struct {
	ThreadMetaCID string           `json:"threadMetaCid"`
	ThreadMeta    types.ThreadMeta `json:"threadMeta"`
	Posts         []ThreadPostItem `json:"posts"`
}

type CreateThreadRequest struct {
	BoardID       string             `json:"boardId"`
	Title         string             `json:"title"`
	DisplayName   string             `json:"displayName"`
	Body          types.PostBody     `json:"body"`
	Attachments   []types.Attachment `json:"attachments"`
	ThreadMeta    map[string]any     `json:"threadMeta"`
	PostMeta      map[string]any     `json:"postMeta"`
	AuthorPrivKey string             `json:"authorPrivKey"`
}

type CreateThreadResponse struct {
	ThreadID     string           `json:"threadId"`
	RootPostCID  string           `json:"rootPostCid"`
	BoardLogCID  string           `json:"boardLogCid"`
	BoardMetaCID string           `json:"boardMetaCid"`
	ThreadMeta   types.ThreadMeta `json:"threadMeta"`
}

type AddPostRequest struct {
	ThreadID      string             `json:"threadId"`
	ParentPostCID *string            `json:"parentPostCid"`
	DisplayName   string             `json:"displayName"`
	Body          types.PostBody     `json:"body"`
	Attachments   []types.Attachment `json:"attachments"`
	Meta          map[string]any     `json:"meta"`
	AuthorPrivKey string             `json:"authorPrivKey"`
}

type AddPostResponse struct {
	PostCID      string `json:"postCid"`
	BoardLogCID  string `json:"boardLogCid"`
	BoardMetaCID string `json:"boardMetaCid"`
}

type EditPostRequest struct {
	Body          types.PostBody `json:"body"`
	DisplayName   *string        `json:"displayName"`
	AuthorPrivKey string         `json:"authorPrivKey"`
}

type EditPostResponse struct {
	OldPostCID   string `json:"oldPostCid"`
	NewPostCID   string `json:"newPostCid"`
	BoardLogCID  string `json:"boardLogCid"`
	BoardMetaCID string `json:"boardMetaCid"`
}

type TombstonePostRequest struct {
	Reason        *string `json:"reason"`
	AuthorPrivKey string  `json:"authorPrivKey"`
}

type TombstonePostResponse struct {
	TargetPostCID string `json:"targetPostCid"`
	BoardLogCID   string `json:"boardLogCid"`
	BoardMetaCID  string `json:"boardMetaCid"`
}

type AnnounceBoardRequest struct {
	BoardMetaCID string `json:"boardMetaCid"`
}

type AnnounceBoardResponse struct {
	BoardID       string `json:"boardId"`
	BoardMetaCID  string `json:"boardMetaCid"`
	Accepted      bool   `json:"accepted"`
	IgnoredReason string `json:"ignoredReason,omitempty"`
	Forwarded     int    `json:"forwarded"`
}
