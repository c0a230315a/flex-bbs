package signature

import (
	"crypto/ed25519"
	"encoding/base64"
	"errors"
)

var (
	ErrInvalidSignature = errors.New("invalid signature")
)

func SignBase64(priv ed25519.PrivateKey, payload string) (string, error) {
	if len(priv) != ed25519.PrivateKeySize {
		return "", ErrInvalidKeyBytes
	}
	sig := ed25519.Sign(priv, []byte(payload))
	return base64.StdEncoding.EncodeToString(sig), nil
}

func VerifyBase64(pub ed25519.PublicKey, payload, signatureBase64 string) error {
	if len(pub) != ed25519.PublicKeySize {
		return ErrInvalidKeyBytes
	}
	sig, err := base64.StdEncoding.DecodeString(signatureBase64)
	if err != nil {
		return err
	}
	if !ed25519.Verify(pub, []byte(payload), sig) {
		return ErrInvalidSignature
	}
	return nil
}
