package flexipfs

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

type Client struct {
	BaseURL    string
	BaseDir    string
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

func validateAttr(attr string) error {
	attr = strings.TrimSpace(attr)
	if attr == "" {
		return fmt.Errorf("attr is empty")
	}
	i := strings.IndexByte(attr, '_')
	if i <= 0 || i >= len(attr)-1 {
		return fmt.Errorf("attr %q must be name_value", attr)
	}
	if strings.Contains(attr[i+1:], "_") {
		return fmt.Errorf("attr %q must contain a single '_' separator", attr)
	}
	if _, err := strconv.Atoi(attr[i+1:]); err != nil {
		return fmt.Errorf("attr %q value must be an integer: %w", attr, err)
	}
	return nil
}

func validateTag(tag string) error {
	tag = strings.TrimSpace(tag)
	if tag == "" {
		return fmt.Errorf("tag is empty")
	}
	i := strings.IndexByte(tag, '_')
	if i <= 0 || i >= len(tag)-1 {
		return fmt.Errorf("tag %q must be name_value", tag)
	}
	return nil
}

func (c *Client) PutValueWithAttr(ctx context.Context, value string, attrs, tags []string) (string, error) {
	for _, a := range attrs {
		if err := validateAttr(a); err != nil {
			return "", err
		}
	}
	for _, t := range tags {
		if err := validateTag(t); err != nil {
			return "", err
		}
	}

	// Flexible-IPFS currently crashes on put when its peer list is empty, returning HTTP 400 with no body.
	// Avoid triggering that by waiting briefly for peers and failing if peerlist remains empty.
	peerWaitUntil := time.Now().Add(30 * time.Second)
	if dl, ok := ctx.Deadline(); ok && dl.Before(peerWaitUntil) {
		peerWaitUntil = dl
	}

	var lastPeerErr error
	for attempt := 0; ; attempt++ {
		peers, perr := c.PeerList(ctx)
		if perr != nil {
			lastPeerErr = perr
		} else if strings.TrimSpace(peers) != "" {
			break
		}

		if time.Now().After(peerWaitUntil) {
			if lastPeerErr != nil {
				return "", lastPeerErr
			}
			return "", fmt.Errorf("flexipfs has no peers (peerlist is empty). Configure a gw endpoint via FLEXIPFS_GW_ENDPOINT / --flexipfs-gw-endpoint or enable --flexipfs-mdns")
		}

		sleep := time.Duration(200*(attempt+1)) * time.Millisecond
		if sleep > time.Second {
			sleep = time.Second
		}
		select {
		case <-ctx.Done():
			if lastPeerErr != nil {
				return "", fmt.Errorf("flexipfs peerlist aborted: %w", lastPeerErr)
			}
			return "", ctx.Err()
		case <-time.After(sleep):
		}
	}

	q := url.Values{}
	q.Set("value", value)
	if len(attrs) > 0 {
		q.Set("attrs", strings.Join(attrs, ","))
	}
	if len(tags) > 0 {
		q.Set("tags", strings.Join(tags, ","))
	}

	// Some Flexible-IPFS builds are unstable when `attrs` is set: they may respond with a 400
	// (Unknown Multihash type...) or even close the connection (EOF). Prepare a tags-only fallback.
	qNoAttrs := url.Values{}
	qNoAttrs.Set("value", value)
	if len(tags) > 0 {
		qNoAttrs.Set("tags", strings.Join(tags, ","))
	}
	useAttrs := len(attrs) > 0
	var lastPutErr error

	retryUntil := time.Now().Add(30 * time.Second)
	if dl, ok := ctx.Deadline(); ok && dl.Before(retryUntil) {
		retryUntil = dl
	}

	var lastErr error
	for attempt := 0; ; attempt++ {
		qAttempt := qNoAttrs
		if useAttrs {
			qAttempt = q
		}

		body, status, header, trailer, err := c.postQuery(ctx, "/dht/putvaluewithattr", qAttempt)
		if err != nil {
			if useAttrs {
				useAttrs = false
				lastPutErr = err
				continue
			}
			if lastPutErr != nil {
				return "", fmt.Errorf("%v (fallback without attrs also failed: %w)", lastPutErr, err)
			}
			return "", err
		}
		if status >= 200 && status < 300 {
			return extractCID(body)
		}

		httpErr := httpError(status, body, header, trailer)

		// Some Flexible-IPFS builds fail any put that includes attrs due to a manager lookup bug.
		// Fall back to tags-only so core flows can still function without attribute indexing.
		if status == http.StatusBadRequest && useAttrs && strings.Contains(strings.ToLower(httpErr.Error()), "unknown multihash type") {
			useAttrs = false
			lastErr = httpErr
			continue
		}

		if status == http.StatusBadRequest && len(bytes.TrimSpace(body)) == 0 {
			// Flexible-IPFS can return HTTP 400 with an empty body when it's still bootstrapping.
			// It's also known to crash on put when its peer list is empty; fail fast in that case.
			if peers, perr := c.PeerList(ctx); perr == nil && strings.TrimSpace(peers) == "" {
				return "", fmt.Errorf("flexipfs put failed: no peers connected (peerlist is empty). Configure a gw endpoint via FLEXIPFS_GW_ENDPOINT / --flexipfs-gw-endpoint or enable --flexipfs-mdns: %w", httpErr)
			}

			// Connection failures are unlikely to resolve by retrying the PUT loop; fail fast so callers can
			// surface a clearer configuration/network error (e.g. bad gw endpoint or blocked TCP/4001).
			lower := strings.ToLower(httpErr.Error())
			if strings.Contains(lower, "connection refused") || strings.Contains(lower, "connectexception") || strings.Contains(lower, "timed out") {
				return "", fmt.Errorf("flexipfs put failed: peer connection error (check FLEXIPFS_GW_ENDPOINT / --flexipfs-gw-endpoint and firewall): %w", httpErr)
			}

			lastErr = fmt.Errorf("flexipfs put failed: http 400 with empty body (check flex-ipfs.log): %w", httpErr)
			if time.Now().After(retryUntil) {
				return "", fmt.Errorf("flexipfs put failed after %d attempts: %w", attempt+1, lastErr)
			}

			// Backoff (fast in tests, capped in production).
			sleep := time.Duration(50*(attempt+1)) * time.Millisecond
			if sleep > time.Second {
				sleep = time.Second
			}
			select {
			case <-ctx.Done():
				if lastErr != nil {
					return "", fmt.Errorf("flexipfs put aborted: %w", lastErr)
				}
				return "", ctx.Err()
			case <-time.After(sleep):
			}
			continue
		}

		return "", httpErr
	}
}

func (c *Client) GetValue(ctx context.Context, cid string) ([]byte, error) {
	if b, err := c.readGetDataValue(cid); err == nil {
		return b, nil
	}

	q := url.Values{}
	q.Set("cid", cid)
	body, status, header, trailer, err := c.postQuery(ctx, "/dht/getvalue", q)
	if err != nil {
		return nil, err
	}
	if status < 200 || status >= 300 {
		return nil, httpError(status, body, header, trailer)
	}

	v := unwrapValue(body)
	if isDownloadingPlaceholder(v, cid) {
		// Flexible-IPFS returns a placeholder string and downloads content to <baseDir>/getdata/<cid>.txt asynchronously.
		// Prefer the local file when we have access to the base directory.
		pollUntil := time.Now().Add(2 * time.Second)
		if dl, ok := ctx.Deadline(); ok && dl.Before(pollUntil) {
			pollUntil = dl
		}
		for {
			if b, err := c.readGetDataValue(cid); err == nil {
				return b, nil
			}
			if time.Now().After(pollUntil) {
				break
			}
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(50 * time.Millisecond):
			}
		}
		return nil, fmt.Errorf("flexipfs getvalue pending: %s", strings.TrimSpace(string(v)))
	}

	return v, nil
}

func (c *Client) GetByAttrs(ctx context.Context, attrs, tags []string, showAll bool) ([]string, error) {
	for _, a := range attrs {
		if err := validateAttr(a); err != nil {
			return nil, err
		}
	}
	for _, t := range tags {
		if err := validateTag(t); err != nil {
			return nil, err
		}
	}

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
		encoded := q.Encode()
		// Flexible-IPFS parses its query string via java.net.URI.getQuery() (percent-decoded but '+' preserved).
		// Encode spaces as %20 instead of '+' so stored values round-trip correctly.
		if strings.Contains(encoded, "+") {
			encoded = strings.ReplaceAll(encoded, "+", "%20")
		}
		fullURL += "?" + encoded
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, fullURL, nil)
	if err != nil {
		return nil, 0, nil, nil, err
	}
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, 0, nil, nil, fmt.Errorf("flexipfs POST %s: %w", fullURL, err)
	}
	defer resp.Body.Close()

	b, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return nil, resp.StatusCode, nil, nil, fmt.Errorf("flexipfs read response %s: %w", fullURL, readErr)
	}
	return b, resp.StatusCode, resp.Header.Clone(), resp.Trailer, nil
}

func httpError(status int, body []byte, header http.Header, trailer http.Header) error {
	msg := strings.TrimSpace(string(body))
	if msg == "" && header != nil {
		// Flexible-IPFS sometimes reports errors in the *Trailer header value* (not the trailer section).
		// Example: Trailer: Attribute+info.+should+be%3A+name_value
		for _, raw := range header.Values("Trailer") {
			v := strings.TrimSpace(raw)
			if v == "" {
				continue
			}
			if decoded, err := url.QueryUnescape(v); err == nil && strings.TrimSpace(decoded) != "" {
				v = decoded
			}
			msg = strings.TrimSpace(v)
			break
		}
	}
	if msg == "" && len(trailer) > 0 {
		// Go may parse the (misused) "Trailer" header value as *trailer key names* and drop the header,
		// leaving `trailer` with keys but no values. In that case, treat the keys as the message.
		var parts []string
		var keys []string
		for k, vals := range trailer {
			if len(vals) == 0 {
				keys = append(keys, k)
				continue
			}
			for _, v := range vals {
				if decoded, err := url.QueryUnescape(v); err == nil && strings.TrimSpace(decoded) != "" {
					v = decoded
				}
				parts = append(parts, fmt.Sprintf("%s: %s", k, v))
			}
		}
		if len(parts) > 0 {
			sort.Strings(parts)
			msg = strings.Join(parts, "; ")
		} else if len(keys) > 0 {
			sort.Strings(keys)
			for i := range keys {
				if decoded, err := url.QueryUnescape(keys[i]); err == nil && strings.TrimSpace(decoded) != "" {
					keys[i] = decoded
				}
			}
			msg = strings.Join(keys, "; ")
		}
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

func isDownloadingPlaceholder(v []byte, cid string) bool {
	s := strings.TrimSpace(string(v))
	return strings.HasPrefix(s, "Downloading chunks for CID:") && strings.Contains(s, cid)
}

func decodeGetDataValue(b []byte) []byte {
	b = bytes.TrimRight(b, "\r\n")
	if len(b) < 2 {
		return b
	}
	prefix := b[0]
	if prefix >= 'A' && prefix <= 'Z' {
		expected := int(prefix-'A') + 1
		if len(b)-1 == expected {
			return b[1:]
		}
	}
	// Flexible-IPFS uses a 3-byte prefix for larger values:
	//   'Y' + uint16(len(payload)) + payload
	// where the uint16 is big-endian.
	if prefix == 'Y' && len(b) >= 3 {
		expected := int(b[1])<<8 | int(b[2])
		if len(b)-3 == expected {
			return b[3:]
		}
	}
	return b
}

func (c *Client) readGetDataValue(cid string) ([]byte, error) {
	baseDir := strings.TrimSpace(c.BaseDir)
	if baseDir == "" {
		return nil, os.ErrNotExist
	}

	dataDirs := []string{
		filepath.Join(baseDir, "getdata"),
	}
	if v := readKadrttProperty(baseDir, "ipfs.datapath"); v != "" {
		if filepath.IsAbs(v) {
			dataDirs = append(dataDirs, v)
		} else {
			dataDirs = append(dataDirs, filepath.Join(baseDir, v))
		}
	}

	var firstErr error
	for _, dir := range uniqStrings(dataDirs) {
		p := filepath.Join(dir, cid+".txt")
		b, err := os.ReadFile(p)
		if err == nil {
			b = decodeGetDataValue(b)
			if len(b) != 0 {
				return b, nil
			}
			continue
		}
		if firstErr == nil && !errors.Is(err, os.ErrNotExist) {
			firstErr = err
		}

		// Fall back to other extensions (e.g., files uploaded via `file=...`), but never the
		// raw `<cid>` file (that's the serialized MerkleDAG metadata, not the stored value).
		matches, globErr := filepath.Glob(filepath.Join(dir, cid+".*"))
		if globErr != nil {
			if firstErr == nil {
				firstErr = globErr
			}
			continue
		}
		sort.Strings(matches)
		for _, p := range matches {
			if strings.HasSuffix(p, ".txt") {
				continue
			}
			b, err := os.ReadFile(p)
			if err != nil {
				if firstErr == nil && !errors.Is(err, os.ErrNotExist) {
					firstErr = err
				}
				continue
			}
			b = decodeGetDataValue(b)
			if len(b) == 0 {
				continue
			}
			return b, nil
		}
	}
	if firstErr != nil {
		return nil, firstErr
	}
	return nil, os.ErrNotExist
}

func readKadrttProperty(baseDir, key string) string {
	propsPath := filepath.Join(baseDir, "kadrtt.properties")
	b, err := os.ReadFile(propsPath)
	if err != nil {
		return ""
	}
	for _, raw := range strings.Split(string(b), "\n") {
		line := strings.TrimSpace(strings.TrimSuffix(raw, "\r"))
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "!") {
			continue
		}
		if !strings.HasPrefix(line, key) {
			continue
		}
		i := strings.IndexAny(line, "=:")
		if i < 0 {
			continue
		}
		return strings.TrimSpace(line[i+1:])
	}
	return ""
}

func uniqStrings(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}
