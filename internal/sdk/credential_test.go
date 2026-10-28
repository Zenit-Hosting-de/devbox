package sdk

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestCredential(t *testing.T) {
	e := newEnv(t, false)
	req := filepath.Join(e.vars["HOME"], "request.json")
	e.vars["DEVBOX_REQUEST"] = req
	body := `{"protocol":1,"kind":"apply","plugin":"npm","name":"registry","dryRun":false,"host":{"distro":"ubuntu","distroVersion":"24.04","arch":"amd64"},
"credential":{"instance":"team-registry","type":"registry","fields":{"url":"https://nexus.example.com","layout":"nexus"},"options":{"scopes":["@example"]},"secret":"tok"}}`
	if err := os.WriteFile(req, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		part, name string
		want       any
		err        string
	}{
		{"instance", "", "team-registry", ""},
		{"type", "", "registry", ""},
		{"fields", "url", "https://nexus.example.com", ""},
		{"options", "scopes", []any{"@example"}, ""},
		{"fields", "username", nil, `no field "username"`},
		{"options", "repository", nil, `no option "repository"`},
		{"secret", "", nil, "unknown credential part"},
	}
	for _, tt := range tests {
		t.Run(tt.part+"/"+tt.name, func(t *testing.T) {
			got, err := e.Credential(tt.part, tt.name)
			if tt.err != "" {
				if err == nil || !strings.Contains(err.Error(), tt.err) {
					t.Fatalf("err = %v, want %q", err, tt.err)
				}
				return
			}
			if err != nil || !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %#v, %v; want %#v", got, err, tt.want)
			}
		})
	}
	if err := os.WriteFile(req, []byte(strings.Replace(body, `"credential":{`, `"credential":null,"x":{`, 1)), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Credential("instance", ""); err == nil || !strings.Contains(err.Error(), "no credential") {
		t.Errorf("without credential: %v", err)
	}
}

func TestJSONKeys(t *testing.T) {
	data := []byte(`{"instances":{"b-two":{"x":1},"a-one":{}},"list":[1]}`)
	tests := []struct {
		pointer string
		want    []string
		found   bool
		err     bool
	}{
		{"/instances", []string{"b-two", "a-one"}, true, false},
		{"", []string{"instances", "list"}, true, false},
		{"/instances/a-one", []string{}, true, false},
		{"/missing", nil, false, false},
		{"/list/0", nil, false, false},
		{"/list", nil, true, true},
	}
	for _, tt := range tests {
		t.Run(tt.pointer, func(t *testing.T) {
			keys, found, err := JSONKeys(data, tt.pointer)
			if (err != nil) != tt.err || found != tt.found {
				t.Fatalf("found=%v err=%v", found, err)
			}
			if tt.want != nil && !reflect.DeepEqual(keys, tt.want) {
				t.Errorf("keys %v, want %v", keys, tt.want)
			}
		})
	}
}
