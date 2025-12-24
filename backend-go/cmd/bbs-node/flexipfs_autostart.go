package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
)

type flexIPFSProc struct {
	cmd         *exec.Cmd
	stdinWriter io.Closer
	logFile     *os.File
}

func maybeStartFlexIPFS(ctx context.Context, baseURL, baseDirOverride, gwEndpointOverride, logDir string) (*flexIPFSProc, error) {
	if !isLocalBaseURL(baseURL) {
		log.Printf("flex-ipfs autostart skipped (non-local base url): %s", baseURL)
		return nil, nil
	}

	// If a local Flexible-IPFS is already running on the target base URL,
	// don't start a second instance (avoids port conflicts).
	if isFlexIPFSUp(ctx, baseURL) {
		log.Printf("flex-ipfs already running at %s", baseURL)
		// Best-effort: keep config files in sync even when flex-ipfs is already running.
		// (Changes only take effect on next restart, but this avoids getting stuck with a stale Bootstrap list.)
		if flexBaseDir, _, err := resolveFlexDirs(baseDirOverride); err == nil && flexBaseDir != "" {
			if strings.TrimSpace(gwEndpointOverride) != "" {
				if err := maybeOverrideKadrttGWEndpoint(flexBaseDir, gwEndpointOverride); err != nil {
					return nil, err
				}
			}
			if err := ensureKadrttGlobalIP(flexBaseDir, gwEndpointOverride); err != nil {
				log.Printf("flex-ipfs ensure ipfs.globalip failed: %v", err)
			}
			if err := syncFlexIPFSBootstrapConfig(flexBaseDir, gwEndpointOverride); err != nil {
				log.Printf("flex-ipfs bootstrap config sync failed: %v", err)
			}
		}
		// Best-effort: if we have a gw endpoint configured (or present in kadrtt.properties),
		// proactively connect so peerlist isn't empty for subsequent operations (e.g. Create Board).
		if endpoint := resolveFlexIPFSConnectEndpoint(baseDirOverride, gwEndpointOverride); endpoint != "" {
			if err := flexIPFSSwarmConnect(ctx, baseURL, endpoint); err != nil {
				log.Printf("flex-ipfs swarm/connect failed: %v", err)
			}
			waitForFlexIPFSPeers(ctx, baseURL, 5*time.Second)
		}
		return nil, nil
	}

	flexBaseDir, runtimeDir, err := resolveFlexDirs(baseDirOverride)
	if err != nil {
		return nil, err
	}

	javaBin, err := findJavaBin(runtimeDir)
	if err != nil {
		return nil, err
	}

	// When bbs-client runs subcommands (e.g. `bbs-node init-board`) it starts a second bbs-node process
	// while the managed backend bbs-node may already be starting/stopping Flexible-IPFS. Without
	// coordination, both processes can attempt to start Flexible-IPFS against the same `.ipfs` dir and
	// crash with:
	//   "Database may be already in use: .../.ipfs/datastore/h2.datastore.mv.db"
	// Use a simple cross-process lock file to ensure only one starter at a time.
	lockPath := filepath.Join(flexBaseDir, ".flex-ipfs-start.lock")
	lockDeadline := time.Now().Add(90 * time.Second)
	for {
		// Another process might have started flex-ipfs while we were resolving paths.
		if isFlexIPFSUp(ctx, baseURL) {
			log.Printf("flex-ipfs already running at %s", baseURL)
			// Best-effort: keep config files in sync even when flex-ipfs was started by another process.
			if err := maybeOverrideKadrttGWEndpoint(flexBaseDir, gwEndpointOverride); err != nil {
				return nil, err
			}
			if err := ensureKadrttGlobalIP(flexBaseDir, gwEndpointOverride); err != nil {
				log.Printf("flex-ipfs ensure ipfs.globalip failed: %v", err)
			}
			if err := syncFlexIPFSBootstrapConfig(flexBaseDir, gwEndpointOverride); err != nil {
				log.Printf("flex-ipfs bootstrap config sync failed: %v", err)
			}
			// Best-effort: explicit connect to a configured gw endpoint (if any).
			if endpoint := resolveFlexIPFSConnectEndpoint(flexBaseDir, gwEndpointOverride); endpoint != "" {
				if err := flexIPFSSwarmConnect(ctx, baseURL, endpoint); err != nil {
					log.Printf("flex-ipfs swarm/connect failed: %v", err)
				}
				waitForFlexIPFSPeers(ctx, baseURL, 5*time.Second)
			}
			return nil, nil
		}

		release, ok, err := tryAcquireFlexIPFSStartLock(lockPath)
		if err != nil {
			return nil, err
		}
		if ok {
			// We are the starter.
			proc, err := startFlexIPFS(javaBin, flexBaseDir, gwEndpointOverride, logDir)
			if err != nil {
				release()
				return nil, err
			}
			// Keep the lock held until the HTTP API is up (prevents concurrent start attempts).
			ready := waitForFlexIPFS(ctx, baseURL, 60*time.Second)
			if !ready {
				// If the API never comes up, kill the child process so the `.ipfs` datastore lock
				// is released and a future retry can succeed.
				proc.stop()
				release()
				return nil, fmt.Errorf("flex-ipfs API not ready after 60s (is another flex-ipfs/java process holding .ipfs/datastore?)")
			}
			release()
			// Best-effort explicit connect to a configured gw endpoint (if any).
			if endpoint := resolveFlexIPFSConnectEndpoint(flexBaseDir, gwEndpointOverride); endpoint != "" {
				if err := flexIPFSSwarmConnect(ctx, baseURL, endpoint); err != nil {
					log.Printf("flex-ipfs swarm/connect failed: %v", err)
				}
			}
			// Best-effort wait for bootstrap to populate peers (prevents early put failures).
			waitForFlexIPFSPeers(ctx, baseURL, 5*time.Second)
			return proc, nil
		}

		// Another process is starting flex-ipfs; wait for it to come up.
		if st, statErr := os.Stat(lockPath); statErr == nil {
			// Stale lock cleanup (e.g. a crashed starter).
			if time.Since(st.ModTime()) > 5*time.Minute {
				_ = os.Remove(lockPath)
				continue
			}
		}
		if time.Now().After(lockDeadline) {
			return nil, fmt.Errorf("timeout waiting for flex-ipfs start lock: %s", lockPath)
		}
		time.Sleep(500 * time.Millisecond)
	}
}

func (p *flexIPFSProc) stop() {
	if p == nil || p.cmd == nil || p.cmd.Process == nil {
		return
	}
	_ = p.stdinWriter.Close()
	// Try graceful interrupt first (no-op on Windows), then kill.
	_ = p.cmd.Process.Signal(os.Interrupt)
	time.Sleep(2 * time.Second)
	_ = p.cmd.Process.Kill()
	if p.logFile != nil {
		_ = p.logFile.Close()
		p.logFile = nil
	}
}

func isLocalBaseURL(baseURL string) bool {
	u, err := url.Parse(baseURL)
	if err != nil {
		return true // assume local if unparseable
	}
	host := u.Hostname()
	return host == "" || host == "127.0.0.1" || host == "localhost" || strings.HasPrefix(host, "0.0.0.0")
}

func resolveFlexDirs(baseOverride string) (flexBaseDir, runtimeDir string, err error) {
	if baseOverride != "" {
		flexBaseDir = baseOverride
	} else {
		exePath, _ := os.Executable()
		exeDir := filepath.Dir(exePath)
		candidates := []string{
			filepath.Join(exeDir, "flexible-ipfs-base"),
			filepath.Join(exeDir, "..", "flexible-ipfs-base"),
			filepath.Join(".", "flexible-ipfs-base"),
		}
		for _, c := range candidates {
			if dirExists(c) {
				flexBaseDir = c
				break
			}
		}
	}
	if flexBaseDir == "" {
		return "", "", os.ErrNotExist
	}

	// runtimeDir candidates
	exePath, _ := os.Executable()
	exeDir := filepath.Dir(exePath)
	runtimeCandidates := []string{
		filepath.Join(exeDir, "flexible-ipfs-runtime"),
		filepath.Join(exeDir, "..", "flexible-ipfs-runtime"),
		filepath.Join(flexBaseDir, "..", "flexible-ipfs-runtime"),
	}
	for _, c := range runtimeCandidates {
		if dirExists(c) {
			runtimeDir = c
			break
		}
	}

	// Ensure paths are absolute; autostart needs a stable layout regardless of the
	// caller's working directory and the bbs-node executable location.
	if abs, absErr := filepath.Abs(flexBaseDir); absErr == nil {
		flexBaseDir = abs
	}
	if runtimeDir != "" {
		if abs, absErr := filepath.Abs(runtimeDir); absErr == nil {
			runtimeDir = abs
		}
	}
	return flexBaseDir, runtimeDir, nil
}

func findJavaBin(runtimeDir string) (string, error) {
	if runtimeDir != "" {
		var cand string
		switch runtime.GOOS {
		case "linux":
			cand = filepath.Join(runtimeDir, "linux-x64", "jre", "bin", "java")
		case "windows":
			cand = filepath.Join(runtimeDir, "win-x64", "jre", "bin", "java.exe")
		case "darwin":
			cand = filepath.Join(runtimeDir, "osx-x64", "jre", "Contents", "Home", "bin", "java")
		}
		if cand != "" && fileExists(cand) {
			return cand, nil
		}
	}
	return exec.LookPath("java")
}

func startFlexIPFS(javaBin, flexBaseDir, gwEndpointOverride, logDir string) (*flexIPFSProc, error) {
	if err := os.MkdirAll(filepath.Join(flexBaseDir, "providers"), 0o755); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Join(flexBaseDir, "getdata"), 0o755); err != nil {
		return nil, err
	}
	attrPath := filepath.Join(flexBaseDir, "attr")
	if _, err := os.Stat(attrPath); os.IsNotExist(err) {
		_ = os.WriteFile(attrPath, []byte{}, 0o644)
	}

	if err := maybeOverrideKadrttGWEndpoint(flexBaseDir, gwEndpointOverride); err != nil {
		return nil, err
	}
	if err := ensureKadrttGlobalIP(flexBaseDir, gwEndpointOverride); err != nil {
		return nil, err
	}
	if err := syncFlexIPFSBootstrapConfig(flexBaseDir, gwEndpointOverride); err != nil {
		return nil, err
	}

	// Keep stdin open to avoid APIServer exiting on EOF.
	stdinR, stdinW := io.Pipe()

	cmd := exec.Command(javaBin, "-cp", "lib/*", "org.peergos.APIServer")
	cmd.Dir = flexBaseDir
	cmd.Env = append(os.Environ(),
		"HOME="+flexBaseDir,
		"IPFS_HOME="+filepath.Join(flexBaseDir, ".ipfs"),
	)
	cmd.Stdin = stdinR
	var logFile *os.File
	logPath := filepath.Join(flexBaseDir, "flex-ipfs.log")
	if strings.TrimSpace(logDir) != "" {
		logPath = filepath.Join(logDir, "flex-ipfs.log")
	}
	_ = os.MkdirAll(filepath.Dir(logPath), 0o755)
	if f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644); err == nil {
		logFile = f
	}

	if !isCharDevice(os.Stdout) || !isCharDevice(os.Stderr) {
		// When bbs-node is run with stdout/stderr redirected (e.g., from the TUI),
		// inheriting those pipes can keep the parent process' output streams open
		// even after bbs-node exits, which can make callers appear to "hang".
		// Log to a file instead in that case.
		if logFile != nil {
			cmd.Stdout = logFile
			cmd.Stderr = logFile
		} else {
			cmd.Stdout = io.Discard
			cmd.Stderr = io.Discard
		}
	} else {
		if logFile != nil {
			mw := io.MultiWriter(os.Stdout, logFile)
			cmd.Stdout = mw
			cmd.Stderr = mw
		} else {
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
		}
	}

	if err := cmd.Start(); err != nil {
		_ = stdinW.Close()
		if logFile != nil {
			_ = logFile.Close()
		}
		return nil, err
	}

	log.Printf("flex-ipfs started pid=%d baseDir=%s java=%s", cmd.Process.Pid, flexBaseDir, javaBin)

	go func() {
		if err := cmd.Wait(); err != nil {
			log.Printf("flex-ipfs exited: %v", err)
		} else {
			log.Printf("flex-ipfs exited")
		}
	}()

	return &flexIPFSProc{cmd: cmd, stdinWriter: stdinW, logFile: logFile}, nil
}

func isCharDevice(f *os.File) bool {
	st, err := f.Stat()
	if err != nil {
		return false
	}
	return (st.Mode() & os.ModeCharDevice) != 0
}

func extractIP4FromMultiaddr(addr string) string {
	const prefix = "/ip4/"
	i := strings.Index(addr, prefix)
	if i < 0 {
		return ""
	}
	rest := addr[i+len(prefix):]
	j := strings.IndexByte(rest, '/')
	if j <= 0 {
		return ""
	}
	ip := rest[:j]
	if net.ParseIP(ip) == nil {
		return ""
	}
	return ip
}

func detectLocalIP4() string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return ""
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			if ip == nil {
				continue
			}
			ip = ip.To4()
			if ip == nil || ip.IsLoopback() {
				continue
			}
			return ip.String()
		}
	}
	return ""
}

func maybeOverrideKadrttGWEndpoint(flexBaseDir, endpoint string) error {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		return nil
	}
	if strings.ContainsAny(endpoint, "\r\n") {
		return fmt.Errorf("FLEXIPFS_GW_ENDPOINT must be a single line")
	}

	propsPath := filepath.Join(flexBaseDir, "kadrtt.properties")
	b, err := os.ReadFile(propsPath)
	if err != nil {
		return err
	}
	original := string(b)

	lineSep := "\n"
	if strings.Contains(original, "\r\n") {
		lineSep = "\r\n"
	}

	reEndpoint := regexp.MustCompile(`^(\s*)ipfs\.endpoint(\s*[:=]).*$`)
	parts := strings.SplitAfter(original, lineSep)

	var out strings.Builder
	out.Grow(len(original) + len(endpoint) + 64)

	replacedEndpoint := false
	for _, part := range parts {
		if part == "" {
			continue
		}
		suffix := ""
		line := part
		if strings.HasSuffix(part, lineSep) {
			suffix = lineSep
			line = strings.TrimSuffix(part, lineSep)
		}

		trimLeft := strings.TrimLeft(line, " \t")
		if strings.HasPrefix(trimLeft, "#") || strings.HasPrefix(trimLeft, "!") {
			out.WriteString(line)
			out.WriteString(suffix)
			continue
		}

		if m := reEndpoint.FindStringSubmatch(line); m != nil {
			out.WriteString(m[1])
			out.WriteString("ipfs.endpoint")
			out.WriteString(m[2])
			out.WriteString(endpoint)
			out.WriteString(suffix)
			replacedEndpoint = true
			continue
		}

		out.WriteString(line)
		out.WriteString(suffix)
	}

	if !replacedEndpoint {
		if !strings.HasSuffix(out.String(), lineSep) && out.Len() > 0 {
			out.WriteString(lineSep)
		}
		out.WriteString("ipfs.endpoint=")
		out.WriteString(endpoint)
		out.WriteString(lineSep)
	}

	st, statErr := os.Stat(propsPath)
	mode := os.FileMode(0o644)
	if statErr == nil {
		mode = st.Mode().Perm()
	}
	if err := os.WriteFile(propsPath, []byte(out.String()), mode); err != nil {
		return err
	}
	log.Printf("flex-ipfs: set ipfs.endpoint=%s (%s)", endpoint, propsPath)
	return nil
}

func ensureKadrttGlobalIP(flexBaseDir, gwEndpointOverride string) error {
	propsPath := filepath.Join(flexBaseDir, "kadrtt.properties")
	b, err := os.ReadFile(propsPath)
	if err != nil {
		return err
	}
	original := string(b)

	var existing string
	for _, raw := range strings.Split(original, "\n") {
		line := strings.TrimSpace(strings.TrimSuffix(raw, "\r"))
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "!") {
			continue
		}
		if !strings.HasPrefix(line, "ipfs.globalip") {
			continue
		}
		if idx := strings.IndexAny(line, "=:"); idx >= 0 {
			existing = strings.TrimSpace(line[idx+1:])
			break
		}
	}
	existing = strings.TrimSpace(existing)

	ip := detectLocalIP4()
	if ip == "" {
		// Best-effort only; leave as-is.
		return nil
	}
	if existing != "" && existing != "null" {
		if existing == ip {
			return nil
		}
		// If a client previously ran with mDNS discovery enabled, older builds could incorrectly
		// write the remote gw endpoint IP into ipfs.globalip. Repair that case automatically.
		if remoteIP := extractIP4FromMultiaddr(gwEndpointOverride); remoteIP != "" && existing == remoteIP {
			// proceed to rewrite to local ip below
		} else {
			return nil
		}
	}

	lineSep := "\n"
	if strings.Contains(original, "\r\n") {
		lineSep = "\r\n"
	}

	reGlobalIP := regexp.MustCompile(`^(\s*)ipfs\.globalip(\s*[:=]).*$`)
	parts := strings.SplitAfter(original, lineSep)

	var out strings.Builder
	out.Grow(len(original) + len(ip) + 64)

	replaced := false
	for _, part := range parts {
		if part == "" {
			continue
		}
		suffix := ""
		line := part
		if strings.HasSuffix(part, lineSep) {
			suffix = lineSep
			line = strings.TrimSuffix(part, lineSep)
		}

		trimLeft := strings.TrimLeft(line, " \t")
		if strings.HasPrefix(trimLeft, "#") || strings.HasPrefix(trimLeft, "!") {
			out.WriteString(line)
			out.WriteString(suffix)
			continue
		}

		if m := reGlobalIP.FindStringSubmatch(line); m != nil {
			out.WriteString(m[1])
			out.WriteString("ipfs.globalip")
			out.WriteString(m[2])
			out.WriteString(ip)
			out.WriteString(suffix)
			replaced = true
			continue
		}

		out.WriteString(line)
		out.WriteString(suffix)
	}

	if !replaced {
		if !strings.HasSuffix(out.String(), lineSep) && out.Len() > 0 {
			out.WriteString(lineSep)
		}
		out.WriteString("ipfs.globalip=")
		out.WriteString(ip)
		out.WriteString(lineSep)
	}

	st, statErr := os.Stat(propsPath)
	mode := os.FileMode(0o644)
	if statErr == nil {
		mode = st.Mode().Perm()
	}
	if err := os.WriteFile(propsPath, []byte(out.String()), mode); err != nil {
		return err
	}
	log.Printf("flex-ipfs: set ipfs.globalip=%s (%s)", ip, propsPath)
	return nil
}

func syncFlexIPFSBootstrapConfig(flexBaseDir, gwEndpointOverride string) error {
	desired := strings.TrimSpace(resolveFlexIPFSConnectEndpoint(flexBaseDir, gwEndpointOverride))
	if desired == "" {
		return nil
	}

	configPath := filepath.Join(flexBaseDir, ".ipfs", "config")
	b, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			// No config yet; APIServer will create it using Kad.GW_ENDPOINT (from kadrtt.properties).
			return nil
		}
		return err
	}

	var cfg map[string]any
	if err := json.Unmarshal(b, &cfg); err != nil {
		return fmt.Errorf("parse %s: %w", configPath, err)
	}

	var bootstrap []string
	if raw, ok := cfg["Bootstrap"]; ok {
		if arr, ok := raw.([]any); ok {
			for _, v := range arr {
				s, ok := v.(string)
				if !ok {
					continue
				}
				s = strings.TrimSpace(s)
				if s == "" {
					continue
				}
				bootstrap = append(bootstrap, s)
			}
		}
	}

	for _, s := range bootstrap {
		if s == desired {
			return nil
		}
	}

	var updated []string
	switch len(bootstrap) {
	case 0:
		updated = []string{desired}
	case 1:
		// The most common case: a stale single bootstrap from the bundled kadrtt.properties.
		updated = []string{desired}
	default:
		updated = append([]string{desired}, bootstrap...)
	}

	cfg["Bootstrap"] = updated
	out, err := json.MarshalIndent(cfg, "", "\t")
	if err != nil {
		return fmt.Errorf("marshal %s: %w", configPath, err)
	}
	out = append(out, '\n')

	st, statErr := os.Stat(configPath)
	mode := os.FileMode(0o644)
	if statErr == nil {
		mode = st.Mode().Perm()
	}
	if err := os.WriteFile(configPath, out, mode); err != nil {
		return err
	}

	log.Printf("flex-ipfs: updated .ipfs/config bootstrap (added %s)", desired)
	return nil
}

func tryAcquireFlexIPFSStartLock(lockPath string) (release func(), acquired bool, err error) {
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err == nil {
		_, _ = fmt.Fprintf(f, "pid=%d\nstarted=%s\n", os.Getpid(), time.Now().Format(time.RFC3339Nano))
		_ = f.Close()
		return func() { _ = os.Remove(lockPath) }, true, nil
	}
	if os.IsExist(err) {
		return func() {}, false, nil
	}
	return func() {}, false, err
}

func resolveFlexIPFSConnectEndpoint(baseDirOrOverride string, gwEndpointOverride string) string {
	if v := strings.TrimSpace(gwEndpointOverride); v != "" {
		return v
	}

	// Allow manual edits in flexible-ipfs-base/kadrtt.properties to work without requiring
	// passing --flexipfs-gw-endpoint every time.
	baseDir := strings.TrimSpace(baseDirOrOverride)
	if baseDir == "" || !dirExists(baseDir) {
		if bd, _, err := resolveFlexDirs(baseDirOrOverride); err == nil && bd != "" {
			baseDir = bd
		}
	}
	if baseDir == "" {
		return ""
	}
	if v, err := readKadrttGWEndpoint(baseDir); err == nil {
		return v
	}
	return ""
}

func readKadrttGWEndpoint(flexBaseDir string) (string, error) {
	propsPath := filepath.Join(flexBaseDir, "kadrtt.properties")
	b, err := os.ReadFile(propsPath)
	if err != nil {
		return "", err
	}
	for _, raw := range strings.Split(string(b), "\n") {
		line := strings.TrimSpace(strings.TrimSuffix(raw, "\r"))
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "!") {
			continue
		}
		if !strings.HasPrefix(line, "ipfs.endpoint") {
			continue
		}
		if idx := strings.IndexAny(line, "=:"); idx >= 0 {
			if v := strings.TrimSpace(line[idx+1:]); v != "" {
				return v, nil
			}
		}
	}
	return "", nil
}

func waitForFlexIPFS(ctx context.Context, baseURL string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	endpoint := strings.TrimRight(baseURL, "/") + "/dht/peerlist"
	client := &http.Client{Timeout: 2 * time.Second}

	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return false
		default:
		}
		req, _ := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, nil)
		resp, err := client.Do(req)
		if err == nil && resp != nil {
			_ = resp.Body.Close()
			if resp.StatusCode >= 200 && resp.StatusCode < 500 {
				log.Printf("flex-ipfs API ready: %s", endpoint)
				return true
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	log.Printf("flex-ipfs API not ready after %s", timeout)
	return false
}

func flexIPFSSwarmConnect(ctx context.Context, baseURL, addr string) error {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return nil
	}
	u := strings.TrimRight(baseURL, "/") + "/swarm/connect"
	q := url.Values{}
	q.Set("arg", addr)
	u += "?" + q.Encode()

	client := &http.Client{Timeout: 8 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<10))
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	// Flexible-IPFS (ipfs-ncl.jar) doesn't implement /swarm/connect; ignore.
	if resp.StatusCode == http.StatusNotFound {
		return nil
	}
	msg := strings.TrimSpace(string(body))
	if msg == "" {
		msg = resp.Status
	}
	return fmt.Errorf("flex-ipfs swarm/connect http %d: %s", resp.StatusCode, msg)
}

func waitForFlexIPFSPeers(ctx context.Context, baseURL string, timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	endpoint := strings.TrimRight(baseURL, "/") + "/dht/peerlist"
	client := &http.Client{Timeout: 2 * time.Second}

	for time.Now().Before(deadline) {
		req, _ := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, nil)
		resp, err := client.Do(req)
		if err == nil && resp != nil {
			body, readErr := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
			_ = resp.Body.Close()
			if readErr == nil && resp.StatusCode >= 200 && resp.StatusCode < 500 {
				var s string
				if jsonErr := json.Unmarshal(bytes.TrimSpace(body), &s); jsonErr == nil {
					if strings.TrimSpace(s) != "" {
						return
					}
				} else if strings.TrimSpace(string(body)) != "" {
					return
				}
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	log.Printf("flex-ipfs peers not discovered after %s", timeout)
}

func isFlexIPFSUp(ctx context.Context, baseURL string) bool {
	endpoint := strings.TrimRight(baseURL, "/") + "/dht/peerlist"
	client := &http.Client{Timeout: 1 * time.Second}
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, nil)
	resp, err := client.Do(req)
	if err != nil || resp == nil {
		return false
	}
	_ = resp.Body.Close()
	return resp.StatusCode >= 200 && resp.StatusCode < 500
}

func dirExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}
