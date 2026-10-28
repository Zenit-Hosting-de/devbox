package plugin

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Zenit-Hosting-de/devbox/internal/schema"
)

// Problem is one finding of Validate. Code is stable, Detail names the subject.
type Problem struct {
	Code   string `json:"code"`
	Detail string `json:"detail"`
}

// Problem codes of Validate.
const (
	ProblemManifest     = "manifest-invalid"
	ProblemCommand      = "command-missing"
	ProblemNotExec      = "command-not-executable"
	ProblemDefault      = "default-invalid"
	ProblemOptionsFrom  = "options-from-invalid"
	ProblemTranslations = "translations-invalid"
	ProblemShellcheck   = "shellcheck"
)

// ValidateOptions control Validate.
type ValidateOptions struct {
	// Shellcheck runs shellcheck on the plugin's shell scripts when it is installed.
	Shellcheck bool
	PathEnv    string
}

// Validate checks a plugin directory: manifest, scripts, defaults, optionsFrom
// references, translations and (optionally) shellcheck.
func Validate(ctx context.Context, v *schema.Validator, dir string, opts ValidateOptions) (*Manifest, []Problem) {
	m, err := Load(v, dir)
	if err != nil {
		return nil, []Problem{{Code: ProblemManifest, Detail: err.Error()}}
	}
	var problems []Problem
	for _, c := range m.Commands() {
		problems = append(problems, checkCommand(dir, c, opts.PathEnv)...)
	}
	for key, field := range m.Settings() {
		def, ok := field["default"]
		if !ok || hasPlaceholder(def) {
			continue
		}
		frag, err := schema.CompileField(field)
		if err != nil {
			problems = append(problems, Problem{Code: ProblemDefault, Detail: key + ": " + err.Error()})
			continue
		}
		if err := frag.Validate(key, def); err != nil {
			problems = append(problems, Problem{Code: ProblemDefault, Detail: key + ": " + firstLine(err.Error())})
		}
	}
	checks, _ := m.Doc["checks"].(map[string]any)
	for _, ref := range m.optionsFrom() {
		check, _, _ := strings.Cut(ref, "#")
		id, name, _ := strings.Cut(check, ".")
		if _, ok := checks[name]; id != m.ID || !ok {
			problems = append(problems, Problem{Code: ProblemOptionsFrom, Detail: ref + ": no such check in this plugin"})
		}
	}
	problems = append(problems, validateTranslations(v, m)...)
	if opts.Shellcheck {
		problems = append(problems, runShellcheck(ctx, dir, opts.PathEnv)...)
	}
	sort.SliceStable(problems, func(i, j int) bool { return problems[i].Code < problems[j].Code })
	return m, problems
}

// Commands returns every "run", "apply", "remove" and "on" command of the manifest.
func (m *Manifest) Commands() [][]string {
	var out [][]string
	add := func(v any) {
		list, _ := v.([]any)
		cmd := make([]string, 0, len(list))
		for _, a := range list {
			s, _ := a.(string)
			cmd = append(cmd, s)
		}
		if len(cmd) > 0 {
			out = append(out, cmd)
		}
	}
	for _, section := range []string{"checks", "actions"} {
		entries, _ := m.Doc[section].(map[string]any)
		for _, name := range sortedKeys(entries) {
			e, _ := entries[name].(map[string]any)
			add(e["run"])
		}
	}
	consumes, _ := m.Doc["consumes"].(map[string]any)
	for _, name := range sortedKeys(consumes) {
		c, _ := consumes[name].(map[string]any)
		add(c["apply"])
		add(c["remove"])
	}
	on, _ := m.Doc["on"].(map[string]any)
	for _, name := range sortedKeys(on) {
		add(on[name])
	}
	return out
}

func (m *Manifest) optionsFrom() []string {
	var refs []string
	actions, _ := m.Doc["actions"].(map[string]any)
	for _, name := range sortedKeys(actions) {
		a, _ := actions[name].(map[string]any)
		params, _ := a["params"].(map[string]any)
		for _, p := range sortedKeys(params) {
			field, _ := params[p].(map[string]any)
			if ref, ok := field["optionsFrom"].(string); ok {
				refs = append(refs, ref)
			}
		}
	}
	return refs
}

func checkCommand(dir string, cmd []string, pathEnv string) []Problem {
	name := cmd[0]
	local := filepath.Join(dir, name)
	info, err := os.Stat(local)
	switch {
	case err == nil && info.Mode().Perm()&0o111 == 0:
		return []Problem{{Code: ProblemNotExec, Detail: name}}
	case err == nil:
		return nil
	case strings.Contains(name, "/"):
		return []Problem{{Code: ProblemCommand, Detail: name}}
	}
	for _, d := range filepath.SplitList(pathEnv) {
		if fi, err := os.Stat(filepath.Join(d, name)); d != "" && err == nil && !fi.IsDir() {
			return nil
		}
	}
	return []Problem{{Code: ProblemCommand, Detail: name}}
}

func hasPlaceholder(v any) bool {
	s, ok := v.(string)
	return ok && strings.Contains(s, "${")
}

func validateTranslations(v *schema.Validator, m *Manifest) []Problem {
	i18n, _ := m.Doc["i18n"].(map[string]any)
	sub, _ := i18n["dir"].(string)
	if sub == "" {
		return nil
	}
	files, _ := filepath.Glob(filepath.Join(m.Dir, sub, "*.json"))
	var problems []Problem
	for _, f := range files {
		data, err := os.ReadFile(f) //nolint:gosec // translation files of the plugin
		if err == nil {
			err = v.ValidateJSON("messages", data)
		}
		if err != nil {
			problems = append(problems, Problem{Code: ProblemTranslations, Detail: filepath.Base(f) + ": " + firstLine(err.Error())})
		}
	}
	return problems
}

func runShellcheck(ctx context.Context, dir, pathEnv string) []Problem {
	bin := ""
	for _, d := range filepath.SplitList(pathEnv) {
		if p := filepath.Join(d, "shellcheck"); d != "" && fileExecutable(p) {
			bin = p
			break
		}
	}
	if bin == "" {
		return nil
	}
	var scripts []string
	_ = filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && (strings.HasSuffix(p, ".sh") || strings.HasSuffix(p, ".bash")) {
			scripts = append(scripts, p)
		}
		return nil
	})
	if len(scripts) == 0 {
		return nil
	}
	args := append([]string{"-x", "--source-path=SCRIPTDIR", "-f", "gcc"}, scripts...)
	out, err := exec.CommandContext(ctx, bin, args...).CombinedOutput() //nolint:gosec // shellcheck from PATH on the plugin's own scripts
	var ee *exec.ExitError
	if err == nil || !errors.As(err, &ee) {
		return nil
	}
	var problems []Problem
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line != "" {
			problems = append(problems, Problem{Code: ProblemShellcheck, Detail: strings.TrimPrefix(line, dir+"/")})
		}
	}
	return problems
}

func fileExecutable(p string) bool {
	info, err := os.Stat(p)
	return err == nil && !info.IsDir() && info.Mode().Perm()&0o111 != 0
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// firstLine joins a multi-line error into one line.
func firstLine(s string) string {
	return strings.Join(strings.Fields(fmt.Sprint(s)), " ")
}
