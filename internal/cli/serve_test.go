package cli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func TestIsLoopback(t *testing.T) {
	tests := map[string]bool{
		"127.0.0.1:7878":   true,
		"127.1.2.3:80":     true,
		"[::1]:7878":       true,
		"localhost:7878":   true,
		"0.0.0.0:7878":     false,
		":7878":            false,
		"[::]:7878":        false,
		"192.0.2.10:7878":  false,
		"example.com:7878": false,
		"127.0.0.1":        false, // no port
		"":                 false,
	}
	for addr, want := range tests {
		if got := isLoopback(addr); got != want {
			t.Errorf("isLoopback(%q) = %v, want %v", addr, got, want)
		}
	}
}

func TestServeRefusesToStart(t *testing.T) {
	tests := []struct {
		name   string
		config string
		args   []string
		want   string
	}{
		{"non-loopback flag", ``, []string{"--listen", "0.0.0.0:7878"}, "not a loopback address"},
		{"non-loopback config", `{"server":{"listen":"192.0.2.10:7878"}}`, nil, "needs server.auth pam and TLS"},
		{"pam", `{"server":{"auth":"pam"}}`, nil, "server.auth pam"},
		{"pam on loopback", `{"server":{"auth":"pam","listen":"127.0.0.1:7878"}}`, []string{"--listen", "127.0.0.1:0"}, "not available yet"},
		{"off on all interfaces", `{"server":{"auth":"off"}}`, []string{"--listen", ":0"}, "not a loopback address"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			home := t.TempDir()
			if tt.config != "" {
				writeFile(t, filepath.Join(home, ".config", "devbox", "config.json"), tt.config)
			}
			sys, _, stderr := testIO(t, home)
			code := Main(append([]string{"serve"}, tt.args...), Build{Version: "1.0.0"}, sys)
			if code != ExitInvalid {
				t.Errorf("exit %d, want %d", code, ExitInvalid)
			}
			if !strings.Contains(stderr.String(), tt.want) {
				t.Errorf("stderr = %q, want %q", stderr, tt.want)
			}
		})
	}
}

func TestServerConfigDefault(t *testing.T) {
	home := t.TempDir()
	sys, _, _ := testIO(t, home)
	a := &app{io: sys, build: Build{Version: "1.0.0"}}
	st, err := a.load(true)
	if err != nil {
		t.Fatal(err)
	}
	sc, err := st.Server()
	if err != nil {
		t.Fatal(err)
	}
	// the defaults come from schema/config.v1.json
	if sc.Listen == "" || !isLoopback(sc.Listen) || sc.Auth != "local" {
		t.Errorf("defaults = %+v", sc)
	}

	// the user file wins over the system file, the system file over the schema
	writeFile(t, filepath.Join(home, "no-system-config.json"), `{"server":{"listen":"127.0.0.1:9001","allowedOrigins":["http://localhost:4200"]}}`)
	if st, err = a.load(true); err != nil {
		t.Fatal(err)
	}
	if sc, _ = st.Server(); sc.Listen != "127.0.0.1:9001" || len(sc.AllowedOrigins) != 1 {
		t.Errorf("system file: %+v", sc)
	}
	writeFile(t, filepath.Join(home, ".config", "devbox", "config.json"), `{"server":{"listen":"127.0.0.1:9002"}}`)
	if st, err = a.load(true); err != nil {
		t.Fatal(err)
	}
	if sc, _ = st.Server(); sc.Listen != "127.0.0.1:9002" {
		t.Errorf("user file: %+v", sc)
	}
}

func TestServeRuns(t *testing.T) {
	home := t.TempDir()
	sys, _, _ := testIO(t, home)
	logs := &lockedBuffer{}
	sys.Stderr = logs
	a := &app{io: sys, build: Build{Version: "1.0.0", Vendor: "zenit-hosting", Protocol: 1}}
	sock := filepath.Join(t.TempDir(), "api.sock")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- a.serve(ctx, serveFlags{listen: "127.0.0.1:0", socket: sock}) }()

	var url string
	deadline := time.Now().Add(20 * time.Second)
	for url == "" && time.Now().Before(deadline) {
		sc := bufio.NewScanner(strings.NewReader(logs.String()))
		for sc.Scan() {
			var line map[string]any
			if json.Unmarshal(sc.Bytes(), &line) == nil && line["msg"] == "listening" && line["url"] != nil {
				url, _ = line["url"].(string)
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	if url == "" {
		t.Fatalf("no listening line in the log:\n%s", logs)
	}
	if strings.HasSuffix(url, ":0") {
		t.Errorf("url %q: port 0 must print the real port", url)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url+"/api/v1/info", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), `"vendor":"zenit-hosting"`) {
		t.Errorf("info: %d %s", resp.StatusCode, body)
	}
	if !strings.Contains(string(body), `"socket"`) {
		t.Errorf("the socket feature is missing: %s", body)
	}
	req, err = http.NewRequestWithContext(ctx, http.MethodGet, url+"/", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	page, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(page), "DevBox") {
		t.Errorf("UI: %d %s", resp.StatusCode, page)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("serve: %v", err)
		}
	case <-time.After(60 * time.Second):
		t.Fatal("serve did not stop")
	}
	if !strings.Contains(logs.String(), `"msg":"request"`) {
		t.Errorf("no access log:\n%s", logs)
	}
}

func TestServeWarnsAboutAuthOff(t *testing.T) {
	home := t.TempDir()
	writeFile(t, filepath.Join(home, ".config", "devbox", "config.json"), `{"server":{"auth":"off"}}`)
	sys, _, _ := testIO(t, home)
	logs := &lockedBuffer{}
	sys.Stderr = logs
	a := &app{io: sys, build: Build{Version: "1.0.0"}}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- a.serve(ctx, serveFlags{listen: "127.0.0.1:0", socket: filepath.Join(t.TempDir(), "s")})
	}()
	deadline := time.Now().Add(20 * time.Second)
	for !strings.Contains(logs.String(), `"url"`) && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	if err := <-done; err != nil {
		t.Errorf("serve: %v", err)
	}
	if !strings.Contains(logs.String(), `"level":"WARN"`) || !strings.Contains(logs.String(), "server.auth is off") {
		t.Errorf("no warning about auth off:\n%s", logs)
	}
}

func TestListenSocketDirectories(t *testing.T) {
	mode := func(t *testing.T, path string) os.FileMode {
		t.Helper()
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		return info.Mode().Perm()
	}
	listen := func(t *testing.T, path string) {
		t.Helper()
		ln, err := listenSocket(context.Background(), path)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = ln.Close() })
		if got := mode(t, path); got != 0o600 {
			t.Errorf("socket mode = %v, want 0600", got)
		}
	}

	t.Run("existing directory keeps its mode", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.Chmod(dir, 0o755); err != nil { //nolint:gosec // the test needs a shared directory
			t.Fatal(err)
		}
		listen(t, filepath.Join(dir, "api.sock"))
		if got := mode(t, dir); got != 0o755 {
			t.Errorf("directory mode = %v, want 0755 (untouched)", got)
		}
	})

	t.Run("directory owned by someone else is not touched", func(t *testing.T) {
		// like --socket /tmp/api.sock: the sticky world-writable directory cannot be chmod-ed
		dir := t.TempDir()
		if err := os.Chmod(dir, 0o1777); err != nil { //nolint:gosec // the test needs a sticky shared directory
			t.Fatal(err)
		}
		listen(t, filepath.Join(dir, "api.sock"))
		if got := mode(t, dir); got != 0o777 {
			t.Errorf("directory mode = %v, want 0777 (untouched)", got)
		}
	})

	t.Run("missing directory is created private", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "devbox")
		listen(t, filepath.Join(dir, "api.sock"))
		if got := mode(t, dir); got != 0o700 {
			t.Errorf("directory mode = %v, want 0700", got)
		}
	})

	t.Run("socket in use and stale socket", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "api.sock")
		ln, err := listenSocket(context.Background(), path)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := listenSocket(context.Background(), path); err == nil {
			t.Error("a socket in use must be refused")
		}
		// a stale socket is a file nobody listens on: closing without unlinking leaves one
		ln.(*net.UnixListener).SetUnlinkOnClose(false)
		_ = ln.Close()
		listen(t, path)
	})
}

func TestSelfRestartExecsTheStartPathWithMarker(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "real", "devbox")
	writeFile(t, target, "binary")
	link := filepath.Join(dir, "bin", "devbox")
	if err := os.MkdirAll(filepath.Dir(link), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	sys, _, _ := testIO(t, t.TempDir())
	var gotPath string
	var gotArgv, gotEnv []string
	sys.Executable = func() (string, error) { return link, nil }
	sys.Environ = func() []string { return []string{"HOME=/h", RestartedEnv + "=1", "TERM=dumb"} }
	sys.Exec = func(path string, argv, env []string) error {
		gotPath, gotArgv, gotEnv = path, argv, env
		return errors.New("exec failed")
	}
	a := &app{io: sys, args: []string{"serve", "--listen", "127.0.0.1:0", "--open"}}

	restart := a.selfRestart(slog.New(slog.NewJSONHandler(io.Discard, nil)))
	if restart == nil {
		t.Fatal("no restart function")
	}
	err := restart()
	if err == nil || !strings.Contains(err.Error(), "exec failed") {
		t.Errorf("error %v: a failed exec must be returned", err)
	}
	realTarget, _ := filepath.EvalSymlinks(target)
	if gotPath != realTarget {
		t.Errorf("path %q, want the resolved start path %q", gotPath, realTarget)
	}
	if want := []string{realTarget, "serve", "--listen", "127.0.0.1:0", "--open"}; !slices.Equal(gotArgv, want) {
		t.Errorf("argv %q, want %q", gotArgv, want)
	}
	if want := []string{"HOME=/h", "TERM=dumb", RestartedEnv + "=1"}; !slices.Equal(gotEnv, want) {
		t.Errorf("env %q, want %q (the marker exactly once)", gotEnv, want)
	}
}

func TestSelfRestartNeedsAKnownBinary(t *testing.T) {
	sys, _, _ := testIO(t, t.TempDir())
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	if (&app{io: sys}).selfRestart(logger) != nil {
		t.Error("a restart without Executable and Exec must not exist")
	}
	sys.Exec = func(string, []string, []string) error { return nil }
	sys.Executable = func() (string, error) { return "", errors.New("unknown") }
	if (&app{io: sys}).selfRestart(logger) != nil {
		t.Error("a restart without a binary path must not exist")
	}
	sys.Executable = func() (string, error) { return "/nonexistent/devbox (deleted)", nil }
	if (&app{io: sys}).selfRestart(logger) != nil {
		t.Error("a path that does not resolve must not give a restart")
	}
}

// TestServeRestartsAfterNotice runs the whole way: an action emits restart-service,
// the service announces it and calls Exec with the marker.
func TestServeRestartsAfterNotice(t *testing.T) {
	home := t.TempDir()
	writeFile(t, filepath.Join(home, ".local", "share", "devbox", "plugins", "upd", "plugin.json"),
		`{"id":"upd","version":"1.0.0","name":"Upd","actions":{"go":{"label":"Go","run":["act.sh"]}}}`)
	script := filepath.Join(home, ".local", "share", "devbox", "plugins", "upd", "act.sh")
	writeFile(t, script, "#!/usr/bin/env bash\nprintf '%s\\n' '{\"type\":\"notice\",\"kind\":\"restart-service\",\"text\":\"x\"}' >&3\nprintf '%s\\n' '{\"type\":\"result\",\"summary\":\"ok\"}' >&3\n")
	if err := os.Chmod(script, 0o700); err != nil { //nolint:gosec // a plugin script must be executable
		t.Fatal(err)
	}
	exe := filepath.Join(home, "bin", "devbox")
	writeFile(t, exe, "binary")
	sys, _, _ := testIO(t, home)
	logs := &lockedBuffer{}
	sys.Stderr = logs
	baseEnv := sys.Getenv
	sys.Getenv = func(k string) string { // the plugin script needs bash from the PATH
		if k == "PATH" {
			return os.Getenv("PATH")
		}
		return baseEnv(k)
	}
	sys.Executable = func() (string, error) { return exe, nil }
	execd := make(chan []string, 1)
	sys.Exec = func(path string, argv, env []string) error {
		execd <- append(append([]string{path}, argv...), env...)
		return errors.New("test: do not exec")
	}
	sys.Environ = func() []string { return []string{"A=b"} }
	a := &app{io: sys, build: Build{Version: "1.0.0", Vendor: "zenit-hosting", Protocol: 1}, args: []string{"serve"}}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- a.serve(ctx, serveFlags{listen: "127.0.0.1:0", socket: filepath.Join(t.TempDir(), "s")})
	}()
	var url string
	deadline := time.Now().Add(20 * time.Second)
	for url == "" && time.Now().Before(deadline) {
		sc := bufio.NewScanner(strings.NewReader(logs.String()))
		for sc.Scan() {
			var line map[string]any
			if json.Unmarshal(sc.Bytes(), &line) == nil && line["msg"] == "listening" && line["url"] != nil {
				url, _ = line["url"].(string)
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	if url == "" {
		t.Fatalf("no listening line in the log:\n%s", logs)
	}
	// The plugin of the test home is the only one that matters; the bundled ones are ready too.
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url+"/api/v1/sessions", strings.NewReader(`{"action":"upd.go"}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-DevBox-Request", "1")
	req.Header.Set("Origin", url)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("session: %d %s", resp.StatusCode, body)
	}
	select {
	case got := <-execd:
		resolved, _ := filepath.EvalSymlinks(exe)
		want := []string{resolved, resolved, "serve", "A=b", RestartedEnv + "=1"}
		if !slices.Equal(got, want) {
			t.Errorf("exec %q, want %q", got, want)
		}
	case <-time.After(30 * time.Second):
		t.Fatalf("Exec was not called:\n%s", logs)
	}
	cancel()
	if err := <-done; err != nil {
		t.Errorf("serve: %v", err)
	}
}
