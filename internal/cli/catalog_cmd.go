package cli

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/Zenit-Hosting-de/devbox/internal/catalog"
	"github.com/Zenit-Hosting-de/devbox/internal/repo"
)

// exitNotConfirmed is the exit code of a confirmation that was refused or could not be
// asked (no terminal and no --yes).
const exitNotConfirmed = 4

// catalogCommands are the plugin subcommands for catalogs.
func (a *app) catalogCommands() []*cobra.Command {
	var catalogName string
	var yes bool
	install := &cobra.Command{
		Use: "install ID[@VERSION]", Short: "install a plugin from a catalog", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error { return a.pluginInstall(cmd, args[0], catalogName, yes) },
	}
	install.Flags().StringVar(&catalogName, "catalog", "", "catalog to install from, when several offer the plugin")
	install.Flags().BoolVar(&yes, "yes", false, "accept the declared permissions without asking")

	var yesUpdate bool
	update := &cobra.Command{
		Use: "update [ID]", Short: "update plugins installed from catalogs", Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error { return a.pluginUpdate(cmd, args, yesUpdate) },
	}
	update.Flags().BoolVar(&yesUpdate, "yes", false, "accept new permissions without asking")

	return []*cobra.Command{
		{Use: "search [QUERY]", Short: "search the catalogs", Args: cobra.MaximumNArgs(1), RunE: a.pluginSearch},
		install,
		update,
		{Use: "remove ID", Short: "remove a plugin installed from a catalog", Args: cobra.ExactArgs(1), RunE: a.pluginRemove},
	}
}

func (a *app) pluginSearch(cmd *cobra.Command, args []string) error {
	st, err := a.load(true)
	if err != nil {
		return err
	}
	q := ""
	if len(args) == 1 {
		q = args[0]
	}
	hits, problems, err := st.CatalogSearch(cmd.Context(), q)
	if err != nil {
		return err
	}
	for name, p := range problems {
		_, _ = fmt.Fprintf(a.io.Stderr, "catalog %s: %s\n", name, p)
	}
	if a.jsonOut {
		return a.printJSON(hits)
	}
	tw := tabwriter.NewWriter(a.io.Stdout, 0, 4, 2, ' ', 0)
	for _, h := range hits {
		installed := ""
		if h.Installed != "" {
			installed = "installed " + h.Installed
		}
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", h.ID, h.Version, h.Catalog, h.Name, installed)
	}
	return tw.Flush()
}

// writePermissions lists the permissions of a plan; new ones are marked with +.
func writePermissions(w io.Writer, p *catalog.Plan) {
	if len(p.Permissions) == 0 {
		_, _ = fmt.Fprintln(w, "  no permissions declared")
		return
	}
	isNew := map[repo.Permission]bool{}
	for _, perm := range p.New {
		isNew[perm] = true
	}
	for _, perm := range p.Permissions {
		mark := " "
		if isNew[perm] && p.Installed != "" {
			mark = "+"
		}
		_, _ = fmt.Fprintf(w, "  %s %s: %s\n", mark, perm.Kind, perm.Value)
	}
}

// confirmPlan asks for the permissions of a plan: always for a new plugin, for an update
// only when it declares new permissions.
func (a *app) confirmPlan(p *catalog.Plan, yes bool) error {
	if yes || (p.Installed != "" && len(p.New) == 0) {
		return nil
	}
	w := a.io.Stderr
	if p.Installed == "" {
		_, _ = fmt.Fprintf(w, "%s %s from catalog %s declares these permissions:\n", p.ID, p.Release.Version, p.Catalog.Name)
	} else {
		_, _ = fmt.Fprintf(w, "%s %s (installed: %s) declares new permissions (+):\n", p.ID, p.Release.Version, p.Installed)
	}
	writePermissions(w, p)
	if fd, ok := fdOf(a.io.Stdin); !ok || !term.IsTerminal(fd) {
		return &exitError{code: exitNotConfirmed, err: fmt.Errorf("%s: the permissions need a confirmation; run in a terminal or pass --yes", p.ID)}
	}
	if !a.confirm("Install " + p.ID + " " + p.Release.Version + "?") {
		return &exitError{code: exitNotConfirmed, err: fmt.Errorf("%s: not confirmed", p.ID)}
	}
	return nil
}

func (a *app) pluginInstall(cmd *cobra.Command, ref, catalogName string, yes bool) error {
	st, err := a.load(true)
	if err != nil {
		return err
	}
	plan, err := st.PrepareInstall(cmd.Context(), ref, catalogName)
	if err != nil {
		return err
	}
	defer plan.Close()
	if err := a.confirmPlan(plan, yes); err != nil {
		return err
	}
	if err := st.Catalog.Commit(plan); err != nil {
		return err
	}
	return a.reportPlan(plan)
}

func (a *app) reportPlan(p *catalog.Plan) error {
	if a.jsonOut {
		return a.printJSON(map[string]any{
			"id": p.ID, "catalog": p.Catalog.Name, "version": p.Release.Version,
			"previousVersion": p.Installed, "digest": p.Release.Digest, "permissions": p.Permissions,
		})
	}
	if p.Installed != "" {
		_, err := fmt.Fprintf(a.io.Stdout, "%s: updated %s -> %s (catalog %s, signature checked)\n", p.ID, p.Installed, p.Release.Version, p.Catalog.Name)
		return err
	}
	_, err := fmt.Fprintf(a.io.Stdout, "%s: %s installed from catalog %s (signature checked)\n", p.ID, p.Release.Version, p.Catalog.Name)
	return err
}

func (a *app) pluginUpdate(cmd *cobra.Command, args []string, yes bool) error {
	st, err := a.load(true)
	if err != nil {
		return err
	}
	ids := args
	if len(ids) == 0 {
		if ids, err = st.InstalledFromCatalogs(); err != nil {
			return err
		}
	}
	var failed []string
	for _, id := range ids {
		plan, err := st.PrepareUpdate(cmd.Context(), id)
		if err != nil {
			if len(args) == 1 {
				return err
			}
			failed = append(failed, id+": "+err.Error())
			continue
		}
		if plan == nil {
			if !a.jsonOut {
				_, _ = fmt.Fprintf(a.io.Stdout, "%s: up to date\n", id)
			}
			continue
		}
		err = a.confirmPlan(plan, yes)
		if err == nil {
			err = st.Catalog.Commit(plan)
		}
		plan.Close()
		if err != nil {
			if len(args) == 1 {
				return err
			}
			failed = append(failed, id+": "+err.Error())
			continue
		}
		if err := a.reportPlan(plan); err != nil {
			return err
		}
	}
	if len(failed) > 0 {
		return fmt.Errorf("update failed for %s", strings.Join(failed, "; "))
	}
	return nil
}

func (a *app) pluginRemove(_ *cobra.Command, args []string) error {
	st, err := a.load(true)
	if err != nil {
		return err
	}
	if err := st.RemoveCatalogPlugin(args[0]); err != nil {
		return err
	}
	if a.jsonOut {
		return a.printJSON(map[string]any{"id": args[0], "removed": true})
	}
	_, err = fmt.Fprintf(a.io.Stdout, "%s: removed\n", args[0])
	return err
}
