package api

import (
	"net/http"
	"path/filepath"
	"testing"
)

func TestProfileLifecycle(t *testing.T) {
	e := newTestEnv(t)
	if got := list(t, e.get("/profiles")); len(got) != 0 {
		t.Fatalf("profiles = %v", got)
	}

	path := filepath.Join(e.home, "team.json")
	writeJSONFile(t, path, map[string]any{
		"id": "example-team", "name": "Example team", "version": "2026.11.1",
		"settings": map[string]any{"demo.pinned": map[string]any{"value": "b", "locked": true}, "demo.mode": map[string]any{"value": "slow"}},
	})
	created := e.mutate(http.MethodPost, "/profiles", map[string]any{"source": "file://" + path}, http.StatusCreated).json(t)
	if created["id"] != "example-team" || created["origin"] != "user" || created["kind"] != "file" || created["settings"] != float64(2) {
		t.Fatalf("created = %v", created)
	}
	if locked, _ := created["locked"].([]any); len(locked) != 1 || locked[0] != "demo.pinned" {
		t.Errorf("locked = %v", created["locked"])
	}
	if _, n := e.lastBroadcast(); n != 1 {
		t.Errorf("%d plugin broadcasts after create, want 1", n)
	}
	profiles, _ := e.readUserConfig()["profiles"].([]any)
	if len(profiles) != 1 {
		t.Fatalf("user file profiles = %v", profiles)
	}

	// the lock of the profile applies to settings at once
	e.wantProblem(e.request(http.MethodPatch, "/settings", map[string]any{"demo.pinned": "c"}), http.StatusConflict, codeSettingLocked)

	// twice is a conflict, an invalid profile and an invalid body are validation errors
	e.wantProblem(e.request(http.MethodPost, "/profiles", map[string]any{"source": "file://" + path}), http.StatusConflict, "conflict")
	broken := filepath.Join(e.home, "broken.json")
	writeJSONFile(t, broken, map[string]any{"id": "broken"})
	e.wantProblem(e.request(http.MethodPost, "/profiles", map[string]any{"source": "file://" + broken}), http.StatusUnprocessableEntity, "validation")
	e.wantProblem(e.request(http.MethodPost, "/profiles", map[string]any{"source": "ftp://example.com/p.json"}), http.StatusUnprocessableEntity, "validation")

	updates := list(t, e.mutate(http.MethodPost, "/profiles/update", nil, http.StatusOK))
	if len(updates) != 1 || updates[0]["changed"] != false || updates[0]["id"] != "example-team" || updates[0]["error"] != nil {
		t.Errorf("updates = %v", updates)
	}

	e.mutate(http.MethodDelete, "/profiles/example-team", nil, http.StatusNoContent)
	if doc := e.readUserConfig(); doc["profiles"] != nil {
		t.Errorf("user file = %v", doc)
	}
	e.wantProblem(e.request(http.MethodDelete, "/profiles/example-team", nil), http.StatusNotFound, codeNotFound)
}

func TestProfileOfSystemFile(t *testing.T) {
	e := newTestEnv(t)
	path := filepath.Join(e.home, "sys.json")
	writeJSONFile(t, path, map[string]any{"id": "sys-team", "name": "System team", "version": "1.0.0"})
	e.writeSystemConfig(map[string]any{"profiles": []any{
		map[string]any{"source": "file://" + path},
		map[string]any{"source": "https://example.com/missing.json"},
	}})
	got := list(t, e.get("/profiles"))
	if len(got) != 2 || got[0]["origin"] != "system" || got[0]["id"] != "sys-team" {
		t.Fatalf("profiles = %v", got)
	}
	if got[1]["id"] != "" || got[1]["problem"] != "not fetched yet" || got[1]["kind"] != "https" {
		t.Errorf("remote without copy = %v", got[1])
	}
	e.wantProblem(e.request(http.MethodDelete, "/profiles/sys-team", nil), http.StatusConflict, "conflict")
}
