package repo

import (
	"fmt"
	"net/url"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/Zenit-Hosting-de/devbox/internal/config"
	"github.com/Zenit-Hosting-de/devbox/internal/sigstore"
)

// KindPlugins is the only kind of repository that is handled; the others are listed
// as unsupported.
const KindPlugins = "plugins"

// KindCatalog is a catalog: an index.json with signed, versioned plugin packages.
const KindCatalog = "catalog"

// Trust values of an entry. TrustSignature stands for the object form (a Sigstore
// identity), which arrives with the catalog (AP-23).
const (
	TrustPrompt    = "prompt"
	TrustPinned    = "pinned"
	TrustSignature = "signature"
)

// Origins of an entry, the same names as the setting layers.
const (
	OriginUser   = config.LayerUser
	OriginSystem = config.LayerSystem
)

// Kinds of sources.
const (
	sourceGit   = "git"
	sourceHTTPS = "https"
	sourceFile  = "file"
)

// credentialName matches the name of a credential instance (schema/credentials.v1.json).
var credentialName = regexp.MustCompile(`^[a-z][a-z0-9-]{1,31}$`)

var fullCommit = regexp.MustCompile(`^[0-9a-f]{40}$`)

// ValidName matches the name of a repository (schema/config.v1.json, repositories).
var ValidName = regexp.MustCompile(`^[a-z][a-z0-9-]{1,31}$`)

// Entry is one repository of the configuration, after the layers are merged.
type Entry struct {
	Name       string
	Kind       string
	Source     string
	Ref        string
	Path       string
	Credential string
	// Trust is TrustPrompt, TrustPinned or TrustSignature.
	Trust string
	// Signer is the Sigstore identity of the object form of trust; nil otherwise.
	Signer  *sigstore.Identity
	Enabled bool
	// Origin is "user", "system" or "profile:<id>".
	Origin string
	// Locked is set when the entry comes from a layer below the user and locks itself.
	Locked bool

	// source and path have the placeholders expanded; expandErr is why not.
	source    string
	path      string
	expandErr error
}

// Layer holds the repositories object of one configuration layer.
type Layer struct {
	// Origin names the layer: "system", "profile:<id>" or "user".
	Origin       string
	Repositories map[string]any
}

// Collect merges the repositories of the layers, which are given in order (system,
// profiles, user): a later layer replaces an entry of the same name, unless the entry
// of the earlier layer is locked. expand resolves the placeholders of source and path.
// The result is sorted by name.
func Collect(layers []Layer, expand func(string) (string, error)) []Entry {
	byName := map[string]Entry{}
	for _, l := range layers {
		for name, raw := range l.Repositories {
			e := parseEntry(name, raw, l.Origin, expand)
			if old, ok := byName[name]; ok && old.Locked {
				continue
			}
			byName[name] = e
		}
	}
	out := make([]Entry, 0, len(byName))
	for _, e := range byName {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func parseEntry(name string, raw any, origin string, expand func(string) (string, error)) Entry {
	m, _ := raw.(map[string]any)
	str := func(key string) string {
		s, _ := m[key].(string)
		return s
	}
	e := Entry{
		Name: name, Kind: str("kind"), Source: str("source"), Ref: str("ref"), Path: str("path"),
		Credential: str("credential"), Trust: TrustPrompt, Enabled: true, Origin: origin,
	}
	switch t := m["trust"].(type) {
	case string:
		e.Trust = t
	case map[string]any:
		e.Trust = TrustSignature
		e.Signer = &sigstore.Identity{}
		e.Signer.Issuer, _ = t["issuer"].(string)
		e.Signer.Identity, _ = t["identity"].(string)
	}
	if enabled, ok := m["enabled"].(bool); ok {
		e.Enabled = enabled
	}
	if locked, _ := m["locked"].(bool); locked && origin != OriginUser {
		e.Locked = true
	}
	var err error
	if e.source, err = expandValue(expand, e.Source); err != nil {
		e.expandErr = fmt.Errorf("source: %w", err)
	} else if e.path, err = expandValue(expand, e.Path); err != nil {
		e.expandErr = fmt.Errorf("path: %w", err)
	}
	return e
}

func expandValue(expand func(string) (string, error), s string) (string, error) {
	if expand == nil || !strings.Contains(s, "${") {
		return s, nil
	}
	return expand(s)
}

// Supported reports whether DevBox handles the kind of the entry.
func (e Entry) Supported() bool { return e.Kind == KindPlugins }

// Active reports whether the entry is loaded: supported and switched on.
func (e Entry) Active() bool { return e.Supported() && e.Enabled }

// SubPath is the directory inside the repository that holds the plugin folders, in
// slash form; empty for the root.
func (e Entry) SubPath() string {
	p := path.Clean(filepath.ToSlash(e.path))
	if p == "." {
		return ""
	}
	return p
}

// splitSource returns the kind of a source and the address to fetch from: the URL for
// git (without the "git+" prefix) and https, the directory for file.
func splitSource(source string) (kind, addr string, err error) {
	scheme, _, ok := strings.Cut(source, "://")
	if !ok {
		return "", "", fmt.Errorf("source %q has no scheme", source)
	}
	switch scheme {
	case "git+https", "git+ssh", "git+file":
		return sourceGit, strings.TrimPrefix(source, "git+"), nil
	case "https":
		return sourceHTTPS, source, nil
	case "file":
		u, err := url.Parse(source)
		if err != nil {
			return "", "", fmt.Errorf("source %q: %w", source, err)
		}
		if u.Host != "" && u.Host != "localhost" {
			return "", "", fmt.Errorf("source %q: a file source has no host (use file:///absolute/path)", source)
		}
		if !filepath.IsAbs(u.Path) {
			return "", "", fmt.Errorf("source %q: the path must be absolute (file:///absolute/path)", source)
		}
		return sourceFile, filepath.Clean(u.Path), nil
	}
	return "", "", fmt.Errorf("source %q: %s:// is not supported for plugin repositories", source, scheme)
}

// Expanded returns the source with placeholders resolved, or why it cannot be.
func (e Entry) Expanded() (string, error) { return e.source, e.expandErr }

// Problem returns why the entry cannot be synced or loaded, or nil. It is about the
// configuration only; entries of other kinds have no problem here.
func (e Entry) Problem() error {
	if !e.Supported() {
		return nil
	}
	invalid := func(format string, a ...any) error {
		return &config.Error{Code: config.CodeValidation, Detail: fmt.Sprintf(format, a...)}
	}
	if e.expandErr != nil {
		return invalid("%v", e.expandErr)
	}
	if e.Trust == TrustSignature {
		return invalid("signature trust arrives with the catalog (AP-23)")
	}
	if e.Credential != "" && !credentialName.MatchString(e.Credential) {
		return invalid("credential %q is not the name of a credential instance", e.Credential)
	}
	if e.source == "" {
		return invalid("source is required")
	}
	kind, _, err := splitSource(e.source)
	if err != nil {
		return invalid("%v", err)
	}
	if err := e.checkAccess(kind); err != nil {
		return err
	}
	if e.Ref != "" {
		if kind != sourceGit {
			return invalid("ref is only supported for git sources")
		}
		if strings.HasPrefix(e.Ref, "-") {
			return invalid("ref %q is not valid", e.Ref)
		}
	}
	if e.Trust == TrustPinned && (kind != sourceGit || !fullCommit.MatchString(e.Ref)) {
		return invalid("trust pinned needs a git source and a full 40-character commit in ref")
	}
	if e.path != "" && !validSubPath(e.path) {
		return invalid("path %q must be a relative directory inside the repository", e.path)
	}
	return nil
}

// checkAccess checks source and credential against each other: a credential belongs
// to an https address (a tarball or Git over https), and an address carries no
// password, which would end up in the configuration.
func (e Entry) checkAccess(kind string) error {
	invalid := func(format string, a ...any) error {
		return &config.Error{Code: config.CodeValidation, Detail: fmt.Sprintf(format, a...)}
	}
	https := strings.HasPrefix(e.source, "https://") || strings.HasPrefix(e.source, "git+https://")
	if https {
		if u, err := url.Parse(e.source); err == nil && u.User != nil {
			if _, hasPassword := u.User.Password(); hasPassword || e.Credential != "" {
				return invalid("source must not contain a user name or password; use credential for private repositories")
			}
		}
	}
	switch {
	case e.Credential == "" || https:
		return nil
	case strings.HasPrefix(e.source, "git+ssh://"):
		return invalid("credential is not supported for git+ssh sources (SSH keys are a separate credential type)")
	case kind == sourceFile || strings.HasPrefix(e.source, "git+file://"):
		return invalid("credential is only used with https and git+https sources")
	}
	return nil
}

func validSubPath(p string) bool {
	return !strings.Contains(p, `\`) && !strings.ContainsRune(p, 0) && filepath.IsLocal(p)
}

// ValidateNew checks the fields of an entry that is about to be written. It returns
// the same errors as Problem, for an entry of kind plugins. Whether the credential
// instance exists is not part of it (the core checks that).
func ValidateNew(name, source, ref, subPath, trust, credential string) error {
	e := Entry{
		Name: name, Kind: KindPlugins, Source: source, Ref: ref, Path: subPath, Trust: trust,
		Credential: credential, Enabled: true, Origin: OriginUser, source: source, path: subPath,
	}
	if e.Trust == "" {
		e.Trust = TrustPrompt
	}
	return e.Problem()
}
