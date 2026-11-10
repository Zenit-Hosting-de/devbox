package core

import (
	"sort"

	"github.com/Zenit-Hosting-de/devbox/internal/config"
	"github.com/Zenit-Hosting-de/devbox/internal/plugin"
)

// Group is a named sequence of actions, e.g. update-all.
type Group struct {
	ID    string `json:"id"`
	Label string `json:"label,omitempty"`
	// Actions are run in this order (<plugin>.<action>).
	Actions []string `json:"actions"`
	// Origin is "plugins" for a group built from contributions, or the layer of the
	// profile that defines it (profile:<id>).
	Origin string `json:"origin"`
}

// GroupOriginPlugins marks a group that plugins contribute to.
const GroupOriginPlugins = "plugins"

// Groups returns the action groups, sorted by ID. Ready plugins append their actions
// to a group (contributes.groups) in plugin order; a profile that defines a group of
// the same ID replaces the contributions, and a later profile wins.
func (st *State) Groups() []Group {
	byID := map[string]*Group{}
	for _, p := range st.Evaluate() {
		if p.State != plugin.StateReady {
			continue
		}
		contributes, _ := p.Manifest.Doc["contributes"].(map[string]any)
		groups, _ := contributes["groups"].(map[string]any)
		ids := make([]string, 0, len(groups))
		for id := range groups {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for _, id := range ids {
			g := byID[id]
			if g == nil {
				g = &Group{ID: id, Origin: GroupOriginPlugins}
				byID[id] = g
			}
			g.Actions = append(g.Actions, AnyStrings(groups[id])...)
		}
	}
	for _, pf := range st.Profiles {
		doc := pf.Plain()
		id, _ := doc["id"].(string)
		groups, _ := doc["groups"].(map[string]any)
		for gid, raw := range groups {
			def, _ := raw.(map[string]any)
			label, _ := def["label"].(string)
			byID[gid] = &Group{ID: gid, Label: label, Actions: AnyStrings(def["actions"]), Origin: config.LayerProfilePrefix + id}
		}
	}
	out := make([]Group, 0, len(byID))
	for _, g := range byID {
		if g.Actions == nil {
			g.Actions = []string{}
		}
		out = append(out, *g)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Group returns the group with the ID.
func (st *State) Group(id string) (Group, error) {
	for _, g := range st.Groups() {
		if g.ID == id {
			return g, nil
		}
	}
	return Group{}, ErrNotFound(id, "no such group")
}
