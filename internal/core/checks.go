package core

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/Zenit-Hosting-de/devbox/internal/plugin"
	"github.com/Zenit-Hosting-de/devbox/internal/runner"
)

// MaxParallelChecks bounds how many checks run at the same time.
const MaxParallelChecks = 4

// CheckDetail is a label-value pair of a check result.
type CheckDetail struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

// CheckHint is a hint of a check result, optionally with a direct action.
type CheckHint struct {
	Text   string `json:"text"`
	Action string `json:"action,omitempty"`
}

// CheckOutcome is the result of one check run.
type CheckOutcome struct {
	ID        string
	Status    string
	Summary   string
	CheckedAt string
	// Label is the label from the manifest, untranslated.
	Label   string
	Details []CheckDetail
	Hints   []CheckHint
	Data    map[string]any
	// Cache is how long the manifest lets the result live; zero when it sets none.
	Cache time.Duration
	// RefreshOn lists the events that invalidate the result.
	RefreshOn []string
}

// Summary counts check results by status (api/openapi.json, StatusReport.summary).
type Summary struct {
	OK      int `json:"ok"`
	Warn    int `json:"warn"`
	Error   int `json:"error"`
	Missing int `json:"missing"`
}

// Summarize counts the outcomes by status; every status other than ok, warn and
// missing counts as error.
func Summarize(outcomes []CheckOutcome) Summary {
	var sum Summary
	for _, o := range outcomes {
		switch o.Status {
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
	return sum
}

// CheckRef is one check of a ready plugin.
type CheckRef struct {
	Plugin *plugin.Plugin
	Name   string
	Def    map[string]any
}

// ID is the fully qualified check, <plugin>.<name>.
func (c CheckRef) ID() string { return c.Plugin.ID + "." + c.Name }

// Checks lists the checks of all ready plugins in plugin order, sorted by name.
func Checks(plugins []*plugin.Plugin) []CheckRef {
	var out []CheckRef
	for _, p := range plugins {
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
			out = append(out, CheckRef{Plugin: p, Name: name, Def: def})
		}
	}
	return out
}

// RunChecks runs the checks of all ready plugins for which filter returns true (nil:
// all), four at a time, and returns the outcomes in plugin order.
func RunChecks(ctx context.Context, st *State, r *runner.Runner, filter func(ref string) bool) []CheckOutcome {
	return RunChecksNotify(ctx, st, r, filter, nil)
}

// RunChecksNotify is RunChecks that also calls done as each check finishes. done may
// be called from several goroutines at once.
func RunChecksNotify(ctx context.Context, st *State, r *runner.Runner, filter func(ref string) bool, done func(CheckOutcome)) []CheckOutcome {
	return RunChecksLang(ctx, st, r, "", filter, done)
}

// RunChecksLang is RunChecksNotify with an explicit language for DEVBOX_LANG of the
// checks, e.g. the language of the UI; an empty lang falls back to ui.language of the
// configuration.
func RunChecksLang(ctx context.Context, st *State, r *runner.Runner, lang string, filter func(ref string) bool, done func(CheckOutcome)) []CheckOutcome {
	var jobs []CheckRef
	for _, c := range Checks(st.Evaluate()) {
		if filter == nil || filter(c.ID()) {
			jobs = append(jobs, c)
		}
	}
	host := st.Host()
	if lang == "" {
		lang = st.UILanguage()
	}
	proxy := st.ProxyEnv()

	out := make([]CheckOutcome, len(jobs))
	sem := make(chan struct{}, MaxParallelChecks)
	var wg sync.WaitGroup
	for i, j := range jobs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			spec := runner.Spec{
				Kind: runner.KindCheck, PluginID: j.Plugin.ID, PluginDir: j.Plugin.Dir, Name: j.Name,
				Command: AnyStrings(j.Def["run"]), Settings: st.Result.Plugin(j.Plugin.ID),
				Lang: lang, Host: host, ExtraEnv: proxy,
			}
			if t, ok := j.Def["timeout"].(string); ok {
				spec.Timeout, _ = time.ParseDuration(t)
			}
			res, err := r.Run(ctx, spec, runner.Callbacks{})
			out[i] = outcome(j, res, err)
			if done != nil {
				done(out[i])
			}
		}()
	}
	wg.Wait()
	return out
}

// outcome turns a check run into a result; a check that fails to run is reported as
// error with the reason.
func outcome(c CheckRef, res *runner.Result, err error) CheckOutcome {
	o := CheckOutcome{ID: c.ID(), Status: "error", CheckedAt: time.Now().UTC().Format(time.RFC3339)}
	o.Label, _ = c.Def["label"].(string)
	o.RefreshOn = AnyStrings(c.Def["refreshOn"])
	if d, ok := c.Def["cache"].(string); ok {
		o.Cache, _ = time.ParseDuration(d)
	}
	switch {
	case err != nil:
		o.Summary = err.Error()
	case res.State != runner.StateSuccess || res.Check == nil:
		o.Summary = fmt.Sprintf("check %s (exit %d)", res.State, res.ExitCode)
		if len(res.Warnings) > 0 {
			o.Summary += ": " + res.Warnings[0]
		}
	default:
		o.Status, _ = res.Check["status"].(string)
		o.Summary, _ = res.Check["summary"].(string)
		o.Data, _ = res.Check["data"].(map[string]any)
		if list, ok := res.Check["details"].([]any); ok {
			for _, e := range list {
				m, _ := e.(map[string]any)
				label, _ := m["label"].(string)
				value, _ := m["value"].(string)
				o.Details = append(o.Details, CheckDetail{Label: label, Value: value})
			}
		}
		if list, ok := res.Check["hints"].([]any); ok {
			for _, e := range list {
				m, _ := e.(map[string]any)
				text, _ := m["text"].(string)
				action, _ := m["action"].(string)
				o.Hints = append(o.Hints, CheckHint{Text: text, Action: action})
			}
		}
	}
	return o
}
