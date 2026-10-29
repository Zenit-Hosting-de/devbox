package api

import (
	"context"
	"net/http"

	"github.com/Zenit-Hosting-de/devbox/internal/core"
	"github.com/Zenit-Hosting-de/devbox/internal/repo"
)

// opContext returns the context of a long operation: the request, and the server
// lifetime, so a shutdown stops a running sync.
func (s *Server) opContext(r *http.Request) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(r.Context())
	stop := context.AfterFunc(s.ctx, cancel)
	return ctx, func() {
		stop()
		cancel()
	}
}

func (s *Server) listRepositories(w http.ResponseWriter, r *http.Request) error {
	st, err := s.load()
	if err != nil {
		return err
	}
	s.writeJSON(w, r, http.StatusOK, st.RepositoryViews())
	return nil
}

// createRepository writes the entry into the user file and syncs it. The entry stays
// when the first sync fails; the failure shows as state "error" with lastError.
func (s *Server) createRepository(w http.ResponseWriter, r *http.Request) error {
	body, err := s.decodeBody(w, r, "RepositoryCreate", false)
	if err != nil {
		return err
	}
	str := func(key string) string {
		v, _ := body[key].(string)
		return v
	}
	spec := core.RepoSpec{Name: str("name"), Source: str("source"), Ref: str("ref"), Path: str("path"), Trust: str("trust")}
	st, err := s.load()
	if err != nil {
		return err
	}
	s.cfgMu.Lock()
	err = st.AddRepository(spec)
	s.cfgMu.Unlock()
	if err != nil {
		return err
	}
	ctx, cancel := s.opContext(r)
	defer cancel()
	if st, err = s.load(); err != nil {
		return err
	}
	if err := st.SyncRepository(ctx, spec.Name); err != nil {
		s.log.Info("first sync of a new repository failed", "repository", spec.Name, "error", repo.ErrorText(err))
	}
	s.announceRepository(st, spec.Name)
	return s.respondRepository(w, r, http.StatusCreated, spec.Name)
}

func (s *Server) syncRepository(w http.ResponseWriter, r *http.Request) error {
	name := r.PathValue("name")
	st, err := s.load()
	if err != nil {
		return err
	}
	ctx, cancel := s.opContext(r)
	defer cancel()
	err = st.SyncRepository(ctx, name)
	s.announceRepository(st, name) // a failed sync is recorded in the state, too
	if err != nil {
		return err
	}
	return s.respondRepository(w, r, http.StatusOK, name)
}

func (s *Server) approveRepository(w http.ResponseWriter, r *http.Request) error {
	name := r.PathValue("name")
	body, err := s.decodeBody(w, r, "RepositoryApprove", false)
	if err != nil {
		return err
	}
	revision, _ := body["revision"].(string)
	st, err := s.load()
	if err != nil {
		return err
	}
	ctx, cancel := s.opContext(r)
	defer cancel()
	if err := st.ApproveRepository(ctx, name, revision); err != nil {
		return err
	}
	s.announceRepository(st, name)
	return s.respondRepository(w, r, http.StatusOK, name)
}

func (s *Server) discardRepository(w http.ResponseWriter, r *http.Request) error {
	name := r.PathValue("name")
	st, err := s.load()
	if err != nil {
		return err
	}
	ctx, cancel := s.opContext(r)
	defer cancel()
	if err := st.DiscardRepository(ctx, name); err != nil {
		return err
	}
	s.announceRepository(st, name)
	return s.respondRepository(w, r, http.StatusOK, name)
}

func (s *Server) deleteRepository(w http.ResponseWriter, r *http.Request) error {
	name := r.PathValue("name")
	st, err := s.load()
	if err != nil {
		return err
	}
	before, err := st.RepositoryView(name)
	if err != nil {
		return err
	}
	ctx, cancel := s.opContext(r)
	defer cancel()
	s.cfgMu.Lock()
	err = st.RemoveRepository(ctx, name)
	s.cfgMu.Unlock()
	if err != nil {
		return err
	}
	s.bc.PluginsChanged(before.PluginIDs(), []string{name})
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// respondRepository answers with the current state of a repository.
func (s *Server) respondRepository(w http.ResponseWriter, r *http.Request, status int, name string) error {
	st, err := s.load()
	if err != nil {
		return err
	}
	view, err := st.RepositoryView(name)
	if err != nil {
		return err
	}
	s.writeJSON(w, r, status, view)
	return nil
}

// announceRepository tells the WebSocket clients that a repository changed.
func (s *Server) announceRepository(st *core.State, name string) {
	view, err := st.RepositoryView(name)
	if err != nil {
		return
	}
	s.bc.PluginsChanged(view.PluginIDs(), []string{name})
}

func (s *Server) patchPlugin(w http.ResponseWriter, r *http.Request) error {
	id := r.PathValue("id")
	body, err := s.decodeBody(w, r, "PluginPatch", false)
	if err != nil {
		return err
	}
	enabled, _ := body["enabled"].(bool)
	st, err := s.load()
	if err != nil {
		return err
	}
	s.cfgMu.Lock()
	changed, err := st.SetPluginEnabled(id, enabled)
	s.cfgMu.Unlock()
	if err != nil {
		return err
	}
	if changed {
		s.bc.PluginsChanged([]string{id}, nil)
	}
	st, p, err := s.findPlugin(id)
	if err != nil {
		return err
	}
	s.writeJSON(w, r, http.StatusOK, st.PluginDetail(p, true))
	return nil
}
