package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ---#4 Ed25519 key handling ---

const (
	// keyPrefixEd25519 は鍵文字列の種類判別用プレフィックス。
	// 形式: ed25519:<base64(raw key bytes)> (パディングなし)
	keyPrefixEd25519 = "ed25519:"
)

var (
	// ErrInvalidKeyFormat は、鍵文字列の形式が不正なときに返す。
	ErrInvalidKeyFormat = errors.New("invalid key format")
	// ErrInvalidKeyType は将来の拡張用(今は未使用)。
	ErrInvalidKeyType = errors.New("invalid key type")
)

// KeyPair は JSON で扱いやすい(エンコード済み)公開鍵/秘密鍵ペア。
type KeyPair struct {
	Public  string `json:"pub"`
	Private string `json:"priv"`
}

// GenerateKeyPair は ed25519 鍵ペアを生成して、文字列(プレフィックス+base64)で返す。
func GenerateKeyPair() (KeyPair, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return KeyPair{}, err
	}
	return KeyPair{
		Public:  EncodeEd25519PublicKey(pub),
		Private: EncodeEd25519PrivateKey(priv),
	}, nil
}

// EncodeEd25519PublicKey は公開鍵バイト列をプレフィックス付き文字列にする。
func EncodeEd25519PublicKey(pub ed25519.PublicKey) string {
	return keyPrefixEd25519 + base64.RawStdEncoding.EncodeToString([]byte(pub))
}

// EncodeEd25519PrivateKey は秘密鍵バイト列をプレフィックス付き文字列にする。
func EncodeEd25519PrivateKey(priv ed25519.PrivateKey) string {
	return keyPrefixEd25519 + base64.RawStdEncoding.EncodeToString([]byte(priv))
}

// ParseEd25519PublicKey は文字列(プレフィックス+base64)から公開鍵を復元する。
func ParseEd25519PublicKey(s string) (ed25519.PublicKey, error) {
	if !strings.HasPrefix(s, keyPrefixEd25519) {
		return nil, fmt.Errorf("%w: expected prefix %q", ErrInvalidKeyFormat, keyPrefixEd25519)
	}
	raw, err := base64.RawStdEncoding.DecodeString(strings.TrimPrefix(s, keyPrefixEd25519))
	if err != nil {
		return nil, fmt.Errorf("%w: base64 decode: %v", ErrInvalidKeyFormat, err)
	}
	if l := len(raw); l != ed25519.PublicKeySize {
		return nil, fmt.Errorf("%w: public key length=%d", ErrInvalidKeyFormat, l)
	}
	return ed25519.PublicKey(raw), nil
}

// ParseEd25519PrivateKey は文字列(プレフィックス+base64)から秘密鍵を復元する。
func ParseEd25519PrivateKey(s string) (ed25519.PrivateKey, error) {
	if !strings.HasPrefix(s, keyPrefixEd25519) {
		return nil, fmt.Errorf("%w: expected prefix %q", ErrInvalidKeyFormat, keyPrefixEd25519)
	}
	raw, err := base64.RawStdEncoding.DecodeString(strings.TrimPrefix(s, keyPrefixEd25519))
	if err != nil {
		return nil, fmt.Errorf("%w: base64 decode: %v", ErrInvalidKeyFormat, err)
	}
	if l := len(raw); l != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("%w: private key length=%d", ErrInvalidKeyFormat, l)
	}
	return ed25519.PrivateKey(raw), nil
}

// SignPayloadEd25519 は payload を署名して base64(StdEncoding) の署名文字列を返す。
func SignPayloadEd25519(privKey string, payload string) (string, error) {
	priv, err := ParseEd25519PrivateKey(privKey)
	if err != nil {
		return "", err
	}
	sig := ed25519.Sign(priv, []byte(payload))
	return base64.StdEncoding.EncodeToString(sig), nil
}

// VerifyPayloadEd25519 は署名(base64)が payload と公開鍵に対して正しいか検証する。
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

// --- Deterministic sign payload builders (field-order strict) ---

// BuildSignPayload は、順序固定の "k=v" 行を '\n' で連結して署名ペイロードを作る。
// 呼び出し側がフィールド順序を必ず守ること。
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

// normOptionalString は nil を "" に正規化する。
func normOptionalString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// PostSignPayload builds the signable payload for Post as specified in docs.
// NOTE: attachments/meta/signature are intentionally excluded.
func PostSignPayload(
	version int,
	threadID string,
	parentPostCid *string,
	authorPubKey string,
	displayName string,
	bodyFormat string,
	bodyContent string,
	createdAt string,
) string {
	return BuildSignPayload([][2]string{
		{"type", "post"},
		{"version", fmt.Sprintf("%d", version)},
		{"threadId", threadID},
		{"parentPostCid", normOptionalString(parentPostCid)},
		{"authorPubKey", authorPubKey},
		{"displayName", displayName},
		{"body.format", bodyFormat},
		{"body.content", bodyContent},
		{"createdAt", createdAt},
	})
}

// BoardLogEntrySignPayload builds the signable payload for BoardLogEntry as specified in docs.
func BoardLogEntrySignPayload(
	version int,
	boardID string,
	op string,
	threadID string,
	postCid *string,
	oldPostCid *string,
	newPostCid *string,
	targetPostCid *string,
	reason *string,
	createdAt string,
	authorPubKey string,
	prevLogCid *string,
) string {
	return BuildSignPayload([][2]string{
		{"type", "boardLogEntry"},
		{"version", fmt.Sprintf("%d", version)},
		{"boardId", boardID},
		{"op", op},
		{"threadId", threadID},
		{"postCid", normOptionalString(postCid)},
		{"oldPostCid", normOptionalString(oldPostCid)},
		{"newPostCid", normOptionalString(newPostCid)},
		{"targetPostCid", normOptionalString(targetPostCid)},
		{"reason", normOptionalString(reason)},
		{"createdAt", createdAt},
		{"authorPubKey", authorPubKey},
		{"prevLogCid", normOptionalString(prevLogCid)},
	})
}

// --- Key file (simple storage format) ---

type StoredKey struct {
	Name string `json:"name"`
	Pub  string `json:"pub"`
	Priv string `json:"priv"`
}

// KeysFile は keys ファイルのトップレベル構造。
type KeysFile struct {
	Keys []StoredKey `json:"keys"`
}

// LoadKeysFile は JSON keys ファイルを読み込む。
func LoadKeysFile(path string) (KeysFile, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return KeysFile{}, err
	}
	var kf KeysFile
	if err := json.Unmarshal(b, &kf); err != nil {
		return KeysFile{}, err
	}
	return kf, nil
}

// SaveKeysFile は JSON keys ファイルを書き込む(一時ファイル→リネームで原子的に更新)。
func SaveKeysFile(path string, kf KeysFile, perm os.FileMode) error {
	b, err := json.MarshalIndent(kf, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, perm); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
