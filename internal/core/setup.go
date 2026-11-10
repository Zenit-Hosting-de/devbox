package core

import (
	"fmt"
	"sort"
	"strings"

	"github.com/Zenit-Hosting-de/devbox/internal/config"
	"github.com/Zenit-Hosting-de/devbox/internal/history"
	"github.com/Zenit-Hosting-de/devbox/internal/plugin"
	"github.com/Zenit-Hosting-de/devbox/internal/runner"
)

// Kinds and states of a setup step.
const (
	SetupKindAction     = "action"
	SetupKindCredential = "credential"

	SetupDone    = "done"
	SetupOpen    = "open"
	SetupBlocked = "blocked"

	// SetupCredentialPrefix marks a credential step: credentials:<instance>.
	SetupCredentialPrefix = "credentials:"
)

// SetupStep is one step of the first-run setup.
type SetupStep struct {
	// Ref is <plugin>.<action> or credentials:<instance>.
	Ref   string `json:"ref"`
	Kind  string `json:"kind"`
	Label string `json:"label,omitempty"`
	// State is done, open or blocked (the plugin is not ready, or the instance is
	// unknown).
	State  string `json:"state"`
	Detail string `json:"detail,omitempty"`
}

// SetupPlan is the ordered plan with the state of every step.
type SetupPlan struct {
	// Origin is "plugins" when the plan is built from contributions, or the layer of
	// the profile that defines it.
	Origin string      `json:"origin"`
	Steps  []SetupStep `json:"steps"`
}

// Open counts the steps that are not done.
func (p SetupPlan) Open() int {
	n := 0
	for _, s := range p.Steps {
		if s.State != SetupDone {
			n++
		}
	}
	return n
}

// SetupPlan returns the plan of the first-run setup. A profile with a setup list
// defines the order (the last such profile wins); otherwise ready plugins contribute
// steps (contributes.setup), ordered so that every step comes after the steps it names
// in "after". An action step is done when the history holds a successful run of the
// action that was no dry run; a credential step is done when the instance has a
// secret.
func (st *State) SetupPlan(store *history.Store) (SetupPlan, error) {
	plan := SetupPlan{Origin: GroupOriginPlugins, Steps: []SetupStep{}}
	var refs []string
	for _, pf := range st.Profiles {
		doc := pf.Plain()
		if list := AnyStrings(doc["setup"]); len(list) > 0 {
			id, _ := doc["id"].(string)
			refs, plan.Origin = list, config.LayerProfilePrefix+id
		}
	}
	plugins := st.Evaluate()
	if refs == nil {
		var err error
		if refs, err = contributedSetup(plugins); err != nil {
			return plan, err
		}
	}

	done := map[string]bool{}
	entries, _, err := store.List(0)
	if err != nil {
		return plan, err
	}
	for _, e := range entries {
		if !e.DryRun && e.State == string(runner.StateSuccess) {
			done[e.Action] = true
		}
	}
	instances, err := Credentials(st)
	if err != nil {
		return plan, err
	}

	for _, ref := range refs {
		step := SetupStep{Ref: ref, Kind: SetupKindAction, State: SetupOpen}
		if name, ok := strings.CutPrefix(ref, SetupCredentialPrefix); ok {
			step.Kind, step.State, step.Detail = SetupKindCredential, SetupBlocked, "no such credential instance"
			for _, inst := range instances {
				if inst.Name != name {
					continue
				}
				step.Label, step.State, step.Detail = inst.Label, SetupOpen, "the secret is missing"
				if inst.Secret != nil {
					step.State, step.Detail = SetupDone, ""
				}
			}
			plan.Steps = append(plan.Steps, step)
			continue
		}
		pluginID, name, _ := strings.Cut(ref, ".")
		p := FindPlugin(plugins, pluginID)
		switch {
		case p == nil:
			step.State, step.Detail = SetupBlocked, "no such plugin"
		case p.State != plugin.StateReady:
			step.State, step.Detail = SetupBlocked, fmt.Sprintf("the plugin is %s", p.State)
		default:
			def, ok := ActionDef(p, name)
			if !ok {
				step.State, step.Detail = SetupBlocked, "no such action"
				break
			}
			step.Label, _ = def["label"].(string)
			if done[ref] {
				step.State = SetupDone
			}
		}
		plan.Steps = append(plan.Steps, step)
	}
	return plan, nil
}

// contributedSetup orders the setup steps the ready plugins contribute: plugin order,
// but every step after the steps of its "after" list that are part of the plan.
func contributedSetup(plugins []*plugin.Plugin) ([]string, error) {
	var order []string
	after := map[string][]string{}
	for _, p := range plugins {
		if p.State != plugin.StateReady {
			continue
		}
		contributes, _ := p.Manifest.Doc["contributes"].(map[string]any)
		list, _ := contributes["setup"].([]any)
		for _, raw := range list {
			entry, _ := raw.(map[string]any)
			ref, _ := entry["action"].(string)
			if ref == "" {
				continue
			}
			if _, seen := after[ref]; !seen {
				order = append(order, ref)
			}
			after[ref] = append(after[ref], AnyStrings(entry["after"])...)
		}
	}
	sort.Strings(order)
	var out []string
	state := map[string]int{} // 1: on the stack, 2: placed
	var visit func(ref string, path []string) error
	visit = func(ref string, path []string) error {
		switch state[ref] {
		case 2:
			return nil
		case 1:
			return &config.Error{Code: config.CodeValidation, Key: ref, Detail: "setup steps depend on each other: " + strings.Join(append(path, ref), " -> ")}
		}
		state[ref] = 1
		deps := append([]string(nil), after[ref]...)
		sort.Strings(deps)
		for _, dep := range deps {
			if _, known := after[dep]; !known {
				continue // a step of a plugin that is not there
			}
			if err := visit(dep, append(path, ref)); err != nil {
				return err
			}
		}
		state[ref] = 2
		out = append(out, ref)
		return nil
	}
	for _, ref := range order {
		if err := visit(ref, nil); err != nil {
			return nil, err
		}
	}
	return out, nil
}
