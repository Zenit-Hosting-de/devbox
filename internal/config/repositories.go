package config

import (
	"github.com/Zenit-Hosting-de/devbox/internal/jsonfile"
	"github.com/Zenit-Hosting-de/devbox/internal/schema"
)

// SetRepository writes the entry repositories.<name> of the user configuration file;
// the fields are written in the order given. An existing entry of the same name is
// replaced.
func SetRepository(v *schema.Validator, path, name string, fields [][2]any) error {
	return UpdateDocument(v, path, func(doc *jsonfile.Object) error {
		repos, ok := mustObject(doc, "repositories")
		if !ok {
			return &Error{Code: CodeValidation, Detail: path + ": repositories is not an object"}
		}
		entry := jsonfile.NewObject()
		for _, kv := range fields {
			key, _ := kv[0].(string)
			entry.Set(key, kv[1])
		}
		repos.Set(name, entry)
		return nil
	})
}

// RemoveRepository deletes the entry repositories.<name> of the user configuration
// file and reports whether there was one. An empty repositories object is removed.
func RemoveRepository(v *schema.Validator, path, name string) (bool, error) {
	removed := false
	err := UpdateDocument(v, path, func(doc *jsonfile.Object) error {
		raw, ok := doc.Get("repositories")
		if !ok {
			return nil
		}
		repos, ok := raw.(*jsonfile.Object)
		if !ok {
			return &Error{Code: CodeValidation, Detail: path + ": repositories is not an object"}
		}
		if _, ok := repos.Get(name); ok {
			repos.Delete(name)
			removed = true
		}
		if repos.Len() == 0 {
			doc.Delete("repositories")
		}
		return nil
	})
	return removed, err
}

// SetPluginDisabled adds a plugin ID to plugins.disabled of the user configuration
// file, or removes it. Doing it twice changes nothing; it reports whether the file
// content changed. An empty plugins.disabled or plugins object is removed.
func SetPluginDisabled(v *schema.Validator, path, id string, disabled bool) (bool, error) {
	changed := false
	err := UpdateDocument(v, path, func(doc *jsonfile.Object) error {
		plugins, ok := mustObject(doc, "plugins")
		if !ok {
			return &Error{Code: CodeValidation, Detail: path + ": plugins is not an object"}
		}
		var ids []any
		if raw, ok := plugins.Get("disabled"); ok {
			ids, _ = raw.([]any)
		}
		next := make([]any, 0, len(ids)+1)
		present := false
		for _, e := range ids {
			if s, _ := e.(string); s == id {
				present = true
				if !disabled {
					continue
				}
			}
			next = append(next, e)
		}
		switch {
		case disabled && !present:
			next = append(next, id)
			changed = true
		case !disabled && present:
			changed = true
		default:
			return ErrUnchanged
		}
		if len(next) > 0 {
			plugins.Set("disabled", next)
		} else {
			plugins.Delete("disabled")
		}
		if plugins.Len() == 0 {
			doc.Delete("plugins")
		}
		return nil
	})
	return changed, err
}

// PatchRepository changes fields of the existing entry repositories.<name> of the user
// configuration file: set writes or replaces fields, remove deletes them; all other
// fields stay as they are. A missing entry is a not-found error.
func PatchRepository(v *schema.Validator, path, name string, set [][2]any, remove []string) error {
	return UpdateDocument(v, path, func(doc *jsonfile.Object) error {
		raw, _ := doc.Get("repositories")
		repos, _ := raw.(*jsonfile.Object)
		var entry *jsonfile.Object
		if repos != nil {
			rawEntry, _ := repos.Get(name)
			entry, _ = rawEntry.(*jsonfile.Object)
		}
		if entry == nil {
			return &Error{Code: CodeNotFound, Key: name, Detail: "no such repository in the user configuration"}
		}
		for _, key := range remove {
			entry.Delete(key)
		}
		for _, kv := range set {
			key, _ := kv[0].(string)
			entry.Set(key, kv[1])
		}
		return nil
	})
}
