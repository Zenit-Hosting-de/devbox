package vault

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zalando/go-keyring"

	"github.com/Zenit-Hosting-de/devbox/internal/schema"
)

// fakeKeyring is an in-memory Secret Service.
type fakeKeyring struct {
	mu          sync.Mutex
	items       map[string]string
	unavailable bool
	failSet     string // user name whose Set fails
}

func newFake() *fakeKeyring { return &fakeKeyring{items: map[string]string{}} }

func (f *fakeKeyring) Get(service, user string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.unavailable {
		return "", errors.New("org.freedesktop.secrets: no such name")
	}
	v, ok := f.items[service+"/"+user]
	if !ok {
		return "", keyring.ErrNotFound
	}
	return v, nil
}

func (f *fakeKeyring) Set(service, user, password string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.unavailable || user == f.failSet {
		return errors.New("org.freedesktop.secrets: write failed")
	}
	f.items[service+"/"+user] = password
	return nil
}

func (f *fakeKeyring) Delete(service, user string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.items[service+"/"+user]; !ok {
		return keyring.ErrNotFound
	}
	delete(f.items, service+"/"+user)
	return nil
}

func TestMain(m *testing.M) {
	scryptWorkFactor = 10 // fast key derivation in tests
	os.Exit(m.Run())
}

func validator(t *testing.T) *schema.Validator {
	t.Helper()
	v, err := schema.New()
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func newAge(t *testing.T) *AgeFile {
	t.Helper()
	dir := t.TempDir()
	a, err := NewAgeKeyFile(filepath.Join(dir, "vault.age"), filepath.Join(dir, "vault.key"), validator(t))
	if err != nil {
		t.Fatal(err)
	}
	return a
}

var (
	updated = time.Date(2026, 10, 27, 9, 12, 0, 0, time.UTC)
	expires = time.Date(2027, 1, 25, 0, 0, 0, 0, time.UTC)
)

func TestBackends(t *testing.T) {
	backends := map[string]func(t *testing.T) Backend{
		"keyring": func(*testing.T) Backend { return NewKeyring(newFake()) },
		"age":     func(t *testing.T) Backend { return newAge(t) },
		"age-passphrase": func(t *testing.T) Backend {
			a, err := NewAgePassphrase(filepath.Join(t.TempDir(), "vault.age"), "correct horse", validator(t))
			if err != nil {
				t.Fatal(err)
			}
			return a
		},
	}
	for name, mk := range backends {
		t.Run(name, func(t *testing.T) {
			b := mk(t)
			if _, err := b.Get("firmen-registry"); !errors.Is(err, ErrNotFound) {
				t.Fatalf("Get on empty = %v", err)
			}
			if err := b.Set("firmen-registry", Entry{Secret: "s3cret", UpdatedAt: updated, ExpiresAt: &expires}); err != nil {
				t.Fatal(err)
			}
			if err := b.Set("team/git", Entry{Secret: "tok"}); err != nil {
				t.Fatal(err)
			}
			got, err := b.Get("firmen-registry")
			if err != nil || got.Secret != "s3cret" || !got.UpdatedAt.Equal(updated) || got.ExpiresAt == nil || !got.ExpiresAt.Equal(expires) {
				t.Fatalf("Get = %+v, %v", got, err)
			}
			if g, _ := b.Get("team/git"); g.ExpiresAt != nil || g.UpdatedAt.IsZero() {
				t.Errorf("entry without expiry = %+v", g)
			}
			if names, err := b.List(); err != nil || strings.Join(names, ",") != "firmen-registry,team/git" {
				t.Errorf("List = %v, %v", names, err)
			}
			if err := b.Set("firmen-registry", Entry{Secret: "new"}); err != nil {
				t.Fatal(err)
			}
			if g, _ := b.Get("firmen-registry"); g.Secret != "new" || g.ExpiresAt != nil {
				t.Errorf("after overwrite = %+v", g)
			}
			if err := b.Delete("firmen-registry"); err != nil {
				t.Fatal(err)
			}
			if err := b.Delete("firmen-registry"); !errors.Is(err, ErrNotFound) {
				t.Errorf("second Delete = %v", err)
			}
			if names, _ := b.List(); strings.Join(names, ",") != "team/git" {
				t.Errorf("List after delete = %v", names)
			}
			for _, bad := range []struct{ name, secret string }{{"a b", "x"}, {":index", "x"}, {"", "x"}, {"ok", ""}} {
				if err := b.Set(bad.name, Entry{Secret: bad.secret}); err == nil {
					t.Errorf("Set(%q, %q) succeeded", bad.name, bad.secret)
				}
			}
		})
	}
}

func TestAgeFileModesAndEncryption(t *testing.T) {
	dir := t.TempDir()
	a, err := NewAgeKeyFile(filepath.Join(dir, "vault.age"), filepath.Join(dir, "vault.key"), validator(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Set("firmen-git", Entry{Secret: "glpat-0123456789"}); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"vault.age", "vault.key"} {
		fi, err := os.Stat(filepath.Join(dir, f))
		if err != nil || fi.Mode().Perm() != 0o600 {
			t.Errorf("%s: mode %v, %v", f, fi.Mode().Perm(), err)
		}
	}
	data, _ := os.ReadFile(filepath.Join(dir, "vault.age"))
	if strings.Contains(string(data), "glpat") || strings.Contains(string(data), "entries") {
		t.Error("vault.age contains plain text")
	}

	// the same key file opens the vault again
	b, err := NewAgeKeyFile(filepath.Join(dir, "vault.age"), filepath.Join(dir, "vault.key"), validator(t))
	if err != nil {
		t.Fatal(err)
	}
	if e, err := b.Get("firmen-git"); err != nil || e.Secret != "glpat-0123456789" {
		t.Errorf("reopen = %+v, %v", e, err)
	}

	// a key readable by others is refused
	if err := os.Chmod(filepath.Join(dir, "vault.key"), 0o644); err != nil { //nolint:gosec // the test needs a too open key file
		t.Fatal(err)
	}
	if _, err := NewAgeKeyFile(filepath.Join(dir, "vault.age"), filepath.Join(dir, "vault.key"), nil); err == nil || !strings.Contains(err.Error(), "0600") {
		t.Errorf("open with 0644 key = %v", err)
	}
}

func TestAgePassphraseMismatch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vault.age")
	a, _ := NewAgePassphrase(path, "right", nil)
	if err := a.Set("x", Entry{Secret: "y"}); err != nil {
		t.Fatal(err)
	}
	b, _ := NewAgePassphrase(path, "wrong", nil)
	if _, err := b.Get("x"); err == nil || !strings.Contains(err.Error(), "decrypt") {
		t.Errorf("wrong passphrase = %v", err)
	}
	if _, err := NewAgePassphrase(path, "", nil); err == nil {
		t.Error("empty passphrase accepted")
	}
}

func fill(t *testing.T, b Backend, n int) map[string]Entry {
	t.Helper()
	want := map[string]Entry{}
	for i := range n {
		name := "cred-" + string(rune('a'+i))
		e := Entry{Secret: "secret-" + name, UpdatedAt: updated.Add(time.Duration(i) * time.Hour)}
		if i%2 == 0 {
			e.ExpiresAt = &expires
		}
		if err := b.Set(name, e); err != nil {
			t.Fatal(err)
		}
		want[name] = e
	}
	return want
}

func TestMigrateKeepsEveryEntry(t *testing.T) {
	tests := []struct {
		name     string
		from, to func(t *testing.T) Backend
	}{
		{"keyring to age", func(*testing.T) Backend { return NewKeyring(newFake()) }, func(t *testing.T) Backend { return newAge(t) }},
		{"age to keyring", func(t *testing.T) Backend { return newAge(t) }, func(*testing.T) Backend { return NewKeyring(newFake()) }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			from, to := tc.from(t), tc.to(t)
			want := fill(t, from, 5)
			moved, err := Migrate(from, to)
			if err != nil || len(moved) != 5 {
				t.Fatalf("Migrate = %v, %v", moved, err)
			}
			for name, w := range want {
				g, err := to.Get(name)
				if err != nil || g.Secret != w.Secret || !g.UpdatedAt.Equal(w.UpdatedAt) || (g.ExpiresAt == nil) != (w.ExpiresAt == nil) {
					t.Errorf("%s after migrate = %+v, %v", name, g, err)
				}
			}
			if left, _ := from.List(); len(left) != 0 {
				t.Errorf("source still has %v", left)
			}
		})
	}
}

func TestMigrateFailureKeepsSource(t *testing.T) {
	from := newAge(t)
	want := fill(t, from, 3)
	fk := newFake()
	fk.failSet = "cred-b"
	if _, err := Migrate(from, NewKeyring(fk)); err == nil {
		t.Fatal("Migrate succeeded with a failing target")
	}
	for name := range want {
		if _, err := from.Get(name); err != nil {
			t.Errorf("%s lost from source: %v", name, err)
		}
	}
}

func TestOpen(t *testing.T) {
	v := validator(t)
	down := newFake()
	down.unavailable = true
	tests := []struct {
		name    string
		backend string
		kr      KeyringAPI
		want    string
		wantErr bool
	}{
		{"auto with Secret Service", "auto", newFake(), BackendKeyring, false},
		{"auto without Secret Service", "auto", down, BackendAge, false},
		{"auto without keyring API", "", nil, BackendAge, false},
		{"keyring forced", "keyring", newFake(), BackendKeyring, false},
		{"keyring forced but missing", "keyring", down, "", true},
		{"age forced", "age", newFake(), BackendAge, false},
		{"unknown", "gpg", nil, "", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			b, err := Open(Options{Backend: tc.backend, DataDir: t.TempDir(), Keyring: tc.kr, Validator: v})
			if tc.wantErr {
				if err == nil {
					t.Fatalf("Open = %v, want error", b.Name())
				}
				return
			}
			if err != nil || b.Name() != tc.want {
				t.Fatalf("Open = %v, %v", b, err)
			}
		})
	}
}

func TestRefs(t *testing.T) {
	tests := []struct {
		backend, name, ref string
	}{
		{BackendKeyring, "firmen-registry", "keyring:devbox/firmen-registry"},
		{BackendAge, "firmen-registry", "age:firmen-registry"},
		{BackendAge, "team/git", "age:team/git"},
	}
	for _, tc := range tests {
		if got := Ref(tc.backend, tc.name); got != tc.ref {
			t.Errorf("Ref = %s, want %s", got, tc.ref)
		}
		b, n, err := ParseRef(tc.ref)
		if err != nil || b != tc.backend || n != tc.name {
			t.Errorf("ParseRef(%s) = %s %s %v", tc.ref, b, n, err)
		}
	}
	for _, bad := range []string{"keyring:other/x", "age:", "plain", "vault:x", "age:a b"} {
		if _, _, err := ParseRef(bad); err == nil {
			t.Errorf("ParseRef(%q) accepted", bad)
		}
	}
}
