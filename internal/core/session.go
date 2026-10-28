package core

import (
	"context"
	"io"
	"sync"
	"time"

	"github.com/Zenit-Hosting-de/devbox/internal/config"
	"github.com/Zenit-Hosting-de/devbox/internal/history"
	"github.com/Zenit-Hosting-de/devbox/internal/runner"
)

// Sink receives what a session produces. All fields are optional. The callbacks of
// one session are called one at a time and in sequence order: a mutex per session is
// held from taking the sequence number to the end of the callback.
type Sink struct {
	// Output receives the terminal output (secrets masked), e.g. the terminal of the CLI.
	Output io.Writer
	// OnOutput is called for every output chunk with the sequence number it took.
	// data is only valid during the call.
	OnOutput func(seq int, data []byte)
	// Event is called for every protocol message after it was written to the events file.
	Event func(ev history.Event)
	// Warn receives non-fatal problems: failed writes, protocol violations.
	Warn func(text string)
	// Queued is called when the session waits for a lock, Started when it runs.
	Queued, Started func()
	// Input is copied to the terminal of the action.
	Input io.Reader
	// ConfigLock is held while a "setting" message is written to the user file.
	ConfigLock sync.Locker
	// Setting is called with the key after a "setting" message was stored.
	Setting func(key string)
}

func (s Sink) warn(text string) {
	if s.Warn != nil {
		s.Warn(text)
	}
}

// outputWriter is the terminal output of one session.
type outputWriter struct {
	mu   *sync.Mutex
	sess *history.Session
	sink Sink
}

func (w *outputWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	seq := w.sess.WriteSeq(p)
	if seq < 0 {
		return 0, nil
	}
	if w.sink.OnOutput != nil {
		w.sink.OnOutput(seq, p)
	}
	if w.sink.Output != nil {
		if _, err := w.sink.Output.Write(p); err != nil {
			return len(p), err
		}
	}
	return len(p), nil
}

// RunSession runs one plugin process as a session of the history: output and events
// go to the sink and the session files, the history line is written at the end and the
// retention settings are applied. ref is the fully qualified action of the history
// line, origin one of the history origins. When the process could not be started the
// failed result is returned together with the error; its history line is written too.
func RunSession(ctx context.Context, st *State, r *runner.Runner, spec runner.Spec, ref, title, origin string, sink Sink) (*runner.Result, error) {
	store := history.New(r.Env.StateDir, st.Validator)
	sess, err := store.Create(spec.SessionID)
	if err != nil {
		return nil, err
	}
	var mu sync.Mutex
	cb := runner.Callbacks{
		Output:  &outputWriter{mu: &mu, sess: sess, sink: sink},
		Queued:  sink.Queued,
		Started: sink.Started,
	}
	cb.Message = func(msg map[string]any) {
		mu.Lock()
		defer mu.Unlock()
		ev := sess.Event(msg, time.Now())
		if sink.Event != nil {
			sink.Event(ev)
		}
		if msg["type"] == "setting" {
			key := spec.PluginID + "." + stringOf(msg["key"])
			if sink.ConfigLock != nil {
				sink.ConfigLock.Lock()
			}
			err := config.Set(st.Validator, st.UserPath, st.Defs, st.Result, key, msg["value"])
			if sink.ConfigLock != nil {
				sink.ConfigLock.Unlock()
			}
			if err != nil {
				sink.warn(err.Error())
			} else if sink.Setting != nil {
				sink.Setting(key)
			}
		}
	}
	if sink.Input != nil {
		spec.Input = sink.Input
	}
	res, err := r.Run(ctx, spec, cb)
	if cerr := sess.Close(); cerr != nil {
		sink.warn(cerr.Error())
	}
	if err != nil {
		// The process did not run (request file, start failure): the session still
		// gets a history line, so it can be found and its files are pruned later.
		now := time.Now()
		res = &runner.Result{
			State: runner.StateFailed, ExitCode: -1, Started: now, Ended: now,
			Result: map[string]any{"summary": FailedSummary},
		}
		record(st, store, spec, ref, title, origin, res, sink)
		return res, err
	}
	stampTimes(res, time.Now())
	record(st, store, spec, ref, title, origin, res, sink)
	for _, w := range res.Warnings {
		sink.warn(w)
	}
	return res, nil
}

// FailedSummary is the summary of a session whose process could not be started.
const FailedSummary = "the session could not run"

// stampTimes fills the times of a run that never started, for example a session that
// was stopped while it waited for a lock. A history line without times would count
// as infinitely old and be pruned at once.
func stampTimes(res *runner.Result, now time.Time) {
	if res.Started.IsZero() {
		res.Started = now
	}
	if res.Ended.IsZero() {
		res.Ended = now
	}
}

func stringOf(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// Entry builds the history line of a finished session.
func Entry(st *State, spec runner.Spec, ref, title, origin string, res *runner.Result) history.Entry {
	if title == "" {
		title = ref
	}
	e := history.Entry{
		ID:        spec.SessionID,
		Action:    ref,
		Plugin:    spec.PluginID,
		Title:     title,
		User:      st.Ctx.Host.User,
		Origin:    origin,
		DryRun:    spec.DryRun,
		StartedAt: history.Timestamp(res.Started),
		EndedAt:   history.Timestamp(res.Ended),
		State:     res.State,
		Changes:   res.Changes,
	}
	if res.ExitCode >= 0 {
		code := res.ExitCode
		e.ExitCode = &code
	}
	if res.Result != nil {
		e.Summary, _ = res.Result["summary"].(string)
	}
	return e
}

// record writes the history line of a finished session and applies the retention
// settings. Failures are warnings: the action itself has already run.
func record(st *State, store *history.Store, spec runner.Spec, ref, title, origin string, res *runner.Result, sink Sink) {
	if err := store.Append(Entry(st, spec, ref, title, origin, res)); err != nil {
		sink.warn(err.Error())
	}
	keepDays, keepSessions := st.Retention()
	if _, err := store.Prune(time.Now(), keepDays, keepSessions); err != nil {
		sink.warn(err.Error())
	}
}
