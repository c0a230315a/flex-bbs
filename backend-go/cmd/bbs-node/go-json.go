package model


import (
	"crypto/ed25519"
	"encoding/base64"
	"testing"
	"time"
)


func mustTime(t *testing.T, s string) string {
	t.Helper()
	if _, err := time.Parse(time.RFC3339, s); err != nil {
		t.Fatalf("invalid time: %v", err)
	}
	return s
}


func TestPostSignaturePayloadExact(t *testing.T) {
	p := &Post{
	Version: 1,
	Type: "post",
	ThreadID: "baf-thread",
	AuthorPubKey: "ed25519:pubkey",
	DisplayName: "tester",
	Body: PostBody{
		Format: "markdown",
		Content: "hello",
	},
	CreatedAt: mustTime(t, "2025-11-28T08:30:00Z"),
	}


	expected := "" +
		"type=post\n" +
		"version=1\n" +
		"threadId=baf-thread\n" +
		"parentPostCid=\n" +
		"authorPubKey=ed25519:pubkey\n" +
		"displayName=tester\n" +
		"body.format=markdown\n" +
		"body.content=hello\n" +
		"createdAt=2025-11-28T08:30:00Z"


	if got := p.SignaturePayload(); got != expected {
		t.Fatalf("payload mismatch\n--- expected ---\n%s\n--- got ---\n%s", expected, got)
	}
}


func TestPostSignAndVerify(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(nil)


	p := &Post{
		Version: 1,
		Type: "post",
		ThreadID: "baf-thread",
		AuthorPubKey: "ed25519:test",
		DisplayName: "tester",
		Body: PostBody{Format: "markdown", Content: "hello"},
		CreatedAt: mustTime(t, "2025-11-28T08:30:00Z"),
	}


	payload := p.SignaturePayload()
	sig := Sign(priv, payload)


	if !Verify(pub, payload, sig) {
		t.Fatal("signature verification failed")
	}
}


func TestBoardLogEntrySignaturePayloadExact(t *testing.T) {
	postCid := "baf-post"


	e := &BoardLogEntry{
	Version: 1,
	Type: "boardLogEntry",
	BoardID: "bbs.general",
	Op: "addPost",
	ThreadID: "baf-thread",
	PostCID: &postCid,
	CreatedAt: mustTime(t, "2025-11-28T08:40:00Z"),
	AuthorPubKey: "ed25519:pub",
	}


	expected := "" +
		"type=boardLogEntry\n" +
		"version=1\n" +
		"boardId=bbs.general\n" +
		"op=addPost\n" +
		"threadId=baf-thread\n" +
		"postCid=baf-post\n" +
		"oldPostCid=\n" +
		"newPostCid=\n" +
		"targetPostCid=\n" +
		"reason=\n" +
		"createdAt=2025-11-28T08:40:00Z\n" +
		"authorPubKey=ed25519:pub\n" +
		"prevLogCid="


	if got := e.SignaturePayload(); got != expected {
		t.Fatalf("payload mismatch\n--- expected ---\n%s\n--- got ---\n%s", expected, got)
	}
}


func TestJSONRoundTrip(t *testing.T) {
	p := &Post{
		Version: 1,
		Type: "post",
		ThreadID: "baf-thread",
		AuthorPubKey: "ed25519:pub",
		DisplayName: "tester",
		Body: PostBody{Format: "markdown", Content: "hello"},
		CreatedAt: mustTime(t, "2025-11-28T08:30:00Z"),
		Signature: base64.StdEncoding.EncodeToString([]byte("sig")),
	}


	b, err := ToJSON(p)
	if err != nil {
		t.Fatal(err)
	}


	var out Post
	if err := FromJSON(b, &out); err != nil {
		t.Fatal(err)
	}


	if out.Body.Content != "hello" || out.Type != "post" {
		t.Fatal("round trip failed")
	}
}