package config

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/Zenit-Hosting-de/devbox/internal/jsonfile"
)

// EnvPrefix starts environment variables that set a setting:
// DEVBOX_SET__<PLUGIN>__<NAME>, e.g. DEVBOX_SET__NODE__CHANNEL=lts.
const EnvPrefix = "DEVBOX_SET__"

// Coerce converts a value given as text (environment, command line) to the type of
// the field. Values that are not text are returned unchanged. Array fields also accept
// plain text (see splitList) besides a JSON array.
func Coerce(field map[string]any, v any) (any, error) {
	s, ok := v.(string)
	if !ok {
		return v, nil
	}
	typ, _ := field["type"].(string)
	switch typ {
	case "integer":
		if _, err := strconv.ParseInt(s, 10, 64); err != nil {
			return nil, fmt.Errorf("%q is not an integer", s)
		}
		return json.Number(s), nil
	case "number":
		if _, err := strconv.ParseFloat(s, 64); err != nil {
			return nil, fmt.Errorf("%q is not a number", s)
		}
		return json.Number(s), nil
	case "boolean":
		b, err := strconv.ParseBool(s)
		if err != nil {
			return nil, fmt.Errorf("%q is not a boolean", s)
		}
		return b, nil
	case "array", "object":
		if typ == "array" && !strings.HasPrefix(strings.TrimSpace(s), "[") {
			return splitList(s), nil
		}
		doc, err := jsonfile.Parse([]byte(s))
		if err != nil {
			return nil, fmt.Errorf("%q is not a JSON %s: %w", s, typ, err)
		}
		return jsonfile.Plain(doc), nil
	default:
		return s, nil
	}
}

// splitList turns plain text into a list: one value is a one-element list, several
// values are separated by ";". Empty parts are dropped. JSON arrays do not come here.
func splitList(s string) []any {
	out := []any{}
	for _, part := range strings.Split(s, ";") {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}

// EnvLayer collects DEVBOX_SET__ variables from environ ("NAME=value" entries).
// Plugin IDs may contain "-", written "_" in the variable name; names are matched
// without regard to case against the known keys. Empty variables count as not set.
func EnvLayer(environ []string, defs Definitions) (Layer, error) {
	byEnvName := map[string]string{}
	for key := range defs {
		byEnvName[envName(key)] = key
	}
	l := Layer{Name: LayerEnv, Values: map[string]any{}}
	for _, kv := range environ {
		name, value, ok := strings.Cut(kv, "=")
		if !ok || !strings.HasPrefix(name, EnvPrefix) || value == "" {
			continue // an empty variable counts as not set
		}
		key, ok := byEnvName[strings.ToUpper(name)]
		if !ok {
			return l, &Error{Code: CodeNotFound, Detail: "no plugin defines the setting of " + name}
		}
		l.Values[key] = value
	}
	return l, nil
}

func envName(key string) string {
	plugin, name, _ := strings.Cut(key, ".")
	return EnvPrefix + strings.ToUpper(strings.ReplaceAll(plugin, "-", "_")) + "__" + strings.ToUpper(name)
}

// CallLayer parses --set key=value arguments.
func CallLayer(args []string) (Layer, error) {
	l := Layer{Name: LayerCall, Values: map[string]any{}}
	for _, a := range args {
		key, value, ok := strings.Cut(a, "=")
		if !ok || !ValidKey(key) {
			return l, &Error{Code: CodeValidation, Detail: fmt.Sprintf("--set %q: want <plugin>.<name>=<value>", a)}
		}
		l.Values[key] = value
	}
	return l, nil
}

// Explanation is the origin of one setting, as returned by GET /settings/{key}/explain
// and `devbox config explain --json` (api/openapi.json, SettingExplain).
type Explanation struct {
	Key    string       `json:"key"`
	Value  any          `json:"value"`
	Origin string       `json:"origin"`
	Locked bool         `json:"locked"`
	Layers []LayerValue `json:"layers"`
}

// Explain returns the explanation of one setting.
func (r *Result) Explain(key string) (*Explanation, error) {
	s, ok := r.settings[key]
	if !ok {
		return nil, &Error{Code: CodeNotFound, Key: key, Detail: "no plugin defines this setting"}
	}
	layers := s.Layers
	if layers == nil {
		layers = []LayerValue{}
	}
	return &Explanation{Key: s.Key, Value: s.Value, Origin: s.Origin, Locked: s.Locked, Layers: layers}, nil
}
