package cli

import (
	"fmt"

	"github.com/Zenit-Hosting-de/devbox/internal/config"
	"github.com/Zenit-Hosting-de/devbox/internal/plugin"
)

// defaultPluginPath is used when no configuration file sets plugins.paths.
const defaultPluginPath = "${xdg:data}/devbox/plugins"

// state is the loaded configuration with the plugins that define the settings.
type state struct {
	env     *env
	system  *config.File
	user    *config.File
	defs    config.Definitions
	layers  []config.Layer // without env and call
	result  *config.Result
	plugins []*plugin.Manifest
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
	st.plugins, _ = plugin.FirstByID(cands)
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
		st.layers = append(st.layers, profiles...)
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

func loadProfiles(e *env, f *config.File) ([]config.Layer, error) {
	entries, _ := f.Plain()["profiles"].([]any)
	var layers []config.Layer
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
		layers = append(layers, config.ProfileLayer(pf))
	}
	return layers, nil
}
