package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/Zenit-Hosting-de/devbox/internal/config"
	"github.com/Zenit-Hosting-de/devbox/internal/credentials"
	"github.com/Zenit-Hosting-de/devbox/internal/plugin"
	"github.com/Zenit-Hosting-de/devbox/internal/runner"
	"github.com/Zenit-Hosting-de/devbox/internal/schema"
	"github.com/Zenit-Hosting-de/devbox/internal/vault"
)

// maxSecretSize bounds a secret read from stdin.
const maxSecretSize = 1 << 20

// controllingTerminal is opened for the vault passphrase when stdin is not a terminal.
const controllingTerminal = "/dev/tty"

var instanceName = regexp.MustCompile(`^[a-z][a-z0-9-]{1,31}$`)

// credTypeDef is a credential type and the plugin that defines it.
type credTypeDef struct {
	pluginID string
	def      map[string]any
}

func (t credTypeDef) fields() map[string]any {
	f, _ := t.def["fields"].(map[string]any)
	return f
}

func (t credTypeDef) secret() map[string]any {
	s, _ := t.def["secret"].(map[string]any)
	return s
}

// credCtx is the loaded configuration together with the credentials directory.
type credCtx struct {
	st        *state
	plugins   []*plugin.Plugin
	path      string
	prepared  []credentials.Prepared
	instances []credentials.Instance
	types     map[string]credTypeDef
}

func (a *app) credContext() (*credCtx, error) {
	st, err := a.load(true)
	if err != nil {
		return nil, err
	}
	c := &credCtx{
		st: st, plugins: a.evaluate(st), types: map[string]credTypeDef{},
		path: filepath.Join(st.env.ctx.XDG.Data, "devbox", credentials.FileName),
	}
	for _, m := range st.plugins { // search order: the first definition of a type wins
		types, _ := m.Doc["credentialTypes"].(map[string]any)
		for name, d := range types {
			if _, dup := c.types[name]; !dup {
				def, _ := d.(map[string]any)
				c.types[name] = credTypeDef{pluginID: m.ID, def: def}
			}
		}
	}
	for _, pf := range st.profiles {
		c.prepared = append(c.prepared, credentials.FromProfile(pf.Plain())...)
	}
	return c, c.reload()
}

func (c *credCtx) reload() error {
	f, err := credentials.Load(c.st.env.validator, c.path)
	if err != nil {
		return err
	}
	c.instances = credentials.Merge(f, c.prepared)
	return nil
}

func (c *credCtx) update(change func(*credentials.File) error) error {
	if err := credentials.Update(c.st.env.validator, c.path, change); err != nil {
		return err
	}
	return c.reload()
}

func (c *credCtx) instance(name string) (*credentials.Instance, error) {
	inst, ok := credentials.Find(c.instances, name)
	if !ok {
		return nil, &config.Error{Code: config.CodeNotFound, Key: name, Detail: "no such credential instance; create it with devbox cred add"}
	}
	return inst, nil
}

func (c *credCtx) typeOf(inst *credentials.Instance) (credTypeDef, error) {
	t, ok := c.types[inst.Type]
	if !ok {
		return t, &config.Error{Code: config.CodeNotFound, Key: inst.Name, Detail: fmt.Sprintf("no plugin defines the credential type %q", inst.Type)}
	}
	return t, nil
}

// withDefaults returns values with the defaults of the fields filled in.
func withDefaults(st *state, values map[string]any, fields map[string]any) (map[string]any, error) {
	out := make(map[string]any, len(values))
	for k, v := range values {
		out[k] = v
	}
	for _, name := range sortedNames(fields) {
		field, _ := fields[name].(map[string]any)
		def, ok := field["default"]
		if _, set := out[name]; set || !ok {
			continue
		}
		v, err := config.ExpandValue(def, settingLookup(st))
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		out[name] = v
	}
	return out, nil
}

// fieldValue converts and checks a value given as text against a field.
func fieldValue(field map[string]any, key, raw string) (any, error) {
	v, err := config.Coerce(field, raw)
	if err != nil {
		return nil, &config.Error{Code: config.CodeValidation, Key: key, Detail: err.Error()}
	}
	frag, err := schema.CompileField(field)
	if err != nil {
		return nil, err
	}
	if err := frag.Validate(key, v); err != nil {
		return nil, &config.Error{Code: config.CodeValidation, Key: key, Detail: firstLineOf(err.Error())}
	}
	return v, nil
}

func firstLineOf(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return line
}

func sortedNames[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func (a *app) credCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "cred", Short: "manage credentials", Args: cobra.NoArgs}

	list := &cobra.Command{
		Use: "list", Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error { return a.credList() },
	}

	var addType, addLabel string
	var addFields, addConsumers []string
	add := &cobra.Command{
		Use: "add NAME", Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return a.credAdd(args[0], addType, addLabel, addFields, addConsumers)
		},
	}
	add.Flags().StringVar(&addType, "type", "", "credential type, e.g. registry")
	add.Flags().StringVar(&addLabel, "label", "", "display name")
	add.Flags().StringArrayVar(&addFields, "field", nil, "field value (name=value)")
	add.Flags().StringArrayVar(&addConsumers, "consumer", nil, "consumer plugin with options (plugin[:name=value,…])")

	var fromStdin bool
	var expires string
	set := &cobra.Command{
		Use: "set NAME", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.credSet(cmd.Context(), args[0], fromStdin, expires)
		},
	}
	set.Flags().BoolVar(&fromStdin, "from-stdin", false, "read the secret from stdin instead of the terminal")
	set.Flags().StringVar(&expires, "expires", "", "expiry date of the secret (YYYY-MM-DD or RFC 3339)")

	verify := &cobra.Command{
		Use: "verify NAME", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error { return a.credVerify(cmd.Context(), args[0]) },
	}

	var applyDry bool
	apply := &cobra.Command{
		Use: "apply NAME", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error { return a.credApply(cmd.Context(), args[0], applyDry) },
	}
	apply.Flags().BoolVar(&applyDry, "dry-run", false, "report planned changes only")

	var removeDry, force bool
	remove := &cobra.Command{
		Use: "remove NAME", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.credRemove(cmd.Context(), args[0], removeDry, force)
		},
	}
	remove.Flags().BoolVar(&removeDry, "dry-run", false, "report planned changes only")
	remove.Flags().BoolVar(&force, "force", false, "delete the secret even if a consumer failed to remove it")

	cmd.AddCommand(list, add, set, verify, apply, remove)
	return cmd
}

// credListOutput is `devbox cred list --json`.
type credListOutput struct {
	Instances []credentials.Instance `json:"instances"`
}

func (a *app) credList() error {
	c, err := a.credContext()
	if err != nil {
		return err
	}
	if a.jsonOut {
		return a.printJSON(credListOutput{Instances: append([]credentials.Instance{}, c.instances...)})
	}
	if len(c.instances) == 0 {
		_, err := fmt.Fprintln(a.io.Stdout, "no credentials")
		return err
	}
	tw := tabwriter.NewWriter(a.io.Stdout, 0, 4, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "NAME\tTYPE\tORIGIN\tSECRET\tVERIFIED\tCONSUMERS")
	for _, inst := range c.instances {
		secret := "missing"
		if inst.Secret != nil {
			secret = "set"
			if inst.Secret.ExpiresAt != nil {
				secret += ", expires " + strings.SplitN(*inst.Secret.ExpiresAt, "T", 2)[0]
			}
		}
		verified := "-"
		if inst.Verified != nil {
			verified = inst.Verified.Status
		}
		consumers := strings.Join(sortedNames(inst.Consumers), ",")
		if consumers == "" {
			consumers = "-"
		}
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n", inst.Name, inst.Type, inst.Origin, secret, verified, consumers)
	}
	return tw.Flush()
}

func (a *app) credAdd(name, typ, label string, fieldArgs, consumerArgs []string) error {
	if !instanceName.MatchString(name) {
		return &config.Error{Code: config.CodeValidation, Key: name, Detail: "an instance name matches ^[a-z][a-z0-9-]{1,31}$"}
	}
	c, err := a.credContext()
	if err != nil {
		return err
	}
	origin := credentials.OriginUser
	inst, exists := credentials.Find(c.instances, name)
	switch {
	case exists && typ != "" && typ != inst.Type:
		return &config.Error{Code: config.CodeValidation, Key: name, Detail: fmt.Sprintf("the instance has the type %q", inst.Type)}
	case exists:
		typ, origin = inst.Type, inst.Origin
	case typ == "":
		return &config.Error{Code: config.CodeValidation, Key: name, Detail: "--type is required for a new instance"}
	default:
		inst = &credentials.Instance{Name: name, Type: typ}
	}
	td, err := c.typeOf(inst)
	if err != nil {
		return err
	}
	fields := map[string]any{}
	typeFields := td.fields()
	for _, kv := range fieldArgs {
		k, raw, ok := strings.Cut(kv, "=")
		field, known := typeFields[k].(map[string]any)
		if !ok || !known {
			return &config.Error{Code: config.CodeValidation, Key: name, Detail: fmt.Sprintf("--field %q: the type %s has no such field", kv, typ)}
		}
		if inst.IsLocked(k) {
			return &config.Error{Code: config.CodeSettingLocked, Key: name + "." + k, Detail: "locked by " + inst.Origin}
		}
		v, err := fieldValue(field, name+"."+k, raw)
		if err != nil {
			return err
		}
		fields[k] = v
	}
	consumers := map[string]map[string]any{}
	for _, arg := range consumerArgs {
		id, opts, err := c.consumerOptions(typ, arg)
		if err != nil {
			return err
		}
		merged := consumers[id]
		if merged == nil {
			merged = map[string]any{}
		}
		for k, v := range opts {
			merged[k] = v
		}
		consumers[id] = merged
	}
	err = c.update(func(f *credentials.File) error {
		s := f.Entry(name, typ, origin)
		if label != "" {
			s.Label = label
		}
		if len(fields) > 0 && s.Fields == nil {
			s.Fields = map[string]any{}
		}
		for k, v := range fields {
			s.Fields[k] = v
		}
		if len(consumers) > 0 && s.Consumers == nil {
			s.Consumers = map[string]map[string]any{}
		}
		for id, opts := range consumers {
			merged := s.Consumers[id]
			if merged == nil {
				merged = map[string]any{}
			}
			for k, v := range opts {
				merged[k] = v
			}
			s.Consumers[id] = merged
		}
		return nil
	})
	if err != nil {
		return err
	}
	inst, err = c.instance(name)
	if err != nil {
		return err
	}
	if a.jsonOut {
		return a.printJSON(inst)
	}
	_, err = fmt.Fprintf(a.io.Stdout, "%s (%s): saved; set the secret with devbox cred set %s\n", name, typ, name)
	return err
}

// consumerOptions parses plugin[:name=value,…] and checks plugin and options.
func (c *credCtx) consumerOptions(typ, arg string) (string, map[string]any, error) {
	id, rest, _ := strings.Cut(arg, ":")
	p := findPlugin(c.plugins, id)
	if p == nil || p.Manifest == nil {
		return "", nil, &config.Error{Code: config.CodeNotFound, Key: id, Detail: "--consumer: no such plugin"}
	}
	consumer, ok := consumerDef(p, typ)
	if !ok {
		return "", nil, &config.Error{Code: config.CodeValidation, Key: id, Detail: fmt.Sprintf("--consumer: the plugin does not consume the type %s", typ)}
	}
	optionFields, _ := consumer["options"].(map[string]any)
	opts := map[string]any{}
	for _, kv := range splitOptions(rest) {
		k, raw, ok := strings.Cut(kv, "=")
		field, known := optionFields[k].(map[string]any)
		if !ok || !known {
			return "", nil, &config.Error{Code: config.CodeValidation, Key: id, Detail: fmt.Sprintf("--consumer: unknown option %q", kv)}
		}
		v, err := fieldValue(field, id+"#"+k, raw)
		if err != nil {
			return "", nil, err
		}
		opts[k] = v
	}
	return id, opts, nil
}

// splitOptions splits name=value pairs at commas outside of JSON brackets and quotes,
// so array values such as scopes=["@a","@b"] stay whole.
func splitOptions(s string) []string {
	var out []string
	depth, quoted, escaped, start := 0, false, false, 0
	for i, r := range s {
		switch {
		case escaped:
			escaped = false
		case quoted && r == '\\':
			escaped = true
		case r == '"':
			quoted = !quoted
		case quoted:
		case r == '[' || r == '{':
			depth++
		case r == ']' || r == '}':
			depth--
		case r == ',' && depth == 0:
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	if s != "" {
		out = append(out, s[start:])
	}
	return out
}

func consumerDef(p *plugin.Plugin, typ string) (map[string]any, bool) {
	if p.Manifest == nil {
		return nil, false
	}
	consumes, _ := p.Manifest.Doc["consumes"].(map[string]any)
	def, ok := consumes[typ].(map[string]any)
	return def, ok
}

// mayAccess reports whether the plugin declares the credential type in
// permissions.credentials.
func mayAccess(p *plugin.Plugin, typ string) bool {
	perms, _ := p.Manifest.Doc["permissions"].(map[string]any)
	for _, t := range anyStrings(perms["credentials"]) {
		if t == typ {
			return true
		}
	}
	return false
}

// openVault opens the vault backend (empty: vault.backend of the configuration).
func (a *app) openVault(st *state, backend string) (vault.Backend, error) {
	passphrase := false
	configured := ""
	for _, f := range []*config.File{st.system, st.user} {
		v, _ := f.Plain()["vault"].(map[string]any)
		if b, ok := v["backend"].(string); ok {
			configured = b
		}
		if p, ok := v["passphrase"].(bool); ok {
			passphrase = p
		}
	}
	if backend == "" {
		backend = configured
	}
	opts := vault.Options{
		Backend: backend, DataDir: filepath.Join(st.env.ctx.XDG.Data, "devbox"),
		Keyring: vault.SystemKeyring{}, Validator: st.env.validator,
	}
	if passphrase && backend != vault.BackendKeyring {
		p, err := a.readPassphrase("Vault passphrase: ")
		if err != nil {
			return nil, err
		}
		opts.Passphrase = p
	}
	return vault.Open(opts)
}

// readHidden reads a line from the terminal on stdin without echo.
func (a *app) readHidden(prompt string) (string, error) {
	fd, ok := fdOf(a.io.Stdin)
	if !ok || !term.IsTerminal(fd) {
		return "", &exitError{code: ExitInvalid, err: errors.New("no terminal for hidden input; pass the secret with --from-stdin")}
	}
	return a.readHiddenFD(fd, prompt)
}

// readPassphrase reads the vault passphrase without echo from the terminal on stdin or,
// when stdin carries the secret (--from-stdin), from the controlling terminal.
func (a *app) readPassphrase(prompt string) (string, error) {
	if fd, ok := fdOf(a.io.Stdin); ok && term.IsTerminal(fd) {
		return a.readHiddenFD(fd, prompt)
	}
	tty, err := os.Open(controllingTerminal)
	if err != nil {
		return "", &exitError{code: ExitInvalid, err: errors.New("vault.passphrase needs a terminal for the passphrase")}
	}
	defer func() { _ = tty.Close() }()
	return a.readHiddenFD(int(tty.Fd()), prompt) //nolint:gosec // file descriptors fit int
}

func (a *app) readHiddenFD(fd int, prompt string) (string, error) {
	_, _ = fmt.Fprint(a.io.Stderr, prompt)
	b, err := term.ReadPassword(fd)
	_, _ = fmt.Fprintln(a.io.Stderr)
	if err != nil {
		return "", fmt.Errorf("read secret: %w", err)
	}
	return string(b), nil
}

// readSecret reads the secret from stdin (without the final line break) or hidden from
// the terminal.
func (a *app) readSecret(prompt string, fromStdin bool) (string, error) {
	var s string
	if fromStdin {
		data, err := io.ReadAll(io.LimitReader(a.io.Stdin, maxSecretSize+1))
		if err != nil {
			return "", fmt.Errorf("read secret: %w", err)
		}
		if len(data) > maxSecretSize {
			return "", &exitError{code: ExitInvalid, err: errors.New("the secret is too long")}
		}
		s = strings.TrimRight(string(data), "\r\n")
	} else {
		var err error
		if s, err = a.readHidden(prompt); err != nil {
			return "", err
		}
	}
	if s == "" {
		return "", &exitError{code: ExitInvalid, err: errors.New("empty secret")}
	}
	return s, nil
}

// parseExpiry accepts YYYY-MM-DD (midnight UTC) or RFC 3339.
func parseExpiry(s string) (*time.Time, error) {
	if s == "" {
		return nil, nil
	}
	for _, layout := range []string{time.DateOnly, time.RFC3339} {
		if t, err := time.Parse(layout, s); err == nil {
			t = t.UTC()
			return &t, nil
		}
	}
	return nil, &exitError{code: ExitInvalid, err: fmt.Errorf("--expires %q: want YYYY-MM-DD or RFC 3339", s)}
}

func timestamp(t time.Time) string { return t.UTC().Truncate(time.Second).Format(time.RFC3339) }

// secretOf reads the secret of an instance from the vault named in its reference.
func (a *app) secretOf(c *credCtx, inst *credentials.Instance) (string, error) {
	if inst.Secret == nil {
		return "", &exitError{code: ExitInvalid, err: fmt.Errorf("%s: no secret; set one with devbox cred set %s", inst.Name, inst.Name)}
	}
	backend, name, err := vault.ParseRef(inst.Secret.Ref)
	if err != nil {
		return "", err
	}
	b, err := a.openVault(c.st, backend)
	if err != nil {
		return "", err
	}
	e, err := b.Get(name)
	if err != nil {
		return "", fmt.Errorf("%s: %w", inst.Name, err)
	}
	return e.Secret, nil
}

func (a *app) credSet(ctx context.Context, name string, fromStdin bool, expires string) error {
	c, err := a.credContext()
	if err != nil {
		return err
	}
	inst, err := c.instance(name)
	if err != nil {
		return err
	}
	td, err := c.typeOf(inst)
	if err != nil {
		return err
	}
	secretDef := td.secret()
	exp, err := parseExpiry(expires)
	if err != nil {
		return err
	}
	switch secretDef["expires"] {
	case "never":
		if exp != nil {
			return &exitError{code: ExitInvalid, err: fmt.Errorf("%s: secrets of the type %s do not expire", name, inst.Type)}
		}
	case "required":
		if exp == nil {
			return &exitError{code: ExitInvalid, err: fmt.Errorf("%s: the type %s needs --expires", name, inst.Type)}
		}
	}
	label, _ := secretDef["label"].(string)
	secret, err := a.readSecret(fmt.Sprintf("%s for %s: ", label, name), fromStdin)
	if err != nil {
		return err
	}
	b, err := a.openVault(c.st, "")
	if err != nil {
		return err
	}
	now := time.Now()
	if err := b.Set(name, vault.Entry{Secret: secret, UpdatedAt: now, ExpiresAt: exp}); err != nil {
		return err
	}
	ref := vault.Ref(b.Name(), name)
	if old := inst.Secret; old != nil && old.Ref != ref {
		a.deleteSecret(c, old.Ref)
	}
	err = c.update(func(f *credentials.File) error {
		s := f.Entry(name, inst.Type, inst.Origin)
		s.Secret = &credentials.Secret{Ref: ref, UpdatedAt: timestamp(now)}
		if exp != nil {
			at := timestamp(*exp)
			s.Secret.ExpiresAt = &at
		}
		s.Verified = nil
		return nil
	})
	if err != nil {
		return err
	}
	a.say("%s: secret saved\n", name)
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()
	inst, _ = c.instance(name)
	r, err := a.newRunner(c.st)
	if err != nil {
		return err
	}
	ok, err := a.verify(ctx, c, r, inst, secret)
	if err != nil {
		return err
	}
	if !ok {
		return &exitError{code: ExitFailed, err: fmt.Errorf("%s: verification failed; the consumers were not applied", name)}
	}
	return a.consumers(ctx, c, r, name, secret, runner.KindApply, false)
}

// say prints progress in text mode only (--json keeps stdout for JSON Lines).
func (a *app) say(format string, args ...any) {
	if !a.jsonOut {
		_, _ = fmt.Fprintf(a.io.Stdout, format, args...)
	}
}

func (a *app) deleteSecret(c *credCtx, ref string) {
	backend, name, err := vault.ParseRef(ref)
	if err != nil {
		return
	}
	b, err := a.openVault(c.st, backend)
	if err == nil {
		err = b.Delete(name)
	}
	if err != nil && !errors.Is(err, vault.ErrNotFound) {
		_, _ = fmt.Fprintln(a.io.Stderr, "devbox: warning: delete secret:", err)
	}
}

func (a *app) credVerify(ctx context.Context, name string) error {
	c, err := a.credContext()
	if err != nil {
		return err
	}
	inst, err := c.instance(name)
	if err != nil {
		return err
	}
	secret, err := a.secretOf(c, inst)
	if err != nil {
		return err
	}
	r, err := a.newRunner(c.st)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()
	ok, err := a.verify(ctx, c, r, inst, secret)
	if err != nil {
		return err
	}
	if !ok {
		return &exitError{code: ExitFailed, err: fmt.Errorf("%s: verification failed", name)}
	}
	return nil
}

// credentialRequest is request.credential for a plugin.
func credentialRequest(inst *credentials.Instance, fields, options map[string]any, secret string) map[string]any {
	req := map[string]any{"instance": inst.Name, "type": inst.Type, "fields": fields, "secret": secret}
	if options != nil {
		req["options"] = options
	}
	return req
}

// verify runs the verify action of the type and records the result. Without a verify
// action it reports success.
func (a *app) verify(ctx context.Context, c *credCtx, r *runner.Runner, inst *credentials.Instance, secret string) (bool, error) {
	td, err := c.typeOf(inst)
	if err != nil {
		return false, err
	}
	ref, _ := td.def["verify"].(string)
	if ref == "" {
		a.say("%s: the type %s defines no verification\n", inst.Name, inst.Type)
		return true, nil
	}
	ok, message, expiresAt := a.runVerify(ctx, c, r, inst, secret, ref)
	status := credentials.VerifiedFailed
	if ok {
		status = credentials.VerifiedOK
	}
	err = c.update(func(f *credentials.File) error {
		s := f.Entry(inst.Name, inst.Type, inst.Origin)
		s.Verified = &credentials.Verified{At: timestamp(time.Now()), Status: status, Message: message}
		if expiresAt != "" && s.Secret != nil && s.Secret.ExpiresAt == nil {
			s.Secret.ExpiresAt = &expiresAt
		}
		return nil
	})
	if err != nil {
		return false, err
	}
	if ok {
		a.say("✓ %s verified: %s\n", inst.Name, message)
	} else {
		_, _ = fmt.Fprintf(a.io.Stderr, "✗ %s not verified: %s\n", inst.Name, message)
	}
	return ok, nil
}

// runVerify runs the verify action as a session. It returns whether it succeeded, a
// message, and an expiry date the action reported in result.data.expiresAt.
func (a *app) runVerify(ctx context.Context, c *credCtx, r *runner.Runner, inst *credentials.Instance, secret, ref string) (ok bool, message, expiresAt string) {
	pluginID, name, _ := strings.Cut(ref, ".")
	p := findPlugin(c.plugins, pluginID)
	if p == nil || p.State != plugin.StateReady {
		state := "missing"
		if p != nil {
			state = fmt.Sprintf("%s (%s)", p.State, reasons(p.Reasons))
		}
		return false, fmt.Sprintf("%s: plugin %s", ref, state), ""
	}
	actions, _ := p.Manifest.Doc["actions"].(map[string]any)
	action, found := actions[name].(map[string]any)
	if !found {
		return false, ref + ": no such action", ""
	}
	td, _ := c.typeOf(inst)
	fields, err := withDefaults(c.st, inst.Fields, td.fields())
	if err != nil {
		return false, err.Error(), ""
	}
	params, err := actionParams(c.st, action, ref, nil)
	if err != nil {
		return false, err.Error(), ""
	}
	spec := runner.Spec{
		Kind: runner.KindAction, SessionID: runner.NewSessionID(time.Now()),
		PluginID: pluginID, PluginDir: p.Dir, Name: name, Command: anyStrings(action["run"]),
		Params: params, Settings: c.st.result.Plugin(pluginID),
		Credential: credentialRequest(inst, fields, nil, secret), Secrets: []string{secret},
		Lang: uiLanguage(c.st), Host: hostMap(c.st, a.io.Getenv), Locks: anyStrings(action["locks"]),
		ExtraEnv: proxyEnv(c.st),
	}
	if t, ok := action["timeout"].(string); ok {
		spec.Timeout, _ = time.ParseDuration(t)
	}
	res, err := a.runSession(ctx, c.st, r, spec, ref, "Verify credential "+inst.Name)
	if err != nil {
		return false, err.Error(), ""
	}
	message = res.State
	if res.Result != nil {
		if s, _ := res.Result["summary"].(string); s != "" {
			message = s
		}
		data, _ := res.Result["data"].(map[string]any)
		if s, _ := data["expiresAt"].(string); s != "" {
			if t, err := time.Parse(time.RFC3339, s); err == nil {
				expiresAt = timestamp(t)
			}
		}
	}
	if res.State != runner.StateSuccess && res.ExitCode > 0 {
		message = fmt.Sprintf("%s (exit %d)", message, res.ExitCode)
	}
	return res.State == runner.StateSuccess, message, expiresAt
}

func (a *app) credApply(ctx context.Context, name string, dryRun bool) error {
	c, err := a.credContext()
	if err != nil {
		return err
	}
	inst, err := c.instance(name)
	if err != nil {
		return err
	}
	secret, err := a.secretOf(c, inst)
	if err != nil {
		return err
	}
	r, err := a.newRunner(c.st)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()
	return a.consumers(ctx, c, r, name, secret, runner.KindApply, dryRun)
}

func (a *app) credRemove(ctx context.Context, name string, dryRun, force bool) error {
	c, err := a.credContext()
	if err != nil {
		return err
	}
	inst, err := c.instance(name)
	if err != nil {
		return err
	}
	secret := ""
	if inst.Secret != nil {
		if secret, err = a.secretOf(c, inst); err != nil {
			_, _ = fmt.Fprintln(a.io.Stderr, "devbox: warning:", err)
		}
	}
	r, err := a.newRunner(c.st)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()
	cerr := a.consumers(ctx, c, r, name, secret, runner.KindRemove, dryRun)
	if dryRun {
		return cerr
	}
	if cerr != nil && !force {
		return &exitError{code: ExitFailed, err: fmt.Errorf("%w; secret and instance kept (retry, or use --force)", cerr)}
	}
	inst, err = c.instance(name)
	if err != nil {
		return err
	}
	if inst.Secret != nil {
		a.deleteSecret(c, inst.Secret.Ref)
	}
	if err := c.update(func(f *credentials.File) error {
		f.Forget(name, inst.Prepared != nil)
		return nil
	}); err != nil {
		return err
	}
	if inst.Prepared != nil {
		a.say("%s: secret removed; the instance stays, the profile %s prepares it\n", name, inst.Prepared.Profile)
	} else {
		a.say("%s: removed\n", name)
	}
	return cerr
}

// consumers runs apply or remove of every consumer of the instance, one after another.
// A failing consumer does not stop the others; the error names all failed ones.
// Consumers whose plugin the configuration disables are skipped on purpose.
func (a *app) consumers(ctx context.Context, c *credCtx, r *runner.Runner, name, secret, kind string, dryRun bool) error {
	inst, err := c.instance(name)
	if err != nil {
		return err
	}
	ids := sortedNames(inst.Consumers)
	if len(ids) == 0 {
		a.say("%s: no consumers\n", name)
		return nil
	}
	var failed []string
	for _, id := range ids {
		if ctx.Err() != nil {
			failed = append(failed, id+": stopped")
			continue
		}
		if p := findPlugin(c.plugins, id); p != nil && p.State == plugin.StateDisabled {
			a.say("– %s %s: skipped, the plugin is disabled\n", id, kind)
			continue
		}
		a.say("▸ %s %s → %s\n", kind, name, id)
		ok, why := a.runConsumer(ctx, c, r, inst, id, secret, kind, dryRun)
		if !ok {
			failed = append(failed, id+": "+why)
			_, _ = fmt.Fprintf(a.io.Stderr, "✗ %s %s: %s\n", id, kind, why)
			continue
		}
		a.say("✓ %s %s\n", id, kind)
		if dryRun {
			continue
		}
		err := c.update(func(f *credentials.File) error {
			s := f.Entry(name, inst.Type, inst.Origin)
			if kind == runner.KindApply {
				if s.Applied == nil {
					s.Applied = map[string]string{}
				}
				s.Applied[id] = timestamp(time.Now())
			} else {
				delete(s.Applied, id)
			}
			return nil
		})
		if err != nil {
			return err
		}
	}
	if len(failed) > 0 {
		return &exitError{code: ExitFailed, err: fmt.Errorf("%s %s failed for %s", kind, name, strings.Join(failed, "; "))}
	}
	return nil
}

func (a *app) runConsumer(ctx context.Context, c *credCtx, r *runner.Runner, inst *credentials.Instance, id, secret, kind string, dryRun bool) (bool, string) {
	p := findPlugin(c.plugins, id)
	if p == nil {
		return false, "no such plugin"
	}
	if p.State != plugin.StateReady {
		return false, fmt.Sprintf("plugin %s (%s)", p.State, reasons(p.Reasons))
	}
	def, ok := consumerDef(p, inst.Type)
	if !ok {
		return false, "the plugin does not consume the type " + inst.Type
	}
	if !mayAccess(p, inst.Type) {
		return false, "the plugin does not declare the type " + inst.Type + " in permissions.credentials"
	}
	optionFields, _ := def["options"].(map[string]any)
	options, err := withDefaults(c.st, inst.Consumers[id], optionFields)
	if err != nil {
		return false, err.Error()
	}
	for _, k := range sortedNames(options) {
		field, known := optionFields[k].(map[string]any)
		if !known {
			return false, fmt.Sprintf("unknown option %q", k)
		}
		frag, err := schema.CompileField(field)
		if err == nil {
			err = frag.Validate(id+"#"+k, options[k])
		}
		if err != nil {
			return false, firstLineOf(err.Error())
		}
	}
	td, _ := c.typeOf(inst)
	fields, err := withDefaults(c.st, inst.Fields, td.fields())
	if err != nil {
		return false, err.Error()
	}
	spec := runner.Spec{
		Kind: kind, SessionID: runner.NewSessionID(time.Now()),
		PluginID: id, PluginDir: p.Dir, Name: inst.Type, Command: anyStrings(def[kind]),
		Settings: c.st.result.Plugin(id), Credential: credentialRequest(inst, fields, options, secret),
		Secrets: []string{secret}, DryRun: dryRun, Lang: uiLanguage(c.st), Host: hostMap(c.st, a.io.Getenv),
		ExtraEnv: proxyEnv(c.st),
	}
	verb := map[string]string{runner.KindApply: "Apply", runner.KindRemove: "Remove"}[kind]
	res, err := a.runSession(ctx, c.st, r, spec, id+"."+kind, fmt.Sprintf("%s credential %s", verb, inst.Name))
	if err != nil {
		return false, err.Error()
	}
	if res.State != runner.StateSuccess {
		return false, fmt.Sprintf("%s (exit %d)", res.State, res.ExitCode)
	}
	return true, ""
}
