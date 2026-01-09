package archive

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"

	"flex-bbs/backend-go/bbs/config"
	bbslog "flex-bbs/backend-go/bbs/log"
	"flex-bbs/backend-go/bbs/storage"
	"flex-bbs/backend-go/bbs/types"
)

type Archiver struct {
	Storage *storage.Storage
	Boards  *config.BoardsStore
	Dir     string
}

func (a *Archiver) SyncOnce(ctx context.Context) error {
	if err := a.Boards.Load(); err != nil {
		return err
	}
	if err := os.MkdirAll(a.Dir, 0o755); err != nil {
		return err
	}

	for _, ref := range a.Boards.List() {
		bm, err := a.Storage.LoadBoardMeta(ctx, ref.BoardMetaCID)
		if err != nil {
			continue
		}
		if !bbslog.VerifyBoardMeta(bm) {
			continue
		}
		_ = a.saveJSON(ref.BoardMetaCID, bm)

		loadLog := func(ctx context.Context, cid string) (*types.BoardLogEntry, error) {
			return a.Storage.LoadBoardLogEntry(ctx, cid)
		}
		boardLog, err := bbslog.FetchChain(ctx, bm.LogHeadCID, loadLog, func(e *types.BoardLogEntry) *string {
			return e.PrevLogCID
		}, bbslog.VerifyBoardLogEntry, 50_000)
		if err != nil {
			continue
		}

		for _, item := range boardLog {
			_ = a.saveJSON(item.CID, item.Value)
			if item.Value != nil {
				a.maybeArchiveThreadMeta(ctx, item.Value.ThreadID)
				a.maybeArchivePost(ctx, item.Value.PostCID)
				a.maybeArchivePost(ctx, item.Value.OldPostCID)
				a.maybeArchivePost(ctx, item.Value.NewPostCID)
				a.maybeArchivePost(ctx, item.Value.TargetPostCID)
			}
		}
	}
	return nil
}

func (a *Archiver) maybeArchivePost(ctx context.Context, cid *string) {
	if cid == nil || *cid == "" {
		return
	}
	p, err := a.Storage.LoadPost(ctx, *cid)
	if err != nil {
		return
	}
	if !bbslog.VerifyPost(p) {
		return
	}
	_ = a.saveJSON(*cid, p)
}

func (a *Archiver) maybeArchiveThreadMeta(ctx context.Context, cid string) {
	if cid == "" {
		return
	}
	tm, err := a.Storage.LoadThreadMeta(ctx, cid)
	if err != nil {
		return
	}
	if !bbslog.VerifyThreadMeta(tm) {
		return
	}
	_ = a.saveJSON(cid, tm)
}

func (a *Archiver) saveJSON(cid string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	p := filepath.Join(a.Dir, cid+".json")
	return os.WriteFile(p, b, 0o644)
}
