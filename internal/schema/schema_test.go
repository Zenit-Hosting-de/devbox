package schema

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// repoRoot is the repository root relative to this package.
const repoRoot = "../.."

var (
	validDir   = filepath.Join(repoRoot, "test", "fixtures", "valid")
	invalidDir = filepath.Join(repoRoot, "test", "fixtures", "invalid")
	conceptDir = filepath.Join(validDir, "concept")
)

// wantSchemas is the list of schemas required by AP-02 (docs/concept.md, "Dateien und Formate").
var wantSchemas = []string{
	"catalog", "check-result", "config", "credentials", "event", "history-entry", "lock",
	"messages", "plugin", "profile", "request", "template", "vault",
}

const minInvalidCases = 3

func newValidator(t *testing.T) *Validator {
	t.Helper()
	v, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return v
}

func TestEmbeddedSchemas(t *testing.T) {
	v := newValidator(t)
	if got := v.Names(); !reflect.DeepEqual(got, wantSchemas) {
		t.Fatalf("embedded schemas = %v, want %v", got, wantSchemas)
	}
	for _, name := range wantSchemas {
		if err := v.Compile(name); err != nil {
			t.Errorf("compile %s: %v", name, err)
		}
		id, _ := v.ID(name)
		if got, known := v.NameForURL(id); got != name || !known {
			t.Errorf("NameForURL(%q) = %q, %v", id, got, known)
		}
	}
	if _, known := v.NameForURL(strings.Replace(mustID(t, v, "config"), ".v1.", ".v9.", 1)); !known {
		t.Error("NameForURL does not recognise an unknown schema in the DevBox schema directory")
	}
}

func mustID(t *testing.T, v *Validator, name string) string {
	t.Helper()
	id, ok := v.ID(name)
	if !ok {
		t.Fatalf("no schema %q", name)
	}
	return id
}

// TestSharedDefsIdentical guards the rule that schemas are self-contained: a $def that
// appears in several schemas under the same name must be identical everywhere.
func TestSharedDefsIdentical(t *testing.T) {
	files, err := filepath.Glob(filepath.Join(repoRoot, "schema", "*.v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	type origin struct {
		file string
		def  any
	}
	seen := map[string]origin{}
	for _, file := range files {
		var doc struct {
			Defs map[string]any `json:"$defs"`
		}
		readJSON(t, file, &doc)
		for name, def := range doc.Defs {
			if first, ok := seen[name]; ok {
				if !reflect.DeepEqual(first.def, def) {
					t.Errorf("$defs/%s differs between %s and %s", name, filepath.Base(first.file), filepath.Base(file))
				}
				continue
			}
			seen[name] = origin{file: file, def: def}
		}
	}
}

func TestValidFixtures(t *testing.T) {
	v := newValidator(t)
	for _, fx := range collectFixtures(t, validDir) {
		t.Run(fx.rel, func(t *testing.T) {
			for _, doc := range fx.docs {
				if err := v.Validate(fx.target, doc.value); err != nil {
					t.Errorf("line %d: %v", doc.line, err)
				}
			}
		})
	}
}

type expectation struct {
	Pointer string `json:"pointer"`
	Keyword string `json:"keyword"`
}

func TestInvalidFixtures(t *testing.T) {
	v := newValidator(t)
	dirs, err := os.ReadDir(invalidDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, dir := range dirs {
		if !dir.IsDir() {
			continue
		}
		target := dir.Name()
		t.Run(target, func(t *testing.T) {
			base := filepath.Join(invalidDir, target)
			var expected map[string]expectation
			readJSONStrict(t, filepath.Join(base, "expected.json"), &expected)

			cases, err := filepath.Glob(filepath.Join(base, "*.json"))
			if err != nil {
				t.Fatal(err)
			}
			var names []string
			for _, c := range cases {
				if filepath.Base(c) != "expected.json" {
					names = append(names, filepath.Base(c))
				}
			}
			if len(names) < minInvalidCases {
				t.Errorf("%d invalid cases, want at least %d", len(names), minInvalidCases)
			}
			for _, name := range names {
				if _, ok := expected[name]; !ok {
					t.Errorf("%s has no entry in expected.json", name)
				}
			}
			for name, exp := range expected {
				t.Run(strings.TrimSuffix(name, ".json"), func(t *testing.T) {
					data, err := os.ReadFile(filepath.Join(base, name))
					if err != nil {
						t.Fatalf("expected.json names a missing case: %v", err)
					}
					err = v.ValidateJSON(target, data)
					var ve *ValidationError
					if !errors.As(err, &ve) {
						t.Fatalf("want a validation error, got %v", err)
					}
					if !ve.Has(exp.Pointer, exp.Keyword) {
						t.Errorf("want %q at %q, got:\n%v", exp.Keyword, displayPointer(exp.Pointer), describe(ve))
					}
				})
			}
		})
	}
}

func describe(ve *ValidationError) string {
	var b strings.Builder
	for _, vi := range ve.Violations {
		b.WriteString("  " + displayPointer(vi.Pointer) + " " + vi.Keyword + ": " + vi.Message + "\n")
	}
	return b.String()
}

// TestEverySchemaHasFixtures checks the AP-02 acceptance criterion: every schema has
// valid fixtures and at least three invalid ones (counted in TestInvalidFixtures).
func TestEverySchemaHasFixtures(t *testing.T) {
	for _, name := range wantSchemas {
		if n := countFiles(t, filepath.Join(validDir, name)); n == 0 {
			t.Errorf("no valid fixtures for %s", name)
		}
		if _, err := os.Stat(filepath.Join(invalidDir, name, "expected.json")); err != nil {
			t.Errorf("no invalid fixtures for %s: %v", name, err)
		}
	}
}

// TestConceptExamples checks that every JSON example in docs/*.md has a fixture under
// test/fixtures/valid/concept (and is therefore validated by TestValidFixtures). An
// example matches a fixture when both are equal, or when the example is an excerpt of
// the fixture: every value in the example is present in the fixture, and a string
// ending in "…" is an abbreviation of a longer fixture string.
func TestConceptExamples(t *testing.T) {
	var fixtures []document
	for _, fx := range collectFixtures(t, conceptDir) {
		fixtures = append(fixtures, fx.docs...)
	}
	docs, err := filepath.Glob(filepath.Join(repoRoot, "docs", "*.md"))
	if err != nil {
		t.Fatal(err)
	}
	metaSchemas := metaSchemaURLs(t)
	examples := 0
	for _, file := range docs {
		for _, ex := range jsonExamples(t, file) {
			if isSchemaDocument(ex.value, metaSchemas) {
				continue // the appendix schemas are covered by schema/*.v1.json
			}
			examples++
			found := false
			for _, fx := range fixtures {
				if isExcerpt(ex.value, fx.value) {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("%s:%d: JSON example has no fixture under %s", file, ex.line, conceptDir)
			}
		}
	}
	if examples == 0 {
		t.Fatal("no JSON examples found in docs/")
	}
}

// metaSchemaURLs returns the "$schema" values of the embedded schemas, i.e. the
// JSON Schema dialect they are written in.
func metaSchemaURLs(t *testing.T) map[string]bool {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(repoRoot, "schema", "*.v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	urls := map[string]bool{}
	for _, file := range files {
		var doc struct {
			Schema string `json:"$schema"`
		}
		readJSON(t, file, &doc)
		urls[doc.Schema] = true
	}
	return urls
}

// isSchemaDocument reports whether v is itself a JSON Schema (like the appendix drafts).
func isSchemaDocument(v any, metaSchemas map[string]bool) bool {
	m, ok := v.(map[string]any)
	if !ok {
		return false
	}
	s, _ := m["$schema"].(string)
	return metaSchemas[s]
}

func isExcerpt(example, fixture any) bool {
	switch e := example.(type) {
	case map[string]any:
		f, ok := fixture.(map[string]any)
		if !ok {
			return false
		}
		for k, ev := range e {
			fv, ok := f[k]
			if !ok || !isExcerpt(ev, fv) {
				return false
			}
		}
		return true
	case []any:
		f, ok := fixture.([]any)
		if !ok || len(f) != len(e) {
			return false
		}
		for i := range e {
			if !isExcerpt(e[i], f[i]) {
				return false
			}
		}
		return true
	case string:
		f, ok := fixture.(string)
		if !ok {
			return false
		}
		if prefix, abbreviated := strings.CutSuffix(e, "…"); abbreviated {
			return strings.HasPrefix(f, prefix)
		}
		return e == f
	default:
		return reflect.DeepEqual(example, fixture)
	}
}

// document is one JSON value from a file; line is 1-based (0 for whole files).
type document struct {
	line  int
	value any
}

type fixture struct {
	rel    string // path relative to the fixture root, for test names
	target string // validation target, see Validator
	docs   []document
}

// collectFixtures reads every .json and .jsonl file below root. The first directory
// below root names the target; under test/fixtures/valid the "concept" directory adds
// one level (concept/<target>/…).
func collectFixtures(t *testing.T, root string) []fixture {
	t.Helper()
	var out []fixture
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		ext := filepath.Ext(path)
		if ext != ".json" && ext != ".jsonl" {
			t.Errorf("unexpected fixture file %s", path)
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		parts := strings.Split(filepath.ToSlash(rel), "/")
		if parts[0] == "concept" && root == validDir {
			return nil // collected through conceptDir
		}
		if len(parts) < 2 {
			t.Errorf("fixture %s is not inside a target directory", path)
			return nil
		}
		out = append(out, fixture{rel: filepath.ToSlash(rel), target: parts[0], docs: readDocuments(t, path)})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if root == validDir {
		out = append(out, collectFixtures(t, conceptDir)...)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].rel < out[j].rel })
	return out
}

func readDocuments(t *testing.T, path string) []document {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasSuffix(data, []byte("\n")) {
		t.Errorf("%s: missing trailing newline", path)
	}
	if filepath.Ext(path) == ".json" {
		v, err := Decode(data)
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		return []document{{value: v}}
	}
	var docs []document
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		v, err := Decode([]byte(line))
		if err != nil {
			t.Fatalf("%s:%d: %v", path, n, err)
		}
		docs = append(docs, document{line: n, value: v})
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return docs
}

// jsonExamples returns the documents of every ```json block in a Markdown file. Lines
// starting with "$ " (a shell prompt) are skipped; a block that is not one JSON value is
// read as JSON Lines.
func jsonExamples(t *testing.T, path string) []document {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(data), "\n")
	var out []document
	for i := 0; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) != "```json" {
			continue
		}
		start := i + 2 // 1-based line of the first content line
		var body []string
		for i++; i < len(lines) && strings.TrimSpace(lines[i]) != "```"; i++ {
			if strings.HasPrefix(lines[i], "$ ") {
				continue
			}
			body = append(body, lines[i])
		}
		text := strings.Join(body, "\n")
		if v, err := Decode([]byte(text)); err == nil {
			out = append(out, document{line: start, value: v})
			continue
		}
		for j, l := range body {
			if strings.TrimSpace(l) == "" {
				continue
			}
			v, err := Decode([]byte(l))
			if err != nil {
				t.Errorf("%s:%d: JSON example does not parse: %v", path, start+j, err)
				continue
			}
			out = append(out, document{line: start + j, value: v})
		}
	}
	return out
}

func countFiles(t *testing.T, dir string) int {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	return len(entries)
}

func readJSON(t *testing.T, path string, v any) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, v); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
}

func readJSONStrict(t *testing.T, path string, v any) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
}

// TestPropertyNamesPointer guards the workaround in newValidationError: jsonschema v6.0.3
// can report a sibling's location for propertyNames errors, depending on map order.
func TestPropertyNamesPointer(t *testing.T) {
	v := newValidator(t)
	doc := []byte(`{
  "$comment": "sibling properties make the library reuse its location buffer",
  "settings": { "nodeChannel": "lts" },
  "plugins": { "paths": [], "disabled": [] },
  "ui": { "language": "en" },
  "history": { "keepDays": 30 }
}`)
	for i := 0; i < 200; i++ {
		var ve *ValidationError
		if !errors.As(v.ValidateJSON("config", doc), &ve) {
			t.Fatal("want a validation error")
		}
		for _, vi := range ve.Violations {
			if vi.Pointer != "/settings" {
				t.Fatalf("run %d: violation %s at %q, want /settings", i, vi.Keyword, vi.Pointer)
			}
		}
	}
}

func TestPointer(t *testing.T) {
	tests := []struct {
		tokens []string
		want   string
	}{
		{nil, ""},
		{[]string{"settings", "node.channel"}, "/settings/node.channel"},
		{[]string{"a/b", "c~d"}, "/a~1b/c~0d"},
	}
	for _, tt := range tests {
		if got := Pointer(tt.tokens); got != tt.want {
			t.Errorf("Pointer(%q) = %q, want %q", tt.tokens, got, tt.want)
		}
	}
}

func TestCompileField(t *testing.T) {
	f, err := CompileField(map[string]any{
		"type": "integer", "enum": []any{8, 11, 17, 21, 25}, "default": "${java.defaultVersion}",
		"label": "Version", "help": "Major version", "advanced": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	ok, _ := Decode([]byte("21"))
	if err := f.Validate("java.version", ok); err != nil {
		t.Errorf("21: %v", err)
	}
	bad, _ := Decode([]byte("22"))
	var ve *ValidationError
	if !errors.As(f.Validate("java.version", bad), &ve) || !ve.Has("", "enum") {
		t.Errorf("22: want enum violation, got %v", ve)
	}
	arr, err := CompileField(map[string]any{"type": "array", "label": "Scopes", "items": map[string]any{"type": "string", "pattern": "^@", "label": "Scope"}})
	if err != nil {
		t.Fatal(err)
	}
	v, _ := Decode([]byte(`["@a", "b"]`))
	if !errors.As(arr.Validate("npm.scopes", v), &ve) || !ve.Has("/1", "pattern") {
		t.Errorf("want pattern violation at /1, got %v", ve)
	}
}

func TestPropertyOrder(t *testing.T) {
	got, err := PropertyOrder("config")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) < 3 || got[0] != "$schema" || got[2] != "profiles" {
		t.Errorf("order = %v", got)
	}
}
