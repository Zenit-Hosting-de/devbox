package api

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/Zenit-Hosting-de/devbox/internal/runner"
	"github.com/Zenit-Hosting-de/devbox/internal/schema"
)

func specWith(cols, rows uint16) runner.Spec {
	return runner.Spec{SessionID: "s_01J0000000000000000000", PluginID: "demo", Cols: cols, Rows: rows}
}

const termManifest = `{
  "id": "term",
  "version": "1.0.0",
  "name": "Term",
  "actions": {
    "count": { "label": "Count", "run": ["act.sh", "count"] },
    "ask": { "label": "Ask", "run": ["act.sh", "ask"] },
    "winch": { "label": "Winch", "run": ["act.sh", "winch"] }
  }
}`

const termActions = `#!/usr/bin/env bash
set -euo pipefail
case "$1" in
count)
  for i in $(seq 1 60); do
    echo "line $i"
    if [ $((i % 10)) -eq 0 ]; then
      printf '%s\n' '{"type":"progress","percent":'$((i * 100 / 60))',"text":"counting"}' >&3
    fi
    sleep 0.03
  done
  ;;
ask)
  echo ready
  read -r line
  echo "got:$line"
  ;;
winch)
  trap 'stty size' WINCH
  echo ready
  for _ in $(seq 1 400); do sleep 0.05; done
  ;;
esac
`

// wsEnv is a test server with a real listener, so WebSocket clients can connect.
type wsEnv struct {
	*testEnv
	ts *httptest.Server
}

func newWSEnv(t *testing.T, ws WSOptions) *wsEnv {
	t.Helper()
	e := newTestEnv(t, func(o *Options) { o.WebSocket = ws })
	write(t, filepath.Join(e.plugins, "term", "plugin.json"), termManifest, 0o600)
	write(t, filepath.Join(e.plugins, "term", "act.sh"), termActions, 0o700)
	ts := httptest.NewServer(e.h)
	t.Cleanup(ts.Close)
	e.srv.guard.setListen(ts.Listener.Addr().String())
	// the helpers of testEnv still send requests to the handler with the fixed host
	e.srv.guard.mu.Lock()
	e.srv.guard.hosts[testListen] = true
	e.srv.guard.origins["http://"+testListen] = true
	e.srv.guard.mu.Unlock()
	return &wsEnv{testEnv: e, ts: ts}
}

func (e *wsEnv) addr() string { return e.ts.Listener.Addr().String() }

// request builds a request for the test server as a browser of the UI would.
func (e *wsEnv) httpRequest(method, path string, body any) (int, []byte) {
	e.t.Helper()
	var rd io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			e.t.Fatal(err)
		}
		rd = bytes.NewReader(data)
	}
	req, err := http.NewRequest(method, e.ts.URL+"/api/v1"+path, rd)
	if err != nil {
		e.t.Fatal(err)
	}
	req.Header.Set(requestHeader, "1")
	req.Header.Set("Origin", e.ts.URL)
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	data, _ := io.ReadAll(res.Body)
	return res.StatusCode, data
}

// wsClient is a WebSocket client; every server message it receives is validated
// against WsServerMessage.
type wsClient struct {
	t    *testing.T
	conn *websocket.Conn
	in   chan map[string]any
	done chan struct{}

	mu    sync.Mutex
	close websocket.StatusCode
}

func (e *wsEnv) dialOrigin(origin string) (*wsClient, *http.Response, error) {
	e.t.Helper()
	hdr := http.Header{}
	if origin != "" {
		hdr.Set("Origin", origin)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn, resp, err := websocket.Dial(ctx, "ws://"+e.addr()+"/api/v1/ws", &websocket.DialOptions{HTTPHeader: hdr})
	if err != nil {
		return nil, resp, err
	}
	c := &wsClient{t: e.t, conn: conn, in: make(chan map[string]any, 1<<16), done: make(chan struct{}), close: -1}
	_, v := contract(e.t)
	go func() {
		defer close(c.done)
		for {
			typ, data, err := conn.Read(context.Background())
			if err != nil {
				c.mu.Lock()
				c.close = websocket.CloseStatus(err)
				c.mu.Unlock()
				return
			}
			if typ != websocket.MessageText {
				e.t.Errorf("a binary frame arrived")
				continue
			}
			if err := v.ValidateJSON(schema.OpenAPITarget+".WsServerMessage", data); err != nil {
				e.t.Errorf("server message does not follow WsServerMessage: %v\n%s", err, data)
			}
			var m map[string]any
			if err := json.Unmarshal(data, &m); err != nil {
				e.t.Errorf("not a JSON object: %s", data)
				continue
			}
			c.in <- m
		}
	}()
	e.t.Cleanup(func() {
		_ = conn.CloseNow()
		<-c.done
	})
	return c, resp, nil
}

func (e *wsEnv) dial() *wsClient {
	e.t.Helper()
	c, resp, err := e.dialOrigin(e.ts.URL)
	if err != nil {
		e.t.Fatalf("dial: %v (%v)", err, resp)
	}
	// the upgrade is part of the contract
	req := httptest.NewRequest(http.MethodGet, "/api/v1/ws", nil)
	checkContract(e.t, req, response{code: resp.StatusCode, header: resp.Header})
	return c
}

func (c *wsClient) send(v any) {
	c.t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		c.t.Fatal(err)
	}
	c.sendRaw(data)
}

func (c *wsClient) sendRaw(data []byte) {
	c.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := c.conn.Write(ctx, websocket.MessageText, data); err != nil {
		c.t.Fatalf("send: %v", err)
	}
}

func (c *wsClient) subscribe(fromSeq *int, topics ...string) {
	c.t.Helper()
	m := map[string]any{"type": "subscribe", "topics": topics}
	if fromSeq != nil {
		m["fromSeq"] = *fromSeq
	}
	c.send(m)
}

// next returns the next message that is not a ping.
func (c *wsClient) next() map[string]any {
	c.t.Helper()
	for {
		m := c.nextAny(15 * time.Second)
		if m == nil {
			c.t.Fatal("no message within 15 s")
		}
		if m["type"] != "ping" {
			return m
		}
	}
}

func (c *wsClient) nextAny(wait time.Duration) map[string]any {
	select {
	case m := <-c.in:
		return m
	case <-time.After(wait):
		return nil
	}
}

// until returns the messages up to and including the first one that matches.
func (c *wsClient) until(match func(map[string]any) bool) []map[string]any {
	c.t.Helper()
	var out []map[string]any
	for {
		m := c.next()
		out = append(out, m)
		if match(m) {
			return out
		}
	}
}

func (c *wsClient) closeStatus() websocket.StatusCode {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.close
}

// finished matches the summary of a session that has ended.
func finished(m map[string]any) bool {
	if m["type"] != "session" {
		return false
	}
	switch s, _ := m["session"].(map[string]any); s["state"] {
	case "queued", "running":
		return false
	}
	return true
}

// outputContains matches output whose decoded data contains text.
func outputContains(text string) func(map[string]any) bool {
	return func(m map[string]any) bool {
		return m["type"] == "output" && strings.Contains(decode(m), text)
	}
}

func decode(m map[string]any) string {
	raw, _ := base64.StdEncoding.DecodeString(m["data"].(string))
	return string(raw)
}

// record is an output or event message of a stream.
type record struct {
	kind string
	seq  int
	data string
}

// records keeps the output and event messages.
func records(ms []map[string]any) []record {
	var out []record
	for _, m := range ms {
		switch m["type"] {
		case "output":
			out = append(out, record{"output", int(m["seq"].(float64)), decode(m)})
		case "event":
			ev, _ := json.Marshal(m["event"])
			out = append(out, record{"event", int(m["seq"].(float64)), string(ev)})
		}
	}
	return out
}

func joinOutput(rs []record) string {
	var b strings.Builder
	for _, r := range rs {
		if r.kind == "output" {
			b.WriteString(r.data)
		}
	}
	return b.String()
}

func topic(id string) string { return "session:" + id }

func TestWSResumeWithFromSeqGivesContiguousOutput(t *testing.T) {
	e := newWSEnv(t, WSOptions{})
	id := e.start(map[string]any{"action": "term.count"})

	first := e.dial()
	first.subscribe(nil, topic(id))
	var got []map[string]any
	lines := 0
	for lines < 5 {
		m := first.next()
		got = append(got, m)
		if m["type"] == "output" {
			lines += strings.Count(decode(m), "\n")
		}
	}
	part1 := records(got)
	last := part1[len(part1)-1].seq
	_ = first.conn.CloseNow() // the window goes away while the action runs

	second := e.dial()
	from := last + 1
	second.subscribe(&from, topic(id))
	part2 := records(second.until(finished))

	all := append(append([]record{}, part1...), part2...)
	for i, r := range all {
		if r.seq != i {
			t.Fatalf("message %d has seq %d: a gap or a duplicate (%v)", i, r.seq, seqList(all))
		}
	}
	e.waitFinished(id)
	log := e.get("/sessions/" + id + "/log")
	e.wantStatus(log, http.StatusOK)
	if joined := joinOutput(all); joined != string(log.body) {
		t.Errorf("the streamed output differs from the log\nstreamed: %q\nlog:      %q", joined, log.body)
	}
	if !strings.Contains(joinOutput(all), "line 60") {
		t.Error("the output is incomplete")
	}
	var events int
	for _, r := range all {
		if r.kind == "event" {
			events++
		}
	}
	if events == 0 {
		t.Error("events share the sequence counter with the output, but none arrived")
	}
}

func seqList(rs []record) []int {
	out := make([]int, len(rs))
	for i, r := range rs {
		out[i] = r.seq
	}
	return out
}

func TestWSTwoWindowsSeeTheSameSession(t *testing.T) {
	e := newWSEnv(t, WSOptions{})
	id := e.start(map[string]any{"action": "term.count"})
	a, b := e.dial(), e.dial()
	a.subscribe(nil, topic(id))
	time.Sleep(150 * time.Millisecond) // b joins while the session runs
	b.subscribe(nil, topic(id))
	ra := records(a.until(finished))
	rb := records(b.until(finished))
	if len(ra) == 0 {
		t.Fatal("no output")
	}
	if len(ra) != len(rb) {
		t.Fatalf("window A got %d messages, window B %d", len(ra), len(rb))
	}
	for i := range ra {
		if ra[i] != rb[i] {
			t.Fatalf("message %d differs: %+v vs %+v", i, ra[i], rb[i])
		}
	}
}

func TestWSInputReachesThePTYAndIsNeverLogged(t *testing.T) {
	e := newWSEnv(t, WSOptions{})
	id := e.start(map[string]any{"action": "term.ask"})
	c := e.dial()
	c.subscribe(nil, topic(id))
	c.until(outputContains("ready"))

	const secret = "hunter2-not-for-the-log"
	c.send(map[string]any{"type": "input", "session": id, "data": base64.StdEncoding.EncodeToString([]byte(secret + "\n"))})
	c.until(outputContains("got:" + secret))
	c.until(finished)

	if logs := e.logs.String(); strings.Contains(logs, secret) || strings.Contains(logs, base64.StdEncoding.EncodeToString([]byte(secret))) {
		t.Errorf("the input appears in the log:\n%s", logs)
	}
}

func TestWSResizeChangesThePTY(t *testing.T) {
	e := newWSEnv(t, WSOptions{})
	id := e.start(map[string]any{"action": "term.winch"})
	c := e.dial()
	c.subscribe(nil, topic(id))
	c.until(outputContains("ready"))
	c.send(map[string]any{"type": "resize", "session": id, "cols": 100, "rows": 40})
	c.until(outputContains("40 100"))

	l, ok := e.srv.Live(id)
	if !ok {
		t.Fatal("the session is gone")
	}
	l.Cancel()
	c.until(finished)
}

func TestLiveSessionKeepsASizeUntilTheTerminalExists(t *testing.T) {
	l := newLive(specWith(80, 24), "demo.install", "", "", "", func() {})
	var applied [][2]uint16
	apply := func(c, r uint16) { applied = append(applied, [2]uint16{c, r}) }

	l.SetResizer(apply)
	if len(applied) != 0 {
		t.Errorf("the size of the start must not be applied again: %v", applied)
	}
	l = newLive(specWith(80, 24), "demo.install", "", "", "", func() {})
	l.Resize(100, 40) // the process has no terminal yet
	l.Resize(120, 50)
	l.SetResizer(apply)
	if len(applied) != 1 || applied[0] != [2]uint16{120, 50} {
		t.Errorf("applied = %v, want the last size once", applied)
	}
	l.Resize(90, 30)
	if len(applied) != 2 || applied[1] != [2]uint16{90, 30} {
		t.Errorf("applied = %v, want the new size at once", applied)
	}
}

func TestWSGapWhenFromSeqIsOlderThanTheBuffer(t *testing.T) {
	e := newWSEnv(t, WSOptions{BufferBytes: 300})
	id := e.start(map[string]any{"action": "term.count"})
	e.waitFinished(id)

	c := e.dial()
	zero := 0
	c.subscribe(&zero, topic(id))
	got := c.until(func(m map[string]any) bool { return outputContains("line 60")(m) })
	var gap map[string]any
	var order []string
	for _, m := range got {
		order = append(order, m["type"].(string))
		if m["type"] == "gap" {
			gap = m
		}
	}
	if gap == nil {
		t.Fatalf("no gap message in %v", order)
	}
	first := int(gap["firstSeq"].(float64))
	if gap["fromSeq"].(float64) != 0 || first == 0 || gap["session"] != id {
		t.Errorf("gap = %v", gap)
	}
	rs := records(got)
	if len(rs) == 0 || rs[0].seq != first {
		t.Fatalf("the stream must continue at firstSeq %d: %v", first, seqList(rs))
	}
	for i := 1; i < len(rs); i++ {
		if rs[i].seq != rs[i-1].seq+1 {
			t.Fatalf("a gap in the continued stream: %v", seqList(rs))
		}
	}
	log := e.get("/sessions/" + id + "/log")
	if !strings.HasSuffix(string(log.body), joinOutput(rs)) {
		t.Errorf("the buffered tail is not the end of the log")
	}

	// a fromSeq inside the buffer gives no gap
	c2 := e.dial()
	c2.subscribe(&first, topic(id))
	for _, m := range c2.until(outputContains("line 60")) {
		if m["type"] == "gap" {
			t.Errorf("unexpected gap for fromSeq %d: %v", first, m)
		}
	}
}

func TestWSForeignOriginIsRejected(t *testing.T) {
	e := newWSEnv(t, WSOptions{})
	for name, origin := range map[string]string{
		"foreign": "http://evil.example.com",
		"missing": "",
		"port":    "http://127.0.0.1:1",
	} {
		t.Run(name, func(t *testing.T) {
			_, resp, err := e.dialOrigin(origin)
			if err == nil {
				t.Fatal("the upgrade was accepted")
			}
			if resp == nil || resp.StatusCode != http.StatusForbidden {
				t.Fatalf("response = %v (%v), want 403", resp, err)
			}
			body, _ := io.ReadAll(resp.Body)
			req := httptest.NewRequest(http.MethodGet, "/api/v1/ws", nil)
			checkContract(t, req, response{code: resp.StatusCode, header: resp.Header, body: body})
			if p := (response{body: body}).problem(t); p.Code != codeCSRF {
				t.Errorf("code = %q, want csrf", p.Code)
			}
		})
	}
}

func TestWSWrongHostIsRejected(t *testing.T) {
	e := newWSEnv(t, WSOptions{})
	req, err := http.NewRequest(http.MethodGet, e.ts.URL+"/api/v1/ws", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Host = "evil.example.com"
	req.Header.Set("Origin", e.ts.URL)
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Sec-WebSocket-Version", "13")
	req.Header.Set("Sec-WebSocket-Key", "dGhlIHNhbXBsZSBub25jZQ==")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	if res.StatusCode != http.StatusForbidden {
		t.Errorf("status %d, want 403", res.StatusCode)
	}
}

func TestWSPlainRequestIsNotAnUpgrade(t *testing.T) {
	e := newWSEnv(t, WSOptions{})
	req := httptestGet(e, "/ws")
	if req.code != http.StatusBadRequest || req.problem(t).Code != codeValidation {
		t.Errorf("response = %d %s", req.code, req.body)
	}
}

func httptestGet(e *wsEnv, path string) response {
	e.t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/v1"+path, nil)
	req.Host = testListen
	req.Header.Set("Origin", "http://"+testListen)
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	return response{code: rec.Code, header: rec.Header(), body: rec.Body.Bytes()}
}

func TestWSUnixSocketSkipsTheOriginCheck(t *testing.T) {
	e := newWSEnv(t, WSOptions{})
	ts := httptest.NewServer(e.srv.SocketHandler())
	t.Cleanup(ts.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "ws://"+ts.Listener.Addr().String()+"/api/v1/ws", nil)
	if err != nil {
		t.Fatalf("dial without Origin: %v", err)
	}
	defer func() { _ = conn.CloseNow() }()
	if err := conn.Write(ctx, websocket.MessageText, []byte(`{"type":"ping"}`)); err != nil {
		t.Fatal(err)
	}
	_, data, err := conn.Read(ctx)
	if err != nil || string(data) != `{"type":"pong"}` {
		t.Errorf("answer = %q (%v), want pong", data, err)
	}
}

func TestWSInvalidMessagesKeepTheConnection(t *testing.T) {
	e := newWSEnv(t, WSOptions{})
	c := e.dial()
	tests := []struct {
		name string
		raw  string
	}{
		{"not JSON", `{"type":`},
		{"unknown type", `{"type":"dance"}`},
		{"unknown key", `{"type":"ping","extra":1}`},
		{"no topics", `{"type":"subscribe","topics":[]}`},
		{"bad topic", `{"type":"subscribe","topics":["credentials"]}`},
		{"bad session ID", `{"type":"input","session":"x","data":"AA=="}`},
		{"resize too large", `{"type":"resize","session":"s_01J0000000000000000000","cols":5000,"rows":1}`},
		{"not an object", `[1,2]`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c.sendRaw([]byte(tt.raw))
			m := c.next()
			if m["type"] != "error" || m["code"] != codeValidation || m["detail"] == "" {
				t.Fatalf("answer = %v, want a validation error", m)
			}
			c.send(map[string]any{"type": "ping"})
			if m := c.next(); m["type"] != "pong" {
				t.Fatalf("the connection is not usable after the error: %v", m)
			}
		})
	}
	// a binary frame is refused, too
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := c.conn.Write(ctx, websocket.MessageBinary, []byte{1, 2}); err != nil {
		t.Fatal(err)
	}
	if m := c.next(); m["type"] != "error" {
		t.Errorf("answer = %v", m)
	}
}

func TestWSRefusesAMessageOverTheReadLimit(t *testing.T) {
	e := newWSEnv(t, WSOptions{})
	c := e.dial()
	c.sendRaw(append([]byte(`{"type":"ping","x":"`), append(bytes.Repeat([]byte("a"), wsReadLimit), '"', '}')...))
	select {
	case <-c.done:
	case <-time.After(10 * time.Second):
		t.Fatal("the connection stayed open")
	}
	if got := c.closeStatus(); got != websocket.StatusMessageTooBig {
		t.Errorf("close status %d, want %d", got, websocket.StatusMessageTooBig)
	}
}

func TestWSUnknownSessionAndWrongStateAnswersWithErrors(t *testing.T) {
	e := newWSEnv(t, WSOptions{})
	c := e.dial()
	const unknown = "s_01J0000000000000000000"

	c.subscribe(nil, topic(unknown), "checks")
	m := c.next()
	if m["type"] != "error" || m["code"] != codeNotFound || m["topic"] != topic(unknown) {
		t.Errorf("subscribe to an unknown session: %v", m)
	}
	c.send(map[string]any{"type": "input", "session": unknown, "data": "AA=="})
	if m := c.next(); m["code"] != codeNotFound {
		t.Errorf("input to an unknown session: %v", m)
	}
	c.send(map[string]any{"type": "resize", "session": unknown, "cols": 10, "rows": 10})
	if m := c.next(); m["code"] != codeNotFound {
		t.Errorf("resize of an unknown session: %v", m)
	}

	id := e.start(map[string]any{"action": "term.count"})
	e.waitFinished(id)
	c.send(map[string]any{"type": "input", "session": id, "data": "AA=="})
	if m := c.next(); m["type"] != "error" || m["code"] != codeConflict {
		t.Errorf("input to a finished session: %v", m)
	}
	c.send(map[string]any{"type": "resize", "session": id, "cols": 10, "rows": 10})
	if m := c.next(); m["code"] != codeConflict {
		t.Errorf("resize of a finished session: %v", m)
	}

	run := e.start(map[string]any{"action": "term.ask"})
	c.subscribe(nil, topic(run))
	c.until(outputContains("ready"))
	c.send(map[string]any{"type": "input", "session": run, "data": "***"})
	if m := c.next(); m["code"] != codeValidation {
		t.Errorf("input that is not base64: %v", m)
	}
	big := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte("a"), wsMaxInput+3))
	c.send(map[string]any{"type": "input", "session": run, "data": big})
	if m := c.next(); m["code"] != codeValidation {
		t.Errorf("input over 64 KiB: %v", m)
	}
	l, _ := e.srv.Live(run)
	l.Cancel()
	c.until(finished)
}

func TestWSChecksAndSettingsTopics(t *testing.T) {
	e := newWSEnv(t, WSOptions{})
	c := e.dial()
	c.subscribe(nil, "checks", "settings")

	code, body := e.httpRequest(http.MethodPost, "/checks/refresh", nil)
	if code != http.StatusAccepted {
		t.Fatalf("refresh: %d %s", code, body)
	}
	m := c.until(func(m map[string]any) bool { return m["type"] == "check" })
	check := m[len(m)-1]
	if check["id"] != "demo.state" || check["result"].(map[string]any)["status"] != "ok" {
		t.Errorf("check = %v", check)
	}

	code, body = e.httpRequest(http.MethodPatch, "/settings", map[string]any{"demo.mode": "slow"})
	if code != http.StatusNoContent {
		t.Fatalf("patch: %d %s", code, body)
	}
	m = c.until(func(m map[string]any) bool { return m["type"] == "settings" })
	if keys, _ := m[len(m)-1]["keys"].([]any); len(keys) != 1 || keys[0] != "demo.mode" {
		t.Errorf("settings = %v", m[len(m)-1])
	}

	c.send(map[string]any{"type": "unsubscribe", "topics": []string{"settings"}})
	c.send(map[string]any{"type": "ping"})
	c.until(func(m map[string]any) bool { return m["type"] == "pong" })
	if code, body := e.httpRequest(http.MethodPatch, "/settings", map[string]any{"demo.mode": "fast"}); code != http.StatusNoContent {
		t.Fatalf("patch: %d %s", code, body)
	}
	if m := c.nextAny(300 * time.Millisecond); m != nil {
		t.Errorf("a message after unsubscribe: %v", m)
	}
}

func TestWSSessionsTopicSeesNewSessions(t *testing.T) {
	e := newWSEnv(t, WSOptions{})
	c := e.dial()
	c.subscribe(nil, "sessions")
	id := e.start(map[string]any{"action": "term.count"})
	m := c.until(finished)
	last := m[len(m)-1]["session"].(map[string]any)
	if last["id"] != id || last["action"] != "term.count" {
		t.Errorf("last summary = %v", last)
	}
	if first := m[0]["session"].(map[string]any); first["state"] != "queued" && first["state"] != "running" {
		t.Errorf("first summary = %v", first)
	}
	// progress events change the summary, at most 4 per second
	var withProgress int
	for _, s := range m {
		if _, ok := s["session"].(map[string]any)["progress"]; ok {
			withProgress++
		}
	}
	if withProgress == 0 {
		t.Error("no summary with progress")
	}
}

func TestWSPingPongAndIdleTimeout(t *testing.T) {
	e := newWSEnv(t, WSOptions{PingInterval: 40 * time.Millisecond, IdleTimeout: 400 * time.Millisecond})
	c := e.dial()

	if m := c.nextAny(2 * time.Second); m == nil || m["type"] != "ping" {
		t.Fatalf("the server sent %v, want a ping", m)
	}
	c.send(map[string]any{"type": "ping"})
	c.until(func(m map[string]any) bool { return m["type"] == "pong" })

	// answering keeps the connection alive beyond the idle time
	deadline := time.Now().Add(900 * time.Millisecond)
	for time.Now().Before(deadline) {
		c.send(map[string]any{"type": "pong"})
		time.Sleep(100 * time.Millisecond)
	}
	select {
	case <-c.done:
		t.Fatal("a connection that answers was closed")
	default:
	}

	// silence ends it
	select {
	case <-c.done:
	case <-time.After(3 * time.Second):
		t.Fatal("an idle connection stayed open")
	}
	if got := c.closeStatus(); got != websocket.StatusPolicyViolation {
		t.Errorf("close status %d, want policy violation", got)
	}
}

func TestWSShutdownClosesConnections(t *testing.T) {
	e := newWSEnv(t, WSOptions{})
	a, b := e.dial(), e.dial()
	a.subscribe(nil, "checks")
	b.send(map[string]any{"type": "ping"})
	b.until(func(m map[string]any) bool { return m["type"] == "pong" })
	for e.srv.hub.clientCount() != 2 {
		time.Sleep(5 * time.Millisecond)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	start := time.Now()
	if err := e.srv.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Errorf("shutdown took %v", d)
	}
	for i, c := range []*wsClient{a, b} {
		select {
		case <-c.done:
		case <-time.After(5 * time.Second):
			t.Fatalf("client %d is still connected", i)
		}
		if got := c.closeStatus(); got != websocket.StatusGoingAway {
			t.Errorf("client %d: close status %d, want going away", i, got)
		}
	}
	if n := e.srv.hub.clientCount(); n != 0 {
		t.Errorf("%d clients registered after shutdown", n)
	}
	// no new connection once the server is down
	if _, _, err := e.dialOrigin(e.ts.URL); err == nil {
		t.Error("a connection was accepted during shutdown")
	}
}

func TestWSClientsAreRemovedWhenTheyLeave(t *testing.T) {
	e := newWSEnv(t, WSOptions{})
	id := e.start(map[string]any{"action": "term.count"})
	for i := 0; i < 5; i++ {
		c := e.dial()
		c.subscribe(nil, topic(id), "checks", "sessions")
		c.next()
		_ = c.conn.Close(websocket.StatusNormalClosure, "")
		<-c.done
	}
	deadline := time.Now().Add(5 * time.Second)
	for e.srv.hub.clientCount() != 0 {
		if time.Now().After(deadline) {
			t.Fatalf("%d clients stayed registered", e.srv.hub.clientCount())
		}
		time.Sleep(10 * time.Millisecond)
	}
	e.srv.hub.mu.Lock()
	subs := len(e.srv.hub.sessions[id].subs) + len(e.srv.hub.topics["checks"]) + len(e.srv.hub.topics["sessions"])
	e.srv.hub.mu.Unlock()
	if subs != 0 {
		t.Errorf("%d subscriptions stayed", subs)
	}
	if l, ok := e.srv.Live(id); ok {
		l.Cancel()
	}
}

func TestWSSlowConsumerIsDisconnected(t *testing.T) {
	// A client that cannot keep up gets a policy-violation close; here the queue
	// limit is hit directly because loopback never backs up.
	e := newWSEnv(t, WSOptions{QueueLimit: 2})
	c := e.dial()
	c.subscribe(nil, "checks")
	for {
		e.srv.hub.mu.Lock()
		n := len(e.srv.hub.topics["checks"])
		e.srv.hub.mu.Unlock()
		if n == 1 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	e.srv.hub.mu.Lock()
	var cl *client
	for k := range e.srv.hub.clients {
		cl = k
	}
	e.srv.hub.mu.Unlock()
	// stall the writer by holding the queue lock while messages arrive
	cl.mu.Lock()
	cl.n = cl.limit
	cl.mu.Unlock()
	e.srv.hub.CheckChanged(CheckSummary{ID: "demo.state", Status: "ok", Summary: "x", CheckedAt: "2026-10-01T12:00:00Z"})
	select {
	case <-c.done:
	case <-time.After(10 * time.Second):
		t.Fatal("the slow consumer stayed connected")
	}
	if got := c.closeStatus(); got != websocket.StatusPolicyViolation {
		t.Errorf("close status %d, want policy violation", got)
	}
}

func TestWSEndsWithoutLeftoverGoroutines(t *testing.T) {
	e := newWSEnv(t, WSOptions{})
	var cs []*wsClient
	for i := 0; i < 4; i++ {
		c := e.dial()
		c.subscribe(nil, "checks", "sessions", "settings")
		cs = append(cs, c)
	}
	_ = cs[0].conn.Close(websocket.StatusNormalClosure, "")
	_ = cs[1].conn.CloseNow()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	// Shutdown waits for every connection handler: it returns only when none is left
	if err := e.srv.Shutdown(ctx); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	if n := e.srv.hub.clientCount(); n != 0 {
		t.Errorf("%d clients left", n)
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		t.Error("shutdown ran into the deadline")
	}
}
