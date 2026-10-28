package config

import (
	"reflect"
	"testing"
)

func TestCoerceArray(t *testing.T) {
	field := map[string]any{"type": "array", "items": map[string]any{"type": "string"}}
	tests := []struct {
		name    string
		in      string
		want    []any
		wantErr bool
	}{
		{"json", `["@a","@b"]`, []any{"@a", "@b"}, false},
		{"json with spaces", `  ["@a"]`, []any{"@a"}, false},
		{"plain single", "@example", []any{"@example"}, false},
		{"plain several", "@a;@b", []any{"@a", "@b"}, false},
		{"plain trims and drops empty", " @a ; ;@b;", []any{"@a", "@b"}, false},
		{"empty", "", []any{}, false},
		{"broken json", `["@a"`, nil, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Coerce(field, tc.in)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tc.wantErr)
			}
			if !tc.wantErr && !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %#v, want %#v", got, tc.want)
			}
		})
	}
}
