package api

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRequestWithoutHeaderGivesCSRF(t *testing.T) {
	e := newTestEnv(t, func(o *Options) { o.Origins = []string{"http://localhost:4200"} })
	tests := []struct {
		name   string
		mod    func(*http.Request)
		status int
	}{
		{"allowed", func(*http.Request) {}, http.StatusAccepted},
		{"without the header", func(r *http.Request) { r.Header.Del(requestHeader) }, http.StatusForbidden},
		{"wrong header value", func(r *http.Request) { r.Header.Set(requestHeader, "0") }, http.StatusForbidden},
		{"without an Origin", func(r *http.Request) { r.Header.Del("Origin") }, http.StatusForbidden},
		{"foreign Origin", func(r *http.Request) { r.Header.Set("Origin", "http://evil.example.com") }, http.StatusForbidden},
		{"Origin null", func(r *http.Request) { r.Header.Set("Origin", "null") }, http.StatusForbidden},
		{"foreign Host", func(r *http.Request) { r.Host = "evil.example.com:7878" }, http.StatusForbidden},
		{"right Host, wrong port", func(r *http.Request) { r.Host = "127.0.0.1:9999" }, http.StatusForbidden},
		{"localhost is the same server", func(r *http.Request) {
			r.Host = "localhost:7878"
			r.Header.Set("Origin", "http://localhost:7878")
		}, http.StatusAccepted},
		{"configured origin", func(r *http.Request) { r.Header.Set("Origin", "http://localhost:4200") }, http.StatusAccepted},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := e.request(http.MethodPost, "/checks/refresh", "", tt.mod)
			e.wantStatus(r, tt.status)
			if tt.status == http.StatusForbidden {
				if p := r.problem(t); p.Code != codeCSRF {
					t.Errorf("code = %s, want csrf", p.Code)
				}
			}
		})
	}

	// reading needs neither the header nor an Origin, but the Host is always checked
	e.wantStatus(e.get("/info"), http.StatusOK)
	r := e.request(http.MethodGet, "/info", nil, func(r *http.Request) { r.Host = "evil.example.com" })
	e.wantStatus(r, http.StatusForbidden)
	if r.problem(t).Code != codeCSRF {
		t.Errorf("foreign Host: %s", r.body)
	}
	// the UI is guarded, too
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Host = "evil.example.com"
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("UI with a foreign Host: %d", rec.Code)
	}
}

func TestSocketSkipsTheChecks(t *testing.T) {
	e := newTestEnv(t)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/checks/refresh", nil)
	req.Host = "ignored"
	rec := httptest.NewRecorder()
	e.srv.SocketHandler().ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Errorf("over the socket: %d %s", rec.Code, rec.Body)
	}
	if !e.srv.CheckOrigin(req.WithContext(context.WithValue(req.Context(), viaSocketKey, true))) {
		t.Error("CheckOrigin must accept requests over the socket")
	}
}

func TestSecurityHeaders(t *testing.T) {
	e := newTestEnv(t)
	for _, path := range []string{"/info", "/nothing", "/settings/demo.nothing/explain"} {
		r := e.get(path)
		if got := r.header.Get("Content-Security-Policy"); got != contentSecurityPolicy {
			t.Errorf("%s: CSP = %q", path, got)
		}
		if r.header.Get("X-Content-Type-Options") != "nosniff" || r.header.Get("Referrer-Policy") != "no-referrer" {
			t.Errorf("%s: headers = %v", path, r.header)
		}
	}
	for _, want := range []string{"default-src 'self'", "frame-ancestors 'none'", "base-uri 'none'", "form-action 'self'", "img-src 'self' data:"} {
		if !strings.Contains(contentSecurityPolicy, want) {
			t.Errorf("CSP lacks %s", want)
		}
	}
	// the UI and the errors of the guard carry them, too
	req, rec := newUIRequest("/")
	e.h.ServeHTTP(rec, req)
	if rec.Header().Get("Content-Security-Policy") == "" {
		t.Error("the UI has no CSP")
	}
	req, rec = newUIRequest("/api/v1/info")
	req.Host = "evil.example.com"
	e.h.ServeHTTP(rec, req)
	if rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Error("a refused request has no security headers")
	}
}

func TestAccessLog(t *testing.T) {
	e := newTestEnv(t)
	e.get("/plugins?manifest=true&secret=hunter2")
	e.request(http.MethodPost, "/checks/refresh", `{"ids":["token-in-body"]}`)
	logs := e.logs.String()
	if strings.Contains(logs, "hunter2") || strings.Contains(logs, "token-in-body") || strings.Contains(logs, "secret=") {
		t.Errorf("the access log leaks a query or a body:\n%s", logs)
	}
	var found bool
	sc := bufio.NewScanner(strings.NewReader(logs))
	for sc.Scan() {
		var line map[string]any
		if err := json.Unmarshal(sc.Bytes(), &line); err != nil {
			t.Fatalf("log line is not JSON: %q", sc.Text())
		}
		if line["msg"] == "request" && line["path"] == "/api/v1/plugins" {
			found = line["method"] == "GET" && line["status"] == float64(200) && line["durationMs"] != nil
		}
	}
	if !found {
		t.Errorf("no access log line for the request:\n%s", logs)
	}
}

func TestInternalErrorIsOpaque(t *testing.T) {
	e := newTestEnv(t)
	// an environment without HOME is not the caller's fault: the detail stays in the log
	e.getenv["HOME"] = ""
	r := e.get("/info")
	p := r.problem(t)
	if r.code != http.StatusInternalServerError || p.Code != codeInternal || p.TraceID == "" || p.Detail != "" {
		t.Errorf("response = %d %+v", r.code, p)
	}
	if !strings.Contains(e.logs.String(), p.TraceID) {
		t.Errorf("the trace ID %s is not in the log", p.TraceID)
	}
}

func TestInvalidConfigurationIsAProblem(t *testing.T) {
	e := newTestEnv(t)
	write(t, filepath.Join(e.home, ".config", "devbox", "config.json"), `{"unknownKey": 1}`, 0o600)
	r := e.get("/settings")
	if r.code >= 500 {
		t.Fatalf("status %d: %s", r.code, r.body)
	}
	if r.problem(t).Code != codeValidation {
		t.Errorf("problem = %s", r.body)
	}
}

func TestOversizedBody(t *testing.T) {
	e := newTestEnv(t)
	big := `{"demo.mode":"` + strings.Repeat("x", maxBody) + `"}`
	r := e.request(http.MethodPatch, "/settings", big)
	e.wantStatus(r, http.StatusRequestEntityTooLarge)
}

// TestServe runs the real listeners: the TCP port the system chose, the socket and the
// shutdown that stops a running session and records it.
func TestServe(t *testing.T) {
	e := newTestEnv(t, func(o *Options) { o.Listen = "127.0.0.1:0" })
	tcp, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	sock := filepath.Join(t.TempDir(), "s.sock")
	unix, err := (&net.ListenConfig{}).Listen(context.Background(), "unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- e.srv.Serve(ctx, tcp, unix) }()

	base := "http://" + tcp.Addr().String()
	client := &http.Client{Timeout: 10 * time.Second}
	do := func(c *http.Client, method, url string, hdr map[string]string) *http.Response {
		req, err := http.NewRequestWithContext(context.Background(), method, url, strings.NewReader(`{"action":"demo.slow"}`))
		if err != nil {
			t.Fatal(err)
		}
		for k, v := range hdr {
			req.Header.Set(k, v)
		}
		resp, err := c.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return resp
	}
	// the Host check knows the port the system chose
	resp := do(client, http.MethodGet, base+"/api/v1/info", nil)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("info: %d", resp.StatusCode)
	}
	// over TCP the CSRF rules apply
	resp = do(client, http.MethodPost, base+"/api/v1/sessions", nil)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("create without the header: %d", resp.StatusCode)
	}
	// over the socket they do not
	unixClient := &http.Client{Timeout: 10 * time.Second, Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", sock)
		},
	}}
	resp = do(unixClient, http.MethodPost, "http://socket.example.com/api/v1/sessions", nil)
	var created map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create over the socket: %d %v", resp.StatusCode, created)
	}
	id := created["id"].(string)
	waitFor(t, "the output", func() bool {
		resp := do(unixClient, http.MethodGet, "http://socket.example.com/api/v1/sessions/"+id+"/log", nil)
		defer func() { _ = resp.Body.Close() }()
		data, _ := io.ReadAll(resp.Body)
		return strings.Contains(string(data), "started")
	})

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Serve: %v", err)
		}
	case <-time.After(60 * time.Second):
		t.Fatal("Serve did not return")
	}
	entry, ok, err := e.store().Get(id)
	if err != nil || !ok || entry.State != "stopped" {
		t.Errorf("history entry after shutdown: %+v %v %v", entry, ok, err)
	}
	if _, err := os.Stat(sock); err == nil {
		t.Error("the socket file was not removed")
	}
}
