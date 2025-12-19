package main

import (
	"context"
	"fmt"
	"io"
	"log"
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
}

func maybeStartFlexIPFS(ctx context.Context, baseURL, baseDirOverride, gwEndpointOverride string) (*flexIPFSProc, error) {
	if !isLocalBaseURL(baseURL) {
		log.Printf("flex-ipfs autostart skipped (non-local base url): %s", baseURL)
		return nil, nil
	}

	// If a local Flexible-IPFS is already running on the target base URL,
	// don't start a second instance (avoids port conflicts).
	if isFlexIPFSUp(ctx, baseURL) {
		log.Printf("flex-ipfs already running at %s", baseURL)
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

	proc, err := startFlexIPFS(javaBin, flexBaseDir, gwEndpointOverride)
	if err != nil {
		return nil, err
	}

	// Best-effort wait for API to come up
	waitForFlexIPFS(ctx, baseURL, 20*time.Second)
	return proc, nil
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

func startFlexIPFS(javaBin, flexBaseDir, gwEndpointOverride string) (*flexIPFSProc, error) {
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

	// Keep stdin open to avoid APIServer exiting on EOF.
	stdinR, stdinW := io.Pipe()

	cmd := exec.Command(javaBin, "-cp", "lib/*", "org.peergos.APIServer")
	cmd.Dir = flexBaseDir
	cmd.Env = append(os.Environ(),
		"HOME="+flexBaseDir,
		"IPFS_HOME="+filepath.Join(flexBaseDir, ".ipfs"),
	)
	cmd.Stdin = stdinR
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		_ = stdinW.Close()
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

	return &flexIPFSProc{cmd: cmd, stdinWriter: stdinW}, nil
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

	re := regexp.MustCompile(`^(\s*)ipfs\.endpoint(\s*[:=]).*$`)
	parts := strings.SplitAfter(original, lineSep)

	var out strings.Builder
	out.Grow(len(original) + len(endpoint) + 32)

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

		if m := re.FindStringSubmatch(line); m != nil {
			out.WriteString(m[1])
			out.WriteString("ipfs.endpoint")
			out.WriteString(m[2])
			out.WriteString(endpoint)
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

func waitForFlexIPFS(ctx context.Context, baseURL string, timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	endpoint := strings.TrimRight(baseURL, "/") + "/dht/peerlist"
	client := &http.Client{Timeout: 2 * time.Second}

	for time.Now().Before(deadline) {
		req, _ := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, nil)
		resp, err := client.Do(req)
		if err == nil && resp != nil {
			_ = resp.Body.Close()
			if resp.StatusCode >= 200 && resp.StatusCode < 500 {
				log.Printf("flex-ipfs API ready: %s", endpoint)
				return
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	log.Printf("flex-ipfs API not ready after %s", timeout)
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
