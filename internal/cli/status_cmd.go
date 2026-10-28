package cli

import (
	"context"
	"fmt"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/Zenit-Hosting-de/devbox/internal/core"
)

func (a *app) statusCommand() *cobra.Command {
	return &cobra.Command{
		Use:  "status",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error { return a.status(cmd.Context()) },
	}
}

// statusOutput is `devbox status --json` (docs/concept.md, CLI example).
type statusOutput struct {
	Host    map[string]any `json:"host"`
	Summary statusSummary  `json:"summary"`
	Checks  []checkLine    `json:"checks"`
}

type statusSummary struct {
	OK      int `json:"ok"`
	Warn    int `json:"warn"`
	Error   int `json:"error"`
	Missing int `json:"missing"`
}

type checkLine struct {
	ID        string `json:"id"`
	Status    string `json:"status"`
	Summary   string `json:"summary"`
	CheckedAt string `json:"checkedAt"`
}

// status runs every check of every ready plugin.
func (a *app) status(ctx context.Context) error {
	st, err := a.load(true)
	if err != nil {
		return err
	}
	r, err := st.NewRunner()
	if err != nil {
		return err
	}
	host := st.Host()
	outcomes := core.RunChecks(ctx, st, r, nil)
	lines := make([]checkLine, len(outcomes))
	for i, o := range outcomes {
		lines[i] = checkLine{ID: o.ID, Status: o.Status, Summary: o.Summary, CheckedAt: o.CheckedAt}
	}

	var sum statusSummary
	for _, l := range lines {
		switch l.Status {
		case "ok":
			sum.OK++
		case "warn":
			sum.Warn++
		case "missing":
			sum.Missing++
		default:
			sum.Error++
		}
	}
	if a.jsonOut {
		return a.printJSON(statusOutput{Host: host, Summary: sum, Checks: append([]checkLine{}, lines...)})
	}
	tw := tabwriter.NewWriter(a.io.Stdout, 0, 4, 2, ' ', 0)
	for _, l := range lines {
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\n", l.ID, l.Status, l.Summary)
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(a.io.Stdout, "\n%d ok, %d warn, %d error, %d missing\n", sum.OK, sum.Warn, sum.Error, sum.Missing)
	return nil
}
