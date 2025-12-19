package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"strings"
)

const ed25519KeyPrefix = "ed25519:"

type KeyPair struct {
	Public  string
	Private string
}

func GenerateKeyPair() (KeyPair, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return KeyPair{}, err
	}
	return KeyPair{Public: encodeEd25519Key(pub), Private: encodeEd25519Key(priv)}, nil
}

func SignPayloadEd25519(privateKey string, payload string) (string, error) {
	priv, err := parseEd25519PrivateKey(privateKey)
	if err != nil {
		return "", err
	}
	sig := ed25519.Sign(priv, []byte(payload))
	return base64.StdEncoding.EncodeToString(sig), nil
}

func VerifyPayloadEd25519(publicKey string, payload string, signatureBase64 string) (bool, error) {
	pub, err := parseEd25519PublicKey(publicKey)
	if err != nil {
		return false, err
	}
	sig, err := base64.StdEncoding.DecodeString(signatureBase64)
	if err != nil {
		return false, err
	}
	return ed25519.Verify(pub, []byte(payload), sig), nil
}

func BuildSignPayload(kv [][2]string) string {
	lines := make([]string, 0, len(kv))
	for _, pair := range kv {
		lines = append(lines, pair[0]+"="+pair[1])
	}
	return strings.Join(lines, "\n")
}

func encodeEd25519Key(key []byte) string {
	return ed25519KeyPrefix + base64.RawStdEncoding.EncodeToString(key)
}

func parseEd25519PublicKey(s string) (ed25519.PublicKey, error) {
	b, err := decodeEd25519Key(s)
	if err != nil {
		return nil, err
	}
	if len(b) != ed25519.PublicKeySize {
		return nil, errors.New("invalid ed25519 public key length")
	}
	return ed25519.PublicKey(b), nil
}

func parseEd25519PrivateKey(s string) (ed25519.PrivateKey, error) {
	b, err := decodeEd25519Key(s)
	if err != nil {
		return nil, err
	}
	switch len(b) {
	case ed25519.PrivateKeySize:
		return ed25519.PrivateKey(b), nil
	case ed25519.SeedSize:
		return ed25519.NewKeyFromSeed(b), nil
	default:
		return nil, errors.New("invalid ed25519 private key length")
	}
}

func decodeEd25519Key(s string) ([]byte, error) {
	if !strings.HasPrefix(s, ed25519KeyPrefix) {
		return nil, errors.New("unsupported key type")
	}
	b64 := strings.TrimPrefix(s, ed25519KeyPrefix)
	if b64 == "" {
		return nil, errors.New("empty key")
	}
	b, err := base64.RawStdEncoding.DecodeString(b64)
	if err != nil {
		return nil, err
	}
	return b, nil
}
