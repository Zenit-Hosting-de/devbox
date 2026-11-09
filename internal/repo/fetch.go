package repo

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/Zenit-Hosting-de/devbox/internal/config"
)

// gitEnvPassThrough lists the variables of the call that Git and SSH get: where the
// home directory, the SSH agent, the proxy and the certificate bundle are.
var gitEnvPassThrough = []string{
	"HOME", "PATH", "USER", "LOGNAME", "LANG", "TMPDIR",
	"XDG_CONFIG_HOME", "SSH_AUTH_SOCK", "SSH_AGENT_PID",
	"HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY", "ALL_PROXY",
	"http_proxy", "https_proxy", "no_proxy", "all_proxy",
	"SSL_CERT_FILE", "SSL_CERT_DIR", "GIT_SSL_CAINFO", "GIT_SSL_CAPATH", "CURL_CA_BUNDLE",
}

// maxCommandOutput bounds the text of a failed command that goes into an error.
const maxCommandOutput = 2000

func prerequisite(format string, a ...any) error {
	return &config.Error{Code: config.CodePrerequisite, Detail: fmt.Sprintf(format, a...)}
}

// fetchInto fetches the source of the entry into the empty directory dest and returns
// the revision.
func (m *Manager) fetchInto(ctx context.Context, e Entry, cred *Credential, dest string) (string, error) {
	kind, addr, err := splitSource(e.source)
	if err != nil {
		return "", invalidf("%v", err)
	}
	switch kind {
	case sourceGit:
		return m.fetchGit(ctx, e, cred, addr, dest)
	case sourceHTTPS:
		return m.fetchHTTPS(ctx, e, cred, addr, dest)
	default:
		return m.fetchFile(addr, dest)
	}
}

func (m *Manager) fetchFile(dir, dest string) (string, error) {
	digest, err := CopyTree(dir, dest)
	if err != nil {
		return "", err
	}
	return strings.TrimPrefix(digest, "sha256:"), nil
}

func (m *Manager) httpClient() *http.Client {
	if m.Client != nil {
		return m.Client
	}
	return &http.Client{Transport: &http.Transport{Proxy: http.ProxyFromEnvironment}}
}

// fetchHTTPS downloads the tarball (at most MaxDownloadBytes), uses the SHA-256 of
// the file as revision and extracts it into dest.
func (m *Manager) fetchHTTPS(ctx context.Context, e Entry, cred *Credential, address, dest string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return "", invalidf("source %q: %s", e.Source, cred.Redact(err.Error()))
	}
	client := m.httpClient()
	if cred != nil {
		if err := cred.CheckHost(req.URL); err != nil {
			return "", err
		}
		if err := cred.SetAuth(req.Header); err != nil {
			return "", err
		}
		client = guardRedirects(client, req.URL)
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", prerequisite("download %s: %s", e.Name, cred.Redact(err.Error()))
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", prerequisite("download %s: %s", e.Name, resp.Status)
	}
	if resp.ContentLength > MaxDownloadBytes {
		return "", invalidf("the download is larger than %d MiB", MaxDownloadBytes>>20)
	}
	tmp, err := os.CreateTemp(m.Store.Dir(e.Name), ".download-*")
	if err != nil {
		return "", err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(tmp, h), io.LimitReader(resp.Body, MaxDownloadBytes+1))
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return "", prerequisite("download %s: %s", e.Name, cred.Redact(err.Error()))
	}
	if n > MaxDownloadBytes {
		return "", invalidf("the download is larger than %d MiB", MaxDownloadBytes>>20)
	}
	if err := ExtractTarGz(tmp.Name(), dest); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// gitCall is one Git installation together with what a call needs for a credential.
type gitCall struct {
	bin string
	// cred is redacted from the output of Git; nil without credential.
	cred *Credential
	// before are arguments in front of the command (the credential helper), env is
	// added to the environment.
	before, env []string
}

// gitEnv is the environment of Git: no questions on the terminal, SSH without
// prompts, and the variables of gitEnvPassThrough.
func (m *Manager) gitEnv(extra []string) []string {
	env := []string{"GIT_TERMINAL_PROMPT=0", "GIT_SSH_COMMAND=ssh -o BatchMode=yes", "LC_ALL=C"}
	for _, k := range gitEnvPassThrough {
		if v := m.Getenv(k); v != "" {
			env = append(env, k+"="+v)
		}
	}
	return append(env, extra...)
}

// commandName is the Git command of the arguments, without leading -c options.
func commandName(args []string) string {
	for i := 0; i < len(args); i++ {
		if args[i] == "-c" {
			i++
			continue
		}
		return args[i]
	}
	return ""
}

func (m *Manager) runGit(ctx context.Context, g *gitCall, dir string, args ...string) (string, error) {
	name := commandName(args)
	full := append(append([]string{}, g.before...), args...)
	cmd := exec.CommandContext(ctx, g.bin, full...) //nolint:gosec // git from the PATH of the call, arguments are built here
	cmd.Dir = dir
	cmd.Env = m.gitEnv(g.env)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return "", fmt.Errorf("git %s: %w", name, ctx.Err())
		}
		// Git echoes addresses (with user information) and may echo more
		msg := strings.TrimSpace(g.cred.Redact(stderr.String()))
		if len(msg) > maxCommandOutput {
			msg = msg[len(msg)-maxCommandOutput:]
		}
		return "", prerequisite("git %s failed: %s", name, firstNonEmpty(msg, g.cred.Redact(err.Error())))
	}
	return strings.TrimSpace(stdout.String()), nil
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// fetchGit makes a shallow copy of the ref (default: the HEAD of the remote) into
// dest, without submodules and without the .git directory, and returns the commit.
// A full commit SHA as ref is fetched directly.
func (m *Manager) fetchGit(ctx context.Context, e Entry, cred *Credential, address, dest string) (string, error) {
	bin, err := m.LookPath("git")
	if err != nil {
		return "", prerequisite("git is needed for git sources and is not installed (or not in PATH)")
	}
	g := &gitCall{bin: bin, cred: cred}
	if cred != nil {
		setup, err := m.newGitCredential(cred, address)
		if err != nil {
			return "", err
		}
		defer setup.cleanup()
		g.before, g.env = setup.args, setup.env
	}
	if fullCommit.MatchString(e.Ref) {
		if err := m.checkoutCommit(ctx, g, address, e.Ref, dest); err != nil {
			return "", err
		}
	} else {
		args := []string{"clone", "--quiet", "--depth", "1", "--no-recurse-submodules"}
		if e.Ref != "" {
			args = append(args, "--branch", e.Ref)
		}
		args = append(args, "--", address, dest)
		if _, err := m.runGit(ctx, g, "", args...); err != nil {
			return "", err
		}
	}
	rev, err := m.runGit(ctx, g, dest, "rev-parse", "HEAD")
	if err != nil {
		return "", err
	}
	if e.Trust == TrustPinned && !strings.EqualFold(rev, e.Ref) {
		return "", invalidf("the checkout is %s, not the pinned commit %s", rev, e.Ref)
	}
	if err := os.RemoveAll(filepath.Join(dest, ".git")); err != nil {
		return "", err
	}
	return strings.ToLower(rev), nil
}

// checkoutCommit fetches one commit: shallow first, and the whole history when the
// server does not hand out single commits.
func (m *Manager) checkoutCommit(ctx context.Context, g *gitCall, address, commit, dest string) error {
	if _, err := m.runGit(ctx, g, dest, "init", "--quiet"); err != nil {
		return err
	}
	if _, err := m.runGit(ctx, g, dest, "remote", "add", "origin", address); err != nil {
		return err
	}
	_, err := m.runGit(ctx, g, dest, "fetch", "--quiet", "--depth", "1", "--no-tags", "--no-recurse-submodules", "origin", commit)
	if err != nil {
		if ctx.Err() != nil {
			return err
		}
		if _, err2 := m.runGit(ctx, g, dest, "fetch", "--quiet", "--no-tags", "--no-recurse-submodules", "origin"); err2 != nil {
			return errors.Join(err, err2)
		}
	}
	_, err = m.runGit(ctx, g, dest, "-c", "advice.detachedHead=false", "checkout", "--quiet", "--detach", commit)
	return err
}

// FetchGit makes a shallow copy of a Git source (git+https://, git+ssh:// or
// git+file://) at ref into the empty directory dest, without the .git directory, and
// returns the commit. It serves sources that are not repositories, such as profiles;
// name only appears in errors.
func (m *Manager) FetchGit(ctx context.Context, name, source, ref, dest string) (string, error) {
	kind, addr, err := splitSource(source)
	if err != nil {
		return "", invalidf("%v", err)
	}
	if kind != sourceGit {
		return "", invalidf("source %q is not a Git source", source)
	}
	e := Entry{Name: name, Source: source, source: source, Ref: ref, Trust: TrustPrompt}
	return m.fetchGit(ctx, e, nil, addr, dest)
}
