package schema

import (
	"errors"
	"fmt"
	"strings"
	"sync/atomic"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/Zenit-Hosting-de/devbox/internal/jsonfile"
	schemafs "github.com/Zenit-Hosting-de/devbox/schema"
)

// fieldOnlyKeywords are keywords of a plugin field (plugin.v1.json#/$defs/field) that
// describe the form, not the value; they are removed before validating a value.
var fieldOnlyKeywords = []string{"label", "help", "advanced", "secret", "optionsFrom"}

// Fragment is a compiled schema fragment, e.g. the field of a plugin setting.
type Fragment struct {
	schema *jsonschema.Schema
}

var fragmentSeq atomic.Uint64

// CompileField compiles a plugin field (setting, parameter, credential field or
// consumer option) into a schema for its value.
func CompileField(field map[string]any) (*Fragment, error) {
	c := jsonschema.NewCompiler()
	c.DefaultDraft(jsonschema.Draft2020)
	c.AssertFormat()
	c.RegisterFormat(&jsonschema.Format{Name: "path", Validate: func(any) error { return nil }})
	// The URL only names the in-memory resource; it is never fetched.
	url := fmt.Sprintf("mem:///field/%d.json", fragmentSeq.Add(1))
	if err := c.AddResource(url, stripFieldKeywords(field)); err != nil {
		return nil, fmt.Errorf("add field schema: %w", err)
	}
	s, err := c.Compile(url)
	if err != nil {
		return nil, fmt.Errorf("compile field schema: %w", err)
	}
	return &Fragment{schema: s}, nil
}

// Validate validates a decoded value; a failure is a *ValidationError.
func (f *Fragment) Validate(target string, v any) error {
	err := f.schema.Validate(v)
	if err == nil {
		return nil
	}
	var ve *jsonschema.ValidationError
	if !errors.As(err, &ve) {
		return err
	}
	return newValidationError(target, v, ve)
}

func stripFieldKeywords(v any) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, e := range t {
			if isFieldOnly(k) {
				continue
			}
			switch k {
			case "properties":
				if props, ok := e.(map[string]any); ok {
					stripped := make(map[string]any, len(props))
					for name, sub := range props {
						stripped[name] = stripFieldKeywords(sub)
					}
					out[k] = stripped
					continue
				}
			case "items", "additionalProperties":
				out[k] = stripFieldKeywords(e)
				continue
			}
			out[k] = e
		}
		return out
	default:
		return v
	}
}

func isFieldOnly(k string) bool {
	for _, f := range fieldOnlyKeywords {
		if k == f {
			return true
		}
	}
	return false
}

// PropertyOrder returns the order of the root properties of an embedded schema; files
// written by DevBox use this key order.
func PropertyOrder(name string) ([]string, error) {
	data, err := schemafs.FS.ReadFile(name + schemaSuffix)
	if err != nil {
		return nil, fmt.Errorf("read schema %s: %w", name, err)
	}
	doc, err := jsonfile.Parse(data)
	if err != nil {
		return nil, fmt.Errorf("parse schema %s: %w", name, err)
	}
	root, ok := doc.(*jsonfile.Object)
	if !ok {
		return nil, fmt.Errorf("schema %s: root is not an object", name)
	}
	props, _ := root.Get("properties")
	obj, ok := props.(*jsonfile.Object)
	if !ok {
		return nil, fmt.Errorf("schema %s: no properties", name)
	}
	return obj.Keys(), nil
}

// Location formats where a violation is: "file:line: /pointer".
func Location(file string, data []byte, pointer string) string {
	var b strings.Builder
	if file != "" {
		fmt.Fprintf(&b, "%s:%d: ", file, jsonfile.Line(data, pointer))
	}
	b.WriteString(displayPointer(pointer))
	return b.String()
}
