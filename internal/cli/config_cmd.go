package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Zenit-Hosting-de/devbox/internal/config"
	"github.com/Zenit-Hosting-de/devbox/internal/jsonfile"
)

func (a *app) configCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "config", Args: cobra.NoArgs}
	cmd.AddCommand(
		&cobra.Command{Use: "get KEY", Args: cobra.ExactArgs(1), RunE: a.configGet},
		&cobra.Command{Use: "set KEY VALUE", Args: cobra.ExactArgs(2), RunE: a.configSet},
		&cobra.Command{Use: "unset KEY", Args: cobra.ExactArgs(1), RunE: a.configUnset},
		&cobra.Command{Use: "explain KEY", Args: cobra.ExactArgs(1), RunE: a.configExplain},
		&cobra.Command{Use: "validate", Args: cobra.NoArgs, RunE: a.configValidate},
		&cobra.Command{Use: "edit", Args: cobra.NoArgs, RunE: a.configEdit},
	)
	return cmd
}

func (a *app) configGet(_ *cobra.Command, args []string) error {
	st, err := a.load(true)
	if err != nil {
		return err
	}
	s, ok := st.Result.Get(args[0])
	if !ok {
		return &config.Error{Code: config.CodeNotFound, Key: args[0], Detail: "no plugin defines this setting"}
	}
	if text, isText := s.Value.(string); isText && !a.jsonOut {
		_, err := fmt.Fprintln(a.io.Stdout, text)
		return err
	}
	return a.printJSON(s.Value)
}

func (a *app) configSet(_ *cobra.Command, args []string) error {
	st, err := a.load(false)
	if err != nil {
		return err
	}
	return config.Set(st.Validator, st.UserPath, st.Defs, st.Result, args[0], args[1])
}

func (a *app) configUnset(_ *cobra.Command, args []string) error {
	e, err := a.env()
	if err != nil {
		return err
	}
	return config.Unset(e.Validator, e.UserPath, args[0])
}

func (a *app) configExplain(_ *cobra.Command, args []string) error {
	st, err := a.load(true)
	if err != nil {
		return err
	}
	ex, err := st.Result.Explain(args[0])
	if err != nil {
		return err
	}
	if a.jsonOut {
		return a.printJSON(ex)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s = %s\n", ex.Key, compact(ex.Value))
	for _, l := range ex.Layers {
		mark := " "
		if l.Layer == ex.Origin {
			mark = "*"
		}
		lock := ""
		if l.Locked {
			lock = " [locked]"
		}
		fmt.Fprintf(&b, "  %s %s: %s%s\n", mark, l.Layer, compact(l.Value), lock)
	}
	_, err = fmt.Fprint(a.io.Stdout, b.String())
	return err
}

func compact(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprint(v)
	}
	return string(b)
}

func (a *app) configValidate(_ *cobra.Command, _ []string) error {
	st, err := a.load(true)
	if err != nil {
		return err
	}
	for _, w := range st.Result.Warnings {
		_, _ = fmt.Fprintln(a.io.Stderr, "devbox: warning:", w)
	}
	return nil
}

func (a *app) configEdit(cmd *cobra.Command, _ []string) error {
	e, err := a.env()
	if err != nil {
		return err
	}
	editor := a.io.Getenv("VISUAL")
	if editor == "" {
		editor = a.io.Getenv("EDITOR")
	}
	if editor == "" {
		return &exitError{code: ExitInvalid, err: errors.New("VISUAL or EDITOR must name an editor")}
	}
	if _, err := os.Stat(e.UserPath); errors.Is(err, os.ErrNotExist) {
		doc := jsonfile.NewObject()
		if id, ok := e.Validator.ID("config"); ok {
			doc.Set("$schema", id)
		}
		data, err := jsonfile.Marshal(doc)
		if err != nil {
			return err
		}
		if err := jsonfile.WriteAtomic(e.UserPath, data, 0o644); err != nil {
			return err
		}
	}
	fields := strings.Fields(editor)
	run := exec.CommandContext(cmd.Context(), fields[0], append(fields[1:], e.UserPath)...) //nolint:gosec // the user's own editor
	run.Stdin, run.Stdout, run.Stderr = a.io.Stdin, a.io.Stdout, a.io.Stderr
	if err := run.Run(); err != nil {
		return fmt.Errorf("editor: %w", err)
	}
	return a.configValidate(cmd, nil)
}
