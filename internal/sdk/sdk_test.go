package sdk

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/Zenit-Hosting-de/devbox/internal/schema"
)

type testEnv struct {
	Env
	vars map[string]string
	msgs *bytes.Buffer
}

func newEnv(t *testing.T, dryRun bool) testEnv {
	t.Helper()
	v, err := schema.New()
	if err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	vars := map[string]string{"HOME": home, "DEVBOX_BACKUP_DIR": filepath.Join(home, ".backup"), "DEVBOX_DRY_RUN": "0"}
	if dryRun {
		vars["DEVBOX_DRY_RUN"] = "1"
	}
	var msgs bytes.Buffer
	return testEnv{Env: Env{Getenv: func(k string) string { return vars[k] }, Messages: &msgs, Validator: v}, vars: vars, msgs: &msgs}
}

func (e testEnv) changes() int { return strings.Count(e.msgs.String(), `"type":"change"`) }

func TestBlockIsIdempotent(t *testing.T) {
	e := newEnv(t, false)
	path := filepath.Join(e.vars["HOME"], ".npmrc")
	if err := os.WriteFile(path, []byte("save-exact=true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	body := []byte("registry=https://nexus.example.com/repository/npm-group/\n")
	for i := 0; i < 2; i++ {
		if err := e.Block("~/.npmrc", "npm", body, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	got, _ := os.ReadFile(path)
	want := "save-exact=true\n\n# >>> devbox:npm >>>\nregistry=https://nexus.example.com/repository/npm-group/\n# <<< devbox:npm <<<\n"
	if string(got) != want {
		t.Errorf("file =\n%s\nwant\n%s", got, want)
	}
	if e.changes() != 1 {
		t.Errorf("%d change messages, want 1 (second run changes nothing):\n%s", e.changes(), e.msgs)
	}
	backup, err := os.ReadFile(filepath.Join(e.vars["DEVBOX_BACKUP_DIR"], path))
	if err != nil || string(backup) != "save-exact=true\n" {
		t.Errorf("backup = %q, %v", backup, err)
	}

	// Replacing the body keeps the position; removing restores the original.
	if err := e.Block("~/.npmrc", "npm", []byte("registry=https://other.example.com/\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := e.BlockRemove("~/.npmrc", "npm"); err != nil {
		t.Fatal(err)
	}
	got, _ = os.ReadFile(path)
	if string(got) != "save-exact=true\n" {
		t.Errorf("after remove: %q", got)
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0o600 {
		t.Errorf("mode %v, want the original 0600", info.Mode().Perm())
	}
}

func TestBlockXMLComments(t *testing.T) {
	got := string(SetBlock(nil, "maven", []byte("<server/>"), CommentFor("settings.xml")))
	want := "<!-- >>> devbox:maven >>> -->\n<server/>\n<!-- <<< devbox:maven <<< -->\n"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestDryRunChangesNothing is the AP-08 criterion: in a dry run every file stays
// unchanged and only change messages are written.
func TestDryRunChangesNothing(t *testing.T) {
	e := newEnv(t, true)
	home := e.vars["HOME"]
	existing := filepath.Join(home, "settings.json")
	if err := os.WriteFile(existing, []byte("{\n    \"a\": 1\n}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := e.Block("~/.npmrc", "npm", []byte("x=1"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := e.JSONSet(existing, "/a", json.Number("2")); err != nil {
		t.Fatal(err)
	}
	if err := e.Download(context.Background(), http.DefaultClient, "https://downloads.example.com/x.tar.gz", "~/x.tar.gz", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(home, ".npmrc")); !os.IsNotExist(err) {
		t.Error(".npmrc was created in a dry run")
	}
	if data, _ := os.ReadFile(existing); string(data) != "{\n    \"a\": 1\n}\n" {
		t.Errorf("settings.json changed in a dry run: %q", data)
	}
	if _, err := os.Stat(e.vars["DEVBOX_BACKUP_DIR"]); !os.IsNotExist(err) {
		t.Error("backups written in a dry run")
	}
	if e.changes() != 3 {
		t.Errorf("%d change messages, want 3:\n%s", e.changes(), e.msgs)
	}
}

// TestJSONSetKeepsFormatting is the AP-08 criterion for json-set.
func TestJSONSetKeepsFormatting(t *testing.T) {
	e := newEnv(t, false)
	path := filepath.Join(e.vars["HOME"], "settings.json")
	src := "{\n    \"editor.tabSize\": 2,\n    \"files.exclude\": { \"**/.git\": true }\n}\n"
	if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := e.JSONSet(path, "/editor.tabSize", json.Number("4")); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	if want := strings.Replace(src, "2", "4", 1); string(got) != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
	if err := e.JSONSet(path, "/editor.tabSize", json.Number("4")); err != nil {
		t.Fatal(err)
	}
	if e.changes() != 1 {
		t.Errorf("setting the same value again must not change the file")
	}
	if err := e.JSONSet(filepath.Join(e.vars["HOME"], "new.json"), "/a/b", "x"); err != nil {
		t.Fatal(err)
	}
	created, _ := os.ReadFile(filepath.Join(e.vars["HOME"], "new.json"))
	if string(created) != "{\n  \"a\": {\n    \"b\": \"x\"\n  }\n}\n" {
		t.Errorf("new file = %q", created)
	}
}

func TestDownload(t *testing.T) {
	payload := bytes.Repeat([]byte("devbox"), 10000)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
		_, _ = w.Write(payload)
	}))
	defer srv.Close()
	sum := sha256.Sum256(payload)
	e := newEnv(t, false)
	if err := e.Download(context.Background(), srv.Client(), srv.URL, "~/dl/file.bin", hex.EncodeToString(sum[:])); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(filepath.Join(e.vars["HOME"], "dl", "file.bin"))
	if !bytes.Equal(got, payload) {
		t.Error("downloaded content differs")
	}
	if !strings.Contains(e.msgs.String(), `"percent":100`) {
		t.Errorf("no progress to 100%%:\n%s", e.msgs)
	}
	err := e.Download(context.Background(), srv.Client(), srv.URL, "~/dl/bad.bin", strings.Repeat("0", 64))
	if err == nil || !strings.Contains(err.Error(), "sha256") {
		t.Errorf("checksum mismatch: err = %v", err)
	}
	if _, err := os.Stat(filepath.Join(e.vars["HOME"], "dl", "bad.bin")); !os.IsNotExist(err) {
		t.Error("a file with a wrong checksum was kept")
	}
}

func TestEmitArgs(t *testing.T) {
	v, err := schema.New()
	if err != nil {
		t.Fatal(err)
	}
	valid := [][]string{
		{"step", "download", "running", "Download"},
		{"progress", "45", "87 of 192 MB"},
		{"progress", "null"},
		{"change", "file", "create", "~/x", "created"},
		{"log", "warn", "careful"},
		{"setting", "installDir", `"/opt/x"`},
		{"notice", "restart-shell", "Open a new terminal."},
		{"result", "done", `{"version":"21.0.5"}`},
	}
	for _, args := range valid {
		m, err := EmitArgs(args)
		if err != nil {
			t.Errorf("%v: %v", args, err)
			continue
		}
		if err := v.Validate("event.message", m); err != nil {
			t.Errorf("%v: %v", args, err)
		}
	}
	for _, args := range [][]string{{}, {"nope"}, {"step", "x"}, {"progress", "lots"}} {
		if _, err := EmitArgs(args); err == nil {
			t.Errorf("%v: want an error", args)
		}
	}
}

func TestJSONGet(t *testing.T) {
	data := []byte(`[{"binary": {"package": {"link": "https://downloads.example.com/a.tgz"}}, "version": {"major": 21, "security": 5}}, {"version": {"major": 17}}]`)
	tests := map[string][]any{
		"/0/binary/package/link": {"https://downloads.example.com/a.tgz"},
		"/*/version/major":       {json.Number("21"), json.Number("17")},
		"/1/binary":              {},
		"/0/version":             {map[string]any{"major": json.Number("21"), "security": json.Number("5")}},
	}
	for ptr, want := range tests {
		got, err := JSONGet(data, ptr)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != len(want) {
			t.Errorf("%s: got %v, want %v", ptr, got, want)
			continue
		}
		for i := range want {
			if Format(got[i]) != Format(want[i]) {
				t.Errorf("%s[%d]: got %v, want %v", ptr, i, got[i], want[i])
			}
		}
	}
}

// TestBlockBeforeInsertsInsideXML covers the insertion point of a new block: inside
// an XML section, idempotent, replaced in place, and removed without a trace.
func TestBlockBeforeInsertsInsideXML(t *testing.T) {
	e := newEnv(t, false)
	path := filepath.Join(e.vars["HOME"], "settings.xml")
	orig := "<settings>\n  <servers>\n    <server><id>own</id></server>\n  </servers>\n</settings>\n"
	if err := os.WriteFile(path, []byte(orig), 0o600); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := e.BlockBefore(path, "maven:a", []byte("    <server><id>a</id></server>"), 0o600, "</servers>"); err != nil {
			t.Fatal(err)
		}
	}
	got, _ := os.ReadFile(path)
	want := "<settings>\n  <servers>\n    <server><id>own</id></server>\n" +
		"    <!-- >>> devbox:maven:a >>> -->\n    <server><id>a</id></server>\n    <!-- <<< devbox:maven:a <<< -->\n" +
		"  </servers>\n</settings>\n"
	if string(got) != want {
		t.Errorf("file =\n%s\nwant\n%s", got, want)
	}
	if e.changes() != 1 {
		t.Errorf("%d change messages, want 1", e.changes())
	}
	// A changed body stays at its place.
	if err := e.BlockBefore(path, "maven:a", []byte("    <server><id>b</id></server>"), 0o600, "</servers>"); err != nil {
		t.Fatal(err)
	}
	got, _ = os.ReadFile(path)
	if !strings.Contains(string(got), "<id>b</id>") || strings.Contains(string(got), "<id>a</id>") {
		t.Errorf("body not replaced:\n%s", got)
	}
	if err := e.BlockRemove(path, "maven:a"); err != nil {
		t.Fatal(err)
	}
	got, _ = os.ReadFile(path)
	if string(got) != orig {
		t.Errorf("after remove:\n%s\nwant\n%s", got, orig)
	}
	// Without the anchor line nothing is written.
	if err := e.BlockBefore(path, "maven:c", []byte("x"), 0o600, "</mirrors>"); err == nil {
		t.Error("missing anchor line: want an error")
	}
	got, _ = os.ReadFile(path)
	if string(got) != orig {
		t.Errorf("file changed despite the error:\n%s", got)
	}
}

func TestSetBlockBeforeKeepsPlainBehaviour(t *testing.T) {
	c := CommentFor("x.conf")
	in := []byte("a=1\n")
	plain := SetBlock(in, "demo", []byte("b=2"), c)
	same, err := SetBlockBefore(in, "demo", []byte("b=2"), c, "")
	if err != nil || string(plain) != string(same) {
		t.Errorf("plain = %q, %q, %v", plain, same, err)
	}
	if string(plain) != "a=1\n\n# >>> devbox:demo >>>\nb=2\n# <<< devbox:demo <<<\n" {
		t.Errorf("plain = %q", plain)
	}
}
