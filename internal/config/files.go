package config

import (
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"strings"

	"github.com/Zenit-Hosting-de/devbox/internal/jsonfile"
	"github.com/Zenit-Hosting-de/devbox/internal/schema"
)

// File is a loaded configuration or profile file.
type File struct {
	Path string
	Data []byte
	// Doc is the parsed document; an empty object when the file does not exist.
	Doc    *jsonfile.Object
	Exists bool
}

// Plain returns the document as map[string]any.
func (f *File) Plain() map[string]any {
	m, _ := jsonfile.Plain(f.Doc).(map[string]any)
	return m
}

// ReadFile loads and strictly validates a JSON file against the named schema. A
// missing file is an empty document when optional is true.
func ReadFile(v *schema.Validator, target, path string, optional bool) (*File, error) {
	f := &File{Path: path, Doc: jsonfile.NewObject()}
	data, err := os.ReadFile(path) //nolint:gosec // configuration paths come from the user or defaults
	if errors.Is(err, fs.ErrNotExist) && optional {
		return f, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	f.Data, f.Exists = data, true
	doc, err := jsonfile.Parse(data)
	if err != nil {
		return nil, &Error{Code: CodeValidation, Detail: fmt.Sprintf("%s: %v", path, err)}
	}
	obj, ok := doc.(*jsonfile.Object)
	if !ok {
		return nil, &Error{Code: CodeValidation, Detail: path + ": the document is not a JSON object"}
	}
	f.Doc = obj
	if err := v.Validate(target, jsonfile.Plain(doc)); err != nil {
		return nil, fileError(path, data, err)
	}
	return f, nil
}

// fileError turns a validation error into an *Error that names file, line and pointer
// of every violation.
func fileError(path string, data []byte, err error) error {
	var ve *schema.ValidationError
	if !errors.As(err, &ve) {
		return err
	}
	var lines []string
	for _, vi := range ve.Violations {
		if !vi.Leaf {
			continue
		}
		pointers := []string{vi.Pointer}
		if len(vi.Properties) > 0 {
			// point at each unknown key instead of the object that contains it
			pointers = pointers[:0]
			for _, p := range vi.Properties {
				pointers = append(pointers, schema.Pointer(append(splitPointer(vi.Pointer), p)))
			}
		}
		for _, p := range pointers {
			lines = append(lines, fmt.Sprintf("%s: %s: %s", schema.Location(path, data, p), vi.Keyword, vi.Message))
		}
	}
	return &Error{Code: CodeValidation, Detail: strings.Join(lines, "\n")}
}

func splitPointer(p string) []string {
	if p == "" {
		return nil
	}
	parts := strings.Split(strings.TrimPrefix(p, "/"), "/")
	for i, s := range parts {
		parts[i] = strings.ReplaceAll(strings.ReplaceAll(s, "~1", "/"), "~0", "~")
	}
	return parts
}

// SettingsLayer returns the "settings" of a configuration file as a layer.
func SettingsLayer(name string, f *File) Layer {
	l := Layer{Name: name, Values: map[string]any{}}
	if s, ok := f.Plain()["settings"].(map[string]any); ok {
		l.Values = s
	}
	return l
}

// ProfileLayer returns the settings of a profile as a layer named profile:<id>.
func ProfileLayer(f *File) Layer {
	doc := f.Plain()
	id, _ := doc["id"].(string)
	l := Layer{Name: LayerProfilePrefix + id, Values: map[string]any{}, Locked: map[string]bool{}}
	settings, _ := doc["settings"].(map[string]any)
	for key, raw := range settings {
		entry, _ := raw.(map[string]any)
		l.Values[key] = entry["value"]
		if locked, _ := entry["locked"].(bool); locked {
			l.Locked[key] = true
		}
	}
	return l
}

// ProfilePath resolves a profile source to a local file. Until AP-12 adds HTTPS and
// Git sources, only file:// sources are supported. Placeholders other than settings
// are expanded first.
func ProfilePath(ctx Context, source string, relPath string) (string, error) {
	expanded, err := Expand(source, func(ref string) (any, error) { return ContextLookup(ctx, ref) })
	if err != nil {
		return "", err
	}
	s, _ := expanded.(string)
	u, err := url.Parse(s)
	if err != nil {
		return "", &Error{Code: CodeValidation, Detail: fmt.Sprintf("profile source %q: %v", source, err)}
	}
	if u.Scheme != "file" {
		return "", &Error{Code: CodeValidation, Detail: fmt.Sprintf("profile source %q: only file:// sources are supported yet", source)}
	}
	p := u.Path
	if relPath != "" {
		p = strings.TrimSuffix(p, "/") + "/" + relPath
	}
	return p, nil
}

// Set writes a user value into the configuration file at path. The value must be
// valid for the field and the key must not be locked by a profile.
func Set(v *schema.Validator, path string, defs Definitions, current *Result, key string, value any) error {
	field, ok := defs[key]
	if !ok {
		return &Error{Code: CodeNotFound, Key: key, Detail: "no plugin defines this setting"}
	}
	if by := current.LockedBy(key); by != "" {
		return &Error{Code: CodeSettingLocked, Key: key, Detail: "locked by " + by}
	}
	coerced, err := Coerce(field, value)
	if err != nil {
		return &Error{Code: CodeValidation, Key: key, Detail: err.Error()}
	}
	// Placeholders are stored as written; validate the plain value only.
	if s, isText := coerced.(string); !isText || !strings.Contains(s, "${") {
		if err := validateValue(key, field, coerced); err != nil {
			return err
		}
	}
	return update(v, path, func(settings *jsonfile.Object) { settings.Set(key, coerced) })
}

// Unset removes a user value from the configuration file at path.
func Unset(v *schema.Validator, path string, key string) error {
	if !ValidKey(key) {
		return &Error{Code: CodeValidation, Key: key, Detail: "want <plugin>.<name>"}
	}
	return update(v, path, func(settings *jsonfile.Object) { settings.Delete(key) })
}

func update(v *schema.Validator, path string, change func(*jsonfile.Object)) error {
	f, err := ReadFile(v, "config", path, true)
	if err != nil {
		return err
	}
	doc := f.Doc
	if _, ok := doc.Get("$schema"); !ok {
		if id, ok := v.ID("config"); ok {
			doc.Set("$schema", id)
		}
	}
	settings, ok := mustObject(doc, "settings")
	if !ok {
		return &Error{Code: CodeValidation, Detail: path + ": settings is not an object"}
	}
	change(settings)
	if settings.Len() == 0 {
		doc.Delete("settings")
	}
	order, err := schema.PropertyOrder("config")
	if err != nil {
		return err
	}
	doc.Reorder(order)
	if err := v.Validate("config", jsonfile.Plain(doc)); err != nil {
		return err
	}
	data, err := jsonfile.Marshal(doc)
	if err != nil {
		return err
	}
	return jsonfile.WriteAtomic(path, data, 0o644)
}

func mustObject(doc *jsonfile.Object, key string) (*jsonfile.Object, bool) {
	v, ok := doc.Get(key)
	if !ok {
		obj := jsonfile.NewObject()
		doc.Set(key, obj)
		return obj, true
	}
	obj, ok := v.(*jsonfile.Object)
	return obj, ok
}
