package api

import (
	"net/http"
	"testing"
)

func TestGroupsAndSetup(t *testing.T) {
	e := newTestEnv(t)

	// without profile no plugin of the test contributes anything
	r := e.get("/groups")
	e.wantStatus(r, http.StatusOK)
	if got := list(t, r); len(got) != 0 {
		t.Errorf("groups = %v", got)
	}
	plan := e.get("/setup")
	e.wantStatus(plan, http.StatusOK)
	if p := plan.json(t); p["origin"] != "plugins" || len(p["steps"].([]any)) != 0 {
		t.Errorf("setup = %v", p)
	}

	e.addProfile(map[string]any{
		"groups": map[string]any{"demo-all": map[string]any{"label": "Everything", "actions": []any{"demo.remember", "demo.install"}}},
		"setup":  []any{"demo.remember", "credentials:nope", "gone.x"},
	})
	groups := list(t, e.get("/groups"))
	if len(groups) != 1 || groups[0]["id"] != "demo-all" || groups[0]["origin"] != "profile:example-team" || groups[0]["label"] != "Everything" {
		t.Fatalf("groups = %v", groups)
	}
	if actions, _ := groups[0]["actions"].([]any); len(actions) != 2 || actions[0] != "demo.remember" {
		t.Errorf("actions = %v", groups[0]["actions"])
	}

	steps := func() []any {
		t.Helper()
		r := e.get("/setup")
		e.wantStatus(r, http.StatusOK)
		p := r.json(t)
		if p["origin"] != "profile:example-team" {
			t.Errorf("origin = %v", p["origin"])
		}
		return p["steps"].([]any)
	}
	before := steps()
	if len(before) != 3 {
		t.Fatalf("steps = %v", before)
	}
	first, _ := before[0].(map[string]any)
	if first["ref"] != "demo.remember" || first["kind"] != "action" || first["state"] != "open" || first["label"] != "Remember" {
		t.Errorf("first = %v", first)
	}
	if s, _ := before[1].(map[string]any); s["kind"] != "credential" || s["state"] != "blocked" {
		t.Errorf("credential step = %v", s)
	}
	if s, _ := before[2].(map[string]any); s["state"] != "blocked" || s["detail"] != "no such plugin" {
		t.Errorf("missing plugin = %v", s)
	}

	// a dry run does not finish the step, a real run does
	e.waitFinished(e.start(map[string]any{"action": "demo.remember", "dryRun": true}))
	if s, _ := steps()[0].(map[string]any); s["state"] != "open" {
		t.Errorf("after the dry run: %v", s)
	}
	e.waitFinished(e.start(map[string]any{"action": "demo.remember"}))
	if s, _ := steps()[0].(map[string]any); s["state"] != "done" {
		t.Errorf("after the run: %v", s)
	}
}
