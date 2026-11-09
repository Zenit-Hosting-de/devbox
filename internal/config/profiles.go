package config

import (
	"github.com/Zenit-Hosting-de/devbox/internal/jsonfile"
	"github.com/Zenit-Hosting-de/devbox/internal/schema"
)

// AddProfile appends an entry to profiles of the user configuration file; the fields
// are written in the order given, a map value becomes a nested object.
func AddProfile(v *schema.Validator, path string, fields [][2]any) error {
	return UpdateDocument(v, path, func(doc *jsonfile.Object) error {
		var list []any
		if raw, ok := doc.Get("profiles"); ok {
			if list, ok = raw.([]any); !ok {
				return &Error{Code: CodeValidation, Detail: path + ": profiles is not an array"}
			}
		}
		doc.Set("profiles", append(list, objectOf(fields)))
		return nil
	})
}

func objectOf(fields [][2]any) *jsonfile.Object {
	obj := jsonfile.NewObject()
	for _, kv := range fields {
		key, _ := kv[0].(string)
		if m, ok := kv[1].(map[string]any); ok {
			nested := make([][2]any, 0, len(m))
			for _, k := range []string{"issuer", "identity"} {
				if val, ok := m[k]; ok {
					nested = append(nested, [2]any{k, val})
				}
			}
			obj.Set(key, objectOf(nested))
			continue
		}
		obj.Set(key, kv[1])
	}
	return obj
}

// RemoveProfile deletes the entries of profiles of the user configuration file for
// which match (given the entry as plain map) is true, and reports how many it removed.
// An empty profiles array is removed.
func RemoveProfile(v *schema.Validator, path string, match func(map[string]any) bool) (int, error) {
	removed := 0
	err := UpdateDocument(v, path, func(doc *jsonfile.Object) error {
		raw, ok := doc.Get("profiles")
		if !ok {
			return ErrUnchanged
		}
		list, ok := raw.([]any)
		if !ok {
			return &Error{Code: CodeValidation, Detail: path + ": profiles is not an array"}
		}
		next := make([]any, 0, len(list))
		for _, e := range list {
			if m, _ := jsonfile.Plain(e).(map[string]any); m != nil && match(m) {
				removed++
				continue
			}
			next = append(next, e)
		}
		if removed == 0 {
			return ErrUnchanged
		}
		if len(next) > 0 {
			doc.Set("profiles", next)
		} else {
			doc.Delete("profiles")
		}
		return nil
	})
	return removed, err
}
