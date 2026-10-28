package cli

import (
	"fmt"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/Zenit-Hosting-de/devbox/internal/config"
	"github.com/Zenit-Hosting-de/devbox/internal/plugin"
)

func (a *app) pluginCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "plugin", Args: cobra.NoArgs}
	cmd.AddCommand(
		&cobra.Command{Use: "list", Args: cobra.NoArgs, RunE: a.pluginList},
		&cobra.Command{Use: "info ID", Args: cobra.ExactArgs(1), RunE: a.pluginInfo},
	)
	cmd.AddCommand(a.pluginDevCommands()...)
	return cmd
}

func (a *app) plugins() ([]*plugin.Plugin, error) {
	st, err := a.load(true)
	if err != nil {
		return nil, err
	}
	return st.Evaluate(), nil
}

func (a *app) pluginList(_ *cobra.Command, _ []string) error {
	plugins, err := a.plugins()
	if err != nil {
		return err
	}
	if a.jsonOut {
		out := make([]plugin.Status, 0, len(plugins))
		for _, p := range plugins {
			out = append(out, p.Status())
		}
		return a.printJSON(out)
	}
	tw := tabwriter.NewWriter(a.io.Stdout, 0, 4, 2, ' ', 0)
	for _, p := range plugins {
		s := p.Status()
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", s.ID, s.Version, s.State, reasons(s.Reasons))
	}
	return tw.Flush()
}

func reasons(rs []plugin.Reason) string {
	parts := make([]string, 0, len(rs))
	for _, r := range rs {
		if r.Detail == "" {
			parts = append(parts, r.Code)
			continue
		}
		parts = append(parts, r.Code+": "+r.Detail)
	}
	return strings.Join(parts, "; ")
}

// Detail is `devbox plugin info --json` and GET /api/v1/plugins/{id}
// (api/openapi.json, PluginDetail): the status plus the manifest.
type Detail struct {
	plugin.Status
	Manifest map[string]any `json:"manifest,omitempty"`
}

func (a *app) pluginInfo(_ *cobra.Command, args []string) error {
	plugins, err := a.plugins()
	if err != nil {
		return err
	}
	for _, p := range plugins {
		if p.ID != args[0] {
			continue
		}
		d := Detail{Status: p.Status()}
		if p.Manifest != nil {
			d.Manifest = p.Manifest.Doc
		}
		if a.jsonOut {
			return a.printJSON(d)
		}
		tw := tabwriter.NewWriter(a.io.Stdout, 0, 4, 2, ' ', 0)
		for _, row := range [][2]string{{"id", d.ID}, {"name", d.Name}, {"version", d.Version}, {"state", string(d.State)}, {"reasons", reasons(d.Reasons)}, {"path", d.Path}} {
			_, _ = fmt.Fprintf(tw, "%s\t%s\n", row[0], row[1])
		}
		return tw.Flush()
	}
	return &config.Error{Code: config.CodeNotFound, Key: args[0], Detail: "no such plugin"}
}
