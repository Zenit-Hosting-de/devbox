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

// RepoPatch changes fields of a repository of the user file. A nil field stays as it
// is; an empty Ref or Path removes the field (and an empty Trust returns to the
// default, prompt).
type RepoPatch struct {
	Source, Ref, Path, Trust *string
}

// Empty reports whether the patch names no field.
func (p RepoPatch) Empty() bool {
	return p.Source == nil && p.Ref == nil && p.Path == nil && p.Trust == nil
}

// UpdateRepository changes fields of a repository of kind plugins in the user file. The
// result must be valid; entries of the system file and of profiles cannot be changed
// here. The repository is not synced. It reports whether the file changed.
func (st *State) UpdateRepository(name string, p RepoPatch) (bool, error) {
	e, err := st.Repository(name)
	if err != nil {
		return false, err
	}
	if e.Origin != repo.OriginUser {
		return false, &config.Error{Code: config.CodeConflict, Key: name, Detail: "the repository comes from " + e.Origin + " and can only be changed there"}
	}
	if !e.Supported() {
		return false, &config.Error{Code: config.CodeConflict, Key: name, Detail: "repositories of the kind " + e.Kind + " are not supported yet"}
	}
	next := map[string]string{"source": e.Source, "ref": e.Ref, "path": e.Path}
	if e.Trust != repo.TrustSignature {
		next["trust"] = e.Trust
	}
	var set [][2]any
	var remove []string
	for _, f := range []struct {
		key string
		val *string
	}{{"source", p.Source}, {"ref", p.Ref}, {"path", p.Path}, {"trust", p.Trust}} {
		if f.val == nil || *f.val == next[f.key] || (f.key == "trust" && *f.val == "" && next[f.key] == repo.TrustPrompt) {
			continue
		}
		next[f.key] = *f.val
		if *f.val == "" && f.key != "source" {
			remove = append(remove, f.key)
		} else {
			set = append(set, [2]any{f.key, *f.val})
		}
	}
	if next["trust"] == "" {
		next["trust"] = repo.TrustPrompt
	}
	if err := repo.ValidateNew(name, next["source"], next["ref"], next["path"], next["trust"]); err != nil {
		return false, err
	}
	if len(set) == 0 && len(remove) == 0 {
		return false, nil
	}
	if err := config.PatchRepository(st.Validator, st.UserPath, name, set, remove); err != nil {
		return false, err
	}
	return true, nil
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
