package log

import (
	"testing"

	"flex-bbs/backend-go/bbs/signature"
	"flex-bbs/backend-go/bbs/types"
)

func TestVerifyBoardMeta_RecoversUTF8FromLatin1(t *testing.T) {
	t.Parallel()

	_, priv, err := signature.GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair: %v", err)
	}

	origTitle := "日本語のタイトル"
	origDescription := "日本語の説明"
	bm := &types.BoardMeta{
		BoardID:     "test",
		Title:       origTitle,
		Description: origDescription,
		CreatedAt:   "2025-01-01T00:00:00Z",
	}
	if err := signature.SignBoardMeta(priv, bm); err != nil {
		t.Fatalf("SignBoardMeta: %v", err)
	}

	bm.Title = corruptUTF8ToLatin1(bm.Title)
	bm.Description = corruptUTF8ToLatin1(bm.Description)

	if !VerifyBoardMeta(bm) {
		t.Fatalf("VerifyBoardMeta failed")
	}
	if bm.Title != origTitle {
		t.Fatalf("title mismatch: got %q want %q", bm.Title, origTitle)
	}
	if bm.Description != origDescription {
		t.Fatalf("description mismatch: got %q want %q", bm.Description, origDescription)
	}
}

func TestVerifyThreadMeta_RecoversUTF8FromLatin1(t *testing.T) {
	t.Parallel()

	_, priv, err := signature.GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair: %v", err)
	}

	origTitle := "日本語スレッド"
	tm := &types.ThreadMeta{
		BoardID:   "test",
		Title:     origTitle,
		CreatedAt: "2025-01-01T00:00:00Z",
	}
	if err := signature.SignThreadMeta(priv, tm); err != nil {
		t.Fatalf("SignThreadMeta: %v", err)
	}

	tm.Title = corruptUTF8ToLatin1(tm.Title)

	if !VerifyThreadMeta(tm) {
		t.Fatalf("VerifyThreadMeta failed")
	}
	if tm.Title != origTitle {
		t.Fatalf("title mismatch: got %q want %q", tm.Title, origTitle)
	}
}

func TestVerifyPost_RecoversUTF8FromLatin1(t *testing.T) {
	t.Parallel()

	_, priv, err := signature.GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair: %v", err)
	}

	origName := "なまえ"
	origBody := "本文"
	p := &types.Post{
		ThreadID:    "baf_thread",
		DisplayName: origName,
		Body: types.PostBody{
			Format:  "markdown",
			Content: origBody,
		},
		CreatedAt: "2025-01-01T00:00:00Z",
	}
	if err := signature.SignPost(priv, p); err != nil {
		t.Fatalf("SignPost: %v", err)
	}

	p.DisplayName = corruptUTF8ToLatin1(p.DisplayName)
	p.Body.Content = corruptUTF8ToLatin1(p.Body.Content)

	if !VerifyPost(p) {
		t.Fatalf("VerifyPost failed")
	}
	if p.DisplayName != origName {
		t.Fatalf("display name mismatch: got %q want %q", p.DisplayName, origName)
	}
	if p.Body.Content != origBody {
		t.Fatalf("body mismatch: got %q want %q", p.Body.Content, origBody)
	}
}

func TestVerifyBoardLogEntry_RecoversUTF8FromLatin1(t *testing.T) {
	t.Parallel()

	_, priv, err := signature.GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair: %v", err)
	}

	reason := "理由"
	e := &types.BoardLogEntry{
		BoardID:    "test",
		Op:         types.OpTombstonePost,
		ThreadID:   "baf_thread",
		Reason:     &reason,
		CreatedAt:  "2025-01-01T00:00:00Z",
		PrevLogCID: nil,
	}
	if err := signature.SignBoardLogEntry(priv, e); err != nil {
		t.Fatalf("SignBoardLogEntry: %v", err)
	}

	corrupted := corruptUTF8ToLatin1(*e.Reason)
	e.Reason = &corrupted

	if !VerifyBoardLogEntry(e) {
		t.Fatalf("VerifyBoardLogEntry failed")
	}
	if e.Reason == nil || *e.Reason != reason {
		if e.Reason == nil {
			t.Fatalf("reason missing after verify")
		}
		t.Fatalf("reason mismatch: got %q want %q", *e.Reason, reason)
	}
}

func corruptUTF8ToLatin1(s string) string {
	b := []byte(s)
	runes := make([]rune, len(b))
	for i, x := range b {
		runes[i] = rune(x)
	}
	return string(runes)
}
