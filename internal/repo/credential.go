package repo

import (
	"bufio"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Credential types a repository may name in credential.
const (
	CredentialTypeGitHost  = "git-host"
	CredentialTypeRegistry = "registry"
)

// AllowedCredentialType reports whether an instance of the type may be used for a
// repository.
func AllowedCredentialType(typ string) bool {
	return typ == CredentialTypeGitHost || typ == CredentialTypeRegistry
}

// HTTP authentication schemes of a credential type (schema/plugin.v1.json,
// credentialType.httpAuth).
const (
	SchemeBearer       = "bearer"
	SchemeBasic        = "basic"
	SchemeToken        = "token"
	SchemePrivateToken = "private-token"
)

// GitCredentialEnv names the environment variable that carries the path of the file
// with the answer of the Git credential helper to that helper. The variable holds
// the path only, never the secret.
const GitCredentialEnv = "DEVBOX_GIT_CREDENTIAL_FILE" //nolint:gosec // the name of a variable, not a credential

// Credential is a credential instance resolved for one sync: where it is valid, how
// it authenticates and the secret. It lives only in memory for the duration of the
// sync and goes only to Git or the download, never to plugins, state, logs or events.
type Credential struct {
	// Name is the name of the instance.
	Name string
	// Type is the credential type.
	Type string
	// Host is the host name of the address of the instance, lower case; the secret goes
	// to this host and its subdomains only.
	Host string
	// Port is the port of the address of the instance; empty if it names none, which
	// allows every port of the host.
	Port string
	// Scheme is the HTTP authentication scheme for downloads.
	Scheme string
	// Username is the user name for basic authentication and for Git.
	Username string
	// Secret is the token or password.
	Secret string
}

// String keeps the secret out of formatted output and logs.
func (c *Credential) String() string { return "credential " + c.Name }

// GoString is String, so %#v does not print the secret.
func (c *Credential) GoString() string { return c.String() }

// Redact removes the secret and the user information of URLs from text that comes
// from Git or the network, so that errors, state and logs do not carry them.
func (c *Credential) Redact(s string) string {
	if c != nil && c.Secret != "" {
		s = strings.ReplaceAll(s, c.Secret, "***")
		s = strings.ReplaceAll(s, url.QueryEscape(c.Secret), "***")
		s = strings.ReplaceAll(s, url.PathEscape(c.Secret), "***")
		s = strings.ReplaceAll(s, base64.StdEncoding.EncodeToString([]byte(c.Username+":"+c.Secret)), "***")
	}
	return stripUserinfo(s)
}

var userinfoPattern = regexp.MustCompile(`(?i)\b([a-z][a-z0-9+.-]*://)[^/\s@]*@`)

// stripUserinfo removes the user name and password from every URL in the text.
func stripUserinfo(s string) string {
	return userinfoPattern.ReplaceAllString(s, "${1}")
}

// CheckHost reports whether the secret may go to the URL: https only, to the host of
// the instance or one of its subdomains (api.github.com for github.com), and to the
// port of the instance if its address names one.
func (c *Credential) CheckHost(u *url.URL) error {
	host := strings.ToLower(u.Hostname())
	switch {
	case u.Scheme != "https":
		return invalidf("credential %s is only sent over https", c.Name)
	case host == "" || c.Host == "":
		return invalidf("credential %s has no valid address", c.Name)
	case host != c.Host && !strings.HasSuffix(host, "."+c.Host):
		return invalidf("credential %s is for %s, not for %s", c.Name, c.Host, host)
	case c.Port != "" && effectivePort(u) != c.Port:
		return invalidf("credential %s is for port %s, not for %s", c.Name, c.Port, effectivePort(u))
	}
	return nil
}

func effectivePort(u *url.URL) string {
	if p := u.Port(); p != "" {
		return p
	}
	return "443"
}

// SetAuth adds the secret to the request headers as the scheme of the instance wants
// it.
func (c *Credential) SetAuth(h http.Header) error {
	if strings.ContainsAny(c.Secret+c.Username, "\r\n\x00") {
		return invalidf("credential %s contains a line break", c.Name)
	}
	switch c.Scheme {
	case SchemeBasic:
		if c.Username == "" {
			return invalidf("credential %s needs a user name (field username) for basic authentication", c.Name)
		}
		h.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(c.Username+":"+c.Secret)))
	case SchemeToken:
		h.Set("Authorization", "token "+c.Secret)
	case SchemePrivateToken:
		h.Set("PRIVATE-TOKEN", c.Secret)
	default:
		h.Set("Authorization", "Bearer "+c.Secret)
	}
	return nil
}

// stripAuth removes the headers SetAuth adds.
func stripAuth(h http.Header) {
	h.Del("Authorization")
	h.Del("PRIVATE-TOKEN")
}

// gitAnswer is the answer of the credential helper: the lines of the Git credential
// protocol (git-credential(1)).
func (c *Credential) gitAnswer() (string, error) {
	switch {
	case c.Username == "":
		return "", invalidf("credential %s needs a user name (field username) for Git", c.Name)
	case strings.ContainsAny(c.Secret+c.Username, "\r\n\x00"):
		return "", invalidf("credential %s contains a line break", c.Name)
	}
	return "username=" + c.Username + "\npassword=" + c.Secret + "\n", nil
}

// guardRedirects returns a client that sends the secret to the host of the first
// request only: a redirect to another host or to http loses the headers (Go drops
// Authorization on its own, but not PRIVATE-TOKEN).
func guardRedirects(c *http.Client, origin *url.URL) *http.Client {
	guarded := *c
	next := c.CheckRedirect
	guarded.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if !strings.EqualFold(req.URL.Host, origin.Host) || req.URL.Scheme != "https" {
			stripAuth(req.Header)
		}
		if next != nil {
			return next(req, via)
		}
		if len(via) >= maxRedirects {
			return fmt.Errorf("stopped after %d redirects", maxRedirects)
		}
		return nil
	}
	return &guarded
}

// maxRedirects is the limit of the default redirect policy of net/http.
const maxRedirects = 10

// gitCredential is the Git side of a credential: the arguments and the environment
// that make Git ask the devbox binary for the secret.
type gitCredential struct {
	// args come before the Git command: they reset the helpers of the user and add ours.
	args []string
	// env is added to the environment of Git.
	env []string
	// cleanup removes the private directory with the secret.
	cleanup func()
}

// newGitCredential writes the answer of the helper into a 0600 file in a private
// directory (0700, below XDG_RUNTIME_DIR when the session has one) and returns the
// arguments and the environment variable for Git. The file name is passed in
// GitCredentialEnv, the host name as an argument of the helper; the secret is in
// neither the arguments nor the environment of Git. The helper command resets the
// helpers of the user, so none of them interferes.
func (m *Manager) newGitCredential(c *Credential, address string) (*gitCredential, error) {
	if m.Helper == nil {
		return nil, prerequisite("credentials for Git are not available here (no path of the devbox binary)")
	}
	u, err := url.Parse(address)
	if err != nil || u.Host == "" {
		return nil, invalidf("the address of the repository is not valid")
	}
	if err := c.CheckHost(u); err != nil {
		return nil, err
	}
	answer, err := c.gitAnswer()
	if err != nil {
		return nil, err
	}
	bin, err := m.Helper()
	if err != nil {
		return nil, prerequisite("the path of the devbox binary is unknown: %v", err)
	}
	dir, err := os.MkdirTemp(m.Getenv("XDG_RUNTIME_DIR"), "devbox-git-credential-")
	if err != nil { // no or unusable runtime directory: the temporary directory of the system
		if dir, err = os.MkdirTemp("", "devbox-git-credential-"); err != nil {
			return nil, prerequisite("private directory for the credential: %v", err)
		}
	}
	g := &gitCredential{cleanup: func() { _ = os.RemoveAll(dir) }}
	file := filepath.Join(dir, "answer")
	if err := os.WriteFile(file, []byte(answer), 0o600); err != nil {
		g.cleanup()
		return nil, prerequisite("write the credential: %v", err)
	}
	helper := "!" + shellQuote(bin) + " repo git-credential --host " + shellQuote(strings.ToLower(u.Host))
	g.args = []string{"-c", "credential.helper=", "-c", "credential.helper=" + helper}
	g.env = []string{GitCredentialEnv + "=" + file}
	return g, nil
}

// shellQuote quotes s for /bin/sh.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// RunGitCredential is the Git credential helper (git-credential(1)) behind the hidden
// command "devbox repo git-credential". For "get" it reads the request from in and,
// when the protocol is https and the host is host, answers with the username and
// password lines of file; for every other request, and for "store" and "erase", it
// answers nothing and succeeds. The secret is read from the file at the moment of the
// answer, so it is in no argument and no environment variable.
func RunGitCredential(in io.Reader, out io.Writer, op, host, file string) error {
	if op != "get" {
		return nil
	}
	req := map[string]string{}
	sc := bufio.NewScanner(in)
	for sc.Scan() {
		line := sc.Text()
		if line == "" {
			break
		}
		if k, v, ok := strings.Cut(line, "="); ok {
			req[k] = v
		}
	}
	if err := sc.Err(); err != nil {
		return err
	}
	if req["protocol"] != "https" || host == "" || !strings.EqualFold(req["host"], host) {
		return nil
	}
	if file == "" {
		return errors.New("git-credential: " + GitCredentialEnv + " is not set")
	}
	data, err := os.ReadFile(file) //nolint:gosec // the file of this sync, named by the caller
	if err != nil {
		return fmt.Errorf("git-credential: %w", err)
	}
	var answer strings.Builder
	for _, line := range strings.Split(string(data), "\n") {
		if k, _, ok := strings.Cut(line, "="); ok && (k == "username" || k == "password") {
			answer.WriteString(line + "\n")
		}
	}
	_, err = io.WriteString(out, answer.String())
	return err
}

// resolveCredential returns the credential instance of the entry, or nil if it names
// none.
func (m *Manager) resolveCredential(ctx context.Context, e Entry) (*Credential, error) {
	if e.Credential == "" {
		return nil, nil
	}
	if m.Credential == nil {
		return nil, prerequisite("credentials for repositories are not available here")
	}
	return m.Credential(ctx, e.Credential)
}
