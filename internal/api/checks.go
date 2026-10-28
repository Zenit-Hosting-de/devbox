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
	running map[string]bool
}

func newCheckCache() *checkCache {
	return &checkCache{results: map[string]cachedCheck{}, running: map[string]bool{}}
}

func (c *checkCache) get(id string) (cachedCheck, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	r, ok := c.results[id]
	return r, ok
}

// claim marks a check as running; it reports false when it already runs, so
// concurrent refreshes of the same check coalesce.
func (c *checkCache) claim(id string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.running[id] {
		return false
	}
	c.running[id] = true
	return true
}

func (c *checkCache) store(o core.CheckOutcome) {
	c.mu.Lock()
	c.results[o.ID] = cachedCheck{out: o, at: time.Now()}
	delete(c.running, o.ID)
	c.mu.Unlock()
}

func (c *checkCache) release(ids []string) {
	c.mu.Lock()
	for _, id := range ids {
		delete(c.running, id)
	}
	c.mu.Unlock()
}

func (c *checkCache) isRunning(id string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.running[id]
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
	var claimed []string
	set := map[string]bool{}
	for _, c := range core.Checks(st.Evaluate()) {
		if match(c) && s.checks.claim(c.ID()) {
			claimed = append(claimed, c.ID())
			set[c.ID()] = true
		}
	}
	if len(claimed) == 0 {
		return
	}
	if !s.spawn(func() {
		defer s.checks.release(claimed)
		core.RunChecksNotify(s.ctx, st, rn, func(ref string) bool { return set[ref] }, func(o core.CheckOutcome) {
			s.checks.store(o)
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
	Summary statusSummary  `json:"summary"`
	Checks  []CheckSummary `json:"checks"`
	Running []string       `json:"running,omitempty"`
}

type statusSummary struct {
	OK      int `json:"ok"`
	Warn    int `json:"warn"`
	Error   int `json:"error"`
	Missing int `json:"missing"`
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
		switch cached.out.Status {
		case "ok":
			report.Summary.OK++
		case "warn":
			report.Summary.Warn++
		case "missing":
			report.Summary.Missing++
		default:
			report.Summary.Error++
		}
		if ttl := cached.out.Cache; ttl > 0 && now.Sub(cached.at) > ttl {
			stale[id] = true
		}
	}
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
