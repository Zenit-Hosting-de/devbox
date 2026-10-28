package api

import (
	"errors"
	"net/http"
	"regexp"
	"sort"

	"github.com/Zenit-Hosting-de/devbox/internal/config"
	"github.com/Zenit-Hosting-de/devbox/internal/core"
	"github.com/Zenit-Hosting-de/devbox/internal/schema"
)

func (s *Server) listSettings(w http.ResponseWriter, r *http.Request) error {
	st, err := s.load()
	if err != nil {
		return err
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"settings": core.Settings(st)})
	return nil
}

// patchSettings validates every key first and writes nothing if any key fails. The
// status follows the worst failure: 404 for an unknown key, then 409 for a locked
// one, then 422.
func (s *Server) patchSettings(w http.ResponseWriter, r *http.Request) error {
	body, err := s.decodeBody(w, r, "SettingsPatch", false)
	if err != nil {
		return err
	}
	st, err := s.load()
	if err != nil {
		return err
	}
	keys := make([]string, 0, len(body))
	for k := range body {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var unknown, locked, bad []fieldError
	for _, key := range keys {
		pointer := "/" + escapePointer(key)
		var err error
		if body[key] == nil { // null removes the user value
			err = config.CheckKey(st.Defs, st.Result, key)
		} else {
			_, err = config.Check(st.Defs, st.Result, key, body[key])
		}
		if err == nil {
			continue
		}
		var ce *config.Error
		if !errors.As(err, &ce) {
			return err
		}
		fe := fieldError{Pointer: pointer, Code: ce.Code, Message: ce.Detail}
		switch ce.Code {
		case config.CodeNotFound:
			unknown = append(unknown, fe)
		case config.CodeSettingLocked:
			locked = append(locked, fe)
		default:
			bad = append(bad, fe)
		}
	}
	switch {
	case len(unknown) > 0:
		e := newError(http.StatusNotFound, codeNotFound, "no plugin defines "+unknown[0].Pointer[1:])
		e.errors = unknown
		return e
	case len(locked) > 0:
		e := newError(http.StatusConflict, codeSettingLocked, locked[0].Message)
		e.errors = locked
		return e
	case len(bad) > 0:
		return invalid("some values are not valid", bad...)
	}

	s.cfgMu.Lock()
	defer s.cfgMu.Unlock()
	for _, key := range keys {
		var err error
		if body[key] == nil {
			err = config.Unset(st.Validator, st.UserPath, key)
		} else {
			err = config.Set(st.Validator, st.UserPath, st.Defs, st.Result, key, body[key])
		}
		if err != nil {
			return err
		}
	}
	s.bc.SettingsChanged(keys)
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (s *Server) explainSetting(w http.ResponseWriter, r *http.Request) error {
	st, err := s.load()
	if err != nil {
		return err
	}
	ex, err := st.Result.Explain(r.PathValue("key"))
	if err != nil {
		return err
	}
	s.writeJSON(w, r, http.StatusOK, ex)
	return nil
}

func (s *Server) listCredentials(w http.ResponseWriter, r *http.Request) error {
	st, err := s.load()
	if err != nil {
		return err
	}
	instances, err := core.Credentials(st)
	if err != nil {
		return err
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"instances": instances})
	return nil
}

var schemaName = regexp.MustCompile(`^[a-z][a-z0-9-]{1,31}$`)

func (s *Server) getSchema(w http.ResponseWriter, r *http.Request) error {
	name := r.PathValue("name")
	if !schemaName.MatchString(name) {
		return notFound("no such schema")
	}
	data, ok := schema.Raw(name)
	if !ok {
		return notFound("no such schema")
	}
	w.Header().Set("Content-Type", "application/schema+json")
	_, _ = w.Write(data) //nolint:gosec // an embedded schema file, not user input
	return nil
}
