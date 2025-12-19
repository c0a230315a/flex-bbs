package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"flex-bbs/backend-go/bbs/api"
	"flex-bbs/backend-go/bbs/archive"
	"flex-bbs/backend-go/bbs/config"
	"flex-bbs/backend-go/bbs/flexipfs"
	"flex-bbs/backend-go/bbs/indexer"
	"flex-bbs/backend-go/bbs/signature"
	"flex-bbs/backend-go/bbs/storage"
	"flex-bbs/backend-go/bbs/types"
)

var (
	role               = flag.String("role", "client", "node role: client | indexer | archiver | full")
	flexIPFSBase       = flag.String("flexipfs-base-url", "http://127.0.0.1:5001/api/v0", "Flexible-IPFS HTTP API base URL")
	flexIPFSBaseDir    = flag.String("flexipfs-base-dir", "", "path to flexible-ipfs-base (auto-detected if empty)")
	flexIPFSGWEndpoint = flag.String("flexipfs-gw-endpoint", "", "override ipfs.endpoint in flexible-ipfs-base/kadrtt.properties when autostarting (also via env FLEXIPFS_GW_ENDPOINT)")
	autoStartFlexIPFS  = flag.Bool("autostart-flexipfs", true, "auto start local Flexible-IPFS if not running")
	httpAddr           = flag.String("http", "127.0.0.1:8080", "HTTP listen address")
	dataDir            = flag.String("data-dir", "", "data directory for boards.json and index db (defaults to OS config dir)")
	boardsFile         = flag.String("boards-file", "", "path to boards.json (defaults to <data-dir>/boards.json)")
	indexDBPath        = flag.String("index-db", "", "path to index sqlite db (defaults to <data-dir>/index.db)")
	indexSyncInterval  = flag.Duration("index-sync-interval", 15*time.Second, "index sync interval (indexer/full)")
	archiveDir         = flag.String("archive-dir", "", "archive directory (archiver/full)")
	archiveInterval    = flag.Duration("archive-interval", 60*time.Second, "archive sync interval (archiver/full)")
)

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "gen-key":
			os.Exit(runGenKey(os.Args[2:]))
		case "init-board":
			os.Exit(runInitBoard(os.Args[2:]))
		case "add-board":
			os.Exit(runAddBoard(os.Args[2:]))
		}
	}

	flag.Parse()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var flexProc *flexIPFSProc
	if *autoStartFlexIPFS {
		p, err := maybeStartFlexIPFS(ctx, *flexIPFSBase, *flexIPFSBaseDir, resolveFlexIPFSGWEndpoint(*flexIPFSGWEndpoint))
		if err != nil {
			log.Printf("flex-ipfs autostart failed: %v", err)
		} else {
			flexProc = p
		}
	}

	// Shutdown hook to stop child processes.
	sigCh := make(chan os.Signal, 1)
	signals := []os.Signal{os.Interrupt}
	if runtime.GOOS != "windows" {
		signals = append(signals, syscall.SIGTERM)
	}
	signal.Notify(sigCh, signals...)
	go func() {
		<-sigCh
		log.Printf("signal received, shutting down")
		cancel()
	}()

	dd := *dataDir
	if dd == "" {
		dd = defaultDataDir()
	}
	bf := *boardsFile
	if bf == "" {
		bf = filepath.Join(dd, "boards.json")
	}
	dbPath := *indexDBPath
	if dbPath == "" {
		dbPath = filepath.Join(dd, "index.db")
	}
	ad := *archiveDir
	if ad == "" {
		ad = filepath.Join(dd, "archive")
	}
	boards := config.NewBoardsStore(bf)
	if err := boards.Load(); err != nil {
		log.Fatalf("boards load error: %v", err)
	}

	flex := flexipfs.New(*flexIPFSBase)
	st := storage.New(flex)

	var ix *indexer.Indexer
	if *role == "indexer" || *role == "full" {
		var err error
		ix, err = indexer.Open(dbPath, st)
		if err != nil {
			log.Fatalf("indexer open error: %v", err)
		}
		go runIndexSyncLoop(ctx, ix, boards, *indexSyncInterval)
	}
	if *role == "archiver" || *role == "full" {
		a := &archive.Archiver{Storage: st, Boards: boards, Dir: ad}
		go runArchiveLoop(ctx, a, *archiveInterval)
	}

	srv := &api.Server{
		Role:    *role,
		Storage: st,
		Boards:  boards,
		Indexer: ix,
	}
	httpServer := &http.Server{
		Addr:    *httpAddr,
		Handler: srv.Handler(),
	}

	log.Printf("bbs-node starting role=%s http=%s flexipfs=%s", *role, *httpAddr, *flexIPFSBase)

	go func() {
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("http server error: %v", err)
			cancel()
		}
	}()

	<-ctx.Done()
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	_ = httpServer.Shutdown(shutdownCtx)
	if flexProc != nil {
		flexProc.stop()
	}
}

func resolveFlexIPFSGWEndpoint(flagValue string) string {
	if v := strings.TrimSpace(flagValue); v != "" {
		return v
	}
	if v := strings.TrimSpace(os.Getenv("FLEXIPFS_GW_ENDPOINT")); v != "" {
		return v
	}
	return ""
}

func defaultDataDir() string {
	if dir, err := os.UserConfigDir(); err == nil && dir != "" {
		return filepath.Join(dir, "flex-bbs")
	}
	return filepath.Join(".", "data")
}

func runGenKey(args []string) int {
	fs := flag.NewFlagSet("gen-key", flag.ExitOnError)
	_ = fs.Parse(args)

	pub, priv, err := signature.GenerateKeyPair()
	if err != nil {
		log.Printf("GenerateKeyPair: %v", err)
		return 1
	}
	fmt.Printf("{\"pub\":%q,\"priv\":%q}\n", pub, priv)
	return 0
}

func runIndexSyncLoop(ctx context.Context, ix *indexer.Indexer, boards *config.BoardsStore, interval time.Duration) {
	if interval <= 0 {
		interval = 15 * time.Second
	}
	t := time.NewTicker(interval)
	defer t.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			_ = boards.Load()
			for _, ref := range boards.List() {
				_ = ix.SyncBoardByMetaCID(ctx, ref.BoardMetaCID)
			}
		}
	}
}

func runArchiveLoop(ctx context.Context, a *archive.Archiver, interval time.Duration) {
	if interval <= 0 {
		interval = 60 * time.Second
	}
	t := time.NewTicker(interval)
	defer t.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			_ = a.SyncOnce(ctx)
		}
	}
}

func runInitBoard(args []string) int {
	fs := flag.NewFlagSet("init-board", flag.ExitOnError)
	boardID := fs.String("board-id", "", "board ID (e.g. bbs.general)")
	title := fs.String("title", "", "board title")
	description := fs.String("description", "", "board description")
	authorPrivKey := fs.String("author-priv-key", "", "author private key (ed25519:...)")
	flexBase := fs.String("flexipfs-base-url", "http://127.0.0.1:5001/api/v0", "Flexible-IPFS HTTP API base URL")
	flexBaseDir := fs.String("flexipfs-base-dir", "", "path to flexible-ipfs-base (auto-detected if empty)")
	flexGWEndpoint := fs.String("flexipfs-gw-endpoint", "", "override ipfs.endpoint in flexible-ipfs-base/kadrtt.properties when autostarting (also via env FLEXIPFS_GW_ENDPOINT)")
	autostartFlexIPFS := fs.Bool("autostart-flexipfs", true, "auto start local Flexible-IPFS if not running")
	dd := fs.String("data-dir", "", "data directory (defaults to OS config dir)")
	bf := fs.String("boards-file", "", "boards.json path (defaults to <data-dir>/boards.json)")
	_ = fs.Parse(args)

	if *boardID == "" || *title == "" || *authorPrivKey == "" {
		log.Printf("missing required fields: --board-id --title --author-priv-key")
		return 2
	}

	data := *dd
	if data == "" {
		data = defaultDataDir()
	}
	boardsPath := *bf
	if boardsPath == "" {
		boardsPath = filepath.Join(data, "boards.json")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var flexProc *flexIPFSProc
	if *autostartFlexIPFS {
		p, err := maybeStartFlexIPFS(ctx, *flexBase, *flexBaseDir, resolveFlexIPFSGWEndpoint(*flexGWEndpoint))
		if err != nil {
			log.Printf("flex-ipfs autostart failed: %v", err)
		} else {
			flexProc = p
			defer flexProc.stop()
		}
	}

	flex := flexipfs.New(*flexBase)
	st := storage.New(flex)
	boards := config.NewBoardsStore(boardsPath)
	_ = boards.Load()

	bm := &types.BoardMeta{
		Version:     1,
		Type:        types.TypeBoardMeta,
		BoardID:     *boardID,
		Title:       *title,
		Description: *description,
		LogHeadCID:  nil,
		CreatedAt:   types.NowUTC(),
		CreatedBy:   "",
		Signature:   "",
	}
	if err := signature.SignBoardMeta(*authorPrivKey, bm); err != nil {
		log.Printf("sign boardMeta: %v", err)
		return 1
	}
	cid, err := st.SaveBoardMeta(ctx, bm)
	if err != nil {
		log.Printf("save boardMeta: %v", err)
		return 1
	}
	if err := boards.Upsert(*boardID, cid); err != nil {
		log.Printf("update boards.json: %v", err)
		return 1
	}
	fmt.Printf("%s\n", cid)
	return 0
}

func runAddBoard(args []string) int {
	fs := flag.NewFlagSet("add-board", flag.ExitOnError)
	boardID := fs.String("board-id", "", "board ID (e.g. bbs.general)")
	boardMetaCID := fs.String("board-meta-cid", "", "BoardMeta CID")
	dd := fs.String("data-dir", "", "data directory (defaults to OS config dir)")
	bf := fs.String("boards-file", "", "boards.json path (defaults to <data-dir>/boards.json)")
	_ = fs.Parse(args)

	if *boardID == "" || *boardMetaCID == "" {
		log.Printf("missing required fields: --board-id --board-meta-cid")
		return 2
	}
	data := *dd
	if data == "" {
		data = defaultDataDir()
	}
	boardsPath := *bf
	if boardsPath == "" {
		boardsPath = filepath.Join(data, "boards.json")
	}
	boards := config.NewBoardsStore(boardsPath)
	_ = boards.Load()
	if err := boards.Upsert(*boardID, *boardMetaCID); err != nil {
		log.Printf("update boards.json: %v", err)
		return 1
	}
	fmt.Printf("ok\n")
	return 0
}
