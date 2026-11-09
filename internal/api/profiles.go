package api

import (
	"net/http"

	"github.com/Zenit-Hosting-de/devbox/internal/core"
	"github.com/Zenit-Hosting-de/devbox/internal/profile"
)

func (s *Server) listProfiles(w http.ResponseWriter, r *http.Request) error {
	views, err := core.Profiles(s.env)
	if err != nil {
		return err
	}
	s.writeJSON(w, r, http.StatusOK, views)
	return nil
}

// createProfile fetches the profile (and checks its signature) and appends the source
// to the user file; nothing is written when the fetch fails.
func (s *Server) createProfile(w http.ResponseWriter, r *http.Request) error {
	body, err := s.decodeBody(w, r, "ProfileCreate", false)
	if err != nil {
		return err
	}
	src := profile.SourcesOf(map[string]any{"profiles": []any{body}}, profile.OriginUser)[0]
	ctx, cancel := s.opContext(r)
	defer cancel()
	s.cfgMu.Lock()
	v, err := core.AddProfile(ctx, s.env, src)
	s.cfgMu.Unlock()
	if err != nil {
		return err
	}
	s.announceProfiles()
	s.writeJSON(w, r, http.StatusCreated, v)
	return nil
}

func (s *Server) deleteProfile(w http.ResponseWriter, r *http.Request) error {
	s.cfgMu.Lock()
	_, err := core.RemoveProfile(s.env, r.PathValue("id"))
	s.cfgMu.Unlock()
	if err != nil {
		return err
	}
	s.announceProfiles()
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// updateProfiles fetches all remote profiles again; a failed fetch is reported per
// profile and keeps the previous copy.
func (s *Server) updateProfiles(w http.ResponseWriter, r *http.Request) error {
	ctx, cancel := s.opContext(r)
	defer cancel()
	s.cfgMu.Lock()
	updates, err := core.UpdateProfiles(ctx, s.env, "")
	s.cfgMu.Unlock()
	if err != nil {
		return err
	}
	for _, u := range updates {
		if u.Changed {
			s.announceProfiles()
			break
		}
	}
	s.writeJSON(w, r, http.StatusOK, updates)
	return nil
}

// announceProfiles tells the WebSocket clients that settings and plugins may have
// changed: a profile sets and locks settings and switches plugins on and off.
func (s *Server) announceProfiles() {
	s.bc.SettingsChanged(nil)
	s.bc.PluginsChanged(nil, nil)
}
