package signature

import (
	"testing"

	"flex-bbs/backend-go/bbs/types"
)

func TestCanonicalPostPayload(t *testing.T) {
	p := &types.Post{
		Version:      types.Version1,
		Type:         types.TypePost,
		ThreadID:     "baf_thread",
		AuthorPubKey: "ed25519:pub",
		DisplayName:  "alice",
		Body: types.PostBody{
			Format:  "markdown",
			Content: "hello",
		},
		CreatedAt: "2025-11-28T08:30:00Z",
	}
	got := CanonicalPostPayload(p)
	want := "" +
		"type=post\n" +
		"version=1\n" +
		"threadId=baf_thread\n" +
		"parentPostCid=\n" +
		"authorPubKey=ed25519:pub\n" +
		"displayName=alice\n" +
		"body.format=markdown\n" +
		"body.content=hello\n" +
		"createdAt=2025-11-28T08:30:00Z"
	if got != want {
		t.Fatalf("payload mismatch\n--- got:\n%s\n--- want:\n%s", got, want)
	}
}

func TestCanonicalBoardLogEntryPayload(t *testing.T) {
	oldCid := "baf_old"
	newCid := "baf_new"
	prevCid := "baf_prev"
	e := &types.BoardLogEntry{
		Version:      types.Version1,
		Type:         types.TypeBoardLogEntry,
		BoardID:      "bbs.general",
		Op:           types.OpEditPost,
		ThreadID:     "baf_thread",
		OldPostCID:   &oldCid,
		NewPostCID:   &newCid,
		CreatedAt:    "2025-11-28T08:40:00Z",
		AuthorPubKey: "ed25519:pub",
		PrevLogCID:   &prevCid,
	}
	got := CanonicalBoardLogEntryPayload(e)
	want := "" +
		"type=boardLogEntry\n" +
		"version=1\n" +
		"boardId=bbs.general\n" +
		"op=editPost\n" +
		"threadId=baf_thread\n" +
		"postCid=\n" +
		"oldPostCid=baf_old\n" +
		"newPostCid=baf_new\n" +
		"targetPostCid=\n" +
		"reason=\n" +
		"createdAt=2025-11-28T08:40:00Z\n" +
		"authorPubKey=ed25519:pub\n" +
		"prevLogCid=baf_prev"
	if got != want {
		t.Fatalf("payload mismatch\n--- got:\n%s\n--- want:\n%s", got, want)
	}
}
