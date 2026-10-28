package api

import (
	"context"
	"net"
	"net/http"
	"strings"
	"sync"
)

// Header every modifying request must carry (CSRF protection), together with an
// allowed Origin.
const requestHeader = "X-DevBox-Request"

// Security headers of every response.
const contentSecurityPolicy = "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; font-src 'self'; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'"

type ctxKey int

const viaSocketKey ctxKey = 1

// viaSocket reports whether the request arrived over the Unix socket, where the file
// mode is the protection and Host, Origin and CSRF checks do not apply.
func viaSocket(r *http.Request) bool {
	v, _ := r.Context().Value(viaSocketKey).(bool)
	return v
}

// guard checks Host (DNS rebinding) and Origin (CSRF) of requests over TCP.
type guard struct {
	mu      sync.RWMutex
	hosts   map[string]bool // allowed Host header values, lower case
	origins map[string]bool // allowed Origin values, lower case, without trailing slash
	extra   []string
}

func newGuard(listen string, extraOrigins []string) *guard {
	g := &guard{extra: extraOrigins}
	g.setListen(listen)
	return g
}

// setListen allows the listen address and the loopback names with the same port. It is
// called again with the real address when the port was chosen by the system.
func (g *guard) setListen(addr string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.hosts = map[string]bool{}
	g.origins = map[string]bool{}
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return
	}
	names := []string{"localhost", "127.0.0.1", "[::1]"}
	if host != "" && host != "0.0.0.0" && host != "::" {
		names = append(names, bracket(host))
	}
	for _, n := range names {
		hp := strings.ToLower(n + ":" + port)
		g.hosts[hp] = true
		g.origins["http://"+hp] = true
	}
	for _, o := range g.extra {
		g.origins[normalizeOrigin(o)] = true
	}
}

func bracket(host string) string {
	if strings.Contains(host, ":") && !strings.HasPrefix(host, "[") {
		return "[" + host + "]"
	}
	return host
}

func normalizeOrigin(o string) string {
	return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(o)), "/")
}

func (g *guard) hostOK(host string) bool {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.hosts[strings.ToLower(host)]
}

func (g *guard) originOK(origin string) bool {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.origins[normalizeOrigin(origin)]
}

func isMutating(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	}
	return false
}

// CheckHost reports whether the Host of the request is allowed. Requests over the Unix
// socket are always allowed.
func (s *Server) CheckHost(r *http.Request) bool {
	return viaSocket(r) || s.guard.hostOK(r.Host)
}

// CheckOrigin reports whether the request carries an allowed Origin: the origin of the
// server itself, server.allowedOrigins or --allow-origin. Requests over the Unix socket
// are always allowed. The WebSocket upgrade uses it, too.
func (s *Server) CheckOrigin(r *http.Request) bool {
	if viaSocket(r) {
		return true
	}
	origin := r.Header.Get("Origin")
	return origin != "" && s.guard.originOK(origin)
}

// guarded wraps the handler with the Host and CSRF checks of requests over TCP.
func (s *Server) guarded(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.CheckHost(r) {
			s.fail(w, r, newError(http.StatusForbidden, codeCSRF, "the Host header is not allowed"))
			return
		}
		if isMutating(r.Method) {
			if r.Header.Get(requestHeader) != "1" {
				s.fail(w, r, newError(http.StatusForbidden, codeCSRF, "the header "+requestHeader+": 1 is required"))
				return
			}
			if !s.CheckOrigin(r) {
				s.fail(w, r, newError(http.StatusForbidden, codeCSRF, "the Origin header is missing or not allowed"))
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// securityHeaders adds the headers every response carries.
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", contentSecurityPolicy)
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}

// socketRequests marks the requests of the Unix socket listener.
func socketRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), viaSocketKey, true)))
	})
}
