package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBuildSignPayload_OrderAndFormat(t *testing.T) {
	// 署名ペイロードの基本フォーマット(行区切り/順序)が固定であること。
	p := BuildSignPayload([][2]string{{"a", "1"}, {"b", "2"}, {"c", "3"}})
	want := "a=1\nb=2\nc=3"
	if p != want {
		t.Fatalf("unexpected payload\nwant=%q\n got=%q", want, p)
	}
}

func TestSignVerify_OK(t *testing.T) {
	// 正しい鍵・正しい payload・正しい署名なら verify が true になること。
	kp, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair: %v", err)
	}

	payload := PostSignPayload(
		1,
		"thread-1",
		nil,
		kp.Public,
		"alice",
		"plain",
		"hello",
		"2025-12-19T00:00:00Z",
	)

	sig, err := SignPayloadEd25519(kp.Private, payload)
	if err != nil {
		t.Fatalf("SignPayloadEd25519: %v", err)
	}

	ok, err := VerifyPayloadEd25519(kp.Public, payload, sig)
	if err != nil {
		t.Fatalf("VerifyPayloadEd25519: %v", err)
	}
	if !ok {
		t.Fatalf("expected verification ok")
	}
}

func TestSignVerify_NG_ModifiedPayload(t *testing.T) {
	// payload を改ざんしたら verify が false になること。
	kp, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair: %v", err)
	}

	payload := BoardLogEntrySignPayload(
		1,
		"board-1",
		"createThread",
		"thread-1",
		nil,
		nil,
		nil,
		nil,
		nil,
		"2025-12-19T00:00:00Z",
		kp.Public,
		nil,
	)

	sig, err := SignPayloadEd25519(kp.Private, payload)
	if err != nil {
		t.Fatalf("SignPayloadEd25519: %v", err)
	}

	modified := payload + "\nextra=1"
	ok, err := VerifyPayloadEd25519(kp.Public, modified, sig)
	if err != nil {
		t.Fatalf("VerifyPayloadEd25519: %v", err)
	}
	if ok {
		t.Fatalf("expected verification NG for modified payload")
	}
}

func TestSignVerify_NG_WrongPubKey(t *testing.T) {
	// 別の公開鍵で検証したら verify が false になること。
	kp1, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair #1: %v", err)
	}
	kp2, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair #2: %v", err)
	}

	payload := "type=test\nversion=1"
	sig, err := SignPayloadEd25519(kp1.Private, payload)
	if err != nil {
		t.Fatalf("SignPayloadEd25519: %v", err)
	}

	ok, err := VerifyPayloadEd25519(kp2.Public, payload, sig)
	if err != nil {
		t.Fatalf("VerifyPayloadEd25519: %v", err)
	}
	if ok {
		t.Fatalf("expected verification NG for wrong pub key")
	}
}

func TestKeysFile_SaveLoad(t *testing.T) {
	// 保存/読込が壊れていないこと(かつ、指定したパーミッションで作られること)。
	dir := t.TempDir()
	path := filepath.Join(dir, "keys.json")

	kp, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair: %v", err)
	}

	in := KeysFile{Keys: []StoredKey{{Name: "default", Pub: kp.Public, Priv: kp.Private}}}
	if err := SaveKeysFile(path, in, 0o600); err != nil {
		t.Fatalf("SaveKeysFile: %v", err)
	}

	st, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if got := st.Mode().Perm(); got != 0o600 {
		t.Fatalf("unexpected perm: got=%#o want=%#o", got, 0o600)
	}

	out, err := LoadKeysFile(path)
	if err != nil {
		t.Fatalf("LoadKeysFile: %v", err)
	}
	if len(out.Keys) != 1 {
		t.Fatalf("unexpected keys length: %d", len(out.Keys))
	}
	if out.Keys[0].Name != "default" || out.Keys[0].Pub != kp.Public || out.Keys[0].Priv != kp.Private {
		t.Fatalf("loaded keys mismatch")
	}
}
