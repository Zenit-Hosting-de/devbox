package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/Zenit-Hosting-de/devbox/internal/schema"
)

type fixture struct {
	t      *testing.T
	runner *Runner
	dir    string // plugin directory
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	v, err := schema.New()
	if err != nil {
		t.Fatal(err)
	}
	base := t.TempDir()
	env := Env{
		Version:    "1.0.0",
		Bin:        "/usr/local/bin/devbox",
		Home:       filepath.Join(base, "home"),
		User:       "tester",
		Path:       os.Getenv("PATH"),
		Lang:       "C.UTF-8",
		Term:       "xterm-256color",
		RuntimeDir: filepath.Join(base, "run"),
		StateDir:   filepath.Join(base, "state"),
		CacheDir:   filepath.Join(base, "cache"),
	}
	if err := os.MkdirAll(env.Home, 0o700); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(base, "plugins", "demo")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	return &fixture{t: t, runner: New(env, v), dir: dir}
}

func (f *fixture) script(name, body string) {
	f.t.Helper()
	src := "#!/usr/bin/env bash\nset -euo pipefail\n" + body + "\n"
	if err := os.WriteFile(filepath.Join(f.dir, name), []byte(src), 0o700); err != nil { //nolint:gosec // test scripts must be executable
		f.t.Fatal(err)
	}
}

func (f *fixture) spec(kind, name string) Spec {
	s := Spec{
		Kind: kind, PluginID: "demo", PluginDir: f.dir, Name: "run", Command: []string{name},
		Host: map[string]any{"distro": "debian", "distroVersion": "13", "arch": "amd64"},
	}
	if kind != KindCheck {
		s.SessionID = NewSessionID(time.Now())
	}
	return s
}

type capture struct {
	mu   sync.Mutex
	out  bytes.Buffer
	msgs []map[string]any
}

func (c *capture) callbacks() Callbacks {
	return Callbacks{
		Output:  writerFunc(func(p []byte) { c.mu.Lock(); c.out.Write(p); c.mu.Unlock() }),
		Message: func(m map[string]any) { c.mu.Lock(); c.msgs = append(c.msgs, m); c.mu.Unlock() },
	}
}

type writerFunc func([]byte)

func (w writerFunc) Write(p []byte) (int, error) { w(p); return len(p), nil }

func TestExitCodes(t *testing.T) {
	f := newFixture(t)
	tests := []struct {
		code  int
		state string
	}{
		{0, StateSuccess},
		{1, StateFailed},
		{2, StateFailed},
		{3, StateBlocked},
		{4, StateStopped},
		{130, StateStopped},
		{143, StateStopped},
		{7, StateFailed},
	}
	for _, tt := range tests {
		t.Run(strconv.Itoa(tt.code), func(t *testing.T) {
			name := "exit" + strconv.Itoa(tt.code) + ".sh"
			f.script(name, "exit "+strconv.Itoa(tt.code))
			res, err := f.runner.Run(context.Background(), f.spec(KindAction, name), Callbacks{})
			if err != nil {
				t.Fatal(err)
			}
			if res.State != tt.state || res.ExitCode != tt.code {
				t.Errorf("state %s, code %d; want %s, %d", res.State, res.ExitCode, tt.state, tt.code)
			}
		})
	}
	f.script("killed.sh", "kill -TERM $$")
	res, err := f.runner.Run(context.Background(), f.spec(KindAction, "killed.sh"), Callbacks{})
	if err != nil {
		t.Fatal(err)
	}
	if res.State != StateStopped || res.ExitCode != 128+int(syscall.SIGTERM) {
		t.Errorf("killed by SIGTERM: %s %d", res.State, res.ExitCode)
	}
}

func TestMessagesAndOutput(t *testing.T) {
	f := newFixture(t)
	f.script("act.sh", `echo "plain output"
printf '%s\n' '{"type":"step","id":"download","label":"Download","state":"running"}' >&3
printf '%s\n' '{"type":"change","kind":"file","op":"create","target":"~/x","summary":"created"}' >&3
printf '%s\n' '{"type":"bogus"}' >&3
printf '%s\n' 'not json' >&3
printf '%s\n' '{"type":"result","summary":"done","data":{"v":1}}' >&3
printf '%s\n' '{"type":"result","summary":"again"}' >&3`)
	var c capture
	res, err := f.runner.Run(context.Background(), f.spec(KindAction, "act.sh"), c.callbacks())
	if err != nil {
		t.Fatal(err)
	}
	if res.State != StateSuccess {
		t.Fatalf("state %s", res.State)
	}
	if !strings.Contains(c.out.String(), "plain output") {
		t.Errorf("output = %q", c.out.String())
	}
	if len(c.msgs) != 3 || res.Changes != 1 || res.Result["summary"] != "done" {
		t.Errorf("messages %v, changes %d, result %v", c.msgs, res.Changes, res.Result)
	}
	if len(res.Warnings) != 3 {
		t.Errorf("warnings = %q, want 3 (bogus, not json, second result)", res.Warnings)
	}
}

// TestSecretsNeverLeak is the AP-05 criterion: a secret appears in no output and no
// message, even when a plugin prints it, also split across writes.
func TestSecretsNeverLeak(t *testing.T) {
	f := newFixture(t)
	const secret = "s3cr3t-T0KEN-value"
	f.script("leak.sh", `cat "$DEVBOX_REQUEST"; echo
tok=$(grep -o '"secret":"[^"]*"' "$DEVBOX_REQUEST" | cut -d'"' -f4)
printf 'half: %s' "${tok:0:7}"; sleep 0.1; printf '%s\n' "${tok:7}"
printf '{"type":"log","level":"info","text":"token %s"}\n' "$tok" >&3
printf '{"type":"result","summary":"used %s"}\n' "$tok" >&3`)
	spec := f.spec(KindApply, "leak.sh")
	spec.Credential = map[string]any{"instance": "company-registry", "type": "registry", "fields": map[string]any{"url": "https://nexus.example.com"}, "options": map[string]any{}, "secret": secret}
	var c capture
	res, err := f.runner.Run(context.Background(), spec, c.callbacks())
	if err != nil {
		t.Fatal(err)
	}
	if res.Result == nil {
		t.Fatal("no result message")
	}
	msgs, err := json.Marshal(c.msgs)
	if err != nil {
		t.Fatal(err)
	}
	all := c.out.String() + "\n" + string(msgs)
	if strings.Contains(all, secret) || strings.Contains(all, secret[:7]+secret[7:]) {
		t.Fatalf("secret leaked:\n%s", all)
	}
	if strings.Count(all, Mask) < 4 {
		t.Errorf("want the secret masked in output, split output, log and result:\n%s", all)
	}
}

func TestEnvironmentAndRequestFile(t *testing.T) {
	f := newFixture(t)
	t.Setenv("DEVBOX_TEST_LEAK", "parent-only")
	f.script("env.sh", `env | sort > "$HOME/env.txt"
stat -c %a "$DEVBOX_REQUEST" > "$HOME/mode.txt"
echo "$DEVBOX_REQUEST" > "$HOME/req.txt"
[ -d "$DEVBOX_STATE_DIR" ] && [ -d "$DEVBOX_CACHE_DIR" ]
ls -l /proc/self/fd/3 > /dev/null`)
	spec := f.spec(KindAction, "env.sh")
	spec.DryRun, spec.Lang = true, "de"
	spec.ExtraEnv = []string{"HTTPS_PROXY=http://proxy.example.com:3128"}
	res, err := f.runner.Run(context.Background(), spec, Callbacks{})
	if err != nil || res.State != StateSuccess {
		t.Fatalf("run: %v %+v", err, res)
	}
	read := func(name string) string {
		b, err := os.ReadFile(filepath.Join(f.runner.Env.Home, name))
		if err != nil {
			t.Fatal(err)
		}
		return strings.TrimSpace(string(b))
	}
	env := read("env.txt")
	for _, want := range []string{"DEVBOX_PROTOCOL=1", "DEVBOX_MSG_FD=3", "DEVBOX_DRY_RUN=1", "DEVBOX_YES=0", "DEVBOX_LANG=de", "DEVBOX_PLUGIN_ID=demo", "DEVBOX_SESSION_ID=" + spec.SessionID, "HTTPS_PROXY=http://proxy.example.com:3128", "USER=tester"} {
		if !strings.Contains(env, want+"\n") && !strings.HasSuffix(env, want) {
			t.Errorf("environment lacks %s", want)
		}
	}
	if strings.Contains(env, "DEVBOX_TEST_LEAK") {
		t.Error("the parent environment leaks into the plugin")
	}
	if mode := read("mode.txt"); mode != "600" {
		t.Errorf("request file mode %s, want 600", mode)
	}
	if _, err := os.Stat(read("req.txt")); !os.IsNotExist(err) {
		t.Errorf("request file not removed after the run: %v", err)
	}
}

// TestCancelStopsProcessGroup is the AP-05 criterion: cancelling ends the whole process
// group in under 11 seconds, even when the script ignores SIGINT and SIGTERM.
func TestCancelStopsProcessGroup(t *testing.T) {
	if testing.Short() {
		t.Skip("takes about 10 s")
	}
	f := newFixture(t)
	f.script("stubborn.sh", `trap '' INT TERM
sleep 300 &
echo $! > "$HOME/child.pid"
while true; do sleep 1; done`)
	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan struct{})
	var res *Result
	done := make(chan error, 1)
	go func() {
		var err error
		res, err = f.runner.Run(ctx, f.spec(KindAction, "stubborn.sh"), Callbacks{Started: func() { close(started) }})
		done <- err
	}()
	<-started
	pidFile := filepath.Join(f.runner.Env.Home, "child.pid")
	for i := 0; i < 50 && !fileExists(pidFile); i++ {
		time.Sleep(20 * time.Millisecond)
	}
	begin := time.Now()
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	elapsed := time.Since(begin)
	if elapsed >= 11*time.Second {
		t.Errorf("cancel took %v, want < 11s", elapsed)
	}
	if res.State != StateStopped {
		t.Errorf("state %s, want stopped", res.State)
	}
	b, _ := os.ReadFile(pidFile)
	pid, _ := strconv.Atoi(strings.TrimSpace(string(b)))
	time.Sleep(100 * time.Millisecond)
	if pid > 0 && syscall.Kill(pid, 0) == nil {
		t.Errorf("child %d of the process group still runs", pid)
	}
}

func TestTimeout(t *testing.T) {
	f := newFixture(t)
	f.runner.Grace = 200 * time.Millisecond
	f.script("slow.sh", "sleep 30")
	spec := f.spec(KindAction, "slow.sh")
	spec.Timeout = 300 * time.Millisecond
	res, err := f.runner.Run(context.Background(), spec, Callbacks{})
	if err != nil {
		t.Fatal(err)
	}
	if res.State != StateTimeout {
		t.Errorf("state %s, want timeout", res.State)
	}
}

func TestLocksQueueSecondAction(t *testing.T) {
	f := newFixture(t)
	f.script("hold.sh", "sleep 0.5")
	first, second := f.spec(KindAction, "hold.sh"), f.spec(KindAction, "hold.sh")
	firstStarted := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, _ = f.runner.Run(context.Background(), first, Callbacks{Started: func() { close(firstStarted) }})
	}()
	<-firstStarted
	queued := false
	if _, err := f.runner.Run(context.Background(), second, Callbacks{Queued: func() { queued = true }}); err != nil {
		t.Fatal(err)
	}
	wg.Wait()
	if !queued {
		t.Error("second action of the same plugin was not queued")
	}

	// Named locks span plugins; free locks do not wait.
	release, err := f.runner.Locks.Acquire(context.Background(), []string{"system-packages"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	other := f.spec(KindAction, "hold.sh")
	other.PluginID, other.Locks = "other", []string{"system-packages"}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	res, err := f.runner.Run(ctx, other, Callbacks{})
	if err != nil || res.State != StateStopped {
		t.Errorf("waiting on a named lock until cancel: %v %+v", err, res)
	}
	release()
}

func TestCheck(t *testing.T) {
	f := newFixture(t)
	f.script("ok.sh", `echo "debug info" >&2
echo '{"status":"warn","summary":"Temurin 21.0.4","data":{"items":[]}}'`)
	var debug bytes.Buffer
	res, err := f.runner.Run(context.Background(), f.spec(KindCheck, "ok.sh"), Callbacks{Debug: &debug})
	if err != nil {
		t.Fatal(err)
	}
	if res.State != StateSuccess || res.Check["status"] != "warn" || !strings.Contains(debug.String(), "debug info") {
		t.Errorf("check: %+v, debug %q", res, debug.String())
	}
	f.script("bad.sh", `echo '{"status":"fine"}'`)
	res, err = f.runner.Run(context.Background(), f.spec(KindCheck, "bad.sh"), Callbacks{})
	if err != nil {
		t.Fatal(err)
	}
	if res.State != StateFailed || len(res.Warnings) != 1 {
		t.Errorf("invalid check output: %+v", res)
	}
}

func TestSessionID(t *testing.T) {
	re := regexp.MustCompile(`^s_[0-9A-HJKMNP-TV-Z]{26}$`)
	a, b := NewSessionID(time.Now()), NewSessionID(time.Now().Add(time.Millisecond))
	if !re.MatchString(a) || !re.MatchString(b) || a >= b {
		t.Errorf("ids %s %s: want valid and time-ordered", a, b)
	}
}

func TestMasker(t *testing.T) {
	var out bytes.Buffer
	m := newMasker(&out, []string{"abcdef", "abc"})
	for _, chunk := range []string{"x ab", "cdef y ab", "c z a"} {
		_, _ = m.Write([]byte(chunk))
	}
	_ = m.Flush()
	if got, want := out.String(), "x "+Mask+" y "+Mask+" z a"; got != want {
		t.Errorf("masked = %q, want %q", got, want)
	}
}

func TestTerminalResize(t *testing.T) {
	f := newFixture(t)
	f.script("winch.sh", `trap 'stty size' WINCH
echo ready
for _ in $(seq 1 100); do sleep 0.05; done`)
	spec := f.spec(KindAction, "winch.sh")
	spec.Cols, spec.Rows = 80, 24

	var (
		mu       sync.Mutex
		out      bytes.Buffer
		term     Terminal
		started  bool
		order    []string
		terminal int
	)
	cb := Callbacks{
		Output: writerFunc(func(p []byte) { mu.Lock(); out.Write(p); mu.Unlock() }),
		Terminal: func(tm Terminal) {
			mu.Lock()
			defer mu.Unlock()
			if tm == nil { // the PTY is about to be closed
				order = append(order, "closed")
				return
			}
			term, terminal = tm, terminal+1
			order = append(order, "terminal")
		},
		Started: func() {
			mu.Lock()
			defer mu.Unlock()
			started = true
			order = append(order, "started")
		},
	}
	seen := func(text string) bool {
		mu.Lock()
		defer mu.Unlock()
		return strings.Contains(out.String(), text)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan *Result, 1)
	go func() {
		res, err := f.runner.Run(ctx, spec, cb)
		if err != nil {
			t.Error(err)
		}
		done <- res
	}()
	waitFor(t, func() bool { return seen("ready") })

	mu.Lock()
	tm := term
	mu.Unlock()
	if tm == nil || !started || terminal != 1 || strings.Join(order, ",") != "terminal,started" {
		t.Fatalf("terminal %v, started %v, calls %d, order %v", tm, started, terminal, order)
	}
	if err := tm.Resize(0, 10); err == nil {
		t.Error("a zero size was accepted")
	}
	if err := tm.Resize(100, 40); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return seen("40 100") })

	cancel()
	res := <-done
	if res.State != StateStopped {
		t.Errorf("state = %s", res.State)
	}
	if err := tm.Resize(90, 30); err == nil {
		t.Error("resizing a finished terminal must fail")
	}
	mu.Lock()
	defer mu.Unlock()
	if got := strings.Join(order, ","); got != "terminal,started,closed" {
		t.Errorf("order = %s, want the handle to be dropped once at the end", got)
	}
}

// Resizing while the process ends and after the PTY was closed must neither race with
// the close nor touch a reused descriptor (run with -race).
func TestTerminalResizeAroundTheEnd(t *testing.T) {
	f := newFixture(t)
	f.script("short.sh", `echo ready
sleep 0.3`)
	spec := f.spec(KindAction, "short.sh")

	var (
		mu     sync.Mutex
		term   Terminal
		out    bytes.Buffer
		closed bool
	)
	cb := Callbacks{
		Output: writerFunc(func(p []byte) { mu.Lock(); out.Write(p); mu.Unlock() }),
		Terminal: func(tm Terminal) {
			mu.Lock()
			defer mu.Unlock()
			if tm == nil {
				closed = true
				return
			}
			term = tm
		},
	}
	done := make(chan *Result, 1)
	go func() {
		res, err := f.runner.Run(context.Background(), spec, cb)
		if err != nil {
			t.Error(err)
		}
		done <- res
	}()
	waitFor(t, func() bool { mu.Lock(); defer mu.Unlock(); return term != nil })
	mu.Lock()
	tm := term
	mu.Unlock()

	// Hammer Resize from several goroutines until the run is over and the PTY closed.
	var wg sync.WaitGroup
	stop := make(chan struct{})
	for i := range uint16(4) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for n := uint16(0); ; n++ {
				select {
				case <-stop:
					return
				default:
				}
				_ = tm.Resize(60+i+n%20, 20+n%10)
			}
		}()
	}
	res := <-done
	close(stop)
	wg.Wait()
	if res.State != StateSuccess {
		t.Errorf("state = %s", res.State)
	}
	mu.Lock()
	defer mu.Unlock()
	if !closed {
		t.Error("the receiver was not told that the terminal is gone")
	}
	if err := tm.Resize(90, 30); !errors.Is(err, errTerminalClosed) {
		t.Errorf("resize after the end = %v, want errTerminalClosed", err)
	}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition not met in time")
}
