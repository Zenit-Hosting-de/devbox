package jsonfile

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseMarshalKeepsOrder(t *testing.T) {
	src := "{\n  \"z\": 1,\n  \"a\": {\n    \"y\": [\n      true,\n      null\n    ],\n    \"b\": \"<x>\"\n  },\n  \"e\": {},\n  \"f\": []\n}\n"
	v, err := Parse([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	out, err := Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != src {
		t.Errorf("round trip changed the document:\n%s", out)
	}
}

func TestParseErrors(t *testing.T) {
	for _, src := range []string{`{"a":1,"a":2}`, `{"a":1} {}`, `{"a":`} {
		if _, err := Parse([]byte(src)); err == nil {
			t.Errorf("Parse(%q): want an error", src)
		}
	}
}

func TestObjectReorderAndDelete(t *testing.T) {
	o := NewObject()
	for _, k := range []string{"x", "settings", "$schema", "profiles"} {
		o.Set(k, 1)
	}
	o.Reorder([]string{"$schema", "profiles", "plugins", "settings"})
	o.Delete("profiles")
	got := o.Keys()
	want := []string{"$schema", "settings", "x"}
	if len(got) != len(want) {
		t.Fatalf("keys = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("keys = %v, want %v", got, want)
		}
	}
}

func TestMarshalSortsPlainMaps(t *testing.T) {
	out, err := Marshal(map[string]any{"b": 1, "a": "x"})
	if err != nil {
		t.Fatal(err)
	}
	if want := "{\n  \"a\": \"x\",\n  \"b\": 1\n}\n"; string(out) != want {
		t.Errorf("got %q, want %q", out, want)
	}
}

func TestWriteAtomic(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "config.json")
	if err := WriteAtomic(path, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := WriteAtomic(path, []byte("{\"a\": 1}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "{\"a\": 1}\n" {
		t.Errorf("content = %q", data)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v, want 0600", info.Mode().Perm())
	}
	entries, _ := os.ReadDir(filepath.Dir(path))
	if len(entries) != 1 {
		t.Errorf("temporary files left behind: %v", entries)
	}
}

func TestLine(t *testing.T) {
	src := []byte("{\n  \"a\": 1,\n  \"settings\": {\n    \"node.channel\": \"lts\",\n    \"x\": [\n      1,\n      {\"deep\": true}\n    ]\n  }\n}\n")
	tests := map[string]int{
		"":                       1,
		"/a":                     2,
		"/settings":              3,
		"/settings/node.channel": 4,
		"/settings/x/1":          7,
		"/settings/x/1/deep":     7,
		"/settings/missing":      3,
		"/nothing/here":          1,
	}
	for ptr, want := range map[string]int{"/colour": 3, "/after": 4} {
		doc := []byte("{\n  \"settings\": {},\n  \"colour\": {\"x\": [1]},\n  \"after\": 1\n}\n")
		if got := Line(doc, ptr); got != want {
			t.Errorf("after an empty object: Line(%q) = %d, want %d", ptr, got, want)
		}
	}
	for ptr, want := range tests {
		if got := Line(src, ptr); got != want {
			t.Errorf("Line(%q) = %d, want %d", ptr, got, want)
		}
	}
}
