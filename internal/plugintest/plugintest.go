// Package plugintest implements `devbox plugin test`: it runs a plugin in a sandbox
// (a fresh HOME with its own XDG directories) and checks the rules of the concept for
// plugins: checks return valid JSON, a dry run changes no file, and a second run of
// an action reports no change.
package plugintest

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Zenit-Hosting-de/devbox/internal/config"
	"github.com/Zenit-Hosting-de/devbox/internal/jsonfile"
	"github.com/Zenit-Hosting-de/devbox/internal/platform"
	"github.com/Zenit-Hosting-de/devbox/internal/plugin"
	"github.com/Zenit-Hosting-de/devbox/internal/runner"
	"github.com/Zenit-Hosting-de/devbox/internal/schema"
	"github.com/Zenit-Hosting-de/devbox/internal/sdk"
)

// Step results.
const (
	StatusOK      = "ok"
	StatusFailed  = "failed"
	StatusSkipped = "skipped"
)

// Step is one test step.
type Step struct {
	Step   string `json:"step"` // validate, check, dry-run, apply, repeat
	Target string `json:"target"`
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
}

// Report is the outcome of Run.
type Report struct {
	Plugin string `json:"plugin"`
	Steps  []Step `json:"steps"`
}

// Failed reports whether a step failed.
func (r *Report) Failed() bool {
	for _, s := range r.Steps {
		if s.Status == StatusFailed {
			return true
		}
	}
	return false
}

// Options control Run.
type Options struct {
	Dir        string
	Apply      bool     // run non-dangerous actions for real, twice
	ChecksOnly bool     // only validate and run checks
	Set        []string // key=value settings for the sandbox
	Version    string
	Bin        string // devbox binary for DEVBOX_BIN
	Path       string // PATH for the plugin
	Host       platform.Host
	Validator  *schema.Validator
	Log        io.Writer // terminal output of actions (optional)
}

type sandbox struct {
	opts     Options
	manifest *plugin.Manifest
	settings map[string]any
	result   *config.Result
	runner   *runner.Runner
	home     string
	ctx      config.Context
	report   *Report
}

// Run tests the plugin in Options.Dir.
func Run(ctx context.Context, opts Options) (*Report, error) {
	m, problems := plugin.Validate(ctx, opts.Validator, opts.Dir, plugin.ValidateOptions{Shellcheck: true, PathEnv: opts.Path})
	rep := &Report{}
	if m != nil {
		rep.Plugin = m.ID
	}
	for _, p := range problems {
		rep.Steps = append(rep.Steps, Step{Step: "validate", Target: p.Code, Status: StatusFailed, Detail: p.Detail})
	}
	if m == nil {
		return rep, nil
	}
	if len(problems) == 0 {
		rep.Steps = append(rep.Steps, Step{Step: "validate", Target: m.ID, Status: StatusOK})
	}

	home, err := os.MkdirTemp("", "devbox-plugin-test-")
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.RemoveAll(home) }()
	s, err := newSandbox(opts, m, home, rep)
	if err != nil {
		return nil, err
	}
	for _, name := range s.order("checks") {
		s.check(ctx, name)
	}
	if opts.ChecksOnly {
		return rep, nil
	}
	actions := s.order("actions")
	for _, name := range actions {
		s.dryRun(ctx, name, false)
	}
	if opts.Apply {
		for _, name := range actions {
			s.applyTwice(ctx, name)
		}
		// Actions whose options come from check data can run now.
		for _, name := range actions {
			s.dryRun(ctx, name, true)
		}
	}
	return rep, nil
}

func newSandbox(opts Options, m *plugin.Manifest, home string, rep *Report) (*sandbox, error) {
	dirs, err := platform.XDG(func(k string) string {
		if k == "HOME" {
			return home
		}
		return ""
	})
	if err != nil {
		return nil, err
	}
	ctx := config.Context{Getenv: func(k string) string {
		if k == "HOME" {
			return home
		}
		return ""
	}, XDG: dirs, Host: opts.Host}
	call, err := config.CallLayer(opts.Set)
	if err != nil {
		return nil, err
	}
	res, err := config.Resolve(m.Settings(), []config.Layer{call}, ctx)
	if err != nil {
		return nil, err
	}
	env := runner.Env{
		Version: opts.Version, Bin: opts.Bin, Home: home, User: opts.Host.User, Path: opts.Path,
		Lang: "C.UTF-8", Term: "xterm-256color",
		RuntimeDir: filepath.Join(home, ".run"),
		StateDir:   filepath.Join(dirs.State, "devbox"),
		CacheDir:   filepath.Join(dirs.Cache, "devbox"),
	}
	return &sandbox{
		opts: opts, manifest: m, settings: res.Plugin(m.ID), result: res,
		runner: runner.New(env, opts.Validator), home: home, ctx: ctx, report: rep,
	}, nil
}

// order returns the names of a manifest section in file order.
func (s *sandbox) order(section string) []string {
	doc, err := jsonfile.Parse(s.manifest.Data)
	if err != nil {
		return nil
	}
	root, _ := doc.(*jsonfile.Object)
	sec, _ := root.Get(section)
	obj, ok := sec.(*jsonfile.Object)
	if !ok {
		return nil
	}
	return obj.Keys()
}

func (s *sandbox) add(step, target, status, detail string) {
	s.report.Steps = append(s.report.Steps, Step{Step: step, Target: s.manifest.ID + "." + target, Status: status, Detail: detail})
}

func (s *sandbox) host() map[string]any {
	h := s.opts.Host
	return map[string]any{"name": h.Name, "user": h.User, "distro": h.Distro, "distroVersion": h.DistroVersion, "arch": h.Arch}
}

func (s *sandbox) entry(section, name string) map[string]any {
	sec, _ := s.manifest.Doc[section].(map[string]any)
	e, _ := sec[name].(map[string]any)
	return e
}

func timeout(e map[string]any) time.Duration {
	if t, ok := e["timeout"].(string); ok {
		if d, err := time.ParseDuration(t); err == nil {
			return d
		}
	}
	return 0
}

func command(e map[string]any) []string {
	list, _ := e["run"].([]any)
	out := make([]string, 0, len(list))
	for _, a := range list {
		str, _ := a.(string)
		out = append(out, str)
	}
	return out
}

// runCheck runs a check and returns its result object.
func (s *sandbox) runCheck(ctx context.Context, name string) (*runner.Result, error) {
	e := s.entry("checks", name)
	return s.runner.Run(ctx, runner.Spec{
		Kind: runner.KindCheck, PluginID: s.manifest.ID, PluginDir: s.manifest.Dir, Name: name,
		Command: command(e), Settings: s.settings, Host: s.host(), Timeout: timeout(e),
	}, runner.Callbacks{})
}

func (s *sandbox) check(ctx context.Context, name string) {
	res, err := s.runCheck(ctx, name)
	switch {
	case err != nil:
		s.add("check", name, StatusFailed, err.Error())
	case res.State != runner.StateSuccess:
		s.add("check", name, StatusFailed, fmt.Sprintf("%s (exit %d) %s", res.State, res.ExitCode, strings.Join(res.Warnings, "; ")))
	default:
		s.add("check", name, StatusOK, fmt.Sprint(res.Check["status"]))
	}
}

// params builds the parameters of an action: defaults, or the first option of an
// optionsFrom reference. ok is false when a parameter has no value.
func (s *sandbox) params(ctx context.Context, action map[string]any) (map[string]any, string, bool) {
	fields, _ := action["params"].(map[string]any)
	out := map[string]any{}
	lookup := func(r string) (any, error) {
		if config.ValidKey(r) {
			if st, ok := s.result.Get(r); ok {
				return st.Value, nil
			}
		}
		return config.ContextLookup(s.ctx, r)
	}
	for _, name := range sortedKeys(fields) {
		field, _ := fields[name].(map[string]any)
		if def, ok := field["default"]; ok {
			v, err := config.ExpandValue(def, lookup)
			if err != nil {
				return nil, name + ": " + err.Error(), false
			}
			out[name] = v
			continue
		}
		if ref, ok := field["optionsFrom"].(string); ok {
			if v, found := s.firstOption(ctx, ref); found {
				out[name] = v
				continue
			}
			return nil, name + ": no options from " + ref, false
		}
		return nil, name + ": no default", false
	}
	return out, "", true
}

func (s *sandbox) firstOption(ctx context.Context, ref string) (any, bool) {
	check, pointer, _ := strings.Cut(ref, "#")
	_, name, _ := strings.Cut(check, ".")
	res, err := s.runCheck(ctx, name)
	if err != nil || res.Check == nil {
		return nil, false
	}
	data, err := jsonfile.Marshal(res.Check)
	if err != nil {
		return nil, false
	}
	values, err := sdk.JSONGet(data, pointer)
	if err != nil || len(values) == 0 {
		return nil, false
	}
	return values[0], true
}

type runOutcome struct {
	res     *runner.Result
	changes int
	err     error
}

func (s *sandbox) runAction(ctx context.Context, name string, params map[string]any, dryRun bool) runOutcome {
	e := s.entry("actions", name)
	out := s.opts.Log
	if out == nil {
		out = io.Discard
	}
	res, err := s.runner.Run(ctx, runner.Spec{
		Kind: runner.KindAction, SessionID: runner.NewSessionID(time.Now()),
		PluginID: s.manifest.ID, PluginDir: s.manifest.Dir, Name: name, Command: command(e),
		Params: params, Settings: s.settings, DryRun: dryRun, Yes: true, Host: s.host(), Timeout: timeout(e),
	}, runner.Callbacks{Output: out})
	if err != nil {
		return runOutcome{err: err}
	}
	return runOutcome{res: res, changes: res.Changes}
}

func (o runOutcome) failure() string {
	if o.err != nil {
		return o.err.Error()
	}
	if o.res.State != runner.StateSuccess {
		return fmt.Sprintf("%s (exit %d) %s", o.res.State, o.res.ExitCode, strings.Join(o.res.Warnings, "; "))
	}
	if len(o.res.Warnings) > 0 {
		return strings.Join(o.res.Warnings, "; ")
	}
	return ""
}

// dryRun runs an action as a dry run and fails when a file outside DevBox's own state
// and cache directories changed. With onlyOptions, only actions that take options
// from check data run (the others ran before).
func (s *sandbox) dryRun(ctx context.Context, name string, onlyOptions bool) {
	action := s.entry("actions", name)
	if onlyOptions && !usesOptions(action) {
		return
	}
	params, why, ok := s.params(ctx, action)
	if !ok {
		if !onlyOptions {
			s.add("dry-run", name, StatusSkipped, why)
		}
		return
	}
	before, err := s.snapshot()
	if err != nil {
		s.add("dry-run", name, StatusFailed, err.Error())
		return
	}
	o := s.runAction(ctx, name, params, true)
	if msg := o.failure(); msg != "" {
		s.add("dry-run", name, StatusFailed, msg)
		return
	}
	after, err := s.snapshot()
	if err != nil {
		s.add("dry-run", name, StatusFailed, err.Error())
		return
	}
	if changed := diff(before, after); len(changed) > 0 {
		s.add("dry-run", name, StatusFailed, "files changed: "+strings.Join(changed, ", "))
		return
	}
	s.add("dry-run", name, StatusOK, fmt.Sprintf("%d planned changes", o.changes))
}

func usesOptions(action map[string]any) bool {
	fields, _ := action["params"].(map[string]any)
	for _, f := range fields {
		field, _ := f.(map[string]any)
		if _, ok := field["optionsFrom"]; ok {
			return true
		}
	}
	return false
}

// applyTwice runs a non-dangerous action twice; the second run must report no change.
func (s *sandbox) applyTwice(ctx context.Context, name string) {
	action := s.entry("actions", name)
	if danger, _ := action["danger"].(bool); danger {
		s.add("apply", name, StatusSkipped, "danger")
		return
	}
	params, why, ok := s.params(ctx, action)
	if !ok {
		s.add("apply", name, StatusSkipped, why)
		return
	}
	first := s.runAction(ctx, name, params, false)
	if msg := first.failure(); msg != "" {
		s.add("apply", name, StatusFailed, msg)
		return
	}
	s.add("apply", name, StatusOK, fmt.Sprintf("%d changes", first.changes))
	second := s.runAction(ctx, name, params, false)
	switch {
	case second.failure() != "":
		s.add("repeat", name, StatusFailed, second.failure())
	case second.changes > 0:
		s.add("repeat", name, StatusFailed, fmt.Sprintf("second run reported %d changes", second.changes))
	default:
		s.add("repeat", name, StatusOK, "no changes")
	}
}

// snapshot hashes every file in the sandbox HOME except DevBox's own state and cache.
func (s *sandbox) snapshot() (map[string]string, error) {
	skip := []string{s.runner.Env.StateDir, s.runner.Env.CacheDir, s.runner.Env.RuntimeDir}
	out := map[string]string{}
	err := filepath.WalkDir(s.home, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		for _, sk := range skip {
			if p == sk {
				return filepath.SkipDir
			}
		}
		rel, _ := filepath.Rel(s.home, p)
		info, err := d.Info()
		if err != nil {
			return err
		}
		switch {
		case d.Type()&fs.ModeSymlink != 0:
			target, _ := os.Readlink(p)
			out[rel] = "link:" + target
		case d.IsDir():
			out[rel] = "dir"
		default:
			data, err := os.ReadFile(p) //nolint:gosec // files of the sandbox
			if err != nil {
				return err
			}
			sum := sha256.Sum256(data)
			out[rel] = fmt.Sprintf("%v:%s", info.Mode().Perm(), hex.EncodeToString(sum[:]))
		}
		return nil
	})
	return out, err
}

func diff(a, b map[string]string) []string {
	var changed []string
	for k, v := range b {
		if a[k] != v {
			changed = append(changed, k)
		}
	}
	for k := range a {
		if _, ok := b[k]; !ok {
			changed = append(changed, k)
		}
	}
	sort.Strings(changed)
	return changed
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
