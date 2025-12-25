package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"flex-bbs/backend-go/bbs/config"
	bbsindexer "flex-bbs/backend-go/bbs/indexer"
	bbslog "flex-bbs/backend-go/bbs/log"
	"flex-bbs/backend-go/bbs/signature"
	"flex-bbs/backend-go/bbs/storage"
	"flex-bbs/backend-go/bbs/types"
)

type Server struct {
	Role            string
	Storage         *storage.Storage
	Boards          *config.BoardsStore
	TrustedIndexers *config.TrustedIndexersStore
	Indexer         *bbsindexer.Indexer

	httpClient        *http.Client
	seenBoardMetaCIDs *seenSet
}

func (s *Server) Handler() http.Handler {
	s.initNetworkDeps()

	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", s.healthz)
	mux.HandleFunc("GET /api/v1/boards", s.listBoards)
	mux.HandleFunc("GET /api/v1/boards/{boardId}", s.getBoard)
	mux.HandleFunc("GET /api/v1/boards/{boardId}/threads", s.listThreads)
	mux.HandleFunc("GET /api/v1/threads/{threadId}", s.getThread)
	mux.HandleFunc("POST /api/v1/threads", s.createThread)
	mux.HandleFunc("POST /api/v1/posts", s.addPost)
	mux.HandleFunc("POST /api/v1/posts/{postCid}/edit", s.editPost)
	mux.HandleFunc("POST /api/v1/posts/{postCid}/tombstone", s.tombstonePost)
	mux.HandleFunc("GET /api/v1/search/boards", s.searchBoards)
	mux.HandleFunc("GET /api/v1/search/threads", s.searchThreads)
	mux.HandleFunc("GET /api/v1/search/posts", s.searchPosts)
	mux.HandleFunc("POST /api/v1/announce/board", s.announceBoard)
	mux.HandleFunc("GET /api/v1/trusted-indexers", s.listTrustedIndexers)

	return mux
}

func (s *Server) initNetworkDeps() {
	if s.httpClient == nil {
		s.httpClient = &http.Client{Timeout: 10 * time.Second}
	}
	if s.seenBoardMetaCIDs == nil {
		s.seenBoardMetaCIDs = newSeenSet(4096, 30*time.Minute)
	}
}

func (s *Server) healthz(w http.ResponseWriter, r *http.Request) {
	writeText(w, http.StatusOK, "ok role="+s.Role)
}

func (s *Server) listBoards(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if err := s.Boards.Load(); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	refs := s.Boards.List()
	out := make([]BoardItem, 0, len(refs))
	for _, ref := range refs {
		bm, err := s.Storage.LoadBoardMeta(ctx, ref.BoardMetaCID)
		if err != nil {
			log.Printf("api listBoards: load boardMeta cid=%s: %v", ref.BoardMetaCID, err)
			continue
		}
		if !bbslog.VerifyBoardMeta(bm) {
			log.Printf("api listBoards: invalid boardMeta signature cid=%s boardId=%s", ref.BoardMetaCID, bm.BoardID)
			continue
		}
		out = append(out, BoardItem{BoardMetaCID: ref.BoardMetaCID, Board: *bm})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) getBoard(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	boardID := r.PathValue("boardId")
	refCID, bm, ok := s.loadBoardByID(ctx, boardID)
	if !ok {
		writeError(w, http.StatusNotFound, "board not found")
		return
	}
	writeJSON(w, http.StatusOK, BoardItem{BoardMetaCID: refCID, Board: *bm})
}

func (s *Server) listThreads(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	boardID := r.PathValue("boardId")
	s.syncBoardFromTrustedIndexersBestEffort(ctx, boardID)
	_, bm, ok := s.loadBoardByID(ctx, boardID)
	if !ok {
		writeError(w, http.StatusNotFound, "board not found")
		return
	}

	limit, offset := parseLimitOffset(r, 50, 0, 200)

	loadLog := func(ctx context.Context, cid string) (*types.BoardLogEntry, error) {
		return s.Storage.LoadBoardLogEntry(ctx, cid)
	}
	boardLog, err := bbslog.FetchChain(ctx, bm.LogHeadCID, loadLog, func(e *types.BoardLogEntry) *string {
		return e.PrevLogCID
	}, bbslog.VerifyBoardLogEntry, 50_000)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}

	type threadState struct {
		ThreadCID        string
		CreatedAt        string
		RootPostCID      string
		RootAuthorPubKey string
		RootTombstoned   bool
	}
	byThread := make(map[string]*threadState)
	for _, item := range boardLog {
		if !item.ValidSignature {
			continue
		}
		e := item.Value
		if e.BoardID != boardID {
			continue
		}

		switch e.Op {
		case types.OpCreateThread:
			if e.PostCID == nil || *e.PostCID == "" {
				continue
			}
			if _, ok := byThread[e.ThreadID]; ok {
				continue
			}
			byThread[e.ThreadID] = &threadState{
				ThreadCID:        e.ThreadID,
				CreatedAt:        e.CreatedAt,
				RootPostCID:      *e.PostCID,
				RootAuthorPubKey: e.AuthorPubKey,
			}

		case types.OpEditPost:
			st, ok := byThread[e.ThreadID]
			if !ok || st == nil {
				continue
			}
			if e.OldPostCID == nil || *e.OldPostCID == "" || e.NewPostCID == nil || *e.NewPostCID == "" {
				continue
			}
			if *e.OldPostCID != st.RootPostCID {
				continue
			}
			// Align with thread replay semantics: only the post author can edit it.
			if st.RootAuthorPubKey != "" && e.AuthorPubKey != st.RootAuthorPubKey {
				continue
			}
			st.RootPostCID = *e.NewPostCID

		case types.OpTombstonePost:
			st, ok := byThread[e.ThreadID]
			if !ok || st == nil {
				continue
			}
			if e.TargetPostCID == nil || *e.TargetPostCID == "" {
				continue
			}
			if *e.TargetPostCID != st.RootPostCID {
				continue
			}
			// Align with thread replay semantics: only the post author can tombstone it.
			if st.RootAuthorPubKey != "" && e.AuthorPubKey != st.RootAuthorPubKey {
				continue
			}
			st.RootTombstoned = true
		}
	}

	threads := make([]ThreadItem, 0, len(byThread))
	for _, x := range byThread {
		if x == nil || x.RootTombstoned {
			continue
		}
		tm, err := s.Storage.LoadThreadMeta(ctx, x.ThreadCID)
		if err != nil {
			log.Printf("api listThreads: load threadMeta cid=%s: %v", x.ThreadCID, err)
			continue
		}
		if !bbslog.VerifyThreadMeta(tm) {
			log.Printf("api listThreads: invalid threadMeta signature cid=%s threadId=%s", x.ThreadCID, tm.ThreadID)
			continue
		}
		tm.ThreadID = x.ThreadCID
		tm.RootPostCID = x.RootPostCID
		threads = append(threads, ThreadItem{ThreadID: x.ThreadCID, ThreadMetaCID: x.ThreadCID, Thread: *tm})
	}

	sortThreadsNewestFirst(threads)
	threads = applyOffsetLimit(threads, offset, limit)
	writeJSON(w, http.StatusOK, threads)
}

func (s *Server) getThread(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	threadCID := r.PathValue("threadId")

	tm, err := s.Storage.LoadThreadMeta(ctx, threadCID)
	if err != nil {
		writeError(w, http.StatusNotFound, "thread not found")
		return
	}
	if !bbslog.VerifyThreadMeta(tm) {
		writeError(w, http.StatusBadGateway, "invalid threadMeta signature")
		return
	}
	boardID := tm.BoardID
	s.syncBoardFromTrustedIndexersBestEffort(ctx, boardID)

	var (
		rootPostCID string
		posts       []bbslog.ReplayedPost
	)

	// Best-effort primary: follow the board log head from local boards.json (consistent ordering),
	// but fall back to tag-based log discovery when the board isn't registered yet or is out-of-date.
	var primaryPosts []bbslog.ReplayedPost
	var primaryRoot string
	if _, bm, ok := s.loadBoardByID(ctx, boardID); ok {
		loadLog := func(ctx context.Context, cid string) (*types.BoardLogEntry, error) {
			return s.Storage.LoadBoardLogEntry(ctx, cid)
		}
		boardLog, err := bbslog.FetchChain(ctx, bm.LogHeadCID, loadLog, func(e *types.BoardLogEntry) *string {
			return e.PrevLogCID
		}, bbslog.VerifyBoardLogEntry, 50_000)
		if err != nil {
			log.Printf("api getThread: board log fetch failed boardId=%s: %v", boardID, err)
		} else {
			for _, item := range boardLog {
				if !item.ValidSignature {
					continue
				}
				e := item.Value
				if e.Op == types.OpCreateThread && e.ThreadID == threadCID && e.PostCID != nil {
					primaryRoot = *e.PostCID
					break
				}
			}

			loadPost := func(ctx context.Context, cid string) (*types.Post, error) {
				return s.Storage.LoadPost(ctx, cid)
			}
			replayed, err := bbslog.ReplayThread(ctx, boardLog, threadCID, loadPost, bbslog.VerifyPost, nil)
			if err != nil {
				log.Printf("api getThread: board log replay failed boardId=%s threadId=%s: %v", boardID, threadCID, err)
			} else {
				primaryPosts = replayed
			}
		}
	}

	fallbackPosts, fallbackRoot, fallbackErr := s.replayThreadFromTags(ctx, boardID, threadCID)
	if fallbackErr != nil {
		log.Printf("api getThread: tag replay failed boardId=%s threadId=%s: %v", boardID, threadCID, fallbackErr)
	}

	// Prefer the result that contains more posts (helps cross-device sync when boards.json is stale).
	rootPostCID = primaryRoot
	posts = primaryPosts
	if len(fallbackPosts) > len(primaryPosts) {
		rootPostCID = fallbackRoot
		posts = fallbackPosts
	} else if rootPostCID == "" {
		rootPostCID = fallbackRoot
	}

	if len(posts) == 0 && fallbackErr != nil && len(primaryPosts) == 0 {
		writeError(w, http.StatusBadGateway, fallbackErr.Error())
		return
	}

	threadMeta := *tm
	threadMeta.ThreadID = threadCID
	if rootPostCID != "" {
		threadMeta.RootPostCID = rootPostCID
	}

	outPosts := make([]ThreadPostItem, 0, len(posts))
	for _, p := range posts {
		post := *p.Post
		postCID := p.CID
		post.PostCID = &postCID
		outPosts = append(outPosts, ThreadPostItem{
			CID:             p.CID,
			Post:            post,
			Tombstoned:      p.Tombstoned,
			TombstoneReason: p.TombstoneReason,
		})
	}

	writeJSON(w, http.StatusOK, ThreadResponse{
		ThreadMetaCID: threadCID,
		ThreadMeta:    threadMeta,
		Posts:         outPosts,
	})
}

func (s *Server) replayThreadFromTags(ctx context.Context, boardID, threadID string) ([]bbslog.ReplayedPost, string, error) {
	if s.Storage == nil || s.Storage.Flex == nil {
		return nil, "", nil
	}

	tag := storage.TagBoardThread(boardID, threadID)
	cids, err := s.Storage.Flex.GetByAttrs(ctx, nil, []string{tag}, true)
	if err != nil {
		return nil, "", err
	}
	if len(cids) == 0 {
		return nil, "", nil
	}

	var entries []bbslog.EntryWithCID[types.BoardLogEntry]
	for _, cid := range cids {
		e, err := s.Storage.LoadBoardLogEntry(ctx, cid)
		if err != nil {
			continue
		}
		if e.Type != types.TypeBoardLogEntry {
			continue
		}
		if e.BoardID != boardID || e.ThreadID != threadID {
			continue
		}
		entries = append(entries, bbslog.EntryWithCID[types.BoardLogEntry]{
			CID:            cid,
			Value:          e,
			ValidSignature: bbslog.VerifyBoardLogEntry(e),
		})
	}

	sort.Slice(entries, func(i, j int) bool {
		a := entries[i].Value
		b := entries[j].Value

		ta, errA := time.Parse(time.RFC3339, a.CreatedAt)
		tb, errB := time.Parse(time.RFC3339, b.CreatedAt)
		switch {
		case errA == nil && errB == nil && !ta.Equal(tb):
			return ta.Before(tb)
		case a.CreatedAt != b.CreatedAt:
			return a.CreatedAt < b.CreatedAt
		}

		// Within the same second, preserve the typical lifecycle order.
		weight := func(op string) int {
			switch op {
			case types.OpCreateThread:
				return 0
			case types.OpAddPost:
				return 1
			case types.OpEditPost:
				return 2
			case types.OpTombstonePost:
				return 3
			default:
				return 9
			}
		}
		wa, wb := weight(a.Op), weight(b.Op)
		if wa != wb {
			return wa < wb
		}
		return entries[i].CID < entries[j].CID
	})

	var rootPostCID string
	for _, item := range entries {
		if !item.ValidSignature {
			continue
		}
		e := item.Value
		if e.Op == types.OpCreateThread && e.PostCID != nil && *e.PostCID != "" {
			rootPostCID = *e.PostCID
			break
		}
	}

	loadPost := func(ctx context.Context, cid string) (*types.Post, error) {
		return s.Storage.LoadPost(ctx, cid)
	}
	posts, err := bbslog.ReplayThread(ctx, entries, threadID, loadPost, bbslog.VerifyPost, nil)
	if err != nil {
		return nil, "", err
	}
	return posts, rootPostCID, nil
}

func (s *Server) createThread(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var req CreateThreadRequest
	if err := readJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.BoardID == "" || req.Title == "" || req.Body.Format == "" || req.AuthorPrivKey == "" {
		writeError(w, http.StatusBadRequest, "missing required fields")
		return
	}
	if req.Body.Content == "" {
		writeError(w, http.StatusBadRequest, "body.content is required")
		return
	}

	_, bm, ok := s.loadBoardByID(ctx, req.BoardID)
	if !ok {
		writeError(w, http.StatusNotFound, "board not found")
		return
	}

	threadMeta := &types.ThreadMeta{
		Version:     1,
		Type:        types.TypeThreadMeta,
		ThreadID:    "",
		BoardID:     req.BoardID,
		Title:       req.Title,
		RootPostCID: "",
		CreatedAt:   types.NowUTC(),
		CreatedBy:   "",
		Meta:        req.ThreadMeta,
		Signature:   "",
	}
	if threadMeta.Meta == nil {
		threadMeta.Meta = map[string]any{}
	}
	if err := signature.SignThreadMeta(req.AuthorPrivKey, threadMeta); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	threadCID, err := s.Storage.SaveThreadMeta(ctx, threadMeta)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}

	rootPost := &types.Post{
		Version:       1,
		Type:          types.TypePost,
		PostCID:       nil,
		ThreadID:      threadCID,
		ParentPostCID: nil,
		AuthorPubKey:  "",
		DisplayName:   req.DisplayName,
		Body:          req.Body,
		Attachments:   req.Attachments,
		CreatedAt:     types.NowUTC(),
		EditedAt:      nil,
		Meta:          req.PostMeta,
		Signature:     "",
	}
	if rootPost.Meta == nil {
		rootPost.Meta = map[string]any{}
	}
	if err := signature.SignPost(req.AuthorPrivKey, rootPost); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	rootPostCID, err := s.Storage.SavePost(ctx, req.BoardID, rootPost)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}

	entry := &types.BoardLogEntry{
		Version:      1,
		Type:         types.TypeBoardLogEntry,
		BoardID:      req.BoardID,
		Op:           types.OpCreateThread,
		ThreadID:     threadCID,
		PostCID:      &rootPostCID,
		CreatedAt:    types.NowUTC(),
		AuthorPubKey: "",
		PrevLogCID:   bm.LogHeadCID,
	}
	if err := signature.SignBoardLogEntry(req.AuthorPrivKey, entry); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	logCID, err := s.Storage.SaveBoardLogEntry(ctx, entry)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}

	newBoardMetaCID, err := s.advanceBoardLogHead(ctx, bm, req.BoardID, logCID)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}

	threadMeta.ThreadID = threadCID
	threadMeta.RootPostCID = rootPostCID
	writeJSON(w, http.StatusOK, CreateThreadResponse{
		ThreadID:     threadCID,
		RootPostCID:  rootPostCID,
		BoardLogCID:  logCID,
		BoardMetaCID: newBoardMetaCID,
		ThreadMeta:   *threadMeta,
	})
}

func (s *Server) addPost(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var req AddPostRequest
	if err := readJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.ThreadID == "" || req.Body.Format == "" || req.Body.Content == "" || req.AuthorPrivKey == "" {
		writeError(w, http.StatusBadRequest, "missing required fields")
		return
	}

	tm, err := s.Storage.LoadThreadMeta(ctx, req.ThreadID)
	if err != nil {
		writeError(w, http.StatusNotFound, "thread not found")
		return
	}
	if !bbslog.VerifyThreadMeta(tm) {
		writeError(w, http.StatusBadGateway, "invalid threadMeta signature")
		return
	}
	boardID := tm.BoardID
	_, bm, ok := s.loadBoardByID(ctx, boardID)
	if !ok {
		writeError(w, http.StatusNotFound, "board not found")
		return
	}

	p := &types.Post{
		Version:       1,
		Type:          types.TypePost,
		PostCID:       nil,
		ThreadID:      req.ThreadID,
		ParentPostCID: req.ParentPostCID,
		AuthorPubKey:  "",
		DisplayName:   req.DisplayName,
		Body:          req.Body,
		Attachments:   req.Attachments,
		CreatedAt:     types.NowUTC(),
		EditedAt:      nil,
		Meta:          req.Meta,
		Signature:     "",
	}
	if p.Meta == nil {
		p.Meta = map[string]any{}
	}
	if err := signature.SignPost(req.AuthorPrivKey, p); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	postCID, err := s.Storage.SavePost(ctx, boardID, p)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}

	e := &types.BoardLogEntry{
		Version:      1,
		Type:         types.TypeBoardLogEntry,
		BoardID:      boardID,
		Op:           types.OpAddPost,
		ThreadID:     req.ThreadID,
		PostCID:      &postCID,
		CreatedAt:    types.NowUTC(),
		AuthorPubKey: "",
		PrevLogCID:   bm.LogHeadCID,
	}
	if err := signature.SignBoardLogEntry(req.AuthorPrivKey, e); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	logCID, err := s.Storage.SaveBoardLogEntry(ctx, e)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	newBoardMetaCID, err := s.advanceBoardLogHead(ctx, bm, boardID, logCID)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, AddPostResponse{
		PostCID:      postCID,
		BoardLogCID:  logCID,
		BoardMetaCID: newBoardMetaCID,
	})
}

func (s *Server) editPost(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	oldCID := r.PathValue("postCid")
	var req EditPostRequest
	if err := readJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Body.Format == "" || req.Body.Content == "" || req.AuthorPrivKey == "" {
		writeError(w, http.StatusBadRequest, "missing required fields")
		return
	}

	oldPost, err := s.Storage.LoadPost(ctx, oldCID)
	if err != nil {
		writeError(w, http.StatusNotFound, "post not found")
		return
	}
	if !bbslog.VerifyPost(oldPost) {
		writeError(w, http.StatusBadGateway, "invalid post signature")
		return
	}

	pubStr, err := pubFromPrivKey(req.AuthorPrivKey)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if pubStr != oldPost.AuthorPubKey {
		writeError(w, http.StatusForbidden, "author mismatch")
		return
	}

	tm, err := s.Storage.LoadThreadMeta(ctx, oldPost.ThreadID)
	if err != nil {
		writeError(w, http.StatusNotFound, "thread not found")
		return
	}
	if !bbslog.VerifyThreadMeta(tm) {
		writeError(w, http.StatusBadGateway, "invalid threadMeta signature")
		return
	}
	boardID := tm.BoardID
	_, bm, ok := s.loadBoardByID(ctx, boardID)
	if !ok {
		writeError(w, http.StatusNotFound, "board not found")
		return
	}

	editedAt := types.NowUTC()
	newPost := &types.Post{
		Version:       1,
		Type:          types.TypePost,
		PostCID:       nil,
		ThreadID:      oldPost.ThreadID,
		ParentPostCID: oldPost.ParentPostCID,
		AuthorPubKey:  "",
		DisplayName:   oldPost.DisplayName,
		Body:          req.Body,
		Attachments:   oldPost.Attachments,
		CreatedAt:     oldPost.CreatedAt,
		EditedAt:      &editedAt,
		Meta:          oldPost.Meta,
		Signature:     "",
	}
	if req.DisplayName != nil {
		newPost.DisplayName = *req.DisplayName
	}
	if newPost.Meta == nil {
		newPost.Meta = map[string]any{}
	}
	if err := signature.SignPost(req.AuthorPrivKey, newPost); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	newCID, err := s.Storage.SavePost(ctx, boardID, newPost)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}

	e := &types.BoardLogEntry{
		Version:      1,
		Type:         types.TypeBoardLogEntry,
		BoardID:      boardID,
		Op:           types.OpEditPost,
		ThreadID:     oldPost.ThreadID,
		OldPostCID:   &oldCID,
		NewPostCID:   &newCID,
		CreatedAt:    types.NowUTC(),
		AuthorPubKey: "",
		PrevLogCID:   bm.LogHeadCID,
	}
	if err := signature.SignBoardLogEntry(req.AuthorPrivKey, e); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	logCID, err := s.Storage.SaveBoardLogEntry(ctx, e)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	newBoardMetaCID, err := s.advanceBoardLogHead(ctx, bm, boardID, logCID)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, EditPostResponse{
		OldPostCID:   oldCID,
		NewPostCID:   newCID,
		BoardLogCID:  logCID,
		BoardMetaCID: newBoardMetaCID,
	})
}

func (s *Server) tombstonePost(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	targetCID := r.PathValue("postCid")
	var req TombstonePostRequest
	if err := readJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.AuthorPrivKey == "" {
		writeError(w, http.StatusBadRequest, "missing required fields")
		return
	}

	target, err := s.Storage.LoadPost(ctx, targetCID)
	if err != nil {
		writeError(w, http.StatusNotFound, "post not found")
		return
	}
	if !bbslog.VerifyPost(target) {
		writeError(w, http.StatusBadGateway, "invalid post signature")
		return
	}

	pubStr, err := pubFromPrivKey(req.AuthorPrivKey)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if pubStr != target.AuthorPubKey {
		writeError(w, http.StatusForbidden, "author mismatch")
		return
	}

	tm, err := s.Storage.LoadThreadMeta(ctx, target.ThreadID)
	if err != nil {
		writeError(w, http.StatusNotFound, "thread not found")
		return
	}
	if !bbslog.VerifyThreadMeta(tm) {
		writeError(w, http.StatusBadGateway, "invalid threadMeta signature")
		return
	}
	boardID := tm.BoardID
	_, bm, ok := s.loadBoardByID(ctx, boardID)
	if !ok {
		writeError(w, http.StatusNotFound, "board not found")
		return
	}

	e := &types.BoardLogEntry{
		Version:       1,
		Type:          types.TypeBoardLogEntry,
		BoardID:       boardID,
		Op:            types.OpTombstonePost,
		ThreadID:      target.ThreadID,
		TargetPostCID: &targetCID,
		Reason:        req.Reason,
		CreatedAt:     types.NowUTC(),
		AuthorPubKey:  "",
		PrevLogCID:    bm.LogHeadCID,
	}
	if err := signature.SignBoardLogEntry(req.AuthorPrivKey, e); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	logCID, err := s.Storage.SaveBoardLogEntry(ctx, e)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	newBoardMetaCID, err := s.advanceBoardLogHead(ctx, bm, boardID, logCID)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, TombstonePostResponse{
		TargetPostCID: targetCID,
		BoardLogCID:   logCID,
		BoardMetaCID:  newBoardMetaCID,
	})
}

func (s *Server) searchPosts(w http.ResponseWriter, r *http.Request) {
	if s.Indexer == nil {
		s.proxySearch(w, r, "/api/v1/search/posts")
		return
	}
	ctx := r.Context()
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	boardID := strings.TrimSpace(r.URL.Query().Get("boardId"))
	author := strings.TrimSpace(r.URL.Query().Get("author"))
	since := strings.TrimSpace(r.URL.Query().Get("since"))
	until := strings.TrimSpace(r.URL.Query().Get("until"))
	limit, offset := parseLimitOffset(r, 50, 0, 200)

	results, err := s.Indexer.SearchPosts(ctx, bbsindexer.SearchPostsParams{
		Query:        q,
		BoardID:      boardID,
		AuthorPubKey: author,
		Since:        since,
		Until:        until,
		Limit:        limit,
		Offset:       offset,
	})
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, results)
}

func (s *Server) searchBoards(w http.ResponseWriter, r *http.Request) {
	if s.Indexer == nil {
		s.proxySearch(w, r, "/api/v1/search/boards")
		return
	}
	ctx := r.Context()
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	limit, offset := parseLimitOffset(r, 50, 0, 200)

	results, err := s.Indexer.SearchBoards(ctx, bbsindexer.SearchBoardsParams{
		Query:  q,
		Limit:  limit,
		Offset: offset,
	})
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}

	out := make([]BoardItem, 0, len(results))
	for _, b := range results {
		out = append(out, BoardItem{
			BoardMetaCID: b.BoardMetaCID,
			Board: types.BoardMeta{
				Version:     types.Version1,
				Type:        types.TypeBoardMeta,
				BoardID:     b.BoardID,
				Title:       b.Title,
				Description: b.Description,
				LogHeadCID:  b.LogHeadCID,
				CreatedAt:   b.CreatedAt,
				CreatedBy:   b.CreatedBy,
				Signature:   b.Signature,
			},
		})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) searchThreads(w http.ResponseWriter, r *http.Request) {
	if s.Indexer == nil {
		s.proxySearch(w, r, "/api/v1/search/threads")
		return
	}
	ctx := r.Context()
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	boardID := strings.TrimSpace(r.URL.Query().Get("boardId"))
	limit, offset := parseLimitOffset(r, 50, 0, 200)

	results, err := s.Indexer.SearchThreads(ctx, bbsindexer.SearchThreadsParams{
		Query:   q,
		BoardID: boardID,
		Limit:   limit,
		Offset:  offset,
	})
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}

	out := make([]ThreadItem, 0, len(results))
	for _, t := range results {
		out = append(out, ThreadItem{
			ThreadID:      t.ThreadID,
			ThreadMetaCID: t.ThreadID,
			Thread: types.ThreadMeta{
				Version:     types.Version1,
				Type:        types.TypeThreadMeta,
				ThreadID:    t.ThreadID,
				BoardID:     t.BoardID,
				Title:       t.Title,
				RootPostCID: t.RootPostCID,
				CreatedAt:   t.CreatedAt,
				CreatedBy:   t.CreatedBy,
				Signature:   t.Signature,
			},
		})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) loadBoardByID(ctx context.Context, boardID string) (refCID string, bm *types.BoardMeta, ok bool) {
	if err := s.Boards.Load(); err != nil {
		return "", nil, false
	}
	refCID, ok = s.Boards.Get(boardID)
	if !ok {
		return "", nil, false
	}
	bm, err := s.Storage.LoadBoardMeta(ctx, refCID)
	if err != nil {
		return "", nil, false
	}
	if !bbslog.VerifyBoardMeta(bm) {
		return "", nil, false
	}
	return refCID, bm, true
}

func (s *Server) advanceBoardLogHead(ctx context.Context, bm *types.BoardMeta, boardID, newLogCID string) (string, error) {
	newBoardMeta := *bm
	newBoardMeta.LogHeadCID = &newLogCID

	newBoardMetaCID, err := s.Storage.SaveBoardMeta(ctx, &newBoardMeta)
	if err != nil {
		return "", err
	}
	if err := s.Boards.Upsert(boardID, newBoardMetaCID); err != nil {
		return "", err
	}

	if s.Indexer != nil {
		_ = s.Indexer.SyncBoardByMetaCID(ctx, newBoardMetaCID)
	}
	s.markSeenBoardMetaCID(newBoardMetaCID)
	_ = s.forwardBoardAnnounceBestEffort(ctx, newBoardMetaCID)
	return newBoardMetaCID, nil
}

func (s *Server) announceBoard(w http.ResponseWriter, r *http.Request) {
	if s.Role != "indexer" && s.Role != "full" && s.Role != "client" {
		writeError(w, http.StatusNotImplemented, "announce is available in client/indexer/full roles")
		return
	}
	ctx := r.Context()
	var req AnnounceBoardRequest
	if err := readJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	req.BoardMetaCID = strings.TrimSpace(req.BoardMetaCID)
	if req.BoardMetaCID == "" {
		writeError(w, http.StatusBadRequest, "boardMetaCid is required")
		return
	}

	if s.seenBoardMetaCIDs != nil && s.seenBoardMetaCIDs.Seen(req.BoardMetaCID) {
		writeJSON(w, http.StatusOK, AnnounceBoardResponse{
			BoardMetaCID:  req.BoardMetaCID,
			Accepted:      false,
			IgnoredReason: "seen",
		})
		return
	}

	bm, err := s.Storage.LoadBoardMeta(ctx, req.BoardMetaCID)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	if !bbslog.VerifyBoardMeta(bm) {
		writeError(w, http.StatusBadRequest, "invalid boardMeta signature")
		return
	}
	boardID := strings.TrimSpace(bm.BoardID)
	if boardID == "" {
		writeError(w, http.StatusBadRequest, "boardId is empty in boardMeta")
		return
	}

	accepted := true
	ignoredReason := ""

	currentCID, currentBM, hasCurrent := s.loadBoardByID(ctx, boardID)

	// Clients should not auto-add unknown boards via announce; only accept updates for boards that are already registered.
	if s.Role == "client" && !hasCurrent {
		writeJSON(w, http.StatusOK, AnnounceBoardResponse{
			BoardID:       boardID,
			BoardMetaCID:  req.BoardMetaCID,
			Accepted:      false,
			IgnoredReason: "not-registered",
			Forwarded:     0,
		})
		return
	}

	if hasCurrent {
		accept, reason, err := s.shouldAcceptBoardMetaUpdate(ctx, boardID, currentCID, currentBM, req.BoardMetaCID, bm)
		if err != nil {
			writeError(w, http.StatusBadGateway, err.Error())
			return
		}
		if !accept {
			accepted = false
			ignoredReason = reason
		}
	}

	s.markSeenBoardMetaCID(req.BoardMetaCID)

	forwarded := 0
	if accepted {
		if err := s.Boards.Upsert(boardID, req.BoardMetaCID); err != nil {
			writeError(w, http.StatusBadGateway, err.Error())
			return
		}
		if s.Indexer != nil {
			_ = s.Indexer.SyncBoardByMetaCID(ctx, req.BoardMetaCID)
		}
		// Avoid bouncing announces back to trusted indexers when a client is merely updating its local state.
		if s.Role != "client" {
			forwarded = s.forwardBoardAnnounceBestEffort(ctx, req.BoardMetaCID)
		}
	} else {
		log.Printf("api announceBoard ignored boardId=%s reason=%s cid=%s", boardID, ignoredReason, req.BoardMetaCID)
	}

	writeJSON(w, http.StatusOK, AnnounceBoardResponse{
		BoardID:       boardID,
		BoardMetaCID:  req.BoardMetaCID,
		Accepted:      accepted,
		IgnoredReason: ignoredReason,
		Forwarded:     forwarded,
	})
}

func (s *Server) shouldAcceptBoardMetaUpdate(
	ctx context.Context,
	boardID string,
	currentBoardMetaCID string,
	current *types.BoardMeta,
	incomingBoardMetaCID string,
	incoming *types.BoardMeta,
) (accept bool, reason string, _ error) {
	_ = incomingBoardMetaCID
	currentHead := strOrEmpty(current.LogHeadCID)
	incomingHead := strOrEmpty(incoming.LogHeadCID)

	if currentHead == incomingHead {
		return true, "same", nil
	}
	if currentHead == "" {
		return true, "advance", nil
	}
	if incomingHead == "" {
		return false, "rollback", nil
	}

	isIncomingDesc, err := s.isBoardLogDescendant(ctx, boardID, incomingHead, currentHead)
	if err != nil {
		return false, "", err
	}
	if isIncomingDesc {
		return true, "advance", nil
	}

	isCurrentDesc, err := s.isBoardLogDescendant(ctx, boardID, currentHead, incomingHead)
	if err != nil {
		return false, "", err
	}
	if isCurrentDesc {
		return false, "rollback", nil
	}

	log.Printf(
		"api announceBoard fork detected boardId=%s currentMeta=%s currentHead=%s incomingMeta=%s incomingHead=%s (keeping current)",
		boardID, currentBoardMetaCID, currentHead, incomingBoardMetaCID, incomingHead,
	)
	return false, "fork", nil
}

func (s *Server) isBoardLogDescendant(ctx context.Context, boardID, headCID, ancestorCID string) (bool, error) {
	if headCID == "" || ancestorCID == "" {
		return false, nil
	}

	visited := make(map[string]struct{})
	current := headCID
	for current != "" {
		if current == ancestorCID {
			return true, nil
		}
		if _, ok := visited[current]; ok {
			return false, nil
		}
		if len(visited) >= 50_000 {
			return false, bbslog.ErrLogTooDeep
		}
		visited[current] = struct{}{}

		e, err := s.Storage.LoadBoardLogEntry(ctx, current)
		if err != nil {
			return false, err
		}
		if !bbslog.VerifyBoardLogEntry(e) {
			return false, fmt.Errorf("invalid boardLogEntry signature cid=%s", current)
		}
		if e.BoardID != boardID {
			return false, fmt.Errorf("boardLogEntry boardId mismatch cid=%s got=%s want=%s", current, e.BoardID, boardID)
		}

		if e.PrevLogCID == nil || *e.PrevLogCID == "" {
			break
		}
		current = *e.PrevLogCID
	}
	return false, nil
}

func (s *Server) syncBoardFromTrustedIndexersBestEffort(ctx context.Context, boardID string) {
	if s.Role != "client" || s.TrustedIndexers == nil || s.Boards == nil {
		return
	}
	boardID = strings.TrimSpace(boardID)
	if boardID == "" {
		return
	}

	// Only sync known boards; clients don't auto-add boards via trusted indexers.
	currentCID, currentBM, ok := s.loadBoardByID(ctx, boardID)
	if !ok || currentBM == nil {
		return
	}
	currentCID = strings.TrimSpace(currentCID)

	if err := s.TrustedIndexers.Load(); err != nil {
		log.Printf("trusted indexers load error: %v", err)
		return
	}
	peers := s.TrustedIndexers.List()
	if len(peers) == 0 {
		return
	}

	s.initNetworkDeps()
	for _, baseURL := range peers {
		endpoint := strings.TrimRight(baseURL, "/") + "/api/v1/boards/" + url.PathEscape(boardID)

		rctx, cancel := context.WithTimeout(ctx, 3*time.Second)
		req, err := http.NewRequestWithContext(rctx, http.MethodGet, endpoint, nil)
		if err != nil {
			cancel()
			continue
		}

		resp, err := s.httpClient.Do(req)
		if err != nil {
			cancel()
			continue
		}
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
		_ = resp.Body.Close()
		cancel()

		if resp.StatusCode == http.StatusNotFound {
			continue
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			continue
		}

		var item BoardItem
		if err := json.Unmarshal(body, &item); err != nil {
			continue
		}
		incomingCID := strings.TrimSpace(item.BoardMetaCID)
		if incomingCID == "" || incomingCID == currentCID {
			continue
		}

		incomingBM := item.Board
		if strings.TrimSpace(incomingBM.BoardID) != boardID {
			continue
		}
		if !bbslog.VerifyBoardMeta(&incomingBM) {
			continue
		}

		accept, _, err := s.shouldAcceptBoardMetaUpdate(ctx, boardID, currentCID, currentBM, incomingCID, &incomingBM)
		if err != nil {
			log.Printf("client board sync failed boardId=%s base=%s: %v", boardID, baseURL, err)
			continue
		}
		if !accept {
			continue
		}

		if err := s.Boards.Upsert(boardID, incomingCID); err != nil {
			log.Printf("client board sync save failed boardId=%s: %v", boardID, err)
			return
		}
		s.markSeenBoardMetaCID(incomingCID)
		return
	}
}

func (s *Server) markSeenBoardMetaCID(boardMetaCID string) {
	s.initNetworkDeps()
	if s.seenBoardMetaCIDs == nil {
		return
	}
	s.seenBoardMetaCIDs.Mark(strings.TrimSpace(boardMetaCID))
}

func (s *Server) forwardBoardAnnounceBestEffort(ctx context.Context, boardMetaCID string) int {
	if s.TrustedIndexers == nil {
		return 0
	}
	if err := s.TrustedIndexers.Load(); err != nil {
		log.Printf("trusted indexers load error: %v", err)
		return 0
	}
	peers := s.TrustedIndexers.List()
	if len(peers) == 0 {
		return 0
	}

	s.initNetworkDeps()
	forwarded := 0
	for _, baseURL := range peers {
		endpoint := strings.TrimRight(baseURL, "/") + "/api/v1/announce/board"
		reqBody, _ := json.Marshal(AnnounceBoardRequest{BoardMetaCID: boardMetaCID})

		rctx, cancel := context.WithTimeout(ctx, 3*time.Second)
		req, err := http.NewRequestWithContext(rctx, http.MethodPost, endpoint, bytes.NewReader(reqBody))
		if err != nil {
			cancel()
			log.Printf("announce forward: request error base=%s: %v", baseURL, err)
			continue
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := s.httpClient.Do(req)
		if err != nil {
			cancel()
			log.Printf("announce forward: http error base=%s: %v", baseURL, err)
			continue
		}
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<10))
		_ = resp.Body.Close()
		cancel()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			msg := strings.TrimSpace(string(body))
			if msg == "" {
				msg = resp.Status
			}
			log.Printf("announce forward: http %d base=%s: %s", resp.StatusCode, baseURL, msg)
			continue
		}
		forwarded++
	}
	return forwarded
}

func (s *Server) listTrustedIndexers(w http.ResponseWriter, r *http.Request) {
	_ = r
	if s.TrustedIndexers == nil {
		writeJSON(w, http.StatusOK, []string{})
		return
	}
	if err := s.TrustedIndexers.Load(); err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, s.TrustedIndexers.List())
}

func (s *Server) proxySearch(w http.ResponseWriter, r *http.Request, apiPath string) {
	if s.TrustedIndexers == nil {
		writeError(w, http.StatusNotImplemented, "search requires an indexer/full role or a trusted indexer proxy")
		return
	}
	if err := s.TrustedIndexers.Load(); err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	peers := s.TrustedIndexers.List()
	if len(peers) == 0 {
		writeError(w, http.StatusNotImplemented, "no trusted indexers configured")
		return
	}

	s.initNetworkDeps()

	query := strings.TrimSpace(r.URL.RawQuery)

	var lastErr string
	for _, baseURL := range peers {
		target := strings.TrimRight(baseURL, "/") + apiPath
		if query != "" {
			target += "?" + query
		}

		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
		if err != nil {
			cancel()
			lastErr = err.Error()
			log.Printf("search proxy request error base=%s: %v", baseURL, err)
			continue
		}

		resp, err := s.httpClient.Do(req)
		if err != nil {
			cancel()
			lastErr = err.Error()
			log.Printf("search proxy http error base=%s: %v", baseURL, err)
			continue
		}
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
		_ = resp.Body.Close()
		cancel()

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			ct := strings.TrimSpace(resp.Header.Get("Content-Type"))
			if ct == "" {
				ct = "application/json; charset=utf-8"
			}
			w.Header().Set("Content-Type", ct)
			w.WriteHeader(resp.StatusCode)
			_, _ = w.Write(body)
			return
		}

		// For client errors, propagate as-is (likely a bad query).
		if resp.StatusCode >= 400 && resp.StatusCode < 500 && resp.StatusCode != http.StatusNotFound {
			ct := strings.TrimSpace(resp.Header.Get("Content-Type"))
			if ct == "" {
				ct = "application/json; charset=utf-8"
			}
			w.Header().Set("Content-Type", ct)
			w.WriteHeader(resp.StatusCode)
			_, _ = w.Write(body)
			return
		}

		msg := strings.TrimSpace(string(body))
		if msg == "" {
			msg = resp.Status
		}
		lastErr = msg
		log.Printf("search proxy failed base=%s status=%d: %s", baseURL, resp.StatusCode, msg)
	}

	if lastErr == "" {
		lastErr = "all trusted indexers failed"
	}
	writeError(w, http.StatusBadGateway, "search proxy failed: "+lastErr)
}

func pubFromPrivKey(privKeyString string) (string, error) {
	priv, err := signature.ParsePrivateKey(privKeyString)
	if err != nil {
		return "", err
	}
	pub, err := signature.PublicKeyFromPrivate(priv)
	if err != nil {
		return "", err
	}
	return signature.PublicKeyString(pub), nil
}

func readJSON(w http.ResponseWriter, r *http.Request, out any) error {
	defer r.Body.Close()
	r.Body = http.MaxBytesReader(w, r.Body, 2<<20)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	return dec.Decode(out)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]any{"error": message})
}

func writeText(w http.ResponseWriter, status int, text string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(text))
}

func parseLimitOffset(r *http.Request, defaultLimit, defaultOffset, maxLimit int) (limit, offset int) {
	limit = defaultLimit
	offset = defaultOffset

	if s := r.URL.Query().Get("limit"); s != "" {
		if v, err := strconv.Atoi(s); err == nil && v > 0 {
			limit = v
		}
	}
	if limit > maxLimit {
		limit = maxLimit
	}
	if s := r.URL.Query().Get("offset"); s != "" {
		if v, err := strconv.Atoi(s); err == nil && v >= 0 {
			offset = v
		}
	}
	return limit, offset
}

func applyOffsetLimit[T any](in []T, offset, limit int) []T {
	if offset >= len(in) {
		if in == nil {
			return []T{}
		}
		return in[:0]
	}
	end := offset + limit
	if end > len(in) {
		end = len(in)
	}
	return in[offset:end]
}

func sortThreadsNewestFirst(threads []ThreadItem) {
	sort.Slice(threads, func(i, j int) bool {
		a := threads[i].Thread.CreatedAt
		b := threads[j].Thread.CreatedAt
		if a == b {
			return threads[i].ThreadID > threads[j].ThreadID
		}
		return a > b
	})
}

func strOrEmpty(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

type seenSet struct {
	mu  sync.Mutex
	ttl time.Duration
	max int
	m   map[string]time.Time
}

func newSeenSet(max int, ttl time.Duration) *seenSet {
	if max <= 0 {
		max = 1024
	}
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	return &seenSet{
		ttl: ttl,
		max: max,
		m:   make(map[string]time.Time),
	}
}

func (s *seenSet) Seen(key string) bool {
	key = strings.TrimSpace(key)
	if key == "" {
		return false
	}
	now := time.Now()

	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.m[key]
	if !ok {
		return false
	}
	if now.Sub(t) >= s.ttl {
		delete(s.m, key)
		return false
	}
	return true
}

func (s *seenSet) Mark(key string) {
	key = strings.TrimSpace(key)
	if key == "" {
		return
	}
	now := time.Now()

	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[key] = now
	s.pruneLocked(now)
}

func (s *seenSet) pruneLocked(now time.Time) {
	for k, t := range s.m {
		if now.Sub(t) >= s.ttl {
			delete(s.m, k)
		}
	}
	for len(s.m) > s.max {
		var oldestKey string
		var oldestTime time.Time
		first := true
		for k, t := range s.m {
			if first || t.Before(oldestTime) {
				oldestKey = k
				oldestTime = t
				first = false
			}
		}
		if first {
			break
		}
		delete(s.m, oldestKey)
	}
}
