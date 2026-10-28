package cli

import (
	"fmt"
	"path/filepath"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/Zenit-Hosting-de/devbox/internal/config"
	"github.com/Zenit-Hosting-de/devbox/internal/history"
)

func (a *app) historyCommand() *cobra.Command {
	var limit int
	cmd := &cobra.Command{
		Use:  "history",
		Args: cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error { return a.historyList(limit) },
	}
	cmd.Flags().IntVar(&limit, "limit", 20, "number of sessions, 0 for all")
	return cmd
}

func (a *app) logsCommand() *cobra.Command {
	return &cobra.Command{
		Use:  "logs SESSION",
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error { return a.historyLogs(args[0]) },
	}
}

func (a *app) historyStore() (*history.Store, error) {
	e, err := a.env()
	if err != nil {
		return nil, err
	}
	return history.New(filepath.Join(e.Ctx.XDG.State, "devbox"), e.Validator), nil
}

// historyOutput is `devbox history --json`.
type historyOutput struct {
	Sessions []history.Entry `json:"sessions"`
}

func (a *app) historyList(limit int) error {
	store, err := a.historyStore()
	if err != nil {
		return err
	}
	entries, problems, err := store.List(limit)
	if err != nil {
		return err
	}
	for _, p := range problems {
		_, _ = fmt.Fprintln(a.io.Stderr, "devbox: warning:", p)
	}
	if a.jsonOut {
		return a.printJSON(historyOutput{Sessions: append([]history.Entry{}, entries...)})
	}
	tw := tabwriter.NewWriter(a.io.Stdout, 0, 4, 2, ' ', 0)
	for _, e := range entries {
		mode := ""
		if e.DryRun {
			mode = "dry-run"
		}
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n", e.ID, e.EndedAt, e.State, e.Action, mode, e.Summary)
	}
	return tw.Flush()
}

// historyLogs prints the output of a session; with --json its history entry, events
// path and backups.
func (a *app) historyLogs(id string) error {
	if !history.ValidID(id) {
		return &config.Error{Code: config.CodeValidation, Key: id, Detail: "not a session ID"}
	}
	store, err := a.historyStore()
	if err != nil {
		return err
	}
	if a.jsonOut {
		e, ok, err := store.Get(id)
		if err != nil {
			return err
		}
		if !ok {
			return &config.Error{Code: config.CodeNotFound, Key: id, Detail: "no such session"}
		}
		backups, err := store.Backups(id)
		if err != nil {
			return err
		}
		return a.printJSON(sessionFiles{Entry: e, Log: store.LogPath(id), Events: store.EventsPath(id), Backups: append([]string{}, backups...)})
	}
	if err := store.ReadLog(id, a.io.Stdout); err != nil {
		return &config.Error{Code: config.CodeNotFound, Key: id, Detail: "no output for this session"}
	}
	return nil
}

// sessionFiles is `devbox logs SESSION --json`.
type sessionFiles struct {
	Entry   history.Entry `json:"entry"`
	Log     string        `json:"log"`
	Events  string        `json:"events"`
	Backups []string      `json:"backups"`
}
