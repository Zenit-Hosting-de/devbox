package history

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Zenit-Hosting-de/devbox/internal/schema"
	schemafs "github.com/Zenit-Hosting-de/devbox/schema"
)

var base = time.Date(2026, 10, 27, 9, 0, 0, 0, time.UTC)

func newStore(t *testing.T) *Store {
	t.Helper()
	v, err := schema.New()
	if err != nil {
		t.Fatal(err)
	}
	return New(t.TempDir(), v)
}

// id returns a valid, time-ordered session ID for n.
func id(n int) string { return fmt.Sprintf("s_01JB8Y%04d", n) }

func entry(n int, ended time.Time) Entry {
	code := 0
	return Entry{
		ID: id(n), Action: "java.update", Plugin: "java", Title: "Update", User: "dev",
		Origin: OriginCLI, StartedAt: Timestamp(ended.Add(-time.Minute)), EndedAt: Timestamp(ended),
		State: "success", ExitCode: &code, Changes: 2, Summary: "done",
	}
}

func TestSessionWritesOneHistoryLine(t *testing.T) {
	s := newStore(t)
	ss, err := s.Create(id(1))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ss.Write([]byte("hello\n")); err != nil {
		t.Fatal(err)
	}
	ev := ss.Event(map[string]any{"type": "change", "kind": "file", "op": "update", "target": "~/.bashrc", "summary": "PATH"}, base)
	if ev.Seq != 1 {
		t.Errorf("event seq = %d, want 1 (output took 0)", ev.Seq)
	}
	if err := ss.Close(); err != nil {
		t.Fatal(err)
	}
	if err := s.Append(entry(1, base)); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(s.historyDir(), "2026-10.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if n := bytes.Count(data, []byte("\n")); n != 1 {
		t.Fatalf("history has %d lines, want 1:\n%s", n, data)
	}
	want := `{"id":"s_01JB8Y0001","action":"java.update","plugin":"java","title":"Update","user":"dev","origin":"cli","dryRun":false,"startedAt":"2026-10-27T08:59:00Z","endedAt":"2026-10-27T09:00:00Z","state":"success","exitCode":0,"changes":2,"summary":"done"}` + "\n"
	if string(data) != want {
		t.Errorf("history line\n got %s\nwant %s", data, want)
	}
	var buf bytes.Buffer
	if err := s.ReadLog(id(1), &buf); err != nil || buf.String() != "hello\n" {
		t.Errorf("log = %q, %v", buf.String(), err)
	}
	events, err := os.ReadFile(s.EventsPath(id(1)))
	if err != nil {
		t.Fatal(err)
	}
	v, _ := schema.New()
	if err := v.ValidateJSON("event", bytes.TrimSpace(events)); err != nil {
		t.Errorf("events line: %v", err)
	}
	for _, p := range []string{s.LogPath(id(1)), s.EventsPath(id(1)), filepath.Join(s.historyDir(), "2026-10.jsonl")} {
		if fi, err := os.Stat(p); err != nil || fi.Mode().Perm() != 0o600 {
			t.Errorf("%s: mode %v, %v", p, fi.Mode().Perm(), err)
		}
	}
}

func TestCreateRejectsDuplicatesAndBadIDs(t *testing.T) {
	s := newStore(t)
	ss, err := s.Create(id(1))
	if err != nil {
		t.Fatal(err)
	}
	_ = ss.Close()
	for _, bad := range []string{id(1), "s_lower", "../x", ""} {
		if _, err := s.Create(bad); err == nil {
			t.Errorf("Create(%q) succeeded", bad)
		}
	}
}

func TestAppendValidates(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Entry)
		want   string
	}{
		{"bad state", func(e *Entry) { e.State = "done" }, "/state"},
		{"bad origin", func(e *Entry) { e.Origin = "web" }, "/origin"},
		{"empty title", func(e *Entry) { e.Title = "" }, "/title"},
		{"bad action", func(e *Entry) { e.Action = "java" }, "/action"},
		{"negative changes", func(e *Entry) { e.Changes = -1 }, "/changes"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := newStore(t)
			e := entry(1, base)
			tc.mutate(&e)
			err := s.Append(e)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("Append() = %v, want error at %s", err, tc.want)
			}
		})
	}
}

func TestNullExitCode(t *testing.T) {
	s := newStore(t)
	e := entry(1, base)
	e.ExitCode, e.State = nil, "stopped"
	if err := s.Append(e); err != nil {
		t.Fatal(err)
	}
	got, ok, err := s.Get(id(1))
	if err != nil || !ok || got.ExitCode != nil {
		t.Errorf("Get() = %+v, %v, %v", got, ok, err)
	}
}

func TestListNewestFirstAcrossMonths(t *testing.T) {
	s := newStore(t)
	for i, ended := range []time.Time{base.AddDate(0, -1, 0), base, base.Add(time.Hour)} {
		if err := s.Append(entry(i+1, ended)); err != nil {
			t.Fatal(err)
		}
	}
	all, problems, err := s.List(0)
	if err != nil || len(problems) > 0 {
		t.Fatal(err, problems)
	}
	if got := ids(all); got != "s_01JB8Y0003 s_01JB8Y0002 s_01JB8Y0001" {
		t.Errorf("List(0) = %s", got)
	}
	two, _, _ := s.List(2)
	if got := ids(two); got != "s_01JB8Y0003 s_01JB8Y0002" {
		t.Errorf("List(2) = %s", got)
	}
	if _, err := os.Stat(filepath.Join(s.historyDir(), "2026-09.jsonl")); err != nil {
		t.Errorf("September file: %v", err)
	}
}

func ids(es []Entry) string {
	out := make([]string, len(es))
	for i, e := range es {
		out[i] = e.ID
	}
	return strings.Join(out, " ")
}

func TestPrune(t *testing.T) {
	tests := []struct {
		name         string
		ages         []int // days before now, per session 1..n
		keepDays     int
		keepSessions int
		removed      string
	}{
		{"nothing to do", []int{0, 1, 2}, 90, 500, ""},
		{"older than keepDays", []int{100, 91, 89, 0}, 90, 500, "s_01JB8Y0001 s_01JB8Y0002"},
		{"exactly keepDays is kept", []int{90}, 90, 500, ""},
		{"beyond keepSessions", []int{14, 13, 12, 11, 10, 9, 8, 7, 6, 5, 4, 3}, 90, 10, "s_01JB8Y0001 s_01JB8Y0002"},
		{"both limits", []int{200, 13, 12, 11, 10, 9, 8, 7, 6, 5, 4, 3}, 90, 10, "s_01JB8Y0001 s_01JB8Y0002"},
		{"defaults for zero", []int{91, 1}, 0, 0, "s_01JB8Y0001"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := newStore(t)
			now := base
			for i, age := range tc.ages {
				n := i + 1
				ss, err := s.Create(id(n))
				if err != nil {
					t.Fatal(err)
				}
				_ = ss.Close()
				backup := filepath.Join(s.BackupDir(id(n)), "home", "dev", ".npmrc")
				if err := os.MkdirAll(filepath.Dir(backup), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(backup, []byte("x"), 0o600); err != nil {
					t.Fatal(err)
				}
				if err := s.Append(entry(n, now.AddDate(0, 0, -age))); err != nil {
					t.Fatal(err)
				}
			}
			removed, err := s.Prune(now, tc.keepDays, tc.keepSessions)
			if err != nil {
				t.Fatal(err)
			}
			if got := strings.Join(removed, " "); got != tc.removed {
				t.Errorf("removed %q, want %q", got, tc.removed)
			}
			left, _, _ := s.List(0)
			if len(left)+len(removed) != len(tc.ages) {
				t.Errorf("%d left + %d removed != %d", len(left), len(removed), len(tc.ages))
			}
			for _, rid := range removed {
				for _, p := range []string{s.LogPath(rid), s.EventsPath(rid), s.BackupDir(rid)} {
					if _, err := os.Stat(p); !os.IsNotExist(err) {
						t.Errorf("%s still exists", p)
					}
				}
			}
			for _, e := range left {
				if b, _ := s.Backups(e.ID); len(b) != 1 || b[0] != "/home/dev/.npmrc" {
					t.Errorf("backups of %s = %v", e.ID, b)
				}
			}
		})
	}
}

func TestPruneKeepsUnreadableLinesAndRemovesEmptyFiles(t *testing.T) {
	s := newStore(t)
	if err := s.Append(entry(1, base.AddDate(0, -6, 0))); err != nil {
		t.Fatal(err)
	}
	if err := s.Append(entry(2, base)); err != nil {
		t.Fatal(err)
	}
	october := filepath.Join(s.historyDir(), "2026-10.jsonl")
	f, err := os.OpenFile(october, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.WriteString("not json\n")
	_ = f.Close()

	_, problems, err := s.List(0)
	if err != nil || len(problems) != 1 || !strings.Contains(problems[0], "2026-10.jsonl:2") {
		t.Fatalf("List problems = %v, %v", problems, err)
	}
	removed, err := s.Prune(base, 90, 500)
	if err != nil || strings.Join(removed, " ") != id(1) {
		t.Fatalf("Prune() = %v, %v", removed, err)
	}
	if _, err := os.Stat(filepath.Join(s.historyDir(), "2026-04.jsonl")); !os.IsNotExist(err) {
		t.Errorf("empty April file kept: %v", err)
	}
	data, _ := os.ReadFile(october)
	if !strings.HasSuffix(string(data), "not json\n") {
		t.Errorf("unreadable line lost:\n%s", data)
	}
}

func TestDefaultsMatchSchema(t *testing.T) {
	data, err := schemafs.FS.ReadFile("config.v1.json")
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Properties struct {
			History struct {
				Properties map[string]struct {
					Default int `json:"default"`
				} `json:"properties"`
			} `json:"history"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	p := doc.Properties.History.Properties
	if p["keepDays"].Default != DefaultKeepDays || p["keepSessions"].Default != DefaultKeepSessions {
		t.Errorf("schema defaults %d/%d, constants %d/%d", p["keepDays"].Default, p["keepSessions"].Default, DefaultKeepDays, DefaultKeepSessions)
	}
}

func TestWriteSeqReturnsTheSequenceNumberOfTheChunk(t *testing.T) {
	s := newStore(t)
	sess, err := s.Create(id(1))
	if err != nil {
		t.Fatal(err)
	}
	if got := sess.WriteSeq([]byte("a")); got != 0 {
		t.Errorf("first chunk took seq %d, want 0", got)
	}
	if got := sess.WriteSeq(nil); got != -1 {
		t.Errorf("an empty chunk takes no seq, got %d", got)
	}
	if ev := sess.Event(map[string]any{"type": "log"}, base); ev.Seq != 1 {
		t.Errorf("event seq %d, want 1", ev.Seq)
	}
	if n, err := sess.Write([]byte("bc")); n != 2 || err != nil {
		t.Errorf("Write = %d, %v", n, err)
	}
	if got := sess.WriteSeq([]byte("d")); got != 3 {
		t.Errorf("seq %d, want 3: Write and WriteSeq share the counter", got)
	}
	if err := sess.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(s.LogPath(id(1)))
	if err != nil || string(data) != "abcd" {
		t.Errorf("log = %q, %v", data, err)
	}
}
