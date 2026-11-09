package core

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/Zenit-Hosting-de/devbox/internal/config"
	"github.com/Zenit-Hosting-de/devbox/internal/profile"
	"github.com/Zenit-Hosting-de/devbox/internal/repo"
)

// ProfileView is a profile source with what its local copy holds.
type ProfileView struct {
	// ID, Name and Version come from the profile; empty while it cannot be read.
	ID      string `json:"id"`
	Name    string `json:"name,omitempty"`
	Version string `json:"version,omitempty"`
	profile.Source
	// Kind is file, https or git.
	Kind string `json:"kind,omitempty"`
	// FetchedAt is when the cached copy of a remote profile was fetched.
	FetchedAt string `json:"fetchedAt,omitempty"`
	// Locked lists the settings the profile locks, sorted.
	Locked []string `json:"locked"`
	// Settings is the number of settings the profile sets.
	Settings int `json:"settings"`
	// Problem tells why the profile cannot be read; empty when it can.
	Problem string `json:"problem,omitempty"`
}

// ProfileUpdate is the outcome of fetching one profile again.
type ProfileUpdate struct {
	ProfileView
	// Changed reports whether the cached copy changed; always false for files.
	Changed bool `json:"changed"`
	// Revision is the Git commit or SHA-256 of the fetched file.
	Revision string `json:"revision,omitempty"`
	// Error is why the fetch failed; the previous copy stays.
	Error string `json:"error,omitempty"`
}

// ProfileFetcher returns the fetcher of profile sources for the environment.
func (b *Base) ProfileFetcher(env Env) *profile.Fetcher {
	look := func(name string) (string, error) { return LookPath(name, env.Getenv("PATH")) }
	git := &repo.Manager{Getenv: env.Getenv, LookPath: look}
	return &profile.Fetcher{
		CacheDir:  filepath.Join(b.Ctx.XDG.Cache, "devbox", "profiles"),
		Validator: b.Validator,
		Expand: func(s string) (string, error) {
			v, err := config.Expand(s, func(ref string) (any, error) { return config.ContextLookup(b.Ctx, ref) })
			str, _ := v.(string)
			return str, err
		},
		Client:   env.HTTPClient,
		Git:      git.FetchGit,
		LookPath: look,
		Getenv:   env.Getenv,
	}
}

// loadProfiles reads the profiles a configuration file names, in order. A remote
// profile without cached copy is fetched now; a file source with trust has its
// signature checked.
func loadProfiles(b *Base, env Env, f *config.File, origin string) ([]*config.File, error) {
	fetcher := b.ProfileFetcher(env)
	var out []*config.File
	for _, src := range profile.SourcesOf(f.Plain(), origin) {
		loc, err := fetcher.Locate(src)
		if err != nil {
			return nil, err
		}
		path := fetcher.Local(loc)
		switch {
		case loc.Remote() && !exists(path):
			if _, err := fetcher.Fetch(context.Background(), src); err != nil {
				return nil, err
			}
		case !loc.Remote() && src.Trust != nil:
			if err := fetcher.Verify(context.Background(), src, path); err != nil {
				return nil, err
			}
		}
		pf, err := config.ReadFile(b.Validator, "profile", path, false)
		if err != nil {
			return nil, err
		}
		out = append(out, pf)
	}
	return out, nil
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// profileFiles reads system and user configuration without plugins.
func profileFiles(b *Base) (system, user *config.File, err error) {
	if system, err = config.ReadFile(b.Validator, "config", b.SystemPath, true); err != nil {
		return nil, nil, err
	}
	if user, err = config.ReadFile(b.Validator, "config", b.UserPath, true); err != nil {
		return nil, nil, err
	}
	return system, user, nil
}

func allSources(system, user *config.File) []profile.Source {
	return append(profile.SourcesOf(system.Plain(), profile.OriginSystem), profile.SourcesOf(user.Plain(), profile.OriginUser)...)
}

// viewOf describes a source from its local copy; it fetches nothing.
func viewOf(b *Base, fetcher *profile.Fetcher, src profile.Source) ProfileView {
	v := ProfileView{Source: src, Locked: []string{}}
	loc, err := fetcher.Locate(src)
	if err != nil {
		v.Problem = err.Error()
		return v
	}
	v.Kind = loc.Kind
	path := fetcher.Local(loc)
	fi, err := os.Stat(path)
	switch {
	case errors.Is(err, fs.ErrNotExist) && loc.Remote():
		v.Problem = "not fetched yet"
		return v
	case err != nil:
		v.Problem = err.Error()
		return v
	}
	if loc.Remote() {
		v.FetchedAt = fi.ModTime().UTC().Format(time.RFC3339)
	}
	pf, err := config.ReadFile(b.Validator, "profile", path, false)
	if err != nil {
		v.Problem = err.Error()
		return v
	}
	doc := pf.Plain()
	v.ID, _ = doc["id"].(string)
	v.Name, _ = doc["name"].(string)
	v.Version, _ = doc["version"].(string)
	layer := config.ProfileLayer(pf)
	v.Settings = len(layer.Values)
	for key := range layer.Locked {
		v.Locked = append(v.Locked, key)
	}
	sort.Strings(v.Locked)
	return v
}

// Profiles lists the profile sources of the system and the user file, in layer order.
// It reads the local copies only and loads no plugins, so it works while a profile is
// broken.
func Profiles(env Env) ([]ProfileView, error) {
	b, err := NewBase(env)
	if err != nil {
		return nil, err
	}
	system, user, err := profileFiles(b)
	if err != nil {
		return nil, err
	}
	fetcher := b.ProfileFetcher(env)
	srcs := allSources(system, user)
	out := make([]ProfileView, 0, len(srcs))
	for _, src := range srcs {
		out = append(out, viewOf(b, fetcher, src))
	}
	return out, nil
}

// findProfile returns the view whose ID or source is ref.
func findProfile(views []ProfileView, ref string) (ProfileView, error) {
	for _, v := range views {
		if v.ID == ref && v.ID != "" {
			return v, nil
		}
	}
	for _, v := range views {
		if v.Source.Source == ref {
			return v, nil
		}
	}
	return ProfileView{}, ErrNotFound(ref, "no such profile (give the ID or the source)")
}

// Profile returns one profile by ID or source with its document; the document is nil
// when the profile cannot be read.
func Profile(env Env, ref string) (*ProfileView, map[string]any, error) {
	views, err := Profiles(env)
	if err != nil {
		return nil, nil, err
	}
	v, err := findProfile(views, ref)
	if err != nil {
		return nil, nil, err
	}
	if v.Problem != "" {
		return &v, nil, nil
	}
	b, err := NewBase(env)
	if err != nil {
		return nil, nil, err
	}
	fetcher := b.ProfileFetcher(env)
	loc, err := fetcher.Locate(v.Source)
	if err != nil {
		return nil, nil, err
	}
	pf, err := config.ReadFile(b.Validator, "profile", fetcher.Local(loc), false)
	if err != nil {
		return nil, nil, err
	}
	return &v, pf.Plain(), nil
}

// AddProfile fetches (or checks) a profile and appends its source to the user file.
// The same source or a second profile with the same ID is a conflict.
func AddProfile(ctx context.Context, env Env, src profile.Source) (*ProfileView, error) {
	b, err := NewBase(env)
	if err != nil {
		return nil, err
	}
	system, user, err := profileFiles(b)
	if err != nil {
		return nil, err
	}
	src.Origin = profile.OriginUser
	fetcher := b.ProfileFetcher(env)
	existing := allSources(system, user)
	for _, e := range existing {
		if e.Same(src) {
			return nil, &config.Error{Code: config.CodeConflict, Key: src.Source, Detail: "the profile source is already configured (" + e.Origin + ")"}
		}
	}
	// check the entry against the schema before anything is fetched
	if err := b.Validator.Validate("config", map[string]any{"profiles": []any{plainFields(src.Fields())}}); err != nil {
		return nil, &config.Error{Code: config.CodeValidation, Key: src.Source, Detail: err.Error()}
	}
	if _, err := fetcher.Fetch(ctx, src); err != nil {
		return nil, err
	}
	v := viewOf(b, fetcher, src)
	if v.Problem != "" {
		return nil, &config.Error{Code: config.CodeValidation, Key: src.Source, Detail: v.Problem}
	}
	for _, e := range existing {
		if other := viewOf(b, fetcher, e); other.ID == v.ID {
			if loc, err := fetcher.Locate(src); err == nil && !sameCache(fetcher, loc, e) {
				_ = fetcher.Remove(loc)
			}
			return nil, &config.Error{Code: config.CodeConflict, Key: v.ID, Detail: "a profile with this ID is already configured: " + e.Source}
		}
	}
	if err := config.AddProfile(b.Validator, b.UserPath, src.Fields()); err != nil {
		return nil, err
	}
	return &v, nil
}

// sameCache reports whether another source uses the same cached copy.
func sameCache(f *profile.Fetcher, loc profile.Location, other profile.Source) bool {
	ol, err := f.Locate(other)
	return err == nil && ol.Key == loc.Key
}

func plainFields(fields [][2]any) map[string]any {
	m := make(map[string]any, len(fields))
	for _, kv := range fields {
		key, _ := kv[0].(string)
		m[key] = kv[1]
	}
	return m
}

// UpdateProfiles fetches the remote profiles again (all, or the one with ID or source
// ref) and checks file profiles. A failed fetch keeps the previous copy and is
// reported in the result, not as error.
func UpdateProfiles(ctx context.Context, env Env, ref string) ([]ProfileUpdate, error) {
	b, err := NewBase(env)
	if err != nil {
		return nil, err
	}
	system, user, err := profileFiles(b)
	if err != nil {
		return nil, err
	}
	fetcher := b.ProfileFetcher(env)
	srcs := allSources(system, user)
	if ref != "" {
		views := make([]ProfileView, 0, len(srcs))
		for _, s := range srcs {
			views = append(views, viewOf(b, fetcher, s))
		}
		v, err := findProfile(views, ref)
		if err != nil {
			return nil, err
		}
		srcs = []profile.Source{v.Source}
	}
	out := make([]ProfileUpdate, 0, len(srcs))
	for _, src := range srcs {
		u := ProfileUpdate{}
		fetched, err := fetcher.Fetch(ctx, src)
		if err != nil {
			u.Error = err.Error()
		} else {
			u.Changed, u.Revision = fetched.Changed, fetched.Revision
		}
		u.ProfileView = viewOf(b, fetcher, src)
		out = append(out, u)
	}
	return out, nil
}

// RemoveProfile removes a profile source of the user file (by ID or source) and its
// cached copy. Profiles of the system file cannot be removed here.
func RemoveProfile(env Env, ref string) (*ProfileView, error) {
	views, err := Profiles(env)
	if err != nil {
		return nil, err
	}
	v, err := findProfile(views, ref)
	if err != nil {
		return nil, err
	}
	if v.Origin != profile.OriginUser {
		return nil, &config.Error{Code: config.CodeConflict, Key: ref, Detail: "the profile comes from the " + v.Origin + " configuration and can only be removed there"}
	}
	b, err := NewBase(env)
	if err != nil {
		return nil, err
	}
	match := func(m map[string]any) bool {
		return profile.SourcesOf(map[string]any{"profiles": []any{m}}, profile.OriginUser)[0].Same(v.Source)
	}
	if _, err := config.RemoveProfile(b.Validator, b.UserPath, match); err != nil {
		return nil, err
	}
	fetcher := b.ProfileFetcher(env)
	if loc, err := fetcher.Locate(v.Source); err == nil {
		shared := false
		for _, o := range views {
			if !o.Same(v.Source) && sameCache(fetcher, loc, o.Source) {
				shared = true
			}
		}
		if !shared {
			if err := fetcher.Remove(loc); err != nil {
				return nil, err
			}
		}
	}
	return &v, nil
}
