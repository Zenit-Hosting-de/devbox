package profile

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Zenit-Hosting-de/devbox/internal/config"
	"github.com/Zenit-Hosting-de/devbox/internal/schema"
)

const testProfile = `{"id": "team", "name": "Team", "version": "1.0.0"}`

func newFetcher(t *testing.T) *Fetcher {
	t.Helper()
	v, err := schema.New()
	if err != nil {
		t.Fatal(err)
	}
	return &Fetcher{CacheDir: filepath.Join(t.TempDir(), "cache"), Validator: v}
}

func TestLocate(t *testing.T) {
	f := newFetcher(t)
	tests := []struct {
		name    string
		src     Source
		kind    string
		address string
		path    string
		wantErr string
	}{
		{name: "file", src: Source{Source: "file:///etc/team.json"}, kind: KindFile, address: "/etc/team.json"},
		{name: "file with path", src: Source{Source: "file:///srv/profiles/", Path: "team.json"}, kind: KindFile, address: "/srv/profiles/team.json"},
		{name: "file with host", src: Source{Source: "file://host/team.json"}, wantErr: "has no host"},
		{name: "https", src: Source{Source: "https://example.com/team.json"}, kind: KindHTTPS, address: "https://example.com/team.json"},
		{name: "https with ref", src: Source{Source: "https://example.com/team.json", Ref: "main"}, wantErr: "only supported for Git"},
		{name: "https with user", src: Source{Source: "https://u:p@example.com/team.json"}, wantErr: "user information"}, //nolint:gosec // an address with user information is the case under test
		{name: "git default path", src: Source{Source: "git+https://example.com/team.git"}, kind: KindGit, address: "git+https://example.com/team.git", path: DefaultGitPath},
		{name: "git path", src: Source{Source: "git+ssh://git@example.com/team.git", Path: "a/b.json"}, kind: KindGit, address: "git+ssh://git@example.com/team.git", path: "a/b.json"},
		{name: "git path outside", src: Source{Source: "git+https://example.com/team.git", Path: "../x.json"}, wantErr: "inside the repository"},
		{name: "no scheme", src: Source{Source: "/etc/team.json"}, wantErr: "no scheme"},
		{name: "http", src: Source{Source: "http://example.com/team.json"}, wantErr: "not supported"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			loc, err := f.Locate(tt.src)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if loc.Kind != tt.kind || loc.Address != tt.address || loc.Path != tt.path {
				t.Errorf("got %+v", loc)
			}
			if loc.Remote() != (loc.Key != "") {
				t.Errorf("key %q for remote=%v", loc.Key, loc.Remote())
			}
		})
	}
}

func TestLocateKeyDependsOnRefAndPath(t *testing.T) {
	f := newFetcher(t)
	a, _ := f.Locate(Source{Source: "git+https://example.com/t.git"})
	b, _ := f.Locate(Source{Source: "git+https://example.com/t.git", Ref: "v1"})
	c, _ := f.Locate(Source{Source: "git+https://example.com/t.git", Path: "x.json"})
	if a.Key == b.Key || a.Key == c.Key || b.Key == c.Key {
		t.Errorf("keys collide: %s %s %s", a.Key, b.Key, c.Key)
	}
}

func TestFetchHTTPS(t *testing.T) {
	body := testProfile
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/team.json" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()
	f := newFetcher(t)
	src := Source{Source: strings.Replace(srv.URL, "http://", "https://", 1) + "/team.json"}
	f.Client = &http.Client{Transport: rewrite{srv.URL}}
	got, err := f.Fetch(context.Background(), src)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Changed || len(got.Revision) != 64 {
		t.Errorf("first fetch: %+v", got)
	}
	again, err := f.Fetch(context.Background(), src)
	if err != nil {
		t.Fatal(err)
	}
	if again.Changed || again.Revision != got.Revision {
		t.Errorf("second fetch: %+v", again)
	}
	body = `{"id": "team"}`
	if _, err := f.Fetch(context.Background(), src); !config.IsCode(err, config.CodeValidation) {
		t.Fatalf("invalid profile: err = %v", err)
	}
	data, err := os.ReadFile(got.Path)
	if err != nil || string(data) != testProfile {
		t.Errorf("the previous copy must stay: %q, %v", data, err)
	}
	entries, _ := os.ReadDir(f.CacheDir)
	if len(entries) != 1 {
		t.Errorf("cache holds %d entries, want 1 (no temporary directories)", len(entries))
	}
}

// rewrite sends https requests to the plain test server.
type rewrite struct{ base string }

func (r rewrite) RoundTrip(req *http.Request) (*http.Response, error) {
	u := *req.URL
	u.Scheme = "http"
	u.Host = strings.TrimPrefix(r.base, "http://")
	out := req.Clone(req.Context())
	out.URL = &u
	return http.DefaultTransport.RoundTrip(out)
}

func TestFetchGit(t *testing.T) {
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git is not installed")
	}
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "p"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "p", "team.json"), []byte(testProfile), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/etc/passwd", filepath.Join(dir, "link.json")); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"init", "-q"}, {"add", "."}, {"-c", "user.name=T", "-c", "user.email=t@example.com", "-c", "commit.gpgsign=false", "commit", "-qm", "one"}} {
		cmd := exec.Command(git, args...) //nolint:gosec // test
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	f := newFetcher(t)
	f.Git = func(ctx context.Context, _, source, _, dest string) (string, error) {
		addr := strings.TrimPrefix(source, "git+file://")
		cmd := exec.CommandContext(ctx, git, "clone", "-q", addr, dest) //nolint:gosec // test
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("clone: %v\n%s", err, out)
		}
		return "abc", nil
	}
	got, err := f.Fetch(context.Background(), Source{Source: "git+file://" + dir, Path: "p/team.json"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Revision != "abc" {
		t.Errorf("revision %q", got.Revision)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(got.Path), "checkout")); !os.IsNotExist(err) {
		t.Errorf("the checkout must not stay in the cache: %v", err)
	}
	if _, err := f.Fetch(context.Background(), Source{Source: "git+file://" + dir, Path: "link.json"}); err == nil || !strings.Contains(err.Error(), "not a regular file") {
		t.Errorf("a link must be refused: %v", err)
	}
}

func TestVerify(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "profile.json")
	if err := os.WriteFile(file, []byte(testProfile), 0o600); err != nil {
		t.Fatal(err)
	}
	cosign := filepath.Join(dir, "cosign")
	script := "#!/bin/sh\necho \"$@\" >> " + filepath.Join(dir, "calls") + "\ngrep -q good \"$3\" || { echo 'Error: bad signature' >&2; exit 1; }\n"
	if err := os.WriteFile(cosign, []byte(script), 0o700); err != nil { //nolint:gosec // executable fake
		t.Fatal(err)
	}
	f := newFetcher(t)
	f.LookPath = func(string) (string, error) { return cosign, nil }
	src := Source{Source: "file://" + file, Trust: &Trust{Issuer: "https://issuer.example.com", Identity: "id-" + t.Name()}}

	if err := f.Verify(context.Background(), src, file); err == nil || !strings.Contains(err.Error(), "no signature bundle") {
		t.Fatalf("missing bundle: %v", err)
	}
	if err := os.WriteFile(file+BundleSuffix, []byte("bad"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := f.Verify(context.Background(), src, file); !config.IsCode(err, config.CodeValidation) || !strings.Contains(err.Error(), "Error: bad signature") {
		t.Fatalf("bad bundle: %v", err)
	}
	if err := os.WriteFile(file+BundleSuffix, []byte("good"), 0o600); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := f.Verify(context.Background(), src, file); err != nil {
			t.Fatalf("good bundle: %v", err)
		}
	}
	calls, _ := os.ReadFile(filepath.Join(dir, "calls"))
	if n := strings.Count(string(calls), "verify-blob"); n != 2 {
		t.Errorf("cosign ran %d times, want 2 (the good result is remembered)", n)
	}
	if !strings.Contains(string(calls), "--certificate-identity id-"+t.Name()+" --certificate-oidc-issuer https://issuer.example.com") {
		t.Errorf("arguments: %s", calls)
	}
	f.LookPath = func(string) (string, error) { return "", os.ErrNotExist }
	src.Trust.Identity = "other"
	if err := f.Verify(context.Background(), src, file); !config.IsCode(err, config.CodePrerequisite) {
		t.Errorf("without cosign: %v", err)
	}
}

func TestSourcesOfAndFields(t *testing.T) {
	doc := map[string]any{"profiles": []any{
		map[string]any{"source": "git+https://example.com/t.git", "ref": "main", "path": "p.json", "trust": map[string]any{"issuer": "i", "identity": "d"}},
		map[string]any{"source": "file:///x.json"},
	}}
	got := SourcesOf(doc, OriginUser)
	if len(got) != 2 || got[0].Trust == nil || got[0].Trust.Identity != "d" || got[1].Origin != OriginUser {
		t.Fatalf("%+v", got)
	}
	fields := got[0].Fields()
	if len(fields) != 4 || fields[0][0] != "source" || fields[3][0] != "trust" {
		t.Errorf("fields %v", fields)
	}
	if !got[0].Same(Source{Source: "git+https://example.com/t.git", Ref: "main", Path: "p.json"}) {
		t.Error("Same ignores trust and origin")
	}
}
