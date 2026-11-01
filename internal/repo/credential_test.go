package repo

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/Zenit-Hosting-de/devbox/internal/config"
)

const testSecret = "s3cr3t-t0k3n"

func testCredential(host, scheme string) *Credential {
	return &Credential{Name: "company", Type: CredentialTypeGitHost, Host: host, Scheme: scheme, Username: "oauth2", Secret: testSecret}
}

// withUserinfo puts user information into the URL where USERINFO stands.
func withUserinfo(address, userinfo string) string {
	return strings.Replace(address, "USERINFO", userinfo, 1)
}

func TestCredentialRedact(t *testing.T) {
	c := testCredential("git.example.com", SchemeBasic)
	basic := base64.StdEncoding.EncodeToString([]byte("oauth2:" + testSecret))
	tests := []struct {
		name, in string
	}{
		{"secret in text", "fatal: token " + testSecret + " is wrong"},
		{"secret in a URL", "fatal: unable to access '" + withUserinfo("https://USERINFO@git.example.com/p.git/", "oauth2:"+testSecret) + "'"},
		{"user information", "fatal: unable to access 'https://alice:other@git.example.com/p.git/': 403"},
		{"user name only", "remote: https://alice@git.example.com/p.git"},
		{"basic header", "Authorization: Basic " + basic},
		{"escaped secret", "GET /p?token=" + url.QueryEscape("a b/"+testSecret)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := c.Redact(tt.in)
			for _, leak := range []string{testSecret, "alice", "other", basic} {
				if strings.Contains(got, leak) {
					t.Errorf("Redact(%q) = %q, still has %q", tt.in, got, leak)
				}
			}
			if !strings.Contains(got, "git.example.com") && strings.Contains(tt.in, "git.example.com") {
				t.Errorf("Redact removed the host: %q", got)
			}
		})
	}
	var none *Credential
	if got := none.Redact("https://u:p@h.example.com/"); strings.Contains(got, "u:p") {
		t.Errorf("without credential, user information stays: %q", got)
	}
	if s := c.String() + " " + c.GoString(); strings.Contains(s, testSecret) {
		t.Errorf("formatting leaks the secret: %s", s)
	}
}

func TestCredentialCheckHost(t *testing.T) {
	c := testCredential("repo.example.com", SchemeBearer)
	withPort := testCredential("git.example.com", SchemeBearer)
	withPort.Port = "8443"
	tests := []struct {
		cred *Credential
		url  string
		want string // part of the error; "" for allowed
	}{
		{c, "https://repo.example.com/o/r.git", ""},
		{c, "https://Repo.example.com/o/r.git", ""},
		{c, "https://api.repo.example.com/repos/o/r/tarball", ""},
		{c, "https://evilrepo.example.com/o/r.git", "is for repo.example.com, not for evilrepo.example.com"},
		{c, "https://repo.example.com.evil.example/o/r.git", "not for repo.example.com.evil.example"},
		{c, "https://example.com/repo.example.com", "not for example.com"},
		{c, "http://repo.example.com/o/r.git", "only sent over https"},
		{withPort, "https://git.example.com:8443/p.git", ""},
		{withPort, "https://git.example.com/p.git", "port 8443, not for 443"},
		{withPort, "https://git.example.com:9000/p.git", "port 8443, not for 9000"},
		{c, "https:///nohost", "no valid address"},
	}
	for _, tt := range tests {
		u, err := url.Parse(tt.url)
		if err != nil {
			t.Fatal(err)
		}
		err = tt.cred.CheckHost(u)
		switch {
		case tt.want == "" && err != nil:
			t.Errorf("%s: %v", tt.url, err)
		case tt.want != "" && (err == nil || !strings.Contains(err.Error(), tt.want)):
			t.Errorf("%s: error = %v, want %q", tt.url, err, tt.want)
		case err != nil && strings.Contains(err.Error(), testSecret):
			t.Errorf("%s: the error has the secret: %v", tt.url, err)
		}
	}
}

func TestCredentialSetAuth(t *testing.T) {
	basic := "Basic " + base64.StdEncoding.EncodeToString([]byte("oauth2:"+testSecret))
	tests := []struct {
		scheme, header, value string
	}{
		{SchemeBearer, "Authorization", "Bearer " + testSecret},
		{"", "Authorization", "Bearer " + testSecret},
		{SchemeToken, "Authorization", "token " + testSecret},
		{SchemePrivateToken, "Private-Token", testSecret},
		{SchemeBasic, "Authorization", basic},
	}
	for _, tt := range tests {
		h := http.Header{}
		if err := testCredential("h.example.com", tt.scheme).SetAuth(h); err != nil {
			t.Fatal(err)
		}
		if got := h.Get(tt.header); got != tt.value {
			t.Errorf("%q: %s = %q, want %q", tt.scheme, tt.header, got, tt.value)
		}
	}
	noUser := testCredential("h.example.com", SchemeBasic)
	noUser.Username = ""
	if err := noUser.SetAuth(http.Header{}); err == nil || strings.Contains(err.Error(), testSecret) {
		t.Errorf("basic without user name: %v", err)
	}
	bad := testCredential("h.example.com", SchemeBearer)
	bad.Secret = "a\r\nX-Injected: 1"
	if err := bad.SetAuth(http.Header{}); err == nil || strings.Contains(err.Error(), "Injected") {
		t.Errorf("a line break in the secret must be refused without echoing it: %v", err)
	}
}

func TestRunGitCredential(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "answer")
	if err := os.WriteFile(file, []byte("username=oauth2\npassword="+testSecret+"\nextra=ignored\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	request := func(lines ...string) *strings.Reader { return strings.NewReader(strings.Join(lines, "\n") + "\n\n") }
	tests := []struct {
		name string
		op   string
		in   *strings.Reader
		host string
		file string
		want string
		fail bool
	}{
		{
			"get for the host", "get", request("protocol=https", "host=git.example.com", "path=p.git"), "git.example.com", file,
			"username=oauth2\npassword=" + testSecret + "\n", false,
		},
		{
			"host with a port", "get", request("protocol=https", "host=git.example.com:8443"), "git.example.com:8443", file,
			"username=oauth2\npassword=" + testSecret + "\n", false,
		},
		{
			"host in other case", "get", request("protocol=https", "host=Git.Example.com"), "git.example.com", file,
			"username=oauth2\npassword=" + testSecret + "\n", false,
		},
		{"another host", "get", request("protocol=https", "host=evil.example.com"), "git.example.com", file, "", false},
		{"suffix of the host", "get", request("protocol=https", "host=evilgit.example.com"), "git.example.com", file, "", false},
		{"the port is part of the host", "get", request("protocol=https", "host=git.example.com:9000"), "git.example.com", file, "", false},
		{"http", "get", request("protocol=http", "host=git.example.com"), "git.example.com", file, "", false},
		{"no host in the request", "get", request("protocol=https"), "git.example.com", file, "", false},
		{"store", "store", request("protocol=https", "host=git.example.com", "username=a", "password=b"), "git.example.com", file, "", false},
		{"erase", "erase", request("protocol=https", "host=git.example.com"), "git.example.com", file, "", false},
		{"no file", "get", request("protocol=https", "host=git.example.com"), "git.example.com", "", "", true},
		{"missing file", "get", request("protocol=https", "host=git.example.com"), "git.example.com", filepath.Join(dir, "gone"), "", true},
		{"no host expected", "get", request("protocol=https", "host=git.example.com"), "", file, "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out bytes.Buffer
			err := RunGitCredential(tt.in, &out, tt.op, tt.host, tt.file)
			if (err != nil) != tt.fail {
				t.Fatalf("error = %v, want failure %v", err, tt.fail)
			}
			if err != nil && strings.Contains(err.Error(), testSecret) {
				t.Errorf("the error has the secret: %v", err)
			}
			if out.String() != tt.want {
				t.Errorf("answer = %q, want %q", out.String(), tt.want)
			}
		})
	}
}

// credentialManager is a manager whose credential lookup answers with c.
func credentialManager(t *testing.T, c *Credential) *Manager {
	t.Helper()
	m := testManager(t)
	m.Credential = func(_ context.Context, name string) (*Credential, error) {
		if name != c.Name {
			return nil, &config.Error{Code: config.CodeNotFound, Key: name, Detail: "credential " + name + " does not exist"}
		}
		return c, nil
	}
	m.Helper = func() (string, error) { return "/opt/dev box/devbox", nil }
	return m
}

func TestNewGitCredential(t *testing.T) {
	runtime := t.TempDir()
	c := testCredential("git.example.com", SchemeBearer)
	m := credentialManager(t, c)
	base := m.Getenv
	m.Getenv = func(k string) string {
		if k == "XDG_RUNTIME_DIR" {
			return runtime
		}
		return base(k)
	}
	g, err := m.newGitCredential(c, "https://git.example.com:8443/team/plugins.git")
	if err != nil {
		t.Fatal(err)
	}
	// the helper command resets the helpers of the user first and holds no secret
	want := []string{"-c", "credential.helper=", "-c", "credential.helper=!'/opt/dev box/devbox' repo git-credential --host 'git.example.com:8443'"}
	if strings.Join(g.args, "\x00") != strings.Join(want, "\x00") {
		t.Errorf("args = %q, want %q", g.args, want)
	}
	for _, s := range append(append([]string{}, g.args...), g.env...) {
		if strings.Contains(s, testSecret) {
			t.Errorf("the secret is in an argument or the environment: %q", s)
		}
	}
	if len(g.env) != 1 || !strings.HasPrefix(g.env[0], GitCredentialEnv+"=") {
		t.Fatalf("env = %q", g.env)
	}
	file := strings.TrimPrefix(g.env[0], GitCredentialEnv+"=")
	if filepath.Dir(filepath.Dir(file)) != runtime {
		t.Errorf("the file %s is not in a directory below the runtime directory %s", file, runtime)
	}
	info, err := os.Stat(file)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("file: %v, %v", info, err)
	}
	if dir, err := os.Stat(filepath.Dir(file)); err != nil || dir.Mode().Perm() != 0o700 {
		t.Errorf("directory: %v, %v", dir, err)
	}
	if data, _ := os.ReadFile(file); string(data) != "username=oauth2\npassword="+testSecret+"\n" { //nolint:gosec // the file of this test
		t.Errorf("file = %q", data)
	}
	g.cleanup()
	if _, err := os.Stat(filepath.Dir(file)); err == nil { //nolint:gosec // the private directory of this test
		t.Error("the directory must be gone after the cleanup")
	}

	// refused before anything is written
	for name, tt := range map[string]struct {
		cred    *Credential
		address string
		want    string
	}{
		"other host":   {c, "https://evil.example.com/p.git", "is for git.example.com, not for evil.example.com"},
		"plain http":   {c, "http://git.example.com/p.git", "only sent over https"},
		"no user name": {&Credential{Name: "company", Host: "git.example.com", Secret: testSecret}, "https://git.example.com/p.git", "needs a user name"},
		"line break":   {&Credential{Name: "company", Host: "git.example.com", Username: "a\nb", Secret: testSecret}, "https://git.example.com/p.git", "line break"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := m.newGitCredential(tt.cred, tt.address)
			if err == nil || !strings.Contains(err.Error(), tt.want) || strings.Contains(err.Error(), testSecret) {
				t.Errorf("error = %v, want %q", err, tt.want)
			}
		})
	}
	m.Helper = nil
	if _, err := m.newGitCredential(c, "https://git.example.com/p.git"); !config.IsCode(err, config.CodePrerequisite) {
		t.Errorf("without helper: %v", err)
	}
}

// fakeServer records the requests that carry credentials.
type fakeServer struct {
	mu      sync.Mutex
	headers []http.Header
}

func (f *fakeServer) record(r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.headers = append(f.headers, r.Header.Clone())
}

func (f *fakeServer) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.headers)
}

func (f *fakeServer) last() (http.Header, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.headers) == 0 {
		return nil, false
	}
	return f.headers[len(f.headers)-1], true
}

func tarballBody(t *testing.T) []byte {
	t.Helper()
	body, err := os.ReadFile(writeTarGz(t, []tarEntry{{name: "repo-main/plugins/alpha/plugin.json", body: manifest("alpha", "1.0.0", "")}}))
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func TestSyncHTTPSWithCredential(t *testing.T) {
	ctx := context.Background()
	body := tarballBody(t)
	var recorded fakeServer
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		recorded.record(r)
		if r.Header.Get("Authorization") != "Bearer "+testSecret {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	host := "127.0.0.1"

	c := testCredential(host, SchemeBearer)
	m := credentialManager(t, c)
	m.Client = srv.Client()
	e := entryFor(t, "team", map[string]any{"kind": "plugins", "source": srv.URL + "/repo.tar.gz", "path": "plugins", "credential": "company"})
	if err := m.Sync(ctx, e); err != nil {
		t.Fatal(err)
	}
	if v := m.View(e); v.State != StatePending || v.Credential != "company" {
		t.Fatalf("view = %+v", v)
	}
	if recorded.count() != 1 {
		t.Errorf("requests = %d", recorded.count())
	}

	// an unknown instance is an error in the state, without a request
	missing := entryFor(t, "missing", map[string]any{"kind": "plugins", "source": srv.URL + "/repo.tar.gz", "credential": "no-such"})
	before := recorded.count()
	if err := m.Sync(ctx, missing); !config.IsCode(err, config.CodeNotFound) {
		t.Errorf("unknown credential: %v", err)
	}
	if v := m.View(missing); v.State != StateError || !strings.Contains(v.LastError, "no-such") {
		t.Errorf("view = %+v", v)
	}
	if recorded.count() != before {
		t.Error("a request was sent without a credential")
	}

	// without a credential lookup, repositories cannot use credentials
	plain := testManager(t)
	plain.Client = srv.Client()
	if err := plain.Sync(ctx, e); !config.IsCode(err, config.CodePrerequisite) {
		t.Errorf("without lookup: %v", err)
	}
}

func TestSyncHTTPSCredentialOnlyToItsHost(t *testing.T) {
	ctx := context.Background()
	var recorded fakeServer
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		recorded.record(r)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	// the credential is for another host: nothing is sent
	c := testCredential("git.example.com", SchemeBearer)
	m := credentialManager(t, c)
	m.Client = srv.Client()
	e := entryFor(t, "team", map[string]any{"kind": "plugins", "source": srv.URL + "/repo.tar.gz", "credential": "company"})
	err := m.Sync(ctx, e)
	if err == nil || !strings.Contains(err.Error(), "is for git.example.com, not for 127.0.0.1") || strings.Contains(err.Error(), testSecret) {
		t.Errorf("error = %v", err)
	}
	if recorded.count() != 0 {
		t.Errorf("%d request(s) reached the server", recorded.count())
	}
	if v := m.View(e); strings.Contains(v.LastError, testSecret) {
		t.Errorf("lastError = %q", v.LastError)
	}
}

func TestSyncHTTPSRedirectDropsCredential(t *testing.T) {
	ctx := context.Background()
	body := tarballBody(t)
	var elsewhere fakeServer
	other := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		elsewhere.record(r)
		_, _ = w.Write(body)
	}))
	t.Cleanup(other.Close)
	// same machine, other port: another host:port for the redirect policy (Go itself
	// compares host names only, and never strips PRIVATE-TOKEN)
	origin := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL+"/file.tar.gz", http.StatusFound)
	}))
	t.Cleanup(origin.Close)

	for _, scheme := range []string{SchemeBearer, SchemePrivateToken} {
		c := testCredential("127.0.0.1", scheme)
		m := credentialManager(t, c)
		m.Client = origin.Client() // both servers use the same test certificate
		e := entryFor(t, "team-"+scheme, map[string]any{"kind": "plugins", "source": origin.URL + "/repo.tar.gz", "path": "plugins", "credential": "company"})
		if err := m.Sync(ctx, e); err != nil {
			t.Fatalf("%s: %v", scheme, err)
		}
		last, ok := elsewhere.last()
		if !ok {
			t.Fatalf("%s: the redirect was not followed", scheme)
		}
		if last.Get("Authorization") != "" || last.Get("Private-Token") != "" {
			t.Errorf("%s: the secret followed the redirect to another host: %v", scheme, last)
		}
	}
}

func TestSyncHTTPSErrorsDoNotLeakTheSecret(t *testing.T) {
	ctx := context.Background()
	c := testCredential("127.0.0.1", SchemeBearer)
	m := credentialManager(t, c)
	m.Client = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return nil, errors.New("dial " + r.URL.String() + " with token " + testSecret + " via " + withUserinfo("https://USERINFO@proxy.example.com/", "alice:"+testSecret))
	})}
	e := entryFor(t, "team", map[string]any{"kind": "plugins", "source": "https://127.0.0.1:1/repo.tar.gz", "credential": "company"})
	err := m.Sync(ctx, e)
	if !config.IsCode(err, config.CodePrerequisite) {
		t.Fatalf("error = %v", err)
	}
	v := m.View(e)
	for _, text := range []string{err.Error(), v.LastError} {
		if strings.Contains(text, testSecret) || strings.Contains(text, "alice") {
			t.Errorf("leak: %q", text)
		}
	}
	if raw, rerr := os.ReadFile(filepath.Join(m.Store.Dir("team"), "state.json")); rerr == nil && bytes.Contains(raw, []byte(testSecret)) {
		t.Errorf("state.json has the secret: %s", raw)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// fakeGit stands in for Git: it records its arguments, the environment and the
// credential file while it runs, and clones a plugin.
const fakeGit = `#!/bin/sh
log="$FAKE_GIT_LOG"
{
  echo "ARGS: $*"
  env | sort | sed 's/^/ENV: /'
  if [ -n "$DEVBOX_GIT_CREDENTIAL_FILE" ]; then
    echo "FILE: $DEVBOX_GIT_CREDENTIAL_FILE"
    echo "MODE: $(stat -c %a "$DEVBOX_GIT_CREDENTIAL_FILE") $(stat -c %a "$(dirname "$DEVBOX_GIT_CREDENTIAL_FILE")")"
    sed 's/^/CONTENT: /' "$DEVBOX_GIT_CREDENTIAL_FILE"
  fi
} >> "$log"
case "$*" in
*" clone "*)
  for last; do :; done
  mkdir -p "$last/plugins/alpha"
  printf '%s' '{"id":"alpha","version":"1.0.0","name":"alpha","category":"tool"}' > "$last/plugins/alpha/plugin.json"
  ;;
*"rev-parse HEAD"*) echo 0123456789abcdef0123456789abcdef01234567 ;;
*)
  cred="oauth2:$(sed -n 's/^password=//p' "$DEVBOX_GIT_CREDENTIAL_FILE")"
  echo "fatal: unable to access 'https://$cred@git.example.com/p.git/': nope" >&2
  exit 128
  ;;
esac
`

func TestSyncGitWithCredential(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	script := filepath.Join(dir, "git")
	logFile := filepath.Join(dir, "log")
	if err := os.WriteFile(script, []byte(strings.ReplaceAll(fakeGit, "$FAKE_GIT_LOG", logFile)), 0o700); err != nil { //nolint:gosec // a script for this test
		t.Fatal(err)
	}
	c := testCredential("git.example.com", SchemeBearer)
	m := credentialManager(t, c)
	m.LookPath = func(string) (string, error) { return script, nil }

	e := entryFor(t, "team", map[string]any{"kind": "plugins", "source": "git+https://git.example.com/team/p.git", "path": "plugins", "credential": "company"})
	if err := m.Sync(ctx, e); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(logFile) //nolint:gosec // the log of this test
	if err != nil {
		t.Fatal(err)
	}
	log := string(raw)
	if strings.Contains(strings.ReplaceAll(log, "CONTENT: password="+testSecret, ""), testSecret) {
		t.Errorf("the secret is in an argument or the environment of Git:\n%s", log)
	}
	for _, want := range []string{
		"ARGS: -c credential.helper= -c credential.helper=!'/opt/dev box/devbox' repo git-credential --host 'git.example.com' clone ",
		"ENV: " + GitCredentialEnv + "=",
		"ENV: GIT_TERMINAL_PROMPT=0",
		"MODE: 600 700",
		"CONTENT: username=oauth2",
	} {
		if !strings.Contains(log, want) {
			t.Errorf("log lacks %q:\n%s", want, log)
		}
	}
	var file string
	for _, line := range strings.Split(log, "\n") {
		if f, ok := strings.CutPrefix(line, "FILE: "); ok {
			file = f
		}
	}
	if file == "" {
		t.Fatal("Git saw no credential file")
	}
	if _, err := os.Stat(filepath.Dir(file)); err == nil { //nolint:gosec // the private directory of this test
		t.Errorf("the private directory %s is still there after the sync", filepath.Dir(file))
	}

	// a failure of Git: its message echoes the address with user information
	failing := entryFor(t, "failing", map[string]any{"kind": "plugins", "source": "git+https://git.example.com/team/p.git", "ref": strings.Repeat("a", 40), "credential": "company"})
	err = m.Sync(ctx, failing)
	if !config.IsCode(err, config.CodePrerequisite) {
		t.Fatalf("error = %v", err)
	}
	v := m.View(failing)
	for _, text := range []string{err.Error(), v.LastError} {
		if strings.Contains(text, testSecret) || strings.Contains(text, "oauth2:") {
			t.Errorf("leak: %q", text)
		}
		if !strings.Contains(text, "https://git.example.com/p.git/") {
			t.Errorf("the message lost the address: %q", text)
		}
	}
}

func TestCommandName(t *testing.T) {
	for args, want := range map[string]string{
		"clone --quiet": "clone",
		"-c advice.detachedHead=false checkout x": "checkout",
		"-c a=b -c c=d fetch":                     "fetch",
		"":                                        "",
	} {
		if got := commandName(strings.Fields(args)); got != want {
			t.Errorf("commandName(%q) = %q, want %q", args, got, want)
		}
	}
}
