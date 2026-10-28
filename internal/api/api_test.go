package api

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"
)

func field(m map[string]any, path ...string) any {
	var cur any = m
	for _, p := range path {
		obj, _ := cur.(map[string]any)
		cur = obj[p]
	}
	return cur
}

func TestInfo(t *testing.T) {
	e := newTestEnv(t)
	write(t, filepath.Join(e.home, ".config", "devbox", "config.json"), `{"ui":{"language":"de","scheme":"dark"}}`, 0o600)
	write(t, e.getenv["DEVBOX_SYSTEM_CONFIG"], `{"ui":{"language":"en","accent":"teal"},"plugins":{"disabled":["base"]}}`, 0o600)

	r := e.get("/info")
	e.wantStatus(r, http.StatusOK)
	info := r.json(t)
	for key, want := range map[string]any{"vendor": "zenit-hosting", "version": "1.2.3", "commit": "abc1234", "auth": "local"} {
		if info[key] != want {
			t.Errorf("%s = %v, want %v", key, info[key], want)
		}
	}
	// the user file wins over the system file, key by key
	ui := field(info, "ui").(map[string]any)
	if ui["language"] != "de" || ui["scheme"] != "dark" || ui["accent"] != "teal" {
		t.Errorf("ui = %v", ui)
	}
	if feats := info["features"].([]any); len(feats) != 0 {
		t.Errorf("features = %v: the UI is not built and nothing else listens", feats)
	}

	e.srv.SetSocket(true)
	e.srv.SetWebSocket(true)
	e.srv.ui = &uiFiles{fsys: fstest.MapFS{"index.html": {Data: []byte("<html></html>")}}}
	feats := e.get("/info").json(t)["features"].([]any)
	if len(feats) != 3 {
		t.Errorf("features = %v, want ui, ws and socket", feats)
	}
}

func TestPlugins(t *testing.T) {
	e := newTestEnv(t)

	r := e.get("/plugins")
	e.wantStatus(r, http.StatusOK)
	var ids []string
	var list []any
	if err := jsonUnmarshal(r.body, &list); err != nil {
		t.Fatal(err)
	}
	for _, item := range list {
		p := item.(map[string]any)
		ids = append(ids, p["id"].(string))
		if _, has := p["manifest"]; has {
			t.Errorf("%s: the manifest is only included with ?manifest=true", p["id"])
		}
	}
	// sorted by ID: bundled plugins (disabled), demo (ready), needy (blocked)
	if got := strings.Join(ids, ","); !strings.Contains(got, "demo,") || !sorted(ids) {
		t.Errorf("ids = %v", ids)
	}

	r = e.get("/plugins?manifest=true")
	e.wantStatus(r, http.StatusOK)
	if err := jsonUnmarshal(r.body, &list); err != nil {
		t.Fatal(err)
	}
	var demo map[string]any
	for _, item := range list {
		if p := item.(map[string]any); p["id"] == "demo" {
			demo = p
		}
	}
	if demo == nil || demo["manifest"] == nil {
		t.Fatalf("demo with manifest missing: %v", demo)
	}
	// placeholders are resolved against the settings
	if v := field(demo, "resolvedDefaults", "install", "version"); v != float64(21) {
		t.Errorf("resolvedDefaults.install.version = %v", v)
	}
	if v := field(demo, "resolvedDefaults", "install", "channel"); v != "lts" {
		t.Errorf("resolvedDefaults.install.channel = %v", v)
	}

	r = e.get("/plugins/demo")
	e.wantStatus(r, http.StatusOK)
	one := r.json(t)
	if one["state"] != "ready" || one["manifest"] == nil || field(one, "resolvedDefaults", "install") == nil {
		t.Errorf("plugin = %v", one)
	}
	r = e.get("/plugins/needy")
	e.wantStatus(r, http.StatusOK)
	if got := r.json(t); got["state"] != "blocked" {
		t.Errorf("needy = %v", got)
	}

	r = e.get("/plugins/gap")
	e.wantStatus(r, http.StatusOK)
	if got := r.json(t); got["state"] != "incomplete" {
		t.Errorf("gap = %v", got)
	}

	e.wantStatus(e.get("/plugins/nope"), http.StatusNotFound)
	e.wantStatus(e.get("/plugins?manifest=maybe"), http.StatusUnprocessableEntity)
}

func TestPluginMessages(t *testing.T) {
	e := newTestEnv(t)
	r := e.get("/plugins/demo/messages/de")
	e.wantStatus(r, http.StatusOK)
	msgs := r.json(t)
	if msgs["demo.title"] != "Demo" || msgs["$comment"] != nil {
		t.Errorf("messages = %v", msgs)
	}
	for _, path := range []string{"/plugins/demo/messages/fr", "/plugins/nope/messages/de", "/plugins/demo/messages/xx_YY", "/plugins/demo/messages/..%2Fplugin"} {
		r := e.get(path)
		e.wantStatus(r, http.StatusNotFound)
		if r.problem(t).Code != codeNotFound {
			t.Errorf("%s: %s", path, r.body)
		}
	}
}

func TestSettings(t *testing.T) {
	e := newTestEnv(t)
	write(t, filepath.Join(e.home, ".config", "devbox", "config.json"),
		`{"profiles":[{"source":"file://${env:HOME}/profile.json"}],"settings":{"demo.mode":"slow"}}`, 0o600)
	write(t, filepath.Join(e.home, "profile.json"), demoProfile, 0o600)

	r := e.get("/settings")
	e.wantStatus(r, http.StatusOK)
	items := map[string]map[string]any{}
	var keys []string
	for _, it := range r.json(t)["settings"].([]any) {
		m := it.(map[string]any)
		items[m["key"].(string)] = m
		keys = append(keys, m["key"].(string))
	}
	if !sorted(keys) {
		t.Errorf("settings are not sorted by key: %v", keys)
	}
	if m := items["demo.mode"]; m["value"] != "slow" || m["origin"] != "user" || m["locked"] != false {
		t.Errorf("demo.mode = %v", m)
	}
	if m := items["demo.defaultVersion"]; m["value"] != float64(21) || m["origin"] != "plugin" {
		t.Errorf("demo.defaultVersion = %v", m)
	}
	if m := items["demo.pinned"]; m["value"] != "b" || m["locked"] != true || m["lockedBy"] != "profile:example-team" {
		t.Errorf("demo.pinned = %v", m)
	}
	// neither a value nor a default: value and origin are absent
	if m := items["gap.token"]; m["plugin"] != "gap" || m["value"] != nil || m["origin"] != nil || field(m, "field", "label") != "Token" {
		t.Errorf("demo.token = %v", m)
	}

	r = e.get("/settings/demo.mode/explain")
	e.wantStatus(r, http.StatusOK)
	if ex := r.json(t); ex["origin"] != "user" || ex["value"] != "slow" {
		t.Errorf("explain = %v", ex)
	}
	e.wantStatus(e.get("/settings/demo.nothing/explain"), http.StatusNotFound)
}

func TestPatchSettings(t *testing.T) {
	e := newTestEnv(t)
	write(t, filepath.Join(e.home, ".config", "devbox", "config.json"),
		`{"profiles":[{"source":"file://${env:HOME}/profile.json"}]}`, 0o600)
	write(t, filepath.Join(e.home, "profile.json"), demoProfile, 0o600)

	tests := []struct {
		name    string
		body    string
		status  int
		code    string
		pointer string
	}{
		{"set one", `{"demo.mode":"slow"}`, http.StatusNoContent, "", ""},
		{"set and unset", `{"demo.defaultVersion":17,"demo.mode":null}`, http.StatusNoContent, "", ""},
		{"wrong value", `{"demo.mode":"warp"}`, http.StatusUnprocessableEntity, codeValidation, "/demo.mode"},
		{"wrong type", `{"demo.defaultVersion":"many"}`, http.StatusUnprocessableEntity, codeValidation, "/demo.defaultVersion"},
		{"unknown key", `{"demo.nothing":1}`, http.StatusNotFound, codeNotFound, "/demo.nothing"},
		{"locked", `{"demo.pinned":"c"}`, http.StatusConflict, codeSettingLocked, "/demo.pinned"},
		{"not a key", `{"nokey":1}`, http.StatusUnprocessableEntity, codeValidation, ""},
		{"empty", `{}`, http.StatusUnprocessableEntity, codeValidation, ""},
		{"not an object", `[1]`, http.StatusUnprocessableEntity, codeValidation, ""},
		{"not JSON", `{`, http.StatusBadRequest, codeValidation, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := e.request(http.MethodPatch, "/settings", tt.body)
			e.wantStatus(r, tt.status)
			if tt.status == http.StatusNoContent {
				return
			}
			p := r.problem(t)
			if p.Code != tt.code {
				t.Errorf("code = %s, want %s", p.Code, tt.code)
			}
			if tt.pointer != "" && (len(p.Errors) == 0 || p.Errors[0].Pointer != tt.pointer) {
				t.Errorf("errors = %+v, want pointer %s", p.Errors, tt.pointer)
			}
		})
	}

	// nothing is written when any key fails
	r := e.request(http.MethodPatch, "/settings", `{"demo.mode":"fast","demo.pinned":"c"}`)
	e.wantStatus(r, http.StatusConflict)
	data, err := os.ReadFile(filepath.Join(e.home, ".config", "devbox", "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), `"fast"`) {
		t.Errorf("a failing patch wrote a value:\n%s", data)
	}

	// the stored values are the effective ones
	e.wantStatus(e.request(http.MethodPatch, "/settings", `{"demo.defaultVersion":17}`), http.StatusNoContent)
	item := e.get("/settings/demo.defaultVersion/explain").json(t)
	if item["value"] != float64(17) || item["origin"] != "user" {
		t.Errorf("explain = %v", item)
	}
	if len(e.bc.settings) == 0 {
		t.Error("the broadcaster was not told about the changed settings")
	}
}

func TestCredentials(t *testing.T) {
	e := newTestEnv(t)
	r := e.get("/credentials")
	e.wantStatus(r, http.StatusOK)
	if got := r.json(t)["instances"].([]any); len(got) != 0 {
		t.Errorf("instances = %v", got)
	}

	write(t, filepath.Join(e.home, ".local", "share", "devbox", "credentials.json"), `{
  "instances": {
    "registry-main": {
      "type": "registry",
      "origin": "user",
      "label": "Main",
      "fields": { "url": "https://registry.example.com", "retries": 3, "tls": true },
      "consumers": { "npm": {} },
      "secret": { "ref": "keyring:registry-main", "updatedAt": "2026-01-02T03:04:05Z", "expiresAt": null },
      "verified": { "at": "2026-01-02T03:04:05Z", "status": "ok" },
      "applied": { "npm": "2026-01-02T03:04:06Z" }
    }
  }
}`, 0o600)
	r = e.get("/credentials")
	e.wantStatus(r, http.StatusOK)
	got := r.json(t)["instances"].([]any)
	if len(got) != 1 {
		t.Fatalf("instances = %v", got)
	}
	inst := got[0].(map[string]any)
	if inst["name"] != "registry-main" || field(inst, "secret", "ref") != "keyring:registry-main" {
		t.Errorf("instance = %v", inst)
	}
	if strings.Contains(string(r.body), "secret\":\"") {
		t.Errorf("a secret value leaked: %s", r.body)
	}
}

func TestSchema(t *testing.T) {
	e := newTestEnv(t)
	r := e.get("/schema/config")
	e.wantStatus(r, http.StatusOK)
	if ct := r.header.Get("Content-Type"); ct != "application/schema+json" {
		t.Errorf("content type = %s", ct)
	}
	if r.json(t)["$id"] == nil {
		t.Error("not a schema")
	}
	e.wantStatus(e.get("/schema/nothing"), http.StatusNotFound)
	e.wantStatus(e.get("/schema/..%2Fx"), http.StatusNotFound)
}

func TestChecks(t *testing.T) {
	e := newTestEnv(t)

	// nothing is cached yet; reading the list finds that out and starts the check
	e.wantStatus(e.get("/checks/demo.state"), http.StatusNotFound)
	r := e.get("/checks")
	e.wantStatus(r, http.StatusOK)

	var res response
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if res = e.get("/checks/demo.state"); res.code == http.StatusOK {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	e.wantStatus(res, http.StatusOK)
	c := res.json(t)
	if c["status"] != "ok" || c["summary"] != "fine" || c["label"] != "State" {
		t.Errorf("check = %v", c)
	}
	if v := field(c, "details"); v == nil || field(c, "hints") == nil || field(c, "data", "count") != float64(2) {
		t.Errorf("details, hints or data missing: %v", c)
	}
	e.wantStatus(e.get("/checks/nope"), http.StatusNotFound)

	report := e.get("/checks").json(t)
	if field(report, "summary", "ok") != float64(1) || len(report["checks"].([]any)) != 1 {
		t.Errorf("report = %v", report)
	}
	if field(report, "host", "name") == nil {
		t.Errorf("host missing: %v", report)
	}
	if len(e.bc.checks) == 0 {
		t.Error("the broadcaster was not told about the check")
	}

	// refresh: unknown ids are refused with their index
	r = e.request(http.MethodPost, "/checks/refresh", `{"ids":["demo.state","demo.nothing"]}`)
	e.wantStatus(r, http.StatusUnprocessableEntity)
	if p := r.problem(t); len(p.Errors) != 1 || p.Errors[0].Pointer != "/ids/1" {
		t.Errorf("errors = %+v", p.Errors)
	}
	e.wantStatus(e.request(http.MethodPost, "/checks/refresh", `{"ids":["demo.state"]}`), http.StatusAccepted)
	e.wantStatus(e.request(http.MethodPost, "/checks/refresh", ``), http.StatusAccepted)
	e.wantStatus(e.request(http.MethodPost, "/checks/refresh", `{"all":true}`), http.StatusUnprocessableEntity)
}

func TestCheckRefreshCoalesces(t *testing.T) {
	c := newCheckCache()
	if !c.claim("demo.state") || c.claim("demo.state") {
		t.Error("a check that already runs must not be claimed twice")
	}
	c.release([]string{"demo.state"})
	if !c.claim("demo.state") {
		t.Error("a released check can run again")
	}
}

func TestUI(t *testing.T) {
	e := newTestEnv(t)
	get := func(path string) response {
		req, rec := newUIRequest(path)
		e.h.ServeHTTP(rec, req)
		return response{code: rec.Code, header: rec.Header(), body: rec.Body.Bytes()}
	}

	// not built: a placeholder for pages, 404 for files
	if r := get("/"); r.code != http.StatusOK || !strings.Contains(string(r.body), "make build") {
		t.Errorf("placeholder: %d %s", r.code, r.body)
	}
	if r := get("/settings"); r.code != http.StatusOK || !strings.Contains(string(r.body), "not built") {
		t.Errorf("placeholder for a route: %d %s", r.code, r.body)
	}
	if r := get("/main.js"); r.code != http.StatusNotFound {
		t.Errorf("file without a build: %d", r.code)
	}

	e.srv.ui = &uiFiles{fsys: fstest.MapFS{
		"index.html":         {Data: []byte("<html>app</html>")},
		"main-5ZJYEXWY.js":   {Data: []byte("console.log(1)")},
		"favicon.ico":        {Data: []byte("icon")},
		"assets/logo-AB.svg": {Data: []byte("<svg/>")},
	}}
	tests := []struct {
		path    string
		code    int
		body    string
		cache   string
		hasBody bool
	}{
		{"/", http.StatusOK, "app", "no-cache", true},
		{"/index.html", http.StatusOK, "app", "no-cache", true},
		{"/sessions/s_01J0000000000000000000", http.StatusOK, "app", "no-cache", true}, // client side route
		{"/main-5ZJYEXWY.js", http.StatusOK, "console.log", "public, max-age=31536000, immutable", true},
		{"/favicon.ico", http.StatusOK, "icon", "", true},
		{"/assets/logo-AB.svg", http.StatusOK, "<svg/>", "", true},
		{"/missing.js", http.StatusNotFound, "", "", false},
		{"/api/v1/nothing", http.StatusNotFound, "", "", false}, // never the UI
	}
	for _, tt := range tests {
		r := get(tt.path)
		if r.code != tt.code {
			t.Errorf("%s: status %d, want %d", tt.path, r.code, tt.code)
			continue
		}
		if tt.hasBody && !strings.Contains(string(r.body), tt.body) {
			t.Errorf("%s: body %q", tt.path, r.body)
		}
		if got := r.header.Get("Cache-Control"); got != tt.cache {
			t.Errorf("%s: Cache-Control %q, want %q", tt.path, got, tt.cache)
		}
	}
}

func TestUnknownRoute(t *testing.T) {
	e := newTestEnv(t)
	for _, tt := range []struct{ method, path string }{
		{http.MethodGet, "/nothing"},
		{http.MethodDelete, "/info"},
		{http.MethodGet, "/sessions/s_01J0000000000000000000/nothing"},
	} {
		r := e.request(tt.method, tt.path, nil)
		e.wantStatus(r, http.StatusNotFound)
		p := r.problem(t)
		if p.Code != codeNotFound || p.Instance != "/api/v1"+tt.path || !strings.HasSuffix(p.Type, "/errors/not-found") {
			t.Errorf("%s %s: %+v", tt.method, tt.path, p)
		}
	}
}
