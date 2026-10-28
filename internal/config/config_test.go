package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Zenit-Hosting-de/devbox/internal/jsonfile"
	"github.com/Zenit-Hosting-de/devbox/internal/platform"
	"github.com/Zenit-Hosting-de/devbox/internal/schema"
)

func num(s string) json.Number { return json.Number(s) }

// javaDefs are the settings of the concept's java manifest
// (test/fixtures/valid/concept/plugin/java.json) plus a channel setting.
func javaDefs() Definitions {
	data, err := os.ReadFile(filepath.Join("..", "..", "test", "fixtures", "valid", "concept", "plugin", "java.json"))
	if err != nil {
		panic(err)
	}
	doc, err := jsonfile.Parse(data)
	if err != nil {
		panic(err)
	}
	manifest := jsonfile.Plain(doc).(map[string]any)
	defs := Definitions{"node.channel": {"type": "string", "enum": []any{"lts", "current"}, "default": "lts", "label": "Channel"}}
	for name, field := range manifest["settings"].(map[string]any) {
		defs["java."+name] = field.(map[string]any)
	}
	return defs
}

func testContext() Context {
	return Context{
		Getenv: func(k string) string {
			return map[string]string{"HOME": "/home/a", "MIRROR": "https://mirror.example.com"}[k]
		},
		XDG:  platform.Dirs{Config: "/home/a/.config", Data: "/home/a/.local/share", State: "/s", Cache: "/c"},
		Host: platform.Host{Name: "dev-vm", User: "a", Arch: "amd64", Distro: "debian", Codename: "trixie"},
	}
}

func TestResolveLayers(t *testing.T) {
	tests := []struct {
		name       string
		layers     []Layer
		key        string
		wantValue  any
		wantOrigin string
		wantLocked bool
	}{
		{
			name: "plugin default", key: "node.channel",
			wantValue: "lts", wantOrigin: LayerPlugin,
		},
		{
			name: "system overrides plugin", key: "node.channel",
			layers:    []Layer{{Name: LayerSystem, Values: map[string]any{"node.channel": "current"}}},
			wantValue: "current", wantOrigin: LayerSystem,
		},
		{
			name: "profile overrides system", key: "java.defaultVersion",
			layers: []Layer{
				{Name: LayerSystem, Values: map[string]any{"java.defaultVersion": num("17")}},
				{Name: "profile:team", Values: map[string]any{"java.defaultVersion": num("25")}},
			},
			wantValue: num("25"), wantOrigin: "profile:team",
		},
		{
			name: "later profile overrides earlier profile", key: "java.defaultVersion",
			layers: []Layer{
				{Name: "profile:company", Values: map[string]any{"java.defaultVersion": num("17")}},
				{Name: "profile:team", Values: map[string]any{"java.defaultVersion": num("21")}},
			},
			wantValue: num("21"), wantOrigin: "profile:team",
		},
		{
			name: "user overrides profile", key: "node.channel",
			layers: []Layer{
				{Name: "profile:team", Values: map[string]any{"node.channel": "lts"}},
				{Name: LayerUser, Values: map[string]any{"node.channel": "current"}},
			},
			wantValue: "current", wantOrigin: LayerUser,
		},
		{
			name: "env overrides user and is coerced", key: "java.keepPatches",
			layers: []Layer{
				{Name: LayerUser, Values: map[string]any{"java.keepPatches": num("2")}},
				{Name: LayerEnv, Values: map[string]any{"java.keepPatches": "3"}},
			},
			wantValue: num("3"), wantOrigin: LayerEnv,
		},
		{
			name: "call overrides env", key: "node.channel",
			layers: []Layer{
				{Name: LayerEnv, Values: map[string]any{"node.channel": "lts"}},
				{Name: LayerCall, Values: map[string]any{"node.channel": "current"}},
			},
			wantValue: "current", wantOrigin: LayerCall,
		},
		{
			name: "locked profile value wins over user file", key: "java.vendorApi",
			layers: []Layer{
				{Name: "profile:example-team", Values: map[string]any{"java.vendorApi": "https://mirror.example.com/adoptium/v3"}, Locked: map[string]bool{"java.vendorApi": true}},
				{Name: LayerUser, Values: map[string]any{"java.vendorApi": "https://other.example.com"}},
			},
			wantValue: "https://mirror.example.com/adoptium/v3", wantOrigin: "profile:example-team", wantLocked: true,
		},
		{
			name: "placeholders: xdg", key: "java.installDir",
			wantValue: "/home/a/.local/share/jdk", wantOrigin: LayerPlugin,
		},
		{
			name: "placeholders: env inside text", key: "java.vendorApi",
			layers:    []Layer{{Name: LayerUser, Values: map[string]any{"java.vendorApi": "${env:MIRROR}/adoptium/v3"}}},
			wantValue: "https://mirror.example.com/adoptium/v3", wantOrigin: LayerUser,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := Resolve(javaDefs(), tt.layers, testContext())
			if err != nil {
				t.Fatal(err)
			}
			s, ok := res.Get(tt.key)
			if !ok {
				t.Fatalf("no setting %s", tt.key)
			}
			if !reflect.DeepEqual(s.Value, tt.wantValue) || s.Origin != tt.wantOrigin || s.Locked != tt.wantLocked {
				t.Errorf("got %v (%s, locked %v), want %v (%s, locked %v)", s.Value, s.Origin, s.Locked, tt.wantValue, tt.wantOrigin, tt.wantLocked)
			}
		})
	}
}

func TestResolveErrors(t *testing.T) {
	locked := Layer{Name: "profile:team", Values: map[string]any{"node.channel": "lts"}, Locked: map[string]bool{"node.channel": true}}
	tests := []struct {
		name     string
		defs     Definitions
		layers   []Layer
		wantCode string
		wantText string
	}{
		{
			name: "env cannot override a lock", layers: []Layer{locked, {Name: LayerEnv, Values: map[string]any{"node.channel": "current"}}},
			wantCode: CodeSettingLocked, wantText: "locked by profile:team",
		},
		{
			name: "call cannot override a lock", layers: []Layer{locked, {Name: LayerCall, Values: map[string]any{"node.channel": "current"}}},
			wantCode: CodeSettingLocked,
		},
		{
			name: "unknown key on the command line", layers: []Layer{{Name: LayerCall, Values: map[string]any{"node.nope": "x"}}},
			wantCode: CodeNotFound,
		},
		{
			name: "value not allowed by the field", layers: []Layer{{Name: LayerUser, Values: map[string]any{"java.defaultVersion": num("22")}}},
			wantCode: CodeValidation, wantText: "enum",
		},
		{
			name: "text that is not an integer", layers: []Layer{{Name: LayerCall, Values: map[string]any{"java.keepPatches": "many"}}},
			wantCode: CodeValidation, wantText: "not an integer",
		},
		{
			name: "cycle with path",
			defs: Definitions{
				"ab.x": {"type": "string", "default": "${ab.y}", "label": "x"},
				"ab.y": {"type": "string", "default": "pre-${ab.z}", "label": "y"},
				"ab.z": {"type": "string", "default": "${ab.x}", "label": "z"},
			},
			wantCode: CodeValidation, wantText: "placeholder cycle: ab.x -> ab.y -> ab.z -> ab.x",
		},
		{
			name:     "reference to an unknown setting",
			defs:     Definitions{"ab.x": {"type": "string", "default": "${ab.missing}", "label": "x"}},
			wantCode: CodeNotFound,
		},
		{
			name:     "unknown placeholder kind",
			defs:     Definitions{"ab.x": {"type": "string", "default": "${foo}", "label": "x"}},
			wantCode: CodeValidation, wantText: "unknown placeholder",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defs := tt.defs
			if defs == nil {
				defs = javaDefs()
			}
			_, err := Resolve(defs, tt.layers, testContext())
			if !IsCode(err, tt.wantCode) {
				t.Fatalf("err = %v, want code %s", err, tt.wantCode)
			}
			if !strings.Contains(err.Error(), tt.wantText) {
				t.Errorf("err = %v, want it to contain %q", err, tt.wantText)
			}
		})
	}
}

func TestResolveWarnsAboutUnknownKeysInFiles(t *testing.T) {
	res, err := Resolve(javaDefs(), []Layer{{Name: LayerUser, Values: map[string]any{"gone.setting": "x"}}}, testContext())
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Warnings) != 1 || !strings.Contains(res.Warnings[0], "gone.setting") {
		t.Errorf("warnings = %v", res.Warnings)
	}
}

func TestExpand(t *testing.T) {
	lookup := func(ref string) (any, error) {
		return map[string]any{"a.n": num("21"), "a.s": "x", "a.b": true}[ref], nil
	}
	tests := map[string]any{
		"plain":           "plain",
		"${a.n}":          num("21"),
		"v${a.n}":         "v21",
		"$${a.n}":         "${a.n}",
		"${a.s}-${a.b}":   "x-true",
		"a $${literal} b": "a ${literal} b",
		"${a.s}$${a.s}":   "x${a.s}",
	}
	for in, want := range tests {
		got, err := Expand(in, lookup)
		if err != nil {
			t.Errorf("Expand(%q): %v", in, err)
			continue
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("Expand(%q) = %#v, want %#v", in, got, want)
		}
	}
	if _, err := Expand("${a.s", lookup); err == nil {
		t.Error("unclosed placeholder: want an error")
	}
}

func TestEnvLayer(t *testing.T) {
	defs := Definitions{"node.channel": {"type": "string", "label": "c"}, "lang-de.mode": {"type": "string", "label": "m"}, "java.defaultVersion": {"type": "integer", "label": "v"}}
	l, err := EnvLayer([]string{"PATH=/bin", "DEVBOX_SET__NODE__CHANNEL=lts", "DEVBOX_SET__LANG_DE__MODE=x", "DEVBOX_SET__JAVA__DEFAULTVERSION=17"}, defs)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"node.channel": "lts", "lang-de.mode": "x", "java.defaultVersion": "17"}
	if !reflect.DeepEqual(l.Values, want) {
		t.Errorf("values = %v, want %v", l.Values, want)
	}
	if _, err := EnvLayer([]string{"DEVBOX_SET__NOPE__X=1"}, defs); !IsCode(err, CodeNotFound) {
		t.Errorf("unknown variable: err = %v", err)
	}
}

// TestExplainConceptExample reproduces `devbox config explain java.vendorApi --json`
// from docs/concept.md (fixture test/fixtures/valid/concept/openapi.SettingExplain).
func TestExplainConceptExample(t *testing.T) {
	profile := Layer{
		Name:   "profile:example-team",
		Values: map[string]any{"java.vendorApi": "https://mirror.example.com/adoptium/v3"},
		Locked: map[string]bool{"java.vendorApi": true},
	}
	res, err := Resolve(javaDefs(), []Layer{profile}, testContext())
	if err != nil {
		t.Fatal(err)
	}
	ex, err := res.Explain("java.vendorApi")
	if err != nil {
		t.Fatal(err)
	}
	got, err := json.Marshal(ex)
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile(filepath.Join("..", "..", "test", "fixtures", "valid", "concept", "openapi.SettingExplain", "java-vendor-api.json"))
	if err != nil {
		t.Fatal(err)
	}
	var g, w any
	_ = json.Unmarshal(got, &g)
	_ = json.Unmarshal(want, &w)
	if !reflect.DeepEqual(g, w) {
		t.Errorf("explain = %s\nwant %s", got, want)
	}
	v, err := schema.New()
	if err != nil {
		t.Fatal(err)
	}
	if err := v.ValidateJSON("openapi.SettingExplain", got); err != nil {
		t.Error(err)
	}
}

func TestSetAndUnset(t *testing.T) {
	v, err := schema.New()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte("{\n  \"ui\": {\n    \"language\": \"en\"\n  }\n}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	defs := javaDefs()
	res, err := Resolve(defs, nil, testContext())
	if err != nil {
		t.Fatal(err)
	}
	if err := Set(v, path, defs, res, "java.defaultVersion", "17"); err != nil {
		t.Fatal(err)
	}
	if err := Set(v, path, defs, res, "node.channel", "current"); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	id, _ := v.ID("config")
	want := "{\n  \"$schema\": \"" + id + "\",\n  \"settings\": {\n    \"java.defaultVersion\": 17,\n    \"node.channel\": \"current\"\n  },\n  \"ui\": {\n    \"language\": \"en\"\n  }\n}\n"
	if string(data) != want {
		t.Errorf("file =\n%s\nwant\n%s", data, want)
	}
	if err := Set(v, path, defs, res, "java.defaultVersion", "22"); !IsCode(err, CodeValidation) {
		t.Errorf("invalid value: err = %v", err)
	}
	if err := Set(v, path, defs, res, "java.nope", "1"); !IsCode(err, CodeNotFound) {
		t.Errorf("unknown key: err = %v", err)
	}
	locked, err := Resolve(defs, []Layer{{Name: "profile:team", Values: map[string]any{"node.channel": "lts"}, Locked: map[string]bool{"node.channel": true}}}, testContext())
	if err != nil {
		t.Fatal(err)
	}
	if err := Set(v, path, defs, locked, "node.channel", "current"); !IsCode(err, CodeSettingLocked) {
		t.Errorf("locked key: err = %v", err)
	}
	if err := Unset(v, path, "java.defaultVersion"); err != nil {
		t.Fatal(err)
	}
	if err := Unset(v, path, "node.channel"); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(path)
	doc, err := jsonfile.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := doc.(*jsonfile.Object).Get("settings"); ok {
		t.Errorf("empty settings object kept:\n%s", data)
	}
}

func TestReadFileReportsLine(t *testing.T) {
	v, err := schema.New()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte("{\n  \"settings\": {},\n  \"colour\": \"blue\"\n}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = ReadFile(v, "config", path, false)
	if !IsCode(err, CodeValidation) || !strings.Contains(err.Error(), path+":3: /colour") || !strings.Contains(err.Error(), "additionalProperties") {
		t.Errorf("err = %v", err)
	}
	if _, err := ReadFile(v, "config", filepath.Join(t.TempDir(), "missing.json"), true); err != nil {
		t.Errorf("missing optional file: %v", err)
	}
}
