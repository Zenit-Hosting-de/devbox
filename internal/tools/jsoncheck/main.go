// Command jsoncheck is a lint tool (make lint). It checks every JSON file of the
// repository: the file parses, ends with a newline, and — when its "$schema" points into
// the DevBox schema directory — validates against that embedded schema.
package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"

	"github.com/Zenit-Hosting-de/devbox/internal/schema"
	"github.com/Zenit-Hosting-de/devbox/internal/tools/repofiles"
)

// Fixtures under test/fixtures/invalid are invalid on purpose; internal/schema tests them.
var exclude = []string{"test/fixtures/invalid/", "node_modules/"}

func main() {
	os.Exit(run(context.Background(), os.Stdout))
}

func run(ctx context.Context, out io.Writer) int {
	v, err := schema.New()
	if err != nil {
		_, _ = fmt.Fprintln(out, "jsoncheck:", err)
		return 1
	}
	files, err := repofiles.List(ctx, []string{".json"}, exclude)
	if err != nil {
		_, _ = fmt.Fprintln(out, "jsoncheck:", err)
		return 1
	}
	failed := 0
	for _, f := range files {
		if err := check(v, f); err != nil {
			failed++
			_, _ = fmt.Fprintf(out, "%s: %v\n", f, err)
		}
	}
	if failed > 0 {
		_, _ = fmt.Fprintf(out, "jsoncheck: %d of %d JSON files failed\n", failed, len(files))
		return 1
	}
	return 0
}

func check(v *schema.Validator, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if !bytes.HasSuffix(data, []byte("\n")) {
		return fmt.Errorf("missing trailing newline")
	}
	doc, err := schema.Decode(data)
	if err != nil {
		return err
	}
	obj, ok := doc.(map[string]any)
	if !ok {
		return nil
	}
	ref, ok := obj["$schema"].(string)
	if !ok {
		return nil
	}
	name, known := v.NameForURL(ref)
	switch {
	case name != "":
		return v.Validate(name, doc)
	case known:
		return fmt.Errorf("$schema %q is not an embedded DevBox schema", ref)
	default:
		return nil // a tool's own schema (tsconfig, renovate, …)
	}
}
