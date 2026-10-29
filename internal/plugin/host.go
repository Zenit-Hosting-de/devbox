package plugin

import (
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Zenit-Hosting-de/devbox/internal/semver"
)

// State of a plugin (docs/concept.md, "Plugin-System").
type State string

// Plugin states. Only ready plugins offer actions.
const (
	StateInvalid    State = "invalid"
	StateBlocked    State = "blocked"
	StateDisabled   State = "disabled"
	StateIncomplete State = "incomplete"
	StateReady      State = "ready"
)

// Reason codes explain a state. They are stable identifiers; frontends translate
// them via the language packs. Detail carries the subject (command, plugin, key …).
const (
	ReasonManifestInvalid  = "manifest-invalid"
	ReasonDisabledBy       = "disabled"
	ReasonRequiresDevbox   = "requires-devbox"
	ReasonRequiresOS       = "requires-os"
	ReasonRequiresArch     = "requires-arch"
	ReasonRequiresDistro   = "requires-distro"
	ReasonRequiresCommand  = "requires-command"
	ReasonRequiresPlugin   = "requires-plugin"
	ReasonDependencyCycle  = "dependency-cycle"
	ReasonSettingMissing   = "setting-missing"
	ReasonShadowsDuplicate = "duplicate-id"
)

// Reason explains why a plugin is not ready, or notes something about it.
type Reason struct {
	Code   string `json:"code"`
	Detail string `json:"detail,omitempty"`
}

// Plugin is a found plugin with its evaluated state.
type Plugin struct {
	ID       string
	Dir      string
	Manifest *Manifest // nil when invalid
	State    State
	Reasons  []Reason
	Origin   Origin
	// Shadowed lists directories of later plugins with the same ID, which this one
	// replaces (first found wins).
	Shadowed []string
}

// Env is what a plugin's requirements are checked against.
type Env struct {
	Version  string // DevBox version
	OS       string
	Arch     string
	Distro   string
	LookPath func(string) (string, error)
	// Disabled names the source that disables a plugin ("config", "profile:<id>"), or "".
	Disabled func(id string) string
	// MissingSettings lists required settings of a plugin that have no value.
	MissingSettings func(m *Manifest) []string
}

// Evaluate turns search candidates into plugins with states. The first candidate of
// an ID wins; later ones are recorded as shadowed. Order of the result is by ID.
func Evaluate(cands []Candidate, env Env) []*Plugin {
	byID := map[string]*Plugin{}
	var all []*Plugin
	for _, c := range cands {
		id := filepath.Base(c.Dir)
		if c.Manifest != nil {
			id = c.Manifest.ID
		}
		if first, ok := byID[id]; ok {
			first.Shadowed = append(first.Shadowed, c.Dir)
			continue
		}
		p := &Plugin{ID: id, Dir: c.Dir, Manifest: c.Manifest, Origin: c.Origin}
		if c.Manifest == nil {
			p.State = StateInvalid
			detail := ""
			if c.Err != nil {
				detail = c.Err.Error()
			}
			p.Reasons = []Reason{{Code: ReasonManifestInvalid, Detail: detail}}
		}
		byID[id] = p
		all = append(all, p)
	}
	sort.Slice(all, func(i, j int) bool { return all[i].ID < all[j].ID })

	visiting := map[string]bool{}
	var eval func(p *Plugin) State
	eval = func(p *Plugin) State {
		if p.State != "" {
			return p.State
		}
		if visiting[p.ID] {
			return StateBlocked
		}
		visiting[p.ID] = true
		defer delete(visiting, p.ID)

		if src := callString(env.Disabled, p.ID); src != "" {
			p.State, p.Reasons = StateDisabled, []Reason{{Code: ReasonDisabledBy, Detail: src}}
			return p.State
		}
		reasons := requirements(p.Manifest, env)
		for _, dep := range sortedDeps(p.Manifest) {
			d, ok := byID[dep.id]
			switch {
			case !ok:
				reasons = append(reasons, Reason{Code: ReasonRequiresPlugin, Detail: dep.id + " " + dep.rng})
			case visiting[dep.id]:
				reasons = append(reasons, Reason{Code: ReasonDependencyCycle, Detail: dep.id})
			case eval(d) != StateReady:
				reasons = append(reasons, Reason{Code: ReasonRequiresPlugin, Detail: dep.id + " (" + string(d.State) + ")"})
			default:
				if !versionIn(d.Manifest.Doc["version"], dep.rng) {
					reasons = append(reasons, Reason{Code: ReasonRequiresPlugin, Detail: dep.id + " " + dep.rng})
				}
			}
		}
		if len(reasons) > 0 {
			p.State, p.Reasons = StateBlocked, reasons
			return p.State
		}
		if env.MissingSettings != nil {
			if missing := env.MissingSettings(p.Manifest); len(missing) > 0 {
				p.State = StateIncomplete
				for _, k := range missing {
					p.Reasons = append(p.Reasons, Reason{Code: ReasonSettingMissing, Detail: k})
				}
				return p.State
			}
		}
		p.State = StateReady
		return p.State
	}
	for _, p := range all {
		eval(p)
		if len(p.Shadowed) > 0 {
			p.Reasons = append(p.Reasons, Reason{Code: ReasonShadowsDuplicate, Detail: strings.Join(p.Shadowed, ", ")})
		}
	}
	return all
}

func callString(f func(string) string, id string) string {
	if f == nil {
		return ""
	}
	return f(id)
}

func requirements(m *Manifest, env Env) []Reason {
	req, _ := m.Doc["requires"].(map[string]any)
	var reasons []Reason
	if rng, ok := req["devbox"].(string); ok && !isDevBuild(env.Version) && !versionIn(env.Version, rng) {
		reasons = append(reasons, Reason{Code: ReasonRequiresDevbox, Detail: rng})
	}
	check := func(key, have, code string) {
		list, ok := req[key].([]any)
		if !ok || len(list) == 0 {
			return
		}
		for _, v := range list {
			if v == have {
				return
			}
		}
		reasons = append(reasons, Reason{Code: code, Detail: joinAny(list)})
	}
	check("os", env.OS, ReasonRequiresOS)
	check("arch", env.Arch, ReasonRequiresArch)
	check("distro", env.Distro, ReasonRequiresDistro)
	cmds, _ := req["commands"].([]any)
	for _, c := range cmds {
		name, _ := c.(string)
		if env.LookPath == nil {
			continue
		}
		if _, err := env.LookPath(name); err != nil {
			reasons = append(reasons, Reason{Code: ReasonRequiresCommand, Detail: name})
		}
	}
	return reasons
}

// isDevBuild reports whether the version is a development build ("…-dev"), which
// satisfies every DevBox requirement so plugins can be developed before a release.
func isDevBuild(version string) bool { return strings.HasSuffix(version, "-dev") }

type dep struct{ id, rng string }

func sortedDeps(m *Manifest) []dep {
	req, _ := m.Doc["requires"].(map[string]any)
	plugins, _ := req["plugins"].(map[string]any)
	out := make([]dep, 0, len(plugins))
	for id, r := range plugins {
		rng, _ := r.(string)
		out = append(out, dep{id: id, rng: rng})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].id < out[j].id })
	return out
}

func versionIn(version any, rng string) bool {
	s, _ := version.(string)
	v, err := semver.Parse(s)
	if err != nil {
		return false
	}
	r, err := semver.ParseRange(rng)
	if err != nil {
		return false
	}
	return r.Match(v)
}

func joinAny(list []any) string {
	parts := make([]string, 0, len(list))
	for _, v := range list {
		if s, ok := v.(string); ok {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, ", ")
}

// Handler is a command a plugin registered for an event in "on".
type Handler struct {
	Plugin  *Plugin
	Command []string
}

// Handlers returns the commands of ready plugins registered for an event. A key in
// "on" may filter by subject: "session.finished:java.*" matches the event
// "session.finished" for subject "java.install".
func Handlers(plugins []*Plugin, event, subject string) []Handler {
	var out []Handler
	for _, p := range plugins {
		if p.State != StateReady {
			continue
		}
		on, _ := p.Manifest.Doc["on"].(map[string]any)
		keys := make([]string, 0, len(on))
		for k := range on {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, key := range keys {
			if !EventMatches(key, event, subject) {
				continue
			}
			raw, _ := on[key].([]any)
			cmd := make([]string, 0, len(raw))
			for _, a := range raw {
				s, _ := a.(string)
				cmd = append(cmd, s)
			}
			out = append(out, Handler{Plugin: p, Command: cmd})
		}
	}
	return out
}

// EventMatches reports whether a subscription key ("event" or "event:glob") matches
// an event with a subject.
func EventMatches(key, event, subject string) bool {
	name, pattern, filtered := strings.Cut(key, ":")
	if name != event {
		return false
	}
	if !filtered {
		return true
	}
	ok, err := path.Match(pattern, subject)
	return err == nil && ok
}

// Status is the JSON form of a plugin for `devbox plugin list --json` and
// GET /api/v1/plugins (api/openapi.json, PluginStatus).
type Status struct {
	ID       string   `json:"id"`
	Name     string   `json:"name,omitempty"`
	Version  string   `json:"version,omitempty"`
	Category string   `json:"category,omitempty"`
	State    State    `json:"state"`
	Reasons  []Reason `json:"reasons"`
	Path     string   `json:"path"`
	Origin   Origin   `json:"origin"`
	Shadowed []string `json:"shadowed,omitempty"`
}

// Status returns the JSON form.
func (p *Plugin) Status() Status {
	s := Status{ID: p.ID, State: p.State, Reasons: p.Reasons, Path: p.Dir, Origin: p.Origin, Shadowed: p.Shadowed}
	if s.Reasons == nil {
		s.Reasons = []Reason{}
	}
	if p.Manifest != nil {
		s.Name, _ = p.Manifest.Doc["name"].(string)
		s.Version, _ = p.Manifest.Doc["version"].(string)
		s.Category, _ = p.Manifest.Doc["category"].(string)
	}
	return s
}
