// Package config merges the configuration layers into effective settings, resolves
// placeholders and tracks where every value comes from (docs/concept.md, section
// "Konfiguration").
//
// Layers, later wins: plugin default, system file, profiles (in order), user file,
// environment (DEVBOX_SET__<PLUGIN>__<NAME>) and the call (--set key=value). A profile
// can lock a key; later layers then cannot override it.
package config

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/Zenit-Hosting-de/devbox/internal/platform"
	"github.com/Zenit-Hosting-de/devbox/internal/schema"
)

// Layer names. Profiles are named LayerProfilePrefix + profile id.
const (
	LayerPlugin        = "plugin"
	LayerSystem        = "system"
	LayerProfilePrefix = "profile:"
	LayerUser          = "user"
	LayerEnv           = "env"
	LayerCall          = "call"
)

// Error codes, shared with the API error format (api/openapi.json, Problem.code).
const (
	CodeValidation    = "validation"
	CodeNotFound      = "not-found"
	CodeSettingLocked = "setting-locked"
)

// Error is a configuration error with a stable code.
type Error struct {
	Code   string
	Key    string // setting key, if the error is about one
	Detail string
}

func (e *Error) Error() string {
	if e.Key == "" {
		return e.Code + ": " + e.Detail
	}
	return e.Code + ": " + e.Key + ": " + e.Detail
}

// IsCode reports whether err is an *Error with the code.
func IsCode(err error, code string) bool {
	var e *Error
	return errors.As(err, &e) && e.Code == code
}

// Definitions maps a fully qualified setting key to its field (plugin.v1.json#/$defs/field).
type Definitions map[string]map[string]any

// Layer holds the values one configuration layer sets.
type Layer struct {
	Name   string
	Values map[string]any
	// Locked lists keys a profile locks.
	Locked map[string]bool
}

// LayerValue is the value of a setting in one layer, before placeholders are resolved.
type LayerValue struct {
	Layer  string `json:"layer"`
	Value  any    `json:"value"`
	Locked bool   `json:"locked,omitempty"`
}

// Setting is the effective state of one setting.
type Setting struct {
	Key    string
	Value  any    // effective value, placeholders resolved
	Origin string // layer the effective value comes from
	Locked bool
	// Layers lists every layer that sets the key, in rank order. A value ignored
	// because of a lock is listed too.
	Layers []LayerValue
}

// Context provides the values for placeholders other than settings.
type Context struct {
	Getenv platform.Getenv
	XDG    platform.Dirs
	Host   platform.Host
	// Credential returns a non-secret field of a credential instance, for
	// ${<type>:<instance>.<field>}; nil while credentials are not available.
	Credential func(typ, instance, field string) (any, error)
}

// Result is the outcome of Resolve.
type Result struct {
	settings map[string]*Setting
	// Warnings lists keys set in files or profiles that no installed plugin defines.
	Warnings []string
}

// Keys returns all setting keys, sorted.
func (r *Result) Keys() []string {
	keys := make([]string, 0, len(r.settings))
	for k := range r.settings {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// Get returns one setting.
func (r *Result) Get(key string) (*Setting, bool) {
	s, ok := r.settings[key]
	return s, ok
}

// Plugin returns the effective values of one plugin, keyed by the short name, as
// the request file of the plugin protocol expects them.
func (r *Result) Plugin(id string) map[string]any {
	out := map[string]any{}
	prefix := id + "."
	for k, s := range r.settings {
		if name, ok := strings.CutPrefix(k, prefix); ok {
			out[name] = s.Value
		}
	}
	return out
}

var keyPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{1,31}\.[a-z][A-Za-z0-9]{0,47}$`)

// ValidKey reports whether key has the form <plugin>.<name>.
func ValidKey(key string) bool { return keyPattern.MatchString(key) }

// Resolve merges the layers (without the plugin layer, which comes from defs) and
// returns the effective settings.
func Resolve(defs Definitions, layers []Layer, ctx Context) (*Result, error) {
	r := &resolver{defs: defs, ctx: ctx, raw: map[string]*Setting{}, done: map[string]bool{}}
	res := &Result{settings: r.raw}

	for key, field := range defs {
		s := &Setting{Key: key, Origin: LayerPlugin}
		if def, ok := field["default"]; ok {
			s.Value = def
			s.Layers = append(s.Layers, LayerValue{Layer: LayerPlugin, Value: def})
		}
		r.raw[key] = s
	}
	for _, l := range layers {
		for _, key := range sortedKeys(l.Values) {
			value := l.Values[key]
			s, ok := r.raw[key]
			if !ok {
				if l.Name == LayerEnv || l.Name == LayerCall {
					return nil, &Error{Code: CodeNotFound, Key: key, Detail: "no plugin defines this setting (" + l.Name + ")"}
				}
				res.Warnings = append(res.Warnings, fmt.Sprintf("%s: %s: no plugin defines this setting", l.Name, key))
				continue
			}
			if l.Name == LayerEnv || l.Name == LayerCall {
				coerced, err := Coerce(defs[key], value)
				if err != nil {
					return nil, &Error{Code: CodeValidation, Key: key, Detail: err.Error()}
				}
				value = coerced
			}
			lv := LayerValue{Layer: l.Name, Value: value, Locked: l.Locked[key]}
			s.Layers = append(s.Layers, lv)
			if s.Locked {
				if l.Name == LayerEnv || l.Name == LayerCall {
					return nil, lockedError(s)
				}
				continue // a lock wins over later files and profiles
			}
			s.Value, s.Origin = value, l.Name
			if lv.Locked {
				s.Locked = true
			}
		}
	}
	for _, key := range res.Keys() {
		if _, err := r.resolve(key, nil); err != nil {
			return nil, err
		}
	}
	return res, nil
}

func lockedError(s *Setting) *Error {
	return &Error{Code: CodeSettingLocked, Key: s.Key, Detail: "locked by " + s.Origin}
}

// LockedBy returns the layer that locks the key, or "" when it is not locked.
func (r *Result) LockedBy(key string) string {
	if s, ok := r.settings[key]; ok && s.Locked {
		return s.Origin
	}
	return ""
}

type resolver struct {
	defs Definitions
	ctx  Context
	raw  map[string]*Setting
	done map[string]bool
}

// resolve expands the placeholders of one setting and validates the result. stack
// holds the keys being resolved, to report cycles with their path.
func (r *resolver) resolve(key string, stack []string) (any, error) {
	s := r.raw[key]
	if r.done[key] {
		return s.Value, nil
	}
	for i, k := range stack {
		if k == key {
			path := append(append([]string(nil), stack[i:]...), key)
			return nil, &Error{Code: CodeValidation, Key: stack[0], Detail: "placeholder cycle: " + strings.Join(path, " -> ")}
		}
	}
	stack = append(stack, key)
	v, err := ExpandValue(s.Value, func(ref string) (any, error) { return r.lookup(ref, stack) })
	if err != nil {
		var e *Error
		if errors.As(err, &e) {
			return nil, err
		}
		return nil, &Error{Code: CodeValidation, Key: key, Detail: err.Error()}
	}
	if s.Value != nil || hasDefault(r.defs[key]) {
		if err := validateValue(key, r.defs[key], v); err != nil {
			return nil, err
		}
	}
	s.Value = v
	r.done[key] = true
	return v, nil
}

func hasDefault(field map[string]any) bool {
	_, ok := field["default"]
	return ok
}

func validateValue(key string, field map[string]any, v any) error {
	frag, err := schema.CompileField(field)
	if err != nil {
		return &Error{Code: CodeValidation, Key: key, Detail: "invalid field definition: " + err.Error()}
	}
	if err := frag.Validate(key, v); err != nil {
		return &Error{Code: CodeValidation, Key: key, Detail: err.Error()}
	}
	return nil
}

func (r *resolver) lookup(ref string, stack []string) (any, error) {
	if ValidKey(ref) {
		if _, ok := r.raw[ref]; !ok {
			return nil, &Error{Code: CodeNotFound, Key: stack[len(stack)-1], Detail: "placeholder ${" + ref + "}: unknown setting"}
		}
		return r.resolve(ref, stack)
	}
	return ContextLookup(r.ctx, ref)
}

// ContextLookup resolves placeholders that do not refer to settings: ${env:…},
// ${xdg:…}, ${host:…} and ${<type>:<instance>.<field>}.
func ContextLookup(ctx Context, ref string) (any, error) {
	kind, name, ok := strings.Cut(ref, ":")
	if !ok {
		return nil, fmt.Errorf("unknown placeholder ${%s}", ref)
	}
	switch kind {
	case "env":
		if ctx.Getenv == nil {
			return "", nil
		}
		return ctx.Getenv(name), nil
	case "xdg":
		switch name {
		case "config":
			return ctx.XDG.Config, nil
		case "data":
			return ctx.XDG.Data, nil
		case "state":
			return ctx.XDG.State, nil
		case "cache":
			return ctx.XDG.Cache, nil
		}
	case "host":
		switch name {
		case "name":
			return ctx.Host.Name, nil
		case "user":
			return ctx.Host.User, nil
		case "arch":
			return ctx.Host.Arch, nil
		case "distro":
			return ctx.Host.Distro, nil
		case "codename":
			return ctx.Host.Codename, nil
		}
	default:
		instance, field, ok := strings.Cut(name, ".")
		if ok && ctx.Credential != nil {
			return ctx.Credential(kind, instance, field)
		}
		if ok {
			return nil, fmt.Errorf("placeholder ${%s}: credentials are not available", ref)
		}
	}
	return nil, fmt.Errorf("unknown placeholder ${%s}", ref)
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
