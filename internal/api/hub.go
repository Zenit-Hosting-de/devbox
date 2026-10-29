package api

import (
	"encoding/base64"
	"encoding/json"
	"sort"
	"sync"
	"time"

	"github.com/Zenit-Hosting-de/devbox/internal/history"
	"github.com/Zenit-Hosting-de/devbox/internal/runner"
)

// Topics of the WebSocket.
const (
	topicChecks   = "checks"
	topicSessions = "sessions"
	topicSettings = "settings"
	topicPlugins  = "plugins"
	topicSession  = "session:"
)

// Limits of the hub (docs/concept.md, "API"); WSOptions overrides them in tests.
const (
	defaultBufferBytes      = 2 << 20
	defaultKeepFinished     = 10 * time.Minute
	defaultMaxFinished      = 32
	defaultProgressInterval = 250 * time.Millisecond
)

// hubOptions are the limits of a hub.
type hubOptions struct {
	// bufferBytes bounds the payload kept per session.
	bufferBytes int
	// keepFinished and maxFinished bound how long and how many buffers of finished
	// sessions stay.
	keepFinished time.Duration
	maxFinished  int
	// progressInterval is the minimum time between two summaries of a session whose
	// state did not change.
	progressInterval time.Duration
	now              func() time.Time
}

func (o hubOptions) withDefaults() hubOptions {
	if o.bufferBytes <= 0 {
		o.bufferBytes = defaultBufferBytes
	}
	if o.keepFinished <= 0 {
		o.keepFinished = defaultKeepFinished
	}
	if o.maxFinished <= 0 {
		o.maxFinished = defaultMaxFinished
	}
	if o.progressInterval <= 0 {
		o.progressInterval = defaultProgressInterval
	}
	if o.now == nil {
		o.now = time.Now
	}
	return o
}

// bufEntry is one buffered message of a session, encoded once for all subscribers.
type bufEntry struct {
	seq     int
	msg     []byte
	payload int
}

// sessionBuf keeps the recent output and events of one session and the clients that
// follow it.
type sessionBuf struct {
	id      string
	entries []bufEntry
	bytes   int
	// nextSeq is one more than the highest sequence number seen.
	nextSeq int
	subs    map[*client]struct{}

	// summary is the latest encoded "session" message; sentState and lastSent describe
	// the last summary that went out, for the progress throttle.
	summary   []byte
	sentState string
	lastSent  time.Time
	pending   bool
	timer     *time.Timer

	// finished is when the session ended; zero while it runs.
	finished time.Time
}

// firstSeq is the sequence number of the oldest buffered message; for an empty buffer
// it is the number the next message takes.
func (b *sessionBuf) firstSeq() int {
	if len(b.entries) > 0 {
		return b.entries[0].seq
	}
	return b.nextSeq
}

// add appends a message and evicts the oldest ones beyond limit bytes of payload.
func (b *sessionBuf) add(e bufEntry, limit int) {
	b.entries = append(b.entries, e)
	b.bytes += e.payload
	if e.seq >= b.nextSeq {
		b.nextSeq = e.seq + 1
	}
	drop := 0
	for drop < len(b.entries) && b.bytes > limit {
		b.bytes -= b.entries[drop].payload
		b.entries[drop] = bufEntry{} // release the message
		drop++
	}
	if drop > 0 {
		b.entries = b.entries[drop:]
	}
}

// from returns the buffered messages with a sequence number of at least seq.
func (b *sessionBuf) from(seq int) []bufEntry {
	i := sort.Search(len(b.entries), func(i int) bool { return b.entries[i].seq >= seq })
	return b.entries[i:]
}

// Hub implements Broadcaster for the WebSocket: it keeps the recent output of the
// sessions and fans everything out to the subscribed clients. All Broadcaster methods
// only enqueue; a slow client never blocks the core.
type Hub struct {
	opts hubOptions

	mu     sync.Mutex
	closed bool
	// restarting is set while a restart is announced; clients that connect meanwhile
	// get the announcement too.
	restarting bool
	sessions   map[string]*sessionBuf
	clients    map[*client]struct{}
	topics     map[string]map[*client]struct{} // checks, sessions, settings, plugins
}

func newHub(o hubOptions) *Hub {
	return &Hub{
		opts:     o.withDefaults(),
		sessions: map[string]*sessionBuf{},
		clients:  map[*client]struct{}{},
		topics:   map[string]map[*client]struct{}{},
	}
}

// Message shapes (api/openapi.json, Ws*).
type (
	wsOutput struct {
		Type    string `json:"type"`
		Session string `json:"session"`
		Seq     int    `json:"seq"`
		Data    string `json:"data"`
	}
	wsEvent struct {
		Type    string         `json:"type"`
		Session string         `json:"session"`
		Seq     int            `json:"seq"`
		Time    string         `json:"time"`
		Event   map[string]any `json:"event"`
	}
	wsSession struct {
		Type    string  `json:"type"`
		Session Session `json:"session"`
	}
	wsCheck struct {
		Type   string       `json:"type"`
		ID     string       `json:"id"`
		Result CheckSummary `json:"result"`
	}
	wsSettings struct {
		Type string   `json:"type"`
		Keys []string `json:"keys"`
	}
	wsPlugins struct {
		Type         string   `json:"type"`
		IDs          []string `json:"ids"`
		Repositories []string `json:"repositories"`
	}
	wsGap struct {
		Type     string `json:"type"`
		Session  string `json:"session"`
		FromSeq  int    `json:"fromSeq"`
		FirstSeq int    `json:"firstSeq"`
	}
	wsError struct {
		Type   string `json:"type"`
		Code   string `json:"code"`
		Detail string `json:"detail,omitempty"`
		Topic  string `json:"topic,omitempty"`
	}
	wsPlain struct {
		Type string `json:"type"`
	}
)

// encode marshals a message; the messages are plain data, so a failure is a bug and
// yields nil, which is never sent.
func encode(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	return b
}

var (
	msgPing = encode(wsPlain{Type: "ping"})
	msgPong = encode(wsPlain{Type: "pong"})
	// msgRestart announces a restart of the core (api/openapi.json, WsRestart).
	msgRestart = encode(wsPlain{Type: "restart"})
)

func terminalState(state string) bool {
	switch state {
	case runner.StateQueued, runner.StateRunning, "":
		return false
	}
	return true
}

// bufLocked returns the buffer of a session and creates it when it is new.
func (h *Hub) bufLocked(id string) *sessionBuf {
	b, ok := h.sessions[id]
	if !ok {
		b = &sessionBuf{id: id, subs: map[*client]struct{}{}}
		h.sessions[id] = b
	}
	return b
}

// SessionOutput implements Broadcaster.
func (h *Hub) SessionOutput(id string, seq int, data []byte) {
	msg := encode(wsOutput{Type: "output", Session: id, Seq: seq, Data: base64.StdEncoding.EncodeToString(data)})
	if msg == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return
	}
	b := h.bufLocked(id)
	b.add(bufEntry{seq: seq, msg: msg, payload: len(data)}, h.opts.bufferBytes)
	for c := range b.subs {
		c.send(msg, false)
	}
}

// SessionEvent implements Broadcaster.
func (h *Hub) SessionEvent(id string, ev history.Event) {
	msg := encode(wsEvent{Type: "event", Session: id, Seq: ev.Seq, Time: ev.Time, Event: ev.Event})
	if msg == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return
	}
	b := h.bufLocked(id)
	b.add(bufEntry{seq: ev.Seq, msg: msg, payload: len(msg)}, h.opts.bufferBytes)
	for c := range b.subs {
		c.send(msg, false)
	}
}

// SessionChanged implements Broadcaster. Summaries that only differ by progress or
// change count are sent at most once per progress interval; the newest one is not
// lost, a timer sends it.
func (h *Hub) SessionChanged(s Session) {
	msg := encode(wsSession{Type: "session", Session: s})
	if msg == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return
	}
	now := h.opts.now()
	b := h.bufLocked(s.ID)
	b.summary = msg
	if terminalState(s.State) {
		if b.finished.IsZero() {
			b.finished = now
		}
	}
	steady := b.sentState == s.State && !terminalState(s.State) && !b.lastSent.IsZero()
	if steady {
		if wait := h.opts.progressInterval - now.Sub(b.lastSent); wait > 0 {
			b.pending = true
			if b.timer == nil {
				b.timer = time.AfterFunc(wait, func() { h.flushSummary(s.ID) })
			}
			return
		}
	}
	h.sendSummaryLocked(b, s.State, now)
	h.pruneLocked(now)
}

// flushSummary sends a summary that the progress throttle held back.
func (h *Hub) flushSummary(id string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	b, ok := h.sessions[id]
	if !ok || h.closed {
		return
	}
	b.timer = nil
	if !b.pending {
		return
	}
	state := b.sentState
	h.sendSummaryLocked(b, state, h.opts.now())
}

// sendSummaryLocked sends the latest summary to the clients of the session and of the
// sessions topic, each client once.
func (h *Hub) sendSummaryLocked(b *sessionBuf, state string, now time.Time) {
	b.pending = false
	if b.timer != nil {
		b.timer.Stop()
		b.timer = nil
	}
	b.lastSent = now
	b.sentState = state
	for c := range b.subs {
		c.send(b.summary, false)
	}
	for c := range h.topics[topicSessions] {
		if _, dup := b.subs[c]; !dup {
			c.send(b.summary, false)
		}
	}
}

// CheckChanged implements Broadcaster.
func (h *Hub) CheckChanged(c CheckSummary) {
	msg := encode(wsCheck{Type: "check", ID: c.ID, Result: c})
	h.publish(topicChecks, msg)
}

// SettingsChanged implements Broadcaster.
func (h *Hub) SettingsChanged(keys []string) {
	if keys == nil {
		keys = []string{}
	}
	h.publish(topicSettings, encode(wsSettings{Type: "settings", Keys: keys}))
}

// PluginsChanged implements Broadcaster.
func (h *Hub) PluginsChanged(ids, repositories []string) {
	if ids == nil {
		ids = []string{}
	}
	if repositories == nil {
		repositories = []string{}
	}
	h.publish(topicPlugins, encode(wsPlugins{Type: "plugins", IDs: ids, Repositories: repositories}))
}

// RestartRequested implements Broadcaster: every connected client, whatever it
// subscribed to, and every client that connects before clearRestart gets the message.
func (h *Hub) RestartRequested() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return
	}
	h.restarting = true
	for c := range h.clients {
		c.send(msgRestart, true)
	}
}

// clearRestart withdraws the announcement: the restart did not happen.
func (h *Hub) clearRestart() {
	h.mu.Lock()
	h.restarting = false
	h.mu.Unlock()
}

// goAway asks every connection to close with status 1012 (service restart); the
// clients reconnect on their own. It does not wait.
func (h *Hub) goAway() {
	h.mu.Lock()
	defer h.mu.Unlock()
	for c := range h.clients {
		if c.goAway != nil {
			c.goAway()
		}
	}
}

func (h *Hub) publish(topic string, msg []byte) {
	if msg == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return
	}
	for c := range h.topics[topic] {
		c.send(msg, false)
	}
}

// pruneLocked drops the buffers of sessions that finished too long ago or beyond the
// limit of finished buffers, oldest first.
func (h *Hub) pruneLocked(now time.Time) {
	var done []*sessionBuf
	for _, b := range h.sessions {
		if b.finished.IsZero() {
			continue
		}
		if now.Sub(b.finished) > h.opts.keepFinished {
			h.dropLocked(b)
			continue
		}
		done = append(done, b)
	}
	if len(done) <= h.opts.maxFinished {
		return
	}
	sort.Slice(done, func(i, j int) bool { return done[i].finished.Before(done[j].finished) })
	for _, b := range done[:len(done)-h.opts.maxFinished] {
		h.dropLocked(b)
	}
}

func (h *Hub) dropLocked(b *sessionBuf) {
	if b.timer != nil {
		b.timer.Stop()
	}
	for c := range b.subs {
		delete(c.topics, topicSession+b.id)
	}
	delete(h.sessions, b.id)
}

// Prune applies the retention of finished sessions now.
func (h *Hub) Prune() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.pruneLocked(h.opts.now())
}

// Known reports whether the hub has the output of a session: it is live or finished
// recently.
func (h *Hub) Known(id string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.pruneLocked(h.opts.now())
	_, ok := h.sessions[id]
	return ok
}

// attach registers a connection.
func (h *Hub) attach(c *client) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return false
	}
	h.clients[c] = struct{}{}
	if h.restarting {
		c.send(msgRestart, true)
	}
	return true
}

// detach removes a connection and all its subscriptions.
func (h *Hub) detach(c *client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.clients, c)
	for t := range c.topics {
		h.unsubscribeLocked(c, t)
	}
}

// clientCount is the number of connected clients.
func (h *Hub) clientCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.clients)
}

// subscribe adds c to a topic. For a session topic it queues the current summary and,
// from the buffer, every output and event message with a sequence number of at least
// *fromSeq (all buffered messages when nil), in order, before any live message: the
// registration and the copy happen under one lock, so nothing is lost or repeated.
// When fromSeq is older than the buffer a gap message goes first. It reports false
// for a session that is neither live nor buffered.
func (h *Hub) subscribe(c *client, topic string, fromSeq *int) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return false
	}
	id, isSession := sessionTopic(topic)
	if !isSession {
		if h.topics[topic] == nil {
			h.topics[topic] = map[*client]struct{}{}
		}
		h.topics[topic][c] = struct{}{}
		c.topics[topic] = struct{}{}
		return true
	}
	h.pruneLocked(h.opts.now())
	b, ok := h.sessions[id]
	if !ok {
		return false
	}
	b.subs[c] = struct{}{}
	c.topics[topic] = struct{}{}
	if b.summary != nil {
		c.send(b.summary, true)
	}
	start := 0
	if fromSeq != nil {
		start = *fromSeq
		if first := b.firstSeq(); start < first {
			c.send(encode(wsGap{Type: "gap", Session: id, FromSeq: start, FirstSeq: first}), true)
			start = first
		}
	}
	for _, e := range b.from(start) {
		c.send(e.msg, true)
	}
	return true
}

// unsubscribe removes c from topics it follows.
func (h *Hub) unsubscribe(c *client, topics []string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, t := range topics {
		h.unsubscribeLocked(c, t)
	}
}

func (h *Hub) unsubscribeLocked(c *client, topic string) {
	delete(c.topics, topic)
	if id, ok := sessionTopic(topic); ok {
		if b, ok := h.sessions[id]; ok {
			delete(b.subs, c)
		}
		return
	}
	delete(h.topics[topic], c)
}

func sessionTopic(topic string) (string, bool) {
	if len(topic) > len(topicSession) && topic[:len(topicSession)] == topicSession {
		return topic[len(topicSession):], true
	}
	return "", false
}

// close stops the hub: nothing is broadcast any more and the timers stop.
func (h *Hub) close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.closed = true
	for _, b := range h.sessions {
		if b.timer != nil {
			b.timer.Stop()
			b.timer = nil
		}
	}
}

// multiBroadcaster passes every call on to several broadcasters.
type multiBroadcaster []Broadcaster

// SessionOutput implements Broadcaster.
func (m multiBroadcaster) SessionOutput(id string, seq int, data []byte) {
	for _, b := range m {
		b.SessionOutput(id, seq, data)
	}
}

// SessionEvent implements Broadcaster.
func (m multiBroadcaster) SessionEvent(id string, ev history.Event) {
	for _, b := range m {
		b.SessionEvent(id, ev)
	}
}

// SessionChanged implements Broadcaster.
func (m multiBroadcaster) SessionChanged(s Session) {
	for _, b := range m {
		b.SessionChanged(s)
	}
}

// CheckChanged implements Broadcaster.
func (m multiBroadcaster) CheckChanged(c CheckSummary) {
	for _, b := range m {
		b.CheckChanged(c)
	}
}

// SettingsChanged implements Broadcaster.
func (m multiBroadcaster) SettingsChanged(keys []string) {
	for _, b := range m {
		b.SettingsChanged(keys)
	}
}

// PluginsChanged implements Broadcaster.
func (m multiBroadcaster) PluginsChanged(ids, repositories []string) {
	for _, b := range m {
		b.PluginsChanged(ids, repositories)
	}
}

// RestartRequested implements Broadcaster.
func (m multiBroadcaster) RestartRequested() {
	for _, b := range m {
		b.RestartRequested()
	}
}
