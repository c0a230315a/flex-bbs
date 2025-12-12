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
)

var (
	role             = flag.String("role", "client", "node role: client | indexer | archiver | full")
	flexIPFSBase     = flag.String("flexipfs-base-url", "http://127.0.0.1:5001/api/v0", "Flexible-IPFS HTTP API base URL")
	flexIPFSBaseDir  = flag.String("flexipfs-base-dir", "", "path to flexible-ipfs-base (auto-detected if empty)")
	autoStartFlexIPFS = flag.Bool("autostart-flexipfs", true, "auto start local Flexible-IPFS if not running")
	httpAddr         = flag.String("http", "127.0.0.1:8080", "HTTP listen address")
)

func main() {
	flag.Parse()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var flexProc *flexIPFSProc
	if *autoStartFlexIPFS {
		p, err := maybeStartFlexIPFS(ctx, *flexIPFSBase, *flexIPFSBaseDir)
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
		if flexProc != nil {
			flexProc.stop()
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
