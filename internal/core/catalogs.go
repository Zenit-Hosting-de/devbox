package core

import (
	"context"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Zenit-Hosting-de/devbox/internal/catalog"
	"github.com/Zenit-Hosting-de/devbox/internal/config"
	"github.com/Zenit-Hosting-de/devbox/internal/plugin"
	"github.com/Zenit-Hosting-de/devbox/internal/repo"
	"github.com/Zenit-Hosting-de/devbox/internal/sigstore"
)

// catalogDir is the directory below the data and the cache directory for catalogs.
const catalogDir = "catalog"

// lockFile is the lock file of plugins installed from catalogs, in the data directory.
const lockFile = "plugins.lock.json"

// CatalogManager returns the manager of catalogs and installed catalog plugins.
func (b *Base) CatalogManager(env Env) *catalog.Manager {
	look := func(name string) (string, error) { return LookPath(name, env.Getenv("PATH")) }
	return &catalog.Manager{
		CacheDir:   filepath.Join(b.Ctx.XDG.Cache, "devbox", catalogDir),
		PluginsDir: filepath.Join(b.DataDir(), catalogDir),
		LockPath:   filepath.Join(b.DataDir(), lockFile),
		Validator:  b.Validator,
		Client:     env.HTTPClient,
		Verifier:   sigstore.Verifier{LookPath: look, Getenv: env.Getenv},
	}
}

// catalogRoot is the search root of the plugins installed from catalogs.
func (b *Base) catalogRoot() plugin.Root {
	return plugin.Root{Dir: filepath.Join(b.DataDir(), catalogDir), Origin: plugin.Origin{Kind: plugin.OriginCatalog}}
}

// markCatalogOrigins names catalog and version of the plugins installed from catalogs.
func (st *State) markCatalogOrigins(cands []plugin.Candidate) {
	lock, err := st.Catalog.Lock()
	if err != nil {
		return
	}
	for i := range cands {
		if cands[i].Origin.Kind != plugin.OriginCatalog {
			continue
		}
		if l, ok := lock[filepath.Base(cands[i].Dir)]; ok {
			cands[i].Origin.Repository, cands[i].Origin.Revision = l.Catalog, l.Version
		}
	}
}

// Catalogs returns the switched-on repositories of kind catalog.
func (st *State) Catalogs() []catalog.Catalog {
	var out []catalog.Catalog
	for _, e := range st.Repositories {
		if c, ok := catalogOf(e); ok {
			out = append(out, c)
		}
	}
	return out
}

func catalogOf(e repo.Entry) (catalog.Catalog, bool) {
	if e.Kind != repo.KindCatalog || !e.Enabled {
		return catalog.Catalog{}, false
	}
	src, err := e.Expanded()
	if err != nil {
		return catalog.Catalog{}, false
	}
	return catalog.Catalog{Name: e.Name, Source: src, Signer: e.Signer}, true
}

// catalogView fills the state of a catalog entry in a repository view: active when its
// index is cached, new otherwise.
func (st *State) catalogView(e repo.Entry, v repo.View) repo.View {
	if e.Kind != repo.KindCatalog {
		return v
	}
	v.Trust = repo.TrustSignature
	c, ok := catalogOf(e)
	switch {
	case !e.Enabled:
		v.State = repo.StateDisabled
	case !ok:
		v.State, v.LastError = repo.StateError, "the source cannot be resolved"
	case e.Signer == nil:
		v.State, v.LastError = repo.StateError, "a catalog needs trust with issuer and identity"
	case st.Catalog.SyncedAt(c) != "":
		v.State = repo.StateActive
		v.LastSync = &repo.LastSync{At: st.Catalog.SyncedAt(c), Changed: false}
	default:
		v.State = repo.StateNew
	}
	return v
}

// SyncCatalog downloads the index of a catalog again.
func (st *State) SyncCatalog(ctx context.Context, name string) error {
	e, err := st.Repository(name)
	if err != nil {
		return err
	}
	c, ok := catalogOf(e)
	switch {
	case !ok:
		return &config.Error{Code: config.CodeConflict, Key: name, Detail: "not a switched-on catalog"}
	case c.Signer == nil:
		return &config.Error{Code: config.CodeConflict, Key: name, Detail: "a catalog needs trust with issuer and identity"}
	}
	_, err = st.Catalog.Sync(ctx, c)
	return err
}

// CatalogSearch lists the plugins of all catalogs that match the query. Catalogs that
// cannot be read are named in the returned problems.
func (st *State) CatalogSearch(ctx context.Context, query string) ([]catalog.Hit, map[string]string, error) {
	hits, errs, err := st.Catalog.Search(ctx, st.Catalogs(), query)
	if err != nil {
		return nil, nil, err
	}
	problems := map[string]string{}
	for name, e := range errs {
		problems[name] = e.Error()
	}
	return hits, problems, nil
}

// PrepareInstall downloads and checks a plugin from a catalog (the named one, or the
// only one that offers it); version empty means the newest stable release. A plugin
// that is installed some other way (bundled, folder, repository) under the same ID is
// a conflict. Close the plan unless it is committed.
func (st *State) PrepareInstall(ctx context.Context, ref, catalogName string) (*catalog.Plan, error) {
	id, version, _ := strings.Cut(ref, "@")
	if p := FindPlugin(st.Evaluate(), id); p != nil && p.Origin.Kind != plugin.OriginCatalog {
		return nil, &config.Error{Code: config.CodeConflict, Key: id, Detail: "a plugin with this ID is already present (" + p.Origin.String() + ")"}
	}
	c, entry, err := st.Catalog.Find(ctx, st.Catalogs(), id, catalogName)
	if err != nil {
		return nil, err
	}
	return st.Catalog.Prepare(ctx, c, id, entry, version)
}

// PrepareUpdate downloads and checks the newest release of an installed catalog plugin;
// nil when the installed version is the newest.
func (st *State) PrepareUpdate(ctx context.Context, id string) (*catalog.Plan, error) {
	lock, err := st.Catalog.Lock()
	if err != nil {
		return nil, err
	}
	l, ok := lock[id]
	if !ok {
		return nil, ErrNotFound(id, "the plugin is not installed from a catalog")
	}
	c, entry, err := st.Catalog.Find(ctx, st.Catalogs(), id, l.Catalog)
	if err != nil {
		return nil, err
	}
	if _, err := st.Catalog.Sync(ctx, c); err == nil {
		if c, entry, err = st.Catalog.Find(ctx, st.Catalogs(), id, l.Catalog); err != nil {
			return nil, err
		}
	}
	rel, ok := entry.Newest("")
	if !ok || rel.Version == l.Version {
		return nil, nil
	}
	return st.Catalog.Prepare(ctx, c, id, entry, rel.Version)
}

// InstalledFromCatalogs returns the IDs in the lock file, sorted.
func (st *State) InstalledFromCatalogs() ([]string, error) {
	lock, err := st.Catalog.Lock()
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(lock))
	for id := range lock {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids, nil
}

// RemoveCatalogPlugin removes a plugin installed from a catalog.
func (st *State) RemoveCatalogPlugin(id string) error {
	_, err := st.Catalog.Remove(id)
	if config.IsCode(err, config.CodeNotFound) {
		if p := FindPlugin(st.Evaluate(), id); p != nil {
			return &config.Error{Code: config.CodeConflict, Key: id, Detail: "the plugin is not installed from a catalog (" + p.Origin.String() + ") and cannot be removed here"}
		}
	}
	return err
}
