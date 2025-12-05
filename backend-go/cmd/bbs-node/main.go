package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
)

var (
	role         = flag.String("role", "client", "node role: client | indexer | archiver | full")
	flexIPFSBase = flag.String("flexipfs-base-url", "http://127.0.0.1:5001/api/v0", "Flexible-IPFS HTTP API base URL")
	httpAddr     = flag.String("http", "127.0.0.1:8080", "HTTP listen address")
)

func main() {
	flag.Parse()

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
