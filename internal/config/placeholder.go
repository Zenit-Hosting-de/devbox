package config

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Lookup returns the value of one placeholder; ref is the text between "${" and "}".
type Lookup func(ref string) (any, error)

// Expand replaces the placeholders in s. "$${" writes a literal "${". When s consists
// of exactly one placeholder, the referenced value keeps its type (e.g. an integer
// setting); otherwise every value is converted to text.
func Expand(s string, lookup Lookup) (any, error) {
	if !strings.Contains(s, "${") {
		return s, nil
	}
	if ref, ok := whole(s); ok {
		return lookup(ref)
	}
	var b strings.Builder
	for i := 0; i < len(s); {
		switch {
		case strings.HasPrefix(s[i:], "$${"):
			b.WriteString("${")
			i += 3
		case strings.HasPrefix(s[i:], "${"):
			end := strings.IndexByte(s[i+2:], '}')
			if end < 0 {
				return nil, fmt.Errorf("unclosed placeholder in %q", s)
			}
			ref := s[i+2 : i+2+end]
			v, err := lookup(ref)
			if err != nil {
				return nil, err
			}
			text, err := toText(v)
			if err != nil {
				return nil, fmt.Errorf("placeholder ${%s}: %w", ref, err)
			}
			b.WriteString(text)
			i += 3 + end
		default:
			b.WriteByte(s[i])
			i++
		}
	}
	return b.String(), nil
}

// whole reports whether s is exactly one placeholder.
func whole(s string) (string, bool) {
	if !strings.HasPrefix(s, "${") || !strings.HasSuffix(s, "}") {
		return "", false
	}
	ref := s[2 : len(s)-1]
	if strings.ContainsAny(ref, "{}") || strings.Contains(ref, "${") {
		return "", false
	}
	return ref, true
}

func toText(v any) (string, error) {
	switch t := v.(type) {
	case string:
		return t, nil
	case json.Number:
		return t.String(), nil
	case bool, int, int64, float64:
		return fmt.Sprint(t), nil
	case nil:
		return "", nil
	default:
		return "", fmt.Errorf("a %T cannot be part of a text", v)
	}
}

// ExpandValue expands placeholders in every string inside v (objects and arrays
// included) and returns a new value.
func ExpandValue(v any, lookup Lookup) (any, error) {
	switch t := v.(type) {
	case string:
		return Expand(t, lookup)
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, e := range t {
			x, err := ExpandValue(e, lookup)
			if err != nil {
				return nil, err
			}
			out[k] = x
		}
		return out, nil
	case []any:
		out := make([]any, len(t))
		for i, e := range t {
			x, err := ExpandValue(e, lookup)
			if err != nil {
				return nil, err
			}
			out[i] = x
		}
		return out, nil
	default:
		return v, nil
	}
}
