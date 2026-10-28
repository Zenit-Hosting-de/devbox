package credentials

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Zenit-Hosting-de/devbox/internal/schema"
)

func validator(t *testing.T) *schema.Validator {
	t.Helper()
	v, err := schema.New()
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func profileDoc() map[string]any {
	return map[string]any{
		"id": "example-team",
		"credentials": map[string]any{
			"team-registry": map[string]any{
				"type":   "registry",
				"label":  "Team registry",
				"fields": map[string]any{"url": "https://nexus.example.com", "layout": "nexus"},
				"locked": []any{"url"},
				"consumers": map[string]any{
					"npm":   map[string]any{"repository": "npm-group"},
					"maven": map[string]any{"mirror": "maven-public"},
				},
			},
		},
	}
}

func TestMerge(t *testing.T) {
	prepared := FromProfile(profileDoc())
	expires := "2027-01-25T00:00:00Z"
	tests := []struct {
		name   string
		stored map[string]*Stored
		want   map[string]Instance
	}{
		{
			name:   "profile only",
			stored: nil,
			want: map[string]Instance{"team-registry": {
				Name: "team-registry", Type: "registry", Origin: "profile:example-team", Label: "Team registry",
				Fields: map[string]any{"url": "https://nexus.example.com", "layout": "nexus"}, Locked: []string{"url"},
				Consumers: map[string]map[string]any{"npm": {"repository": "npm-group"}, "maven": {"mirror": "maven-public"}},
				Applied:   map[string]string{},
			}},
		},
		{
			name: "user fields, consumers and secret merged; locked field kept",
			stored: map[string]*Stored{"team-registry": {
				Type: "registry", Origin: "profile:example-team",
				Fields:    map[string]any{"url": "https://evil.example.com", "layout": "artifactory", "username": "m.muster"},
				Consumers: map[string]map[string]any{"npm": {"scopes": []any{"@example"}}, "pip": {"repository": "pypi-group"}},
				Secret:    &Secret{Ref: "age:team-registry", UpdatedAt: "2026-10-27T09:12:00Z", ExpiresAt: &expires},
				Applied:   map[string]string{"npm": "2026-10-27T09:12:03Z"},
			}},
			want: map[string]Instance{"team-registry": {
				Name: "team-registry", Type: "registry", Origin: "profile:example-team", Label: "Team registry",
				Fields: map[string]any{"url": "https://nexus.example.com", "layout": "artifactory", "username": "m.muster"},
				Locked: []string{"url"},
				Consumers: map[string]map[string]any{
					"npm":   {"repository": "npm-group", "scopes": []any{"@example"}},
					"maven": {"mirror": "maven-public"},
					"pip":   {"repository": "pypi-group"},
				},
				Secret:  &Secret{Ref: "age:team-registry", UpdatedAt: "2026-10-27T09:12:00Z", ExpiresAt: &expires},
				Applied: map[string]string{"npm": "2026-10-27T09:12:03Z"},
			}},
		},
		{
			name: "user instance next to the profile one",
			stored: map[string]*Stored{"mine": {
				Type: "registry", Origin: "user", Label: "Mine",
				Fields: map[string]any{"url": "https://mine.example.com"},
			}},
			want: map[string]Instance{
				"mine": {
					Name: "mine", Type: "registry", Origin: "user", Label: "Mine",
					Fields: map[string]any{"url": "https://mine.example.com"}, Locked: []string{},
					Consumers: map[string]map[string]any{}, Applied: map[string]string{},
				},
				"team-registry": {
					Name: "team-registry", Type: "registry", Origin: "profile:example-team", Label: "Team registry",
					Fields: map[string]any{"url": "https://nexus.example.com", "layout": "nexus"}, Locked: []string{"url"},
					Consumers: map[string]map[string]any{"npm": {"repository": "npm-group"}, "maven": {"mirror": "maven-public"}},
					Applied:   map[string]string{},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := &File{Instances: tt.stored}
			if f.Instances == nil {
				f.Instances = map[string]*Stored{}
			}
			got := Merge(f, prepared)
			if len(got) != len(tt.want) {
				t.Fatalf("got %d instances, want %d", len(got), len(tt.want))
			}
			for i, inst := range got {
				if i > 0 && got[i-1].Name >= inst.Name {
					t.Errorf("instances not sorted: %s before %s", got[i-1].Name, inst.Name)
				}
				want := tt.want[inst.Name]
				inst.Prepared = nil
				if !reflect.DeepEqual(inst, want) {
					t.Errorf("%s:\n got %#v\nwant %#v", inst.Name, inst, want)
				}
			}
		})
	}
}

func TestLaterProfileWins(t *testing.T) {
	second := map[string]any{"id": "second", "credentials": map[string]any{
		"team-registry": map[string]any{"type": "registry", "fields": map[string]any{"url": "https://second.example.com"}},
	}}
	prepared := append(FromProfile(profileDoc()), FromProfile(second)...)
	got := Merge(&File{Instances: map[string]*Stored{}}, prepared)
	if len(got) != 1 || got[0].Origin != "profile:second" || got[0].Fields["url"] != "https://second.example.com" {
		t.Fatalf("got %#v", got)
	}
}

func TestForget(t *testing.T) {
	tests := []struct {
		name     string
		stored   *Stored
		prepared bool
		want     *Stored // nil: entry removed
	}{
		{"user instance disappears", &Stored{Type: "registry", Origin: "user", Secret: &Secret{Ref: "age:x"}}, false, nil},
		{
			"profile instance keeps the user's fields",
			&Stored{
				Type: "registry", Origin: "profile:example-team", Fields: map[string]any{"username": "m.muster"},
				Consumers: map[string]map[string]any{"pip": {}}, Secret: &Secret{Ref: "age:x"},
				Verified: &Verified{At: "2026-10-27T09:12:00Z", Status: VerifiedOK}, Applied: map[string]string{"pip": "2026-10-27T09:12:00Z"},
			},
			true,
			&Stored{Type: "registry", Origin: "profile:example-team", Fields: map[string]any{"username": "m.muster"}},
		},
		{"profile instance without user fields disappears", &Stored{Type: "registry", Origin: "profile:example-team", Secret: &Secret{Ref: "age:x"}}, true, nil},
		{"orphaned profile instance disappears", &Stored{Type: "registry", Origin: "profile:gone", Fields: map[string]any{"username": "x"}}, false, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := &File{Instances: map[string]*Stored{"x": tt.stored}}
			f.Forget("x", tt.prepared)
			got := f.Instances["x"]
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestSaveLoad(t *testing.T) {
	v := validator(t)
	path := filepath.Join(t.TempDir(), "devbox", FileName)
	expires := "2027-01-25T00:00:00Z"
	err := Update(v, path, func(f *File) error {
		s := f.Entry("team-registry", "registry", "profile:example-team")
		s.Fields = map[string]any{"username": "m.muster"}
		s.Secret = &Secret{Ref: "keyring:devbox/team-registry", UpdatedAt: "2026-10-27T09:12:00Z", ExpiresAt: &expires}
		s.Applied = map[string]string{"npm": "2026-10-27T09:12:03Z", "maven": "2026-10-27T09:12:04Z"}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path) //nolint:gosec // test file
	if err != nil {
		t.Fatal(err)
	}
	want := `{
  "$schema": "SCHEMA",
  "instances": {
    "team-registry": {
      "type": "registry",
      "origin": "profile:example-team",
      "fields": {
        "username": "m.muster"
      },
      "secret": {
        "ref": "keyring:devbox/team-registry",
        "updatedAt": "2026-10-27T09:12:00Z",
        "expiresAt": "2027-01-25T00:00:00Z"
      },
      "applied": {
        "maven": "2026-10-27T09:12:04Z",
        "npm": "2026-10-27T09:12:03Z"
      }
    }
  }
}
`
	id, _ := v.ID(SchemaName)
	want = strings.Replace(want, "SCHEMA", id, 1)
	if string(data) != want {
		t.Errorf("file:\n%s\nwant:\n%s", data, want)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("mode %v, want 0600", info.Mode().Perm())
	}
	f, err := Load(v, path)
	if err != nil {
		t.Fatal(err)
	}
	if got := f.Instances["team-registry"].Secret.Ref; got != "keyring:devbox/team-registry" {
		t.Errorf("ref %q", got)
	}
}

func TestLoadErrors(t *testing.T) {
	v := validator(t)
	tests := []struct {
		name, content, want string
	}{
		{"unknown key", `{"instances":{},"extra":1}`, "additionalProperties"},
		{"missing origin", `{"instances":{"x1":{"type":"registry"}}}`, "origin"},
		{"bad ref", `{"instances":{"x1":{"type":"registry","origin":"user","secret":{"ref":"plain:x"}}}}`, "ref"},
		{"not JSON", `{`, "parse JSON"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), FileName)
			if err := os.WriteFile(path, []byte(tt.content), 0o600); err != nil {
				t.Fatal(err)
			}
			_, err := Load(v, path)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("err = %v, want it to mention %q", err, tt.want)
			}
		})
	}
	f, err := Load(v, filepath.Join(t.TempDir(), "missing.json"))
	if err != nil || len(f.Instances) != 0 {
		t.Errorf("missing file: %v, %v", f, err)
	}
}
