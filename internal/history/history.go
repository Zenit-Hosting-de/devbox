package history

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/Zenit-Hosting-de/devbox/internal/jsonfile"
	"github.com/Zenit-Hosting-de/devbox/internal/schema"
)

// Retention defaults, the same as the defaults of history.keepDays and
// history.keepSessions in schema/config.v1.json.
const (
	DefaultKeepDays     = 90
	DefaultKeepSessions = 500
)

// Origins of a session (schema/history-entry.v1.json).
const (
	OriginUI    = "ui"
	OriginCLI   = "cli"
	OriginTUI   = "tui"
	OriginAPI   = "api"
	OriginSetup = "setup"
	OriginGroup = "group"
	OriginEvent = "event"
)

var sessionID = regexp.MustCompile(`^s_[0-9A-HJKMNP-TV-Z]{10,26}$`)

// ValidID reports whether id is a session ID.
func ValidID(id string) bool { return sessionID.MatchString(id) }

// Entry is one line of the history, fields in schema order.
type Entry struct {
	ID        string `json:"id"`
	Action    string `json:"action"`
	Plugin    string `json:"plugin,omitempty"`
	Group     string `json:"group,omitempty"`
	Title     string `json:"title"`
	User      string `json:"user"`
	Origin    string `json:"origin"`
	DryRun    bool   `json:"dryRun"`
	StartedAt string `json:"startedAt"`
	EndedAt   string `json:"endedAt"`
	State     string `json:"state"`
	ExitCode  *int   `json:"exitCode"`
	Changes   int    `json:"changes"`
	Summary   string `json:"summary"`
}

// Timestamp formats a time for an entry: RFC 3339, UTC, whole seconds.
func Timestamp(t time.Time) string { return t.UTC().Format(time.RFC3339) }

// Store keeps the history, the session files and the backups below the state
// directory (~/.local/state/devbox).
type Store struct {
	dir       string
	validator *schema.Validator
}

// New returns the store for stateDir. The validator checks every entry before it is
// written; nil skips the check.
func New(stateDir string, v *schema.Validator) *Store {
	return &Store{dir: stateDir, validator: v}
}

// LogPath is the raw terminal output of a session.
func (s *Store) LogPath(id string) string {
	return filepath.Join(s.dir, "sessions", id+".log")
}

// EventsPath holds all protocol messages of a session (schema/event.v1.json).
func (s *Store) EventsPath(id string) string {
	return filepath.Join(s.dir, "sessions", id+".events.jsonl")
}

// BackupDir holds the originals of files a session changed, at their absolute path.
func (s *Store) BackupDir(id string) string {
	return filepath.Join(s.dir, "backups", id)
}

func (s *Store) historyDir() string { return filepath.Join(s.dir, "history") }

// Session writes the output and events of one running session.
type Session struct {
	id     string
	mu     sync.Mutex
	log    *os.File
	events *os.File
	seq    int
	err    error
}

// Event is one line of the events file.
type Event struct {
	Seq   int            `json:"seq"`
	Time  string         `json:"time"`
	Event map[string]any `json:"event"`
}

// Create opens the output and events files of a new session.
func (s *Store) Create(id string) (*Session, error) {
	if !ValidID(id) {
		return nil, fmt.Errorf("history: invalid session ID %q", id)
	}
	if err := os.MkdirAll(filepath.Join(s.dir, "sessions"), 0o700); err != nil {
		return nil, fmt.Errorf("history: %w", err)
	}
	const flags = os.O_WRONLY | os.O_CREATE | os.O_EXCL
	log, err := os.OpenFile(s.LogPath(id), flags, 0o600)
	if err != nil {
		return nil, fmt.Errorf("history: %w", err)
	}
	events, err := os.OpenFile(s.EventsPath(id), flags, 0o600)
	if err != nil {
		_ = log.Close()
		return nil, fmt.Errorf("history: %w", err)
	}
	return &Session{id: id, log: log, events: events}, nil
}

// ID returns the session ID.
func (ss *Session) ID() string { return ss.id }

// Write appends terminal output (already masked) and takes one sequence number.
func (ss *Session) Write(p []byte) (int, error) {
	ss.WriteSeq(p)
	return len(p), nil
}

// WriteSeq is Write that returns the sequence number the chunk took (the value before
// the increment), or -1 for an empty chunk, which takes none. The log is a copy for
// the history; a full disk must not stop the action, Close reports the error.
func (ss *Session) WriteSeq(p []byte) int {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	if len(p) == 0 {
		return -1
	}
	seq := ss.seq
	ss.seq++
	if _, err := ss.log.Write(p); err != nil && ss.err == nil {
		ss.err = err
	}
	return seq
}

// Event appends a protocol message (already masked) and returns its line.
func (ss *Session) Event(msg map[string]any, now time.Time) Event {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	ev := Event{Seq: ss.seq, Time: now.UTC().Format(time.RFC3339Nano), Event: msg}
	ss.seq++
	data, err := json.Marshal(ev)
	if err == nil {
		_, err = ss.events.Write(append(data, '\n'))
	}
	if err != nil && ss.err == nil {
		ss.err = err
	}
	return ev
}

// Close closes the session files and reports the first write error.
func (ss *Session) Close() error {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	err := errors.Join(ss.err, ss.log.Close(), ss.events.Close())
	if err != nil {
		return fmt.Errorf("history: session %s: %w", ss.id, err)
	}
	return nil
}

// Append adds one line for a finished session to history/<YYYY-MM>.jsonl, chosen by
// the end time.
func (s *Store) Append(e Entry) error {
	data, err := s.marshal(e)
	if err != nil {
		return err
	}
	ended, err := time.Parse(time.RFC3339, e.EndedAt)
	if err != nil {
		return fmt.Errorf("history: endedAt: %w", err)
	}
	unlock, err := s.lock()
	if err != nil {
		return err
	}
	defer unlock()
	path := filepath.Join(s.historyDir(), ended.UTC().Format("2006-01")+".jsonl")
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600) //nolint:gosec // below the state directory, name from the end time
	if err != nil {
		return fmt.Errorf("history: %w", err)
	}
	if _, err := f.Write(append(data, '\n')); err != nil {
		_ = f.Close()
		return fmt.Errorf("history: %w", err)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return fmt.Errorf("history: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("history: %w", err)
	}
	return nil
}

func (s *Store) marshal(e Entry) ([]byte, error) {
	data, err := json.Marshal(e)
	if err != nil {
		return nil, fmt.Errorf("history: %w", err)
	}
	if s.validator != nil {
		if err := s.validator.ValidateJSON("history-entry", data); err != nil {
			return nil, fmt.Errorf("history: entry %s: %w", e.ID, err)
		}
	}
	return data, nil
}

// lock serialises writers of the history directory across processes.
func (s *Store) lock() (func(), error) {
	if err := os.MkdirAll(s.historyDir(), 0o700); err != nil {
		return nil, fmt.Errorf("history: %w", err)
	}
	f, err := os.OpenFile(filepath.Join(s.historyDir(), ".lock"), os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, fmt.Errorf("history: %w", err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil { //nolint:gosec // file descriptors fit int
		_ = f.Close()
		return nil, fmt.Errorf("history: lock: %w", err)
	}
	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN) //nolint:gosec // file descriptors fit int
		_ = f.Close()
	}, nil
}

// line is an entry together with its file and raw text, for rewriting on prune.
type line struct {
	entry Entry
	file  string
	raw   []byte
}

// readAll reads every month file. Lines that do not parse are reported as problems
// and kept untouched on prune.
func (s *Store) readAll() (lines []line, problems []string, err error) {
	files, err := filepath.Glob(filepath.Join(s.historyDir(), "*.jsonl"))
	if err != nil {
		return nil, nil, fmt.Errorf("history: %w", err)
	}
	sort.Strings(files)
	for _, file := range files {
		data, err := os.ReadFile(file) //nolint:gosec // month files found below the state directory
		if err != nil {
			return nil, nil, fmt.Errorf("history: %w", err)
		}
		sc := bufio.NewScanner(bytes.NewReader(data))
		sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		n := 0
		for sc.Scan() {
			n++
			raw := bytes.TrimSpace(sc.Bytes())
			if len(raw) == 0 {
				continue
			}
			var e Entry
			dec := json.NewDecoder(bytes.NewReader(raw))
			dec.DisallowUnknownFields()
			if err := dec.Decode(&e); err != nil || !ValidID(e.ID) {
				problems = append(problems, fmt.Sprintf("%s:%d: not a history entry", filepath.Base(file), n))
				continue
			}
			lines = append(lines, line{entry: e, file: file, raw: append([]byte(nil), raw...)})
		}
		if err := sc.Err(); err != nil {
			return nil, nil, fmt.Errorf("history: %s: %w", file, err)
		}
	}
	// Session IDs are ULIDs: sorting by ID sorts by start time; newest first.
	sort.SliceStable(lines, func(i, j int) bool { return lines[i].entry.ID > lines[j].entry.ID })
	return lines, problems, nil
}

// List returns up to limit entries, newest first (limit <= 0: all), and the lines
// that could not be read.
func (s *Store) List(limit int) ([]Entry, []string, error) {
	lines, problems, err := s.readAll()
	if err != nil {
		return nil, nil, err
	}
	if limit > 0 && len(lines) > limit {
		lines = lines[:limit]
	}
	out := make([]Entry, len(lines))
	for i, l := range lines {
		out[i] = l.entry
	}
	return out, problems, nil
}

// Get returns the entry of a session.
func (s *Store) Get(id string) (Entry, bool, error) {
	lines, _, err := s.readAll()
	if err != nil {
		return Entry{}, false, err
	}
	for _, l := range lines {
		if l.entry.ID == id {
			return l.entry, true, nil
		}
	}
	return Entry{}, false, nil
}

// Backups lists the original paths of the files a session backed up, sorted.
func (s *Store) Backups(id string) ([]string, error) {
	root := s.BackupDir(id)
	var out []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			out = append(out, string(filepath.Separator)+rel)
		}
		return nil
	})
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("history: %w", err)
	}
	sort.Strings(out)
	return out, nil
}

// Prune removes sessions that ended more than keepDays ago and all beyond the newest
// keepSessions, with their output, events and backups. It returns the removed IDs.
func (s *Store) Prune(now time.Time, keepDays, keepSessions int) ([]string, error) {
	if keepDays <= 0 {
		keepDays = DefaultKeepDays
	}
	if keepSessions <= 0 {
		keepSessions = DefaultKeepSessions
	}
	unlock, err := s.lock()
	if err != nil {
		return nil, err
	}
	defer unlock()
	lines, _, err := s.readAll()
	if err != nil {
		return nil, err
	}
	cutoff := now.Add(-time.Duration(keepDays) * 24 * time.Hour)
	drop := map[string]bool{}
	var removed []string
	for i, l := range lines {
		ended, err := time.Parse(time.RFC3339, l.entry.EndedAt)
		if i < keepSessions && (err != nil || !ended.Before(cutoff)) {
			continue
		}
		drop[l.entry.ID] = true
		removed = append(removed, l.entry.ID)
	}
	if len(removed) == 0 {
		return nil, nil
	}
	if err := s.rewrite(drop); err != nil {
		return nil, err
	}
	var errs []error
	for _, id := range removed {
		for _, p := range []string{s.LogPath(id), s.EventsPath(id)} {
			if err := os.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
				errs = append(errs, err)
			}
		}
		if err := os.RemoveAll(s.BackupDir(id)); err != nil {
			errs = append(errs, err)
		}
	}
	sort.Strings(removed)
	if err := errors.Join(errs...); err != nil {
		return removed, fmt.Errorf("history: prune: %w", err)
	}
	return removed, nil
}

// rewrite drops the lines of the given sessions from every month file, keeping all
// other lines byte for byte; files left empty are removed.
func (s *Store) rewrite(drop map[string]bool) error {
	files, err := filepath.Glob(filepath.Join(s.historyDir(), "*.jsonl"))
	if err != nil {
		return fmt.Errorf("history: %w", err)
	}
	for _, file := range files {
		data, err := os.ReadFile(file) //nolint:gosec // month files found below the state directory
		if err != nil {
			return fmt.Errorf("history: %w", err)
		}
		var out bytes.Buffer
		changed := false
		for _, raw := range strings.SplitAfter(string(data), "\n") {
			if raw == "" {
				continue
			}
			var head struct {
				ID string `json:"id"`
			}
			if json.Unmarshal([]byte(raw), &head) == nil && drop[head.ID] {
				changed = true
				continue
			}
			out.WriteString(raw)
		}
		switch {
		case !changed:
		case out.Len() == 0:
			if err := os.Remove(file); err != nil {
				return fmt.Errorf("history: %w", err)
			}
		default:
			if err := jsonfile.WriteAtomic(file, out.Bytes(), 0o600); err != nil {
				return fmt.Errorf("history: %w", err)
			}
		}
	}
	return nil
}

// ReadLog copies the output of a session to w.
func (s *Store) ReadLog(id string, w io.Writer) error {
	if !ValidID(id) {
		return fmt.Errorf("history: invalid session ID %q", id)
	}
	f, err := os.Open(s.LogPath(id))
	if err != nil {
		return fmt.Errorf("history: %w", err)
	}
	defer func() { _ = f.Close() }()
	if _, err := io.Copy(w, f); err != nil {
		return fmt.Errorf("history: %w", err)
	}
	return nil
}
