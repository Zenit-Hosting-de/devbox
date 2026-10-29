package repo

import (
	"strings"
	"testing"
)

const commit = "0123456789abcdef0123456789abcdef01234567"

func layerOf(origin string, entries map[string]any) Layer {
	return Layer{Origin: origin, Repositories: entries}
}

func plugins(source string, extra map[string]any) map[string]any {
	m := map[string]any{"kind": "plugins", "source": source}
	for k, v := range extra {
		m[k] = v
	}
	return m
}

func TestCollectPrecedence(t *testing.T) {
	tests := []struct {
		name       string
		layers     []Layer
		wantSource string
		wantOrigin string
		wantLocked bool
	}{
		{
			"user wins over a profile",
			[]Layer{
				layerOf("profile:team", map[string]any{"x-repo": plugins("https://a.example.com/a.tgz", nil)}),
				layerOf(OriginUser, map[string]any{"x-repo": plugins("https://b.example.com/b.tgz", nil)}),
			},
			"https://b.example.com/b.tgz", OriginUser, false,
		},
		{
			"a locked profile entry wins over the user",
			[]Layer{
				layerOf("profile:team", map[string]any{"x-repo": plugins("https://a.example.com/a.tgz", map[string]any{"locked": true})}),
				layerOf(OriginUser, map[string]any{"x-repo": plugins("https://b.example.com/b.tgz", nil)}),
			},
			"https://a.example.com/a.tgz", "profile:team", true,
		},
		{
			"a locked system entry wins over a profile",
			[]Layer{
				layerOf(OriginSystem, map[string]any{"x-repo": plugins("https://s.example.com/s.tgz", map[string]any{"locked": true})}),
				layerOf("profile:team", map[string]any{"x-repo": plugins("https://a.example.com/a.tgz", nil)}),
			},
			"https://s.example.com/s.tgz", OriginSystem, true,
		},
		{
			"a later profile wins over an earlier one",
			[]Layer{
				layerOf("profile:one", map[string]any{"x-repo": plugins("https://1.example.com/1.tgz", nil)}),
				layerOf("profile:two", map[string]any{"x-repo": plugins("https://2.example.com/2.tgz", nil)}),
			},
			"https://2.example.com/2.tgz", "profile:two", false,
		},
		{
			"locked in the user file locks nothing",
			[]Layer{layerOf(OriginUser, map[string]any{"x-repo": plugins("https://b.example.com/b.tgz", map[string]any{"locked": true})})},
			"https://b.example.com/b.tgz", OriginUser, false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Collect(tt.layers, nil)
			if len(got) != 1 {
				t.Fatalf("entries = %v", got)
			}
			e := got[0]
			if e.Source != tt.wantSource || e.Origin != tt.wantOrigin || e.Locked != tt.wantLocked {
				t.Errorf("entry = %+v", e)
			}
		})
	}
}

func TestCollectDefaultsAndOrder(t *testing.T) {
	got := Collect([]Layer{layerOf(OriginUser, map[string]any{
		"zeta":  map[string]any{"kind": "catalog", "source": "https://c.example.com/index.json", "trust": map[string]any{"issuer": "x", "identity": "y"}},
		"alpha": plugins("file:///srv/plugins", map[string]any{"enabled": false}),
		"beta":  plugins("https://b.example.com/b.tgz", nil),
	})}, nil)
	if names := strings.Join(Names(got), ","); names != "alpha,beta,zeta" {
		t.Fatalf("names = %s", names)
	}
	if got[0].Enabled || !got[1].Enabled || got[1].Trust != TrustPrompt {
		t.Errorf("enabled/trust: %+v %+v", got[0], got[1])
	}
	if got[2].Trust != TrustSignature || got[2].Supported() {
		t.Errorf("catalog entry: %+v", got[2])
	}
}

func TestEntryProblem(t *testing.T) {
	tests := []struct {
		name  string
		entry map[string]any
		want  string // part of the message, "" for no problem
	}{
		{"https tarball", plugins("https://repo.example.com/p.tar.gz", nil), ""},
		{"git over https", plugins("git+https://git.example.com/p.git", map[string]any{"ref": "main"}), ""},
		{"git over ssh", plugins("git+ssh://git@git.example.com/p.git", nil), ""},
		{"local git", plugins("git+file:///srv/p.git", nil), ""},
		{"local directory", plugins("file:///srv/plugins", map[string]any{"path": "plugins/sub"}), ""},
		{"pinned git commit", plugins("git+https://git.example.com/p.git", map[string]any{"ref": commit, "trust": "pinned"}), ""},
		{"pinned without ref", plugins("git+https://git.example.com/p.git", map[string]any{"trust": "pinned"}), "pinned needs"},
		{"pinned with a branch", plugins("git+https://git.example.com/p.git", map[string]any{"ref": "main", "trust": "pinned"}), "pinned needs"},
		{"pinned with an abbreviated commit", plugins("git+https://git.example.com/p.git", map[string]any{"ref": commit[:12], "trust": "pinned"}), "pinned needs"},
		{"pinned with a tarball", plugins("https://repo.example.com/p.tgz", map[string]any{"trust": "pinned"}), "pinned needs"},
		{"signature trust", plugins("git+https://git.example.com/p.git", map[string]any{"trust": map[string]any{"issuer": "i", "identity": "d"}}), "AP-23"},
		{"credential", plugins("git+https://git.example.com/p.git", map[string]any{"credential": "company-git"}), "not supported yet"},
		{"ref on a tarball", plugins("https://repo.example.com/p.tgz", map[string]any{"ref": "main"}), "only supported for git"},
		{"ref that looks like an option", plugins("git+https://git.example.com/p.git", map[string]any{"ref": "--upload-pack=x"}), "not valid"},
		{"file with a host", plugins("file://other/srv", nil), "no host"},
		{"relative file path", plugins("file://srv", nil), "no host"},
		{"path leaves the repository", plugins("file:///srv/p", map[string]any{"path": "../x"}), "relative directory"},
		{"absolute path", plugins("file:///srv/p", map[string]any{"path": "/etc"}), "relative directory"},
		{"missing source", map[string]any{"kind": "plugins"}, "source is required"},
		{"other kind is not checked", map[string]any{"kind": "packages", "credential": "x"}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := Collect([]Layer{layerOf(OriginUser, map[string]any{"some-repo": tt.entry})}, nil)[0]
			err := e.Problem()
			switch {
			case tt.want == "" && err != nil:
				t.Errorf("unexpected problem: %v", err)
			case tt.want != "" && (err == nil || !strings.Contains(err.Error(), tt.want)):
				t.Errorf("problem = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestEntryExpandsPlaceholders(t *testing.T) {
	expand := func(s string) (string, error) { return strings.ReplaceAll(s, "${env:HOME}", "/home/u"), nil }
	e := Collect([]Layer{layerOf(OriginUser, map[string]any{"some-repo": plugins("file://${env:HOME}/src", map[string]any{"path": "p"})})}, expand)[0]
	if err := e.Problem(); err != nil {
		t.Fatal(err)
	}
	if e.source != "file:///home/u/src" || e.Source != "file://${env:HOME}/src" || e.SubPath() != "p" {
		t.Errorf("source = %q, raw = %q, path = %q", e.source, e.Source, e.SubPath())
	}
}

func TestValidateNew(t *testing.T) {
	if err := ValidateNew("team", "git+https://git.example.com/p.git", commit, "", TrustPinned); err != nil {
		t.Error(err)
	}
	if err := ValidateNew("team", "git+https://git.example.com/p.git", "main", "", TrustPinned); err == nil {
		t.Error("pinned with a branch must be refused")
	}
	if err := ValidateNew("team", "ftp://x.example.com/p", "", "", ""); err == nil {
		t.Error("an unknown scheme must be refused")
	}
}
