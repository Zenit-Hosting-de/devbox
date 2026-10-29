package api

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Zenit-Hosting-de/devbox/internal/core"
	"github.com/Zenit-Hosting-de/devbox/internal/runner"
	"github.com/Zenit-Hosting-de/devbox/internal/schema"
)

// maxBody bounds the size of a request body.
const maxBody = 1 << 20

// Timeouts of the HTTP server and of the shutdown.
const (
	readHeaderTimeout = 10 * time.Second
	shutdownGrace     = 20 * time.Second
	httpGrace         = 5 * time.Second
)

// Options configure a Server.
type Options struct {
	Build core.Build
	Env   core.Env
	// Listen is the address of the TCP listener (host:port); it defines the Host
	// header values the guard accepts. Serve replaces it by the real address.
	Listen string
	// Origins are origins accepted in addition to the own origin (server.allowedOrigins
	// and --allow-origin).
	Origins []string
	// Auth is the value of server.auth shown in /info.
	Auth   string
	Logger *slog.Logger
	// Broadcaster additionally receives everything the WebSocket hub receives.
	Broadcaster Broadcaster
	// WebSocket tunes the WebSocket; the zero value is the production setting.
	WebSocket WSOptions
}

// Server is the REST API.
type Server struct {
	opts      Options
	log       *slog.Logger
	env       core.Env
	validator *schema.Validator
	errorBase string
	guard     *guard
	bc        Broadcaster
	hub       *Hub
	wsOpts    WSOptions
	locks     *runner.Locks
	cfgMu     sync.Mutex
	live      *registry
	checks    *checkCache
	ui        *uiFiles
	mux       *http.ServeMux

	ctx    context.Context //nolint:containedctx // the server lifetime, runs outlive their requests
	cancel context.CancelFunc
	wg     sync.WaitGroup
	socket atomic.Bool
	ws     atomic.Bool

	runMu   sync.Mutex
	closing bool
}

// New returns a server. Nothing runs until Serve (or the handlers) are used.
func New(opts Options) (*Server, error) {
	v, err := schema.New()
	if err != nil {
		return nil, fmt.Errorf("api: %w", err)
	}
	if opts.Logger == nil {
		opts.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	if opts.Broadcaster == nil {
		opts.Broadcaster = NopBroadcaster{}
	}
	env := opts.Env
	env.Validator = v
	wsOpts := opts.WebSocket.withDefaults()
	hub := newHub(wsOpts.hub())
	bc := Broadcaster(hub)
	if _, nop := opts.Broadcaster.(NopBroadcaster); !nop {
		bc = multiBroadcaster{hub, opts.Broadcaster}
	}
	s := &Server{
		opts:      opts,
		log:       opts.Logger,
		env:       env,
		validator: v,
		errorBase: errorBase(v),
		guard:     newGuard(opts.Listen, opts.Origins),
		bc:        bc,
		hub:       hub,
		wsOpts:    wsOpts,
		locks:     runner.NewLocks(),
		live:      newRegistry(),
		checks:    newCheckCache(),
		ui:        newUIFiles(),
		mux:       http.NewServeMux(),
	}
	s.ctx, s.cancel = context.WithCancel(context.Background())
	s.routes()
	s.SetWebSocket(true)
	return s, nil
}

// SetSocket records whether the Unix socket is listening (feature "socket" of /info).
func (s *Server) SetSocket(on bool) { s.socket.Store(on) }

// Handler serves the API and the UI over TCP, with the Host, Origin and CSRF checks.
func (s *Server) Handler() http.Handler {
	return s.accessLog("tcp", securityHeaders(s.guarded(s.mux)))
}

// SocketHandler serves the same over the Unix socket, where the file mode is the
// protection and the Host, Origin and CSRF checks are skipped.
func (s *Server) SocketHandler() http.Handler {
	return s.accessLog("socket", securityHeaders(socketRequests(s.mux)))
}

// Live returns a running or queued session.
func (s *Server) Live(id string) (*LiveSession, bool) { return s.live.get(id) }

// Context is cancelled when the server shuts down; background work stops with it.
func (s *Server) Context() context.Context { return s.ctx }

type handlerFunc func(w http.ResponseWriter, r *http.Request) error

func (s *Server) route(pattern string, h handlerFunc) {
	s.mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
		if err := h(w, r); err != nil {
			s.fail(w, r, err)
		}
	})
}

func (s *Server) routes() {
	s.route("GET /api/v1/info", s.getInfo)
	s.route("GET /api/v1/plugins", s.listPlugins)
	s.route("GET /api/v1/plugins/{id}", s.getPlugin)
	s.route("PATCH /api/v1/plugins/{id}", s.patchPlugin)
	s.route("GET /api/v1/plugins/{id}/messages/{lang}", s.getPluginMessages)
	s.route("GET /api/v1/checks", s.listChecks)
	s.route("POST /api/v1/checks/refresh", s.refreshChecks)
	s.route("GET /api/v1/checks/{id}", s.getCheck)
	s.route("GET /api/v1/settings", s.listSettings)
	s.route("PATCH /api/v1/settings", s.patchSettings)
	s.route("GET /api/v1/settings/{key}/explain", s.explainSetting)
	s.route("GET /api/v1/credentials", s.listCredentials)
	s.route("GET /api/v1/repositories", s.listRepositories)
	s.route("POST /api/v1/repositories", s.createRepository)
	s.route("POST /api/v1/repositories/{name}/sync", s.syncRepository)
	s.route("POST /api/v1/repositories/{name}/approve", s.approveRepository)
	s.route("DELETE /api/v1/repositories/{name}/pending", s.discardRepository)
	s.route("DELETE /api/v1/repositories/{name}", s.deleteRepository)
	s.route("GET /api/v1/sessions", s.listSessions)
	s.route("POST /api/v1/sessions", s.createSession)
	s.route("GET /api/v1/sessions/{id}", s.getSession)
	s.route("GET /api/v1/sessions/{id}/events", s.listSessionEvents)
	s.route("GET /api/v1/sessions/{id}/log", s.getSessionLog)
	s.route("POST /api/v1/sessions/{id}/cancel", s.cancelSession)
	s.route("GET /api/v1/schema/{name}", s.getSchema)
	s.route("GET /api/v1/ws", s.serveWS)
	// Everything else below /api is unknown; the UI answers all other paths.
	unknown := func(w http.ResponseWriter, r *http.Request) {
		s.fail(w, r, notFound("no such endpoint"))
	}
	s.mux.HandleFunc("/api/", unknown)
	s.mux.HandleFunc("/api", unknown)
	s.mux.HandleFunc("/", s.serveUI)
}

// Register adds a handler to the API.
func (s *Server) Register(pattern string, h http.Handler) { s.mux.Handle(pattern, h) }

// load reads the configuration and the plugins again.
func (s *Server) load() (*core.State, error) {
	return core.Load(s.env, s.opts.Build, true)
}

// loaded returns the state and the evaluated plugins.
func (s *Server) newRunner(st *core.State) (*runner.Runner, error) {
	r, err := st.NewRunner()
	if err != nil {
		return nil, err
	}
	r.Locks = s.locks // actions of one plugin queue across requests
	return r, nil
}

// decodeBody reads a request body (at most 1 MiB), decodes it strictly and validates
// it against the component. An empty body is allowed when optional is set and then
// yields an empty object.
func (s *Server) decodeBody(w http.ResponseWriter, r *http.Request, component string, optional bool) (map[string]any, error) {
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBody))
	if err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			return nil, newError(http.StatusRequestEntityTooLarge, codeValidation, "the request body is larger than 1 MiB")
		}
		return nil, invalid("the request body cannot be read")
	}
	if len(bytes.TrimSpace(data)) == 0 {
		if optional {
			return map[string]any{}, nil
		}
		return nil, invalid("the request body is empty", fieldError{Pointer: "", Code: "required", Message: "a JSON object is required"})
	}
	doc, err := schema.Decode(data)
	if err != nil {
		return nil, newError(http.StatusBadRequest, codeValidation, "the request body is not valid JSON")
	}
	if err := s.validator.Validate(schema.OpenAPITarget+"."+component, doc); err != nil {
		var ve *schema.ValidationError
		if errors.As(err, &ve) {
			return nil, invalid("the request body does not follow the schema", violations("", ve)...)
		}
		return nil, err
	}
	obj, _ := doc.(map[string]any)
	return obj, nil
}

// Serve serves the listeners until ctx is cancelled, then shuts down: it stops
// accepting, cancels live sessions (they end as stopped and are recorded) and waits
// for them. A listener whose address is a Unix socket gets the socket handler.
func (s *Server) Serve(ctx context.Context, lns ...net.Listener) error {
	servers := make([]*http.Server, 0, len(lns))
	errc := make(chan error, len(lns))
	for _, ln := range lns {
		h := s.Handler()
		if ln.Addr().Network() == "unix" {
			h = s.SocketHandler()
		} else {
			s.guard.setListen(ln.Addr().String())
		}
		srv := &http.Server{Handler: h, ReadHeaderTimeout: readHeaderTimeout}
		servers = append(servers, srv)
		go func() {
			if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
				errc <- err
				return
			}
			errc <- nil
		}()
	}
	s.StartChecks()
	s.startJanitor()

	var serveErr error
	select {
	case <-ctx.Done():
	case serveErr = <-errc:
	}
	httpCtx, cancel := context.WithTimeout(context.Background(), httpGrace)
	defer cancel()
	for _, srv := range servers {
		if err := srv.Shutdown(httpCtx); err != nil {
			_ = srv.Close()
		}
	}
	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), shutdownGrace)
	defer cancelShutdown()
	if err := s.Shutdown(shutdownCtx); err != nil && serveErr == nil {
		serveErr = err
	}
	return serveErr
}

// Shutdown cancels live sessions and background work and waits for them until ctx ends.
func (s *Server) Shutdown(ctx context.Context) error {
	s.runMu.Lock()
	s.closing = true
	s.runMu.Unlock()
	s.cancel()
	s.hub.close()
	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("api: shutdown: %w", ctx.Err())
	}
}

// spawn runs f in the background until it ends or the server shuts down; it reports
// false when the server is already shutting down.
func (s *Server) spawn(f func()) bool {
	s.runMu.Lock()
	if s.closing {
		s.runMu.Unlock()
		return false
	}
	s.wg.Add(1)
	s.runMu.Unlock()
	go func() {
		defer s.wg.Done()
		f()
	}()
	return true
}
