package core

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/Zenit-Hosting-de/devbox/internal/config"
	"github.com/Zenit-Hosting-de/devbox/internal/repo"
)

// reposDir is the directory below the data directory that holds the repositories.
const reposDir = "repositories"

// categoryCore is the category of plugins that cannot be switched off.
const categoryCore = "core"

func (st *State) newRepoManager() *repo.Manager {
	return &repo.Manager{
		Store:     repo.Store{Root: filepath.Join(st.DataDir(), reposDir)},
		Validator: st.Validator,
		Getenv:    st.Env.Getenv,
		LookPath: func(name string) (string, error) {
			return LookPath(name, st.Env.Getenv("PATH"))
		},
		Client: st.Env.HTTPClient,
	}
}

// collectRepositories merges the repositories of the system file, the profiles and the
// user file.
func (st *State) collectRepositories() []repo.Entry {
	layers := []repo.Layer{{Origin: repo.OriginSystem, Repositories: repositoriesOf(st.System.Plain())}}
	// profiles of the system file come first, then those of the user file
	for _, pf := range st.Profiles {
		doc := pf.Plain()
		layers = append(layers, repo.Layer{
			Origin:       config.LayerProfilePrefix + fmt.Sprint(doc["id"]),
			Repositories: repositoriesOf(doc),
		})
	}
	layers = append(layers, repo.Layer{Origin: repo.OriginUser, Repositories: repositoriesOf(st.User.Plain())})
	expand := func(s string) (string, error) {
		v, err := config.Expand(s, func(ref string) (any, error) { return config.ContextLookup(st.Ctx, ref) })
		if err != nil {
			return "", err
		}
		out, _ := v.(string)
		return out, nil
	}
	return repo.Collect(layers, expand)
}

func repositoriesOf(doc map[string]any) map[string]any {
	m, _ := doc["repositories"].(map[string]any)
	return m
}

// Repository returns the merged entry with the name.
func (st *State) Repository(name string) (repo.Entry, error) {
	for _, e := range st.Repositories {
		if e.Name == name {
			return e, nil
		}
	}
	return repo.Entry{}, ErrNotFound(name, "no such repository")
}

// RepositoryViews lists all repositories with their state, sorted by name.
func (st *State) RepositoryViews() []repo.View { return st.Repos.Views(st.Repositories) }

// RepositoryView returns one repository with its state.
func (st *State) RepositoryView(name string) (repo.View, error) {
	e, err := st.Repository(name)
	if err != nil {
		return repo.View{}, err
	}
	return st.Repos.View(e), nil
}

// RepoSpec is a new plugin repository.
type RepoSpec struct {
	Name, Source, Ref, Path, Trust string
}

// AddRepository writes a repository of kind plugins into the user file. The name must
// be free in every layer and the fields must be valid; the repository is not synced.
func (st *State) AddRepository(spec RepoSpec) error {
	for _, e := range st.Repositories {
		if e.Name == spec.Name {
			return &config.Error{Code: config.CodeConflict, Key: spec.Name, Detail: "a repository with this name exists (" + e.Origin + ")"}
		}
	}
	if err := repo.ValidateNew(spec.Name, spec.Source, spec.Ref, spec.Path, spec.Trust); err != nil {
		return err
	}
	fields := [][2]any{{"kind", repo.KindPlugins}, {"source", spec.Source}}
	for _, kv := range [][2]string{{"ref", spec.Ref}, {"path", spec.Path}, {"trust", spec.Trust}} {
		if kv[1] != "" {
			fields = append(fields, [2]any{kv[0], kv[1]})
		}
	}
	return config.SetRepository(st.Validator, st.UserPath, spec.Name, fields)
}

// RemoveRepository deletes a repository of the user file and its files. Entries of
// the system file and of profiles cannot be removed here.
func (st *State) RemoveRepository(ctx context.Context, name string) error {
	e, err := st.Repository(name)
	if err != nil {
		return err
	}
	if e.Origin != repo.OriginUser {
		return &config.Error{Code: config.CodeConflict, Key: name, Detail: "the repository comes from " + e.Origin + " and can only be removed there"}
	}
	if _, err := config.RemoveRepository(st.Validator, st.UserPath, name); err != nil {
		return err
	}
	return st.Repos.Store.Remove(ctx, name)
}

// SyncRepository fetches a repository.
func (st *State) SyncRepository(ctx context.Context, name string) error {
	e, err := st.Repository(name)
	if err != nil {
		return err
	}
	return st.Repos.Sync(ctx, e)
}

// ApproveRepository makes the pending revision of a repository active.
func (st *State) ApproveRepository(ctx context.Context, name, revision string) error {
	e, err := st.Repository(name)
	if err != nil {
		return err
	}
	return st.Repos.Approve(ctx, e, revision)
}

// DiscardRepository drops the pending revision of a repository.
func (st *State) DiscardRepository(ctx context.Context, name string) error {
	e, err := st.Repository(name)
	if err != nil {
		return err
	}
	return st.Repos.Discard(ctx, e)
}

// SetPluginEnabled switches a plugin on or off in the user file. Plugins of the
// category core cannot be switched off, and a plugin that the system file or a
// profile switches off cannot be switched on. Doing the same twice changes nothing.
// It reports whether the file changed.
func (st *State) SetPluginEnabled(id string, enabled bool) (bool, error) {
	p := FindPlugin(st.Evaluate(), id)
	if p == nil {
		return false, ErrNotFound(id, "no such plugin")
	}
	if enabled {
		if by := st.disabledSources(false)(id); by != "" {
			return false, &config.Error{Code: config.CodeConflict, Key: id, Detail: "switched off by " + by + "; the user cannot switch it on"}
		}
	} else if p.Manifest != nil {
		if category, _ := p.Manifest.Doc["category"].(string); category == categoryCore {
			return false, &config.Error{Code: config.CodeConflict, Key: id, Detail: "core plugins cannot be switched off"}
		}
	}
	return config.SetPluginDisabled(st.Validator, st.UserPath, id, !enabled)
}
