package core

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/Zenit-Hosting-de/devbox/internal/credentials"
	"github.com/Zenit-Hosting-de/devbox/internal/plugin"
)

// SettingItem is a setting with its definition, effective value and lock
// (api/openapi.json, SettingItem).
type SettingItem struct {
	Key    string         `json:"key"`
	Plugin string         `json:"plugin"`
	Field  map[string]any `json:"field"`
	// Value is absent when the setting has neither a value nor a default.
	Value    any    `json:"value,omitempty"`
	Origin   string `json:"origin,omitempty"`
	Locked   bool   `json:"locked"`
	LockedBy string `json:"lockedBy,omitempty"`
}

// Settings lists the settings of all found plugins, sorted by key.
func Settings(st *State) []SettingItem {
	items := make([]SettingItem, 0, len(st.Defs))
	for _, key := range st.Result.Keys() {
		field, ok := st.Defs[key]
		if !ok {
			continue
		}
		pluginID, _, _ := strings.Cut(key, ".")
		item := SettingItem{Key: key, Plugin: pluginID, Field: field}
		if s, ok := st.Result.Get(key); ok {
			if s.Value != nil {
				item.Value, item.Origin = s.Value, s.Origin
			}
			if by := st.Result.LockedBy(key); by != "" {
				item.Locked, item.LockedBy = true, by
			}
		}
		items = append(items, item)
	}
	return items
}

// CredentialsPath is the credentials directory file.
func (st *State) CredentialsPath() string {
	return filepath.Join(st.DataDir(), credentials.FileName)
}

// PreparedCredentials returns the instances the profiles prepare.
func (st *State) PreparedCredentials() []credentials.Prepared {
	var out []credentials.Prepared
	for _, pf := range st.Profiles {
		out = append(out, credentials.FromProfile(pf.Plain())...)
	}
	return out
}

// Credentials returns the credential instances, profile and user parts merged, sorted
// by name. They never contain a secret value.
func Credentials(st *State) ([]credentials.Instance, error) {
	f, err := credentials.Load(st.Validator, st.CredentialsPath())
	if err != nil {
		return nil, err
	}
	return credentials.Merge(f, st.PreparedCredentials()), nil
}

var langPattern = regexp.MustCompile(`^[a-z]{2,3}(-[A-Z]{2})?$`)

// Messages reads the translations of a plugin for one language from
// <plugin dir>/<i18n.dir or "i18n">/<lang>.json, validated against the messages
// schema. $schema and $comment are dropped.
func (st *State) Messages(m *plugin.Manifest, lang string) (map[string]string, error) {
	if !langPattern.MatchString(lang) {
		return nil, ErrInvalid("lang", "not a language code")
	}
	sub := "i18n"
	if i18n, ok := m.Doc["i18n"].(map[string]any); ok {
		if d, ok := i18n["dir"].(string); ok && d != "" {
			sub = d
		}
	}
	if !filepath.IsLocal(sub) {
		return nil, ErrInvalid(m.ID, "i18n.dir leaves the plugin directory")
	}
	path := filepath.Join(m.Dir, sub, lang+".json")
	data, err := os.ReadFile(path) //nolint:gosec // below the plugin directory, name checked above
	if errors.Is(err, fs.ErrNotExist) {
		return nil, ErrNotFound(m.ID, "no translations for "+lang)
	}
	if err != nil {
		return nil, fmt.Errorf("messages: %w", err)
	}
	if err := st.Validator.ValidateJSON("messages", data); err != nil {
		return nil, fmt.Errorf("messages: %s: %w", path, err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("messages: %s: %w", path, err)
	}
	out := make(map[string]string, len(doc))
	for k, v := range doc {
		if k == "$schema" || k == "$comment" {
			continue
		}
		s, _ := v.(string)
		out[k] = s
	}
	return out, nil
}
