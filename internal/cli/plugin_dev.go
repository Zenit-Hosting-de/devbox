package cli

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/Zenit-Hosting-de/devbox/internal/config"
	"github.com/Zenit-Hosting-de/devbox/internal/platform"
	"github.com/Zenit-Hosting-de/devbox/internal/plugin"
	"github.com/Zenit-Hosting-de/devbox/internal/plugintest"
	"github.com/Zenit-Hosting-de/devbox/internal/schema"
	templates "github.com/Zenit-Hosting-de/devbox/templates/plugin"
)

var pluginIDPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{1,31}$`)

func (a *app) pluginDevCommands() []*cobra.Command {
	var lang, dir string
	newCmd := &cobra.Command{
		Use: "new NAME", Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error { return a.pluginNew(args[0], lang, dir) },
	}
	newCmd.Flags().StringVar(&lang, "lang", "bash", "template language")
	newCmd.Flags().StringVar(&dir, "dir", "", "target directory (default: ./NAME)")

	var apply, checksOnly bool
	testCmd := &cobra.Command{
		Use: "test PATH", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.pluginTest(cmd.Context(), args[0], apply, checksOnly)
		},
	}
	testCmd.Flags().BoolVar(&apply, "apply", false, "run non-dangerous actions twice and expect no change on the second run")
	testCmd.Flags().BoolVar(&checksOnly, "checks-only", false, "only validate and run the checks")

	return []*cobra.Command{
		newCmd,
		{Use: "validate PATH", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
			return a.pluginValidate(cmd.Context(), args[0])
		}},
		testCmd,
	}
}

// pluginNew copies a template and replaces {{id}} and {{name}}.
func (a *app) pluginNew(id, lang, dir string) error {
	if !pluginIDPattern.MatchString(id) {
		return &config.Error{Code: config.CodeValidation, Key: id, Detail: "plugin IDs match ^[a-z][a-z0-9-]{1,31}$"}
	}
	sub, err := fs.Sub(templates.FS, lang)
	if err != nil || !hasEntries(sub) {
		return &config.Error{Code: config.CodeNotFound, Key: lang, Detail: "no template for this language"}
	}
	if dir == "" {
		dir = id
	}
	if _, err := os.Stat(dir); err == nil {
		return &config.Error{Code: config.CodeValidation, Key: dir, Detail: "target exists"}
	}
	name := strings.ToUpper(id[:1]) + strings.ReplaceAll(id[1:], "-", " ")
	repl := strings.NewReplacer("{{id}}", id, "{{name}}", name)
	return fs.WalkDir(sub, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		target := filepath.Join(dir, filepath.FromSlash(p))
		if d.IsDir() {
			return os.MkdirAll(target, 0o750)
		}
		data, err := fs.ReadFile(sub, p)
		if err != nil {
			return err
		}
		mode := os.FileMode(0o644)
		if strings.HasSuffix(p, ".sh") || strings.HasSuffix(p, ".bats") {
			mode = 0o755
		}
		return os.WriteFile(target, []byte(repl.Replace(string(data))), mode) //nolint:gosec // scripts must be executable
	})
}

func hasEntries(fsys fs.FS) bool {
	entries, err := fs.ReadDir(fsys, ".")
	return err == nil && len(entries) > 0
}

type validateReport struct {
	Plugin   string           `json:"plugin"`
	Problems []plugin.Problem `json:"problems"`
}

func (a *app) pluginValidate(ctx context.Context, dir string) error {
	v, err := schema.New()
	if err != nil {
		return err
	}
	m, problems := plugin.Validate(ctx, v, dir, plugin.ValidateOptions{Shellcheck: true, PathEnv: a.io.Getenv("PATH")})
	rep := validateReport{Plugin: dir, Problems: problems}
	if m != nil {
		rep.Plugin = m.ID
	}
	if rep.Problems == nil {
		rep.Problems = []plugin.Problem{}
	}
	if a.jsonOut {
		if err := a.printJSON(rep); err != nil {
			return err
		}
	} else {
		for _, p := range problems {
			_, _ = fmt.Fprintf(a.io.Stdout, "%s: %s\n", p.Code, p.Detail)
		}
	}
	if len(problems) > 0 {
		return &exitError{code: ExitInvalid, err: fmt.Errorf("%s: %d problems", rep.Plugin, len(problems))}
	}
	return nil
}

func (a *app) pluginTest(ctx context.Context, dir string, apply, checksOnly bool) error {
	v, err := schema.New()
	if err != nil {
		return err
	}
	host, err := platform.DetectHost(platform.OSReleasePaths)
	if err != nil {
		return err
	}
	bin, err := selfPath(a.io.Getenv("PATH"))
	if err != nil {
		return err
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	rep, err := plugintest.Run(ctx, plugintest.Options{
		Dir: abs, Apply: apply, ChecksOnly: checksOnly, Set: a.sets, Version: a.build.Version,
		Bin: bin, Path: a.io.Getenv("PATH"), Host: host, Validator: v,
	})
	if err != nil {
		return err
	}
	if a.jsonOut {
		if err := a.printJSON(rep); err != nil {
			return err
		}
	} else {
		tw := tabwriter.NewWriter(a.io.Stdout, 0, 4, 2, ' ', 0)
		for _, s := range rep.Steps {
			_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", s.Status, s.Step, s.Target, s.Detail)
		}
		_ = tw.Flush()
	}
	if rep.Failed() {
		return &exitError{code: ExitFailed, err: fmt.Errorf("%s: plugin test failed", rep.Plugin)}
	}
	return nil
}
