package log

import (
	"context"
	"fmt"

	"flex-bbs/backend-go/bbs/types"
)

type ReplayedPost struct {
	CID             string
	Post            *types.Post
	Tombstoned      bool
	TombstoneReason *string
}

func ReplayThread(
	ctx context.Context,
	boardLog []EntryWithCID[types.BoardLogEntry],
	threadID string,
	loadPost LoadFunc[types.Post],
	verifyPost func(*types.Post) bool,
	loadOldForAuth LoadFunc[types.Post],
) ([]ReplayedPost, error) {
	type slot struct {
		ReplayedPost
	}

	var (
		order      []slot
		indexByCID = make(map[string]int)
	)

	for _, item := range boardLog {
		e := item.Value
		if !item.ValidSignature {
			continue
		}
		if e.ThreadID != threadID {
			continue
		}

		switch e.Op {
		case types.OpCreateThread, types.OpAddPost:
			if e.PostCID == nil || *e.PostCID == "" {
				continue
			}
			postCID := *e.PostCID
			p, err := loadPost(ctx, postCID)
			if err != nil {
				return nil, err
			}
			if !verifyPost(p) {
				continue
			}
			if _, exists := indexByCID[postCID]; exists {
				continue
			}
			order = append(order, slot{ReplayedPost: ReplayedPost{CID: postCID, Post: p}})
			indexByCID[postCID] = len(order) - 1

		case types.OpEditPost:
			if e.OldPostCID == nil || *e.OldPostCID == "" {
				continue
			}
			if e.NewPostCID == nil || *e.NewPostCID == "" {
				continue
			}
			oldCID := *e.OldPostCID
			newCID := *e.NewPostCID

			idx, ok := indexByCID[oldCID]
			if !ok {
				continue
			}
			oldP := order[idx].Post
			if oldP == nil && loadOldForAuth != nil {
				var err error
				oldP, err = loadOldForAuth(ctx, oldCID)
				if err != nil {
					return nil, err
				}
				if !verifyPost(oldP) {
					continue
				}
			}
			if oldP == nil {
				continue
			}

			newP, err := loadPost(ctx, newCID)
			if err != nil {
				return nil, err
			}
			if !verifyPost(newP) {
				continue
			}
			if e.AuthorPubKey != oldP.AuthorPubKey || e.AuthorPubKey != newP.AuthorPubKey {
				continue
			}

			prevTombstoned := order[idx].Tombstoned
			prevReason := order[idx].TombstoneReason

			delete(indexByCID, oldCID)
			indexByCID[newCID] = idx
			order[idx].CID = newCID
			order[idx].Post = newP
			order[idx].Tombstoned = prevTombstoned
			order[idx].TombstoneReason = prevReason

		case types.OpTombstonePost:
			if e.TargetPostCID == nil || *e.TargetPostCID == "" {
				continue
			}
			targetCID := *e.TargetPostCID
			idx, ok := indexByCID[targetCID]
			if !ok {
				continue
			}
			target := order[idx].Post
			if target == nil {
				continue
			}
			if e.AuthorPubKey != target.AuthorPubKey {
				continue
			}
			order[idx].Tombstoned = true
			order[idx].TombstoneReason = e.Reason

		default:
			return nil, fmt.Errorf("unknown op: %s", e.Op)
		}
	}

	out := make([]ReplayedPost, 0, len(order))
	for _, s := range order {
		out = append(out, s.ReplayedPost)
	}
	return out, nil
}
