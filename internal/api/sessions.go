package api

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Zenit-Hosting-de/devbox/internal/config"
	"github.com/Zenit-Hosting-de/devbox/internal/core"
	"github.com/Zenit-Hosting-de/devbox/internal/history"
	"github.com/Zenit-Hosting-de/devbox/internal/plugin"
	"github.com/Zenit-Hosting-de/devbox/internal/runner"
)

// Paging of GET /sessions.
const (
	defaultLimit = 50
	maxLimit     = 500
)

// nextSeqHeader tells how far the body of GET /sessions/{id}/log reaches.
const nextSeqHeader = "X-DevBox-Next-Seq"

// store returns the history of this user without loading the plugins.
func (s *Server) store() (*history.Store, error) {
	b, err := core.NewBase(s.env)
	if err != nil {
		return nil, err
	}
	return history.New(b.StateDir(), s.validator), nil
}

func asInt(v any) (int, bool) { return intOf(v) }

func (s *Server) createSession(w http.ResponseWriter, r *http.Request) error {
	body, err := s.decodeBody(w, r, "SessionCreate", false)
	if err != nil {
		return err
	}
	ref, _ := body["action"].(string)
	pluginID, name, _ := strings.Cut(ref, ".")
	dryRun, _ := body["dryRun"].(bool)
	yes, _ := body["yes"].(bool)

	st, err := s.load()
	if err != nil {
		return err
	}
	p := core.FindPlugin(st.Evaluate(), pluginID)
	if p == nil {
		return notFound("no such plugin")
	}
	if p.State != plugin.StateReady {
		return newError(http.StatusConflict, codePluginNotReady, fmt.Sprintf("%s is %s (%s)", pluginID, p.State, reasonText(p.Reasons)))
	}
	action, ok := core.ActionDef(p, name)
	if !ok {
		return notFound("no such action")
	}
	if confirm, has := action["confirm"]; has && confirm != false && !dryRun && !yes {
		prompt, _ := confirm.(string)
		if prompt == "" {
			prompt = "the action needs a confirmation"
		}
		return invalid("the action needs a confirmation", fieldError{Pointer: "/yes", Code: "confirm", Message: prompt})
	}
	given, _ := body["params"].(map[string]any)
	params, err := st.ResolveParams(action, ref, given)
	if err != nil {
		var ce *config.Error
		if errors.As(err, &ce) && ce.Code == config.CodeValidation {
			if _, param, found := strings.Cut(ce.Key, "#"); found {
				return invalid("the parameters are not valid", fieldError{Pointer: "/params/" + escapePointer(param), Code: codeValidation, Message: ce.Detail})
			}
		}
		return err
	}
	rn, err := s.newRunner(st)
	if err != nil {
		return err
	}
	spec, title, err := st.ActionSpec(p, name, params, dryRun, yes)
	if err != nil {
		return err
	}
	lang, err := langParam(stringOf(body["lang"]), "/lang")
	if err != nil {
		return err
	}
	if lang != "" {
		spec.Lang = lang
	}
	if n, ok := asInt(body["cols"]); ok && n > 0 && n <= 1<<16-1 {
		spec.Cols = uint16(n)
	}
	if n, ok := asInt(body["rows"]); ok && n > 0 && n <= 1<<16-1 {
		spec.Rows = uint16(n)
	}
	if title == "" {
		title = ref
	}
	origin := history.OriginAPI
	if r.Header.Get("Origin") != "" {
		origin = history.OriginUI // a browser
	}

	ctx, cancel := context.WithCancel(s.ctx)
	live := newLive(spec, ref, title, st.Ctx.Host.User, origin, cancel)
	if err := s.live.add(live); err != nil {
		cancel()
		return err
	}
	spec.Input = live.input
	snapshot := live.Session()
	s.bc.SessionChanged(snapshot)
	if !s.spawn(func() { s.runLive(ctx, cancel, st, rn, spec, live, ref, title, origin) }) {
		cancel()
		s.live.remove(live.id)
		return newError(http.StatusServiceUnavailable, codeInternal, "the service is shutting down")
	}
	w.Header().Set("Location", "/api/v1/sessions/"+live.id)
	s.writeJSON(w, r, http.StatusCreated, snapshot)
	return nil
}

func reasonText(rs []plugin.Reason) string {
	parts := make([]string, 0, len(rs))
	for _, r := range rs {
		if r.Detail == "" {
			parts = append(parts, r.Code)
			continue
		}
		parts = append(parts, r.Code+": "+r.Detail)
	}
	return strings.Join(parts, "; ")
}

// runLive runs a session in the background and keeps the live registry current.
func (s *Server) runLive(ctx context.Context, cancel context.CancelFunc, st *core.State, rn *runner.Runner, spec runner.Spec, live *LiveSession, ref, title, origin string) {
	defer cancel()
	id := live.id
	sink := core.Sink{
		Input:      live.input,
		ConfigLock: &s.cfgMu,
		Warn:       func(text string) { s.log.Warn("session", "id", id, "warning", text) },
		Queued: func() {
			live.setState(runner.StateQueued, time.Time{})
			s.bc.SessionChanged(live.Session())
		},
		Started: func() {
			live.setState(runner.StateRunning, time.Now())
			s.bc.SessionChanged(live.Session())
		},
		Terminal: func(t runner.Terminal) {
			if t == nil { // the terminal is closed: no more resizing
				live.terminalClosed()
				return
			}
			live.SetResizer(func(cols, rows uint16) {
				if err := t.Resize(cols, rows); err != nil {
					s.log.Debug("session", "id", id, "resize", err.Error())
				}
			})
		},
		OnOutput: func(seq int, data []byte) {
			live.onOutput(seq, len(data))
			s.bc.SessionOutput(id, seq, append([]byte(nil), data...))
		},
		Event: func(ev history.Event) {
			changed := live.onEvent(ev)
			s.bc.SessionEvent(id, ev)
			if changed {
				s.bc.SessionChanged(live.Session())
			}
		},
		Setting: func(key string) { s.bc.SettingsChanged([]string{key}) },
	}
	var final Session
	res, err := core.RunSession(ctx, st, rn, spec, ref, title, origin, sink)
	if err != nil {
		s.log.Error("session failed to run", "id", id, "action", ref, "error", err)
	}
	if res != nil {
		final = sessionOf(core.Entry(st, spec, ref, title, origin, res))
	} else { // the history line could not even be built
		now := history.Timestamp(time.Now())
		final = Session{
			ID: id, Action: ref, Plugin: spec.PluginID, Title: title, User: live.user, Origin: origin,
			State: runner.StateFailed, DryRun: spec.DryRun, StartedAt: now, EndedAt: now, Summary: core.FailedSummary,
		}
	}
	live.finish(final)
	s.live.remove(id) // the history has the entry now
	s.bc.SessionChanged(final)
	s.refreshAfterSession(spec.PluginID, ref, spec.Lang)
}

func (s *Server) listSessions(w http.ResponseWriter, r *http.Request) error {
	q := r.URL.Query()
	limit := defaultLimit
	var errs []fieldError
	if raw := q.Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > maxLimit {
			errs = append(errs, fieldError{Pointer: "/limit", Code: "range", Message: "limit is a number from 1 to 500"})
		} else {
			limit = n
		}
	}
	state := q.Get("state")
	if state != "" {
		switch state {
		case runner.StateQueued, runner.StateRunning, runner.StateSuccess, runner.StateFailed, runner.StateBlocked, runner.StateStopped, runner.StateTimeout:
		default:
			errs = append(errs, fieldError{Pointer: "/state", Code: "enum", Message: "not a session state"})
		}
	}
	pluginID := q.Get("plugin")
	if pluginID != "" && !pluginName.MatchString(pluginID) {
		errs = append(errs, fieldError{Pointer: "/plugin", Code: "pattern", Message: "not a plugin ID"})
	}
	if len(errs) > 0 {
		return invalid("the query is not valid", errs...)
	}
	cursor := q.Get("cursor")

	store, err := s.store()
	if err != nil {
		return err
	}
	entries, problems, err := store.List(0)
	if err != nil {
		return err
	}
	for _, p := range problems {
		s.log.Warn("history", "problem", p)
	}
	var all []Session
	seen := map[string]bool{}
	for _, l := range s.live.list() {
		seen[l.id] = true
		all = append(all, l.Session())
	}
	for _, e := range entries {
		if !seen[e.ID] {
			all = append(all, sessionOf(e))
		}
	}
	sort.SliceStable(all, func(i, j int) bool { return all[i].ID > all[j].ID })

	page := make([]Session, 0, limit)
	more := false
	for _, ss := range all {
		if cursor != "" && ss.ID >= cursor {
			continue
		}
		if (state != "" && ss.State != state) || (pluginID != "" && ss.Plugin != pluginID) {
			continue
		}
		if len(page) == limit {
			more = true
			break
		}
		page = append(page, ss)
	}
	out := struct {
		Sessions []Session `json:"sessions"`
		Next     string    `json:"next,omitempty"`
	}{Sessions: page}
	if more {
		out.Next = page[len(page)-1].ID
	}
	s.writeJSON(w, r, http.StatusOK, out)
	return nil
}

// sessionByID finds a live or recorded session.
func (s *Server) sessionByID(id string) (Session, *LiveSession, error) {
	if !history.ValidID(id) {
		return Session{}, nil, notFound("no such session")
	}
	if l, ok := s.live.get(id); ok {
		return l.Session(), l, nil
	}
	store, err := s.store()
	if err != nil {
		return Session{}, nil, err
	}
	e, ok, err := store.Get(id)
	if err != nil {
		return Session{}, nil, err
	}
	if !ok {
		return Session{}, nil, notFound("no such session")
	}
	return sessionOf(e), nil, nil
}

func (s *Server) getSession(w http.ResponseWriter, r *http.Request) error {
	sess, _, err := s.sessionByID(r.PathValue("id"))
	if err != nil {
		return err
	}
	s.writeJSON(w, r, http.StatusOK, sess)
	return nil
}

// readEvents reads the complete lines of an events file; a line that is still being
// written is left out.
func readEvents(path string) ([]history.Event, error) {
	data, err := os.ReadFile(path) //nolint:gosec // below the state directory, name from a checked session ID
	if err != nil {
		return nil, err
	}
	if i := bytes.LastIndexByte(data, '\n'); i >= 0 {
		data = data[:i+1]
	} else {
		data = nil
	}
	events := []history.Event{}
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var ev history.Event
		if err := json.Unmarshal(line, &ev); err != nil {
			return nil, fmt.Errorf("events: %w", err)
		}
		events = append(events, ev)
	}
	return events, sc.Err()
}

func (s *Server) listSessionEvents(w http.ResponseWriter, r *http.Request) error {
	sess, _, err := s.sessionByID(r.PathValue("id"))
	if err != nil {
		return err
	}
	store, err := s.store()
	if err != nil {
		return err
	}
	events, err := readEvents(store.EventsPath(sess.ID))
	if errors.Is(err, fs.ErrNotExist) {
		events, err = []history.Event{}, nil // nothing was written yet
	}
	if err != nil {
		return err
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"events": events})
	return nil
}

func (s *Server) getSessionLog(w http.ResponseWriter, r *http.Request) error {
	sess, live, err := s.sessionByID(r.PathValue("id"))
	if err != nil {
		return err
	}
	store, err := s.store()
	if err != nil {
		return err
	}
	f, err := os.Open(store.LogPath(sess.ID))
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		if live == nil {
			return notFound("there is no output for this session")
		}
		f = nil // the live session has not written yet
	}
	if f != nil {
		defer func() { _ = f.Close() }()
	}
	var body io.Reader = bytes.NewReader(nil)
	var next, size int64
	if live != nil {
		// A consistent snapshot: the sequence number and the byte count belong together.
		seq, n := live.snapshot()
		next, size = int64(seq), n
		if f != nil {
			body = io.LimitReader(f, n)
		}
	} else {
		events, err := readEvents(store.EventsPath(sess.ID))
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		if len(events) > 0 {
			next = int64(events[len(events)-1].Seq) + 1
		}
		info, err := f.Stat()
		if err != nil {
			return err
		}
		body, size = f, info.Size()
	}
	h := w.Header()
	h.Set("Content-Type", "application/octet-stream")
	h.Set(nextSeqHeader, strconv.FormatInt(next, 10))
	h.Set("Content-Length", strconv.FormatInt(size, 10))
	h.Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, body)
	return nil
}

func (s *Server) cancelSession(w http.ResponseWriter, r *http.Request) error {
	sess, live, err := s.sessionByID(r.PathValue("id"))
	if err != nil {
		return err
	}
	if live == nil {
		return conflict(sess.ID + " has finished")
	}
	live.Cancel()
	s.writeJSON(w, r, http.StatusAccepted, live.Session())
	return nil
}
