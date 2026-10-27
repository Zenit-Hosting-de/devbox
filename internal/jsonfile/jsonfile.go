// Package jsonfile reads and writes the JSON files of DevBox the way the concept
// requires: objects keep their key order, output uses two-space indentation and a
// trailing newline, and writes are atomic (temporary file, fsync, rename).
package jsonfile

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Object is a JSON object that remembers the order of its keys.
type Object struct {
	keys   []string
	values map[string]any
}

// NewObject returns an empty object.
func NewObject() *Object { return &Object{values: map[string]any{}} }

// Keys returns the keys in order.
func (o *Object) Keys() []string { return append([]string(nil), o.keys...) }

// Get returns the value of a key.
func (o *Object) Get(key string) (any, bool) {
	v, ok := o.values[key]
	return v, ok
}

// Set sets a value; a new key is appended.
func (o *Object) Set(key string, v any) {
	if _, ok := o.values[key]; !ok {
		o.keys = append(o.keys, key)
	}
	o.values[key] = v
}

// Delete removes a key.
func (o *Object) Delete(key string) {
	if _, ok := o.values[key]; !ok {
		return
	}
	delete(o.values, key)
	for i, k := range o.keys {
		if k == key {
			o.keys = append(o.keys[:i], o.keys[i+1:]...)
			return
		}
	}
}

// Len returns the number of keys.
func (o *Object) Len() int { return len(o.keys) }

// Reorder moves the listed keys to the front, in the given order; other keys keep
// their relative order after them.
func (o *Object) Reorder(order []string) {
	rank := map[string]int{}
	for i, k := range order {
		rank[k] = i
	}
	var known, rest []string
	for _, k := range o.keys {
		if _, ok := rank[k]; ok {
			known = append(known, k)
		} else {
			rest = append(rest, k)
		}
	}
	sortByRank(known, rank)
	o.keys = append(known, rest...)
}

func sortByRank(keys []string, rank map[string]int) {
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && rank[keys[j]] < rank[keys[j-1]]; j-- {
			keys[j], keys[j-1] = keys[j-1], keys[j]
		}
	}
}

// Parse decodes one JSON document. Objects become *Object, arrays []any, numbers
// json.Number. Trailing data after the document is an error.
func Parse(data []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	v, err := parseValue(dec)
	if err != nil {
		return nil, err
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, errors.New("unexpected data after the JSON document")
	}
	return v, nil
}

func parseValue(dec *json.Decoder) (any, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	switch t := tok.(type) {
	case json.Delim:
		switch t {
		case '{':
			obj := NewObject()
			for dec.More() {
				kt, err := dec.Token()
				if err != nil {
					return nil, err
				}
				key, _ := kt.(string)
				if _, dup := obj.values[key]; dup {
					return nil, fmt.Errorf("duplicate key %q", key)
				}
				v, err := parseValue(dec)
				if err != nil {
					return nil, err
				}
				obj.Set(key, v)
			}
			_, err := dec.Token()
			return obj, err
		case '[':
			arr := []any{}
			for dec.More() {
				v, err := parseValue(dec)
				if err != nil {
					return nil, err
				}
				arr = append(arr, v)
			}
			_, err := dec.Token()
			return arr, err
		}
		return nil, fmt.Errorf("unexpected delimiter %v", t)
	default:
		return t, nil
	}
}

// Plain converts *Object values (recursively) to map[string]any, for validation
// and for code that does not care about key order.
func Plain(v any) any {
	switch t := v.(type) {
	case *Object:
		m := make(map[string]any, len(t.keys))
		for _, k := range t.keys {
			m[k] = Plain(t.values[k])
		}
		return m
	case []any:
		out := make([]any, len(t))
		for i, e := range t {
			out[i] = Plain(e)
		}
		return out
	default:
		return v
	}
}

// Marshal encodes v with two-space indentation and a trailing newline. *Object keeps
// its key order; map[string]any is written with sorted keys.
func Marshal(v any) ([]byte, error) {
	var buf bytes.Buffer
	if err := write(&buf, v, ""); err != nil {
		return nil, err
	}
	buf.WriteByte('\n')
	return buf.Bytes(), nil
}

func write(buf *bytes.Buffer, v any, indent string) error {
	inner := indent + "  "
	switch t := v.(type) {
	case *Object:
		if t.Len() == 0 {
			buf.WriteString("{}")
			return nil
		}
		buf.WriteString("{\n")
		for i, k := range t.keys {
			buf.WriteString(inner)
			writeString(buf, k)
			buf.WriteString(": ")
			if err := write(buf, t.values[k], inner); err != nil {
				return err
			}
			if i < len(t.keys)-1 {
				buf.WriteByte(',')
			}
			buf.WriteByte('\n')
		}
		buf.WriteString(indent + "}")
		return nil
	case map[string]any:
		obj := NewObject()
		for _, k := range sortedKeys(t) {
			obj.Set(k, t[k])
		}
		return write(buf, obj, indent)
	case []any:
		if len(t) == 0 {
			buf.WriteString("[]")
			return nil
		}
		buf.WriteString("[\n")
		for i, e := range t {
			buf.WriteString(inner)
			if err := write(buf, e, inner); err != nil {
				return err
			}
			if i < len(t)-1 {
				buf.WriteByte(',')
			}
			buf.WriteByte('\n')
		}
		buf.WriteString(indent + "]")
		return nil
	case string:
		writeString(buf, t)
		return nil
	default:
		b, err := json.Marshal(t)
		if err != nil {
			return err
		}
		buf.Write(b)
		return nil
	}
}

func writeString(buf *bytes.Buffer, s string) {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(s) // encoding a string cannot fail
	buf.Write(bytes.TrimSuffix(b.Bytes(), []byte("\n")))
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && keys[j] < keys[j-1]; j-- {
			keys[j], keys[j-1] = keys[j-1], keys[j]
		}
	}
	return keys
}

// WriteAtomic writes data to path through a temporary file in the same directory,
// fsync and rename, so readers never see a partial file. The directory is created
// with mode 0700 if needed.
func WriteAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary file: %w", err)
	}
	name := tmp.Name()
	cleanup := func() { _ = os.Remove(name) }
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("write %s: %w", name, err)
	}
	if err := tmp.Chmod(perm); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("chmod %s: %w", name, err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("sync %s: %w", name, err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return fmt.Errorf("close %s: %w", name, err)
	}
	if err := os.Rename(name, path); err != nil {
		cleanup()
		return fmt.Errorf("rename to %s: %w", path, err)
	}
	if d, err := os.Open(dir); err == nil { //nolint:gosec // directory of the file just written
		_ = d.Sync()
		_ = d.Close()
	}
	return nil
}

// Line returns the 1-based line of the value at a JSON Pointer in data, or of the
// closest existing ancestor when the pointer does not exist (e.g. a missing key).
func Line(data []byte, pointer string) int {
	target := splitPointer(pointer)
	dec := json.NewDecoder(bytes.NewReader(data))
	var stack []frame
	path := func() []string {
		p := make([]string, 0, len(stack))
		for _, f := range stack {
			if f.array {
				p = append(p, strconv.Itoa(f.index))
			} else {
				p = append(p, f.key)
			}
		}
		return p
	}
	best := 1
	matches := func(p []string) int {
		n := 0
		for n < len(p) && n < len(target) && p[n] == target[n] {
			n++
		}
		if n == len(p) {
			return n
		}
		return -1
	}
	bestDepth := -1
	for {
		offset := dec.InputOffset()
		tok, err := dec.Token()
		if err != nil {
			return best
		}
		if d, ok := tok.(json.Delim); ok && (d == '}' || d == ']') {
			stack = stack[:len(stack)-1]
			advance(stack)
			continue
		}
		isValue := true
		if len(stack) > 0 && !stack[len(stack)-1].array && !stack[len(stack)-1].ready {
			// object key
			key, _ := tok.(string)
			stack[len(stack)-1].key = key
			stack[len(stack)-1].ready = true
			isValue = false
		}
		if isValue {
			p := path()
			if n := matches(p); n > bestDepth {
				bestDepth = n
				best = lineAt(data, valueStart(data, offset))
				if n == len(target) {
					return best
				}
			}
			if d, ok := tok.(json.Delim); ok {
				stack = append(stack, frame{array: d == '['})
				continue
			}
			advance(stack)
		}
	}
}

// frame is one open object or array while scanning for a pointer.
type frame struct {
	array bool
	index int
	key   string
	ready bool // for objects: the next token is the value of key
}

func advance(stack []frame) {
	if len(stack) == 0 {
		return
	}
	top := &stack[len(stack)-1]
	if top.array {
		top.index++
	} else {
		top.ready = false
	}
}

// valueStart skips separators and white space between the decoder offset and a value.
func valueStart(data []byte, offset int64) int {
	i := int(offset)
	for i < len(data) && strings.ContainsRune(" \t\r\n,:", rune(data[i])) {
		i++
	}
	return i
}

func lineAt(data []byte, offset int) int {
	if offset > len(data) {
		offset = len(data)
	}
	return bytes.Count(data[:offset], []byte("\n")) + 1
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
