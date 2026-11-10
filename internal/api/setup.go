package api

import (
	"net/http"

	"github.com/Zenit-Hosting-de/devbox/internal/history"
)

func (s *Server) listGroups(w http.ResponseWriter, r *http.Request) error {
	st, err := s.load()
	if err != nil {
		return err
	}
	s.writeJSON(w, r, http.StatusOK, st.Groups())
	return nil
}

func (s *Server) getSetup(w http.ResponseWriter, r *http.Request) error {
	st, err := s.load()
	if err != nil {
		return err
	}
	plan, err := st.SetupPlan(history.New(st.StateDir(), st.Validator))
	if err != nil {
		return err
	}
	s.writeJSON(w, r, http.StatusOK, plan)
	return nil
}
