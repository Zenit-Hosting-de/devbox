// Package credentials keeps the credentials directory
// (~/.local/share/devbox/credentials.json, schema/credentials.v1.json): metadata of
// credential instances without secrets. It merges the instances that profiles prepare
// (fields preset and possibly locked, origin profile:<id>) with what the user added
// (docs/concept.md, "Zugangsdaten").
package credentials

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"

	"github.com/Zenit-Hosting-de/devbox/internal/jsonfile"
	"github.com/Zenit-Hosting-de/devbox/internal/schema"
)

// FileName is the name of the credentials directory below ~/.local/share/devbox.
const FileName = "credentials.json"

// SchemaName is the schema of the file in the validator.
const SchemaName = "credentials"

// Origins of an instance.
const (
	OriginUser          = "user"
	OriginProfilePrefix = "profile:"
)

// Verification results.
const (
	VerifiedOK     = "ok"
	VerifiedFailed = "failed"
)

// Secret references the secret of an instance in the vault.
type Secret struct {
	Ref       string  `json:"ref"`
	UpdatedAt string  `json:"updatedAt,omitempty"`
	ExpiresAt *string `json:"expiresAt,omitempty"`
}

// Verified is the result of the last verification.
type Verified struct {
	At      string `json:"at"`
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
}

// Stored is one instance as written to credentials.json, fields in schema order.
// For an instance a profile prepares, Fields and Consumers hold only what the user
// added; the profile part is merged in when the file is read (Merge).
type Stored struct {
	Type      string                    `json:"type"`
	Origin    string                    `json:"origin"`
	Label     string                    `json:"label,omitempty"`
	Fields    map[string]any            `json:"fields,omitempty"`
	Consumers map[string]map[string]any `json:"consumers,omitempty"`
	Secret    *Secret                   `json:"secret,omitempty"`
	Verified  *Verified                 `json:"verified,omitempty"`
	Applied   map[string]string         `json:"applied,omitempty"`
}

// File is the credentials directory.
type File struct {
	Schema    string             `json:"$schema,omitempty"`
	Comment   string             `json:"$comment,omitempty"`
	Instances map[string]*Stored `json:"instances"`
}

// Prepared is an instance a profile prepares (schema/profile.v1.json, credential).
type Prepared struct {
	Name      string
	Profile   string // profile ID
	Type      string
	Label     string
	Fields    map[string]any
	Locked    []string
	Consumers map[string]map[string]any
}

// Origin is profile:<id>.
func (p *Prepared) Origin() string { return OriginProfilePrefix + p.Profile }

// Instance is the effective state of one instance: profile and user parts merged.
type Instance struct {
	Name      string                    `json:"name"`
	Type      string                    `json:"type"`
	Origin    string                    `json:"origin"`
	Label     string                    `json:"label,omitempty"`
	Fields    map[string]any            `json:"fields"`
	Locked    []string                  `json:"locked"`
	Consumers map[string]map[string]any `json:"consumers"`
	Secret    *Secret                   `json:"secret,omitempty"`
	Verified  *Verified                 `json:"verified,omitempty"`
	Applied   map[string]string         `json:"applied"`

	// Prepared is the profile part; nil for instances of the user.
	Prepared *Prepared `json:"-"`
}

// IsLocked reports whether a profile locks the field.
func (i *Instance) IsLocked(field string) bool {
	for _, l := range i.Locked {
		if l == field {
			return true
		}
	}
	return false
}

// Load reads and strictly validates the file; a missing file is an empty directory.
func Load(v *schema.Validator, path string) (*File, error) {
	f := &File{Instances: map[string]*Stored{}}
	data, err := os.ReadFile(path) //nolint:gosec // path below the user's data directory
	if errors.Is(err, fs.ErrNotExist) {
		return f, nil
	}
	if err != nil {
		return nil, fmt.Errorf("credentials: %w", err)
	}
	doc, err := schema.Decode(data)
	if err != nil {
		return nil, fmt.Errorf("credentials: %s: %w", path, err)
	}
	if err := v.Validate(SchemaName, doc); err != nil {
		return nil, fmt.Errorf("credentials: %s: %w", path, err)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	if err := dec.Decode(f); err != nil {
		return nil, fmt.Errorf("credentials: %s: %w", path, err)
	}
	if f.Instances == nil {
		f.Instances = map[string]*Stored{}
	}
	return f, nil
}

// Marshal encodes the file: two-space indentation, keys in schema order (maps sorted),
// trailing newline.
func Marshal(f *File) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(f); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// Save validates the file and writes it atomically with mode 0600.
func Save(v *schema.Validator, path string, f *File) error {
	if f.Schema == "" {
		if id, ok := v.ID(SchemaName); ok {
			f.Schema = id
		}
	}
	data, err := Marshal(f)
	if err != nil {
		return err
	}
	doc, err := schema.Decode(data)
	if err != nil {
		return err
	}
	if err := v.Validate(SchemaName, doc); err != nil {
		return fmt.Errorf("credentials: %w", err)
	}
	return jsonfile.WriteAtomic(path, data, 0o600)
}

// Update reads the file, applies change and writes the result, holding a lock so
// that concurrent DevBox processes do not lose each other's changes.
func Update(v *schema.Validator, path string, change func(*File) error) error {
	unlock, err := lock(path)
	if err != nil {
		return err
	}
	defer unlock()
	f, err := Load(v, path)
	if err != nil {
		return err
	}
	if err := change(f); err != nil {
		return err
	}
	return Save(v, path, f)
}

func lock(path string) (func(), error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("credentials: %w", err)
	}
	lf, err := os.OpenFile(filepath.Join(dir, "."+filepath.Base(path)+".lock"), os.O_RDWR|os.O_CREATE, 0o600) //nolint:gosec // lock file next to the credentials file
	if err != nil {
		return nil, fmt.Errorf("credentials: %w", err)
	}
	if err := syscall.Flock(int(lf.Fd()), syscall.LOCK_EX); err != nil { //nolint:gosec // file descriptors fit int
		_ = lf.Close()
		return nil, fmt.Errorf("credentials: lock: %w", err)
	}
	return func() {
		_ = syscall.Flock(int(lf.Fd()), syscall.LOCK_UN) //nolint:gosec // file descriptors fit int
		_ = lf.Close()
	}, nil
}

// FromProfile returns the instances a profile document prepares, sorted by name.
func FromProfile(doc map[string]any) []Prepared {
	id, _ := doc["id"].(string)
	creds, _ := doc["credentials"].(map[string]any)
	out := make([]Prepared, 0, len(creds))
	for _, name := range sortedKeys(creds) {
		c, _ := creds[name].(map[string]any)
		p := Prepared{Name: name, Profile: id, Fields: map[string]any{}, Consumers: map[string]map[string]any{}}
		p.Type, _ = c["type"].(string)
		p.Label, _ = c["label"].(string)
		if fields, ok := c["fields"].(map[string]any); ok {
			p.Fields = copyMap(fields)
		}
		for _, l := range anyList(c["locked"]) {
			if s, ok := l.(string); ok {
				p.Locked = append(p.Locked, s)
			}
		}
		consumers, _ := c["consumers"].(map[string]any)
		for plugin, opts := range consumers {
			o, _ := opts.(map[string]any)
			p.Consumers[plugin] = copyMap(o)
		}
		out = append(out, p)
	}
	return out
}

// Merge combines the stored instances with the prepared ones (in profile order; a
// later profile replaces an earlier one with the same instance name) and returns the
// effective instances sorted by name.
//
// For a prepared instance the profile decides type and origin; fields are the
// profile's, overridden by the user's except for locked ones; consumers are the
// profile's plus the user's (for the same plugin the user's options win key by key).
func Merge(f *File, prepared []Prepared) []Instance {
	byName := map[string]*Prepared{}
	for i := range prepared {
		byName[prepared[i].Name] = &prepared[i]
	}
	names := map[string]bool{}
	for n := range byName {
		names[n] = true
	}
	for n := range f.Instances {
		names[n] = true
	}
	out := make([]Instance, 0, len(names))
	for _, name := range sortedKeys(names) {
		out = append(out, merge(name, byName[name], f.Instances[name]))
	}
	return out
}

func merge(name string, p *Prepared, s *Stored) Instance {
	inst := Instance{
		Name: name, Fields: map[string]any{}, Locked: []string{},
		Consumers: map[string]map[string]any{}, Applied: map[string]string{},
	}
	if p != nil {
		inst.Prepared = p
		inst.Type, inst.Origin, inst.Label = p.Type, p.Origin(), p.Label
		inst.Fields = copyMap(p.Fields)
		inst.Locked = append(inst.Locked, p.Locked...)
		for plugin, opts := range p.Consumers {
			inst.Consumers[plugin] = copyMap(opts)
		}
	}
	if s == nil {
		return inst
	}
	if p == nil {
		inst.Type, inst.Origin = s.Type, s.Origin
	}
	if s.Label != "" {
		inst.Label = s.Label
	}
	for k, v := range s.Fields {
		if !inst.IsLocked(k) {
			inst.Fields[k] = v
		}
	}
	for plugin, opts := range s.Consumers {
		merged := inst.Consumers[plugin]
		if merged == nil {
			merged = map[string]any{}
		}
		for k, v := range opts {
			merged[k] = v
		}
		inst.Consumers[plugin] = merged
	}
	inst.Secret, inst.Verified = s.Secret, s.Verified
	for k, v := range s.Applied {
		inst.Applied[k] = v
	}
	return inst
}

// Find returns the instance with the name.
func Find(list []Instance, name string) (*Instance, bool) {
	for i := range list {
		if list[i].Name == name {
			return &list[i], true
		}
	}
	return nil, false
}

// Entry returns the stored entry of an instance, creating it for an instance that
// exists only in a profile (or not at all, then as an instance of the user).
func (f *File) Entry(name, typ, origin string) *Stored {
	if s, ok := f.Instances[name]; ok {
		return s
	}
	s := &Stored{Type: typ, Origin: origin}
	f.Instances[name] = s
	return s
}

// Forget removes what the user added to an instance. Instances of the user disappear;
// for an instance a profile prepares, the fields the user set are kept (they are not
// part of the secret) and secret, consumers, verification and applied times go. An
// entry with nothing left is removed.
func (f *File) Forget(name string, prepared bool) {
	s, ok := f.Instances[name]
	if !ok {
		return
	}
	if !prepared || !strings.HasPrefix(s.Origin, OriginProfilePrefix) {
		delete(f.Instances, name)
		return
	}
	s.Consumers, s.Secret, s.Verified, s.Applied = nil, nil, nil, nil
	if len(s.Fields) == 0 && s.Label == "" {
		delete(f.Instances, name)
	}
}

func copyMap(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func anyList(v any) []any {
	l, _ := v.([]any)
	return l
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
