package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// BoardMeta は仕様書で定義された BoardMeta JSON を表します。
type BoardMeta struct {
	Version    int       `json:"version"`
	Type       string    `json:"type"`
	BoardID    string    `json:"boardId"`
	Title      string    `json:"title"`
	Description string   `json:"description"`
	LogHeadCID string    `json:"logHeadCid,omitempty"`
	CreatedAt  time.Time `json:"createdAt"`
	CreatedBy  string    `json:"createdBy"`
	Signature  string    `json:"signature,omitempty"`
}

// boardMetaConfigFile は設定ファイルの JSON 形式です。
type boardMetaConfigFile struct {
	Boards []BoardMeta `json:"boards"`
}

// loadBoardMetaConfig は設定ファイルから BoardMeta 一覧を読み込みます。
// path が空の場合は組み込みのデフォルト一覧を返します。
func loadBoardMetaConfig(path string) ([]BoardMeta, error) {
	if path == "" {
		return defaultBoardMetas(), nil
	}

	resolved := path
	if !filepath.IsAbs(path) {
		// 実行ファイルのディレクトリ基準に解決
		exe, err := os.Executable()
		if err == nil {
			resolved = filepath.Join(filepath.Dir(exe), path)
		}
	}

	f, err := os.Open(resolved)
	if err != nil {
		// 設定ファイルが見つからない場合は警告してデフォルトにフォールバック
		log.Printf("boards config not found (%s), using defaults: %v", resolved, err)
		return defaultBoardMetas(), nil
	}
	defer f.Close()

	var cfg boardMetaConfigFile
	if err := json.NewDecoder(f).Decode(&cfg); err != nil {
		log.Printf("failed to parse boards config (%s), using defaults: %v", resolved, err)
		return defaultBoardMetas(), nil
	}

	if len(cfg.Boards) == 0 {
		log.Printf("boards config (%s) has no boards, using defaults", resolved)
		return defaultBoardMetas(), nil
	}
	return cfg.Boards, nil
}

// defaultBoardMetas は外部配布なしでも最低限動作するための組み込み板一覧です。
func defaultBoardMetas() []BoardMeta {
	now := time.Now().UTC()
	return []BoardMeta{
		{
			Version:     1,
			Type:        "boardMeta",
			BoardID:     "bbs.general",
			Title:       "雑談板",
			Description: "汎用雑談用の掲示板",
			CreatedAt:   now,
			CreatedBy:   "system",
		},
	}
}

// registerBoardsHTTP は BoardMeta 関連の REST API を登録します。
// - GET /api/v1/boards
// - GET /api/v1/boards/{boardId}
func registerBoardsHTTP(mux *http.ServeMux, boards []BoardMeta) {
	// インメモリマップを作成
	byID := make(map[string]BoardMeta, len(boards))
	for _, b := range boards {
		byID[b.BoardID] = b
	}

	mux.HandleFunc("/api/v1/boards", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		// 一覧を返す
		resp := struct {
			Boards []BoardMeta `json:"boards"`
		}{Boards: boards}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			log.Printf("failed to write /api/v1/boards response: %v", err)
		}
	})

	mux.HandleFunc("/api/v1/boards/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		boardID := r.URL.Path[len("/api/v1/boards/"):]
		if boardID == "" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		b, ok := byID[boardID]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		if err := json.NewEncoder(w).Encode(b); err != nil {
			log.Printf("failed to write /api/v1/boards/%s response: %v", boardID, err)
		}
	})
}
