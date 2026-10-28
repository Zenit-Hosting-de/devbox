package api

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/Zenit-Hosting-de/devbox/internal/config"
	"github.com/Zenit-Hosting-de/devbox/internal/schema"
)

// Problem codes of api/openapi.json (Problem.code).
const (
	codeValidation       = "validation"
	codeNotFound         = "not-found"
	codePluginNotReady   = "plugin-not-ready"
	codeSettingLocked    = "setting-locked"
	codePermissionDenied = "permission-denied"
	codeUnauthenticated  = "unauthenticated"
	codeCSRF             = "csrf"
	codeConflict         = "conflict"
	codeInternal         = "internal"
)

var problemTitles = map[string]string{
	codeValidation:       "Validation failed",
	codeNotFound:         "Not found",
	codePluginNotReady:   "Plugin not ready",
	codeSettingLocked:    "Setting locked",
	codePermissionDenied: "Permission denied",
	codeUnauthenticated:  "Authentication required",
	codeCSRF:             "Request not allowed",
	codeConflict:         "Conflict",
	codeInternal:         "Internal error",
}

// fieldError is one entry of Problem.errors.
type fieldError struct {
	Pointer string `json:"pointer"`
	Code    string `json:"code"`
	Message string `json:"message,omitempty"`
}

// problem is the RFC 9457 body (api/openapi.json, Problem).
type problem struct {
	Type     string       `json:"type"`
	Title    string       `json:"title"`
	Status   int          `json:"status"`
	Code     string       `json:"code"`
	Detail   string       `json:"detail,omitempty"`
	Instance string       `json:"instance,omitempty"`
	TraceID  string       `json:"traceId,omitempty"`
	Errors   []fieldError `json:"errors,omitempty"`
}

// apiError is an error a handler returns; fail turns it into a problem response.
type apiError struct {
	status int
	code   string
	detail string
	errors []fieldError
	header http.Header
}

func (e *apiError) Error() string { return e.code + ": " + e.detail }

func newError(status int, code, detail string) *apiError {
	return &apiError{status: status, code: code, detail: detail}
}

func notFound(detail string) *apiError { return newError(http.StatusNotFound, codeNotFound, detail) }

func conflict(detail string) *apiError { return newError(http.StatusConflict, codeConflict, detail) }

func invalid(detail string, errs ...fieldError) *apiError {
	e := newError(http.StatusUnprocessableEntity, codeValidation, detail)
	e.errors = errs
	return e
}

// errorBase derives the base URL of the error types from the "$id" of the embedded
// schemas, so no host is written in code.
func errorBase(v *schema.Validator) string {
	id, ok := v.ID("config")
	if !ok {
		return ""
	}
	u, err := url.Parse(id)
	if err != nil {
		return ""
	}
	return u.Scheme + "://" + u.Host + "/errors/"
}

func traceID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "unknown"
	}
	return hex.EncodeToString(b[:])
}

// toAPIError maps any error of the core to an apiError.
func toAPIError(err error) *apiError {
	var ae *apiError
	if errors.As(err, &ae) {
		return ae
	}
	var ce *config.Error
	if errors.As(err, &ce) {
		switch ce.Code {
		case config.CodeNotFound:
			return notFound(ce.Detail)
		case config.CodeSettingLocked:
			return newError(http.StatusConflict, codeSettingLocked, ce.Detail)
		case config.CodeValidation:
			e := invalid(ce.Detail)
			if ce.Key != "" {
				e.errors = []fieldError{{Pointer: "/" + escapePointer(ce.Key), Code: codeValidation, Message: ce.Detail}}
			}
			return e
		}
	}
	var ve *schema.ValidationError
	if errors.As(err, &ve) {
		return invalid("the document does not follow its schema", violations("", ve)...)
	}
	return nil
}

// fail writes err as a problem response. Errors the API does not know are internal:
// the detail is logged with a trace ID and not sent.
func (s *Server) fail(w http.ResponseWriter, r *http.Request, err error) {
	ae := toAPIError(err)
	p := problem{Instance: r.URL.Path}
	if ae == nil {
		p.Status, p.Code, p.TraceID = http.StatusInternalServerError, codeInternal, traceID()
		s.log.Error("internal error", "traceId", p.TraceID, "method", r.Method, "path", r.URL.Path, "error", err)
	} else {
		p.Status, p.Code, p.Detail, p.Errors = ae.status, ae.code, ae.detail, ae.errors
		for k, v := range ae.header {
			w.Header()[k] = v
		}
	}
	p.Title = problemTitles[p.Code]
	p.Type = s.errorBase + p.Code
	body, merr := json.Marshal(p)
	if merr != nil {
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(p.Status)
	_, _ = w.Write(append(body, '\n'))
}

// writeJSON writes a response body; a body that cannot be encoded is an internal error.
func (s *Server) writeJSON(w http.ResponseWriter, r *http.Request, status int, v any) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		s.fail(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(buf.Bytes())
}

func escapePointer(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "~", "~0"), "/", "~1")
}

// violations turns schema violations into field errors; prefix is prepended to every
// pointer.
func violations(prefix string, ve *schema.ValidationError) []fieldError {
	var out []fieldError
	for _, v := range ve.Violations {
		if !v.Leaf {
			continue
		}
		code := v.Keyword
		if code == "additionalProperties" && len(v.Properties) > 0 {
			for _, name := range v.Properties {
				out = append(out, fieldError{Pointer: prefix + v.Pointer + "/" + escapePointer(name), Code: "unknown-key", Message: "unknown key"})
			}
			continue
		}
		out = append(out, fieldError{Pointer: prefix + v.Pointer, Code: code, Message: v.Message})
	}
	if len(out) == 0 {
		out = append(out, fieldError{Pointer: prefix, Code: codeValidation, Message: ve.Error()})
	}
	return out
}
