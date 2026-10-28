// Package runner runs checks and actions of plugins as separate processes, following
// the plugin protocol (docs/concept.md, "Plugin-Protokoll"): a request file, a fixed
// environment, a PTY for actions, JSON Lines messages on file descriptor 3, exit codes,
// cancellation of the whole process group, timeouts, locks and secret masking.
package runner

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"

	"github.com/Zenit-Hosting-de/devbox/internal/jsonfile"
	"github.com/Zenit-Hosting-de/devbox/internal/schema"
)

// Protocol is the plugin protocol version.
const Protocol = 1

// MsgFD is the file descriptor for protocol messages.
const MsgFD = 3

// Kinds of runs.
const (
	KindCheck  = "check"
	KindAction = "action"
	KindApply  = "apply"
	KindRemove = "remove"
	KindEvent  = "event"
)

// Session states (docs/concept.md, exit code table).
const (
	StateQueued  = "queued"
	StateRunning = "running"
	StateSuccess = "success"
	StateFailed  = "failed"
	StateBlocked = "blocked"
	StateStopped = "stopped"
	StateTimeout = "timeout"
)

// Default time limits when a manifest sets none.
const (
	DefaultCheckTimeout  = 30 * time.Second
	DefaultActionTimeout = time.Hour
)

// Spec describes one run.
type Spec struct {
	Kind      string
	SessionID string // empty for checks
	PluginID  string
	PluginDir string
	Name      string   // check, action, credential type or event name
	Command   []string // "run" of the manifest
	Params    map[string]any
	Settings  map[string]any
	// Credential is the credential instance for apply and remove, including the secret.
	Credential map[string]any
	Event      map[string]any
	DryRun     bool
	Yes        bool
	Lang       string
	Host       map[string]any
	Timeout    time.Duration
	Locks      []string
	// Secrets are masked in output and messages (the credential secret is added).
	Secrets []string
	Cols    uint16
	Rows    uint16
	// Input is copied to the PTY of an action, e.g. for a sudo password.
	Input io.Reader
	// ExtraEnv adds variables such as the proxy settings (NAME=value).
	ExtraEnv []string
}

// Env is the environment shared by all runs.
type Env struct {
	Version    string
	Bin        string // path of the devbox binary (DEVBOX_BIN)
	Home       string
	User       string
	Path       string // PATH for plugins
	Lang       string // LANG
	Term       string // TERM for PTYs
	RuntimeDir string // directory for request files ($XDG_RUNTIME_DIR/devbox)
	StateDir   string // ~/.local/state/devbox
	CacheDir   string // ~/.cache/devbox
}

// Callbacks receive what a run produces. All are optional.
type Callbacks struct {
	// Output receives the terminal output of an action, secrets masked.
	Output io.Writer
	// Message receives every valid protocol message, secrets masked.
	Message func(msg map[string]any)
	// Queued is called when the run waits for a lock.
	Queued func()
	// Started is called when the process runs.
	Started func()
	// Terminal is called once for an action, right after its PTY was created and
	// before Started, with a handle to change the size of the terminal. The handle
	// is only valid while the action runs; later calls return an error.
	Terminal func(t Terminal)
	// Debug receives the stderr of checks.
	Debug io.Writer
}

// Terminal is the handle of the PTY of a running action. It does not expose the
// terminal device itself.
type Terminal interface {
	// Resize sets the size of the terminal; the process gets SIGWINCH.
	Resize(cols, rows uint16) error
}

// ptyTerminal resizes the PTY master.
type ptyTerminal struct{ tty *os.File }

// Resize implements Terminal.
func (t ptyTerminal) Resize(cols, rows uint16) error {
	if cols == 0 || rows == 0 {
		return errors.New("the terminal size must not be zero")
	}
	if err := pty.Setsize(t.tty, &pty.Winsize{Cols: cols, Rows: rows}); err != nil {
		return fmt.Errorf("resize terminal: %w", err)
	}
	return nil
}

// Result is the outcome of a run.
type Result struct {
	State    string
	ExitCode int // -1 when the process did not exit by itself
	// Result is the "result" message of an action.
	Result map[string]any
	// Check is the result object of a check.
	Check map[string]any
	// Changes counts "change" messages.
	Changes int
	// Warnings lists protocol violations (invalid messages, a second result …).
	Warnings []string
	Started  time.Time
	Ended    time.Time
}

// Runner starts plugin processes.
type Runner struct {
	Env       Env
	Locks     *Locks
	Validator *schema.Validator
	// Grace is the time between SIGINT, SIGTERM and SIGKILL (5 s by default).
	Grace time.Duration
}

// New returns a runner.
func New(env Env, v *schema.Validator) *Runner {
	return &Runner{Env: env, Locks: NewLocks(), Validator: v, Grace: 5 * time.Second}
}

// Run executes one check or action. Cancelling ctx stops it (state "stopped").
func (r *Runner) Run(ctx context.Context, spec Spec, cb Callbacks) (*Result, error) {
	if len(spec.Command) == 0 {
		return nil, errors.New("empty command")
	}
	if spec.Kind != KindCheck {
		release, err := r.Locks.Acquire(ctx, append([]string{"plugin:" + spec.PluginID}, spec.Locks...), cb.Queued)
		if err != nil {
			return &Result{State: StateStopped, ExitCode: -1}, nil
		}
		defer release()
	}
	secrets := append([]string(nil), spec.Secrets...)
	if s, ok := spec.Credential["secret"].(string); ok {
		secrets = append(secrets, s)
	}

	reqPath, err := r.writeRequest(spec)
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.Remove(reqPath) }()

	cmd, err := r.command(spec, reqPath)
	if err != nil {
		return nil, err
	}
	timeout := spec.Timeout
	if timeout <= 0 {
		timeout = DefaultActionTimeout
		if spec.Kind == KindCheck {
			timeout = DefaultCheckTimeout
		}
	}
	if spec.Kind == KindCheck {
		return r.runCheck(ctx, cmd, timeout, secrets, cb)
	}
	return r.runAction(ctx, cmd, spec, timeout, secrets, cb)
}

func (r *Runner) writeRequest(spec Spec) (string, error) {
	dir := r.Env.RuntimeDir
	if dir == "" {
		return "", errors.New("no runtime directory for request files")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("create %s: %w", dir, err)
	}
	req := map[string]any{
		"protocol":   Protocol,
		"kind":       spec.Kind,
		"plugin":     spec.PluginID,
		"name":       spec.Name,
		"params":     orEmpty(spec.Params),
		"settings":   orEmpty(spec.Settings),
		"credential": nil,
		"dryRun":     spec.DryRun,
		"host":       orEmpty(spec.Host),
	}
	if spec.Credential != nil {
		req["credential"] = spec.Credential
	}
	if spec.Event != nil {
		req["event"] = spec.Event
	}
	if spec.Lang != "" {
		req["lang"] = spec.Lang
	}
	if r.Validator != nil {
		if err := r.Validator.Validate("request", jsonfile.Plain(req)); err != nil {
			return "", fmt.Errorf("request: %w", err)
		}
	}
	data, err := json.Marshal(req)
	if err != nil {
		return "", err
	}
	f, err := os.CreateTemp(dir, "request-*.json")
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	if err := f.Chmod(0o600); err != nil {
		return "", err
	}
	if _, err := f.Write(data); err != nil {
		return "", err
	}
	return f.Name(), nil
}

func orEmpty(m map[string]any) map[string]any {
	if m == nil {
		return map[string]any{}
	}
	return m
}

func boolFlag(b bool) string {
	if b {
		return "1"
	}
	return "0"
}

// command builds the process with the protocol environment. The first element of
// "run" is relative to the plugin directory if such a file exists, else a command
// in PATH.
func (r *Runner) command(spec Spec, reqPath string) (*exec.Cmd, error) {
	name := spec.Command[0]
	if local := filepath.Join(spec.PluginDir, name); !filepath.IsAbs(name) && fileExists(local) {
		name = local
	} else if !strings.Contains(name, "/") {
		found, err := lookPath(name, r.Env.Path)
		if err != nil {
			return nil, err
		}
		name = found
	}
	stateDir := filepath.Join(r.Env.StateDir, "plugins", spec.PluginID)
	cacheDir := filepath.Join(r.Env.CacheDir, "plugins", spec.PluginID)
	backupDir := ""
	if spec.SessionID != "" {
		backupDir = filepath.Join(r.Env.StateDir, "backups", spec.SessionID)
	}
	for _, d := range []string{stateDir, cacheDir} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			return nil, fmt.Errorf("create %s: %w", d, err)
		}
	}
	lang := spec.Lang
	env := []string{
		"PATH=" + r.Env.Path,
		"HOME=" + r.Env.Home,
		"USER=" + r.Env.User,
		"LANG=" + r.Env.Lang,
		"TERM=" + r.Env.Term,
		fmt.Sprintf("DEVBOX_PROTOCOL=%d", Protocol),
		"DEVBOX_VERSION=" + r.Env.Version,
		"DEVBOX_PLUGIN_ID=" + spec.PluginID,
		"DEVBOX_PLUGIN_DIR=" + spec.PluginDir,
		"DEVBOX_SESSION_ID=" + spec.SessionID,
		"DEVBOX_REQUEST=" + reqPath,
		fmt.Sprintf("DEVBOX_MSG_FD=%d", MsgFD),
		"DEVBOX_DRY_RUN=" + boolFlag(spec.DryRun),
		"DEVBOX_YES=" + boolFlag(spec.Yes),
		"DEVBOX_LANG=" + lang,
		"DEVBOX_STATE_DIR=" + stateDir,
		"DEVBOX_CACHE_DIR=" + cacheDir,
		"DEVBOX_BACKUP_DIR=" + backupDir,
		"DEVBOX_BIN=" + r.Env.Bin,
	}
	env = append(env, spec.ExtraEnv...)
	cmd := exec.Command(name, spec.Command[1:]...) //nolint:gosec // plugin commands come from validated manifests
	cmd.Dir = r.Env.Home
	cmd.Env = env
	return cmd, nil
}

func fileExists(p string) bool {
	info, err := os.Stat(p)
	return err == nil && !info.IsDir()
}

func lookPath(name, pathEnv string) (string, error) {
	for _, dir := range filepath.SplitList(pathEnv) {
		if dir == "" {
			continue
		}
		p := filepath.Join(dir, name)
		if info, err := os.Stat(p); err == nil && !info.IsDir() && info.Mode().Perm()&0o111 != 0 {
			return p, nil
		}
	}
	return "", fmt.Errorf("%s: not found in PATH", name)
}

// runCheck runs a check without PTY: one JSON object on stdout, stderr to Debug.
func (r *Runner) runCheck(ctx context.Context, cmd *exec.Cmd, timeout time.Duration, secrets []string, cb Callbacks) (*Result, error) {
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	debug := cb.Debug
	if debug == nil {
		debug = io.Discard
	}
	m := newMasker(debug, secrets)
	cmd.Stderr = m
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	res := &Result{Started: time.Now()}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start %s: %w", cmd.Path, err)
	}
	if cb.Started != nil {
		cb.Started()
	}
	state, code := r.wait(ctx, cmd, timeout)
	_ = m.Flush()
	res.Ended, res.State, res.ExitCode = time.Now(), state, code
	if state != StateSuccess {
		return res, nil
	}
	out := MaskString(stdout.String(), secrets)
	doc, err := schema.Decode([]byte(out))
	if err == nil && r.Validator != nil {
		err = r.Validator.Validate("check-result", doc)
	}
	if err != nil {
		res.State = StateFailed
		res.Warnings = append(res.Warnings, "check output: "+err.Error())
		return res, nil
	}
	res.Check, _ = doc.(map[string]any)
	return res, nil
}

// runAction runs an action in a PTY and reads messages from file descriptor 3.
func (r *Runner) runAction(ctx context.Context, cmd *exec.Cmd, spec Spec, timeout time.Duration, secrets []string, cb Callbacks) (*Result, error) {
	msgR, msgW, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	cmd.ExtraFiles = []*os.File{msgW} // becomes fd 3
	size := &pty.Winsize{Cols: spec.Cols, Rows: spec.Rows}
	if size.Cols == 0 || size.Rows == 0 {
		size = &pty.Winsize{Cols: 120, Rows: 32}
	}
	res := &Result{Started: time.Now()}
	tty, err := pty.StartWithSize(cmd, size) // new session: the child leads its process group
	_ = msgW.Close()
	if err != nil {
		_ = msgR.Close()
		return nil, fmt.Errorf("start %s: %w", cmd.Path, err)
	}
	defer func() { _ = tty.Close() }()
	if cb.Terminal != nil {
		cb.Terminal(ptyTerminal{tty: tty})
	}
	if cb.Started != nil {
		cb.Started()
	}
	if spec.Input != nil {
		go func() { _, _ = io.Copy(tty, spec.Input) }()
	}

	out := cb.Output
	if out == nil {
		out = io.Discard
	}
	m := newMasker(out, secrets)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, _ = io.Copy(m, tty) // ends with EIO when the child and its group exit
	}()
	var mu sync.Mutex
	go func() {
		defer wg.Done()
		r.readMessages(msgR, secrets, res, &mu, cb.Message)
	}()

	state, code := r.wait(ctx, cmd, timeout)
	// The PTY stays open while grandchildren hold it; give the readers a moment.
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		_ = tty.Close()
		_ = msgR.Close()
		<-done
	}
	_ = msgR.Close()
	_ = m.Flush()
	mu.Lock()
	defer mu.Unlock()
	res.Ended, res.State, res.ExitCode = time.Now(), state, code
	return res, nil
}

func (r *Runner) readMessages(rd io.Reader, secrets []string, res *Result, mu *sync.Mutex, emit func(map[string]any)) {
	sc := bufio.NewScanner(rd)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		line := MaskString(strings.TrimSpace(sc.Text()), secrets)
		if line == "" {
			continue
		}
		doc, err := schema.Decode([]byte(line))
		if err == nil && r.Validator != nil {
			err = r.Validator.Validate("event.message", doc)
		}
		mu.Lock()
		if err != nil {
			res.Warnings = append(res.Warnings, "message ignored: "+firstLine(err.Error()))
			mu.Unlock()
			continue
		}
		msg, _ := doc.(map[string]any)
		switch msg["type"] {
		case "change":
			res.Changes++
		case "result":
			if res.Result != nil {
				res.Warnings = append(res.Warnings, "second result message ignored")
				mu.Unlock()
				continue
			}
			res.Result = msg
		}
		mu.Unlock()
		if emit != nil {
			emit(msg)
		}
	}
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return line
}

// wait waits for the process; on cancellation or timeout it stops the process group:
// SIGINT, after Grace SIGTERM, after another Grace SIGKILL.
func (r *Runner) wait(ctx context.Context, cmd *exec.Cmd, timeout time.Duration) (state string, code int) {
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	var stopState string
	select {
	case err := <-exited:
		return stateForExit(err)
	case <-ctx.Done():
		stopState = StateStopped
	case <-timer.C:
		stopState = StateTimeout
	}
	pgid := cmd.Process.Pid
	for _, sig := range []syscall.Signal{syscall.SIGINT, syscall.SIGTERM, syscall.SIGKILL} {
		_ = syscall.Kill(-pgid, sig)
		select {
		case err := <-exited:
			_ = syscall.Kill(-pgid, syscall.SIGKILL) // leftovers of the group
			_, c := stateForExit(err)
			return stopState, c
		case <-time.After(r.Grace):
		}
	}
	err := <-exited
	_, c := stateForExit(err)
	return stopState, c
}

// stateForExit maps an exit status to a session state (plugin protocol, exit codes).
func stateForExit(err error) (string, int) {
	if err == nil {
		return StateSuccess, 0
	}
	var ee *exec.ExitError
	if !errors.As(err, &ee) {
		return StateFailed, -1
	}
	if ws, ok := ee.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
		code := 128 + int(ws.Signal())
		if ws.Signal() == syscall.SIGINT || ws.Signal() == syscall.SIGTERM {
			return StateStopped, code
		}
		return StateFailed, code
	}
	code := ee.ExitCode()
	switch code {
	case 1, 2:
		return StateFailed, code
	case 3:
		return StateBlocked, code
	case 4, 130, 143:
		return StateStopped, code
	default:
		return StateFailed, code
	}
}
