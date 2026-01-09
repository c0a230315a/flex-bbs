package log

import (
	"context"
	"testing"

	"flex-bbs/backend-go/bbs/signature"
	"flex-bbs/backend-go/bbs/types"
)

func TestReplayThread_EditAndTombstone(t *testing.T) {
	pubStr, privStr, err := signature.GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair: %v", err)
	}
	priv, err := signature.ParsePrivateKey(privStr)
	if err != nil {
		t.Fatalf("ParsePrivateKey: %v", err)
	}

	threadID := "baf_thread"
	boardID := "bbs.general"

	rootPostCID := "baf_root"
	postCID := "baf_post"
	editedCID := "baf_post_edited"

	pRoot := &types.Post{
		Version:      1,
		Type:         types.TypePost,
		PostCID:      nil,
		ThreadID:     threadID,
		AuthorPubKey: pubStr,
		DisplayName:  "alice",
		Body:         types.PostBody{Format: "markdown", Content: "root"},
		Attachments:  nil,
		CreatedAt:    "2025-01-01T00:00:00Z",
		EditedAt:     nil,
		Meta:         map[string]any{},
	}
	pRoot.Signature, _ = signature.SignBase64(priv, signature.CanonicalPostPayload(pRoot))

	p1 := &types.Post{
		Version:      1,
		Type:         types.TypePost,
		ThreadID:     threadID,
		AuthorPubKey: pubStr,
		DisplayName:  "alice",
		Body:         types.PostBody{Format: "markdown", Content: "hi"},
		CreatedAt:    "2025-01-01T00:01:00Z",
		Meta:         map[string]any{},
	}
	p1.Signature, _ = signature.SignBase64(priv, signature.CanonicalPostPayload(p1))

	editedAt := "2025-01-01T00:02:00Z"
	p1Edited := &types.Post{
		Version:      1,
		Type:         types.TypePost,
		ThreadID:     threadID,
		AuthorPubKey: pubStr,
		DisplayName:  "alice",
		Body:         types.PostBody{Format: "markdown", Content: "hi (edited)"},
		CreatedAt:    p1.CreatedAt,
		EditedAt:     &editedAt,
		Meta:         map[string]any{},
	}
	p1Edited.Signature, _ = signature.SignBase64(priv, signature.CanonicalPostPayload(p1Edited))

	log1CID := "baf_log1"
	log2CID := "baf_log2"
	log3CID := "baf_log3"
	log4CID := "baf_log4"

	log1 := &types.BoardLogEntry{
		Version:      1,
		Type:         types.TypeBoardLogEntry,
		BoardID:      boardID,
		Op:           types.OpCreateThread,
		ThreadID:     threadID,
		PostCID:      &rootPostCID,
		CreatedAt:    "2025-01-01T00:00:10Z",
		AuthorPubKey: pubStr,
		PrevLogCID:   nil,
	}
	log1.Signature, _ = signature.SignBase64(priv, signature.CanonicalBoardLogEntryPayload(log1))

	log2 := &types.BoardLogEntry{
		Version:      1,
		Type:         types.TypeBoardLogEntry,
		BoardID:      boardID,
		Op:           types.OpAddPost,
		ThreadID:     threadID,
		PostCID:      &postCID,
		CreatedAt:    "2025-01-01T00:01:10Z",
		AuthorPubKey: pubStr,
		PrevLogCID:   &log1CID,
	}
	log2.Signature, _ = signature.SignBase64(priv, signature.CanonicalBoardLogEntryPayload(log2))

	log3 := &types.BoardLogEntry{
		Version:      1,
		Type:         types.TypeBoardLogEntry,
		BoardID:      boardID,
		Op:           types.OpEditPost,
		ThreadID:     threadID,
		OldPostCID:   &postCID,
		NewPostCID:   &editedCID,
		CreatedAt:    "2025-01-01T00:02:10Z",
		AuthorPubKey: pubStr,
		PrevLogCID:   &log2CID,
	}
	log3.Signature, _ = signature.SignBase64(priv, signature.CanonicalBoardLogEntryPayload(log3))

	reason := "spam"
	log4 := &types.BoardLogEntry{
		Version:       1,
		Type:          types.TypeBoardLogEntry,
		BoardID:       boardID,
		Op:            types.OpTombstonePost,
		ThreadID:      threadID,
		TargetPostCID: &editedCID,
		Reason:        &reason,
		CreatedAt:     "2025-01-01T00:03:10Z",
		AuthorPubKey:  pubStr,
		PrevLogCID:    &log3CID,
	}
	log4.Signature, _ = signature.SignBase64(priv, signature.CanonicalBoardLogEntryPayload(log4))

	posts := map[string]*types.Post{
		rootPostCID: pRoot,
		postCID:     p1,
		editedCID:   p1Edited,
	}
	logs := map[string]*types.BoardLogEntry{
		log1CID: log1,
		log2CID: log2,
		log3CID: log3,
		log4CID: log4,
	}

	loadLog := func(ctx context.Context, cid string) (*types.BoardLogEntry, error) {
		return logs[cid], nil
	}
	loadPost := func(ctx context.Context, cid string) (*types.Post, error) {
		return posts[cid], nil
	}

	chain, err := FetchChain(context.Background(), &log4CID, loadLog, func(e *types.BoardLogEntry) *string {
		return e.PrevLogCID
	}, VerifyBoardLogEntry, 100)
	if err != nil {
		t.Fatalf("FetchChain: %v", err)
	}

	out, err := ReplayThread(context.Background(), chain, threadID, loadPost, VerifyPost, nil)
	if err != nil {
		t.Fatalf("ReplayThread: %v", err)
	}
	if len(out) != 2 {
		t.Fatalf("post count: %d", len(out))
	}
	if out[0].CID != rootPostCID || out[0].Post.Body.Content != "root" || out[0].Tombstoned {
		t.Fatalf("root mismatch: %#v", out[0])
	}
	if out[1].CID != editedCID || out[1].Post.Body.Content != "hi (edited)" || !out[1].Tombstoned {
		t.Fatalf("edited mismatch: %#v", out[1])
	}
	if out[1].TombstoneReason == nil || *out[1].TombstoneReason != "spam" {
		t.Fatalf("tombstone reason mismatch: %#v", out[1].TombstoneReason)
	}
}
