package main

import (
	"encoding/json"
	"log"
	"net/http"
	"time"
)

// --- small helpers (shared within package) ---
//
// このディレクトリ配下のAPIは、当面 `net/http` の DefaultServeMux と
// 直書きのハンドラーで組み立てているため、よく使う処理をここに集約する。
//
// ポイント:
// - エラーは {"error": "...", "code": "..."} の形に統一する
// - Content-Type は application/json; charset=utf-8 を返す
// - createdAt/editedAt の許容フォーマット判定を共通化する

type jsonErrorResponse struct {
	// Error は人間向けの簡易メッセージ。
	// クライアントが表示する想定なので、内部情報を含めすぎないこと。
	Error string `json:"error"`
	// Code は機械判定用の短いエラーコード。
	// 例: invalid_json / invalid_request / invalid_signature / not_found
	Code  string `json:"code"`
}

// writeJSON は JSON レスポンスを書き込む。
// エンコード失敗時はログのみ出してハンドラーは落とさない。
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("error encoding json: %v", err)
	}
}

// writeJSONError は統一形式のエラーレスポンスを書き込む。
func writeJSONError(w http.ResponseWriter, status int, code string, message string) {
	writeJSON(w, status, jsonErrorResponse{Error: message, Code: code})
}

// isRFC3339OrNano は RFC3339 / RFC3339Nano のどちらかで parse できるかを判定する。
//
// NOTE:
// - 仕様が固まったら、許容フォーマットやタイムゾーン要件をここで一括調整できるようにしている。
func isRFC3339OrNano(s string) bool {
	if _, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return true
	}
	if _, err := time.Parse(time.RFC3339, s); err == nil {
		return true
	}
	return false
}
