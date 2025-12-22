package api

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"flex-bbs/backend-go/bbs/config"
	bbsindexer "flex-bbs/backend-go/bbs/indexer"
	bbslog "flex-bbs/backend-go/bbs/log"
	"flex-bbs/backend-go/bbs/signature"
	"flex-bbs/backend-go/bbs/storage"
	"flex-bbs/backend-go/bbs/types"
)

type Server struct {
	Role    string
	Storage *storage.Storage
	Boards  *config.BoardsStore
	Indexer *bbsindexer.Indexer
}

func (s *Server) Handler() http.Handler {
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
	mux.HandleFunc("GET /api/v1/search/posts", s.searchPosts)

	return mux
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

	type th struct {
		ThreadCID   string
		CreatedAt   string
		RootPostCID string
	}
	byThread := make(map[string]th)
	for _, item := range boardLog {
		if !item.ValidSignature {
			continue
		}
		e := item.Value
		if e.BoardID != boardID {
			continue
		}
		if e.Op != types.OpCreateThread {
			continue
		}
		if e.PostCID == nil || *e.PostCID == "" {
			continue
		}
		if _, ok := byThread[e.ThreadID]; ok {
			continue
		}
		byThread[e.ThreadID] = th{ThreadCID: e.ThreadID, CreatedAt: e.CreatedAt, RootPostCID: *e.PostCID}
	}

	threads := make([]ThreadItem, 0, len(byThread))
	for _, x := range byThread {
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
	_, bm, ok := s.loadBoardByID(ctx, boardID)
	if !ok {
		writeError(w, http.StatusNotFound, "board not found")
		return
	}

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

	var rootPostCID string
	for _, item := range boardLog {
		if !item.ValidSignature {
			continue
		}
		e := item.Value
		if e.Op == types.OpCreateThread && e.ThreadID == threadCID && e.PostCID != nil {
			rootPostCID = *e.PostCID
			break
		}
	}

	loadPost := func(ctx context.Context, cid string) (*types.Post, error) {
		return s.Storage.LoadPost(ctx, cid)
	}
	posts, err := bbslog.ReplayThread(ctx, boardLog, threadCID, loadPost, bbslog.VerifyPost, nil)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
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
		writeError(w, http.StatusNotImplemented, "search is available in indexer/full roles")
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
	return newBoardMetaCID, nil
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
		return nil
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
