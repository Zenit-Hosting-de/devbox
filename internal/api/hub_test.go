package api

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Zenit-Hosting-de/devbox/internal/history"
)

const hubSession = "s_01J0000000000000000000"

// decoded is a message a fake client received.
type decoded struct {
	Type    string
	Session string // the ID, also for a "session" message
	Seq     int
	Data    string
	FromSeq int
	First   int
	Keys    []string
	ID      string
	Sess    json.RawMessage // the summary of a "session" message
}

// drain returns what the client has queued so far.
func drain(t *testing.T, c *client) []decoded {
	t.Helper()
	var out []decoded
	for _, m := range c.take() {
		var raw struct {
			Type     string          `json:"type"`
			Session  json.RawMessage `json:"session"`
			Seq      int             `json:"seq"`
			Data     string          `json:"data"`
			FromSeq  int             `json:"fromSeq"`
			FirstSeq int             `json:"firstSeq"`
			Keys     []string        `json:"keys"`
			ID       string          `json:"id"`
		}
		if err := json.Unmarshal(m, &raw); err != nil {
			t.Fatalf("not JSON: %v: %s", err, m)
		}
		d := decoded{Type: raw.Type, Seq: raw.Seq, Data: raw.Data, FromSeq: raw.FromSeq, First: raw.FirstSeq, Keys: raw.Keys, ID: raw.ID}
		if raw.Type == "session" {
			var s Session
			if err := json.Unmarshal(raw.Session, &s); err != nil {
				t.Fatalf("session: %v", err)
			}
			d.Sess, d.Session = raw.Session, s.ID
		} else if len(raw.Session) > 0 {
			if err := json.Unmarshal(raw.Session, &d.Session); err != nil {
				t.Fatalf("session: %v", err)
			}
		}
		out = append(out, d)
	}
	return out
}

func testClient(limit int) (*client, *atomic.Int32) {
	slow := &atomic.Int32{}
	return newClient(limit, func() { slow.Add(1) }), slow
}

func output(h *Hub, seq int, text string) { h.SessionOutput(hubSession, seq, []byte(text)) }

func seqsOf(ms []decoded) []int {
	var out []int
	for _, m := range ms {
		if m.Type == "output" || m.Type == "event" {
			out = append(out, m.Seq)
		}
	}
	return out
}

func TestHubReplayFromSeq(t *testing.T) {
	h := newHub(hubOptions{})
	h.SessionChanged(Session{ID: hubSession, Action: "demo.install", State: "running"})
	for i := 0; i < 6; i++ {
		output(h, i, fmt.Sprintf("line %d\n", i))
	}
	tests := []struct {
		name string
		from *int
		want []int
	}{
		{"everything buffered by default", nil, []int{0, 1, 2, 3, 4, 5}},
		{"from the middle", ptr(3), []int{3, 4, 5}},
		{"after the last", ptr(6), nil},
		{"from zero", ptr(0), []int{0, 1, 2, 3, 4, 5}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := testClient(8)
			if !h.subscribe(c, topicSession+hubSession, tt.from) {
				t.Fatal("session unknown")
			}
			got := drain(t, c)
			if len(got) == 0 || got[0].Type != "session" {
				t.Fatalf("the summary must come first: %+v", got)
			}
			if s := seqsOf(got); fmt.Sprint(s) != fmt.Sprint(tt.want) {
				t.Errorf("seqs = %v, want %v", s, tt.want)
			}
			for _, m := range got {
				if m.Type == "gap" {
					t.Errorf("unexpected gap: %+v", m)
				}
			}
		})
	}
}

func ptr(n int) *int { return &n }

func TestHubUnknownSession(t *testing.T) {
	h := newHub(hubOptions{})
	c, _ := testClient(8)
	if h.subscribe(c, topicSession+hubSession, nil) {
		t.Error("an unknown session was subscribed")
	}
	if h.Known(hubSession) {
		t.Error("an unknown session is known")
	}
}

func TestHubEvictionAndGap(t *testing.T) {
	// every chunk has 10 bytes of payload, the buffer holds 30
	h := newHub(hubOptions{bufferBytes: 30})
	h.SessionChanged(Session{ID: hubSession, Action: "demo.install", State: "running"})
	for i := 0; i < 10; i++ {
		output(h, i, "0123456789")
	}
	b := h.sessions[hubSession]
	if b.bytes != 30 || len(b.entries) != 3 || b.firstSeq() != 7 {
		t.Fatalf("buffer = %d bytes, %d entries, first %d; want 30, 3, 7", b.bytes, len(b.entries), b.firstSeq())
	}

	c, _ := testClient(8)
	h.subscribe(c, topicSession+hubSession, ptr(2))
	got := drain(t, c)
	var gap *decoded
	for i := range got {
		if got[i].Type == "gap" {
			gap = &got[i]
		}
	}
	if gap == nil || gap.FromSeq != 2 || gap.First != 7 || gap.Session != hubSession {
		t.Fatalf("gap = %+v in %+v", gap, got)
	}
	if s := seqsOf(got); fmt.Sprint(s) != "[7 8 9]" {
		t.Errorf("seqs = %v, want [7 8 9]", s)
	}
	// the gap comes before the first replayed message
	for i, m := range got {
		if m.Type == "output" && got[1].Type != "gap" {
			t.Errorf("message %d: the gap must precede the output: %+v", i, got)
		}
	}

	// no gap when fromSeq is inside the buffer, or exactly at its start
	for _, from := range []int{7, 8} {
		c2, _ := testClient(8)
		h.subscribe(c2, topicSession+hubSession, ptr(from))
		for _, m := range drain(t, c2) {
			if m.Type == "gap" {
				t.Errorf("fromSeq %d: unexpected gap", from)
			}
		}
	}

	// a single message larger than the buffer is not kept, the buffer ends up empty
	h.SessionOutput(hubSession, 10, make([]byte, 100))
	if b.firstSeq() != 11 || len(b.entries) != 0 {
		t.Errorf("after an oversized chunk: first %d, %d entries", b.firstSeq(), len(b.entries))
	}
}

func TestHubLiveMessagesFollowReplayWithoutGapOrDuplicate(t *testing.T) {
	h := newHub(hubOptions{})
	h.SessionChanged(Session{ID: hubSession, Action: "demo.install", State: "running"})
	const total = 4000
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < total; i++ {
			output(h, i, "x")
		}
	}()

	// subscribers join while output is being produced
	type sub struct {
		c    *client
		got  []int
		from int
	}
	var subs []*sub
	for i := 0; i < 8; i++ {
		c, _ := testClient(1 << 20)
		from := 0
		if i%2 == 1 {
			from = i * 10
		}
		h.subscribe(c, topicSession+hubSession, ptr(from))
		subs = append(subs, &sub{c: c, from: from})
		time.Sleep(time.Millisecond)
	}
	wg.Wait()
	for _, s := range subs {
		s.got = seqsOf(drain(t, s.c))
		if len(s.got) != total-s.from {
			t.Fatalf("subscriber from %d got %d messages, want %d", s.from, len(s.got), total-s.from)
		}
		for i, seq := range s.got {
			if seq != s.from+i {
				t.Fatalf("subscriber from %d: message %d has seq %d, want %d", s.from, i, seq, s.from+i)
			}
		}
	}
}

func TestHubFinishedSessionsAreKeptForTenMinutesAtMost32(t *testing.T) {
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	h := newHub(hubOptions{now: clock, maxFinished: 3})
	id := func(n int) string { return fmt.Sprintf("s_01J000000000000000000%d", n) }
	for n := 0; n < 5; n++ {
		h.SessionChanged(Session{ID: id(n), Action: "demo.install", State: "running"})
		h.SessionOutput(id(n), 0, []byte("x"))
		now = now.Add(time.Second)
		h.SessionChanged(Session{ID: id(n), Action: "demo.install", State: "success"})
	}
	h.SessionChanged(Session{ID: id(9), Action: "demo.install", State: "running"})
	for n := 0; n < 5; n++ {
		if want := n >= 2; h.Known(id(n)) != want {
			t.Errorf("session %d known = %v, want %v (the 3 newest finished ones stay)", n, !want, want)
		}
	}
	if !h.Known(id(9)) {
		t.Error("a running session must stay")
	}
	base := now // id(4) finished now, id(3) a second earlier
	now = base.Add(10*time.Minute - time.Second)
	if !h.Known(id(4)) {
		t.Error("the newest session is still within ten minutes")
	}
	now = base.Add(10*time.Minute + time.Second)
	if h.Known(id(4)) || h.Known(id(3)) {
		t.Error("sessions finished more than ten minutes ago must be dropped")
	}
	if !h.Known(id(9)) {
		t.Error("a running session never expires")
	}
}

func TestHubDroppedBufferLeavesNoSubscription(t *testing.T) {
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	h := newHub(hubOptions{now: func() time.Time { return now }})
	h.SessionChanged(Session{ID: hubSession, Action: "demo.install", State: "success"})
	c, _ := testClient(8)
	h.subscribe(c, topicSession+hubSession, nil)
	now = now.Add(time.Hour)
	h.Prune()
	if len(c.topics) != 0 || len(h.sessions) != 0 {
		t.Errorf("topics %v, sessions %d", c.topics, len(h.sessions))
	}
}

func TestHubTopics(t *testing.T) {
	h := newHub(hubOptions{})
	checks, _ := testClient(8)
	sessions, _ := testClient(8)
	settings, _ := testClient(8)
	both, _ := testClient(8)
	h.subscribe(checks, topicChecks, nil)
	h.subscribe(sessions, topicSessions, nil)
	h.subscribe(settings, topicSettings, nil)
	h.SessionChanged(Session{ID: hubSession, Action: "demo.install", State: "queued"})
	h.subscribe(both, topicSessions, nil)
	h.subscribe(both, topicSession+hubSession, nil)
	drain(t, both) // the summary of the subscription

	h.CheckChanged(CheckSummary{ID: "demo.state", Status: "ok", Summary: "fine", CheckedAt: "2026-10-01T12:00:00Z"})
	h.SettingsChanged([]string{"demo.mode"})
	h.SessionChanged(Session{ID: hubSession, Action: "demo.install", State: "running"})
	output(h, 0, "hello")

	if got := drain(t, checks); len(got) != 1 || got[0].Type != "check" || got[0].ID != "demo.state" {
		t.Errorf("checks = %+v", got)
	}
	if got := drain(t, settings); len(got) != 1 || got[0].Type != "settings" || fmt.Sprint(got[0].Keys) != "[demo.mode]" {
		t.Errorf("settings = %+v", got)
	}
	got := drain(t, sessions) // queued and running
	if len(got) != 2 || got[0].Type != "session" || got[1].Type != "session" {
		t.Errorf("the sessions topic gets summaries only: %+v", got)
	}
	got = drain(t, both)
	if len(got) != 2 || got[0].Type != "session" || got[1].Type != "output" {
		t.Errorf("a client of both topics gets each summary once: %+v", got)
	}

	h.unsubscribe(both, []string{topicSession + hubSession, topicSessions})
	output(h, 1, "again")
	h.SessionChanged(Session{ID: hubSession, Action: "demo.install", State: "success"})
	if got := drain(t, both); len(got) != 0 {
		t.Errorf("after unsubscribe: %+v", got)
	}
}

func TestHubProgressIsThrottled(t *testing.T) {
	h := newHub(hubOptions{progressInterval: 100 * time.Millisecond})
	t.Cleanup(h.close)
	c, _ := testClient(1000)
	h.subscribe(c, topicSessions, nil)
	h.SessionChanged(Session{ID: hubSession, Action: "demo.install", State: "running"})
	percent := func(n int) Session {
		return Session{ID: hubSession, Action: "demo.install", State: "running", Progress: &Progress{Percent: &n}}
	}
	for n := 0; n <= 100; n++ {
		h.SessionChanged(percent(n))
	}
	if got := drain(t, c); len(got) != 1 {
		t.Fatalf("a burst must reach the client once, got %d messages", len(got))
	}
	time.Sleep(300 * time.Millisecond)
	got := drain(t, c)
	if len(got) != 1 {
		t.Fatalf("the newest progress must follow once, got %d messages", len(got))
	}
	var s Session
	if err := json.Unmarshal(got[0].Sess, &s); err != nil || s.Progress == nil || *s.Progress.Percent != 100 {
		t.Errorf("last summary = %+v (%v), want progress 100", s, err)
	}

	// a change of state is never held back, and ends a pending summary
	h.SessionChanged(percent(1))
	h.SessionChanged(percent(2))
	drain(t, c)
	h.SessionChanged(Session{ID: hubSession, Action: "demo.install", State: "success"})
	got = drain(t, c)
	if len(got) != 1 {
		t.Fatalf("the final state must be sent at once: %d messages", len(got))
	}
	time.Sleep(200 * time.Millisecond)
	if got := drain(t, c); len(got) != 0 {
		t.Errorf("no stale progress after the final state: %+v", got)
	}
}

func TestHubSlowConsumer(t *testing.T) {
	h := newHub(hubOptions{})
	h.SessionChanged(Session{ID: hubSession, Action: "demo.install", State: "running"})
	for i := 0; i < 50; i++ {
		output(h, i, "x")
	}
	c, slow := testClient(5)
	// a replay larger than the limit is fine
	h.subscribe(c, topicSession+hubSession, nil)
	if slow.Load() != 0 {
		t.Fatal("the replay counts as a slow consumer")
	}
	for i := 50; i < 56; i++ {
		output(h, i, "x")
	}
	if slow.Load() != 1 {
		t.Fatalf("slow handler called %d times, want once", slow.Load())
	}
	output(h, 56, "x")
	if slow.Load() != 1 {
		t.Error("the slow handler must run once")
	}
	if got := seqsOf(drain(t, c)); len(got) != 55 {
		t.Errorf("queued %d messages, want the replay and 5 live ones", len(got))
	}
	// the writer drained the queue: live messages count from zero again, but a client
	// that was marked slow gets nothing more
	output(h, 57, "x")
	if got := drain(t, c); len(got) != 0 {
		t.Errorf("a slow consumer gets nothing more: %+v", got)
	}
}

func TestHubDoesNotBlockWithoutWriter(t *testing.T) {
	h := newHub(hubOptions{})
	c, _ := testClient(4)
	h.attach(c)
	h.subscribe(c, topicChecks, nil)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 10000; i++ {
			h.CheckChanged(CheckSummary{ID: "demo.state"})
		}
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the hub blocked on a client that does not read")
	}
}

func TestHubCloseStopsBroadcasting(t *testing.T) {
	h := newHub(hubOptions{})
	c, _ := testClient(8)
	h.subscribe(c, topicChecks, nil)
	h.close()
	h.CheckChanged(CheckSummary{ID: "demo.state"})
	if got := drain(t, c); len(got) != 0 {
		t.Errorf("after close: %+v", got)
	}
	if h.attach(c) {
		t.Error("a closed hub accepted a client")
	}
}

func TestHubEvents(t *testing.T) {
	h := newHub(hubOptions{})
	h.SessionChanged(Session{ID: hubSession, Action: "demo.install", State: "running"})
	output(h, 0, "a")
	h.SessionEvent(hubSession, history.Event{Seq: 1, Time: "2026-10-01T12:00:00Z", Event: map[string]any{"type": "step", "id": "x"}})
	output(h, 2, "b")
	c, _ := testClient(8)
	h.subscribe(c, topicSession+hubSession, ptr(1))
	got := drain(t, c)
	var types []string
	for _, m := range got {
		types = append(types, m.Type)
	}
	if fmt.Sprint(types) != "[session event output]" {
		t.Errorf("types = %v, want the summary, then event and output in seq order", types)
	}
	raw := base64.StdEncoding.EncodeToString([]byte("b"))
	if got[2].Data != raw {
		t.Errorf("data = %q, want %q", got[2].Data, raw)
	}
}
