package log

import (
	"flex-bbs/backend-go/bbs/signature"
	"flex-bbs/backend-go/bbs/types"
)

func VerifyPost(p *types.Post) bool {
	pub, err := signature.ParsePublicKey(p.AuthorPubKey)
	if err != nil {
		if recovered, ok := recoverUTF8FromLatin1(p.AuthorPubKey); ok {
			p.AuthorPubKey = recovered
			pub, err = signature.ParsePublicKey(p.AuthorPubKey)
		}
		if err != nil {
			return false
		}
	}
	payload := signature.CanonicalPostPayload(p)
	if signature.VerifyBase64(pub, payload, p.Signature) == nil {
		return true
	}

	fixed := *p
	changed := false
	if recovered, ok := recoverUTF8FromLatin1(fixed.ThreadID); ok {
		fixed.ThreadID = recovered
		changed = true
	}
	if fixed.ParentPostCID != nil {
		if recovered, ok := recoverUTF8FromLatin1(*fixed.ParentPostCID); ok {
			v := recovered
			fixed.ParentPostCID = &v
			changed = true
		}
	}
	if recovered, ok := recoverUTF8FromLatin1(fixed.DisplayName); ok {
		fixed.DisplayName = recovered
		changed = true
	}
	if recovered, ok := recoverUTF8FromLatin1(fixed.Body.Format); ok {
		fixed.Body.Format = recovered
		changed = true
	}
	if recovered, ok := recoverUTF8FromLatin1(fixed.Body.Content); ok {
		fixed.Body.Content = recovered
		changed = true
	}
	if fixed.EditedAt != nil {
		if recovered, ok := recoverUTF8FromLatin1(*fixed.EditedAt); ok {
			v := recovered
			fixed.EditedAt = &v
			changed = true
		}
	}
	if !changed {
		return false
	}

	payload = signature.CanonicalPostPayload(&fixed)
	if signature.VerifyBase64(pub, payload, fixed.Signature) != nil {
		return false
	}
	*p = fixed
	return true
}

func VerifyBoardLogEntry(e *types.BoardLogEntry) bool {
	pub, err := signature.ParsePublicKey(e.AuthorPubKey)
	if err != nil {
		if recovered, ok := recoverUTF8FromLatin1(e.AuthorPubKey); ok {
			e.AuthorPubKey = recovered
			pub, err = signature.ParsePublicKey(e.AuthorPubKey)
		}
		if err != nil {
			return false
		}
	}
	payload := signature.CanonicalBoardLogEntryPayload(e)
	if signature.VerifyBase64(pub, payload, e.Signature) == nil {
		return true
	}

	fixed := *e
	changed := false
	if recovered, ok := recoverUTF8FromLatin1(fixed.BoardID); ok {
		fixed.BoardID = recovered
		changed = true
	}
	if recovered, ok := recoverUTF8FromLatin1(fixed.Op); ok {
		fixed.Op = recovered
		changed = true
	}
	if recovered, ok := recoverUTF8FromLatin1(fixed.ThreadID); ok {
		fixed.ThreadID = recovered
		changed = true
	}
	if fixed.PostCID != nil {
		if recovered, ok := recoverUTF8FromLatin1(*fixed.PostCID); ok {
			v := recovered
			fixed.PostCID = &v
			changed = true
		}
	}
	if fixed.OldPostCID != nil {
		if recovered, ok := recoverUTF8FromLatin1(*fixed.OldPostCID); ok {
			v := recovered
			fixed.OldPostCID = &v
			changed = true
		}
	}
	if fixed.NewPostCID != nil {
		if recovered, ok := recoverUTF8FromLatin1(*fixed.NewPostCID); ok {
			v := recovered
			fixed.NewPostCID = &v
			changed = true
		}
	}
	if fixed.TargetPostCID != nil {
		if recovered, ok := recoverUTF8FromLatin1(*fixed.TargetPostCID); ok {
			v := recovered
			fixed.TargetPostCID = &v
			changed = true
		}
	}
	if fixed.Reason != nil {
		if recovered, ok := recoverUTF8FromLatin1(*fixed.Reason); ok {
			v := recovered
			fixed.Reason = &v
			changed = true
		}
	}
	if fixed.PrevLogCID != nil {
		if recovered, ok := recoverUTF8FromLatin1(*fixed.PrevLogCID); ok {
			v := recovered
			fixed.PrevLogCID = &v
			changed = true
		}
	}
	if recovered, ok := recoverUTF8FromLatin1(fixed.CreatedAt); ok {
		fixed.CreatedAt = recovered
		changed = true
	}
	if recovered, ok := recoverUTF8FromLatin1(fixed.AuthorPubKey); ok {
		fixed.AuthorPubKey = recovered
		changed = true
	}
	if !changed {
		return false
	}

	pub2, err := signature.ParsePublicKey(fixed.AuthorPubKey)
	if err != nil {
		return false
	}
	payload = signature.CanonicalBoardLogEntryPayload(&fixed)
	if signature.VerifyBase64(pub2, payload, fixed.Signature) != nil {
		return false
	}
	*e = fixed
	return true
}

func VerifyThreadMeta(m *types.ThreadMeta) bool {
	pub, err := signature.ParsePublicKey(m.CreatedBy)
	if err != nil {
		if recovered, ok := recoverUTF8FromLatin1(m.CreatedBy); ok {
			m.CreatedBy = recovered
			pub, err = signature.ParsePublicKey(m.CreatedBy)
		}
		if err != nil {
			return false
		}
	}
	payload := signature.CanonicalThreadMetaPayload(m)
	if signature.VerifyBase64(pub, payload, m.Signature) == nil {
		return true
	}

	fixed := *m
	changed := false
	if recovered, ok := recoverUTF8FromLatin1(fixed.BoardID); ok {
		fixed.BoardID = recovered
		changed = true
	}
	if recovered, ok := recoverUTF8FromLatin1(fixed.Title); ok {
		fixed.Title = recovered
		changed = true
	}
	if recovered, ok := recoverUTF8FromLatin1(fixed.CreatedAt); ok {
		fixed.CreatedAt = recovered
		changed = true
	}
	if recovered, ok := recoverUTF8FromLatin1(fixed.CreatedBy); ok {
		fixed.CreatedBy = recovered
		changed = true
	}
	if !changed {
		return false
	}

	pub2, err := signature.ParsePublicKey(fixed.CreatedBy)
	if err != nil {
		return false
	}
	payload = signature.CanonicalThreadMetaPayload(&fixed)
	if signature.VerifyBase64(pub2, payload, fixed.Signature) != nil {
		return false
	}
	*m = fixed
	return true
}

func VerifyBoardMeta(m *types.BoardMeta) bool {
	pub, err := signature.ParsePublicKey(m.CreatedBy)
	if err != nil {
		if recovered, ok := recoverUTF8FromLatin1(m.CreatedBy); ok {
			m.CreatedBy = recovered
			pub, err = signature.ParsePublicKey(m.CreatedBy)
		}
		if err != nil {
			return false
		}
	}
	payload := signature.CanonicalBoardMetaPayload(m)
	if signature.VerifyBase64(pub, payload, m.Signature) == nil {
		return true
	}

	fixed := *m
	changed := false
	if recovered, ok := recoverUTF8FromLatin1(fixed.BoardID); ok {
		fixed.BoardID = recovered
		changed = true
	}
	if recovered, ok := recoverUTF8FromLatin1(fixed.Title); ok {
		fixed.Title = recovered
		changed = true
	}
	if recovered, ok := recoverUTF8FromLatin1(fixed.Description); ok {
		fixed.Description = recovered
		changed = true
	}
	if recovered, ok := recoverUTF8FromLatin1(fixed.CreatedAt); ok {
		fixed.CreatedAt = recovered
		changed = true
	}
	if recovered, ok := recoverUTF8FromLatin1(fixed.CreatedBy); ok {
		fixed.CreatedBy = recovered
		changed = true
	}
	if !changed {
		return false
	}

	pub2, err := signature.ParsePublicKey(fixed.CreatedBy)
	if err != nil {
		return false
	}
	payload = signature.CanonicalBoardMetaPayload(&fixed)
	if signature.VerifyBase64(pub2, payload, fixed.Signature) != nil {
		return false
	}
	*m = fixed
	return true
}
