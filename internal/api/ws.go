package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/Zenit-Hosting-de/devbox/internal/schema"
)

// Limits of a WebSocket connection (docs/concept.md, "API").
const (
	wsReadLimit     = 128 << 10 // bytes of one client message
	wsMaxInput      = 64 << 10  // bytes of one input message, decoded
	wsQueueLimit    = 1024      // live messages a client may lag behind
	wsPingInterval  = 30 * time.Second
	wsIdleTimeout   = 75 * time.Second
	wsWriteTimeout  = 10 * time.Second
	wsJanitorPeriod = time.Minute
)

// WSOptions tune the WebSocket; the zero value is the production setting. The fields
// exist for tests.
type WSOptions struct {
	// BufferBytes is the output kept per session for resuming (2 MiB).
	BufferBytes int
	// KeepFinished and MaxFinished bound the buffers of finished sessions (10 minutes,
	// 32 sessions).
	KeepFinished time.Duration
	MaxFinished  int
	// ProgressInterval is the minimum time between two summaries of a session whose
	// state did not change (250 ms).
	ProgressInterval time.Duration
	// QueueLimit is how many live messages a client may lag behind before it is
	// disconnected as a slow consumer.
	QueueLimit int
	// PingInterval is the time between server pings, IdleTimeout the silence of a
	// client after which the connection is closed, WriteTimeout the time one message
	// may take to be written.
	PingInterval time.Duration
	IdleTimeout  time.Duration
	WriteTimeout time.Duration
}

func (o WSOptions) withDefaults() WSOptions {
	if o.QueueLimit <= 0 {
		o.QueueLimit = wsQueueLimit
	}
	if o.PingInterval <= 0 {
		o.PingInterval = wsPingInterval
	}
	if o.IdleTimeout <= 0 {
		o.IdleTimeout = wsIdleTimeout
	}
	if o.WriteTimeout <= 0 {
		o.WriteTimeout = wsWriteTimeout
	}
	return o
}

func (o WSOptions) hub() hubOptions {
	return hubOptions{
		bufferBytes: o.BufferBytes, keepFinished: o.KeepFinished, maxFinished: o.MaxFinished,
		progressInterval: o.ProgressInterval,
	}
}

// client is the outbound side of one WebSocket connection: a queue of encoded
// messages that the writer drains. Enqueueing never blocks.
type client struct {
	limit  int
	onSlow func()
	// goAway closes the connection with status 1012 (restart); set before the client
	// is attached.
	goAway func()
	notify chan struct{}

	mu   sync.Mutex
	q    [][]byte
	n    int // live messages queued since the writer took the queue
	over bool

	// topics is guarded by the mutex of the hub.
	topics map[string]struct{}
}

func newClient(limit int, onSlow func()) *client {
	return &client{limit: limit, onSlow: onSlow, notify: make(chan struct{}, 1), topics: map[string]struct{}{}}
}

// send queues a message. Messages of a replay are forced: they are bounded by the
// session buffer. A live message beyond the limit marks the client as a slow
// consumer; it is dropped and the connection is closed, the client resumes with fromSeq.
func (c *client) send(msg []byte, force bool) {
	if msg == nil {
		return
	}
	c.mu.Lock()
	if c.over {
		c.mu.Unlock()
		return
	}
	if !force {
		if c.n >= c.limit {
			c.over = true
			c.mu.Unlock()
			c.onSlow()
			return
		}
		c.n++
	}
	c.q = append(c.q, msg)
	c.mu.Unlock()
	select {
	case c.notify <- struct{}{}:
	default:
	}
}

// take hands the queued messages to the writer.
func (c *client) take() [][]byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	q := c.q
	c.q, c.n = nil, 0
	return q
}

// wsConn is one accepted connection.
type wsConn struct {
	s    *Server
	conn *websocket.Conn
	cl   *client
	opts WSOptions

	ctx    context.Context //nolint:containedctx // the lifetime of the connection
	cancel context.CancelFunc

	termMu     sync.Mutex
	terminated chan struct{}
	started    bool
}

// terminate closes the connection with a status from another goroutine; it never
// blocks the caller.
func (w *wsConn) terminate(code websocket.StatusCode, reason string) {
	w.termMu.Lock()
	defer w.termMu.Unlock()
	if w.started || w.ctx.Err() != nil {
		return
	}
	w.started = true
	go func() {
		defer close(w.terminated)
		_ = w.conn.Close(code, reason)
	}()
}

// waitTerminated waits for a close started by terminate.
func (w *wsConn) waitTerminated() {
	w.termMu.Lock()
	started := w.started
	w.termMu.Unlock()
	if started {
		<-w.terminated
	}
}

// track registers a connection with the server so that Shutdown waits for it.
func (s *Server) track() (done func(), ok bool) {
	s.runMu.Lock()
	defer s.runMu.Unlock()
	if s.closing {
		return nil, false
	}
	s.wg.Add(1)
	return s.wg.Done, true
}

// serveWS upgrades the request to a WebSocket and serves it until the client leaves,
// the connection is closed by the core or the server shuts down.
func (s *Server) serveWS(w http.ResponseWriter, r *http.Request) error {
	// The Host check is part of the guard. The Origin of a browser must be allowed;
	// the Unix socket skips this check like all others.
	if !s.CheckOrigin(r) {
		return newError(http.StatusForbidden, codeCSRF, "the Origin header is missing or not allowed")
	}
	if !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
		return newError(http.StatusBadRequest, codeValidation, "a WebSocket upgrade request is required")
	}
	done, ok := s.track()
	if !ok {
		return newError(http.StatusServiceUnavailable, codeInternal, "the service is shutting down")
	}
	defer done()

	// The Origin was checked above against the origins the server allows, so the check
	// of the library (which only knows the Host) is skipped.
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: true, //nolint:gosec // checked by CheckOrigin above
		CompressionMode:    websocket.CompressionDisabled,
	})
	if err != nil {
		s.log.Debug("websocket: upgrade refused", "error", err.Error()) // the library has answered
		return nil
	}
	conn.SetReadLimit(wsReadLimit)

	opts := s.wsOpts
	ctx, cancel := context.WithCancel(context.Background())
	wc := &wsConn{s: s, conn: conn, opts: opts, ctx: ctx, cancel: cancel, terminated: make(chan struct{})}
	wc.cl = newClient(opts.QueueLimit, func() { wc.terminate(websocket.StatusPolicyViolation, "slow consumer") })
	wc.cl.goAway = func() { wc.terminate(websocket.StatusServiceRestart, "restarting") }
	if !s.hub.attach(wc.cl) {
		_ = conn.Close(websocket.StatusGoingAway, "server shutting down")
		cancel()
		return nil
	}
	s.log.Debug("websocket: connected")

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		wc.writeLoop()
	}()
	go func() { // a shutdown of the server ends the connection with a close frame
		defer wg.Done()
		select {
		case <-s.ctx.Done():
			wc.terminate(websocket.StatusGoingAway, "server shutting down")
		case <-ctx.Done():
		}
	}()

	idle := time.AfterFunc(opts.IdleTimeout, func() { wc.terminate(websocket.StatusPolicyViolation, "idle") })
	wc.readLoop(idle)

	idle.Stop()
	cancel()
	wg.Wait()
	wc.waitTerminated()
	s.hub.detach(wc.cl)
	_ = conn.CloseNow()
	s.log.Debug("websocket: disconnected")
	return nil
}

// writeLoop sends the queued messages and the pings.
func (w *wsConn) writeLoop() {
	tick := time.NewTicker(w.opts.PingInterval)
	defer tick.Stop()
	for {
		batch := w.cl.take()
		if len(batch) == 0 {
			select {
			case <-w.cl.notify:
			case <-tick.C:
				batch = [][]byte{msgPing}
			case <-w.ctx.Done():
				return
			}
		} else {
			select {
			case <-tick.C:
				batch = append(batch, msgPing)
			default:
			}
		}
		for _, m := range batch {
			if !w.write(m) {
				w.cancel()
				return
			}
		}
	}
}

func (w *wsConn) write(msg []byte) bool {
	ctx, cancel := context.WithTimeout(w.ctx, w.opts.WriteTimeout)
	defer cancel()
	return w.conn.Write(ctx, websocket.MessageText, msg) == nil
}

// readLoop handles the client messages until the connection ends.
func (w *wsConn) readLoop(idle *time.Timer) {
	for {
		typ, data, err := w.conn.Read(w.ctx)
		if err != nil {
			return
		}
		idle.Reset(w.opts.IdleTimeout)
		if typ != websocket.MessageText {
			w.fail(codeValidation, "messages are JSON objects in text frames", "")
			continue
		}
		w.handle(data)
	}
}

// clientMessage holds the fields of all client messages; the validation against
// WsClientMessage has run before it is decoded.
type clientMessage struct {
	Type    string   `json:"type"`
	Topics  []string `json:"topics"`
	FromSeq *int     `json:"fromSeq"`
	Session string   `json:"session"`
	Data    string   `json:"data"`
	Cols    uint16   `json:"cols"`
	Rows    uint16   `json:"rows"`
}

// fail sends an error message to the client.
func (w *wsConn) fail(code, detail, topic string) {
	w.cl.send(encode(wsError{Type: "error", Code: code, Detail: detail, Topic: topic}), false)
}

// handle validates and executes one client message. Input is never logged.
func (w *wsConn) handle(data []byte) {
	s := w.s
	doc, err := schema.Decode(data)
	if err != nil {
		w.fail(codeValidation, "the message is not valid JSON", "")
		return
	}
	if err := s.validator.Validate(schema.OpenAPITarget+".WsClientMessage", doc); err != nil {
		w.fail(codeValidation, violationText(err), "")
		return
	}
	var m clientMessage
	if err := json.Unmarshal(data, &m); err != nil {
		w.fail(codeValidation, "the message cannot be read", "")
		return
	}
	switch m.Type {
	case "ping":
		w.cl.send(msgPong, false)
	case "pong":
	case "subscribe":
		for _, t := range m.Topics {
			if _, isSession := sessionTopic(t); !isSession {
				s.hub.subscribe(w.cl, t, nil)
				continue
			}
			if !s.hub.subscribe(w.cl, t, m.FromSeq) {
				w.fail(codeNotFound, "no such session, or its output is no longer kept", t)
			}
		}
	case "unsubscribe":
		s.hub.unsubscribe(w.cl, m.Topics)
	case "input":
		w.input(m)
	case "resize":
		w.resize(m)
	}
}

// violationText summarises why a message does not follow the schema.
func violationText(err error) string {
	var ve *schema.ValidationError
	if !errors.As(err, &ve) {
		return "the message does not follow the schema"
	}
	var parts []string
	for _, v := range ve.Violations {
		if !v.Leaf {
			continue
		}
		parts = append(parts, fmt.Sprintf("%s: %s", pointerText(v.Pointer), v.Message))
		if len(parts) == 3 {
			break
		}
	}
	if len(parts) == 0 {
		return "the message does not follow the schema"
	}
	return strings.Join(parts, "; ")
}

func pointerText(p string) string {
	if p == "" {
		return "message"
	}
	return p
}

// sessionFor finds the live session of an input or resize message and reports
// why there is none.
func (w *wsConn) sessionFor(id string) (*LiveSession, bool) {
	if l, ok := w.s.live.get(id); ok {
		return l, true
	}
	if w.s.hub.Known(id) {
		w.fail(codeConflict, id+" has finished", "")
	} else {
		w.fail(codeNotFound, "no such session", "")
	}
	return nil, false
}

func (w *wsConn) input(m clientMessage) {
	l, ok := w.sessionFor(m.Session)
	if !ok {
		return
	}
	if !l.Running() {
		w.fail(codeConflict, m.Session+" is not running", "")
		return
	}
	raw, err := base64.StdEncoding.DecodeString(m.Data)
	if err != nil {
		w.fail(codeValidation, "data is not valid base64", "")
		return
	}
	if len(raw) > wsMaxInput {
		w.fail(codeValidation, "input is larger than 64 KiB", "")
		return
	}
	if err := l.WriteInput(raw); err != nil {
		w.fail(codeConflict, m.Session+" does not take input", "")
	}
}

func (w *wsConn) resize(m clientMessage) {
	l, ok := w.sessionFor(m.Session)
	if !ok {
		return
	}
	if err := l.Resize(m.Cols, m.Rows); err != nil {
		w.fail(codeConflict, m.Session+" is not running", "")
	}
}

// startJanitor drops the buffers of finished sessions that outlived their retention
// while nobody asked for them.
func (s *Server) startJanitor() {
	s.spawn(func() {
		t := time.NewTicker(wsJanitorPeriod)
		defer t.Stop()
		for {
			select {
			case <-s.ctx.Done():
				return
			case <-t.C:
				s.hub.Prune()
			}
		}
	})
}
