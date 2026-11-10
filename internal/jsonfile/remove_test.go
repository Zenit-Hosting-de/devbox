package jsonfile

import (
	"strings"
	"testing"
)

func TestRemovePointer(t *testing.T) {
	src := "{\n\t\"first\": 1,\n\t\"mid\": {\"k\": [1, 2, 3]},\n\t\"last\": \"x\"\n}\n"
	tests := []struct {
		ptr, want string
	}{
		{"/first", "{\n\t\"mid\": {\"k\": [1, 2, 3]},\n\t\"last\": \"x\"\n}\n"},
		{"/mid", "{\n\t\"first\": 1,\n\t\"last\": \"x\"\n}\n"},
		{"/last", "{\n\t\"first\": 1,\n\t\"mid\": {\"k\": [1, 2, 3]}\n}\n"},
		{"/mid/k", "{\n\t\"first\": 1,\n\t\"mid\": {},\n\t\"last\": \"x\"\n}\n"},
		{"/mid/k/0", "{\n\t\"first\": 1,\n\t\"mid\": {\"k\": [2, 3]},\n\t\"last\": \"x\"\n}\n"},
		{"/mid/k/2", "{\n\t\"first\": 1,\n\t\"mid\": {\"k\": [1, 2]},\n\t\"last\": \"x\"\n}\n"},
	}
	for _, tt := range tests {
		got, found, err := RemovePointer([]byte(src), tt.ptr)
		if err != nil || !found {
			t.Errorf("%s: found = %v, err = %v", tt.ptr, found, err)
			continue
		}
		if string(got) != tt.want {
			t.Errorf("%s:\n%s\nwant\n%s", tt.ptr, got, tt.want)
		}
		if _, err := Parse(got); err != nil {
			t.Errorf("%s: result does not parse: %v", tt.ptr, err)
		}
	}
	if got, found, err := RemovePointer([]byte(src), "/none/k"); err != nil || found || string(got) != src {
		t.Errorf("missing value: found = %v, err = %v, got %s", found, err, got)
	}
	if _, _, err := RemovePointer([]byte(src), ""); err == nil {
		t.Error("removing the document: want an error")
	}
}

// TestRemovePointerUndoesSetPointer: a member that SetPointer added is removed without
// a trace, whatever the formatting of the document.
func TestRemovePointerUndoesSetPointer(t *testing.T) {
	docs := []string{
		"{}\n",
		"{\n  \"a\": 1\n}\n",
		"{\n\t\"auths\": {\n\t\t\"registry.example.com\": {\n\t\t\t\"auth\": \"eA==\"\n\t\t}\n\t}\n}",
		"{\"a\": {\"b\": 1}, \"c\": []}",
		"{\r\n    \"a\": {}\r\n}\r\n",
	}
	for _, doc := range docs {
		for _, ptr := range []string{"/new", "/a/new"} {
			set, err := SetPointer([]byte(doc), ptr, map[string]any{"k": "v"})
			if err != nil {
				continue // /a is not an object in this document
			}
			back, found, err := RemovePointer(set, ptr)
			if err != nil || !found {
				t.Errorf("%q %s: found = %v, err = %v", doc, ptr, found, err)
				continue
			}
			if ptr == "/a/new" && !strings.Contains(doc, "\"a\"") {
				// SetPointer created the parent as well
				if back, _, err = RemovePointer(back, "/a"); err != nil {
					t.Fatal(err)
				}
			}
			if string(back) != doc {
				t.Errorf("%q %s: after set and remove\n%q", doc, ptr, back)
			}
		}
	}
}
