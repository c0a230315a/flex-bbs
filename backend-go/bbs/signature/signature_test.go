package signature

import (
	"testing"
)

func TestSignVerifyRoundtrip(t *testing.T) {
	pubStr, privStr, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair: %v", err)
	}

	priv, err := ParsePrivateKey(privStr)
	if err != nil {
		t.Fatalf("ParsePrivateKey: %v", err)
	}
	pub, err := ParsePublicKey(pubStr)
	if err != nil {
		t.Fatalf("ParsePublicKey: %v", err)
	}

	payload := "type=post\nversion=1\nthreadId=x\nparentPostCid=\nauthorPubKey=y\ndisplayName=z\nbody.format=markdown\nbody.content=hi\ncreatedAt=2025-01-01T00:00:00Z"
	sig, err := SignBase64(priv, payload)
	if err != nil {
		t.Fatalf("SignBase64: %v", err)
	}
	if err := VerifyBase64(pub, payload, sig); err != nil {
		t.Fatalf("VerifyBase64: %v", err)
	}
}
