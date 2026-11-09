package catalog

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Zenit-Hosting-de/devbox/internal/config"
	"github.com/Zenit-Hosting-de/devbox/internal/jsonfile"
	"github.com/Zenit-Hosting-de/devbox/internal/plugin"
	"github.com/Zenit-Hosting-de/devbox/internal/repo"
	"github.com/Zenit-Hosting-de/devbox/internal/schema"
	"github.com/Zenit-Hosting-de/devbox/internal/semver"
	"github.com/Zenit-Hosting-de/devbox/internal/sigstore"
)

const (
	// IndexFile is the name of a cached index.
	IndexFile = "index.json"
	// MaxIndexBytes bounds a downloaded index.
	MaxIndexBytes = 8 << 20
	// MaxPackageBytes bounds a downloaded package.
	MaxPackageBytes = 100 << 20
	// MaxBundleBytes bounds a downloaded Sigstore bundle.
	MaxBundleBytes = 1 << 20
	// Timeout bounds one sync or one download with its signature check.
	Timeout = 5 * time.Minute
)

// Catalog is a repositories entry of kind catalog.
type Catalog struct {
	Name string
	// Source is the address of index.json (https:// or file://), placeholders resolved.
	Source string
	// Signer is the identity that must have signed every package; nil when the entry
	// has no trust object (then nothing can be installed from it).
	Signer *sigstore.Identity
}

// Release is one published version of a plugin.
type Release struct {
	Version     string `json:"version"`
	URL         string `json:"url"`
	Digest      string `json:"digest"`
	Bundle      string `json:"bundle"`
	PublishedAt string `json:"publishedAt"`
	Yanked      bool   `json:"yanked,omitempty"`
}

// Entry is a plugin of an index.
type Entry struct {
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	Category    string    `json:"category,omitempty"`
	Homepage    string    `json:"homepage,omitempty"`
	Versions    []Release `json:"versions"`
}

// Index is a parsed index.json.
type Index struct {
	Name        string           `json:"name,omitempty"`
	GeneratedAt string           `json:"generatedAt"`
	Plugins     map[string]Entry `json:"plugins"`
}

// LockEntry is one plugin installed from a catalog (schema/lock.v1.json).
type LockEntry struct {
	Catalog     string `json:"catalog"`
	Version     string `json:"version"`
	Digest      string `json:"digest"`
	SignedBy    string `json:"signedBy"`
	InstalledAt string `json:"installedAt"`
}

// Manager syncs catalogs and installs plugins from them.
type Manager struct {
	// CacheDir holds the indexes (~/.cache/devbox/catalog/<name>/index.json).
	CacheDir string
	// PluginsDir holds the installed plugins, one folder per plugin ID.
	PluginsDir string
	// LockPath is plugins.lock.json.
	LockPath  string
	Validator *schema.Validator
	// Client downloads; nil uses the proxy of the environment.
	Client   *http.Client
	Verifier sigstore.Verifier
	// Now returns the current time; nil uses the clock.
	Now func() time.Time
}

func invalidf(format string, a ...any) error {
	return &config.Error{Code: config.CodeValidation, Detail: fmt.Sprintf(format, a...)}
}

func prerequisitef(format string, a ...any) error {
	return &config.Error{Code: config.CodePrerequisite, Detail: fmt.Sprintf(format, a...)}
}

func notFoundf(key, format string, a ...any) error {
	return &config.Error{Code: config.CodeNotFound, Key: key, Detail: fmt.Sprintf(format, a...)}
}

func conflictf(key, format string, a ...any) error {
	return &config.Error{Code: config.CodeConflict, Key: key, Detail: fmt.Sprintf(format, a...)}
}

func (m *Manager) now() string {
	t := time.Now()
	if m.Now != nil {
		t = m.Now()
	}
	return t.UTC().Format(time.RFC3339)
}

func (m *Manager) client() *http.Client {
	if m.Client != nil {
		return m.Client
	}
	return &http.Client{Transport: &http.Transport{Proxy: http.ProxyFromEnvironment}}
}

func (m *Manager) indexPath(name string) string {
	return filepath.Join(m.CacheDir, name, IndexFile)
}

// fetch reads address (https:// or file://) into dest, at most limit bytes, and returns
// the SHA-256 of the content.
func (m *Manager) fetch(ctx context.Context, address, dest string, limit int64) (string, error) {
	u, err := url.Parse(address)
	if err != nil {
		return "", invalidf("address %q: %v", address, err)
	}
	var body io.ReadCloser
	switch u.Scheme {
	case "file":
		f, err := os.Open(u.Path) //nolint:gosec // a file source of the catalog configured by the user
		if errors.Is(err, fs.ErrNotExist) {
			return "", notFoundf(address, "the file does not exist")
		}
		if err != nil {
			return "", err
		}
		body = f
	case "https":
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
		if err != nil {
			return "", invalidf("address %q: %v", address, err)
		}
		resp, err := m.client().Do(req)
		if err != nil {
			return "", prerequisitef("download %s: %v", address, err)
		}
		if resp.StatusCode == http.StatusNotFound {
			_ = resp.Body.Close()
			return "", notFoundf(address, "the server has no such file (404)")
		}
		if resp.StatusCode != http.StatusOK {
			_ = resp.Body.Close()
			return "", prerequisitef("download %s: %s", address, resp.Status)
		}
		body = resp.Body
	default:
		return "", invalidf("address %q: only https:// and file:// are supported", address)
	}
	defer func() { _ = body.Close() }()
	if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
		return "", err
	}
	out, err := os.OpenFile(dest, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600) //nolint:gosec // a file of the cache, built here
	if err != nil {
		return "", err
	}
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(out, h), io.LimitReader(body, limit+1))
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return "", prerequisitef("download %s: %v", address, err)
	}
	if n > limit {
		return "", invalidf("download %s: larger than %d MiB", address, limit>>20)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// Sync downloads the index of a catalog, validates it and replaces the cached copy.
func (m *Manager) Sync(ctx context.Context, c Catalog) (*Index, error) {
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()
	dest := m.indexPath(c.Name)
	tmp := dest + ".tmp"
	defer func() { _ = os.Remove(tmp) }()
	if _, err := m.fetch(ctx, c.Source, tmp, MaxIndexBytes); err != nil {
		return nil, err
	}
	idx, err := m.readIndex(tmp)
	if err != nil {
		return nil, err
	}
	if err := os.Rename(tmp, dest); err != nil {
		return nil, err
	}
	return idx, nil
}

func (m *Manager) readIndex(path string) (*Index, error) {
	f, err := config.ReadFile(m.Validator, "catalog", path, false)
	if err != nil {
		return nil, err
	}
	var idx Index
	if err := json.Unmarshal(f.Data, &idx); err != nil {
		return nil, invalidf("%s: %v", path, err)
	}
	return &idx, nil
}

// Cached returns the cached index of a catalog, or nil when it was never synced.
func (m *Manager) Cached(c Catalog) (*Index, error) {
	idx, err := m.readIndex(m.indexPath(c.Name))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	return idx, err
}

// SyncedAt returns when the cached index was written; empty when there is none.
func (m *Manager) SyncedAt(c Catalog) string {
	fi, err := os.Stat(m.indexPath(c.Name))
	if err != nil {
		return ""
	}
	return fi.ModTime().UTC().Format(time.RFC3339)
}

// index returns the cached index and syncs once when there is none.
func (m *Manager) index(ctx context.Context, c Catalog) (*Index, error) {
	idx, err := m.Cached(c)
	if err != nil || idx != nil {
		return idx, err
	}
	return m.Sync(ctx, c)
}

// Newest returns the newest release that is not withdrawn and matches the range (empty:
// any stable release); false when there is none.
func (e Entry) Newest(rng string) (Release, bool) {
	var r *semver.Range
	if rng != "" {
		parsed, err := semver.ParseRange(rng)
		if err != nil {
			return Release{}, false
		}
		r = &parsed
	}
	var best Release
	var bestV semver.Version
	found := false
	for _, rel := range e.Versions {
		if rel.Yanked {
			continue
		}
		v, err := semver.Parse(rel.Version)
		if err != nil {
			continue
		}
		if r != nil && !r.Match(v) || r == nil && len(v.Pre) > 0 {
			continue
		}
		if !found || semver.Compare(v, bestV) > 0 {
			best, bestV, found = rel, v, true
		}
	}
	return best, found
}

// Hit is a search result.
type Hit struct {
	ID          string `json:"id"`
	Catalog     string `json:"catalog"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Category    string `json:"category,omitempty"`
	Version     string `json:"version"`
	// Installed is the version installed from a catalog; empty when it is not.
	Installed string `json:"installed,omitempty"`
}

// Search lists the plugins of the catalogs whose ID, name or description contains
// query (all for an empty query), sorted by ID and catalog. A catalog that was never
// synced is synced first; one that fails is reported in errs and skipped.
func (m *Manager) Search(ctx context.Context, cats []Catalog, query string) (hits []Hit, errs map[string]error, err error) {
	lock, err := m.Lock()
	if err != nil {
		return nil, nil, err
	}
	q := strings.ToLower(strings.TrimSpace(query))
	hits = []Hit{}
	errs = map[string]error{}
	for _, c := range cats {
		idx, err := m.index(ctx, c)
		if err != nil {
			errs[c.Name] = err
			continue
		}
		for id, e := range idx.Plugins {
			text := strings.ToLower(id + " " + e.Name + " " + e.Description)
			if q != "" && !strings.Contains(text, q) {
				continue
			}
			rel, ok := e.Newest("")
			if !ok {
				continue
			}
			h := Hit{ID: id, Catalog: c.Name, Name: e.Name, Description: e.Description, Category: e.Category, Version: rel.Version}
			if l, ok := lock[id]; ok && l.Catalog == c.Name {
				h.Installed = l.Version
			}
			hits = append(hits, h)
		}
	}
	sort.Slice(hits, func(i, j int) bool {
		if hits[i].ID != hits[j].ID {
			return hits[i].ID < hits[j].ID
		}
		return hits[i].Catalog < hits[j].Catalog
	})
	return hits, errs, nil
}

// Lock reads plugins.lock.json; a missing file is an empty lock.
func (m *Manager) Lock() (map[string]LockEntry, error) {
	f, err := config.ReadFile(m.Validator, "lock", m.LockPath, true)
	if err != nil {
		return nil, err
	}
	out := map[string]LockEntry{}
	if !f.Exists {
		return out, nil
	}
	var doc struct {
		Plugins map[string]LockEntry `json:"plugins"`
	}
	if err := json.Unmarshal(f.Data, &doc); err != nil {
		return nil, invalidf("%s: %v", m.LockPath, err)
	}
	for k, v := range doc.Plugins {
		out[k] = v
	}
	return out, nil
}

func (m *Manager) writeLock(lock map[string]LockEntry) error {
	doc := jsonfile.NewObject()
	if id, ok := m.Validator.ID("lock"); ok {
		doc.Set("$schema", id)
	}
	plugins := jsonfile.NewObject()
	ids := make([]string, 0, len(lock))
	for id := range lock {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		l := lock[id]
		e := jsonfile.NewObject()
		e.Set("catalog", l.Catalog)
		e.Set("version", l.Version)
		e.Set("digest", l.Digest)
		e.Set("signedBy", l.SignedBy)
		e.Set("installedAt", l.InstalledAt)
		plugins.Set(id, e)
	}
	doc.Set("plugins", plugins)
	if err := m.Validator.Validate("lock", jsonfile.Plain(doc)); err != nil {
		return err
	}
	data, err := jsonfile.Marshal(doc)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(m.LockPath), 0o700); err != nil {
		return err
	}
	return jsonfile.WriteAtomic(m.LockPath, data, 0o600)
}

// Plan is a downloaded and verified package that waits to be installed.
type Plan struct {
	Catalog Catalog
	ID      string
	Release Release
	// Installed is the version installed before; empty for a new plugin.
	Installed string
	// Permissions are the declared permissions of the package; New those the installed
	// version does not declare (all of them for a new plugin).
	Permissions []repo.Permission
	New         []repo.Permission
	dir, tmp    string
}

// Close removes the downloaded files of a plan that was not committed.
func (p *Plan) Close() {
	if p.tmp != "" {
		_ = os.RemoveAll(p.tmp)
	}
}

// Find returns the catalog that offers the plugin: the named one, or the only catalog
// with it. Several catalogs with the plugin and no name is a conflict.
func (m *Manager) Find(ctx context.Context, cats []Catalog, id, name string) (Catalog, Entry, error) {
	var found []Catalog
	var entries []Entry
	for _, c := range cats {
		if name != "" && c.Name != name {
			continue
		}
		idx, err := m.index(ctx, c)
		if err != nil {
			if name != "" {
				return Catalog{}, Entry{}, err
			}
			continue
		}
		if e, ok := idx.Plugins[id]; ok {
			found = append(found, c)
			entries = append(entries, e)
		}
	}
	switch {
	case name != "" && len(cats) > 0 && len(found) == 0:
		return Catalog{}, Entry{}, notFoundf(id, "catalog %s does not offer this plugin", name)
	case len(found) == 0:
		return Catalog{}, Entry{}, notFoundf(id, "no catalog offers this plugin")
	case len(found) > 1:
		names := make([]string, 0, len(found))
		for _, c := range found {
			names = append(names, c.Name)
		}
		return Catalog{}, Entry{}, conflictf(id, "several catalogs offer this plugin (%s); choose one with --catalog", strings.Join(names, ", "))
	}
	return found[0], entries[0], nil
}

// Prepare downloads a release, checks its digest and its Sigstore signature, unpacks it
// and compares its permissions with the installed version. Close the plan when it is
// not committed.
func (m *Manager) Prepare(ctx context.Context, c Catalog, id string, e Entry, version string) (*Plan, error) {
	if c.Signer == nil {
		return nil, invalidf("catalog %s has no trust identity; packages of a catalog must be signed (set trust with issuer and identity)", c.Name)
	}
	var rel Release
	if version != "" {
		ok := false
		for _, r := range e.Versions {
			if r.Version == version {
				rel, ok = r, true
			}
		}
		if !ok {
			return nil, notFoundf(id, "catalog %s has no version %s", c.Name, version)
		}
		if rel.Yanked {
			return nil, conflictf(id, "version %s was withdrawn from catalog %s", version, c.Name)
		}
	} else {
		var ok bool
		if rel, ok = e.Newest(""); !ok {
			return nil, notFoundf(id, "catalog %s has no stable release of this plugin", c.Name)
		}
	}
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()
	if err := os.MkdirAll(m.PluginsDir, 0o700); err != nil {
		return nil, err
	}
	tmp, err := os.MkdirTemp(m.PluginsDir, ".install-*")
	if err != nil {
		return nil, err
	}
	p := &Plan{Catalog: c, ID: id, Release: rel, tmp: tmp}
	if err := m.download(ctx, p); err != nil {
		p.Close()
		return nil, err
	}
	return p, nil
}

func (m *Manager) download(ctx context.Context, p *Plan) error {
	c, rel := p.Catalog, p.Release
	pkgURL, err := resolve(c.Source, rel.URL)
	if err != nil {
		return err
	}
	bundleURL, err := resolve(c.Source, rel.Bundle)
	if err != nil {
		return err
	}
	pkg := filepath.Join(p.tmp, "package.tar.gz")
	sum, err := m.fetch(ctx, pkgURL, pkg, MaxPackageBytes)
	if err != nil {
		return err
	}
	if "sha256:"+sum != rel.Digest {
		return invalidf("the package of %s %s does not match its digest (catalog %s says %s, the download is sha256:%s)", p.ID, rel.Version, c.Name, rel.Digest, sum)
	}
	bundle := filepath.Join(p.tmp, "package.sigstore.json")
	if _, err := m.fetch(ctx, bundleURL, bundle, MaxBundleBytes); err != nil {
		if config.IsCode(err, config.CodeNotFound) {
			return invalidf("the package of %s %s is not signed: catalog %s names no signature bundle that exists", p.ID, rel.Version, c.Name)
		}
		return err
	}
	if err := m.Verifier.Verify(ctx, pkg, bundle, *c.Signer); err != nil {
		var invalid *sigstore.InvalidError
		switch {
		case errors.Is(err, sigstore.ErrNoCosign):
			return prerequisitef("cosign is needed to check the signature of %s from catalog %s and is not installed (or not in PATH)", p.ID, c.Name)
		case errors.As(err, &invalid):
			return invalidf("the signature of %s %s from catalog %s is not valid for %s (issuer %s): %s", p.ID, rel.Version, c.Name, c.Signer.Identity, c.Signer.Issuer, invalid.Output)
		}
		return err
	}
	p.dir = filepath.Join(p.tmp, p.ID)
	if err := repo.ExtractTarGz(pkg, p.dir); err != nil {
		return err
	}
	man, err := plugin.Load(m.Validator, p.dir)
	if err != nil {
		return invalidf("the package of %s %s has no valid manifest: %v", p.ID, rel.Version, err)
	}
	if man.ID != p.ID {
		return invalidf("the package of %s holds the plugin %s", p.ID, man.ID)
	}
	if v, _ := man.Doc["version"].(string); v != rel.Version {
		return invalidf("the package of %s %s holds version %s", p.ID, rel.Version, v)
	}
	p.Permissions = repo.PermissionsOf(man.Doc)
	old := map[repo.Permission]bool{}
	if prev, err := plugin.Load(m.Validator, filepath.Join(m.PluginsDir, p.ID)); err == nil {
		p.Installed, _ = prev.Doc["version"].(string)
		for _, perm := range repo.PermissionsOf(prev.Doc) {
			old[perm] = true
		}
	}
	p.New = []repo.Permission{}
	for _, perm := range p.Permissions {
		if !old[perm] {
			p.New = append(p.New, perm)
		}
	}
	return nil
}

// resolve makes ref absolute against the address of index.json.
func resolve(base, ref string) (string, error) {
	b, err := url.Parse(base)
	if err != nil {
		return "", invalidf("catalog address %q: %v", base, err)
	}
	r, err := url.Parse(ref)
	if err != nil {
		return "", invalidf("package address %q: %v", ref, err)
	}
	u := b.ResolveReference(r)
	if u.Scheme != b.Scheme && (b.Scheme != "file" || u.Scheme != "https") {
		return "", invalidf("package address %q: only %s:// is allowed in this catalog", ref, b.Scheme)
	}
	if u.Scheme != "https" && u.Scheme != "file" {
		return "", invalidf("package address %q: only https:// is allowed", ref)
	}
	return u.String(), nil
}

// Commit puts the plugin of the plan in place (replacing an installed version) and
// records it in the lock file.
func (m *Manager) Commit(p *Plan) error {
	defer p.Close()
	lock, err := m.Lock()
	if err != nil {
		return err
	}
	if l, ok := lock[p.ID]; ok && l.Catalog != p.Catalog.Name {
		return conflictf(p.ID, "the plugin is installed from catalog %s; remove it first", l.Catalog)
	}
	dest := filepath.Join(m.PluginsDir, p.ID)
	old := filepath.Join(p.tmp, "previous")
	if err := os.Rename(dest, old); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if err := os.Rename(p.dir, dest); err != nil {
		_ = os.Rename(old, dest)
		return err
	}
	lock[p.ID] = LockEntry{
		Catalog: p.Catalog.Name, Version: p.Release.Version, Digest: p.Release.Digest,
		SignedBy: p.Catalog.Signer.Identity, InstalledAt: m.now(),
	}
	return m.writeLock(lock)
}

// Remove deletes a plugin installed from a catalog and its lock entry.
func (m *Manager) Remove(id string) (LockEntry, error) {
	lock, err := m.Lock()
	if err != nil {
		return LockEntry{}, err
	}
	l, ok := lock[id]
	if !ok {
		return LockEntry{}, notFoundf(id, "the plugin is not installed from a catalog")
	}
	if err := os.RemoveAll(filepath.Join(m.PluginsDir, id)); err != nil {
		return LockEntry{}, err
	}
	delete(lock, id)
	return l, m.writeLock(lock)
}
