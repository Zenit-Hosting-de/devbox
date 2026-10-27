package cli

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/Zenit-Hosting-de/devbox/internal/schema"
	"github.com/Zenit-Hosting-de/devbox/internal/sdk"
)

// sdkCommand implements `devbox sdk …`, the helpers for plugin scripts. They work on
// the request file and message channel of the calling plugin, not on the configuration.
func (a *app) sdkCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "sdk", Args: cobra.NoArgs, Hidden: true}
	env := func() (sdk.Env, error) {
		v, err := schema.New()
		if err != nil {
			return sdk.Env{}, err
		}
		e := sdk.FromProcess(v)
		e.Getenv = a.io.Getenv
		return e, nil
	}
	lookup := func(section string) func(*cobra.Command, []string) error {
		return func(_ *cobra.Command, args []string) error {
			e, err := env()
			if err != nil {
				return err
			}
			v, err := e.Lookup(section, args[0])
			if err != nil {
				return &exitError{code: ExitInvalid, err: err}
			}
			_, err = fmt.Fprintln(a.io.Stdout, sdk.Format(v))
			return err
		}
	}
	var mode string
	var sha string
	block := &cobra.Command{
		Use: "block FILE NAME", Args: cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			e, err := env()
			if err != nil {
				return err
			}
			body, err := io.ReadAll(a.io.Stdin)
			if err != nil {
				return err
			}
			perm, err := strconv.ParseUint(mode, 8, 32)
			if err != nil {
				return &exitError{code: ExitInvalid, err: fmt.Errorf("--mode %q: %w", mode, err)}
			}
			return e.Block(args[0], args[1], body, os.FileMode(perm))
		},
	}
	block.Flags().StringVar(&mode, "mode", "644", "mode for a new file (octal), e.g. 600 for files with secrets")
	download := &cobra.Command{
		Use: "download URL DEST", Args: cobra.ExactArgs(2),
		RunE: func(c *cobra.Command, args []string) error {
			e, err := env()
			if err != nil {
				return err
			}
			client := &http.Client{Transport: &http.Transport{Proxy: http.ProxyFromEnvironment}}
			return e.Download(c.Context(), client, args[0], args[1], sha)
		},
	}
	download.Flags().StringVar(&sha, "sha256", "", "expected SHA-256 checksum")

	cmd.AddCommand(
		&cobra.Command{Use: "param NAME", Args: cobra.ExactArgs(1), RunE: lookup("params")},
		&cobra.Command{Use: "setting NAME", Args: cobra.ExactArgs(1), RunE: lookup("settings")},
		&cobra.Command{
			Use: "secret", Args: cobra.NoArgs,
			RunE: func(_ *cobra.Command, _ []string) error {
				e, err := env()
				if err != nil {
					return err
				}
				s, err := e.Secret()
				if err != nil {
					return &exitError{code: ExitInvalid, err: err}
				}
				_, err = fmt.Fprint(a.io.Stdout, s)
				return err
			},
		},
		&cobra.Command{
			Use: "emit TYPE ARGS…", Args: cobra.MinimumNArgs(1), DisableFlagParsing: true,
			RunE: func(_ *cobra.Command, args []string) error {
				e, err := env()
				if err != nil {
					return err
				}
				msg, err := sdk.EmitArgs(args)
				if err != nil {
					return &exitError{code: ExitInvalid, err: err}
				}
				if err := e.Emit(msg); err != nil {
					return &exitError{code: ExitInvalid, err: err}
				}
				return nil
			},
		},
		block,
		&cobra.Command{
			Use: "block-remove FILE NAME", Args: cobra.ExactArgs(2),
			RunE: func(_ *cobra.Command, args []string) error {
				e, err := env()
				if err != nil {
					return err
				}
				return e.BlockRemove(args[0], args[1])
			},
		},
		&cobra.Command{
			Use: "json-set FILE POINTER VALUE", Args: cobra.ExactArgs(3),
			RunE: func(_ *cobra.Command, args []string) error {
				e, err := env()
				if err != nil {
					return err
				}
				v, err := schema.Decode([]byte(args[2]))
				if err != nil {
					return &exitError{code: ExitInvalid, err: fmt.Errorf("VALUE must be JSON, e.g. '\"text\"': %w", err)}
				}
				return e.JSONSet(args[0], args[1], v)
			},
		},
		download,
	)
	return cmd
}
