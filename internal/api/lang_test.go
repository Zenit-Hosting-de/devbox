package api

import (
	"net/http"
	"path/filepath"
	"strings"
	"testing"
)

const talkManifest = `{
  "id": "talk",
  "version": "1.0.0",
  "name": "Talk",
  "checks": {
    "lang": { "label": "Language", "run": ["check.sh"], "cache": "1h" }
  },
  "actions": {
    "lines": { "label": "Lines", "run": ["act.sh", "lines"] },
    "silent": { "label": "Silent", "run": ["act.sh", "silent"] }
  }
}`

const talkActions = `#!/usr/bin/env bash
set -euo pipefail
case "$1" in
lines)
  echo "first line"
  echo "lang=$DEVBOX_LANG"
  printf '%s\n' '{"type":"change","kind":"file","op":"create","target":"~/.config/talk","summary":"written"}' >&3
  echo "last line"
  ;;
silent)
  # Like the bundled plugins: everything goes through the protocol, nothing to the terminal.
  printf '%s\n' '{"type":"step","id":"work","label":"Work","state":"running"}' >&3
  printf '%s\n' '{"type":"change","kind":"file","op":"create","target":"~/.config/talk","summary":"written"}' >&3
  printf '%s\n' '{"type":"result","summary":"done"}' >&3
  ;;
esac
`

const talkCheck = `#!/usr/bin/env bash
set -euo pipefail
printf '{"status":"ok","summary":"lang=%s"}\n' "$DEVBOX_LANG"
`

func newTalkEnv(t *testing.T) *testEnv {
	t.Helper()
	e := newTestEnv(t)
	write(t, filepath.Join(e.plugins, "talk", "plugin.json"), talkManifest, 0o600)
	write(t, filepath.Join(e.plugins, "talk", "act.sh"), talkActions, 0o700)
	write(t, filepath.Join(e.plugins, "talk", "check.sh"), talkCheck, 0o700)
	return e
}

// The terminal output of a session reaches the log and every subscriber, chunk by chunk.
func TestSessionOutputReachesLogAndBroadcast(t *testing.T) {
	e := newTalkEnv(t)
	id := e.start(map[string]any{"action": "talk.lines"})
	e.waitFinished(id)

	log := string(e.get("/sessions/" + id + "/log").body)
	for _, want := range []string{"first line", "last line"} {
		if !strings.Contains(log, want) {
			t.Errorf("log = %q, want %q", log, want)
		}
	}
	e.bc.mu.Lock()
	defer e.bc.mu.Unlock()
	var streamed strings.Builder
	for _, o := range e.bc.outputs {
		if o.id == id {
			streamed.WriteString(o.data)
		}
	}
	if streamed.String() != log {
		t.Errorf("streamed %q differs from the log %q", streamed.String(), log)
	}
}

// A plugin that only speaks the protocol has no terminal output at all; its steps,
// changes and result are events. The UI has to show them, because the terminal stays
// empty.
func TestActionWithoutTerminalOutputHasOnlyEvents(t *testing.T) {
	e := newTalkEnv(t)
	id := e.start(map[string]any{"action": "talk.silent"})
	e.waitFinished(id)

	if log := e.get("/sessions/" + id + "/log"); len(log.body) != 0 {
		t.Errorf("log = %q, want none", log.body)
	}
	events := e.get("/sessions/" + id + "/events").json(t)["events"].([]any)
	var types []string
	for _, raw := range events {
		types = append(types, field(raw.(map[string]any), "event", "type").(string))
	}
	if got := strings.Join(types, ","); got != "step,change,result" {
		t.Errorf("event types = %s", got)
	}
}

func TestSessionRunsInTheLanguageOfTheUI(t *testing.T) {
	e := newTalkEnv(t)
	id := e.start(map[string]any{"action": "talk.lines", "lang": "de"})
	e.waitFinished(id)
	if log := string(e.get("/sessions/" + id + "/log").body); !strings.Contains(log, "lang=de") {
		t.Errorf("log = %q, want lang=de", log)
	}

	// without lang, ui.language of the configuration applies
	write(t, filepath.Join(e.home, ".config", "devbox", "config.json"), `{"ui":{"language":"en"}}`, 0o600)
	id = e.start(map[string]any{"action": "talk.lines"})
	e.waitFinished(id)
	if log := string(e.get("/sessions/" + id + "/log").body); !strings.Contains(log, "lang=en") {
		t.Errorf("log = %q, want lang=en", log)
	}

	r := e.request(http.MethodPost, "/sessions", map[string]any{"action": "talk.lines", "lang": "Deutsch"})
	e.wantStatus(r, http.StatusUnprocessableEntity)
	if p := r.problem(t); len(p.Errors) != 1 || p.Errors[0].Pointer != "/lang" {
		t.Errorf("problem = %+v", p)
	}
}

func (e *testEnv) checkSummary(id string) string {
	e.t.Helper()
	r := e.get("/checks/" + id)
	if r.code != http.StatusOK {
		return ""
	}
	s, _ := r.json(e.t)["summary"].(string)
	return s
}

func TestChecksRunInTheLanguageOfTheUI(t *testing.T) {
	e := newTalkEnv(t)

	// the first read starts the checks in the language that was asked for
	e.wantStatus(e.get("/checks?lang=de"), http.StatusOK)
	waitFor(t, "the check in German", func() bool { return e.checkSummary("talk.lang") == "lang=de" })

	// another language makes the cached result stale: it is listed and runs again
	r := e.get("/checks?lang=en")
	e.wantStatus(r, http.StatusOK)
	if got := field(r.json(t), "checks"); got == nil {
		t.Errorf("the stale result is still listed: %v", r.json(t))
	}
	waitFor(t, "the check in English", func() bool { return e.checkSummary("talk.lang") == "lang=en" })

	// the same language again is served from the cache
	e.bc.mu.Lock()
	before := len(e.bc.checks)
	e.bc.mu.Unlock()
	e.wantStatus(e.get("/checks?lang=en"), http.StatusOK)
	e.bc.mu.Lock()
	after := len(e.bc.checks)
	e.bc.mu.Unlock()
	if after != before {
		t.Errorf("a cached result in the right language ran again (%d -> %d broadcasts)", before, after)
	}

	// an explicit refresh takes the language of its body
	e.wantStatus(e.request(http.MethodPost, "/checks/refresh", map[string]any{"ids": []string{"talk.lang"}, "lang": "fr"}), http.StatusAccepted)
	waitFor(t, "the check in French", func() bool { return e.checkSummary("talk.lang") == "lang=fr" })

	r = e.request(http.MethodPost, "/checks/refresh", map[string]any{"lang": "x"})
	e.wantStatus(r, http.StatusUnprocessableEntity)
	e.wantStatus(e.get("/checks?lang=nonsense"), http.StatusUnprocessableEntity)
}

func TestInfoHasTheHomeDirectory(t *testing.T) {
	e := newTestEnv(t)
	info := e.get("/info").json(t)
	if info["home"] != e.home {
		t.Errorf("home = %v, want %s", info["home"], e.home)
	}
}
