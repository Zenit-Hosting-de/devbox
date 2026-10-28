package schema

import (
	"fmt"
	"strings"

	schemafs "github.com/Zenit-Hosting-de/devbox/schema"
)

// Default returns the value at a JSON Pointer of an embedded schema, normally a
// "default", e.g. ("config", "/properties/server/properties/listen/default"). Defaults
// such as the listen address live in the schemas only, never in Go code.
func Default(name, pointer string) (any, error) {
	data, err := schemafs.FS.ReadFile(name + schemaSuffix)
	if err != nil {
		return nil, fmt.Errorf("read schema %s: %w", name, err)
	}
	doc, err := Decode(data)
	if err != nil {
		return nil, fmt.Errorf("parse schema %s: %w", name, err)
	}
	cur := doc
	for _, part := range strings.Split(strings.TrimPrefix(pointer, "/"), "/") {
		part = strings.ReplaceAll(strings.ReplaceAll(part, "~1", "/"), "~0", "~")
		obj, ok := cur.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("schema %s: %s is not in the schema", name, pointer)
		}
		if cur, ok = obj[part]; !ok {
			return nil, fmt.Errorf("schema %s: %s is not in the schema", name, pointer)
		}
	}
	return cur, nil
}

// DefaultString is Default for a string value.
func DefaultString(name, pointer string) (string, error) {
	v, err := Default(name, pointer)
	if err != nil {
		return "", err
	}
	s, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("schema %s: %s is not a string", name, pointer)
	}
	return s, nil
}

// Raw returns the bytes of an embedded schema (name without the version suffix).
func Raw(name string) ([]byte, bool) {
	data, err := schemafs.FS.ReadFile(name + schemaSuffix)
	if err != nil {
		return nil, false
	}
	return data, true
}
