// Package sdk implements the helper commands for plugin scripts (`devbox sdk …`,
// docs/concept.md, "Hilfsbefehle für Shell-Plugins"). They read the request file,
// write protocol messages to file descriptor 3 and change files idempotently:
// marked blocks, JSON values, downloads — always with a backup first, a "change"
// message, and only a message in a dry run.
package sdk

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Zenit-Hosting-de/devbox/internal/jsonfile"
	"github.com/Zenit-Hosting-de/devbox/internal/schema"
)

// Env is the protocol environment of the calling plugin.
type Env struct {
	Getenv func(string) string
	// Messages receives protocol messages; nil when DEVBOX_MSG_FD is not set.
	Messages  io.Writer
	Validator *schema.Validator
}

// FromProcess builds the environment of the current process.
func FromProcess(v *schema.Validator) Env {
	e := Env{Getenv: os.Getenv, Validator: v}
	if fd, err := strconv.Atoi(os.Getenv("DEVBOX_MSG_FD")); err == nil && fd > 2 {
		e.Messages = os.NewFile(uintptr(fd), "devbox-messages")
	}
	return e
}

// DryRun reports whether the run is a dry run.
func (e Env) DryRun() bool { return e.Getenv("DEVBOX_DRY_RUN") == "1" }

// Request reads the request file.
func (e Env) Request() (map[string]any, error) {
	path := e.Getenv("DEVBOX_REQUEST")
	if path == "" {
		return nil, errors.New("DEVBOX_REQUEST is not set; devbox sdk runs inside plugins only")
	}
	data, err := os.ReadFile(path) //nolint:gosec // path set by the runner
	if err != nil {
		return nil, err
	}
	doc, err := schema.Decode(data)
	if err != nil {
		return nil, err
	}
	req, ok := doc.(map[string]any)
	if !ok {
		return nil, errors.New("request is not an object")
	}
	return req, nil
}

// Lookup returns a field of a request section ("params", "settings").
func (e Env) Lookup(section, name string) (any, error) {
	req, err := e.Request()
	if err != nil {
		return nil, err
	}
	values, _ := req[section].(map[string]any)
	v, ok := values[name]
	if !ok {
		return nil, fmt.Errorf("%s: no %s %q", req["name"], strings.TrimSuffix(section, "s"), name)
	}
	return v, nil
}

// Secret returns the secret of the credential (apply and remove only).
func (e Env) Secret() (string, error) {
	req, err := e.Request()
	if err != nil {
		return "", err
	}
	cred, _ := req["credential"].(map[string]any)
	s, ok := cred["secret"].(string)
	if !ok {
		return "", errors.New("no credential in the request")
	}
	return s, nil
}

// Format prints a value for shell use: strings raw, everything else as JSON.
func Format(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	b, _ := json.Marshal(v)
	return string(b)
}

// Emit validates and writes one message. Without a message channel it does nothing.
func (e Env) Emit(msg map[string]any) error {
	if e.Validator != nil {
		if err := e.Validator.Validate("event.message", jsonfile.Plain(msg)); err != nil {
			return err
		}
	}
	if e.Messages == nil {
		return nil
	}
	b, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	_, err = e.Messages.Write(append(b, '\n'))
	return err
}

// EmitArgs builds a message from command line arguments:
//
//	step ID STATE LABEL | progress PERCENT [TEXT] | change KIND OP TARGET SUMMARY |
//	log LEVEL TEXT | setting KEY VALUE | notice KIND TEXT | result SUMMARY [DATA]
//
// VALUE and DATA are JSON when they parse, else text.
func EmitArgs(args []string) (map[string]any, error) {
	if len(args) == 0 {
		return nil, errors.New("emit: message type missing")
	}
	typ, rest := args[0], args[1:]
	need := func(lo, hi int) error {
		if len(rest) < lo || len(rest) > hi {
			return fmt.Errorf("emit %s: want %d to %d arguments, got %d", typ, lo, hi, len(rest))
		}
		return nil
	}
	m := map[string]any{"type": typ}
	switch typ {
	case "step":
		if err := need(3, 3); err != nil {
			return nil, err
		}
		m["id"], m["state"], m["label"] = rest[0], rest[1], rest[2]
	case "progress":
		if err := need(1, 2); err != nil {
			return nil, err
		}
		if rest[0] == "null" {
			m["percent"] = nil
		} else {
			n, err := strconv.Atoi(rest[0])
			if err != nil {
				return nil, fmt.Errorf("emit progress: percent %q is not a number", rest[0])
			}
			m["percent"] = json.Number(strconv.Itoa(n))
		}
		if len(rest) == 2 {
			m["text"] = rest[1]
		}
	case "change":
		if err := need(4, 4); err != nil {
			return nil, err
		}
		m["kind"], m["op"], m["target"], m["summary"] = rest[0], rest[1], rest[2], rest[3]
	case "log":
		if err := need(2, 2); err != nil {
			return nil, err
		}
		m["level"], m["text"] = rest[0], rest[1]
	case "setting":
		if err := need(2, 2); err != nil {
			return nil, err
		}
		m["key"], m["value"] = rest[0], jsonOrText(rest[1])
	case "notice":
		if err := need(2, 2); err != nil {
			return nil, err
		}
		m["kind"], m["text"] = rest[0], rest[1]
	case "result":
		if err := need(1, 2); err != nil {
			return nil, err
		}
		m["summary"] = rest[0]
		if len(rest) == 2 {
			m["data"] = jsonOrText(rest[1])
		}
	default:
		return nil, fmt.Errorf("emit: unknown message type %q", typ)
	}
	return m, nil
}

func jsonOrText(s string) any {
	if doc, err := schema.Decode([]byte(s)); err == nil {
		return doc
	}
	return s
}

// ExpandHome replaces a leading "~/" with $HOME.
func (e Env) ExpandHome(p string) string {
	if rest, ok := strings.CutPrefix(p, "~/"); ok {
		return filepath.Join(e.Getenv("HOME"), rest)
	}
	return p
}

// Write changes a file the protocol way: no change when the content is equal; in a
// dry run only the change message; otherwise backup, atomic write and message.
// mode is used for new files; existing files keep their mode.
func (e Env) Write(path string, content []byte, mode os.FileMode, summary string) (changed bool, err error) {
	old, err := os.ReadFile(path) //nolint:gosec // paths come from the plugin, which declares them
	exists := err == nil
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return false, err
	}
	if exists && bytes.Equal(old, content) {
		return false, nil
	}
	op := "update"
	if !exists {
		op = "create"
	}
	if !e.DryRun() {
		if exists {
			if err := e.backup(path, old); err != nil {
				return false, err
			}
			if info, err := os.Stat(path); err == nil {
				mode = info.Mode().Perm()
			}
		}
		if err := jsonfile.WriteAtomic(path, content, mode); err != nil {
			return false, err
		}
	}
	return true, e.Emit(map[string]any{"type": "change", "kind": "file", "op": op, "target": e.display(path), "summary": summary})
}

// backup keeps the first original of a file per session under DEVBOX_BACKUP_DIR.
func (e Env) backup(path string, data []byte) error {
	dir := e.Getenv("DEVBOX_BACKUP_DIR")
	if dir == "" {
		return nil
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	target := filepath.Join(dir, abs)
	if _, err := os.Stat(target); err == nil {
		return nil // the first version of this session is kept
	}
	return jsonfile.WriteAtomic(target, data, 0o600)
}

// display shortens paths in $HOME to "~/…" for messages.
func (e Env) display(path string) string {
	home := e.Getenv("HOME")
	if home != "" {
		if rest, ok := strings.CutPrefix(path, home+"/"); ok {
			return "~/" + rest
		}
	}
	return path
}
