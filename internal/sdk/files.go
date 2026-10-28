package sdk

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Zenit-Hosting-de/devbox/internal/jsonfile"
)

// Comment syntax of the block markers.
type Comment struct{ Start, End string }

// CommentFor picks the comment syntax by file name: XML files use <!-- -->,
// everything else "#".
func CommentFor(path string) Comment {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".xml", ".config", ".props", ".targets", ".csproj":
		return Comment{Start: "<!--", End: "-->"}
	}
	return Comment{Start: "#"}
}

// CommentStyle picks the comment syntax by name: "xml" for <!-- -->, "hash" for "#"
// and "" or "auto" by the file name (see CommentFor).
func CommentStyle(style, path string) (Comment, error) {
	switch style {
	case "", "auto":
		return CommentFor(path), nil
	case "xml":
		return Comment{Start: "<!--", End: "-->"}, nil
	case "hash":
		return Comment{Start: "#"}, nil
	}
	return Comment{}, fmt.Errorf("unknown comment style %q (use auto, xml or hash)", style)
}

// outsideComments reports for every line whether it starts outside an XML comment.
// It is only meaningful for the XML comment syntax.
func outsideComments(lines []string) []bool {
	out := make([]bool, len(lines))
	in := false
	for i, l := range lines {
		out[i] = !in
		for rest := l; ; {
			if in {
				k := strings.Index(rest, "-->")
				if k < 0 {
					break
				}
				rest, in = rest[k+3:], false
			} else {
				k := strings.Index(rest, "<!--")
				if k < 0 {
					break
				}
				rest, in = rest[k+4:], true
			}
		}
	}
	return out
}

// findLine returns the index of the first line that equals text (ignoring white
// space), or -1. With the XML comment syntax, lines inside a comment do not count.
func findLine(lines []string, text string, c Comment) int {
	var outside []bool
	if c.End == "-->" {
		outside = outsideComments(lines)
	}
	for i, l := range lines {
		if outside != nil && !outside[i] {
			continue
		}
		if strings.TrimSpace(l) == text {
			return i
		}
	}
	return -1
}

// HasLine reports whether content has a line that equals text (ignoring white
// space); with the XML comment syntax, lines inside a comment do not count.
func HasLine(content []byte, text string, c Comment) bool {
	return findLine(strings.SplitAfter(string(content), "\n"), text, c) >= 0
}

func (c Comment) line(text string) string {
	if c.End == "" {
		return c.Start + " " + text
	}
	return c.Start + " " + text + " " + c.End
}

// Markers returns the first and last line of a block.
func Markers(name string, c Comment) (begin, end string) {
	return c.line(">>> devbox:" + name + " >>>"), c.line("<<< devbox:" + name + " <<<")
}

// SetBlock returns content with the marked block for name replaced by body, or
// appended (after a blank line) when there is none. Running it twice gives the same
// result.
func SetBlock(content []byte, name string, body []byte, c Comment) []byte {
	out, _ := SetBlockBefore(content, name, body, c, "")
	return out
}

// SetBlockBefore is SetBlock with an insertion point: when the block is new and
// before is not empty, the block is inserted directly before the first line that
// equals before (ignoring surrounding white space; with XML comments, lines inside a
// comment do not count) instead of being appended. The
// marker lines are indented one level deeper than that line; an existing block keeps
// the indentation of its marker. It is an error when before is not in the content.
func SetBlockBefore(content []byte, name string, body []byte, c Comment, before string) ([]byte, error) {
	begin, end := Markers(name, c)
	lines, from, to := findBlock(string(content), begin, end)
	indent := ""
	if from >= 0 {
		indent = leadingSpace(lines[from])
	}
	if from < 0 && before != "" {
		at := findLine(lines, before, c)
		if at < 0 {
			return nil, fmt.Errorf("no line %q to insert the block %s before", before, name)
		}
		indent = leadingSpace(lines[at]) + "  "
		return []byte(strings.Join(lines[:at], "") + renderBlock(begin, end, body, indent) + strings.Join(lines[at:], "")), nil
	}
	block := renderBlock(begin, end, body, indent)
	if from >= 0 {
		return []byte(strings.Join(lines[:from], "") + block + strings.Join(lines[to+1:], "")), nil
	}
	text := string(content)
	switch {
	case text == "":
	case strings.HasSuffix(text, "\n\n"):
	case strings.HasSuffix(text, "\n"):
		text += "\n"
	default:
		text += "\n\n"
	}
	return []byte(text + block), nil
}

func leadingSpace(s string) string { return s[:len(s)-len(strings.TrimLeft(s, " \t"))] }

func renderBlock(begin, end string, body []byte, indent string) string {
	return indent + begin + "\n" + strings.TrimRight(string(body), "\n") + "\n" + indent + end + "\n"
}

// RemoveBlock returns content without the marked block (and one blank line before it,
// unless the block is indented: those sit inside a structure, not after a blank line).
func RemoveBlock(content []byte, name string, c Comment) []byte {
	begin, end := Markers(name, c)
	lines, from, to := findBlock(string(content), begin, end)
	if from < 0 {
		return content
	}
	if from > 0 && leadingSpace(lines[from]) == "" && strings.TrimSpace(lines[from-1]) == "" {
		from--
	}
	return []byte(strings.Join(lines[:from], "") + strings.Join(lines[to+1:], ""))
}

// findBlock splits text into lines (with their newlines) and finds the marker lines.
func findBlock(text, begin, end string) (lines []string, from, to int) {
	lines = strings.SplitAfter(text, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	from, to = -1, -1
	for i, l := range lines {
		t := strings.TrimSpace(l)
		if from < 0 && t == begin {
			from = i
		} else if from >= 0 && t == end {
			to = i
			break
		}
	}
	if to < 0 {
		return lines, -1, -1
	}
	return lines, from, to
}

func readOptional(path string) ([]byte, error) {
	data, err := os.ReadFile(path) //nolint:gosec // paths come from the plugin, which declares them
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	return data, err
}

// Block writes the marked block for name into the file.
func (e Env) Block(path, name string, body []byte, mode os.FileMode) error {
	return e.BlockBefore(path, name, body, mode, "")
}

// BlockBefore is Block for a new block that goes before the line "before" (see
// SetBlockBefore) instead of at the end of the file, e.g. inside an XML section.
func (e Env) BlockBefore(path, name string, body []byte, mode os.FileMode, before string) error {
	return e.BlockAs(path, name, body, mode, before, "")
}

// BlockAs is BlockBefore with an explicit comment style (see CommentStyle).
func (e Env) BlockAs(path, name string, body []byte, mode os.FileMode, before, style string) error {
	c, err := CommentStyle(style, path)
	if err != nil {
		return err
	}
	path = e.ExpandHome(path)
	old, err := readOptional(path)
	if err != nil {
		return err
	}
	updated, err := SetBlockBefore(old, name, body, c, before)
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	_, err = e.Write(path, updated, mode, "devbox:"+name+" block")
	return err
}

// BlockRemove removes the marked block for name from the file.
func (e Env) BlockRemove(path, name string) error {
	return e.BlockRemoveAs(path, name, "")
}

// BlockRemoveAs is BlockRemove with an explicit comment style (see CommentStyle).
func (e Env) BlockRemoveAs(path, name, style string) error {
	c, err := CommentStyle(style, path)
	if err != nil {
		return err
	}
	path = e.ExpandHome(path)
	old, err := readOptional(path)
	if err != nil || old == nil {
		return err
	}
	_, err = e.Write(path, RemoveBlock(old, name, c), 0o600, "devbox:"+name+" block removed")
	return err
}

// JSONSet sets the value at a JSON Pointer in a JSON file and keeps its formatting.
// A missing file is created as an object.
func (e Env) JSONSet(path, pointer string, value any) error {
	path = e.ExpandHome(path)
	old, err := readOptional(path)
	if err != nil {
		return err
	}
	if old == nil {
		old = []byte("{}\n")
	}
	updated, err := jsonfile.SetPointer(old, pointer, value)
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	_, err = e.Write(path, updated, 0o644, pointer+" set")
	return err
}

// Download fetches url to dest through the proxy of the environment, reports
// progress, and checks the SHA-256 checksum when one is given. In a dry run only the
// change is reported.
func (e Env) Download(ctx context.Context, client *http.Client, url, dest, sha string) error {
	dest = e.ExpandHome(dest)
	if e.DryRun() {
		return e.Emit(map[string]any{"type": "change", "kind": "file", "op": "create", "target": e.display(dest), "summary": "download " + url})
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download %s: %s", url, resp.Status)
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o750); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(dest), "."+filepath.Base(dest)+".*.part")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	hash := sha256.New()
	pr := &progress{env: e, total: resp.ContentLength}
	if _, err := io.Copy(io.MultiWriter(tmp, hash, pr), resp.Body); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("download %s: %w", url, err)
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if sha != "" {
		if got := hex.EncodeToString(hash.Sum(nil)); !strings.EqualFold(got, sha) {
			return fmt.Errorf("download %s: sha256 %s, want %s", url, got, sha)
		}
	}
	if err := os.Rename(tmp.Name(), dest); err != nil {
		return err
	}
	return e.Emit(map[string]any{"type": "change", "kind": "file", "op": "create", "target": e.display(dest), "summary": "downloaded " + url})
}

// progress reports download progress in steps of 10 percent (or null without size).
type progress struct {
	env   Env
	total int64
	done  int64
	last  int
}

func (p *progress) Write(b []byte) (int, error) {
	p.done += int64(len(b))
	if p.total <= 0 {
		if p.last == 0 {
			p.last = -1
			_ = p.env.Emit(map[string]any{"type": "progress", "percent": nil})
		}
		return len(b), nil
	}
	pct := int(p.done * 100 / p.total)
	if pct >= p.last+10 || pct == 100 && p.last != 100 {
		p.last = pct
		_ = p.env.Emit(map[string]any{"type": "progress", "percent": json.Number(strconv.Itoa(pct)), "text": fmt.Sprintf("%d / %d", p.done, p.total)})
	}
	return len(b), nil
}

// Fetch reads url through the proxy of the environment and writes the body to w. It
// changes nothing and therefore also runs in a dry run (e.g. release metadata).
func (e Env) Fetch(ctx context.Context, client *http.Client, url string, w io.Writer) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("fetch %s: %s", url, resp.Status)
	}
	_, err = io.Copy(w, resp.Body)
	return err
}

// JSONGet returns the values at a JSON Pointer in a document. A "*" token selects
// every element of an array or every value of an object (in key order), like
// "optionsFrom" in manifests.
func JSONGet(data []byte, pointer string) ([]any, error) {
	doc, err := jsonfile.Parse(data)
	if err != nil {
		return nil, err
	}
	values := []any{doc}
	for _, tok := range splitPointer(pointer) {
		var next []any
		for _, v := range values {
			switch t := v.(type) {
			case *jsonfile.Object:
				if tok == "*" {
					for _, k := range t.Keys() {
						x, _ := t.Get(k)
						next = append(next, x)
					}
				} else if x, ok := t.Get(tok); ok {
					next = append(next, x)
				}
			case []any:
				if tok == "*" {
					next = append(next, t...)
				} else if i, err := strconv.Atoi(tok); err == nil && i >= 0 && i < len(t) {
					next = append(next, t[i])
				}
			}
		}
		values = next
	}
	out := make([]any, len(values))
	for i, v := range values {
		out[i] = jsonfile.Plain(v)
	}
	return out, nil
}

// JSONKeys returns the keys of the object at a JSON Pointer, in document order. ok is
// false when nothing is at the pointer.
func JSONKeys(data []byte, pointer string) (keys []string, ok bool, err error) {
	doc, err := jsonfile.Parse(data)
	if err != nil {
		return nil, false, err
	}
	v := doc
	for _, tok := range splitPointer(pointer) {
		obj, isObj := v.(*jsonfile.Object)
		if !isObj {
			return nil, false, nil
		}
		if v, ok = obj.Get(tok); !ok {
			return nil, false, nil
		}
	}
	obj, isObj := v.(*jsonfile.Object)
	if !isObj {
		return nil, true, fmt.Errorf("json-keys: %s is not an object", pointer)
	}
	return append([]string{}, obj.Keys()...), true, nil
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
