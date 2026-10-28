package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/Zenit-Hosting-de/devbox/internal/config"
	"github.com/Zenit-Hosting-de/devbox/internal/core"
	"github.com/Zenit-Hosting-de/devbox/internal/history"
	"github.com/Zenit-Hosting-de/devbox/internal/plugin"
	"github.com/Zenit-Hosting-de/devbox/internal/runner"
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
	plugins := st.Evaluate()
	pluginID, name, ok := strings.Cut(ref, ".")
	if !ok || !config.ValidKey(ref) {
		return &config.Error{Code: config.CodeValidation, Key: ref, Detail: "want <plugin>.<action>"}
	}
	p := core.FindPlugin(plugins, pluginID)
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
	given, err := coerceParams(action, ref, f.params)
	if err != nil {
		return err
	}
	params, err := st.ResolveParams(action, ref, given)
	if err != nil {
		return err
	}
	if confirm, ok := action["confirm"]; ok && confirm != false && !f.yes && !f.dryRun {
		if !a.confirm(confirm) {
			return &exitError{code: 4, err: fmt.Errorf("%s: not confirmed", ref)}
		}
	}

	r, err := st.NewRunner()
	if err != nil {
		return err
	}
	spec, title, err := st.ActionSpec(p, name, params, f.dryRun, f.yes)
	if err != nil {
		return err
	}
	if fd, ok := fdOf(a.io.Stdout); ok && term.IsTerminal(fd) {
		if w, h, err := term.GetSize(fd); err == nil {
			spec.Cols, spec.Rows = uint16(w), uint16(h) //nolint:gosec // terminal sizes fit uint16
		}
	}
	if fd, ok := fdOf(a.io.Stdin); ok && term.IsTerminal(fd) {
		spec.Input = a.io.Stdin
	}

	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()
	res, err := a.runSession(ctx, st, r, spec, ref, title)
	if err != nil {
		return err
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

// runSession runs one plugin process as a session of the history: output and events
// go to the terminal (or JSON Lines with --json) and the session files, the history
// line is written at the end. ref is the fully qualified action of the history line.
func (a *app) runSession(ctx context.Context, st *core.State, r *runner.Runner, spec runner.Spec, ref, title string) (*runner.Result, error) {
	sink := core.Sink{
		Warn: func(text string) { _, _ = fmt.Fprintln(a.io.Stderr, "devbox: warning:", text) },
		Event: func(ev history.Event) {
			if a.jsonOut {
				_ = json.NewEncoder(a.io.Stdout).Encode(ev)
			} else {
				a.renderMessage(ev.Event, spec.DryRun)
			}
		},
	}
	if !a.jsonOut { // JSON Lines only on stdout
		sink.Output = a.io.Stdout
	}
	return core.RunSession(ctx, st, r, spec, ref, title, history.OriginCLI, sink)
}

// coerceParams turns the -p name=value texts into typed values.
func coerceParams(action map[string]any, ref string, given []string) (map[string]any, error) {
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
