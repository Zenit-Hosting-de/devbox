package jsonfile

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// Span is the byte range [Start, End) of a value in a JSON document.
type Span struct {
	Start, End int
	// Container is '{' or '[' for objects and arrays, 0 otherwise.
	Container byte
}

// Spans maps the JSON Pointer of every value in data to its byte range.
func Spans(data []byte) (map[string]Span, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	spans := map[string]Span{}
	type open struct {
		ptr   string
		start int
		array bool
		index int
		key   string
		ready bool
	}
	var stack []open
	childPtr := func() string {
		top := stack[len(stack)-1]
		if top.array {
			return top.ptr + "/" + strconv.Itoa(top.index)
		}
		return top.ptr + "/" + escapeToken(top.key)
	}
	afterValue := func() {
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
	for {
		before := dec.InputOffset()
		tok, err := dec.Token()
		if err != nil {
			if len(stack) == 0 && len(spans) > 0 {
				return spans, nil
			}
			return nil, fmt.Errorf("parse JSON: %w", err)
		}
		if d, ok := tok.(json.Delim); ok && (d == '}' || d == ']') {
			top := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			spans[top.ptr] = Span{Start: top.start, End: int(dec.InputOffset()), Container: map[bool]byte{true: '[', false: '{'}[top.array]}
			afterValue()
			continue
		}
		if len(stack) > 0 && !stack[len(stack)-1].array && !stack[len(stack)-1].ready {
			key, _ := tok.(string)
			stack[len(stack)-1].key = key
			stack[len(stack)-1].ready = true
			continue
		}
		ptr := ""
		if len(stack) > 0 {
			ptr = childPtr()
		}
		start := valueStart(data, before)
		if d, ok := tok.(json.Delim); ok {
			stack = append(stack, open{ptr: ptr, start: start, array: d == '['})
			continue
		}
		spans[ptr] = Span{Start: start, End: int(dec.InputOffset())}
		afterValue()
	}
}

func escapeToken(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "~", "~0"), "/", "~1")
}

// SetPointer sets the value at a JSON Pointer and keeps the rest of the document
// byte for byte: an existing value is replaced in place, a missing key is added to
// its parent object with the indentation used there. Missing parent objects are
// created. Array elements can be replaced but not added.
func SetPointer(data []byte, pointer string, value any) ([]byte, error) {
	spans, err := Spans(data)
	if err != nil {
		return nil, err
	}
	if s, ok := spans[pointer]; ok {
		text, err := encodeValue(value, indentAt(data, s.Start), indentUnit(data))
		if err != nil {
			return nil, err
		}
		return splice(data, s.Start, s.End, text), nil
	}
	tokens := splitPointer(pointer)
	if len(tokens) == 0 {
		return nil, fmt.Errorf("pointer %q: no value to replace", pointer)
	}
	// Find the deepest existing ancestor and build the missing part as nested objects.
	depth := len(tokens) - 1
	for depth > 0 {
		if _, ok := spans[joinPointer(tokens[:depth])]; ok {
			break
		}
		depth--
	}
	parent := joinPointer(tokens[:depth])
	ps := spans[parent]
	if ps.Container != '{' {
		return nil, fmt.Errorf("pointer %q: %q is not an object", pointer, parent)
	}
	nested := value
	for i := len(tokens) - 1; i > depth; i-- {
		obj := NewObject()
		obj.Set(tokens[i], nested)
		nested = obj
	}
	key := tokens[depth]
	unit := indentUnit(data)
	outer := indentAt(data, ps.Start)
	inner := outer + unit
	text, err := encodeValue(nested, inner, unit)
	if err != nil {
		return nil, err
	}
	var kb bytes.Buffer
	writeString(&kb, key)
	member := kb.String() + ": " + string(text)

	closing := ps.End - 1 // the '}'
	if hasChildren(spans, parent) {
		// after the last member: skip back over white space before '}'
		at := closing
		for at > ps.Start && isSpace(data[at-1]) {
			at--
		}
		return splice(data, at, at, []byte(",\n"+inner+member)), nil
	}
	return splice(data, ps.Start+1, closing, []byte("\n"+inner+member+"\n"+outer)), nil
}

func hasChildren(spans map[string]Span, parent string) bool {
	prefix := parent + "/"
	for p := range spans {
		if strings.HasPrefix(p, prefix) && !strings.Contains(p[len(prefix):], "/") {
			return true
		}
	}
	return false
}

func joinPointer(tokens []string) string {
	var b strings.Builder
	for _, t := range tokens {
		b.WriteString("/" + escapeToken(t))
	}
	return b.String()
}

func splice(data []byte, start, end int, text []byte) []byte {
	out := make([]byte, 0, len(data)-(end-start)+len(text))
	out = append(out, data[:start]...)
	out = append(out, text...)
	return append(out, data[end:]...)
}

func isSpace(b byte) bool { return b == ' ' || b == '\t' || b == '\n' || b == '\r' }

// indentAt returns the leading white space of the line that contains offset.
func indentAt(data []byte, offset int) string {
	lineStart := bytes.LastIndexByte(data[:offset], '\n') + 1
	end := lineStart
	for end < len(data) && (data[end] == ' ' || data[end] == '\t') {
		end++
	}
	return string(data[lineStart:end])
}

// indentUnit guesses the indentation step of a document (two spaces by default).
func indentUnit(data []byte) string {
	for _, line := range bytes.Split(data, []byte("\n")) {
		trimmed := bytes.TrimLeft(line, " \t")
		if len(trimmed) > 0 && len(trimmed) < len(line) {
			return string(line[:len(line)-len(trimmed)])
		}
	}
	return "  "
}

// encodeValue writes a value; objects and arrays are indented relative to base.
func encodeValue(v any, base, unit string) ([]byte, error) {
	var buf bytes.Buffer
	if err := write(&buf, v, ""); err != nil {
		return nil, err
	}
	text := buf.Bytes()
	if unit != "  " {
		lines := bytes.Split(text, []byte("\n"))
		for i, l := range lines {
			trimmed := bytes.TrimLeft(l, " ")
			level := (len(l) - len(trimmed)) / 2
			lines[i] = append([]byte(strings.Repeat(unit, level)), trimmed...)
		}
		text = bytes.Join(lines, []byte("\n"))
	}
	return bytes.ReplaceAll(text, []byte("\n"), []byte("\n"+base)), nil
}
