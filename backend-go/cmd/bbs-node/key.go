package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
)

// --- Ed25519 key handling + deterministic sign payload builders ---

const (
	// keyPrefixEd25519 is used to identify encoded Ed25519 keys.
	// Format: ed25519:<base64(raw key bytes)> (RawStdEncoding, no padding)
	keyPrefixEd25519 = "ed25519:"
)

var (
	ErrInvalidKeyFormat = errors.New("invalid key format")
)

// KeyPair is a JSON-friendly encoded public/private key pair.
// Public/Private are prefix+base64 strings.
type KeyPair struct {
	Public  string `json:"pub"`
	Private string `json:"priv"`
}

func GenerateKeyPair() (KeyPair, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return KeyPair{}, err
	}
	return KeyPair{Public: EncodeEd25519PublicKey(pub), Private: EncodeEd25519PrivateKey(priv)}, nil
}

func EncodeEd25519PublicKey(pub ed25519.PublicKey) string {
	return keyPrefixEd25519 + base64.RawStdEncoding.EncodeToString([]byte(pub))
}

func EncodeEd25519PrivateKey(priv ed25519.PrivateKey) string {
	return keyPrefixEd25519 + base64.RawStdEncoding.EncodeToString([]byte(priv))
}

func ParseEd25519PublicKey(s string) (ed25519.PublicKey, error) {
	if !strings.HasPrefix(s, keyPrefixEd25519) {
		return nil, fmt.Errorf("%w: expected prefix %q", ErrInvalidKeyFormat, keyPrefixEd25519)
	}
	raw, err := base64.RawStdEncoding.DecodeString(strings.TrimPrefix(s, keyPrefixEd25519))
	if err != nil {
		return nil, fmt.Errorf("%w: base64 decode: %v", ErrInvalidKeyFormat, err)
	}
	if len(raw) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("%w: public key length=%d", ErrInvalidKeyFormat, len(raw))
	}
	return ed25519.PublicKey(raw), nil
}

func ParseEd25519PrivateKey(s string) (ed25519.PrivateKey, error) {
	if !strings.HasPrefix(s, keyPrefixEd25519) {
		return nil, fmt.Errorf("%w: expected prefix %q", ErrInvalidKeyFormat, keyPrefixEd25519)
	}
	raw, err := base64.RawStdEncoding.DecodeString(strings.TrimPrefix(s, keyPrefixEd25519))
	if err != nil {
		return nil, fmt.Errorf("%w: base64 decode: %v", ErrInvalidKeyFormat, err)
	}
	if len(raw) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("%w: private key length=%d", ErrInvalidKeyFormat, len(raw))
	}
	return ed25519.PrivateKey(raw), nil
}

// SignPayloadEd25519 signs payload and returns base64.StdEncoding signature.
func SignPayloadEd25519(privKey string, payload string) (string, error) {
	priv, err := ParseEd25519PrivateKey(privKey)
	if err != nil {
		return "", err
	}
	sig := ed25519.Sign(priv, []byte(payload))
	return base64.StdEncoding.EncodeToString(sig), nil
}

// VerifyPayloadEd25519 verifies signature (base64.StdEncoding) against payload and public key.
func VerifyPayloadEd25519(pubKey string, payload string, signatureBase64 string) (bool, error) {
	pub, err := ParseEd25519PublicKey(pubKey)
	if err != nil {
		return false, err
	}
	sig, err := base64.StdEncoding.DecodeString(signatureBase64)
	if err != nil {
		return false, fmt.Errorf("invalid signature base64: %w", err)
	}
	if len(sig) != ed25519.SignatureSize {
		return false, fmt.Errorf("invalid signature length=%d", len(sig))
	}
	return ed25519.Verify(pub, []byte(payload), sig), nil
}

// BuildSignPayload builds a deterministic sign payload by joining ordered "k=v" lines with '\n'.
func BuildSignPayload(orderedKV [][2]string) string {
	var b strings.Builder
	for i, kv := range orderedKV {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(kv[0])
		b.WriteByte('=')
		b.WriteString(kv[1])
	}
	return b.String()
}
