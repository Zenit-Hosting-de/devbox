package core

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/Zenit-Hosting-de/devbox/internal/config"
	"github.com/Zenit-Hosting-de/devbox/internal/plugin"
	"github.com/Zenit-Hosting-de/devbox/internal/repo"
	bundled "github.com/Zenit-Hosting-de/devbox/plugins"
)

// defaultPluginPath is used when no configuration file sets plugins.paths.
const defaultPluginPath = "${xdg:data}/devbox/plugins"

// State is the loaded configuration with the plugins that define the settings.
type State struct {
	*Base
	Build Build
	Env   Env

	System *config.File
	User   *config.File
	Defs   config.Definitions
	Result *config.Result
	// Manifests are the winning valid manifests.
	Manifests  []*plugin.Manifest
	Candidates []plugin.Candidate
	// Profiles are the loaded profile files in layer order.
	Profiles []*config.File
	// Repositories are the merged entries of the repositories key of the system file,
	// the profiles and the user file, sorted by name.
	Repositories []repo.Entry
	// Repos syncs the repositories and reads their state.
	Repos *repo.Manager

	layers []config.Layer // without env and call
}

// Load reads system and user configuration, profiles and plugin manifests and
// resolves the settings. withCall adds the environment and --set layers.
func Load(env Env, build Build, withCall bool) (*State, error) {
	b, err := NewBase(env)
	if err != nil {
		return nil, err
	}
	st := &State{Base: b, Build: build, Env: env}
	if st.System, err = config.ReadFile(b.Validator, "config", b.SystemPath, true); err != nil {
		return nil, err
	}
	if st.User, err = config.ReadFile(b.Validator, "config", b.UserPath, true); err != nil {
		return nil, err
	}

	for _, f := range []*config.File{st.System, st.User} {
		profiles, err := loadProfiles(b, f)
		if err != nil {
			return nil, err
		}
		st.Profiles = append(st.Profiles, profiles...)
	}
	st.Repositories = st.collectRepositories()
	st.Repos = st.newRepoManager()

	paths, err := pluginPaths(b.Ctx, st.System, st.User)
	if err != nil {
		return nil, err
	}
	roots := make([]plugin.Root, 0, len(paths))
	for _, p := range paths {
		roots = append(roots, plugin.Root{Dir: p, Origin: plugin.Origin{Kind: plugin.OriginPath, Path: p}})
	}
	// plugins from repositories come after plugins.paths and before the bundled ones
	roots = append(roots, st.Repos.Roots(st.Repositories)...)
	builtin, err := plugin.ExtractBuiltin(bundled.FS, filepath.Join(b.Ctx.XDG.Data, "devbox", "builtin"), build.Version)
	if err != nil {
		return nil, err
	}
	// bundled plugins come last, so others can replace them
	roots = append(roots, plugin.Root{Dir: builtin, Origin: plugin.Origin{Kind: plugin.OriginBuiltin}})
	cands, err := plugin.SearchRoots(b.Validator, roots)
	if err != nil {
		return nil, err
	}
	st.Candidates = cands
	st.Manifests = plugin.Winners(cands)
	st.Defs = config.Definitions{}
	for _, m := range st.Manifests {
		for key, field := range m.Settings() {
			st.Defs[key] = field
		}
	}

	st.layers = append(st.layers, config.SettingsLayer(config.LayerSystem, st.System))
	for _, pf := range st.Profiles {
		st.layers = append(st.layers, config.ProfileLayer(pf))
	}
	st.layers = append(st.layers, config.SettingsLayer(config.LayerUser, st.User))

	layers := st.layers
	if withCall {
		envLayer, err := config.EnvLayer(env.Environ(), st.Defs)
		if err != nil {
			return nil, err
		}
		callLayer, err := config.CallLayer(env.Sets)
		if err != nil {
			return nil, err
		}
		layers = append(append(layers[:len(layers):len(layers)], envLayer), callLayer)
	}
	if st.Result, err = config.Resolve(st.Defs, layers, b.Ctx); err != nil {
		return nil, err
	}
	return st, nil
}

func pluginPaths(ctx config.Context, files ...*config.File) ([]string, error) {
	raw := []any{defaultPluginPath}
	for _, f := range files {
		if plugins, ok := f.Plain()["plugins"].(map[string]any); ok {
			if paths, ok := plugins["paths"].([]any); ok {
				raw = paths // the user file overrides the system file
			}
		}
	}
	var out []string
	for _, p := range raw {
		s, _ := p.(string)
		v, err := config.Expand(s, func(ref string) (any, error) { return config.ContextLookup(ctx, ref) })
		if err != nil {
			return nil, &config.Error{Code: config.CodeValidation, Detail: fmt.Sprintf("plugins.paths: %v", err)}
		}
		str, _ := v.(string)
		out = append(out, str)
	}
	return out, nil
}

func loadProfiles(b *Base, f *config.File) ([]*config.File, error) {
	entries, _ := f.Plain()["profiles"].([]any)
	var out []*config.File
	for _, raw := range entries {
		entry, _ := raw.(map[string]any)
		source, _ := entry["source"].(string)
		rel, _ := entry["path"].(string)
		path, err := config.ProfilePath(b.Ctx, source, rel)
		if err != nil {
			return nil, err
		}
		pf, err := config.ReadFile(b.Validator, "profile", path, false)
		if err != nil {
			return nil, err
		}
		out = append(out, pf)
	}
	return out, nil
}

// Evaluate determines the state of every found plugin.
func (st *State) Evaluate() []*plugin.Plugin {
	env := plugin.Env{
		Version: st.Build.Version,
		OS:      runtime.GOOS,
		Arch:    st.Ctx.Host.Arch,
		Distro:  st.Ctx.Host.Distro,
		LookPath: func(name string) (string, error) {
			return LookPath(name, st.Env.Getenv("PATH"))
		},
		Disabled: st.disabledSources(true),
		MissingSettings: func(m *plugin.Manifest) []string {
			var missing []string
			for key, field := range m.Settings() {
				if _, hasDefault := field["default"]; hasDefault {
					continue
				}
				if s, ok := st.Result.Get(key); !ok || s.Value == nil {
					missing = append(missing, key)
				}
			}
			sort.Strings(missing)
			return missing
		},
	}
	return plugin.Evaluate(st.Candidates, env)
}

// disabledSources merges plugins.disabled of the system file, plugins.disable and
// plugins.enable of the profiles and plugins.disabled of the user file, in layer order.
// Without the user file it tells what keeps a plugin off apart from the user's choice.
func (st *State) disabledSources(withUser bool) func(string) string {
	src := map[string]string{}
	fromConfig := func(f *config.File, name string) {
		plugins, _ := f.Plain()["plugins"].(map[string]any)
		ids, _ := plugins["disabled"].([]any)
		for _, id := range ids {
			if s, ok := id.(string); ok {
				src[s] = name
			}
		}
	}
	fromConfig(st.System, config.LayerSystem)
	for _, pf := range st.Profiles {
		doc := pf.Plain()
		name := config.LayerProfilePrefix + fmt.Sprint(doc["id"])
		plugins, _ := doc["plugins"].(map[string]any)
		for _, id := range AnyStrings(plugins["disable"]) {
			src[id] = name
		}
		for _, id := range AnyStrings(plugins["enable"]) {
			delete(src, id)
		}
	}
	if withUser {
		fromConfig(st.User, config.LayerUser)
	}
	return func(id string) string { return src[id] }
}

// AnyStrings returns the strings of a JSON array.
func AnyStrings(v any) []string {
	list, _ := v.([]any)
	out := make([]string, 0, len(list))
	for _, e := range list {
		if s, ok := e.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// FindPlugin returns the plugin with the ID, or nil.
func FindPlugin(plugins []*plugin.Plugin, id string) *plugin.Plugin {
	for _, p := range plugins {
		if p.ID == id {
			return p
		}
	}
	return nil
}

// LookPath looks a command up in the given PATH (the environment of the call, not of
// the process, so tests and the service see the same result).
func LookPath(name, pathEnv string) (string, error) {
	if strings.Contains(name, "/") {
		return name, statExecutable(name)
	}
	for _, dir := range filepath.SplitList(pathEnv) {
		if dir == "" {
			continue
		}
		p := filepath.Join(dir, name)
		if statExecutable(p) == nil {
			return p, nil
		}
	}
	return "", fmt.Errorf("%s: not found in PATH", name)
}

func statExecutable(p string) error {
	info, err := os.Stat(p) //nolint:gosec // looks up commands in the user's PATH on purpose
	if err != nil {
		return err
	}
	if info.IsDir() || info.Mode().Perm()&0o111 == 0 {
		return fmt.Errorf("%s: not executable", p)
	}
	return nil
}
