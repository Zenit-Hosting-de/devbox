package cli

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/Zenit-Hosting-de/devbox/internal/config"
	"github.com/Zenit-Hosting-de/devbox/internal/core"
	"github.com/Zenit-Hosting-de/devbox/internal/repo"
)

func (a *app) repoCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "repo", Short: "manage plugin repositories", Args: cobra.NoArgs}
	var spec core.RepoSpec
	add := &cobra.Command{
		Use: "add NAME", Short: "add a plugin repository and sync it", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			spec.Name = args[0]
			return a.repoAdd(cmd, spec)
		},
	}
	add.Flags().StringVar(&spec.Source, "source", "", "address of the repository: Git (git+ prefix), tar.gz (https) or local directory (file)")
	add.Flags().StringVar(&spec.Ref, "ref", "", "Git branch, tag or commit (default: the HEAD of the remote)")
	add.Flags().StringVar(&spec.Path, "path", "", "sub-directory that holds the plugin folders")
	add.Flags().StringVar(&spec.Trust, "trust", "", "prompt (default) or pinned (git commit in --ref, no approval)")
	_ = add.MarkFlagRequired("source")

	var yes bool
	approve := &cobra.Command{
		Use: "approve NAME", Short: "make the pending revision active", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error { return a.repoApprove(cmd, args[0], yes) },
	}
	approve.Flags().BoolVar(&yes, "yes", false, "approve without asking")

	cmd.AddCommand(
		&cobra.Command{Use: "list", Short: "list the repositories", Args: cobra.NoArgs, RunE: a.repoList},
		add,
		&cobra.Command{Use: "sync [NAME]", Short: "fetch new revisions and show what changed", Args: cobra.MaximumNArgs(1), RunE: a.repoSync},
		approve,
		&cobra.Command{Use: "discard NAME", Short: "drop the pending revision", Args: cobra.ExactArgs(1), RunE: a.repoDiscard},
		&cobra.Command{Use: "remove NAME", Short: "remove a repository and its files", Args: cobra.ExactArgs(1), RunE: a.repoRemove},
	)
	return cmd
}

func (a *app) repoList(_ *cobra.Command, _ []string) error {
	st, err := a.load(true)
	if err != nil {
		return err
	}
	views := st.RepositoryViews()
	if a.jsonOut {
		return a.printJSON(views)
	}
	tw := tabwriter.NewWriter(a.io.Stdout, 0, 4, 2, ' ', 0)
	for _, v := range views {
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n", v.Name, v.State, v.Kind, v.Origin, v.Source, revisions(v))
	}
	return tw.Flush()
}

// revisions is the short text of the revisions of a repository.
func revisions(v repo.View) string {
	var parts []string
	if v.Active != nil {
		parts = append(parts, "active "+repo.ShortRevision(v.Active.Revision))
	}
	if v.Pending != nil {
		parts = append(parts, "pending "+repo.ShortRevision(v.Pending.Revision))
	}
	return strings.Join(parts, ", ")
}

func (a *app) repoAdd(cmd *cobra.Command, spec core.RepoSpec) error {
	st, err := a.load(true)
	if err != nil {
		return err
	}
	if err := st.AddRepository(spec); err != nil {
		return err
	}
	if st, err = a.load(true); err != nil {
		return err
	}
	syncErr := st.SyncRepository(cmd.Context(), spec.Name)
	if err := a.printRepo(st, spec.Name); err != nil {
		return err
	}
	if syncErr != nil {
		return fmt.Errorf("%s was added, the first sync failed: %w", spec.Name, syncErr)
	}
	return nil
}

func (a *app) repoSync(cmd *cobra.Command, args []string) error {
	st, err := a.load(true)
	if err != nil {
		return err
	}
	names := args
	if len(names) == 0 {
		for _, e := range st.Repositories {
			if e.Active() {
				names = append(names, e.Name)
			}
		}
	}
	var views []repo.View
	var failed []string
	for _, name := range names {
		syncErr := st.SyncRepository(cmd.Context(), name)
		if syncErr != nil && len(args) > 0 {
			return syncErr // a named repository that cannot be synced is the error of the call
		}
		view, err := st.RepositoryView(name)
		if err != nil {
			return err
		}
		views = append(views, view)
		if syncErr != nil {
			failed = append(failed, name+": "+repo.ErrorText(syncErr))
		}
	}
	if a.jsonOut {
		if len(args) == 1 {
			err = a.printJSON(views[0])
		} else {
			err = a.printJSON(emptyViews(views))
		}
	} else {
		for _, v := range views {
			writeReport(a.io.Stdout, v)
		}
	}
	if err != nil {
		return err
	}
	if len(failed) > 0 {
		return fmt.Errorf("sync failed for %s", strings.Join(failed, "; "))
	}
	return nil
}

func emptyViews(v []repo.View) []repo.View {
	if v == nil {
		return []repo.View{}
	}
	return v
}

func (a *app) repoApprove(cmd *cobra.Command, name string, yes bool) error {
	st, err := a.load(true)
	if err != nil {
		return err
	}
	view, err := st.RepositoryView(name)
	if err != nil {
		return err
	}
	if view.Pending == nil {
		return &config.Error{Code: config.CodeConflict, Key: name, Detail: "there is no pending revision to approve"}
	}
	if !yes {
		if fd, ok := fdOf(a.io.Stdin); !ok || !term.IsTerminal(fd) {
			return &exitError{code: ExitInvalid, err: fmt.Errorf("approving %s needs a terminal to ask; review with 'devbox repo sync %s' and pass --yes to approve", name, name)}
		}
		writeReport(a.io.Stderr, view)
		if !a.confirm(fmt.Sprintf("Approve revision %s of %s?", repo.ShortRevision(view.Pending.Revision), name)) {
			return &exitError{code: ExitFailed, err: errors.New(name + ": not approved")}
		}
	}
	if err := st.ApproveRepository(cmd.Context(), name, view.Pending.Revision); err != nil {
		return err
	}
	return a.printRepo(st, name)
}

func (a *app) repoDiscard(cmd *cobra.Command, args []string) error {
	st, err := a.load(true)
	if err != nil {
		return err
	}
	if err := st.DiscardRepository(cmd.Context(), args[0]); err != nil {
		return err
	}
	return a.printRepo(st, args[0])
}

func (a *app) repoRemove(cmd *cobra.Command, args []string) error {
	st, err := a.load(true)
	if err != nil {
		return err
	}
	return st.RemoveRepository(cmd.Context(), args[0])
}

// printRepo prints a repository as JSON or as a report.
func (a *app) printRepo(st *core.State, name string) error {
	view, err := st.RepositoryView(name)
	if err != nil {
		return err
	}
	if a.jsonOut {
		return a.printJSON(view)
	}
	writeReport(a.io.Stdout, view)
	return nil
}

// writeReport describes the state of a repository: what is active and, for a pending
// revision, every plugin with its change and permissions; new permissions are marked.
func writeReport(w io.Writer, v repo.View) {
	switch v.State {
	case repo.StatePending:
		_, _ = fmt.Fprintf(w, "%s: revision %s waits for approval", v.Name, repo.ShortRevision(v.Pending.Revision))
		if v.Active != nil {
			_, _ = fmt.Fprintf(w, " (active: %s)", repo.ShortRevision(v.Active.Revision))
		}
		_, _ = fmt.Fprintln(w)
		for _, p := range v.Pending.Plugins {
			writePlugin(w, p)
		}
		_, _ = fmt.Fprintf(w, "approve with: devbox repo approve %s\n", v.Name)
	case repo.StateActive:
		if v.LastSync != nil && v.LastSync.Changed {
			_, _ = fmt.Fprintf(w, "%s: revision %s is active (%d plugins)\n", v.Name, repo.ShortRevision(v.Active.Revision), len(v.Active.Plugins))
			for _, p := range v.Active.Plugins {
				writePlugin(w, p)
			}
			return
		}
		_, _ = fmt.Fprintf(w, "%s: up to date, revision %s is active (%d plugins)\n", v.Name, repo.ShortRevision(v.Active.Revision), len(v.Active.Plugins))
	case repo.StateError:
		_, _ = fmt.Fprintf(w, "%s: error: %s\n", v.Name, v.LastError)
	default:
		_, _ = fmt.Fprintf(w, "%s: %s\n", v.Name, v.State)
	}
}

var changeMarks = map[string]string{
	repo.ChangeAdded: "+", repo.ChangeRemoved: "-", repo.ChangeChanged: "~", repo.ChangeUnchanged: "=", "": "*",
}

func writePlugin(w io.Writer, p repo.PluginView) {
	title := p.ID
	if p.Version != "" {
		title += " " + p.Version
	}
	if p.PreviousVersion != "" && p.PreviousVersion != p.Version {
		title += " (was " + p.PreviousVersion + ")"
	}
	if p.Change != "" && p.Change != repo.ChangeAdded {
		title += " [" + p.Change + "]"
	}
	_, _ = fmt.Fprintf(w, "  %s %s\n", changeMarks[p.Change], title)
	if p.Error != "" {
		_, _ = fmt.Fprintf(w, "      invalid: %s\n", p.Error)
	}
	if p.Change == repo.ChangeRemoved || p.Change == repo.ChangeUnchanged {
		return
	}
	for _, perm := range p.Permissions {
		mark := ""
		if perm.New {
			mark = "  (new)"
		}
		_, _ = fmt.Fprintf(w, "      %-12s %s%s\n", perm.Kind, perm.Value, mark)
	}
	if len(p.RequiresCommands) > 0 {
		_, _ = fmt.Fprintf(w, "      %-12s %s\n", "commands", strings.Join(p.RequiresCommands, ", "))
	}
}
