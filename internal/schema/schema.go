// Package schema validates DevBox documents against the embedded JSON Schemas
// (schema/*.v1.json) and the component schemas of the API contract (api/openapi.json).
//
// A validation target is named like this:
//
//	config                 schema/config.v1.json
//	event.message          schema/event.v1.json#/$defs/message
//	openapi.Problem        api/openapi.json#/components/schemas/Problem
//
// Schema URLs are never written in code: they come from the "$id" of each embedded schema.
package schema

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"path"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/santhosh-tekuri/jsonschema/v6/kind"
	"golang.org/x/text/language"
	"golang.org/x/text/message"

	apifs "github.com/Zenit-Hosting-de/devbox/api"
	schemafs "github.com/Zenit-Hosting-de/devbox/schema"
)

// OpenAPITarget is the target prefix for component schemas of api/openapi.json.
const OpenAPITarget = "openapi"

const schemaSuffix = ".v1.json"

// Validator validates documents against the embedded schemas. It is safe for concurrent use.
type Validator struct {
	mu         sync.Mutex
	compiler   *jsonschema.Compiler
	ids        map[string]string // schema name → $id
	names      map[string]string // $id → schema name
	baseURL    *url.URL          // directory of all schema $ids
	openAPIURL string
	compiled   map[string]*jsonschema.Schema
}

// New loads all embedded schemas and the OpenAPI document.
func New() (*Validator, error) {
	c := jsonschema.NewCompiler()
	c.DefaultDraft(jsonschema.Draft2020)
	c.AssertFormat()
	// "path" is a DevBox format for file system paths; any string is accepted here,
	// placeholders are resolved and checked by the configuration layer.
	c.RegisterFormat(&jsonschema.Format{Name: "path", Validate: func(any) error { return nil }})

	v := &Validator{
		compiler: c,
		ids:      map[string]string{},
		names:    map[string]string{},
		compiled: map[string]*jsonschema.Schema{},
	}

	files, err := fs.Glob(schemafs.FS, "*"+schemaSuffix)
	if err != nil {
		return nil, fmt.Errorf("list embedded schemas: %w", err)
	}
	if len(files) == 0 {
		return nil, errors.New("no embedded schemas found")
	}
	for _, file := range files {
		if err := v.addSchema(file); err != nil {
			return nil, err
		}
	}

	openAPIURL := v.baseURL.ResolveReference(&url.URL{Path: "../api/openapi.json"})
	v.openAPIURL = openAPIURL.String()
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(apifs.OpenAPI))
	if err != nil {
		return nil, fmt.Errorf("parse api/openapi.json: %w", err)
	}
	if err := c.AddResource(v.openAPIURL, doc); err != nil {
		return nil, fmt.Errorf("add api/openapi.json: %w", err)
	}
	return v, nil
}

func (v *Validator) addSchema(file string) error {
	data, err := schemafs.FS.ReadFile(file)
	if err != nil {
		return fmt.Errorf("read schema %s: %w", file, err)
	}
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("parse schema %s: %w", file, err)
	}
	obj, ok := doc.(map[string]any)
	if !ok {
		return fmt.Errorf("schema %s: root is not an object", file)
	}
	id, _ := obj["$id"].(string)
	u, err := url.Parse(id)
	if err != nil || !u.IsAbs() {
		return fmt.Errorf("schema %s: $id %q is not an absolute URL", file, id)
	}
	if path.Base(u.Path) != file {
		return fmt.Errorf("schema %s: $id %q does not end with the file name", file, id)
	}
	dir := u.ResolveReference(&url.URL{Path: "./"})
	if v.baseURL == nil {
		v.baseURL = dir
	} else if dir.String() != v.baseURL.String() {
		return fmt.Errorf("schema %s: $id %q is not below %s", file, id, v.baseURL)
	}
	if err := v.compiler.AddResource(id, doc); err != nil {
		return fmt.Errorf("add schema %s: %w", file, err)
	}
	name := strings.TrimSuffix(file, schemaSuffix)
	v.ids[name] = id
	v.names[id] = name
	return nil
}

// Names returns the names of all embedded schemas, sorted.
func (v *Validator) Names() []string {
	names := make([]string, 0, len(v.ids))
	for name := range v.ids {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// ID returns the "$id" of the named schema.
func (v *Validator) ID(name string) (string, bool) {
	id, ok := v.ids[name]
	return id, ok
}

// NameForURL maps a "$schema" value of a document to the schema name. known reports
// whether the URL points into the DevBox schema directory at all, so that a typo
// such as a wrong version can be reported instead of being skipped.
func (v *Validator) NameForURL(schemaURL string) (name string, known bool) {
	if name, ok := v.names[schemaURL]; ok {
		return name, true
	}
	return "", strings.HasPrefix(schemaURL, v.baseURL.String())
}

func (v *Validator) location(target string) (string, error) {
	base, def, hasDef := strings.Cut(target, ".")
	if base == OpenAPITarget {
		if !hasDef || def == "" {
			return "", fmt.Errorf("target %q: missing component name", target)
		}
		return v.openAPIURL + "#/components/schemas/" + def, nil
	}
	id, ok := v.ids[base]
	if !ok {
		return "", fmt.Errorf("unknown schema %q", base)
	}
	if !hasDef {
		return id, nil
	}
	return id + "#/$defs/" + def, nil
}

func (v *Validator) schema(target string) (*jsonschema.Schema, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if s, ok := v.compiled[target]; ok {
		return s, nil
	}
	loc, err := v.location(target)
	if err != nil {
		return nil, err
	}
	s, err := v.compiler.Compile(loc)
	if err != nil {
		return nil, fmt.Errorf("compile %s: %w", target, err)
	}
	v.compiled[target] = s
	return s, nil
}

// Compile checks that the target exists and compiles.
func (v *Validator) Compile(target string) error {
	_, err := v.schema(target)
	return err
}

// Validate validates a decoded document (as returned by Decode) against the target.
// A validation failure is returned as *ValidationError.
func (v *Validator) Validate(target string, doc any) error {
	s, err := v.schema(target)
	if err != nil {
		return err
	}
	err = s.Validate(doc)
	if err == nil {
		return nil
	}
	var ve *jsonschema.ValidationError
	if !errors.As(err, &ve) {
		return fmt.Errorf("validate %s: %w", target, err)
	}
	return newValidationError(target, doc, ve)
}

// ValidateJSON decodes data and validates it against the target.
func (v *Validator) ValidateJSON(target string, data []byte) error {
	doc, err := Decode(data)
	if err != nil {
		return err
	}
	return v.Validate(target, doc)
}

// Decode parses one JSON document, keeping numbers exact.
func Decode(data []byte) (any, error) {
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("parse JSON: %w", err)
	}
	return doc, nil
}

// Violation is one failed keyword at one instance location.
type Violation struct {
	// Pointer is the JSON Pointer (RFC 6901) of the failing value, "" for the root.
	Pointer string
	// Keyword is the failing JSON Schema keyword, e.g. "required" or "pattern".
	Keyword string
	// Message describes the failure in English, for developers and logs.
	Message string
	// Leaf is true for the innermost failures that explain the error.
	Leaf bool
}

// ValidationError lists every violation found while validating one document.
type ValidationError struct {
	Target     string
	Violations []Violation
}

func (e *ValidationError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "invalid %s:", e.Target)
	for _, vi := range e.Violations {
		if vi.Leaf {
			fmt.Fprintf(&b, "\n  %s: %s: %s", displayPointer(vi.Pointer), vi.Keyword, vi.Message)
		}
	}
	return b.String()
}

// Has reports whether a violation with this pointer and keyword exists.
func (e *ValidationError) Has(pointer, keyword string) bool {
	for _, vi := range e.Violations {
		if vi.Pointer == pointer && vi.Keyword == keyword {
			return true
		}
	}
	return false
}

func displayPointer(p string) string {
	if p == "" {
		return "/"
	}
	return p
}

var printer = message.NewPrinter(language.English)

func newValidationError(target string, doc any, root *jsonschema.ValidationError) *ValidationError {
	out := &ValidationError{Target: target}
	// override is set for subtrees whose own InstanceLocation cannot be trusted.
	var walk func(ve *jsonschema.ValidationError, override *string)
	walk = func(ve *jsonschema.ValidationError, override *string) {
		pointer := Pointer(ve.InstanceLocation)
		if pn, ok := ve.ErrorKind.(*kind.PropertyNames); ok {
			// jsonschema v6.0.3 stores its internal location slice for propertyNames errors
			// without copying it, so validating sibling properties can overwrite the tokens
			// (the length stays right). Locate the object by depth and property name instead.
			// The causes describe the property name, not a document location, so they get
			// the same pointer.
			fixed := locateObjectWithKey(doc, len(ve.InstanceLocation), pn.Property, pointer)
			override = &fixed
		}
		if override != nil {
			pointer = *override
		}
		keywords := ve.ErrorKind.KeywordPath()
		if len(keywords) > 0 {
			out.Violations = append(out.Violations, Violation{
				Pointer: pointer,
				Keyword: keywords[len(keywords)-1],
				Message: ve.ErrorKind.LocalizedString(printer),
				Leaf:    len(ve.Causes) == 0,
			})
		}
		for _, cause := range ve.Causes {
			walk(cause, override)
		}
	}
	walk(root, nil)
	return out
}

// locateObjectWithKey returns the pointer of the only object at the given depth that has
// the key. With no or several candidates it returns fallback.
func locateObjectWithKey(doc any, depth int, key, fallback string) string {
	var found []string
	var visit func(v any, tokens []string)
	visit = func(v any, tokens []string) {
		if len(tokens) == depth {
			if obj, ok := v.(map[string]any); ok {
				if _, has := obj[key]; has {
					found = append(found, Pointer(tokens))
				}
			}
			return
		}
		switch t := v.(type) {
		case map[string]any:
			for k, child := range t {
				visit(child, append(slices.Clone(tokens), k))
			}
		case []any:
			for i, child := range t {
				visit(child, append(slices.Clone(tokens), strconv.Itoa(i)))
			}
		}
	}
	visit(doc, nil)
	if len(found) == 1 {
		return found[0]
	}
	return fallback
}

// Pointer builds a JSON Pointer from unescaped reference tokens.
func Pointer(tokens []string) string {
	var b strings.Builder
	for _, t := range tokens {
		b.WriteByte('/')
		t = strings.ReplaceAll(t, "~", "~0")
		b.WriteString(strings.ReplaceAll(t, "/", "~1"))
	}
	return b.String()
}
