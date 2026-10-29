package repo

import (
	"sort"
)

// States of a repository in the list (api/openapi.json, Repository.state).
const (
	StateNew         = "new"
	StateActive      = "active"
	StatePending     = "pending"
	StateError       = "error"
	StateDisabled    = "disabled"
	StateUnsupported = "unsupported"
)

// Changes of a plugin in a pending revision compared to the active one.
const (
	ChangeAdded     = "added"
	ChangeRemoved   = "removed"
	ChangeChanged   = "changed"
	ChangeUnchanged = "unchanged"
)

// View is a repository as GET /repositories lists it (api/openapi.json, Repository).
type View struct {
	Name      string       `json:"name"`
	Kind      string       `json:"kind"`
	Source    string       `json:"source,omitempty"`
	Ref       string       `json:"ref,omitempty"`
	Path      string       `json:"path,omitempty"`
	Trust     string       `json:"trust,omitempty"`
	Enabled   bool         `json:"enabled"`
	Origin    string       `json:"origin"`
	Locked    bool         `json:"locked"`
	State     string       `json:"state"`
	Active    *ActiveView  `json:"active,omitempty"`
	Pending   *PendingView `json:"pending,omitempty"`
	LastSync  *LastSync    `json:"lastSync,omitempty"`
	LastError string       `json:"lastError,omitempty"`
}

// ActiveView is the revision whose plugins are loaded.
type ActiveView struct {
	Revision string       `json:"revision"`
	Path     string       `json:"path,omitempty"`
	SyncedAt string       `json:"syncedAt"`
	Plugins  []PluginView `json:"plugins"`
	// SuggestedPaths are folders that hold plugin folders; only set without plugins.
	SuggestedPaths []string `json:"suggestedPaths,omitempty"`
}

// PendingView is the revision that waits for approval, with the plugins of both
// revisions compared.
type PendingView struct {
	Revision  string       `json:"revision"`
	Path      string       `json:"path,omitempty"`
	FetchedAt string       `json:"fetchedAt"`
	Plugins   []PluginView `json:"plugins"`
	// SuggestedPaths are folders that hold plugin folders; only set without plugins.
	SuggestedPaths []string `json:"suggestedPaths,omitempty"`
}

// PluginView is a plugin of a revision.
type PluginView struct {
	ID          string `json:"id"`
	Name        string `json:"name,omitempty"`
	Version     string `json:"version,omitempty"`
	Description string `json:"description,omitempty"`
	Digest      string `json:"digest"`
	// Change is set in a pending revision only.
	Change string `json:"change,omitempty"`
	// PreviousVersion is the version of the active revision of a changed plugin.
	PreviousVersion  string           `json:"previousVersion,omitempty"`
	Permissions      []PermissionView `json:"permissions"`
	RequiresCommands []string         `json:"requiresCommands"`
	Error            string           `json:"error,omitempty"`
}

// PermissionView is a declared permission; New is set when the active revision does
// not declare it.
type PermissionView struct {
	Kind  string `json:"kind"`
	Value string `json:"value"`
	New   bool   `json:"new"`
}

// View returns the repository as the API shows it. A state that cannot be read shows
// as state "error".
func (m *Manager) View(e Entry) View {
	v := View{
		Name: e.Name, Kind: e.Kind, Source: e.Source, Ref: e.Ref, Path: e.Path,
		Enabled: e.Enabled, Origin: e.Origin, Locked: e.Locked,
	}
	if e.Supported() {
		v.Trust = e.Trust
	}
	if !e.Supported() {
		v.State = StateUnsupported
		return v
	}
	st, err := m.Store.ReadState(m.Validator, e.Name)
	if err != nil {
		v.State, v.LastError = StateError, err.Error()
		return v
	}
	v.LastSync, v.LastError = st.LastSync, st.LastError
	if st.Active != nil {
		v.Active = activeView(st.Active)
	}
	if st.Pending != nil {
		v.Pending = pendingView(st.Active, st.Pending)
	}
	problem := e.Problem()
	switch {
	case !e.Enabled:
		v.State = StateDisabled
	case problem != nil:
		v.State, v.LastError = StateError, ErrorText(problem)
	case st.Pending != nil:
		v.State = StatePending
	case st.LastError != "":
		v.State = StateError
	case st.Active != nil:
		v.State = StateActive
	default:
		v.State = StateNew
	}
	return v
}

// Views returns the views of the entries, in their order.
func (m *Manager) Views(entries []Entry) []View {
	out := make([]View, 0, len(entries))
	for _, e := range entries {
		out = append(out, m.View(e))
	}
	return out
}

func activeView(r *Revision) *ActiveView {
	a := &ActiveView{
		Revision: r.Revision, Path: r.Path, SyncedAt: r.FetchedAt, SuggestedPaths: r.SuggestedPaths,
		Plugins: make([]PluginView, 0, len(r.Plugins)),
	}
	for _, p := range r.Plugins {
		a.Plugins = append(a.Plugins, pluginView(p, "", nil))
	}
	return a
}

func pluginView(p Plugin, change string, known map[Permission]bool) PluginView {
	v := PluginView{
		ID: p.ID, Name: p.Name, Version: p.Version, Description: p.Description, Digest: p.Digest,
		Change: change, RequiresCommands: p.RequiresCommands, Error: p.Error,
		Permissions: make([]PermissionView, 0, len(p.Permissions)),
	}
	if v.RequiresCommands == nil {
		v.RequiresCommands = []string{}
	}
	for _, perm := range p.Permissions {
		v.Permissions = append(v.Permissions, PermissionView{Kind: perm.Kind, Value: perm.Value, New: change != "" && !known[perm]})
	}
	return v
}

// pendingView compares the plugins of the pending revision with the active one: a
// plugin is added, changed (another digest), unchanged, or removed; the permissions
// that the active revision does not declare for the plugin are marked new.
func pendingView(active, pending *Revision) *PendingView {
	byID := map[string]Plugin{}
	if active != nil {
		for _, p := range active.Plugins {
			byID[p.ID] = p
		}
	}
	out := &PendingView{
		Revision: pending.Revision, Path: pending.Path, FetchedAt: pending.FetchedAt,
		SuggestedPaths: pending.SuggestedPaths, Plugins: []PluginView{},
	}
	inPending := map[string]bool{}
	for _, p := range pending.Plugins {
		inPending[p.ID] = true
		old, existed := byID[p.ID]
		switch {
		case !existed:
			out.Plugins = append(out.Plugins, pluginView(p, ChangeAdded, nil))
		case old.Digest != p.Digest:
			v := pluginView(p, ChangeChanged, permissionSet(old))
			v.PreviousVersion = old.Version
			out.Plugins = append(out.Plugins, v)
		default:
			out.Plugins = append(out.Plugins, pluginView(p, ChangeUnchanged, permissionSet(old)))
		}
	}
	for id, old := range byID {
		if !inPending[id] {
			out.Plugins = append(out.Plugins, pluginView(old, ChangeRemoved, permissionSet(old)))
		}
	}
	sort.SliceStable(out.Plugins, func(i, j int) bool { return out.Plugins[i].ID < out.Plugins[j].ID })
	return out
}

// PluginIDs returns the IDs of the plugins of the active and the pending revision,
// sorted and without duplicates.
func (v View) PluginIDs() []string {
	seen := map[string]bool{}
	var ids []string
	add := func(plugins []PluginView) {
		for _, p := range plugins {
			if !seen[p.ID] {
				seen[p.ID] = true
				ids = append(ids, p.ID)
			}
		}
	}
	if v.Active != nil {
		add(v.Active.Plugins)
	}
	if v.Pending != nil {
		add(v.Pending.Plugins)
	}
	sort.Strings(ids)
	if ids == nil {
		ids = []string{}
	}
	return ids
}

func permissionSet(p Plugin) map[Permission]bool {
	set := make(map[Permission]bool, len(p.Permissions))
	for _, perm := range p.Permissions {
		set[perm] = true
	}
	return set
}
