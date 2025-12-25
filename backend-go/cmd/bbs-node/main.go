package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
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
	bbslog "flex-bbs/backend-go/bbs/log"
	"flex-bbs/backend-go/bbs/signature"
	"flex-bbs/backend-go/bbs/storage"
	"flex-bbs/backend-go/bbs/types"
)

var (
	role               = flag.String("role", "client", "node role: client | indexer | archiver | full")
	flexIPFSBase       = flag.String("flexipfs-base-url", "http://127.0.0.1:5001/api/v0", "Flexible-IPFS HTTP API base URL")
	flexIPFSBaseDir    = flag.String("flexipfs-base-dir", "", "path to flexible-ipfs-base (auto-detected if empty)")
	flexIPFSGWEndpoint = flag.String("flexipfs-gw-endpoint", "", "override ipfs.endpoint in flexible-ipfs-base/kadrtt.properties when autostarting (also via env FLEXIPFS_GW_ENDPOINT)")
	flexIPFSMdns       = flag.Bool("flexipfs-mdns", false, "use mDNS on LAN to discover/advertise flex-ipfs gw endpoint")
	flexIPFSMdnsSvc    = flag.String("flexipfs-mdns-service", defaultFlexIPFSMdnsService, "mDNS service type for flex-ipfs gw endpoint (e.g. _flexipfs-gw._tcp)")
	flexIPFSMdnsTO     = flag.Duration("flexipfs-mdns-timeout", defaultFlexIPFSMdnsTimeout, "mDNS discovery timeout")
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
		case "list-trusted-indexers":
			os.Exit(runListTrustedIndexers(os.Args[2:]))
		case "add-trusted-indexer":
			os.Exit(runAddTrustedIndexer(os.Args[2:]))
		case "remove-trusted-indexer":
			os.Exit(runRemoveTrustedIndexer(os.Args[2:]))
		case "sync-trusted-indexers":
			os.Exit(runSyncTrustedIndexers(os.Args[2:]))
		}
	}

	flag.Parse()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	flexGWEndpoint, flexGWExplicit := resolveFlexIPFSGWEndpointWithMdns(ctx, *flexIPFSGWEndpoint, *flexIPFSMdns, *flexIPFSMdnsSvc, *flexIPFSMdnsTO)
	var stopMdns func()
	if flexGWExplicit {
		s, err := maybeAdvertiseFlexIPFSGWEndpointMdns(flexGWEndpoint, *flexIPFSMdns, *flexIPFSMdnsSvc)
		if err != nil {
			log.Printf("flex-ipfs mdns advertise failed: %v", err)
		} else {
			stopMdns = s
		}
	}

	dd := *dataDir
	if dd == "" {
		dd = defaultDataDir()
	}
	logDir := filepath.Join(dd, "logs")

	var flexProc *flexIPFSProc
	if *autoStartFlexIPFS {
		p, err := maybeStartFlexIPFS(ctx, *flexIPFSBase, *flexIPFSBaseDir, flexGWEndpoint, logDir)
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

	if stopMdns != nil {
		defer stopMdns()
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

	trustedIndexers := config.NewTrustedIndexersStore(defaultTrustedIndexersPath(dd))
	if err := trustedIndexers.Load(); err != nil {
		log.Printf("trusted indexers load error: %v", err)
	}
	if !flexGWExplicit {
		maybeTrustIndexerFromFlexIPFSGWMdns(ctx, trustedIndexers, flexGWEndpoint)
	}

	flex := flexipfs.New(*flexIPFSBase)
	if isLocalBaseURL(*flexIPFSBase) {
		if baseDir, _, err := resolveFlexDirs(*flexIPFSBaseDir); err == nil && baseDir != "" {
			flex.BaseDir = baseDir
		}
	}
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
		Role:            *role,
		Storage:         st,
		Boards:          boards,
		TrustedIndexers: trustedIndexers,
		Indexer:         ix,
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

func defaultDataDir() string {
	if dir, err := os.UserConfigDir(); err == nil && dir != "" {
		return filepath.Join(dir, "flex-bbs")
	}
	return filepath.Join(".", "data")
}

func defaultTrustedIndexersPath(dataDir string) string {
	return filepath.Join(dataDir, "trusted_indexers.json")
}

func maybeTrustIndexerFromFlexIPFSGWMdns(ctx context.Context, trusted *config.TrustedIndexersStore, gwEndpoint string) {
	if trusted == nil {
		return
	}
	gwEndpoint = strings.TrimSpace(gwEndpoint)
	if gwEndpoint == "" {
		return
	}

	ip := extractIP4FromMultiaddr(gwEndpoint)
	if ip == "" {
		return
	}

	// Convention: bbs-node HTTP is typically exposed on 8080 on the same host as the advertised flex-ipfs gw endpoint.
	// When the gw endpoint was discovered via mDNS, treat that host as a trust anchor automatically.
	baseURL := fmt.Sprintf("http://%s:8080", ip)

	// Best-effort: avoid trusting obviously non-indexer peers, but keep behavior non-fatal.
	role, err := fetchBbsNodeRole(ctx, baseURL)
	if err == nil && role != "indexer" && role != "full" {
		return
	}

	if changed, err := trusted.Add(baseURL); err != nil {
		log.Printf("trusted indexers auto-add failed: %v", err)
	} else if changed {
		log.Printf("trusted indexers: auto-added (mdns gw bootstrap) %s", baseURL)
	}
}

func fetchBbsNodeRole(ctx context.Context, baseURL string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	endpoint := strings.TrimRight(strings.TrimSpace(baseURL), "/") + "/healthz"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", err
	}
	resp, err := (&http.Client{Timeout: 2 * time.Second}).Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	b, err := io.ReadAll(io.LimitReader(resp.Body, 8<<10))
	if err != nil {
		return "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("healthz http %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}

	s := strings.TrimSpace(string(b))
	const prefix = "ok role="
	if !strings.HasPrefix(s, prefix) {
		return "", fmt.Errorf("unexpected healthz response: %q", s)
	}
	return strings.TrimSpace(strings.TrimPrefix(s, prefix)), nil
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
	flexMdns := fs.Bool("flexipfs-mdns", false, "use mDNS on LAN to discover flex-ipfs gw endpoint")
	flexMdnsSvc := fs.String("flexipfs-mdns-service", defaultFlexIPFSMdnsService, "mDNS service type for flex-ipfs gw endpoint (e.g. _flexipfs-gw._tcp)")
	flexMdnsTO := fs.Duration("flexipfs-mdns-timeout", defaultFlexIPFSMdnsTimeout, "mDNS discovery timeout")
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
		flexGW, _ := resolveFlexIPFSGWEndpointWithMdns(ctx, *flexGWEndpoint, *flexMdns, *flexMdnsSvc, *flexMdnsTO)
		p, err := maybeStartFlexIPFS(ctx, *flexBase, *flexBaseDir, flexGW, filepath.Join(data, "logs"))
		if err != nil {
			log.Printf("flex-ipfs autostart failed: %v", err)
		} else {
			flexProc = p
			defer flexProc.stop()
		}
	}

	flex := flexipfs.New(*flexBase)
	if isLocalBaseURL(*flexBase) {
		if baseDir, _, err := resolveFlexDirs(*flexBaseDir); err == nil && baseDir != "" {
			flex.BaseDir = baseDir
		}
	}
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
	flexBase := fs.String("flexipfs-base-url", "http://127.0.0.1:5001/api/v0", "Flexible-IPFS HTTP API base URL")
	flexBaseDir := fs.String("flexipfs-base-dir", "", "path to flexible-ipfs-base (auto-detected if empty)")
	flexGWEndpoint := fs.String("flexipfs-gw-endpoint", "", "override ipfs.endpoint in flexible-ipfs-base/kadrtt.properties when autostarting (also via env FLEXIPFS_GW_ENDPOINT)")
	flexMdns := fs.Bool("flexipfs-mdns", false, "use mDNS on LAN to discover flex-ipfs gw endpoint")
	flexMdnsSvc := fs.String("flexipfs-mdns-service", defaultFlexIPFSMdnsService, "mDNS service type for flex-ipfs gw endpoint (e.g. _flexipfs-gw._tcp)")
	flexMdnsTO := fs.Duration("flexipfs-mdns-timeout", defaultFlexIPFSMdnsTimeout, "mDNS discovery timeout")
	autostartFlexIPFS := fs.Bool("autostart-flexipfs", true, "auto start local Flexible-IPFS if not running")
	dd := fs.String("data-dir", "", "data directory (defaults to OS config dir)")
	bf := fs.String("boards-file", "", "boards.json path (defaults to <data-dir>/boards.json)")
	_ = fs.Parse(args)

	if *boardMetaCID == "" {
		log.Printf("missing required fields: --board-meta-cid")
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

	if strings.TrimSpace(*boardID) == "" {
		if *autostartFlexIPFS {
			flexGW, _ := resolveFlexIPFSGWEndpointWithMdns(ctx, *flexGWEndpoint, *flexMdns, *flexMdnsSvc, *flexMdnsTO)
			p, err := maybeStartFlexIPFS(ctx, *flexBase, *flexBaseDir, flexGW, filepath.Join(data, "logs"))
			if err != nil {
				log.Printf("flex-ipfs autostart failed: %v", err)
			} else {
				flexProc = p
				defer flexProc.stop()
			}
		}

		flex := flexipfs.New(*flexBase)
		if isLocalBaseURL(*flexBase) {
			if baseDir, _, err := resolveFlexDirs(*flexBaseDir); err == nil && baseDir != "" {
				flex.BaseDir = baseDir
			}
		}
		st := storage.New(flex)

		bm, err := st.LoadBoardMeta(ctx, *boardMetaCID)
		if err != nil {
			log.Printf("load boardMeta: %v", err)
			return 1
		}
		if !bbslog.VerifyBoardMeta(bm) {
			log.Printf("invalid boardMeta signature cid=%s boardId=%s", *boardMetaCID, bm.BoardID)
			return 1
		}
		if strings.TrimSpace(bm.BoardID) == "" {
			log.Printf("boardId is empty in boardMeta cid=%s", *boardMetaCID)
			return 1
		}
		*boardID = bm.BoardID
	}

	boards := config.NewBoardsStore(boardsPath)
	_ = boards.Load()
	if err := boards.Upsert(*boardID, *boardMetaCID); err != nil {
		log.Printf("update boards.json: %v", err)
		return 1
	}
	fmt.Printf("ok boardId=%s\n", *boardID)
	return 0
}

func runListTrustedIndexers(args []string) int {
	fs := flag.NewFlagSet("list-trusted-indexers", flag.ExitOnError)
	dd := fs.String("data-dir", "", "data directory (defaults to OS config dir)")
	path := fs.String("trusted-indexers-file", "", "trusted_indexers.json path (defaults to <data-dir>/trusted_indexers.json)")
	_ = fs.Parse(args)

	data := *dd
	if data == "" {
		data = defaultDataDir()
	}
	filePath := *path
	if filePath == "" {
		filePath = defaultTrustedIndexersPath(data)
	}

	s := config.NewTrustedIndexersStore(filePath)
	if err := s.Load(); err != nil {
		log.Printf("trusted indexers load error: %v", err)
		return 1
	}
	b, err := json.Marshal(s.List())
	if err != nil {
		log.Printf("marshal error: %v", err)
		return 1
	}
	fmt.Printf("%s\n", b)
	return 0
}

func runAddTrustedIndexer(args []string) int {
	fs := flag.NewFlagSet("add-trusted-indexer", flag.ExitOnError)
	baseURL := fs.String("base-url", "", "trusted indexer base URL (http://... or https://...)")
	dd := fs.String("data-dir", "", "data directory (defaults to OS config dir)")
	path := fs.String("trusted-indexers-file", "", "trusted_indexers.json path (defaults to <data-dir>/trusted_indexers.json)")
	_ = fs.Parse(args)

	if strings.TrimSpace(*baseURL) == "" {
		log.Printf("missing required fields: --base-url")
		return 2
	}

	data := *dd
	if data == "" {
		data = defaultDataDir()
	}
	filePath := *path
	if filePath == "" {
		filePath = defaultTrustedIndexersPath(data)
	}

	s := config.NewTrustedIndexersStore(filePath)
	if err := s.Load(); err != nil {
		log.Printf("trusted indexers load error: %v", err)
		return 1
	}
	changed, err := s.Add(*baseURL)
	if err != nil {
		log.Printf("add error: %v", err)
		return 1
	}
	if changed {
		fmt.Printf("ok added=%s\n", strings.TrimSpace(*baseURL))
	} else {
		fmt.Printf("ok already-exists=%s\n", strings.TrimSpace(*baseURL))
	}
	return 0
}

func runRemoveTrustedIndexer(args []string) int {
	fs := flag.NewFlagSet("remove-trusted-indexer", flag.ExitOnError)
	baseURL := fs.String("base-url", "", "trusted indexer base URL (http://... or https://...)")
	dd := fs.String("data-dir", "", "data directory (defaults to OS config dir)")
	path := fs.String("trusted-indexers-file", "", "trusted_indexers.json path (defaults to <data-dir>/trusted_indexers.json)")
	_ = fs.Parse(args)

	if strings.TrimSpace(*baseURL) == "" {
		log.Printf("missing required fields: --base-url")
		return 2
	}

	data := *dd
	if data == "" {
		data = defaultDataDir()
	}
	filePath := *path
	if filePath == "" {
		filePath = defaultTrustedIndexersPath(data)
	}

	s := config.NewTrustedIndexersStore(filePath)
	if err := s.Load(); err != nil {
		log.Printf("trusted indexers load error: %v", err)
		return 1
	}
	changed, err := s.Remove(*baseURL)
	if err != nil {
		log.Printf("remove error: %v", err)
		return 1
	}
	if changed {
		fmt.Printf("ok removed=%s\n", strings.TrimSpace(*baseURL))
	} else {
		fmt.Printf("ok not-found=%s\n", strings.TrimSpace(*baseURL))
	}
	return 0
}

func runSyncTrustedIndexers(args []string) int {
	fs := flag.NewFlagSet("sync-trusted-indexers", flag.ExitOnError)
	bootstrapURL := fs.String("bootstrap-url", "", "bootstrap indexer base URL (http://... or https://...)")
	timeout := fs.Duration("timeout", 10*time.Second, "HTTP timeout")
	dd := fs.String("data-dir", "", "data directory (defaults to OS config dir)")
	path := fs.String("trusted-indexers-file", "", "trusted_indexers.json path (defaults to <data-dir>/trusted_indexers.json)")
	_ = fs.Parse(args)

	raw := strings.TrimSpace(*bootstrapURL)
	if raw == "" {
		log.Printf("missing required fields: --bootstrap-url")
		return 2
	}
	normalized, err := config.NormalizeBaseURL(raw)
	if err != nil {
		log.Printf("invalid bootstrap url: %v", err)
		return 2
	}

	data := *dd
	if data == "" {
		data = defaultDataDir()
	}
	filePath := *path
	if filePath == "" {
		filePath = defaultTrustedIndexersPath(data)
	}

	s := config.NewTrustedIndexersStore(filePath)
	if err := s.Load(); err != nil {
		log.Printf("trusted indexers load error: %v", err)
		return 1
	}

	// Always trust the bootstrap itself (the user explicitly chose it).
	_, _ = s.Add(normalized)

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	client := &http.Client{Timeout: *timeout}
	imported, err := fetchTrustedIndexersFromBootstrap(ctx, client, normalized)
	if err != nil {
		log.Printf("bootstrap fetch error: %v", err)
		return 1
	}

	added := 0
	for _, u := range imported {
		if changed, err := s.Add(u); err == nil && changed {
			added++
		}
	}

	out := map[string]any{
		"bootstrap": normalized,
		"imported":  len(imported),
		"added":     added,
		"total":     len(s.List()),
	}
	b, _ := json.Marshal(out)
	fmt.Printf("%s\n", b)
	return 0
}

func fetchTrustedIndexersFromBootstrap(ctx context.Context, client *http.Client, bootstrapBaseURL string) ([]string, error) {
	endpoint := strings.TrimRight(bootstrapBaseURL, "/") + "/api/v1/trusted-indexers"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg := strings.TrimSpace(string(body))
		if msg == "" {
			msg = resp.Status
		}
		return nil, fmt.Errorf("http %d: %s", resp.StatusCode, msg)
	}

	var list []string
	if err := json.Unmarshal(body, &list); err == nil {
		return list, nil
	}

	var obj map[string]any
	if err := json.Unmarshal(body, &obj); err != nil {
		return nil, err
	}
	if v, ok := obj["trustedIndexers"]; ok {
		if arr, ok := v.([]any); ok {
			for _, x := range arr {
				if s, ok := x.(string); ok {
					list = append(list, s)
				}
			}
		}
	}
	return list, nil
}
