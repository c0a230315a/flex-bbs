package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// ========================================
// 型定義とインターフェース
// ========================================

// FlexIPFSClient は Flexible-IPFS HTTP API とやり取りするためのインターフェースです。
type FlexIPFSClient interface {
	// PutValueWithAttr は属性とタグ付きで値をDHTに格納します。
	PutValueWithAttr(ctx context.Context, key string, value []byte, attrs map[string]string, tags []string) error

	// GetValue はキーで指定された値をDHTから取得します。
	GetValue(ctx context.Context, key string) (*FlexGetValueResponse, error)

	// PutValue は属性なしで値をDHTに格納します（基本的なDHT put）。
	PutValue(ctx context.Context, key string, value []byte) error

	// FindProviders は指定されたキーのプロバイダーを検索します。
	FindProviders(ctx context.Context, key string) (*FlexFindProvidersResponse, error)

	// Provide はこのノードが指定されたキーを提供できることをアナウンスします。
	Provide(ctx context.Context, key string) error

	// PeerList はDHT内のピアのリストを取得します。
	PeerList(ctx context.Context) (*FlexPeerListResponse, error)

	// BaseURL は Flexible-IPFS API のベースURLを返します。
	BaseURL() string
}

// FlexGetValueResponse は /dht/getvalue からのレスポンスを表します。
type FlexGetValueResponse struct {
	Value []byte            `json:"value"`
	Attrs map[string]string `json:"attrs,omitempty"`
	Tags  []string          `json:"tags,omitempty"`
	Type  string            `json:"type,omitempty"` // デコードされた型情報
}

// FlexFindProvidersResponse は /dht/findprovs からのレスポンスを表します。
type FlexFindProvidersResponse struct {
	Providers []FlexPeer `json:"providers"`
}

// FlexPeer は DHTピアを表します。
type FlexPeer struct {
	ID    string   `json:"id"`
	Addrs []string `json:"addrs,omitempty"`
}

// FlexPeerListResponse は /dht/peerlist からのレスポンスを表します。
type FlexPeerListResponse struct {
	Peers []FlexPeer `json:"peers"`
}

// FlexErrorResponse は Flexible-IPFS API からのエラーレスポンスを表します。
type FlexErrorResponse struct {
	Message string `json:"message"`
	Code    string `json:"code"`
}

func (e *FlexErrorResponse) Error() string {
	if e.Code != "" {
		return fmt.Sprintf("flexipfs error [%s]: %s", e.Code, e.Message)
	}
	return fmt.Sprintf("flexipfs error: %s", e.Message)
}

// FlexClientError はクライアント側のエラーを表します。
type FlexClientError struct {
	Op  string // 操作名
	Err error  // 元となるエラー
}

func (e *FlexClientError) Error() string {
	return fmt.Sprintf("flexipfs client error in %s: %v", e.Op, e.Err)
}

func (e *FlexClientError) Unwrap() error {
	return e.Err
}

// ========================================
// HTTPクライアント実装
// ========================================

// httpFlexIPFSClient は FlexIPFSClient のHTTPベース実装です。
type httpFlexIPFSClient struct {
	baseURL    string
	httpClient *http.Client
}

// NewFlexIPFSClient は新しい Flexible-IPFS HTTPクライアントを作成します。
func NewFlexIPFSClient(baseURL string) FlexIPFSClient {
	return &httpFlexIPFSClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// NewFlexIPFSClientWithHTTPClient はカスタムHTTPクライアントを使用してクライアントを作成します。
func NewFlexIPFSClientWithHTTPClient(baseURL string, httpClient *http.Client) FlexIPFSClient {
	return &httpFlexIPFSClient{
		baseURL:    strings.TrimRight(baseURL, "/"),
		httpClient: httpClient,
	}
}

func (c *httpFlexIPFSClient) BaseURL() string {
	return c.baseURL
}

func (c *httpFlexIPFSClient) PutValueWithAttr(ctx context.Context, key string, value []byte, attrs map[string]string, tags []string) error {
	endpoint := c.baseURL + "/dht/putvaluewithattr"

	// マルチパートフォームを作成
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	// キーを追加
	if err := writer.WriteField("arg", key); err != nil {
		return &FlexClientError{Op: "PutValueWithAttr", Err: err}
	}

	// 値をファイルパートとして追加
	part, err := writer.CreateFormFile("file", "value")
	if err != nil {
		return &FlexClientError{Op: "PutValueWithAttr", Err: err}
	}
	if _, err := part.Write(value); err != nil {
		return &FlexClientError{Op: "PutValueWithAttr", Err: err}
	}

	// 属性を追加
	if len(attrs) > 0 {
		attrsJSON, err := json.Marshal(attrs)
		if err != nil {
			return &FlexClientError{Op: "PutValueWithAttr", Err: fmt.Errorf("marshal attrs: %w", err)}
		}
		if err := writer.WriteField("attrs", string(attrsJSON)); err != nil {
			return &FlexClientError{Op: "PutValueWithAttr", Err: err}
		}
	}

	// タグを追加
	if len(tags) > 0 {
		tagsJSON, err := json.Marshal(tags)
		if err != nil {
			return &FlexClientError{Op: "PutValueWithAttr", Err: fmt.Errorf("marshal tags: %w", err)}
		}
		if err := writer.WriteField("tags", string(tagsJSON)); err != nil {
			return &FlexClientError{Op: "PutValueWithAttr", Err: err}
		}
	}

	if err := writer.Close(); err != nil {
		return &FlexClientError{Op: "PutValueWithAttr", Err: err}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, &buf)
	if err != nil {
		return &FlexClientError{Op: "PutValueWithAttr", Err: err}
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return &FlexClientError{Op: "PutValueWithAttr", Err: err}
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return c.parseErrorResponse(resp, "PutValueWithAttr")
	}

	return nil
}

func (c *httpFlexIPFSClient) GetValue(ctx context.Context, key string) (*FlexGetValueResponse, error) {
	endpoint := c.baseURL + "/dht/getvalue"

	params := url.Values{}
	params.Set("arg", key)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint+"?"+params.Encode(), nil)
	if err != nil {
		return nil, &FlexClientError{Op: "GetValue", Err: err}
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, &FlexClientError{Op: "GetValue", Err: err}
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, c.parseErrorResponse(resp, "GetValue")
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, &FlexClientError{Op: "GetValue", Err: err}
	}

	// まずJSON（attrs/tagsを含む構造化されたレスポンス）としてパースを試みる
	var result FlexGetValueResponse
	if err := json.Unmarshal(body, &result); err == nil && result.Value != nil {
		// 属性から型を推論
		if result.Attrs != nil {
			if t, ok := result.Attrs["type"]; ok {
				result.Type = t
			}
		}
		return &result, nil
	}

	// フォールバック: 生データとして扱う
	return &FlexGetValueResponse{
		Value: body,
	}, nil
}

func (c *httpFlexIPFSClient) PutValue(ctx context.Context, key string, value []byte) error {
	endpoint := c.baseURL + "/dht/putvalue"

	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	if err := writer.WriteField("arg", key); err != nil {
		return &FlexClientError{Op: "PutValue", Err: err}
	}

	part, err := writer.CreateFormFile("file", "value")
	if err != nil {
		return &FlexClientError{Op: "PutValue", Err: err}
	}
	if _, err := part.Write(value); err != nil {
		return &FlexClientError{Op: "PutValue", Err: err}
	}

	if err := writer.Close(); err != nil {
		return &FlexClientError{Op: "PutValue", Err: err}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, &buf)
	if err != nil {
		return &FlexClientError{Op: "PutValue", Err: err}
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return &FlexClientError{Op: "PutValue", Err: err}
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return c.parseErrorResponse(resp, "PutValue")
	}

	return nil
}

func (c *httpFlexIPFSClient) FindProviders(ctx context.Context, key string) (*FlexFindProvidersResponse, error) {
	endpoint := c.baseURL + "/dht/findprovs"

	params := url.Values{}
	params.Set("arg", key)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint+"?"+params.Encode(), nil)
	if err != nil {
		return nil, &FlexClientError{Op: "FindProviders", Err: err}
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, &FlexClientError{Op: "FindProviders", Err: err}
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, c.parseErrorResponse(resp, "FindProviders")
	}

	var result FlexFindProvidersResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, &FlexClientError{Op: "FindProviders", Err: err}
	}

	return &result, nil
}

func (c *httpFlexIPFSClient) Provide(ctx context.Context, key string) error {
	endpoint := c.baseURL + "/dht/provide"

	params := url.Values{}
	params.Set("arg", key)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint+"?"+params.Encode(), nil)
	if err != nil {
		return &FlexClientError{Op: "Provide", Err: err}
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return &FlexClientError{Op: "Provide", Err: err}
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return c.parseErrorResponse(resp, "Provide")
	}

	return nil
}

func (c *httpFlexIPFSClient) PeerList(ctx context.Context) (*FlexPeerListResponse, error) {
	endpoint := c.baseURL + "/dht/peerlist"

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, nil)
	if err != nil {
		return nil, &FlexClientError{Op: "PeerList", Err: err}
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, &FlexClientError{Op: "PeerList", Err: err}
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, c.parseErrorResponse(resp, "PeerList")
	}

	var result FlexPeerListResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, &FlexClientError{Op: "PeerList", Err: err}
	}

	return &result, nil
}

func (c *httpFlexIPFSClient) parseErrorResponse(resp *http.Response, op string) error {
	body, _ := io.ReadAll(resp.Body)

	var flexErr FlexErrorResponse
	if err := json.Unmarshal(body, &flexErr); err == nil && flexErr.Message != "" {
		return &flexErr
	}

	// 汎用エラーにフォールバック
	return &FlexClientError{
		Op:  op,
		Err: fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body)),
	}
}

// ========================================
// テスト用モッククライアント
// ========================================

// MockFlexIPFSClient はテスト用のモック実装です。
type MockFlexIPFSClient struct {
	mu sync.RWMutex

	baseURL string
	storage map[string]*mockStorageEntry

	// エラーシナリオをテストするためのフック関数
	PutValueWithAttrFunc func(ctx context.Context, key string, value []byte, attrs map[string]string, tags []string) error
	GetValueFunc         func(ctx context.Context, key string) (*FlexGetValueResponse, error)
	PutValueFunc         func(ctx context.Context, key string, value []byte) error
	FindProvidersFunc    func(ctx context.Context, key string) (*FlexFindProvidersResponse, error)
	ProvideFunc          func(ctx context.Context, key string) error
	PeerListFunc         func(ctx context.Context) (*FlexPeerListResponse, error)
}

type mockStorageEntry struct {
	Value []byte
	Attrs map[string]string
	Tags  []string
}

// NewMockFlexIPFSClient はテスト用の新しいモッククライアントを作成します。
func NewMockFlexIPFSClient(baseURL string) *MockFlexIPFSClient {
	return &MockFlexIPFSClient{
		baseURL: baseURL,
		storage: make(map[string]*mockStorageEntry),
	}
}

func (m *MockFlexIPFSClient) BaseURL() string {
	return m.baseURL
}

func (m *MockFlexIPFSClient) PutValueWithAttr(ctx context.Context, key string, value []byte, attrs map[string]string, tags []string) error {
	if m.PutValueWithAttrFunc != nil {
		return m.PutValueWithAttrFunc(ctx, key, value, attrs, tags)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// 外部からの変更を避けるためにディープコピー
	attrsCopy := make(map[string]string)
	for k, v := range attrs {
		attrsCopy[k] = v
	}

	tagsCopy := make([]string, len(tags))
	copy(tagsCopy, tags)

	valueCopy := make([]byte, len(value))
	copy(valueCopy, value)

	m.storage[key] = &mockStorageEntry{
		Value: valueCopy,
		Attrs: attrsCopy,
		Tags:  tagsCopy,
	}

	return nil
}

func (m *MockFlexIPFSClient) GetValue(ctx context.Context, key string) (*FlexGetValueResponse, error) {
	if m.GetValueFunc != nil {
		return m.GetValueFunc(ctx, key)
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	entry, ok := m.storage[key]
	if !ok {
		return nil, &FlexErrorResponse{
			Message: "key not found",
			Code:    "NOT_FOUND",
		}
	}

	// Deep copy to avoid external modifications
	valueCopy := make([]byte, len(entry.Value))
	copy(valueCopy, entry.Value)

	attrsCopy := make(map[string]string)
	for k, v := range entry.Attrs {
		attrsCopy[k] = v
	}

	tagsCopy := make([]string, len(entry.Tags))
	copy(tagsCopy, entry.Tags)

	result := &FlexGetValueResponse{
		Value: valueCopy,
		Attrs: attrsCopy,
		Tags:  tagsCopy,
	}

	// 属性から型を推論
	if t, ok := attrsCopy["type"]; ok {
		result.Type = t
	}

	return result, nil
}

func (m *MockFlexIPFSClient) PutValue(ctx context.Context, key string, value []byte) error {
	if m.PutValueFunc != nil {
		return m.PutValueFunc(ctx, key, value)
	}

	return m.PutValueWithAttr(ctx, key, value, nil, nil)
}

func (m *MockFlexIPFSClient) FindProviders(ctx context.Context, key string) (*FlexFindProvidersResponse, error) {
	if m.FindProvidersFunc != nil {
		return m.FindProvidersFunc(ctx, key)
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	// モック: ストレージにキーが存在する場合、偽のプロバイダーを返す
	if _, ok := m.storage[key]; ok {
		return &FlexFindProvidersResponse{
			Providers: []FlexPeer{
				{
					ID:    "mock-peer-123",
					Addrs: []string{"/ip4/127.0.0.1/tcp/5001"},
				},
			},
		}, nil
	}

	return &FlexFindProvidersResponse{
		Providers: []FlexPeer{},
	}, nil
}

func (m *MockFlexIPFSClient) Provide(ctx context.Context, key string) error {
	if m.ProvideFunc != nil {
		return m.ProvideFunc(ctx, key)
	}

	// モック: provideは何もしない
	return nil
}

func (m *MockFlexIPFSClient) PeerList(ctx context.Context) (*FlexPeerListResponse, error) {
	if m.PeerListFunc != nil {
		return m.PeerListFunc(ctx)
	}

	// モック: 偽のピアリストを返す
	return &FlexPeerListResponse{
		Peers: []FlexPeer{
			{
				ID:    "mock-peer-1",
				Addrs: []string{"/ip4/127.0.0.1/tcp/5001"},
			},
			{
				ID:    "mock-peer-2",
				Addrs: []string{"/ip4/127.0.0.1/tcp/5002"},
			},
		},
	}, nil
}

// GetStorageKeys はモックストレージ内の全てのキーを返します（テスト用）。
func (m *MockFlexIPFSClient) GetStorageKeys() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	keys := make([]string, 0, len(m.storage))
	for k := range m.storage {
		keys = append(keys, k)
	}
	return keys
}

// ClearStorage はモックストレージ内の全てのデータをクリアします（テスト用）。
func (m *MockFlexIPFSClient) ClearStorage() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.storage = make(map[string]*mockStorageEntry)
}

// ========================================
// ヘルパー関数
// ========================================

// InferTypeFromAttrs は属性から "type" フィールドを抽出します。
func InferTypeFromAttrs(attrs map[string]string) string {
	if attrs == nil {
		return ""
	}
	return attrs["type"]
}

// ValidateKey はキーがDHT操作に対して有効かどうかをチェックします。
func ValidateKey(key string) error {
	if key == "" {
		return fmt.Errorf("key cannot be empty")
	}
	if len(key) > 256 {
		return fmt.Errorf("key too long (max 256 characters)")
	}
	return nil
}

// ValidateAttrs は属性が有効かどうかをチェックします。
func ValidateAttrs(attrs map[string]string) error {
	if attrs == nil {
		return nil
	}
	for k, v := range attrs {
		if k == "" {
			return fmt.Errorf("attribute key cannot be empty")
		}
		if len(k) > 128 {
			return fmt.Errorf("attribute key too long: %s", k)
		}
		if len(v) > 1024 {
			return fmt.Errorf("attribute value too long for key: %s", k)
		}
	}
	return nil
}

// ValidateTags はタグが有効かどうかをチェックします。
func ValidateTags(tags []string) error {
	if tags == nil {
		return nil
	}
	for _, tag := range tags {
		if tag == "" {
			return fmt.Errorf("tag cannot be empty")
		}
		if len(tag) > 128 {
			return fmt.Errorf("tag too long: %s", tag)
		}
	}
	return nil
}
