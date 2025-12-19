package signature

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strings"
)

var (
	ErrInvalidKeyFormat = errors.New("invalid key format")
	ErrInvalidKeyBytes  = errors.New("invalid key bytes")
)

func GenerateKeyPair() (pubKeyString, privKeyString string, err error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return "", "", err
	}
	return PublicKeyString(pub), PrivateKeyString(priv), nil
}

func PublicKeyString(pub ed25519.PublicKey) string {
	return "ed25519:" + base64.RawStdEncoding.EncodeToString(pub)
}

func PrivateKeyString(priv ed25519.PrivateKey) string {
	return "ed25519:" + base64.RawStdEncoding.EncodeToString(priv)
}

func ParsePublicKey(s string) (ed25519.PublicKey, error) {
	b, err := decodeEd25519KeyString(s)
	if err != nil {
		return nil, err
	}
	if len(b) != ed25519.PublicKeySize {
		return nil, ErrInvalidKeyBytes
	}
	return ed25519.PublicKey(b), nil
}

func ParsePrivateKey(s string) (ed25519.PrivateKey, error) {
	b, err := decodeEd25519KeyString(s)
	if err != nil {
		return nil, err
	}
	switch len(b) {
	case ed25519.PrivateKeySize:
		return ed25519.PrivateKey(b), nil
	case ed25519.SeedSize:
		return ed25519.NewKeyFromSeed(b), nil
	default:
		return nil, ErrInvalidKeyBytes
	}
}

func PublicKeyFromPrivate(priv ed25519.PrivateKey) (ed25519.PublicKey, error) {
	if len(priv) != ed25519.PrivateKeySize {
		return nil, ErrInvalidKeyBytes
	}
	pub := priv.Public()
	pubKey, ok := pub.(ed25519.PublicKey)
	if !ok {
		return nil, ErrInvalidKeyBytes
	}
	return pubKey, nil
}

func decodeEd25519KeyString(s string) ([]byte, error) {
	if !strings.HasPrefix(s, "ed25519:") {
		return nil, ErrInvalidKeyFormat
	}
	enc := strings.TrimPrefix(s, "ed25519:")
	if enc == "" {
		return nil, ErrInvalidKeyFormat
	}

	// Prefer hex if it looks like hex.
	if looksHex(enc) && len(enc)%2 == 0 {
		if b, err := hex.DecodeString(enc); err == nil {
			return b, nil
		}
	}

	if b, err := base64.StdEncoding.DecodeString(enc); err == nil {
		return b, nil
	}
	return base64.RawStdEncoding.DecodeString(enc)
}

func looksHex(s string) bool {
	for _, r := range s {
		switch {
		case '0' <= r && r <= '9':
		case 'a' <= r && r <= 'f':
		case 'A' <= r && r <= 'F':
		default:
			return false
		}
	}
	return true
}
