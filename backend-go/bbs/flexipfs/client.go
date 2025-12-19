package flexipfs

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

type Client struct {
	BaseURL    string
	HTTPClient *http.Client
}

func New(baseURL string) *Client {
	return &Client{
		BaseURL: strings.TrimRight(baseURL, "/"),
		HTTPClient: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
}

func (c *Client) PutValueWithAttr(ctx context.Context, value string, attrs, tags []string) (string, error) {
	q := url.Values{}
	q.Set("value", value)
	if len(attrs) > 0 {
		q.Set("attrs", strings.Join(attrs, ","))
	}
	if len(tags) > 0 {
		q.Set("tags", strings.Join(tags, ","))
	}

	body, status, header, trailer, err := c.postQuery(ctx, "/dht/putvaluewithattr", q)
	if err != nil {
		return "", err
	}
	if status < 200 || status >= 300 {
		return "", httpError(status, body, header, trailer)
	}
	return extractCID(body)
}

func (c *Client) GetValue(ctx context.Context, cid string) ([]byte, error) {
	q := url.Values{}
	q.Set("cid", cid)
	body, status, header, trailer, err := c.postQuery(ctx, "/dht/getvalue", q)
	if err != nil {
		return nil, err
	}
	if status < 200 || status >= 300 {
		return nil, httpError(status, body, header, trailer)
	}
	return unwrapValue(body), nil
}

func (c *Client) GetByAttrs(ctx context.Context, attrs, tags []string, showAll bool) ([]string, error) {
	q := url.Values{}
	if len(attrs) > 0 {
		q.Set("attrs", strings.Join(attrs, ","))
	}
	if len(tags) > 0 {
		q.Set("tags", strings.Join(tags, ","))
	}
	if showAll {
		q.Set("showall", "true")
	}
	body, status, header, trailer, err := c.postQuery(ctx, "/dht/getbyattrs", q)
	if err != nil {
		return nil, err
	}
	if status < 200 || status >= 300 {
		return nil, httpError(status, body, header, trailer)
	}
	cids, err := extractCIDList(body)
	if err != nil {
		return nil, err
	}
	sort.Strings(cids)
	return cids, nil
}

func (c *Client) ListAttrs(ctx context.Context) ([]string, error) {
	body, status, header, trailer, err := c.postQuery(ctx, "/dht/listattrs", nil)
	if err != nil {
		return nil, err
	}
	if status < 200 || status >= 300 {
		return nil, httpError(status, body, header, trailer)
	}
	var out []string
	if err := json.Unmarshal(body, &out); err == nil {
		sort.Strings(out)
		return out, nil
	}
	s, err := extractJSONString(body)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(s) == "" {
		return nil, nil
	}
	out = splitCSV(s)
	sort.Strings(out)
	return out, nil
}

func (c *Client) ListTags(ctx context.Context) ([]string, error) {
	body, status, header, trailer, err := c.postQuery(ctx, "/dht/listtags", nil)
	if err != nil {
		return nil, err
	}
	if status < 200 || status >= 300 {
		return nil, httpError(status, body, header, trailer)
	}
	var out []string
	if err := json.Unmarshal(body, &out); err == nil {
		sort.Strings(out)
		return out, nil
	}
	s, err := extractJSONString(body)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(s) == "" {
		return nil, nil
	}
	out = splitCSV(s)
	sort.Strings(out)
	return out, nil
}

func (c *Client) PeerList(ctx context.Context) (string, error) {
	body, status, header, trailer, err := c.postQuery(ctx, "/dht/peerlist", nil)
	if err != nil {
		return "", err
	}
	if status < 200 || status >= 300 {
		return "", httpError(status, body, header, trailer)
	}
	s, err := extractJSONString(body)
	if err == nil {
		return s, nil
	}
	return string(bytes.TrimSpace(body)), nil
}

func (c *Client) postQuery(ctx context.Context, apiPath string, q url.Values) (body []byte, status int, header http.Header, trailer http.Header, err error) {
	fullURL := c.BaseURL + apiPath
	if q != nil {
		fullURL += "?" + q.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, fullURL, nil)
	if err != nil {
		return nil, 0, nil, nil, err
	}
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, 0, nil, nil, err
	}
	defer resp.Body.Close()

	b, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return nil, resp.StatusCode, nil, nil, readErr
	}
	return b, resp.StatusCode, resp.Header.Clone(), resp.Trailer, nil
}

func httpError(status int, body []byte, header http.Header, trailer http.Header) error {
	msg := strings.TrimSpace(string(body))
	if msg == "" && header != nil {
		// Flexible-IPFS sometimes reports errors in the *Trailer header value* (not the trailer section).
		// Example: Trailer: Attribute+info.+should+be%3A+name_value
		if v := strings.TrimSpace(header.Get("Trailer")); v != "" {
			if decoded, err := url.QueryUnescape(v); err == nil && strings.TrimSpace(decoded) != "" {
				v = decoded
			}
			msg = strings.TrimSpace(v)
		}
	}
	if msg == "" && len(trailer) > 0 {
		var parts []string
		for k := range trailer {
			for _, v := range trailer.Values(k) {
				if decoded, err := url.QueryUnescape(v); err == nil && strings.TrimSpace(decoded) != "" {
					v = decoded
				}
				parts = append(parts, fmt.Sprintf("%s: %s", k, v))
			}
		}
		sort.Strings(parts)
		msg = strings.Join(parts, "; ")
	}
	if msg == "" {
		msg = "empty response"
	}
	return fmt.Errorf("flexipfs http %d: %s", status, msg)
}

func extractCID(body []byte) (string, error) {
	body = bytes.TrimSpace(body)
	if len(body) == 0 {
		return "", fmt.Errorf("empty response")
	}
	if body[0] == '"' {
		return extractJSONString(body)
	}
	if body[0] != '{' {
		return string(body), nil
	}
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		return "", err
	}
	if v, ok := m["CID_file"]; ok {
		if s, ok := v.(string); ok && s != "" {
			return s, nil
		}
	}
	for k, v := range m {
		if !strings.Contains(strings.ToLower(k), "cid") {
			continue
		}
		if s, ok := v.(string); ok && s != "" {
			return s, nil
		}
	}
	return "", fmt.Errorf("cid not found in response")
}

func extractCIDList(body []byte) ([]string, error) {
	body = bytes.TrimSpace(body)
	if len(body) == 0 {
		return nil, nil
	}

	var out []string
	if body[0] == '[' {
		if err := json.Unmarshal(body, &out); err == nil {
			return out, nil
		}
		var anyArr []any
		if err := json.Unmarshal(body, &anyArr); err != nil {
			return nil, err
		}
		for _, v := range anyArr {
			if s, ok := v.(string); ok {
				out = append(out, s)
			}
		}
		return out, nil
	}
	if body[0] == '"' {
		s, err := extractJSONString(body)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(s) == "" {
			return nil, nil
		}
		return splitCSV(s), nil
	}
	if body[0] == '{' {
		var obj map[string]any
		if err := json.Unmarshal(body, &obj); err != nil {
			return nil, err
		}
		for _, v := range obj {
			switch vv := v.(type) {
			case []any:
				for _, x := range vv {
					if s, ok := x.(string); ok {
						out = append(out, s)
					}
				}
			case []string:
				out = append(out, vv...)
			case string:
				out = append(out, splitCSV(vv)...)
			}
		}
		return out, nil
	}
	return splitCSV(string(body)), nil
}

func extractJSONString(body []byte) (string, error) {
	var s string
	if err := json.Unmarshal(body, &s); err != nil {
		return "", err
	}
	return s, nil
}

func splitCSV(s string) []string {
	parts := strings.Split(s, ",")
	var out []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		out = append(out, p)
	}
	return out
}

func unwrapValue(body []byte) []byte {
	b := bytes.TrimSpace(body)
	if len(b) == 0 {
		return nil
	}
	if b[0] == '"' {
		if s, err := extractJSONString(b); err == nil {
			return []byte(s)
		}
	}

	// If the response wraps the stored value (unknown schema), try common keys.
	if b[0] == '{' {
		var obj map[string]json.RawMessage
		if err := json.Unmarshal(b, &obj); err == nil {
			for _, k := range []string{"Value", "value", "Data", "data"} {
				if raw, ok := obj[k]; ok {
					raw = bytes.TrimSpace(raw)
					if len(raw) == 0 {
						continue
					}
					if raw[0] == '"' {
						if s, err := extractJSONString(raw); err == nil {
							return []byte(s)
						}
					}
					return raw
				}
			}
		}
	}
	return b
}
