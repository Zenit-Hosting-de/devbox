package cli

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/Zenit-Hosting-de/devbox/internal/config"
	"github.com/Zenit-Hosting-de/devbox/internal/core"
	"github.com/Zenit-Hosting-de/devbox/internal/plugin"
	"github.com/Zenit-Hosting-de/devbox/internal/schema"
	"github.com/Zenit-Hosting-de/devbox/internal/vault"
)

// Exit codes of `devbox status --strict` and `devbox check --strict`.
const (
	exitChecksWarn  = 5
	exitChecksError = 6
)

// strictExit maps a summary to the exit code of --strict: 6 when a check reports
// error or missing, 5 when one warns.
func strictExit(sum core.Summary) error {
	switch {
	case sum.Error > 0 || sum.Missing > 0:
		return &exitError{code: exitChecksError, err: fmt.Errorf("%d checks report error or missing", sum.Error+sum.Missing)}
	case sum.Warn > 0:
		return &exitError{code: exitChecksWarn, err: fmt.Errorf("%d checks warn", sum.Warn)}
	}
	return nil
}

// checkOutput is one check of `devbox check --json`.
type checkOutput struct {
	checkLine
	Label   string             `json:"label,omitempty"`
	Details []core.CheckDetail `json:"details"`
	Hints   []core.CheckHint   `json:"hints"`
}

func (a *app) checkCommand() *cobra.Command {
	var strict bool
	cmd := &cobra.Command{
		Use:   "check [ID ...]",
		Short: "run checks and show their details (IDs are <plugin>.<check> or a plugin)",
		RunE:  func(cmd *cobra.Command, args []string) error { return a.check(cmd.Context(), args, strict) },
	}
	cmd.Flags().BoolVar(&strict, "strict", false, "exit 5 when a check warns, 6 when one reports error or missing")
	return cmd
}

// check runs the named checks (all without arguments) and prints summary, details
// and hints of each.
func (a *app) check(ctx context.Context, ids []string, strict bool) error {
	st, err := a.load(true)
	if err != nil {
		return err
	}
	known := map[string]bool{}
	for _, c := range core.Checks(st.Evaluate()) {
		known[c.ID()] = true
		known[c.Plugin.ID] = true
	}
	for _, id := range ids {
		if !known[id] {
			return &config.Error{Code: config.CodeNotFound, Key: id, Detail: "no such check of a ready plugin"}
		}
	}
	var filter func(string) bool
	if len(ids) > 0 {
		filter = func(ref string) bool {
			pluginID, _, _ := strings.Cut(ref, ".")
			for _, id := range ids {
				if id == ref || id == pluginID {
					return true
				}
			}
			return false
		}
	}
	r, err := st.NewRunner()
	if err != nil {
		return err
	}
	outcomes := core.RunChecks(ctx, st, r, filter)
	sum := core.Summarize(outcomes)
	if a.jsonOut {
		out := make([]checkOutput, 0, len(outcomes))
		for _, o := range outcomes {
			c := checkOutput{
				checkLine: checkLine{ID: o.ID, Status: o.Status, Summary: o.Summary, CheckedAt: o.CheckedAt},
				Label:     o.Label, Details: o.Details, Hints: o.Hints,
			}
			if c.Details == nil {
				c.Details = []core.CheckDetail{}
			}
			if c.Hints == nil {
				c.Hints = []core.CheckHint{}
			}
			out = append(out, c)
		}
		if err := a.printJSON(struct {
			Summary core.Summary  `json:"summary"`
			Checks  []checkOutput `json:"checks"`
		}{sum, out}); err != nil {
			return err
		}
	} else {
		w := a.io.Stdout
		for _, o := range outcomes {
			_, _ = fmt.Fprintf(w, "%s  %s  %s\n", o.ID, o.Status, o.Summary)
			for _, d := range o.Details {
				_, _ = fmt.Fprintf(w, "    %s: %s\n", d.Label, d.Value)
			}
			for _, h := range o.Hints {
				if h.Action != "" {
					_, _ = fmt.Fprintf(w, "    hint: %s (devbox run %s)\n", h.Text, h.Action)
				} else {
					_, _ = fmt.Fprintf(w, "    hint: %s\n", h.Text)
				}
			}
		}
		_, _ = fmt.Fprintf(w, "\n%d ok, %d warn, %d error, %d missing\n", sum.OK, sum.Warn, sum.Error, sum.Missing)
	}
	if strict {
		return strictExit(sum)
	}
	return nil
}

// actionOutput is one action of `devbox actions --json`.
type actionOutput struct {
	ID      string           `json:"id"`
	Label   string           `json:"label,omitempty"`
	Confirm bool             `json:"confirm"`
	Params  []actionParamOut `json:"params"`
}

type actionParamOut struct {
	Name     string `json:"name"`
	Type     string `json:"type,omitempty"`
	Label    string `json:"label,omitempty"`
	Required bool   `json:"required"`
	Default  any    `json:"default,omitempty"`
	Enum     []any  `json:"enum,omitempty"`
}

func (a *app) actionsCommand() *cobra.Command {
	return &cobra.Command{
		Use: "actions [PLUGIN]", Short: "list the actions of the ready plugins with their parameters", Args: cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error { return a.actions(args) },
	}
}

func (a *app) actions(args []string) error {
	st, err := a.load(true)
	if err != nil {
		return err
	}
	plugins := st.Evaluate()
	if len(args) == 1 {
		p := core.FindPlugin(plugins, args[0])
		if p == nil {
			return &config.Error{Code: config.CodeNotFound, Key: args[0], Detail: "no such plugin"}
		}
		if p.State != plugin.StateReady {
			return &exitError{code: ExitInvalid, err: fmt.Errorf("plugin-not-ready: %s: %s (%s)", p.ID, p.State, reasons(p.Reasons))}
		}
		plugins = []*plugin.Plugin{p}
	}
	out := []actionOutput{}
	for _, p := range plugins {
		if p.State != plugin.StateReady {
			continue
		}
		actions, _ := p.Manifest.Doc["actions"].(map[string]any)
		names := make([]string, 0, len(actions))
		for name := range actions {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			def, _ := actions[name].(map[string]any)
			ao := actionOutput{ID: p.ID + "." + name, Params: []actionParamOut{}}
			ao.Label, _ = def["label"].(string)
			if c, ok := def["confirm"]; ok && c != false {
				ao.Confirm = true
			}
			params, _ := def["params"].(map[string]any)
			pnames := make([]string, 0, len(params))
			for pn := range params {
				pnames = append(pnames, pn)
			}
			sort.Strings(pnames)
			for _, pn := range pnames {
				field, _ := params[pn].(map[string]any)
				po := actionParamOut{Name: pn, Default: field["default"]}
				po.Type, _ = field["type"].(string)
				po.Label, _ = field["label"].(string)
				po.Required, _ = field["required"].(bool)
				po.Enum, _ = field["enum"].([]any)
				ao.Params = append(ao.Params, po)
			}
			out = append(out, ao)
		}
	}
	if a.jsonOut {
		return a.printJSON(out)
	}
	w := a.io.Stdout
	for _, ao := range out {
		confirm := ""
		if ao.Confirm {
			confirm = "  (asks for confirmation)"
		}
		_, _ = fmt.Fprintf(w, "%s  %s%s\n", ao.ID, ao.Label, confirm)
		for _, p := range ao.Params {
			var notes []string
			if p.Type != "" {
				notes = append(notes, p.Type)
			}
			if p.Required {
				notes = append(notes, "required")
			}
			if p.Default != nil {
				notes = append(notes, "default "+compact(p.Default))
			}
			if len(p.Enum) > 0 {
				notes = append(notes, "one of "+compact(p.Enum))
			}
			_, _ = fmt.Fprintf(w, "    -p %s=…  %s (%s)\n", p.Name, p.Label, strings.Join(notes, ", "))
		}
	}
	return nil
}

func (a *app) groupCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "group", Short: "list and run action groups", Args: cobra.NoArgs}
	var f runFlags
	run := &cobra.Command{
		Use: "run ID", Short: "run the actions of a group one after another", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error { return a.groupRun(cmd.Context(), args[0], f) },
	}
	run.Flags().BoolVar(&f.dryRun, "dry-run", false, "report planned changes only")
	run.Flags().BoolVar(&f.yes, "yes", false, "answer confirmations with yes")
	cmd.AddCommand(
		&cobra.Command{Use: "list", Short: "list the groups", Args: cobra.NoArgs, RunE: a.groupList},
		run,
	)
	return cmd
}

func (a *app) groupList(_ *cobra.Command, _ []string) error {
	st, err := a.load(true)
	if err != nil {
		return err
	}
	groups := st.Groups()
	if a.jsonOut {
		return a.printJSON(groups)
	}
	tw := tabwriter.NewWriter(a.io.Stdout, 0, 4, 2, ' ', 0)
	for _, g := range groups {
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\n", g.ID, g.Origin, strings.Join(g.Actions, ", "))
	}
	return tw.Flush()
}

// groupRun runs the actions of a group in order, each as its own session. An action
// that fails, is refused or belongs to a plugin that is not ready does not stop the
// others; the exit code is 1 when any failed. A cancelled run (Ctrl+C) ends the group.
func (a *app) groupRun(ctx context.Context, id string, f runFlags) error {
	st, err := a.load(true)
	if err != nil {
		return err
	}
	g, err := st.Group(id)
	if err != nil {
		return err
	}
	if len(g.Actions) == 0 {
		return &config.Error{Code: config.CodeConflict, Key: id, Detail: "the group has no actions"}
	}
	var failed, skipped []string
	for _, ref := range g.Actions {
		pluginID, _, _ := strings.Cut(ref, ".")
		if p := core.FindPlugin(st.Evaluate(), pluginID); p == nil || p.State != plugin.StateReady {
			skipped = append(skipped, ref)
			if !a.jsonOut {
				_, _ = fmt.Fprintf(a.io.Stdout, "== %s: skipped, the plugin is not ready\n", ref)
			}
			continue
		}
		if !a.jsonOut {
			_, _ = fmt.Fprintf(a.io.Stdout, "== %s\n", ref)
		}
		err := a.run(ctx, ref, f)
		var ee *exitError
		switch {
		case err == nil:
		case errors.As(err, &ee) && ee.code == exitInterrupted:
			return err
		default:
			failed = append(failed, ref)
			_, _ = fmt.Fprintf(a.io.Stderr, "devbox: %v\n", err)
		}
	}
	if !a.jsonOut {
		_, _ = fmt.Fprintf(a.io.Stdout, "\n%s: %d run, %d failed, %d skipped\n", id, len(g.Actions)-len(skipped), len(failed), len(skipped))
	}
	if len(failed) > 0 {
		return fmt.Errorf("group %s: failed: %s", id, strings.Join(failed, ", "))
	}
	return nil
}

func (a *app) schemaCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "schema", Short: "embedded JSON Schemas", Args: cobra.NoArgs}
	cmd.AddCommand(
		&cobra.Command{
			Use: "list", Short: "list the names of the embedded schemas", Args: cobra.NoArgs,
			RunE: func(_ *cobra.Command, _ []string) error {
				v, err := schema.New()
				if err != nil {
					return err
				}
				if a.jsonOut {
					return a.printJSON(v.Names())
				}
				for _, n := range v.Names() {
					_, _ = fmt.Fprintln(a.io.Stdout, n)
				}
				return nil
			},
		},
		&cobra.Command{
			Use: "print NAME", Short: "print an embedded schema", Args: cobra.ExactArgs(1),
			RunE: func(_ *cobra.Command, args []string) error {
				data, ok := schema.Raw(args[0])
				if !ok {
					return &config.Error{Code: config.CodeNotFound, Key: args[0], Detail: "no such schema (see devbox schema list)"}
				}
				_, err := a.io.Stdout.Write(data)
				return err
			},
		},
	)
	return cmd
}

func (a *app) completionCommand(root *cobra.Command) *cobra.Command {
	return &cobra.Command{
		Use: "completion bash|zsh|fish", Short: "print the shell completion script", Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			switch args[0] {
			case "bash":
				return root.GenBashCompletionV2(a.io.Stdout, true)
			case "zsh":
				return root.GenZshCompletion(a.io.Stdout)
			case "fish":
				return root.GenFishCompletion(a.io.Stdout, true)
			}
			return &exitError{code: ExitInvalid, err: fmt.Errorf("unknown shell %q: want bash, zsh or fish", args[0])}
		},
	}
}

// vaultFileName is the age file of the vault in the data directory.
const vaultFileName = "vault.age"

// doctorItem is one line of the self-test.
type doctorItem struct {
	ID     string `json:"id"`
	Status string `json:"status"` // ok, warn, error or info
	Text   string `json:"text"`
}

func (a *app) doctorCommand() *cobra.Command {
	var offline bool
	cmd := &cobra.Command{
		Use: "doctor", Short: "self-test: configuration, schemas, plugins, vault, service, proxy, network", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error { return a.doctor(cmd.Context(), offline) },
	}
	cmd.Flags().BoolVar(&offline, "offline", false, "skip the network test")
	return cmd
}

// doctor tests the installation itself. Only an error item makes the exit code 1.
func (a *app) doctor(ctx context.Context, offline bool) error {
	var items []doctorItem
	add := func(id, status, format string, args ...any) {
		items = append(items, doctorItem{ID: id, Status: status, Text: fmt.Sprintf(format, args...)})
	}

	v, err := schema.New()
	if err != nil {
		add("schemas", "error", "%v", err)
	} else {
		bad := 0
		for _, n := range v.Names() {
			if err := v.Compile(n); err != nil {
				bad++
				add("schemas", "error", "%s: %v", n, err)
			}
		}
		if bad == 0 {
			add("schemas", "ok", "%d embedded schemas compile", len(v.Names()))
		}
	}

	st, err := a.load(true)
	if err != nil {
		add("config", "error", "%v", err)
		return a.doctorReport(items)
	}
	add("config", "ok", "user file %s, system file %s, %d profiles", st.UserPath, st.SystemPath, len(st.Profiles))
	for _, w := range st.Result.Warnings {
		add("config", "warn", "%s", w)
	}

	counts := map[plugin.State]int{}
	for _, p := range st.Evaluate() {
		counts[p.State]++
		if p.State == plugin.StateInvalid {
			add("plugins", "error", "%s is invalid: %s", p.ID, reasons(p.Reasons))
		}
	}
	states := make([]string, 0, len(counts))
	for s, n := range counts {
		states = append(states, fmt.Sprintf("%d %s", n, s))
	}
	sort.Strings(states)
	add("plugins", "ok", "%s", strings.Join(states, ", "))

	items = append(items, vaultItem(st))

	socket := ""
	if rt := a.io.Getenv("XDG_RUNTIME_DIR"); rt != "" {
		socket = filepath.Join(rt, "devbox", socketName)
	}
	if socket == "" {
		add("service", "info", "XDG_RUNTIME_DIR is not set, so there is no Unix socket")
	} else {
		d := net.Dialer{Timeout: 2 * time.Second}
		conn, err := d.DialContext(ctx, "unix", socket)
		if err != nil {
			add("service", "info", "not running (no answer on %s)", socket)
		} else {
			_ = conn.Close()
			add("service", "ok", "running, socket %s", socket)
		}
	}

	proxy := st.ProxyEnv()
	if len(proxy) == 0 {
		add("proxy", "info", "no proxy configured")
	} else {
		names := make([]string, 0, len(proxy))
		for _, kv := range proxy {
			name, _, _ := strings.Cut(kv, "=")
			names = append(names, name)
		}
		add("proxy", "ok", "set for plugins: %s", strings.Join(names, ", "))
	}

	switch {
	case offline:
		add("network", "info", "skipped (--offline)")
	default:
		target := ""
		if s, ok := st.Result.Get("system.releaseUrl"); ok {
			target, _ = s.Value.(string)
		}
		if target == "" {
			add("network", "info", "no address to test (system.releaseUrl is not set)")
			break
		}
		ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		req, err := http.NewRequestWithContext(ctx, http.MethodHead, target, nil)
		if err != nil {
			cancel()
			add("network", "warn", "system.releaseUrl: %v", err)
			break
		}
		resp, err := (&http.Client{Transport: &http.Transport{Proxy: http.ProxyFromEnvironment}}).Do(req)
		cancel()
		if err != nil {
			add("network", "warn", "%s is not reachable: %v", req.URL.Host, err)
		} else {
			_ = resp.Body.Close()
			add("network", "ok", "%s answers (%s)", req.URL.Host, resp.Status)
		}
	}
	return a.doctorReport(items)
}

func (a *app) doctorReport(items []doctorItem) error {
	errs := 0
	for _, it := range items {
		if it.Status == "error" {
			errs++
		}
	}
	if a.jsonOut {
		if err := a.printJSON(struct {
			OK    bool         `json:"ok"`
			Items []doctorItem `json:"items"`
		}{errs == 0, items}); err != nil {
			return err
		}
	} else {
		tw := tabwriter.NewWriter(a.io.Stdout, 0, 4, 2, ' ', 0)
		for _, it := range items {
			_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\n", it.ID, it.Status, it.Text)
		}
		if err := tw.Flush(); err != nil {
			return err
		}
	}
	if errs > 0 {
		return fmt.Errorf("doctor: %d problems", errs)
	}
	return nil
}

// vaultItem describes the vault without opening it: opening would create the key of
// an age vault, and a self-test changes nothing.
func vaultItem(st *core.State) doctorItem {
	backend := vault.BackendAuto
	for _, f := range []*config.File{st.System, st.User} {
		v, _ := f.Plain()["vault"].(map[string]any)
		if b, ok := v["backend"].(string); ok {
			backend = b
		}
	}
	var keyring vault.KeyringAPI = vault.SystemKeyring{}
	if st.Env.Keyring != nil {
		keyring = st.Env.Keyring
	}
	hasKeyring := vault.NewKeyring(keyring).Available()
	file := filepath.Join(st.DataDir(), vaultFileName)
	_, statErr := os.Stat(file)
	hasFile := statErr == nil
	switch {
	case backend == vault.BackendKeyring && !hasKeyring:
		return doctorItem{ID: "vault", Status: "error", Text: "vault.backend is keyring, but no Secret Service answers"}
	case backend == vault.BackendKeyring || (backend == vault.BackendAuto && hasKeyring):
		return doctorItem{ID: "vault", Status: "ok", Text: "Secret Service (keyring) is available"}
	case hasFile:
		return doctorItem{ID: "vault", Status: "ok", Text: "age file " + file}
	}
	return doctorItem{ID: "vault", Status: "info", Text: "age file, not created yet (" + file + ")"}
}
