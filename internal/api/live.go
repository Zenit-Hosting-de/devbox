package api

import (
	"context"
	"errors"
	"io"
	"sort"
	"sync"
	"time"

	"github.com/Zenit-Hosting-de/devbox/internal/history"
	"github.com/Zenit-Hosting-de/devbox/internal/runner"
)

// Broadcaster receives what happens in the core, for the WebSocket hub. The methods
// must not block: the hub enqueues. All calls for one session are made in sequence
// order (the output and the events of a session are serialised by one mutex). Data is
// copied before it is handed over.
type Broadcaster interface {
	SessionOutput(id string, seq int, data []byte)
	SessionEvent(id string, ev history.Event)
	SessionChanged(s Session)
	CheckChanged(c CheckSummary)
	SettingsChanged(keys []string)
	// PluginsChanged reports that plugins were switched on or off or that the plugins of
	// repositories changed: ids are the plugins concerned, repositories the names of
	// the repositories concerned (both may be empty).
	PluginsChanged(ids, repositories []string)
	// RestartRequested reports that the core restarts itself with a new binary.
	RestartRequested()
}

// NopBroadcaster ignores everything; it is the default.
type NopBroadcaster struct{}

// SessionOutput implements Broadcaster.
func (NopBroadcaster) SessionOutput(string, int, []byte) {}

// SessionEvent implements Broadcaster.
func (NopBroadcaster) SessionEvent(string, history.Event) {}

// SessionChanged implements Broadcaster.
func (NopBroadcaster) SessionChanged(Session) {}

// CheckChanged implements Broadcaster.
func (NopBroadcaster) CheckChanged(CheckSummary) {}

// SettingsChanged implements Broadcaster.
func (NopBroadcaster) SettingsChanged([]string) {}

// PluginsChanged implements Broadcaster.
func (NopBroadcaster) PluginsChanged([]string, []string) {}

// RestartRequested implements Broadcaster.
func (NopBroadcaster) RestartRequested() {}

// Progress is the progress of a running session.
type Progress struct {
	Percent *int   `json:"percent"`
	Text    string `json:"text,omitempty"`
}

// Session is the summary of a session (api/openapi.json, Session).
type Session struct {
	ID        string    `json:"id"`
	Action    string    `json:"action"`
	Plugin    string    `json:"plugin,omitempty"`
	Group     string    `json:"group,omitempty"`
	Title     string    `json:"title,omitempty"`
	User      string    `json:"user,omitempty"`
	Origin    string    `json:"origin,omitempty"`
	State     string    `json:"state"`
	DryRun    bool      `json:"dryRun"`
	StartedAt string    `json:"startedAt,omitempty"`
	EndedAt   string    `json:"endedAt,omitempty"`
	ExitCode  *int      `json:"exitCode"`
	Changes   int       `json:"changes"`
	Summary   string    `json:"summary,omitempty"`
	Progress  *Progress `json:"progress,omitempty"`
	NextSeq   *int      `json:"nextSeq,omitempty"`
}

// sessionOf converts a history line.
func sessionOf(e history.Entry) Session {
	return Session{
		ID: e.ID, Action: e.Action, Plugin: e.Plugin, Group: e.Group, Title: e.Title, User: e.User,
		Origin: e.Origin, State: e.State, DryRun: e.DryRun, StartedAt: e.StartedAt, EndedAt: e.EndedAt,
		ExitCode: e.ExitCode, Changes: e.Changes, Summary: e.Summary,
	}
}

// errInputFull is returned when the terminal does not take input fast enough.
var errInputFull = errors.New("the session does not take input")

const inputTimeout = 5 * time.Second

// inputPipe feeds the terminal of a session. Unlike io.Pipe it buffers, so input sent
// while the session still waits for a lock does not block the writer.
type inputPipe struct {
	ch     chan []byte
	closed chan struct{}
	once   sync.Once
	rest   []byte
}

func newInputPipe() *inputPipe {
	return &inputPipe{ch: make(chan []byte, 64), closed: make(chan struct{})}
}

// Read implements io.Reader for runner.Spec.Input.
func (p *inputPipe) Read(b []byte) (int, error) {
	if len(p.rest) == 0 {
		select {
		case d := <-p.ch:
			p.rest = d
		case <-p.closed:
			return 0, io.EOF
		}
	}
	n := copy(b, p.rest)
	p.rest = p.rest[n:]
	return n, nil
}

func (p *inputPipe) write(d []byte) error {
	select {
	case <-p.closed:
		return io.ErrClosedPipe
	default:
	}
	c := append([]byte(nil), d...)
	timer := time.NewTimer(inputTimeout)
	defer timer.Stop()
	select {
	case p.ch <- c:
		return nil
	case <-p.closed:
		return io.ErrClosedPipe
	case <-timer.C:
		return errInputFull
	}
}

func (p *inputPipe) close() { p.once.Do(func() { close(p.closed) }) }

// LiveSession is a queued or running session.
type LiveSession struct {
	id     string
	ref    string
	plugin string
	title  string
	user   string
	origin string
	dryRun bool
	cancel context.CancelFunc
	input  *inputPipe

	mu       sync.Mutex
	state    string
	started  time.Time
	nextSeq  int
	logBytes int64
	progress *Progress
	changes  int
	final    *Session
	resizer  func(cols, rows uint16)
	// resizePending is set when a size was requested while there was no terminal yet.
	resizePending bool
	// terminalGone is set when the terminal was closed; it never gets a size again.
	terminalGone bool
	cols         uint16
	rows         uint16
}

// ID returns the session ID.
func (l *LiveSession) ID() string { return l.id }

// Session returns a snapshot of the summary.
func (l *LiveSession) Session() Session {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.final != nil {
		return *l.final
	}
	s := Session{
		ID: l.id, Action: l.ref, Plugin: l.plugin, Title: l.title, User: l.user, Origin: l.origin,
		State: l.state, DryRun: l.dryRun, Changes: l.changes,
	}
	if !l.started.IsZero() {
		s.StartedAt = history.Timestamp(l.started)
	}
	if l.progress != nil {
		p := *l.progress
		s.Progress = &p
	}
	next := l.nextSeq
	s.NextSeq = &next
	return s
}

// snapshot returns the next sequence number and the number of log bytes written so
// far, taken together.
func (l *LiveSession) snapshot() (nextSeq int, logBytes int64) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.nextSeq, l.logBytes
}

// WriteInput sends keyboard input to the terminal of the session. It fails when the
// session has finished.
func (l *LiveSession) WriteInput(p []byte) error { return l.input.write(p) }

// SetResizer sets the function that applies a new terminal size to the running
// process. A size requested before the process had a terminal is applied now.
func (l *LiveSession) SetResizer(f func(cols, rows uint16)) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.resizer = f
	if f != nil && l.resizePending {
		l.resizePending = false
		f(l.cols, l.rows)
	}
}

// terminalClosed drops the resizer for good: the terminal of the process is closed.
func (l *LiveSession) terminalClosed() {
	l.mu.Lock()
	l.terminalGone = true
	l.resizer = nil
	l.resizePending = false
	l.mu.Unlock()
}

// errNotRunning is returned by Resize when the session has no running process.
var errNotRunning = errors.New("the session is not running")

// Resize remembers the terminal size and applies it when the process has a terminal;
// otherwise the size is applied as soon as it has one. It fails once the session has
// finished or its terminal was closed.
func (l *LiveSession) Resize(cols, rows uint16) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.final != nil || l.terminalGone {
		return errNotRunning
	}
	l.cols, l.rows = cols, rows
	if l.resizer == nil {
		l.resizePending = true
		return nil
	}
	l.resizer(cols, rows)
	return nil
}

// Running reports whether the process of the session runs.
func (l *LiveSession) Running() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.final == nil && l.state == runner.StateRunning
}

// Cancel stops the session.
func (l *LiveSession) Cancel() { l.cancel() }

func (l *LiveSession) setState(state string, started time.Time) {
	l.mu.Lock()
	l.state = state
	if !started.IsZero() {
		l.started = started
	}
	l.mu.Unlock()
}

func (l *LiveSession) onOutput(seq, n int) {
	l.mu.Lock()
	l.nextSeq = seq + 1
	l.logBytes += int64(n)
	l.mu.Unlock()
}

// onEvent tracks the sequence number, the progress and the change count; it reports
// whether the summary changed.
func (l *LiveSession) onEvent(ev history.Event) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.nextSeq = ev.Seq + 1
	switch ev.Event["type"] {
	case "progress":
		p := &Progress{}
		p.Text, _ = ev.Event["text"].(string)
		if n, ok := ev.Event["percent"]; ok && n != nil {
			if v, ok := intOf(n); ok {
				p.Percent = &v
			}
		}
		l.progress = p
		return true
	case "change":
		l.changes++
		return true
	}
	return false
}

func intOf(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case float64:
		return int(n), true
	case interface{ Int64() (int64, error) }:
		i, err := n.Int64()
		return int(i), err == nil
	}
	return 0, false
}

func (l *LiveSession) finish(s Session) {
	l.mu.Lock()
	l.final = &s
	l.mu.Unlock()
	l.input.close()
}

// registry holds the live sessions.
type registry struct {
	mu   sync.Mutex
	byID map[string]*LiveSession
}

func newRegistry() *registry { return &registry{byID: map[string]*LiveSession{}} }

// add registers a session; it fails when the same action is already queued or running.
func (r *registry) add(l *LiveSession) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, o := range r.byID {
		if o.ref == l.ref {
			return conflict(l.ref + " is already queued or running as " + o.id)
		}
	}
	r.byID[l.id] = l
	return nil
}

func (r *registry) get(id string) (*LiveSession, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	l, ok := r.byID[id]
	return l, ok
}

func (r *registry) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.byID)
}

func (r *registry) remove(id string) {
	r.mu.Lock()
	delete(r.byID, id)
	r.mu.Unlock()
}

// list returns the live sessions, newest first.
func (r *registry) list() []*LiveSession {
	r.mu.Lock()
	out := make([]*LiveSession, 0, len(r.byID))
	for _, l := range r.byID {
		out = append(out, l)
	}
	r.mu.Unlock()
	sort.Slice(out, func(i, j int) bool { return out[i].id > out[j].id })
	return out
}

// newLive prepares a session before it runs.
func newLive(spec runner.Spec, ref, title, user, origin string, cancel context.CancelFunc) *LiveSession {
	return &LiveSession{
		id: spec.SessionID, ref: ref, plugin: spec.PluginID, title: title, user: user, origin: origin,
		dryRun: spec.DryRun, cancel: cancel, input: newInputPipe(), state: runner.StateQueued,
		cols: spec.Cols, rows: spec.Rows,
	}
}
