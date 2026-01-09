package signature

import (
	"crypto/ed25519"

	"flex-bbs/backend-go/bbs/types"
)

func SignPost(privKeyString string, p *types.Post) error {
	priv, pubStr, err := parsePrivAndPub(privKeyString)
	if err != nil {
		return err
	}
	p.Version = types.Version1
	p.Type = types.TypePost
	p.AuthorPubKey = pubStr

	sig, err := SignBase64(priv, CanonicalPostPayload(p))
	if err != nil {
		return err
	}
	p.Signature = sig
	return nil
}

func SignThreadMeta(privKeyString string, m *types.ThreadMeta) error {
	priv, pubStr, err := parsePrivAndPub(privKeyString)
	if err != nil {
		return err
	}
	m.Version = types.Version1
	m.Type = types.TypeThreadMeta
	m.CreatedBy = pubStr

	sig, err := SignBase64(priv, CanonicalThreadMetaPayload(m))
	if err != nil {
		return err
	}
	m.Signature = sig
	return nil
}

func SignBoardMeta(privKeyString string, m *types.BoardMeta) error {
	priv, pubStr, err := parsePrivAndPub(privKeyString)
	if err != nil {
		return err
	}
	m.Version = types.Version1
	m.Type = types.TypeBoardMeta
	m.CreatedBy = pubStr

	sig, err := SignBase64(priv, CanonicalBoardMetaPayload(m))
	if err != nil {
		return err
	}
	m.Signature = sig
	return nil
}

func SignBoardLogEntry(privKeyString string, e *types.BoardLogEntry) error {
	priv, pubStr, err := parsePrivAndPub(privKeyString)
	if err != nil {
		return err
	}
	e.Version = types.Version1
	e.Type = types.TypeBoardLogEntry
	e.AuthorPubKey = pubStr

	sig, err := SignBase64(priv, CanonicalBoardLogEntryPayload(e))
	if err != nil {
		return err
	}
	e.Signature = sig
	return nil
}

func parsePrivAndPub(privKeyString string) (ed25519.PrivateKey, string, error) {
	priv, err := ParsePrivateKey(privKeyString)
	if err != nil {
		return nil, "", err
	}
	pub, err := PublicKeyFromPrivate(priv)
	if err != nil {
		return nil, "", err
	}
	return priv, PublicKeyString(pub), nil
}
