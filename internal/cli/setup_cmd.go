package cli

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/Zenit-Hosting-de/devbox/internal/config"
	"github.com/Zenit-Hosting-de/devbox/internal/core"
	"github.com/Zenit-Hosting-de/devbox/internal/history"
	"github.com/Zenit-Hosting-de/devbox/internal/profile"
)

type setupFlags struct {
	profile string
	plan    bool
	dryRun  bool
	yes     bool
}

func (a *app) setupCommand() *cobra.Command {
	var f setupFlags
	cmd := &cobra.Command{
		Use: "setup", Short: "first-run setup: run the open steps of the plan in order", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error { return a.setup(cmd.Context(), f) },
	}
	cmd.Flags().StringVar(&f.profile, "profile", "", "profile source to add first (file, https or git address)")
	cmd.Flags().BoolVar(&f.plan, "plan", false, "only show the plan with the state of every step")
	cmd.Flags().BoolVar(&f.dryRun, "dry-run", false, "run every open step as a dry run")
	cmd.Flags().BoolVar(&f.yes, "yes", false, "start without asking and answer confirmations with yes")
	return cmd
}

// setupPlan loads the state and returns the plan.
func (a *app) setupPlan() (core.SetupPlan, error) {
	st, err := a.load(true)
	if err != nil {
		return core.SetupPlan{}, err
	}
	store, err := a.historyStore()
	if err != nil {
		return core.SetupPlan{}, err
	}
	return st.SetupPlan(store)
}

func writePlan(a *app, plan core.SetupPlan) error {
	tw := tabwriter.NewWriter(a.io.Stdout, 0, 4, 2, ' ', 0)
	for i, s := range plan.Steps {
		_, _ = fmt.Fprintf(tw, "%d\t%s\t%s\t%s\n", i+1, s.Ref, s.State, s.Detail)
	}
	return tw.Flush()
}

// setup runs the open steps of the plan in order. A step that fails stops the run;
// the next call continues with the first open step, because finished steps are found
// in the history. Credential steps are not run here: an instance without secret is
// reported with the command that sets it.
func (a *app) setup(ctx context.Context, f setupFlags) error {
	if f.profile != "" {
		_, err := core.AddProfile(ctx, a.coreEnv(), profile.Source{Source: f.profile})
		if err != nil && !config.IsCode(err, config.CodeConflict) {
			return err
		}
	}
	plan, err := a.setupPlan()
	if err != nil {
		return err
	}
	if f.plan {
		if a.jsonOut {
			return a.printJSON(plan)
		}
		return writePlan(a, plan)
	}
	if !a.jsonOut {
		if err := writePlan(a, plan); err != nil {
			return err
		}
	}
	if plan.Open() == 0 {
		if !a.jsonOut {
			_, _ = fmt.Fprintln(a.io.Stdout, "\nsetup: nothing to do, every step is done")
		}
		return nil
	}
	if !f.yes && !f.dryRun {
		if fd, ok := fdOf(a.io.Stdin); !ok || !term.IsTerminal(fd) {
			return &exitError{code: exitNotConfirmed, err: errors.New("setup: starting needs a confirmation; run in a terminal or pass --yes")}
		}
		if !a.confirm(fmt.Sprintf("Run the %d open steps?", plan.Open())) {
			return &exitError{code: exitNotConfirmed, err: errors.New("setup: not confirmed")}
		}
	}

	a.origin = history.OriginSetup
	defer func() { a.origin = "" }()
	var ran, blocked, waiting []string
	for i := range plan.Steps {
		// a step can make a later plugin ready (it installs a command), so the state is
		// read again before every step
		current, err := a.setupPlan()
		if err != nil {
			return err
		}
		if i >= len(current.Steps) {
			break
		}
		step := current.Steps[i]
		switch {
		case step.State == core.SetupDone:
			continue
		case step.Kind == core.SetupKindCredential && step.State == core.SetupOpen:
			waiting = append(waiting, step.Ref)
			if !a.jsonOut {
				name := strings.TrimPrefix(step.Ref, core.SetupCredentialPrefix)
				_, _ = fmt.Fprintf(a.io.Stdout, "== %s: waits for its secret (devbox cred set %s)\n", step.Ref, name)
			}
			continue
		case step.State == core.SetupBlocked:
			blocked = append(blocked, step.Ref)
			if !a.jsonOut {
				_, _ = fmt.Fprintf(a.io.Stdout, "== %s: blocked, %s\n", step.Ref, step.Detail)
			}
			continue
		}
		if !a.jsonOut {
			_, _ = fmt.Fprintf(a.io.Stdout, "== %s\n", step.Ref)
		}
		if err := a.run(ctx, step.Ref, runFlags{dryRun: f.dryRun, yes: f.yes}); err != nil {
			_, _ = fmt.Fprintf(a.io.Stderr, "devbox: setup stopped at %s; run devbox setup again to continue there\n", step.Ref)
			return err
		}
		ran = append(ran, step.Ref)
	}
	if !a.jsonOut {
		_, _ = fmt.Fprintf(a.io.Stdout, "\nsetup: %d steps run, %d blocked, %d wait for a secret\n", len(ran), len(blocked), len(waiting))
	}
	if len(blocked) > 0 {
		return &exitError{code: ExitPrerequisite, err: fmt.Errorf("setup: blocked steps: %s", strings.Join(blocked, ", "))}
	}
	return nil
}
