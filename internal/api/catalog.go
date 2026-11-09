package api

import (
	"net/http"

	"github.com/Zenit-Hosting-de/devbox/internal/catalog"
	"github.com/Zenit-Hosting-de/devbox/internal/config"
	"github.com/Zenit-Hosting-de/devbox/internal/repo"
)

type catalogSearch struct {
	Plugins  []catalog.Hit     `json:"plugins"`
	Problems map[string]string `json:"problems"`
}

func (s *Server) searchCatalog(w http.ResponseWriter, r *http.Request) error {
	st, err := s.load()
	if err != nil {
		return err
	}
	ctx, cancel := s.opContext(r)
	defer cancel()
	hits, problems, err := st.CatalogSearch(ctx, r.URL.Query().Get("q"))
	if err != nil {
		return err
	}
	s.writeJSON(w, r, http.StatusOK, catalogSearch{Plugins: hits, Problems: problems})
	return nil
}

type catalogInstall struct {
	ID              string            `json:"id"`
	Catalog         string            `json:"catalog"`
	Version         string            `json:"version"`
	PreviousVersion string            `json:"previousVersion,omitempty"`
	Digest          string            `json:"digest"`
	SignedBy        string            `json:"signedBy,omitempty"`
	Permissions     []repo.Permission `json:"permissions"`
	NewPermissions  []repo.Permission `json:"newPermissions"`
	Installed       bool              `json:"installed"`
}

// installFromCatalog downloads and checks a package; it puts the plugin in place when
// no confirmation is needed or confirm is set.
func (s *Server) installFromCatalog(w http.ResponseWriter, r *http.Request) error {
	body, err := s.decodeBody(w, r, "CatalogInstallRequest", false)
	if err != nil {
		return err
	}
	str := func(key string) string {
		v, _ := body[key].(string)
		return v
	}
	confirm, _ := body["confirm"].(bool)
	st, err := s.load()
	if err != nil {
		return err
	}
	ref := str("id")
	if v := str("version"); v != "" {
		ref += "@" + v
	}
	ctx, cancel := s.opContext(r)
	defer cancel()
	plan, err := st.PrepareInstall(ctx, ref, str("catalog"))
	if err != nil {
		return err
	}
	defer plan.Close()
	if plan.Installed == plan.Release.Version {
		return &config.Error{Code: config.CodeConflict, Key: plan.ID, Detail: "version " + plan.Installed + " is installed already"}
	}
	out := catalogInstall{
		ID: plan.ID, Catalog: plan.Catalog.Name, Version: plan.Release.Version, PreviousVersion: plan.Installed,
		Digest: plan.Release.Digest, Permissions: plan.Permissions, NewPermissions: plan.New,
	}
	if plan.Catalog.Signer != nil {
		out.SignedBy = plan.Catalog.Signer.Identity
	}
	needsConfirm := plan.Installed == "" || len(plan.New) > 0
	if needsConfirm && !confirm {
		s.writeJSON(w, r, http.StatusOK, out)
		return nil
	}
	s.cfgMu.Lock()
	err = st.Catalog.Commit(plan)
	s.cfgMu.Unlock()
	if err != nil {
		return err
	}
	out.Installed = true
	s.bc.PluginsChanged([]string{plan.ID}, nil)
	s.writeJSON(w, r, http.StatusOK, out)
	return nil
}

func (s *Server) deletePlugin(w http.ResponseWriter, r *http.Request) error {
	id := r.PathValue("id")
	st, err := s.load()
	if err != nil {
		return err
	}
	s.cfgMu.Lock()
	err = st.RemoveCatalogPlugin(id)
	s.cfgMu.Unlock()
	if err != nil {
		return err
	}
	s.bc.PluginsChanged([]string{id}, nil)
	w.WriteHeader(http.StatusNoContent)
	return nil
}
