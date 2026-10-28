package api

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Zenit-Hosting-de/devbox/internal/core"
)

func TestSessionEndToEnd(t *testing.T) {
	e := newTestEnv(t)
	r := e.request(http.MethodPost, "/sessions", map[string]any{"action": "demo.install", "params": map[string]any{"version": 17}})
	e.wantStatus(r, http.StatusCreated)
	created := r.json(t)
	id := created["id"].(string)
	if state := created["state"]; state != "queued" && state != "running" {
		t.Errorf("state = %v", state)
	}
	if created["origin"] != "ui" || created["plugin"] != "demo" || created["title"] != "Install" || created["dryRun"] != false {
		t.Errorf("created = %v", created)
	}
	if created["nextSeq"] == nil {
		t.Errorf("a live session reports nextSeq: %v", created)
	}
	if loc := r.header.Get("Location"); loc != "/api/v1/sessions/"+id {
		t.Errorf("Location = %q", loc)
	}

	done := e.waitFinished(id)
	if done["state"] != "success" || done["exitCode"] != float64(0) || done["changes"] != float64(1) || done["summary"] != "installed" {
		t.Errorf("finished = %v", done)
	}
	if done["nextSeq"] != nil {
		t.Errorf("a finished session has no nextSeq: %v", done)
	}

	// the history line exists
	entry, ok, err := e.store().Get(id)
	if err != nil || !ok {
		t.Fatalf("history entry: %v %v", ok, err)
	}
	if entry.Action != "demo.install" || entry.Origin != "ui" || entry.State != "success" || entry.Changes != 1 {
		t.Errorf("entry = %+v", entry)
	}

	// the log has the output, secrets masked, and says how far it reaches
	logRes := e.get("/sessions/" + id + "/log")
	e.wantStatus(logRes, http.StatusOK)
	if !strings.Contains(string(logRes.body), "version=17 dryRun=false") || !strings.Contains(string(logRes.body), "after progress") {
		t.Errorf("log = %q", logRes.body)
	}

	// the events carry increasing sequence numbers
	events := e.get("/sessions/" + id + "/events").json(t)["events"].([]any)
	var types []string
	last := -1
	for _, raw := range events {
		ev := raw.(map[string]any)
		seq := int(ev["seq"].(float64))
		if seq <= last {
			t.Errorf("seq %d after %d", seq, last)
		}
		last = seq
		types = append(types, field(ev, "event", "type").(string))
	}
	if want := []string{"step", "progress", "change", "result"}; !slices.Equal(types, want) {
		t.Errorf("event types = %v, want %v", types, want)
	}
	if got := logRes.header.Get(nextSeqHeader); got != strconv.Itoa(last+1) {
		t.Errorf("%s = %s, want %d", nextSeqHeader, got, last+1)
	}

	// the lists have it, newest first, filtered and paged
	list := e.get("/sessions").json(t)
	if sess := list["sessions"].([]any); len(sess) != 1 || sess[0].(map[string]any)["id"] != id {
		t.Errorf("list = %v", list)
	}
	if got := e.get("/sessions?state=failed").json(t)["sessions"].([]any); len(got) != 0 {
		t.Errorf("state filter: %v", got)
	}
	if got := e.get("/sessions?plugin=demo&state=success").json(t)["sessions"].([]any); len(got) != 1 {
		t.Errorf("plugin filter: %v", got)
	}

	// the finished session refreshes the checks of its plugin
	waitFor(t, "the check of the plugin", func() bool { return e.get("/checks/demo.state").code == http.StatusOK })
}

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if ok() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for %s", what)
}

func TestBroadcastOrder(t *testing.T) {
	e := newTestEnv(t)
	id := e.start(map[string]any{"action": "demo.install"})
	e.waitFinished(id)

	e.bc.mu.Lock()
	defer e.bc.mu.Unlock()
	seqs := e.bc.seqs[id]
	if len(seqs) < 5 {
		t.Fatalf("seqs = %v", seqs)
	}
	for i := 1; i < len(seqs); i++ {
		if seqs[i] != seqs[i-1]+1 {
			t.Errorf("broadcasts are not in sequence order: %v", seqs)
			break
		}
	}
	var out strings.Builder
	for _, o := range e.bc.outputs {
		out.WriteString(o.data)
	}
	if !strings.Contains(out.String(), "after progress") {
		t.Errorf("output = %q", out.String())
	}
	var states []string
	for _, s := range e.bc.sessions {
		if s.ID == id {
			states = append(states, s.State)
		}
	}
	if len(states) < 2 || states[0] != "queued" || states[len(states)-1] != "success" {
		t.Errorf("session states = %v", states)
	}
	sawProgress := false
	for _, s := range e.bc.sessions {
		if s.Progress != nil && s.Progress.Text == "half" && s.Progress.Percent != nil && *s.Progress.Percent == 50 {
			sawProgress = true
		}
	}
	if !sawProgress {
		t.Error("the progress message did not reach the session summary")
	}
}

func TestSessionErrors(t *testing.T) {
	e := newTestEnv(t)
	tests := []struct {
		name    string
		body    string
		status  int
		code    string
		pointer string
	}{
		{"unknown plugin", `{"action":"nope.install"}`, http.StatusNotFound, codeNotFound, ""},
		{"unknown action", `{"action":"demo.nothing"}`, http.StatusNotFound, codeNotFound, ""},
		{"plugin blocked", `{"action":"needy.go"}`, http.StatusConflict, codePluginNotReady, ""},
		{"plugin incomplete", `{"action":"gap.go"}`, http.StatusConflict, codePluginNotReady, ""},
		{"confirm", `{"action":"demo.remove"}`, http.StatusUnprocessableEntity, codeValidation, "/yes"},
		{"bad param", `{"action":"demo.install","params":{"version":"many"}}`, http.StatusUnprocessableEntity, codeValidation, "/params/version"},
		{"unknown param", `{"action":"demo.install","params":{"colour":"red"}}`, http.StatusUnprocessableEntity, codeValidation, "/params/colour"},
		{"param outside enum", `{"action":"demo.install","params":{"channel":"nightly"}}`, http.StatusUnprocessableEntity, codeValidation, "/params/channel"},
		{"unknown key", `{"action":"demo.install","bogus":1}`, http.StatusUnprocessableEntity, codeValidation, "/bogus"},
		{"bad action ref", `{"action":"install"}`, http.StatusUnprocessableEntity, codeValidation, "/action"},
		{"no action", `{}`, http.StatusUnprocessableEntity, codeValidation, ""},
		{"empty body", ``, http.StatusUnprocessableEntity, codeValidation, ""},
		{"wrong type", `{"action":"demo.install","dryRun":"yes"}`, http.StatusUnprocessableEntity, codeValidation, "/dryRun"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := e.request(http.MethodPost, "/sessions", tt.body)
			e.wantStatus(r, tt.status)
			p := r.problem(t)
			if p.Code != tt.code {
				t.Errorf("code = %s, want %s", p.Code, tt.code)
			}
			if tt.pointer != "" && (len(p.Errors) == 0 || p.Errors[0].Pointer != tt.pointer) {
				t.Errorf("errors = %+v, want pointer %s", p.Errors, tt.pointer)
			}
		})
	}

	// the confirm rule: a code the UI can recognise, a dry run needs none
	r := e.request(http.MethodPost, "/sessions", `{"action":"demo.remove"}`)
	if p := r.problem(t); p.Errors[0].Code != "confirm" || p.Errors[0].Message != "Removes everything." {
		t.Errorf("confirm error = %+v", p.Errors)
	}
	id := e.start(`{"action":"demo.remove","dryRun":true}`)
	if s := e.waitFinished(id); s["dryRun"] != true || s["state"] != "success" {
		t.Errorf("dry run = %v", s)
	}
	id = e.start(`{"action":"demo.remove","yes":true}`)
	e.waitFinished(id)
	if log := e.get("/sessions/" + id + "/log"); !strings.Contains(string(log.body), "removed") {
		t.Errorf("log = %q", log.body)
	}

	for _, path := range []string{"/sessions/nope", "/sessions/s_01J0000000000000000000", "/sessions/s_01J0000000000000000000/events", "/sessions/s_01J0000000000000000000/log"} {
		if r := e.get(path); r.code != http.StatusNotFound || r.problem(t).Code != codeNotFound {
			t.Errorf("%s: %d %s", path, r.code, r.body)
		}
	}
	e.wantStatus(e.get("/sessions?limit=0"), http.StatusUnprocessableEntity)
	e.wantStatus(e.get("/sessions?limit=501"), http.StatusUnprocessableEntity)
	e.wantStatus(e.get("/sessions?state=sleeping"), http.StatusUnprocessableEntity)
	e.wantStatus(e.get("/sessions?plugin=Bad"), http.StatusUnprocessableEntity)
}

func TestSessionFailureAndSettingMessage(t *testing.T) {
	e := newTestEnv(t)
	id := e.start(`{"action":"demo.fail"}`)
	s := e.waitFinished(id)
	if s["state"] != "failed" || s["exitCode"] != float64(1) {
		t.Errorf("failed session = %v", s)
	}

	// a setting message from the plugin lands in the user file and is announced
	id = e.start(`{"action":"demo.remember"}`)
	e.waitFinished(id)
	if v := e.get("/settings/demo.lastVersion/explain").json(t); v["value"] != float64(7) || v["origin"] != "user" {
		t.Errorf("lastVersion = %v", v)
	}
	e.bc.mu.Lock()
	defer e.bc.mu.Unlock()
	if len(e.bc.settings) == 0 || e.bc.settings[0][0] != "demo.lastVersion" {
		t.Errorf("announced settings = %v", e.bc.settings)
	}
}

func TestCancelAndConflict(t *testing.T) {
	e := newTestEnv(t)
	id := e.start(`{"action":"demo.slow"}`)
	e.waitLog(id, "started")

	// a live session shows up with a running state and its progress of the log
	live := e.get("/sessions/" + id).json(t)
	if live["state"] != "running" || live["nextSeq"] == nil || live["startedAt"] == nil {
		t.Errorf("live = %v", live)
	}
	logRes := e.get("/sessions/" + id + "/log")
	if logRes.header.Get(nextSeqHeader) == "" || !strings.Contains(string(logRes.body), "started") {
		t.Errorf("live log: %v %q", logRes.header, logRes.body)
	}
	if list := e.get("/sessions?state=running").json(t)["sessions"].([]any); len(list) != 1 {
		t.Errorf("running list = %v", list)
	}

	// the same action again conflicts; another action of the plugin queues behind it
	r := e.request(http.MethodPost, "/sessions", `{"action":"demo.slow"}`)
	e.wantStatus(r, http.StatusConflict)
	if r.problem(t).Code != codeConflict {
		t.Errorf("problem = %s", r.body)
	}
	queued := e.start(`{"action":"demo.fail"}`)
	if st := e.get("/sessions/" + queued).json(t)["state"]; st != "queued" {
		t.Errorf("the second action of the plugin must queue behind the first, got %v", st)
	}

	r = e.request(http.MethodPost, "/sessions/"+id+"/cancel", nil)
	e.wantStatus(r, http.StatusAccepted)
	if r.json(t)["id"] != id {
		t.Errorf("cancel = %s", r.body)
	}
	done := e.waitFinished(id)
	if done["state"] != "stopped" {
		t.Errorf("cancelled = %v", done)
	}
	e.waitFinished(queued) // runs after the first one is gone

	// finished or unknown
	r = e.request(http.MethodPost, "/sessions/"+id+"/cancel", nil)
	e.wantStatus(r, http.StatusConflict)
	e.wantStatus(e.request(http.MethodPost, "/sessions/s_01J0000000000000000000/cancel", nil), http.StatusNotFound)
	e.wantStatus(e.request(http.MethodPost, "/sessions/bad/cancel", nil), http.StatusNotFound)
}

func TestSessionPaging(t *testing.T) {
	e := newTestEnv(t)
	var ids []string
	for range 3 {
		id := e.start(`{"action":"demo.fail"}`)
		e.waitFinished(id)
		ids = append(ids, id)
	}
	slices.Reverse(ids) // newest first

	page := e.get("/sessions?limit=2").json(t)
	got := page["sessions"].([]any)
	if len(got) != 2 || got[0].(map[string]any)["id"] != ids[0] || got[1].(map[string]any)["id"] != ids[1] {
		t.Fatalf("first page = %v", page)
	}
	next, _ := page["next"].(string)
	if next != ids[1] {
		t.Fatalf("next = %q, want %s", next, ids[1])
	}
	page = e.get("/sessions?limit=2&cursor=" + next).json(t)
	got = page["sessions"].([]any)
	if len(got) != 1 || got[0].(map[string]any)["id"] != ids[2] || page["next"] != nil {
		t.Fatalf("second page = %v", page)
	}
}

func TestLiveInput(t *testing.T) {
	e := newTestEnv(t)
	id := e.start(`{"action":"demo.slow"}`)
	e.waitLog(id, "started")
	live, ok := e.srv.Live(id)
	if !ok {
		t.Fatal("no live session")
	}
	if err := live.WriteInput([]byte("hello\n")); err != nil {
		t.Fatal(err)
	}
	e.waitLog(id, "hello") // the terminal echoes what it is given
	var resized [2]uint16
	live.SetResizer(func(cols, rows uint16) { resized = [2]uint16{cols, rows} })
	live.Resize(100, 30)
	if resized != [2]uint16{100, 30} {
		t.Errorf("resizer got %v", resized)
	}
	live.Cancel()
	e.waitFinished(id)
	if err := live.WriteInput([]byte("late")); err == nil {
		t.Error("input after the end must fail")
	}
}

func TestShutdownStopsSessions(t *testing.T) {
	e := newTestEnv(t)
	id := e.start(`{"action":"demo.slow"}`)
	e.waitLog(id, "started")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := e.srv.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	entry, ok, err := e.store().Get(id)
	if err != nil || !ok {
		t.Fatalf("history entry: %v %v", ok, err)
	}
	if entry.State != "stopped" {
		t.Errorf("state = %s, want stopped", entry.State)
	}
	// a stopped server starts nothing new
	r := e.request(http.MethodPost, "/sessions", `{"action":"demo.fail"}`)
	if r.code != http.StatusServiceUnavailable {
		t.Errorf("status %d", r.code)
	}
}

func TestCancelQueuedSession(t *testing.T) {
	e := newTestEnv(t)
	id := e.start(`{"action":"demo.slow"}`)
	e.waitLog(id, "started")
	queued := e.start(`{"action":"demo.fail"}`)
	if st := e.get("/sessions/" + queued).json(t)["state"]; st != "queued" {
		t.Fatalf("state = %v, want queued", st)
	}

	e.wantStatus(e.request(http.MethodPost, "/sessions/"+queued+"/cancel", nil), http.StatusAccepted)
	done := e.waitFinished(queued)
	if done["state"] != "stopped" {
		t.Errorf("cancelled queued session = %v", done)
	}
	// the session stays findable: in the history, with real times, and in the list
	entry, ok, err := e.store().Get(queued)
	if err != nil || !ok {
		t.Fatalf("history entry of the queued session: %v %v", ok, err)
	}
	if entry.State != "stopped" || strings.HasPrefix(entry.EndedAt, "0001") || strings.HasPrefix(entry.StartedAt, "0001") {
		t.Errorf("entry = %+v", entry)
	}
	if !e.listed("/sessions?state=stopped", queued) {
		t.Error("the cancelled session is missing from the session list")
	}
	e.request(http.MethodPost, "/sessions/"+id+"/cancel", nil)
	e.waitFinished(id)
}

func TestShutdownWithQueuedSession(t *testing.T) {
	e := newTestEnv(t)
	id := e.start(`{"action":"demo.slow"}`)
	e.waitLog(id, "started")
	queued := e.start(`{"action":"demo.fail"}`)
	if st := e.get("/sessions/" + queued).json(t)["state"]; st != "queued" {
		t.Fatalf("state = %v, want queued", st)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := e.srv.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	for _, sid := range []string{id, queued} {
		entry, ok, err := e.store().Get(sid)
		if err != nil || !ok {
			t.Fatalf("history entry of %s: %v %v", sid, ok, err)
		}
		if entry.State != "stopped" || strings.HasPrefix(entry.EndedAt, "0001") {
			t.Errorf("entry of %s = %+v", sid, entry)
		}
	}
}

func TestSessionThatCannotRun(t *testing.T) {
	e := newTestEnv(t)
	// the process cannot be started: the answer is 201 and the session ends as failed
	if err := os.Chmod(filepath.Join(e.plugins, "demo", "act.sh"), 0o600); err != nil {
		t.Fatal(err)
	}
	id := e.start(`{"action":"demo.fail"}`)
	s := e.waitFinished(id)
	if s["state"] != "failed" {
		t.Errorf("session = %v", s)
	}
	entry, ok, err := e.store().Get(id)
	if err != nil || !ok {
		t.Fatalf("history entry: %v %v", ok, err)
	}
	if entry.State != "failed" || entry.Summary != core.FailedSummary || strings.HasPrefix(entry.EndedAt, "0001") {
		t.Errorf("entry = %+v", entry)
	}
	e.wantStatus(e.get("/sessions/"+id), http.StatusOK)
	if !e.listed("/sessions", id) {
		t.Error("the failed session is missing from the session list")
	}
}

// listed reports whether the session list at path contains the session id.
func (e *testEnv) listed(path, id string) bool {
	e.t.Helper()
	for _, s := range e.get(path).json(e.t)["sessions"].([]any) {
		if s.(map[string]any)["id"] == id {
			return true
		}
	}
	return false
}
