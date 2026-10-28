package cli

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/Zenit-Hosting-de/devbox/internal/plugin"
	"github.com/Zenit-Hosting-de/devbox/internal/runner"
)

// maxParallelChecks bounds how many checks run at the same time.
const maxParallelChecks = 4

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
	r, err := a.newRunner(st)
	if err != nil {
		return err
	}
	host := hostMap(st, a.io.Getenv)
	type job struct {
		p    *plugin.Plugin
		name string
		def  map[string]any
	}
	var jobs []job
	for _, p := range a.evaluate(st) {
		if p.State != plugin.StateReady {
			continue
		}
		checks, _ := p.Manifest.Doc["checks"].(map[string]any)
		names := make([]string, 0, len(checks))
		for name := range checks {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			def, _ := checks[name].(map[string]any)
			jobs = append(jobs, job{p: p, name: name, def: def})
		}
	}

	lines := make([]checkLine, len(jobs))
	sem := make(chan struct{}, maxParallelChecks)
	var wg sync.WaitGroup
	for i, j := range jobs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			spec := runner.Spec{
				Kind: runner.KindCheck, PluginID: j.p.ID, PluginDir: j.p.Dir, Name: j.name,
				Command: anyStrings(j.def["run"]), Settings: st.result.Plugin(j.p.ID),
				Lang: uiLanguage(st), Host: host, ExtraEnv: proxyEnv(st),
			}
			if t, ok := j.def["timeout"].(string); ok {
				spec.Timeout, _ = time.ParseDuration(t)
			}
			res, err := r.Run(ctx, spec, runner.Callbacks{})
			lines[i] = checkOutcome(j.p.ID+"."+j.name, res, err)
		}()
	}
	wg.Wait()

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

// checkOutcome turns a check run into a status line; a check that fails to run is
// reported as error with the reason.
func checkOutcome(id string, res *runner.Result, err error) checkLine {
	l := checkLine{ID: id, Status: "error", CheckedAt: time.Now().UTC().Format(time.RFC3339)}
	switch {
	case err != nil:
		l.Summary = err.Error()
	case res.State != runner.StateSuccess || res.Check == nil:
		l.Summary = fmt.Sprintf("check %s (exit %d)", res.State, res.ExitCode)
		if len(res.Warnings) > 0 {
			l.Summary += ": " + res.Warnings[0]
		}
	default:
		l.Status, _ = res.Check["status"].(string)
		l.Summary, _ = res.Check["summary"].(string)
	}
	return l
}
