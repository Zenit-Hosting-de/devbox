package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/Zenit-Hosting-de/devbox/internal/config"
	"github.com/Zenit-Hosting-de/devbox/internal/core"
)

// Exit codes of the CLI; they follow the plugin protocol (docs/concept.md, "CLI").
const (
	ExitOK      = 0
	ExitFailed  = 1
	ExitInvalid = 2
)

// SystemConfigEnv overrides the path of the system configuration, e.g. for tests and
// images that keep it elsewhere.
const SystemConfigEnv = core.SystemConfigEnv

// DefaultSystemConfig is the system configuration (layer 2).
const DefaultSystemConfig = core.DefaultSystemConfig

// Build describes the binary.
type Build = core.Build

// IO bundles the process environment, so commands can run in tests.
type IO struct {
	Stdin   io.Reader
	Stdout  io.Writer
	Stderr  io.Writer
	Getenv  func(string) string
	Environ func() []string
}

// OSIO returns the IO of the current process.
func OSIO() IO {
	return IO{Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr, Getenv: os.Getenv, Environ: os.Environ}
}

type app struct {
	io    IO
	build Build

	// global flags
	jsonOut    bool
	configPath string
	sets       []string
}

// exitError carries an exit code through cobra.
type exitError struct {
	code int
	err  error
}

func (e *exitError) Error() string { return e.err.Error() }
func (e *exitError) Unwrap() error { return e.err }

// Main runs the CLI and returns the exit code.
func Main(args []string, build Build, sys IO) int {
	a := &app{io: sys, build: build}
	root := a.rootCommand()
	root.SetArgs(args)
	root.SetIn(sys.Stdin)
	root.SetOut(sys.Stdout)
	root.SetErr(sys.Stderr)
	err := root.Execute()
	if err == nil {
		return ExitOK
	}
	code := ExitFailed
	var ee *exitError
	if errors.As(err, &ee) {
		code = ee.code
	}
	var ce *config.Error
	if errors.As(err, &ce) {
		code = ExitInvalid
	}
	_, _ = fmt.Fprintln(sys.Stderr, "devbox:", err)
	return code
}

func (a *app) rootCommand() *cobra.Command {
	root := &cobra.Command{
		Use:           "devbox",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			// AP-21 starts the terminal menu here.
			return &exitError{code: ExitInvalid, err: errors.New("usage: devbox <command>")}
		},
	}
	root.CompletionOptions.DisableDefaultCmd = true
	pf := root.PersistentFlags()
	pf.BoolVar(&a.jsonOut, "json", false, "print JSON")
	pf.StringVar(&a.configPath, "config", "", "user configuration file")
	pf.StringArrayVar(&a.sets, "set", nil, "set a value for this call (key=value)")
	root.AddCommand(a.versionCommand(), a.configCommand(), a.pluginCommand(), a.runCommand(), a.historyCommand(), a.logsCommand(), a.statusCommand(), a.credCommand(), a.sdkCommand())
	return root
}

func (a *app) versionCommand() *cobra.Command {
	return &cobra.Command{
		Use:  "version",
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			if a.jsonOut {
				return a.printJSON(a.build)
			}
			_, err := fmt.Fprintln(a.io.Stdout, a.build.Version)
			return err
		},
	}
}

func (a *app) printJSON(v any) error {
	enc := json.NewEncoder(a.io.Stdout)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}

func (a *app) coreEnv() core.Env {
	return core.Env{Getenv: a.io.Getenv, Environ: a.io.Environ, ConfigPath: a.configPath, Sets: a.sets}
}

// env returns the directories and paths without loading the plugins.
func (a *app) env() (*core.Base, error) { return core.NewBase(a.coreEnv()) }

// load reads the configuration and the plugins; withCall adds the environment and
// --set layers.
func (a *app) load(withCall bool) (*core.State, error) {
	return core.Load(a.coreEnv(), a.build, withCall)
}
