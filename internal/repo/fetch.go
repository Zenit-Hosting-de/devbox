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
func (m *Manager) fetchInto(ctx context.Context, e Entry, dest string) (string, error) {
	kind, addr, err := splitSource(e.source)
	if err != nil {
		return "", invalidf("%v", err)
	}
	switch kind {
	case sourceGit:
		return m.fetchGit(ctx, e, addr, dest)
	case sourceHTTPS:
		return m.fetchHTTPS(ctx, e, addr, dest)
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
func (m *Manager) fetchHTTPS(ctx context.Context, e Entry, address, dest string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return "", invalidf("source %q: %v", e.Source, err)
	}
	resp, err := m.httpClient().Do(req)
	if err != nil {
		return "", prerequisite("download %s: %v", e.Name, err)
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
		return "", prerequisite("download %s: %v", e.Name, err)
	}
	if n > MaxDownloadBytes {
		return "", invalidf("the download is larger than %d MiB", MaxDownloadBytes>>20)
	}
	if err := ExtractTarGz(tmp.Name(), dest); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// gitEnv is the environment of Git: no questions on the terminal, SSH without
// prompts, and the variables of gitEnvPassThrough.
func (m *Manager) gitEnv() []string {
	env := []string{"GIT_TERMINAL_PROMPT=0", "GIT_SSH_COMMAND=ssh -o BatchMode=yes", "LC_ALL=C"}
	for _, k := range gitEnvPassThrough {
		if v := m.Getenv(k); v != "" {
			env = append(env, k+"="+v)
		}
	}
	return env
}

func (m *Manager) runGit(ctx context.Context, bin, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, bin, args...) //nolint:gosec // git from the PATH of the call, arguments are built here
	cmd.Dir = dir
	cmd.Env = m.gitEnv()
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return "", fmt.Errorf("git %s: %w", args[0], ctx.Err())
		}
		msg := strings.TrimSpace(stderr.String())
		if len(msg) > maxCommandOutput {
			msg = msg[len(msg)-maxCommandOutput:]
		}
		return "", prerequisite("git %s failed: %s", args[0], firstNonEmpty(msg, err.Error()))
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
func (m *Manager) fetchGit(ctx context.Context, e Entry, address, dest string) (string, error) {
	bin, err := m.LookPath("git")
	if err != nil {
		return "", prerequisite("git is needed for git sources and is not installed (or not in PATH)")
	}
	if fullCommit.MatchString(e.Ref) {
		if err := m.checkoutCommit(ctx, bin, address, e.Ref, dest); err != nil {
			return "", err
		}
	} else {
		args := []string{"clone", "--quiet", "--depth", "1", "--no-recurse-submodules"}
		if e.Ref != "" {
			args = append(args, "--branch", e.Ref)
		}
		args = append(args, "--", address, dest)
		if _, err := m.runGit(ctx, bin, "", args...); err != nil {
			return "", err
		}
	}
	rev, err := m.runGit(ctx, bin, dest, "rev-parse", "HEAD")
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
func (m *Manager) checkoutCommit(ctx context.Context, bin, address, commit, dest string) error {
	if _, err := m.runGit(ctx, bin, dest, "init", "--quiet"); err != nil {
		return err
	}
	if _, err := m.runGit(ctx, bin, dest, "remote", "add", "origin", address); err != nil {
		return err
	}
	_, err := m.runGit(ctx, bin, dest, "fetch", "--quiet", "--depth", "1", "--no-tags", "--no-recurse-submodules", "origin", commit)
	if err != nil {
		if ctx.Err() != nil {
			return err
		}
		if _, err2 := m.runGit(ctx, bin, dest, "fetch", "--quiet", "--no-tags", "--no-recurse-submodules", "origin"); err2 != nil {
			return errors.Join(err, err2)
		}
	}
	_, err = m.runGit(ctx, bin, dest, "-c", "advice.detachedHead=false", "checkout", "--quiet", "--detach", commit)
	return err
}
