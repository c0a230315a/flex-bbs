package log

import (
	"flex-bbs/backend-go/bbs/signature"
	"flex-bbs/backend-go/bbs/types"
)

func VerifyPost(p *types.Post) bool {
	pub, err := signature.ParsePublicKey(p.AuthorPubKey)
	if err != nil {
		return false
	}
	payload := signature.CanonicalPostPayload(p)
	return signature.VerifyBase64(pub, payload, p.Signature) == nil
}

func VerifyBoardLogEntry(e *types.BoardLogEntry) bool {
	pub, err := signature.ParsePublicKey(e.AuthorPubKey)
	if err != nil {
		return false
	}
	payload := signature.CanonicalBoardLogEntryPayload(e)
	return signature.VerifyBase64(pub, payload, e.Signature) == nil
}

func VerifyThreadMeta(m *types.ThreadMeta) bool {
	pub, err := signature.ParsePublicKey(m.CreatedBy)
	if err != nil {
		return false
	}
	payload := signature.CanonicalThreadMetaPayload(m)
	return signature.VerifyBase64(pub, payload, m.Signature) == nil
}

func VerifyBoardMeta(m *types.BoardMeta) bool {
	pub, err := signature.ParsePublicKey(m.CreatedBy)
	if err != nil {
		return false
	}
	payload := signature.CanonicalBoardMetaPayload(m)
	return signature.VerifyBase64(pub, payload, m.Signature) == nil
}
