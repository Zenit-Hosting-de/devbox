package api

import (
	"errors"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
)

const updManifest = `{
  "id": "upd",
  "version": "1.0.0",
  "name": "Upd",
  "actions": {
    "notice": { "label": "Notice", "run": ["act.sh", "notice"] },
    "plain": { "label": "Plain", "run": ["act.sh", "plain"] },
    "fail": { "label": "Fail", "run": ["act.sh", "fail"] },
    "ask": { "label": "Ask", "run": ["act.sh", "ask"] }
  }
}`

const updActions = `#!/usr/bin/env bash
set -euo pipefail
case "$1" in
notice)
  printf '%s\n' '{"type":"notice","kind":"restart-service","text":"restart"}' >&3
  printf '%s\n' '{"type":"result","summary":"updated"}' >&3
  ;;
plain)
  printf '%s\n' '{"type":"notice","kind":"restart-shell","text":"shell"}' >&3
  printf '%s\n' '{"type":"result","summary":"done"}' >&3
  ;;
fail)
  printf '%s\n' '{"type":"notice","kind":"restart-service","text":"restart"}' >&3
  exit 1
  ;;
esac
`

// restartCalls counts the calls of Options.Restart and returns the result in err.
type restartCalls struct {
	mu    sync.Mutex
	calls int
	err   error
	hook  func()
	ch    chan struct{}
}

func newRestartCalls(err error) *restartCalls {
	return &restartCalls{err: err, ch: make(chan struct{}, 8)}
}

func (r *restartCalls) restart() error {
	r.mu.Lock()
	r.calls++
	hook, err := r.hook, r.err
	r.mu.Unlock()
	if hook != nil {
		hook()
	}
	r.ch <- struct{}{}
	return err
}

func (r *restartCalls) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.calls
}

func (r *restartCalls) wait(t *testing.T) {
	t.Helper()
	select {
	case <-r.ch:
	case <-time.After(20 * time.Second):
		t.Fatal("Restart was not called")
	}
}

func newRestartEnv(t *testing.T, rc *restartCalls) *wsEnv {
	t.Helper()
	var mods []func(*Options)
	mods = append(mods, func(o *Options) {
		o.RestartDelay = 20 * time.Millisecond
		if rc != nil {
			o.Restart = rc.restart
		}
	})
	e := newWSEnv(t, WSOptions{}, mods...)
	write(t, filepath.Join(e.plugins, "upd", "plugin.json"), updManifest, 0o600)
	write(t, filepath.Join(e.plugins, "upd", "act.sh"), updActions, 0o700)
	return e
}

func isRestart(m map[string]any) bool { return m["type"] == "restart" }

func TestRestartAfterNoticeAnnouncesClosesAndCalls(t *testing.T) {
	rc := newRestartCalls(nil)
	e := newRestartEnv(t, rc)
	c := e.dial()
	late := e.dial() // subscribed to nothing: the announcement goes to every client

	id := e.start(`{"action":"upd.notice"}`)
	c.until(isRestart)
	late.until(isRestart)
	e.waitFinished(id)
	rc.wait(t)
	if got := e.bc.restartCount(); got != 1 {
		t.Errorf("RestartRequested called %d times, want 1", got)
	}
	// the connections were asked to reconnect with status 1012 before the exec
	deadline := time.Now().Add(10 * time.Second)
	for c.closeStatus() != websocket.StatusServiceRestart && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if got := c.closeStatus(); got != websocket.StatusServiceRestart {
		t.Errorf("close status %d, want %d", got, websocket.StatusServiceRestart)
	}
}

func TestRestartWaitsForOtherSessions(t *testing.T) {
	rc := newRestartCalls(nil)
	e := newRestartEnv(t, rc)
	c := e.dial()

	slow := e.start(`{"action":"demo.slow"}`)
	e.waitLog(slow, "started")
	id := e.start(`{"action":"upd.notice"}`)
	c.until(isRestart) // announced at once ...
	e.waitFinished(id)
	time.Sleep(300 * time.Millisecond)
	if rc.count() != 0 {
		t.Fatal("Restart was called while another session ran")
	}
	if r := e.get("/sessions/" + slow); r.json(t)["state"] != "running" {
		t.Errorf("the other session must keep running: %s", r.body)
	}
	// ... and carried out when the last session ended
	e.request(http.MethodPost, "/sessions/"+slow+"/cancel", nil)
	e.waitFinished(slow)
	rc.wait(t)
	if e.bc.restartCount() != 1 {
		t.Errorf("RestartRequested called %d times, want 1", e.bc.restartCount())
	}
}

func TestRestartFailureKeepsServing(t *testing.T) {
	rc := newRestartCalls(errors.New("exec: no such file"))
	e := newRestartEnv(t, rc)

	id := e.start(`{"action":"upd.notice"}`)
	e.waitFinished(id)
	rc.wait(t)

	deadline := time.Now().Add(10 * time.Second)
	for !strings.Contains(e.logs.String(), "restart failed") && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if !strings.Contains(e.logs.String(), "restart failed") {
		t.Fatalf("the failure is not logged:\n%s", e.logs)
	}
	e.wantStatus(e.get("/info"), http.StatusOK)
	// the state is reset: a client that connects now gets no announcement, new sessions
	// run, and a new notice asks for a restart again
	c := e.dial()
	if m := c.nextAny(300 * time.Millisecond); m != nil && isRestart(m) {
		t.Error("a failed restart is still announced to new clients")
	}
	id = e.start(`{"action":"upd.notice"}`)
	e.waitFinished(id)
	rc.wait(t)
	if rc.count() != 2 {
		t.Errorf("Restart called %d times, want 2", rc.count())
	}
}

func TestNoRestartWithoutNoticeOrOnFailure(t *testing.T) {
	rc := newRestartCalls(nil)
	e := newRestartEnv(t, rc)

	e.waitFinished(e.start(`{"action":"upd.plain"}`)) // another notice kind
	e.waitFinished(e.start(`{"action":"demo.install","dryRun":true}`))
	failed := e.waitFinished(e.start(`{"action":"upd.fail"}`)) // notice, but the action failed
	if failed["state"] != "failed" {
		t.Fatalf("state = %v, want failed", failed["state"])
	}
	time.Sleep(300 * time.Millisecond)
	if rc.count() != 0 || e.bc.restartCount() != 0 {
		t.Errorf("Restart called %d times, announced %d times; want none", rc.count(), e.bc.restartCount())
	}
}

func TestRestartNoticeIsIgnoredWithoutRestartFunc(t *testing.T) {
	e := newRestartEnv(t, nil)
	e.waitFinished(e.start(`{"action":"upd.notice"}`))
	time.Sleep(200 * time.Millisecond)
	if e.bc.restartCount() != 0 {
		t.Error("announced a restart that cannot happen")
	}
	if !strings.Contains(e.logs.String(), "cannot restart itself") {
		t.Errorf("no log line about the ignored notice:\n%s", e.logs)
	}
}

func TestNoSessionStartsWhileRestarting(t *testing.T) {
	rc := newRestartCalls(errors.New("exec failed"))
	e := newRestartEnv(t, rc)
	refused := make(chan int, 1)
	rc.hook = func() { // Options.Restart runs: the last moment before the exec
		refused <- e.request(http.MethodPost, "/sessions", `{"action":"upd.plain"}`).code
	}
	e.waitFinished(e.start(`{"action":"upd.notice"}`))
	rc.wait(t)
	if code := <-refused; code != http.StatusServiceUnavailable {
		t.Errorf("status %d while restarting, want 503", code)
	}
	// after the failed restart sessions run again
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		r := e.request(http.MethodPost, "/sessions", `{"action":"upd.plain"}`)
		if r.code == http.StatusCreated {
			e.waitFinished(r.json(t)["id"].(string))
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("sessions are still refused after the failed restart")
}
