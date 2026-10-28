package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/Zenit-Hosting-de/devbox/internal/config"
	"github.com/Zenit-Hosting-de/devbox/internal/history"
	"github.com/Zenit-Hosting-de/devbox/internal/plugin"
	"github.com/Zenit-Hosting-de/devbox/internal/runner"
	"github.com/Zenit-Hosting-de/devbox/internal/schema"
)

// Exit code for a plugin that is not ready because a requirement is missing.
const exitBlocked = 3

// exitInterrupted is the exit code after the user cancelled a run.
const exitInterrupted = 130

type runFlags struct {
	params []string
	dryRun bool
	yes    bool
}

func (a *app) runCommand() *cobra.Command {
	var f runFlags
	cmd := &cobra.Command{
		Use:  "run ACTION",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error { return a.run(cmd.Context(), args[0], f) },
	}
	cmd.Flags().StringArrayVarP(&f.params, "param", "p", nil, "action parameter (name=value)")
	cmd.Flags().BoolVar(&f.dryRun, "dry-run", false, "report planned changes only")
	cmd.Flags().BoolVar(&f.yes, "yes", false, "answer confirmations with yes")
	return cmd
}

func (a *app) run(ctx context.Context, ref string, f runFlags) error {
	st, err := a.load(true)
	if err != nil {
		return err
	}
	plugins := a.evaluate(st)
	pluginID, name, ok := strings.Cut(ref, ".")
	if !ok || !config.ValidKey(ref) {
		return &config.Error{Code: config.CodeValidation, Key: ref, Detail: "want <plugin>.<action>"}
	}
	p := findPlugin(plugins, pluginID)
	if p == nil {
		return &config.Error{Code: config.CodeNotFound, Key: ref, Detail: "no such plugin"}
	}
	if p.State != plugin.StateReady {
		code := ExitInvalid
		if p.State == plugin.StateBlocked {
			code = exitBlocked
		}
		return &exitError{code: code, err: fmt.Errorf("plugin-not-ready: %s: %s (%s)", pluginID, p.State, reasons(p.Reasons))}
	}
	actions, _ := p.Manifest.Doc["actions"].(map[string]any)
	action, ok := actions[name].(map[string]any)
	if !ok {
		return &config.Error{Code: config.CodeNotFound, Key: ref, Detail: "no such action"}
	}
	params, err := actionParams(st, action, ref, f.params)
	if err != nil {
		return err
	}
	if confirm, ok := action["confirm"]; ok && confirm != false && !f.yes && !f.dryRun {
		if !a.confirm(confirm) {
			return &exitError{code: 4, err: fmt.Errorf("%s: not confirmed", ref)}
		}
	}

	r, err := a.newRunner(st)
	if err != nil {
		return err
	}
	spec := runner.Spec{
		Kind:      runner.KindAction,
		SessionID: runner.NewSessionID(time.Now()),
		PluginID:  pluginID,
		PluginDir: p.Dir,
		Name:      name,
		Command:   anyStrings(action["run"]),
		Params:    params,
		Settings:  st.result.Plugin(pluginID),
		DryRun:    f.dryRun,
		Yes:       f.yes,
		Lang:      uiLanguage(st),
		Host:      hostMap(st, a.io.Getenv),
		Locks:     anyStrings(action["locks"]),
		ExtraEnv:  proxyEnv(st),
	}
	if t, ok := action["timeout"].(string); ok {
		if spec.Timeout, err = time.ParseDuration(t); err != nil {
			return &config.Error{Code: config.CodeValidation, Key: ref, Detail: "timeout: " + err.Error()}
		}
	}
	if fd, ok := fdOf(a.io.Stdout); ok && term.IsTerminal(fd) {
		if w, h, err := term.GetSize(fd); err == nil {
			spec.Cols, spec.Rows = uint16(w), uint16(h) //nolint:gosec // terminal sizes fit uint16
		}
	}
	if fd, ok := fdOf(a.io.Stdin); ok && term.IsTerminal(fd) {
		spec.Input = a.io.Stdin
	}

	store := history.New(r.Env.StateDir, st.env.validator)
	sess, err := store.Create(spec.SessionID)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()
	cb := runner.Callbacks{Output: io.MultiWriter(a.io.Stdout, sess)}
	emit := func(msg map[string]any) {
		ev := sess.Event(msg, time.Now())
		if a.jsonOut {
			_ = json.NewEncoder(a.io.Stdout).Encode(ev)
		} else {
			a.renderMessage(msg, spec.DryRun)
		}
		if msg["type"] == "setting" {
			key := pluginID + "." + fmt.Sprint(msg["key"])
			if err := config.Set(st.env.validator, st.env.userPath, st.defs, st.result, key, msg["value"]); err != nil {
				_, _ = fmt.Fprintln(a.io.Stderr, "devbox: warning:", err)
			}
		}
	}
	cb.Message = emit
	if a.jsonOut {
		cb.Output = sess // JSON Lines only on stdout
	}
	res, err := r.Run(ctx, spec, cb)
	if cerr := sess.Close(); cerr != nil {
		_, _ = fmt.Fprintln(a.io.Stderr, "devbox: warning:", cerr)
	}
	if err != nil {
		return err
	}
	a.record(st, store, spec, action, res)
	for _, w := range res.Warnings {
		_, _ = fmt.Fprintln(a.io.Stderr, "devbox: warning:", w)
	}
	switch {
	case res.State == runner.StateSuccess:
		return nil
	case res.ExitCode > 0 && res.ExitCode < 128:
		return &exitError{code: res.ExitCode, err: fmt.Errorf("%s: %s (exit %d)", ref, res.State, res.ExitCode)}
	case res.State == runner.StateStopped:
		return &exitError{code: exitInterrupted, err: fmt.Errorf("%s: %s", ref, res.State)}
	default:
		return &exitError{code: ExitFailed, err: fmt.Errorf("%s: %s", ref, res.State)}
	}
}

// renderMessage prints a protocol message as one readable line in text mode.
func (a *app) renderMessage(msg map[string]any, dryRun bool) {
	str := func(k string) string { v, _ := msg[k].(string); return v }
	switch msg["type"] {
	case "step":
		mark := map[string]string{"running": "▸", "done": "✓", "skipped": "–", "failed": "✗"}[str("state")]
		_, _ = fmt.Fprintf(a.io.Stdout, "%s %s\n", mark, str("label"))
	case "change":
		op := map[string]string{"create": "+", "update": "~", "delete": "-"}[str("op")]
		prefix := ""
		if dryRun {
			prefix = "(dry run) "
		}
		_, _ = fmt.Fprintf(a.io.Stdout, "  %s%s %s: %s\n", prefix, op, str("target"), str("summary"))
	case "log":
		if lvl := str("level"); lvl != "info" {
			_, _ = fmt.Fprintf(a.io.Stderr, "%s: %s\n", lvl, str("text"))
		} else {
			_, _ = fmt.Fprintln(a.io.Stdout, str("text"))
		}
	case "notice":
		_, _ = fmt.Fprintf(a.io.Stdout, "note: %s\n", str("text"))
	case "result":
		_, _ = fmt.Fprintf(a.io.Stdout, "→ %s\n", str("summary"))
	}
}

// record writes the history line of a finished session and applies the retention
// settings. Failures are warnings: the action itself has already run.
func (a *app) record(st *state, store *history.Store, spec runner.Spec, action map[string]any, res *runner.Result) {
	title, _ := action["label"].(string)
	if title == "" {
		title = spec.PluginID + "." + spec.Name
	}
	e := history.Entry{
		ID:        spec.SessionID,
		Action:    spec.PluginID + "." + spec.Name,
		Plugin:    spec.PluginID,
		Title:     title,
		User:      st.env.ctx.Host.User,
		Origin:    history.OriginCLI,
		DryRun:    spec.DryRun,
		StartedAt: history.Timestamp(res.Started),
		EndedAt:   history.Timestamp(res.Ended),
		State:     res.State,
		Changes:   res.Changes,
	}
	if res.ExitCode >= 0 {
		code := res.ExitCode
		e.ExitCode = &code
	}
	if res.Result != nil {
		e.Summary, _ = res.Result["summary"].(string)
	}
	if err := store.Append(e); err != nil {
		_, _ = fmt.Fprintln(a.io.Stderr, "devbox: warning:", err)
	}
	keepDays, keepSessions := retention(st)
	if _, err := store.Prune(time.Now(), keepDays, keepSessions); err != nil {
		_, _ = fmt.Fprintln(a.io.Stderr, "devbox: warning:", err)
	}
}

// intValue reads a JSON integer as parsed by jsonfile (json.Number).
func intValue(v any) (int, bool) {
	n, ok := v.(json.Number)
	if !ok {
		return 0, false
	}
	i, err := n.Int64()
	return int(i), err == nil
}

// retention reads history.keepDays and history.keepSessions; the user file wins over
// the system file, zero means the schema default.
func retention(st *state) (keepDays, keepSessions int) {
	for _, f := range []*config.File{st.system, st.user} {
		h, _ := f.Plain()["history"].(map[string]any)
		if n, ok := intValue(h["keepDays"]); ok {
			keepDays = n
		}
		if n, ok := intValue(h["keepSessions"]); ok {
			keepSessions = n
		}
	}
	return keepDays, keepSessions
}

func findPlugin(plugins []*plugin.Plugin, id string) *plugin.Plugin {
	for _, p := range plugins {
		if p.ID == id {
			return p
		}
	}
	return nil
}

// actionParams applies -p values and defaults (placeholders resolved against the
// settings) and checks every value against its field.
func actionParams(st *state, action map[string]any, ref string, given []string) (map[string]any, error) {
	fields, _ := action["params"].(map[string]any)
	out := map[string]any{}
	for _, kv := range given {
		name, value, ok := strings.Cut(kv, "=")
		field, known := fields[name].(map[string]any)
		if !ok || !known {
			return nil, &config.Error{Code: config.CodeValidation, Key: ref, Detail: fmt.Sprintf("unknown parameter %q", kv)}
		}
		v, err := config.Coerce(field, value)
		if err != nil {
			return nil, &config.Error{Code: config.CodeValidation, Key: ref + "#" + name, Detail: err.Error()}
		}
		out[name] = v
	}
	lookup := func(r string) (any, error) {
		if config.ValidKey(r) {
			if s, ok := st.result.Get(r); ok {
				return s.Value, nil
			}
			return nil, fmt.Errorf("unknown setting %s", r)
		}
		return config.ContextLookup(st.env.ctx, r)
	}
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
				return nil, &config.Error{Code: config.CodeValidation, Key: ref + "#" + name, Detail: "parameter required"}
			}
			v, err := config.ExpandValue(def, lookup)
			if err != nil {
				return nil, &config.Error{Code: config.CodeValidation, Key: ref + "#" + name, Detail: err.Error()}
			}
			out[name] = v
		}
		frag, err := schema.CompileField(field)
		if err != nil {
			return nil, err
		}
		if err := frag.Validate(ref+"#"+name, out[name]); err != nil {
			return nil, &config.Error{Code: config.CodeValidation, Key: ref + "#" + name, Detail: err.Error()}
		}
	}
	return out, nil
}

// confirm asks on the terminal; without a terminal the answer is no.
func (a *app) confirm(prompt any) bool {
	fd, ok := fdOf(a.io.Stdin)
	if !ok || !term.IsTerminal(fd) {
		return false
	}
	text, _ := prompt.(string)
	_, _ = fmt.Fprintf(a.io.Stderr, "%s [y/N] ", text)
	line, _ := bufio.NewReader(a.io.Stdin).ReadString('\n')
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes", "j", "ja":
		return true
	}
	return false
}

type fder interface{ Fd() uintptr }

func fdOf(v any) (int, bool) {
	f, ok := v.(fder)
	if !ok {
		return 0, false
	}
	return int(f.Fd()), true //nolint:gosec // file descriptors fit int
}

func (a *app) newRunner(st *state) (*runner.Runner, error) {
	dirs := st.env.ctx.XDG
	bin, err := selfPath(a.io.Getenv("PATH"))
	if err != nil {
		return nil, err
	}
	runtimeDir := dirs.Runtime
	if runtimeDir == "" {
		runtimeDir = filepath.Join(os.TempDir(), "devbox-"+strconv.Itoa(os.Getuid()))
	} else {
		runtimeDir = filepath.Join(runtimeDir, "devbox")
	}
	termName := a.io.Getenv("TERM")
	if termName == "" {
		termName = "xterm-256color"
	}
	env := runner.Env{
		Version:    a.build.Version,
		Bin:        bin,
		Home:       a.io.Getenv("HOME"),
		User:       st.env.ctx.Host.User,
		Path:       a.io.Getenv("PATH"),
		Lang:       a.io.Getenv("LANG"),
		Term:       termName,
		RuntimeDir: runtimeDir,
		StateDir:   filepath.Join(dirs.State, "devbox"),
		CacheDir:   filepath.Join(dirs.Cache, "devbox"),
	}
	return runner.New(env, st.env.validator), nil
}

// selfPath returns how this binary was started (absolute), so plugins can call it as
// $DEVBOX_BIN under the same name; it falls back to the executable path.
func selfPath(pathEnv string) (string, error) {
	if arg := os.Args[0]; filepath.IsAbs(arg) {
		return arg, nil
	} else if strings.Contains(arg, "/") {
		return filepath.Abs(arg)
	} else if p, err := lookPath(arg, pathEnv); err == nil {
		return p, nil
	}
	return os.Executable()
}

func uiLanguage(st *state) string {
	for _, f := range []*config.File{st.user, st.system} {
		if ui, ok := f.Plain()["ui"].(map[string]any); ok {
			if l, ok := ui["language"].(string); ok && l != "" {
				return l
			}
		}
	}
	return ""
}

func hostMap(st *state, getenv func(string) string) map[string]any {
	h := st.env.ctx.Host
	m := map[string]any{"name": h.Name, "user": h.User, "distro": h.Distro, "distroVersion": h.DistroVersion, "arch": h.Arch}
	if h.Codename != "" {
		m["codename"] = h.Codename
	}
	if d := strings.ToLower(strings.Split(getenv("XDG_CURRENT_DESKTOP"), ":")[0]); d != "" {
		m["desktop"] = d
	}
	return m
}

// proxyEnv passes the network settings of the configuration to plugins.
func proxyEnv(st *state) []string {
	var network map[string]any
	for _, f := range []*config.File{st.system, st.user} {
		if n, ok := f.Plain()["network"].(map[string]any); ok {
			network = n
		}
	}
	expand := func(key string) string {
		s, _ := network[key].(string)
		v, err := config.Expand(s, func(r string) (any, error) { return config.ContextLookup(st.env.ctx, r) })
		if err != nil {
			return ""
		}
		out, _ := v.(string)
		return out
	}
	var env []string
	if p := expand("proxy"); p != "" {
		env = append(env, "HTTPS_PROXY="+p, "HTTP_PROXY="+p, "https_proxy="+p, "http_proxy="+p)
	}
	if n := expand("noProxy"); n != "" {
		env = append(env, "NO_PROXY="+n, "no_proxy="+n)
	}
	if c := expand("caFile"); c != "" {
		env = append(env, "SSL_CERT_FILE="+c)
	}
	return env
}
