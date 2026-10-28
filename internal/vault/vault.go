package vault

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Backend names (config vault.backend and the prefix of a secret reference).
const (
	BackendAuto    = "auto"
	BackendKeyring = "keyring"
	BackendAge     = "age"
)

// ErrNotFound is returned for a name without a secret.
var ErrNotFound = errors.New("vault: secret not found")

// ErrUnavailable is returned when a backend cannot be used in this session, e.g.
// no Secret Service on the session bus.
var ErrUnavailable = errors.New("vault: backend not available")

var namePattern = regexp.MustCompile(`^[A-Za-z0-9._/-]+$`)

// ValidName reports whether name can be stored (schema/vault.v1.json, entries).
func ValidName(name string) bool { return namePattern.MatchString(name) }

// Entry is one secret with its metadata.
type Entry struct {
	Secret    string
	UpdatedAt time.Time
	ExpiresAt *time.Time
}

// Backend stores secrets by name.
type Backend interface {
	// Name is BackendKeyring or BackendAge.
	Name() string
	Get(name string) (Entry, error)
	Set(name string, e Entry) error
	Delete(name string) error
	// List returns all names, sorted.
	List() ([]string, error)
}

// Ref is the reference stored in credentials.json for a name in a backend, e.g.
// keyring:devbox/firmen-registry or age:firmen-registry.
func Ref(backend, name string) string {
	if backend == BackendKeyring {
		return BackendKeyring + ":" + keyringService + "/" + name
	}
	return backend + ":" + name
}

// ParseRef splits a reference into backend and name.
func ParseRef(ref string) (backend, name string, err error) {
	backend, rest, ok := strings.Cut(ref, ":")
	switch {
	case !ok:
	case backend == BackendKeyring:
		if n, ok := strings.CutPrefix(rest, keyringService+"/"); ok && ValidName(n) {
			return backend, n, nil
		}
	case backend == BackendAge:
		if ValidName(rest) {
			return backend, rest, nil
		}
	}
	return "", "", fmt.Errorf("vault: invalid secret reference %q", ref)
}

// checkEntry validates an entry and sets UpdatedAt to now when it is unset.
func checkEntry(name string, e Entry) (Entry, error) {
	if !ValidName(name) {
		return e, fmt.Errorf("vault: invalid name %q", name)
	}
	if e.Secret == "" {
		return e, fmt.Errorf("vault: %s: empty secret", name)
	}
	if e.UpdatedAt.IsZero() {
		e.UpdatedAt = time.Now()
	}
	e.UpdatedAt = e.UpdatedAt.UTC().Truncate(time.Second)
	if e.ExpiresAt != nil {
		t := e.ExpiresAt.UTC().Truncate(time.Second)
		e.ExpiresAt = &t
	}
	return e, nil
}

// Migrate copies every entry from one backend to another and deletes it from the
// source only after all copies were written and read back. It returns the moved
// names. A failure leaves every entry readable in at least one backend.
func Migrate(from, to Backend) ([]string, error) {
	names, err := from.List()
	if err != nil {
		return nil, fmt.Errorf("vault: migrate: %w", err)
	}
	for _, name := range names {
		e, err := from.Get(name)
		if err != nil {
			return nil, fmt.Errorf("vault: migrate %s: %w", name, err)
		}
		if err := to.Set(name, e); err != nil {
			return nil, fmt.Errorf("vault: migrate %s: %w", name, err)
		}
		got, err := to.Get(name)
		if err != nil || got.Secret != e.Secret {
			return nil, fmt.Errorf("vault: migrate %s: read-back failed: %w", name, errors.Join(err, ErrNotFound))
		}
	}
	var errs []error
	for _, name := range names {
		if err := from.Delete(name); err != nil && !errors.Is(err, ErrNotFound) {
			errs = append(errs, fmt.Errorf("%s: %w", name, err))
		}
	}
	if err := errors.Join(errs...); err != nil {
		return names, fmt.Errorf("vault: migrate: entries copied, removing from %s: %w", from.Name(), err)
	}
	return names, nil
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
