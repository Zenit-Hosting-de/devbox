package core

import (
	"fmt"
	"sort"
	"time"

	"github.com/Zenit-Hosting-de/devbox/internal/config"
	"github.com/Zenit-Hosting-de/devbox/internal/plugin"
	"github.com/Zenit-Hosting-de/devbox/internal/runner"
	"github.com/Zenit-Hosting-de/devbox/internal/schema"
)

// Lookup resolves placeholders in defaults: settings and context values.
func (st *State) Lookup() config.Lookup {
	return func(r string) (any, error) {
		if config.ValidKey(r) {
			if s, ok := st.Result.Get(r); ok {
				return s.Value, nil
			}
			return nil, fmt.Errorf("unknown setting %s", r)
		}
		return config.ContextLookup(st.Ctx, r)
	}
}

// ActionDef returns the action of a plugin from its manifest.
func ActionDef(p *plugin.Plugin, name string) (map[string]any, bool) {
	if p == nil || p.Manifest == nil {
		return nil, false
	}
	actions, _ := p.Manifest.Doc["actions"].(map[string]any)
	action, ok := actions[name].(map[string]any)
	return action, ok
}

// ResolveParams checks the given parameter values (typed JSON values), fills in the
// defaults with placeholders resolved against the settings and validates every value
// against its field. Errors are config.Error with the key ref#name.
func (st *State) ResolveParams(action map[string]any, ref string, values map[string]any) (map[string]any, error) {
	fields, _ := action["params"].(map[string]any)
	out := map[string]any{}
	given := make([]string, 0, len(values))
	for name := range values {
		given = append(given, name)
	}
	sort.Strings(given)
	for _, name := range given {
		if _, known := fields[name].(map[string]any); !known {
			return nil, ErrInvalid(ref+"#"+name, "unknown parameter")
		}
		out[name] = values[name]
	}
	lookup := st.Lookup()
	names := make([]string, 0, len(fields))
	for name := range fields {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		field, _ := fields[name].(map[string]any)
		if _, set := out[name]; !set {
			def, ok := field["default"]
			if !ok {
				return nil, ErrInvalid(ref+"#"+name, "parameter required")
			}
			v, err := config.ExpandValue(def, lookup)
			if err != nil {
				return nil, ErrInvalid(ref+"#"+name, err.Error())
			}
			out[name] = v
		}
		frag, err := schema.CompileField(field)
		if err != nil {
			return nil, err
		}
		if err := frag.Validate(ref+"#"+name, out[name]); err != nil {
			return nil, ErrInvalid(ref+"#"+name, err.Error())
		}
	}
	return out, nil
}

// ResolvedDefaults returns the defaults of all action parameters of a manifest with
// placeholders resolved, keyed by action and parameter name. A parameter whose
// default cannot be resolved is left out; so is an action without any.
func (st *State) ResolvedDefaults(m *plugin.Manifest) map[string]map[string]any {
	out := map[string]map[string]any{}
	if m == nil {
		return out
	}
	lookup := st.Lookup()
	actions, _ := m.Doc["actions"].(map[string]any)
	for name, a := range actions {
		action, _ := a.(map[string]any)
		fields, _ := action["params"].(map[string]any)
		for pname, f := range fields {
			field, _ := f.(map[string]any)
			def, ok := field["default"]
			if !ok {
				continue
			}
			v, err := config.ExpandValue(def, lookup)
			if err != nil {
				continue
			}
			if out[name] == nil {
				out[name] = map[string]any{}
			}
			out[name][pname] = v
		}
	}
	return out
}

// ActionSpec builds the run specification of an action of a ready plugin: timeout,
// locks, settings, host, language, proxy variables and a new session ID. It also
// returns the title (the label of the action). The caller sets terminal size and input.
func (st *State) ActionSpec(p *plugin.Plugin, name string, params map[string]any, dryRun, yes bool) (runner.Spec, string, error) {
	ref := p.ID + "." + name
	action, ok := ActionDef(p, name)
	if !ok {
		return runner.Spec{}, "", ErrNotFound(ref, "no such action")
	}
	spec := runner.Spec{
		Kind:      runner.KindAction,
		SessionID: runner.NewSessionID(time.Now()),
		PluginID:  p.ID,
		PluginDir: p.Dir,
		Name:      name,
		Command:   AnyStrings(action["run"]),
		Params:    params,
		Settings:  st.Result.Plugin(p.ID),
		DryRun:    dryRun,
		Yes:       yes,
		Lang:      st.UILanguage(),
		Host:      st.Host(),
		Locks:     AnyStrings(action["locks"]),
		ExtraEnv:  st.ProxyEnv(),
	}
	if t, ok := action["timeout"].(string); ok {
		var err error
		if spec.Timeout, err = time.ParseDuration(t); err != nil {
			return runner.Spec{}, "", ErrInvalid(ref, "timeout: "+err.Error())
		}
	}
	title, _ := action["label"].(string)
	return spec, title, nil
}
