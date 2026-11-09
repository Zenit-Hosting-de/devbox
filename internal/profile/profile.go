package profile

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/Zenit-Hosting-de/devbox/internal/config"
	"github.com/Zenit-Hosting-de/devbox/internal/schema"
	"github.com/Zenit-Hosting-de/devbox/internal/sigstore"
)

const (
	// FileName is the name of a cached profile.
	FileName = "profile.json"
	// BundleSuffix is appended to the profile file name to find its Sigstore bundle.
	BundleSuffix = ".sigstore.json"
	// MaxBytes bounds a downloaded profile and its bundle.
	MaxBytes = 1 << 20
	// FetchTimeout bounds one fetch: download or Git and the signature check.
	FetchTimeout = 2 * time.Minute
	// DefaultGitPath is the profile file inside a Git repository without path.
	DefaultGitPath = "profile.json"
)

// Origins of a source: the configuration file whose profiles key names it.
const (
	OriginSystem = "system"
	OriginUser   = "user"
)

// Kinds of a source.
const (
	KindFile  = "file"
	KindHTTPS = "https"
	KindGit   = "git"
)

// Trust is the Sigstore identity that must have signed a profile.
type Trust struct {
	Issuer   string `json:"issuer"`
	Identity string `json:"identity"`
}

// Source is one entry of the profiles key of a configuration file.
type Source struct {
	// Source is the address as written, with placeholders.
	Source string `json:"source"`
	Ref    string `json:"ref,omitempty"`
	Path   string `json:"path,omitempty"`
	Trust  *Trust `json:"trust,omitempty"`
	// Origin is OriginSystem or OriginUser.
	Origin string `json:"origin"`
}

// SourcesOf returns the profile sources of a configuration document.
func SourcesOf(doc map[string]any, origin string) []Source {
	entries, _ := doc["profiles"].([]any)
	out := make([]Source, 0, len(entries))
	for _, raw := range entries {
		m, _ := raw.(map[string]any)
		s := Source{Origin: origin}
		s.Source, _ = m["source"].(string)
		s.Ref, _ = m["ref"].(string)
		s.Path, _ = m["path"].(string)
		if t, ok := m["trust"].(map[string]any); ok {
			s.Trust = &Trust{}
			s.Trust.Issuer, _ = t["issuer"].(string)
			s.Trust.Identity, _ = t["identity"].(string)
		}
		out = append(out, s)
	}
	return out
}

// Same reports whether two sources name the same profile.
func (s Source) Same(o Source) bool {
	return s.Source == o.Source && s.Ref == o.Ref && s.Path == o.Path
}

// Fields returns the entry as written into a configuration file, in schema order.
func (s Source) Fields() [][2]any {
	fields := [][2]any{{"source", s.Source}}
	if s.Ref != "" {
		fields = append(fields, [2]any{"ref", s.Ref})
	}
	if s.Path != "" {
		fields = append(fields, [2]any{"path", s.Path})
	}
	if s.Trust != nil {
		fields = append(fields, [2]any{"trust", map[string]any{"issuer": s.Trust.Issuer, "identity": s.Trust.Identity}})
	}
	return fields
}

// Fetcher resolves sources to local files, fetches remote ones and checks signatures.
type Fetcher struct {
	// CacheDir holds the remote profiles (~/.cache/devbox/profiles).
	CacheDir  string
	Validator *schema.Validator
	// Expand resolves the placeholders of a source (no settings).
	Expand func(string) (string, error)
	// Client downloads https sources; nil uses the proxy of the environment.
	Client *http.Client
	// Git makes a shallow copy of a Git source into dest and returns the commit.
	Git func(ctx context.Context, name, source, ref, dest string) (string, error)
	// LookPath finds a command (cosign) in the PATH of the call.
	LookPath func(string) (string, error)
	// Getenv reads the environment of the call; cosign gets a few variables of it.
	Getenv func(string) string
}

func invalidf(format string, a ...any) error {
	return &config.Error{Code: config.CodeValidation, Detail: fmt.Sprintf(format, a...)}
}

func prerequisitef(format string, a ...any) error {
	return &config.Error{Code: config.CodePrerequisite, Detail: fmt.Sprintf(format, a...)}
}

// Location is a source with placeholders resolved.
type Location struct {
	Kind string
	// Address is the file path (file), the URL (https) or the Git source (git, with
	// the git+ prefix).
	Address string
	// Path is the profile file inside a Git repository.
	Path string
	// Key names the cache directory of a remote source.
	Key string
}

// Locate resolves the placeholders of a source and checks its form.
func (f *Fetcher) Locate(s Source) (Location, error) {
	addr := s.Source
	if f.Expand != nil {
		var err error
		if addr, err = f.Expand(s.Source); err != nil {
			return Location{}, invalidf("profile source %q: %v", s.Source, err)
		}
	}
	scheme, _, ok := strings.Cut(addr, "://")
	if !ok {
		return Location{}, invalidf("profile source %q has no scheme", s.Source)
	}
	loc := Location{Address: addr}
	switch scheme {
	case "file":
		u, err := url.Parse(addr)
		if err != nil {
			return Location{}, invalidf("profile source %q: %v", s.Source, err)
		}
		if u.Host != "" && u.Host != "localhost" {
			return Location{}, invalidf("profile source %q: a file source has no host (use file:///absolute/path)", s.Source)
		}
		p := u.Path
		if s.Path != "" {
			p = strings.TrimSuffix(p, "/") + "/" + s.Path
		}
		if !filepath.IsAbs(p) {
			return Location{}, invalidf("profile source %q: the path must be absolute (file:///absolute/path)", s.Source)
		}
		loc.Kind, loc.Address = KindFile, filepath.Clean(p)
	case "https":
		if s.Ref != "" || s.Path != "" {
			return Location{}, invalidf("profile source %q: ref and path are only supported for Git sources", s.Source)
		}
		loc.Kind = KindHTTPS
	case "git+https", "git+ssh", "git+file":
		loc.Kind = KindGit
		loc.Path = s.Path
		if loc.Path == "" {
			loc.Path = DefaultGitPath
		}
		if !filepath.IsLocal(loc.Path) {
			return Location{}, invalidf("profile source %q: path %q must stay inside the repository", s.Source, s.Path)
		}
	default:
		return Location{}, invalidf("profile source %q: %s:// is not supported (file, https, git+https, git+ssh)", s.Source, scheme)
	}
	if strings.Contains(addr, "@") && loc.Kind == KindHTTPS {
		if u, err := url.Parse(addr); err == nil && u.User != nil {
			return Location{}, invalidf("profile source %q: user information in the address is not supported", s.Source)
		}
	}
	if loc.Kind != KindFile {
		sum := sha256.Sum256([]byte(addr + "\n" + s.Ref + "\n" + loc.Path))
		loc.Key = hex.EncodeToString(sum[:8])
	}
	return loc, nil
}

// Remote reports whether the profile is fetched into the cache.
func (l Location) Remote() bool { return l.Kind != KindFile }

// Local returns the file that holds the profile: the file of a file source, the cached
// copy of a remote one (which may not exist yet).
func (f *Fetcher) Local(loc Location) string {
	if !loc.Remote() {
		return loc.Address
	}
	return filepath.Join(f.CacheDir, loc.Key, FileName)
}

// Fetched is the result of a fetch.
type Fetched struct {
	// Path is the local profile file.
	Path string
	// Revision is the Git commit or the SHA-256 of the downloaded file; empty for file
	// sources.
	Revision string
	// Changed reports whether the cached copy changed.
	Changed bool
}

// Fetch fetches a remote source into the cache, after checking the signature (with
// trust) and the schema; the previous copy stays when anything fails. A file source is
// only checked.
func (f *Fetcher) Fetch(ctx context.Context, s Source) (*Fetched, error) {
	loc, err := f.Locate(s)
	if err != nil {
		return nil, err
	}
	if !loc.Remote() {
		if err := f.Check(ctx, s, loc.Address); err != nil {
			return nil, err
		}
		return &Fetched{Path: loc.Address}, nil
	}
	ctx, cancel := context.WithTimeout(ctx, FetchTimeout)
	defer cancel()
	if err := os.MkdirAll(f.CacheDir, 0o700); err != nil {
		return nil, err
	}
	tmp, err := os.MkdirTemp(f.CacheDir, ".fetch-*")
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.RemoveAll(tmp) }()
	file := filepath.Join(tmp, FileName)
	var rev string
	switch loc.Kind {
	case KindHTTPS:
		if rev, err = f.download(ctx, loc.Address, file, true); err != nil {
			return nil, err
		}
		if s.Trust != nil {
			if _, err := f.download(ctx, loc.Address+BundleSuffix, file+BundleSuffix, false); err != nil {
				return nil, err
			}
		}
	case KindGit:
		if rev, err = f.fromGit(ctx, s, loc, tmp, file); err != nil {
			return nil, err
		}
	}
	if err := f.Check(ctx, s, file); err != nil {
		return nil, err
	}
	dest := filepath.Join(f.CacheDir, loc.Key)
	changed := !sameFiles(file, filepath.Join(dest, FileName))
	if err := replaceDir(tmp, dest); err != nil {
		return nil, err
	}
	return &Fetched{Path: filepath.Join(dest, FileName), Revision: rev, Changed: changed}, nil
}

func (f *Fetcher) fromGit(ctx context.Context, s Source, loc Location, tmp, file string) (string, error) {
	if f.Git == nil {
		return "", prerequisitef("Git sources are not available")
	}
	checkout := filepath.Join(tmp, "checkout")
	rev, err := f.Git(ctx, "profile", loc.Address, s.Ref, checkout)
	if err != nil {
		return "", err
	}
	src := filepath.Join(checkout, filepath.FromSlash(loc.Path))
	if err := copyRegular(src, file); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", invalidf("profile source %q: the repository has no file %s", s.Source, loc.Path)
		}
		return "", err
	}
	if s.Trust != nil {
		if err := copyRegular(src+BundleSuffix, file+BundleSuffix); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return "", err
		}
	}
	return rev, os.RemoveAll(checkout)
}

// copyRegular copies a regular file of at most MaxBytes; links are refused, so a
// repository cannot point outside of itself.
func copyRegular(src, dest string) error {
	fi, err := os.Lstat(src)
	if err != nil {
		return err
	}
	if !fi.Mode().IsRegular() {
		return invalidf("%s is not a regular file", filepath.Base(src))
	}
	if fi.Size() > MaxBytes {
		return invalidf("%s is larger than %d KiB", filepath.Base(src), MaxBytes>>10)
	}
	data, err := os.ReadFile(src) //nolint:gosec // a file of the fetched copy
	if err != nil {
		return err
	}
	return os.WriteFile(dest, data, 0o600) //nolint:gosec // dest is a file of the fetch directory, built here
}

func (f *Fetcher) client() *http.Client {
	if f.Client != nil {
		return f.Client
	}
	return &http.Client{Transport: &http.Transport{Proxy: http.ProxyFromEnvironment}}
}

// download saves address into dest and returns the SHA-256 of the file. A missing
// bundle (required false) is reported as a missing signature.
func (f *Fetcher) download(ctx context.Context, address, dest string, required bool) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return "", invalidf("profile source %q: %v", address, err)
	}
	resp, err := f.client().Do(req)
	if err != nil {
		return "", prerequisitef("download %s: %v", address, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusNotFound && !required {
		return "", nil // Check reports the missing bundle
	}
	if resp.StatusCode != http.StatusOK {
		return "", prerequisitef("download %s: %s", address, resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, MaxBytes+1))
	if err != nil {
		return "", prerequisitef("download %s: %v", address, err)
	}
	if len(data) > MaxBytes {
		return "", invalidf("download %s: larger than %d KiB", address, MaxBytes>>10)
	}
	if err := os.WriteFile(dest, data, 0o600); err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

// Check validates the profile file against the schema and, with trust, its signature.
func (f *Fetcher) Check(ctx context.Context, s Source, file string) error {
	if _, err := config.ReadFile(f.Validator, "profile", file, false); err != nil {
		return err
	}
	if s.Trust == nil {
		return nil
	}
	return f.Verify(ctx, s, file)
}

// Verify checks the Sigstore bundle <file>.sigstore.json with cosign: the profile must
// be signed by the identity and issuer of the source.
func (f *Fetcher) Verify(ctx context.Context, s Source, file string) error {
	bundle := file + BundleSuffix
	v := sigstore.Verifier{LookPath: f.LookPath, Getenv: f.Getenv}
	err := v.Verify(ctx, file, bundle, sigstore.Identity{Issuer: s.Trust.Issuer, Identity: s.Trust.Identity})
	var invalid *sigstore.InvalidError
	switch {
	case err == nil:
		return nil
	case errors.Is(err, sigstore.ErrNoBundle):
		return invalidf("profile source %q requires a signature by %s, but there is no signature bundle (%s)", s.Source, s.Trust.Identity, path.Base(bundle))
	case errors.Is(err, sigstore.ErrNoCosign):
		return prerequisitef("cosign is needed to check the signature of profile source %q and is not installed (or not in PATH)", s.Source)
	case errors.As(err, &invalid):
		return invalidf("the signature of profile source %q is not valid for %s (issuer %s): %s", s.Source, s.Trust.Identity, s.Trust.Issuer, invalid.Output)
	}
	return err
}

// Remove deletes the cached copy of a remote source.
func (f *Fetcher) Remove(loc Location) error {
	if !loc.Remote() {
		return nil
	}
	return os.RemoveAll(filepath.Join(f.CacheDir, loc.Key))
}

func sameFiles(a, b string) bool {
	da, err1 := os.ReadFile(a) //nolint:gosec // files of the cache
	db, err2 := os.ReadFile(b) //nolint:gosec // files of the cache
	return err1 == nil && err2 == nil && bytes.Equal(da, db)
}

// replaceDir moves src to dest; an existing dest is replaced.
func replaceDir(src, dest string) error {
	old := dest + ".old"
	_ = os.RemoveAll(old)
	if err := os.Rename(dest, old); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if err := os.Rename(src, dest); err != nil {
		_ = os.Rename(old, dest)
		return err
	}
	return os.RemoveAll(old)
}
