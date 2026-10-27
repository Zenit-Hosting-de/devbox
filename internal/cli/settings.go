package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/Zenit-Hosting-de/devbox/internal/config"
	"github.com/Zenit-Hosting-de/devbox/internal/plugin"
)

// defaultPluginPath is used when no configuration file sets plugins.paths.
const defaultPluginPath = "${xdg:data}/devbox/plugins"

// state is the loaded configuration with the plugins that define the settings.
type state struct {
	env      *env
	system   *config.File
	user     *config.File
	defs     config.Definitions
	layers   []config.Layer // without env and call
	result   *config.Result
	plugins  []*plugin.Manifest // winning valid manifests
	cands    []plugin.Candidate
	profiles []*config.File // in layer order
}

// load reads system and user configuration, profiles and plugin manifests and
// resolves the settings. withCall adds the environment and --set layers.
func (a *app) load(withCall bool) (*state, error) {
	e, err := a.env()
	if err != nil {
		return nil, err
	}
	st := &state{env: e}
	if st.system, err = config.ReadFile(e.validator, "config", e.systemPath, true); err != nil {
		return nil, err
	}
	if st.user, err = config.ReadFile(e.validator, "config", e.userPath, true); err != nil {
		return nil, err
	}

	paths, err := pluginPaths(e.ctx, st.system, st.user)
	if err != nil {
		return nil, err
	}
	cands, err := plugin.Search(e.validator, paths)
	if err != nil {
		return nil, err
	}
	st.cands = cands
	st.plugins = plugin.Winners(cands)
	st.defs = config.Definitions{}
	for _, m := range st.plugins {
		for key, field := range m.Settings() {
			st.defs[key] = field
		}
	}

	st.layers = append(st.layers, config.SettingsLayer(config.LayerSystem, st.system))
	for _, f := range []*config.File{st.system, st.user} {
		profiles, err := loadProfiles(e, f)
		if err != nil {
			return nil, err
		}
		for _, pf := range profiles {
			st.profiles = append(st.profiles, pf)
			st.layers = append(st.layers, config.ProfileLayer(pf))
		}
	}
	st.layers = append(st.layers, config.SettingsLayer(config.LayerUser, st.user))

	layers := st.layers
	if withCall {
		envLayer, err := config.EnvLayer(a.io.Environ(), st.defs)
		if err != nil {
			return nil, err
		}
		callLayer, err := config.CallLayer(a.sets)
		if err != nil {
			return nil, err
		}
		layers = append(append(layers[:len(layers):len(layers)], envLayer), callLayer)
	}
	if st.result, err = config.Resolve(st.defs, layers, e.ctx); err != nil {
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

func loadProfiles(e *env, f *config.File) ([]*config.File, error) {
	entries, _ := f.Plain()["profiles"].([]any)
	var out []*config.File
	for _, raw := range entries {
		entry, _ := raw.(map[string]any)
		source, _ := entry["source"].(string)
		rel, _ := entry["path"].(string)
		path, err := config.ProfilePath(e.ctx, source, rel)
		if err != nil {
			return nil, err
		}
		pf, err := config.ReadFile(e.validator, "profile", path, false)
		if err != nil {
			return nil, err
		}
		out = append(out, pf)
	}
	return out, nil
}

// evaluate determines the state of every found plugin.
func (a *app) evaluate(st *state) []*plugin.Plugin {
	env := plugin.Env{
		Version: a.build.Version,
		OS:      runtime.GOOS,
		Arch:    st.env.ctx.Host.Arch,
		Distro:  st.env.ctx.Host.Distro,
		LookPath: func(name string) (string, error) {
			return lookPath(name, a.io.Getenv("PATH"))
		},
		Disabled: disabledSources(st),
		MissingSettings: func(m *plugin.Manifest) []string {
			var missing []string
			for key, field := range m.Settings() {
				if _, hasDefault := field["default"]; hasDefault {
					continue
				}
				if s, ok := st.result.Get(key); !ok || s.Value == nil {
					missing = append(missing, key)
				}
			}
			sort.Strings(missing)
			return missing
		},
	}
	return plugin.Evaluate(st.cands, env)
}

// disabledSources merges plugins.disabled of the system file, plugins.disable and
// plugins.enable of the profiles and plugins.disabled of the user file, in layer order.
func disabledSources(st *state) func(string) string {
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
	fromConfig(st.system, config.LayerSystem)
	for _, pf := range st.profiles {
		doc := pf.Plain()
		name := config.LayerProfilePrefix + fmt.Sprint(doc["id"])
		plugins, _ := doc["plugins"].(map[string]any)
		for _, id := range anyStrings(plugins["disable"]) {
			src[id] = name
		}
		for _, id := range anyStrings(plugins["enable"]) {
			delete(src, id)
		}
	}
	fromConfig(st.user, config.LayerUser)
	return func(id string) string { return src[id] }
}

func anyStrings(v any) []string {
	list, _ := v.([]any)
	out := make([]string, 0, len(list))
	for _, e := range list {
		if s, ok := e.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// lookPath looks a command up in the given PATH (the environment of the call, not of
// the process, so tests and the service see the same result).
func lookPath(name, pathEnv string) (string, error) {
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
	info, err := os.Stat(p)
	if err != nil {
		return err
	}
	if info.IsDir() || info.Mode().Perm()&0o111 == 0 {
		return fmt.Errorf("%s: not executable", p)
	}
	return nil
}
