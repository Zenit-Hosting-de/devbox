package cli

import (
	"strings"
	"testing"

	"github.com/Zenit-Hosting-de/devbox/internal/credentials"
)

func TestCheckFields(t *testing.T) {
	defs := map[string]any{
		"layout": map[string]any{"type": "string", "enum": []any{"nexus", "gitlab"}, "label": "Layout"},
		"url":    map[string]any{"type": "string", "label": "Address"},
	}
	tests := []struct {
		name   string
		values map[string]any
		locked []string
		want   string
	}{
		{"valid", map[string]any{"layout": "nexus", "url": "https://example.com"}, []string{"url"}, ""},
		{"enum violation", map[string]any{"layout": "nexsu"}, nil, "layout"},
		{"unknown field", map[string]any{"lyout": "nexus"}, nil, "unknown field"},
		{"wrong type", map[string]any{"url": 3}, nil, "url"},
		{"locked names no field", map[string]any{}, []string{"nope"}, "names no field"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inst := &credentials.Instance{Name: "x", Type: "registry", Locked: tt.locked}
			err := checkFields(inst, tt.values, defs)
			if tt.want == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want containing %q", err, tt.want)
			}
		})
	}
}
