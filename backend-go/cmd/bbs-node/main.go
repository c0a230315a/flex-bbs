package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"syscall"

	"flex-bbs/backend-go/internal/indexer"
)

var (
	role             = flag.String("role", "client", "node role: client | indexer | archiver | full")
	flexIPFSBase     = flag.String("flexipfs-base-url", "http://127.0.0.1:5001/api/v0", "Flexible-IPFS HTTP API base URL")
	flexIPFSBaseDir  = flag.String("flexipfs-base-dir", "", "path to flexible-ipfs-base (auto-detected if empty)")
	autoStartFlexIPFS = flag.Bool("autostart-flexipfs", true, "auto start local Flexible-IPFS if not running")
	httpAddr         = flag.String("http", "127.0.0.1:8080", "HTTP listen address")
)

// roleType はノードのロール種別です。
type roleType string

const (
	roleClient  roleType = "client"
	roleIndexer roleType = "indexer"
	roleArchiver roleType = "archiver"
	roleFull    roleType = "full"
)

// roleFeatures はロールごとの機能ON/OFFを表します。
type roleFeatures struct {
	enableClient  bool
	enableIndexer bool
	enableArchiver bool
}

// resolveRole は不正なロール指定を含めてロール種別を決定します。
func resolveRole(raw string) roleType {
	switch raw {
	case string(roleClient), string(roleIndexer), string(roleArchiver), string(roleFull):
		return roleType(raw)
	default:
		log.Printf("unknown role %q, fallback to client", raw)
		return roleClient
	}
}

// featuresForRole はロールから有効化する機能を返します。
func featuresForRole(r roleType) roleFeatures {
	switch r {
	case roleClient:
		return roleFeatures{enableClient: true}
	case roleIndexer:
		return roleFeatures{enableIndexer: true}
	case roleArchiver:
		return roleFeatures{enableArchiver: true}
	case roleFull:
		return roleFeatures{enableClient: true, enableIndexer: true, enableArchiver: true}
	default:
		return roleFeatures{enableClient: true}
	}
}

func main() {
	flag.Parse()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// ロール判定
	r := resolveRole(*role)
	feats := featuresForRole(r)
	log.Printf("node role=%s features: client=%t indexer=%t archiver=%t", r, feats.enableClient, feats.enableIndexer, feats.enableArchiver)

	var flexProc *flexIPFSProc
	if *autoStartFlexIPFS {
		p, err := maybeStartFlexIPFS(ctx, *flexIPFSBase, *flexIPFSBaseDir)
		if err != nil {
			log.Printf("flex-ipfs autostart failed: %v", err)
		} else {
			flexProc = p
		}
	}

	// indexer 機能の初期化
	var indexerDB indexer.DB
	if feats.enableIndexer {
		db, err := indexer.NewSQLiteDB("bbs-indexer.db")
		if err != nil {
			log.Fatalf("failed to init indexer db: %v", err)
		}
		indexerDB = db
		h := indexer.NewAPIHandler(indexerDB)
		h.RegisterRoutes(http.DefaultServeMux)
		log.Printf("indexer API enabled")
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
		if flexProc != nil {
			flexProc.stop()
		}
		if indexerDB != nil {
			_ = indexerDB.Close()
		}
		cancel()
		os.Exit(0)
	}()

	// TODO: 後でちゃんと role ごとの処理を実装する
	http.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		// とりあえず生存確認用
		fmt.Fprintf(w, "ok role=%s", *role)
	})

	log.Printf("bbs-node starting role=%s http=%s flexipfs=%s", *role, *httpAddr, *flexIPFSBase)

	if err := http.ListenAndServe(*httpAddr, nil); err != nil {
		log.Fatalf("http server error: %v", err)
	}
}
