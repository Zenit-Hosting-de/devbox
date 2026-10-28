package api

import (
	"fmt"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/Zenit-Hosting-de/devbox/internal/config"
	"github.com/Zenit-Hosting-de/devbox/internal/core"
	"github.com/Zenit-Hosting-de/devbox/internal/plugin"
)

// CheckSummary is the cached result of one check (api/openapi.json, CheckSummary).
type CheckSummary struct {
	ID        string             `json:"id"`
	Status    string             `json:"status"`
	Summary   string             `json:"summary"`
	CheckedAt string             `json:"checkedAt"`
	Label     string             `json:"label,omitempty"`
	Details   []core.CheckDetail `json:"details,omitempty"`
	Hints     []core.CheckHint   `json:"hints,omitempty"`
	Data      map[string]any     `json:"data,omitempty"`
}

func summaryOf(o core.CheckOutcome) CheckSummary {
	return CheckSummary{
		ID: o.ID, Status: o.Status, Summary: o.Summary, CheckedAt: o.CheckedAt,
		Label: o.Label, Details: o.Details, Hints: o.Hints, Data: o.Data,
	}
}

type cachedCheck struct {
	out core.CheckOutcome
	at  time.Time
}

// checkCache keeps the last result per check in memory. Nothing is written to disk
// in this milestone; after a restart the checks run again.
type checkCache struct {
	mu      sync.Mutex
	results map[string]cachedCheck
	// running holds, per running check, the generation of the claim that started it.
	running map[string]uint64
	gen     uint64
}

func newCheckCache() *checkCache {
	return &checkCache{results: map[string]cachedCheck{}, running: map[string]uint64{}}
}

func (c *checkCache) get(id string) (cachedCheck, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	r, ok := c.results[id]
	return r, ok
}

// claim marks a check as running and returns the generation of the claim; it
// reports false when the check already runs, so concurrent refreshes coalesce.
func (c *checkCache) claim(id string) (uint64, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.running[id]; ok {
		return 0, false
	}
	c.gen++
	c.running[id] = c.gen
	return c.gen, true
}

// store keeps the result of a check and ends the claim gen that produced it. A newer
// claim of the same check, made after this result arrived, stays.
func (c *checkCache) store(o core.CheckOutcome, gen uint64) {
	c.mu.Lock()
	c.results[o.ID] = cachedCheck{out: o, at: time.Now()}
	if c.running[o.ID] == gen {
		delete(c.running, o.ID)
	}
	c.mu.Unlock()
}

// release ends the claims of a batch that are still current: a check that got no
// outcome. Claims that were replaced by a newer refresh are left alone.
func (c *checkCache) release(claims map[string]uint64) {
	c.mu.Lock()
	for id, gen := range claims {
		if c.running[id] == gen {
			delete(c.running, id)
		}
	}
	c.mu.Unlock()
}

func (c *checkCache) isRunning(id string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, ok := c.running[id]
	return ok
}

// StartChecks starts a background refresh of all checks.
func (s *Server) StartChecks() { s.refreshWhere(func(core.CheckRef) bool { return true }) }

// refreshWhere runs, in the background, the checks that match and do not run yet.
func (s *Server) refreshWhere(match func(core.CheckRef) bool) {
	st, err := s.load()
	if err != nil {
		s.log.Warn("checks: cannot load the configuration", "error", err)
		return
	}
	rn, err := s.newRunner(st)
	if err != nil {
		s.log.Warn("checks: cannot prepare the runner", "error", err)
		return
	}
	claimed := map[string]uint64{}
	for _, c := range core.Checks(st.Evaluate()) {
		if !match(c) {
			continue
		}
		if gen, ok := s.checks.claim(c.ID()); ok {
			claimed[c.ID()] = gen
		}
	}
	if len(claimed) == 0 {
		return
	}
	if !s.spawn(func() {
		defer s.checks.release(claimed)
		core.RunChecksNotify(s.ctx, st, rn, func(ref string) bool { _, ok := claimed[ref]; return ok }, func(o core.CheckOutcome) {
			s.checks.store(o, claimed[o.ID])
			s.bc.CheckChanged(summaryOf(o))
		})
	}) {
		s.checks.release(claimed)
	}
}

// refreshAfterSession refreshes the checks of the plugin of a finished session and
// those whose refreshOn matches session.finished:<ref>.
func (s *Server) refreshAfterSession(pluginID, ref string) {
	if s.ctx.Err() != nil {
		return
	}
	s.refreshWhere(func(c core.CheckRef) bool {
		if c.Plugin.ID == pluginID {
			return true
		}
		for _, key := range core.AnyStrings(c.Def["refreshOn"]) {
			if plugin.EventMatches(key, "session.finished", ref) {
				return true
			}
		}
		return false
	})
}

// StatusReport is GET /checks (api/openapi.json, StatusReport).
type StatusReport struct {
	Host    map[string]any `json:"host"`
	Summary core.Summary   `json:"summary"`
	Checks  []CheckSummary `json:"checks"`
	Running []string       `json:"running,omitempty"`
}

func (s *Server) listChecks(w http.ResponseWriter, r *http.Request) error {
	st, err := s.load()
	if err != nil {
		return err
	}
	refs := core.Checks(st.Evaluate())
	now := time.Now()
	report := StatusReport{Host: st.Host(), Checks: []CheckSummary{}}
	stale := map[string]bool{}
	var known []core.CheckOutcome
	for _, c := range refs {
		id := c.ID()
		if s.checks.isRunning(id) {
			report.Running = append(report.Running, id)
		}
		cached, ok := s.checks.get(id)
		if !ok {
			stale[id] = true // never checked: run it now
			continue
		}
		report.Checks = append(report.Checks, summaryOf(cached.out))
		known = append(known, cached.out)
		if ttl := cached.out.Cache; ttl > 0 && now.Sub(cached.at) > ttl {
			stale[id] = true
		}
	}
	report.Summary = core.Summarize(known)
	if len(stale) > 0 {
		s.refreshWhere(func(c core.CheckRef) bool { return stale[c.ID()] })
		// Checks that were just claimed now count as running.
		report.Running = report.Running[:0]
		for _, c := range refs {
			if s.checks.isRunning(c.ID()) {
				report.Running = append(report.Running, c.ID())
			}
		}
	}
	sort.Strings(report.Running)
	s.writeJSON(w, r, http.StatusOK, report)
	return nil
}

func (s *Server) getCheck(w http.ResponseWriter, r *http.Request) error {
	id := r.PathValue("id")
	cached, ok := s.checks.get(id)
	if !config.ValidKey(id) || !ok {
		return notFound("there is no cached result for " + id)
	}
	s.writeJSON(w, r, http.StatusOK, summaryOf(cached.out))
	return nil
}

func (s *Server) refreshChecks(w http.ResponseWriter, r *http.Request) error {
	body, err := s.decodeBody(w, r, "ChecksRefresh", true)
	if err != nil {
		return err
	}
	st, err := s.load()
	if err != nil {
		return err
	}
	known := map[string]bool{}
	for _, c := range core.Checks(st.Evaluate()) {
		known[c.ID()] = true
	}
	want := map[string]bool{}
	ids, _ := body["ids"].([]any)
	var errs []fieldError
	for i, raw := range ids {
		id, _ := raw.(string)
		if !known[id] {
			errs = append(errs, fieldError{Pointer: fmt.Sprintf("/ids/%d", i), Code: codeNotFound, Message: "no such check"})
			continue
		}
		want[id] = true
	}
	if len(errs) > 0 {
		return invalid("unknown checks", errs...)
	}
	s.refreshWhere(func(c core.CheckRef) bool { return len(ids) == 0 || want[c.ID()] })
	w.WriteHeader(http.StatusAccepted)
	return nil
}
