package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Zenit-Hosting-de/devbox/internal/core"
	"github.com/Zenit-Hosting-de/devbox/internal/history"
)

// testListen is the address the test servers pretend to listen on.
const testListen = "127.0.0.1:7878"

// bundled lists the plugins that ship with the binary; the tests disable them so no
// real tool or network is touched.
var bundled = []string{"base", "credentials", "java", "maven", "node", "npm", "registry", "system"}

const demoManifest = `{
  "id": "demo",
  "version": "1.0.0",
  "name": "Demo",
  "category": "runtime",
  "settings": {
    "defaultVersion": { "type": "integer", "default": 21, "label": "Default version" },
    "lastVersion": { "type": "integer", "default": 0, "label": "Last version" },
    "mode": { "type": "string", "enum": ["fast", "slow"], "default": "fast", "label": "Mode" },
    "pinned": { "type": "string", "default": "a", "label": "Pinned" }
  },
  "checks": {
    "state": { "label": "State", "run": ["check.sh"], "cache": "1h", "refreshOn": ["session.finished:demo.*"] }
  },
  "actions": {
    "install": {
      "label": "Install",
      "run": ["act.sh", "install"],
      "params": {
        "version": { "type": "integer", "default": "${demo.defaultVersion}", "label": "Version" },
        "channel": { "type": "string", "enum": ["lts", "current"], "default": "lts", "label": "Channel" }
      }
    },
    "remember": { "label": "Remember", "run": ["act.sh", "remember"] },
    "remove": { "label": "Remove", "run": ["act.sh", "remove"], "confirm": "Removes everything.", "danger": true },
    "slow": { "label": "Slow", "run": ["act.sh", "slow"] },
    "fail": { "label": "Fail", "run": ["act.sh", "fail"] }
  }
}`

const demoActions = `#!/usr/bin/env bash
set -euo pipefail
field() { grep -o "\"$1\":[^,}]*" "$DEVBOX_REQUEST" | head -n1 | cut -d: -f2 | tr -d '"'; }
case "$1" in
install)
  echo "version=$(field version) dryRun=$(field dryRun)"
  printf '%s\n' '{"type":"step","id":"download","label":"Download","state":"running"}' >&3
  printf '%s\n' '{"type":"progress","percent":50,"text":"half"}' >&3
  echo "after progress"
  printf '%s\n' '{"type":"change","kind":"file","op":"create","target":"~/.local/share/demo","summary":"unpacked"}' >&3
  printf '%s\n' '{"type":"result","summary":"installed"}' >&3
  ;;
remember)
  printf '%s\n' '{"type":"setting","key":"lastVersion","value":7}' >&3
  ;;
remove)
  echo removed
  ;;
slow)
  echo started
  sleep 30
  ;;
fail)
  echo failing
  exit 1
  ;;
esac
`

const demoCheck = `#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' '{"status":"ok","summary":"fine","details":[{"label":"Version","value":"1"}],"hints":[{"text":"Install it","action":"demo.install"}],"data":{"count":2}}'
`

const needyManifest = `{
  "id": "needy",
  "version": "1.0.0",
  "name": "Needy",
  "requires": { "commands": ["devbox-test-no-such-command"] },
  "actions": { "go": { "label": "Go", "run": ["act.sh"] } }
}`

const gapManifest = `{
  "id": "gap",
  "version": "1.0.0",
  "name": "Gap",
  "settings": { "token": { "type": "string", "label": "Token" } },
  "actions": { "go": { "label": "Go", "run": ["act.sh"] } }
}`

const demoProfile = `{
  "id": "example-team",
  "name": "Example team",
  "version": "2026.10.1",
  "settings": { "demo.pinned": { "value": "b", "locked": true } }
}`

const demoMessages = `{
  "$comment": "Demo translations",
  "demo.title": "Demo",
  "demo.install.label": "Installieren"
}`

// testEnv is a server with a temporary home, the plugin "demo" and a recording
// broadcaster.
type testEnv struct {
	t       *testing.T
	home    string
	srv     *Server
	h       http.Handler
	bc      *recorder
	logs    *syncBuffer
	getenv  map[string]string
	plugins string
}

type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func write(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
}

func newTestEnv(t *testing.T, mods ...func(*Options)) *testEnv {
	t.Helper()
	home := t.TempDir()
	e := &testEnv{t: t, home: home, bc: &recorder{}, logs: &syncBuffer{}}
	e.getenv = map[string]string{
		"HOME":               home,
		"PATH":               os.Getenv("PATH"),
		"XDG_RUNTIME_DIR":    filepath.Join(home, "run"),
		core.SystemConfigEnv: filepath.Join(home, "etc", "system.json"),
		"LANG":               "C.UTF-8",
		"TERM":               "xterm-256color",
	}
	disabled, err := json.Marshal(bundled)
	if err != nil {
		t.Fatal(err)
	}
	write(t, e.getenv[core.SystemConfigEnv], `{"plugins":{"disabled":`+string(disabled)+`}}`, 0o600)

	e.plugins = filepath.Join(home, ".local", "share", "devbox", "plugins")
	write(t, filepath.Join(e.plugins, "demo", "plugin.json"), demoManifest, 0o600)
	write(t, filepath.Join(e.plugins, "demo", "act.sh"), demoActions, 0o700)
	write(t, filepath.Join(e.plugins, "demo", "check.sh"), demoCheck, 0o700)
	write(t, filepath.Join(e.plugins, "demo", "i18n", "de.json"), demoMessages, 0o600)
	write(t, filepath.Join(e.plugins, "needy", "plugin.json"), needyManifest, 0o600)
	write(t, filepath.Join(e.plugins, "needy", "act.sh"), "#!/usr/bin/env bash\n", 0o700)
	write(t, filepath.Join(e.plugins, "gap", "plugin.json"), gapManifest, 0o600)
	write(t, filepath.Join(e.plugins, "gap", "act.sh"), "#!/usr/bin/env bash\n", 0o700)

	opts := Options{
		Build:       core.Build{Vendor: "zenit-hosting", Version: "1.2.3", Commit: "abc1234", Date: "2026-01-02T03:04:05Z", Protocol: 1},
		Env:         core.Env{Getenv: func(k string) string { return e.getenv[k] }, Environ: func() []string { return nil }},
		Listen:      testListen,
		Auth:        "local",
		Logger:      slog.New(slog.NewJSONHandler(e.logs, nil)),
		Broadcaster: e.bc,
	}
	for _, m := range mods {
		m(&opts)
	}
	srv, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := srv.Shutdown(ctx); err != nil {
			t.Errorf("shutdown: %v", err)
		}
	})
	e.srv, e.h = srv, srv.Handler()
	return e
}

// response is what a request returned.
type response struct {
	code   int
	header http.Header
	body   []byte
}

func (r response) json(t *testing.T) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(r.body, &m); err != nil {
		t.Fatalf("not a JSON object: %v\n%s", err, r.body)
	}
	return m
}

// problem decodes a problem response.
func (r response) problem(t *testing.T) problem {
	t.Helper()
	var p problem
	if err := json.Unmarshal(r.body, &p); err != nil {
		t.Fatalf("not a problem: %v\n%s", err, r.body)
	}
	return p
}

// request sends a request to the handler as a browser of the UI would, and checks
// the response against the contract.
func (e *testEnv) request(method, path string, body any, mods ...func(*http.Request)) response {
	e.t.Helper()
	var rd io.Reader
	switch b := body.(type) {
	case nil:
	case string:
		rd = strings.NewReader(b)
	default:
		data, err := json.Marshal(b)
		if err != nil {
			e.t.Fatal(err)
		}
		rd = bytes.NewReader(data)
	}
	req := httptest.NewRequest(method, "/api/v1"+path, rd)
	req.Host = testListen
	if isMutating(method) {
		req.Header.Set(requestHeader, "1")
		req.Header.Set("Origin", "http://"+testListen)
		req.Header.Set("Content-Type", "application/json")
	}
	for _, m := range mods {
		m(req)
	}
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	res := response{code: rec.Code, header: rec.Header(), body: rec.Body.Bytes()}
	checkContract(e.t, req, res)
	return res
}

func (e *testEnv) get(path string) response {
	e.t.Helper()
	return e.request(http.MethodGet, path, nil)
}

func (e *testEnv) wantStatus(r response, code int) {
	e.t.Helper()
	if r.code != code {
		e.t.Fatalf("status %d, want %d\n%s", r.code, code, r.body)
	}
}

// waitFinished polls until the session is no longer queued or running.
func (e *testEnv) waitFinished(id string) map[string]any {
	e.t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		r := e.get("/sessions/" + id)
		e.wantStatus(r, http.StatusOK)
		s := r.json(e.t)
		if st := s["state"]; st != "queued" && st != "running" {
			if _, live := e.srv.Live(id); !live {
				return s
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	e.t.Fatalf("session %s did not finish", id)
	return nil
}

// waitLog polls the log of a session until it contains text.
func (e *testEnv) waitLog(id, text string) {
	e.t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if r := e.get("/sessions/" + id + "/log"); r.code == http.StatusOK && strings.Contains(string(r.body), text) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	e.t.Fatalf("the log of %s never contained %q", id, text)
}

// start creates a session and returns its ID.
func (e *testEnv) start(body any) string {
	e.t.Helper()
	r := e.request(http.MethodPost, "/sessions", body)
	e.wantStatus(r, http.StatusCreated)
	return r.json(e.t)["id"].(string)
}

func (e *testEnv) store() *history.Store {
	e.t.Helper()
	b, err := core.NewBase(e.srv.env)
	if err != nil {
		e.t.Fatal(err)
	}
	return history.New(b.StateDir(), e.srv.validator)
}

// recorder is a Broadcaster that remembers everything it is told.
type recorder struct {
	mu       sync.Mutex
	outputs  []recordedOutput
	events   []recordedEvent
	seqs     map[string][]int // sequence numbers per session, in call order
	sessions []Session
	checks   []CheckSummary
	settings [][]string
	plugins  []recordedPlugins
	restarts int
}

type recordedPlugins struct {
	ids, repositories []string
}

type recordedOutput struct {
	id   string
	seq  int
	data string
}

type recordedEvent struct {
	id string
	ev history.Event
}

func (r *recorder) SessionOutput(id string, seq int, data []byte) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.outputs = append(r.outputs, recordedOutput{id, seq, string(data)})
	r.addSeq(id, seq)
}

func (r *recorder) SessionEvent(id string, ev history.Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, recordedEvent{id, ev})
	r.addSeq(id, ev.Seq)
}

func (r *recorder) addSeq(id string, seq int) {
	if r.seqs == nil {
		r.seqs = map[string][]int{}
	}
	r.seqs[id] = append(r.seqs[id], seq)
}

func (r *recorder) SessionChanged(s Session) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sessions = append(r.sessions, s)
}

func (r *recorder) CheckChanged(c CheckSummary) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.checks = append(r.checks, c)
}

func (r *recorder) SettingsChanged(keys []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.settings = append(r.settings, keys)
}

func (r *recorder) PluginsChanged(ids, repositories []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.plugins = append(r.plugins, recordedPlugins{ids, repositories})
}

func (r *recorder) RestartRequested() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.restarts++
}

func (r *recorder) restartCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.restarts
}

func jsonUnmarshal(data []byte, v any) error { return json.Unmarshal(data, v) }

func sorted(s []string) bool { return slices.IsSorted(s) }

// newUIRequest builds a request for a page of the UI.
func newUIRequest(path string) (*http.Request, *httptest.ResponseRecorder) {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Host = testListen
	return req, httptest.NewRecorder()
}
