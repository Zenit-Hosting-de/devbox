package vault

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"filippo.io/age"

	"github.com/Zenit-Hosting-de/devbox/internal/jsonfile"
	"github.com/Zenit-Hosting-de/devbox/internal/schema"
)

// scryptWorkFactor overrides the age default (log2 of the scrypt N) when set; tests
// lower it.
var scryptWorkFactor int

// AgeFile stores all secrets in one age-encrypted JSON file (schema/vault.v1.json).
type AgeFile struct {
	path      string
	identity  age.Identity
	recipient age.Recipient
	validator *schema.Validator

	// cache holds the decrypted entries while the file is unchanged, because a
	// passphrase costs one scrypt derivation per decryption.
	mu    sync.Mutex
	cache map[string]Entry
	stamp fileStamp
}

// fileStamp identifies a version of the vault file. Writes replace the file by
// rename, so the inode changes with every write, also within one mtime tick.
type fileStamp struct {
	ino     uint64
	size    int64
	modTime time.Time
	ok      bool
}

func stampOf(path string) fileStamp {
	fi, err := os.Stat(path)
	if err != nil {
		return fileStamp{}
	}
	st := fileStamp{size: fi.Size(), modTime: fi.ModTime(), ok: true}
	if sys, ok := fi.Sys().(*syscall.Stat_t); ok {
		st.ino = sys.Ino
	}
	return st
}

func copyEntries(in map[string]Entry) map[string]Entry {
	out := make(map[string]Entry, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

// NewAgeKeyFile uses the X25519 key in keyPath, created with mode 0600 on first use.
// The key file uses the age format, so `age -d -i vault.key vault.age` can recover
// the secrets without DevBox.
func NewAgeKeyFile(path, keyPath string, v *schema.Validator) (*AgeFile, error) {
	id, err := loadOrCreateKey(keyPath)
	if err != nil {
		return nil, err
	}
	return &AgeFile{path: path, identity: id, recipient: id.Recipient(), validator: v}, nil
}

// NewAgePassphrase derives the key from a passphrase (age scrypt).
func NewAgePassphrase(path, passphrase string, v *schema.Validator) (*AgeFile, error) {
	if passphrase == "" {
		return nil, errors.New("vault: empty passphrase")
	}
	r, err := age.NewScryptRecipient(passphrase)
	if err != nil {
		return nil, fmt.Errorf("vault: %w", err)
	}
	id, err := age.NewScryptIdentity(passphrase)
	if err != nil {
		return nil, fmt.Errorf("vault: %w", err)
	}
	if scryptWorkFactor > 0 {
		r.SetWorkFactor(scryptWorkFactor)
	}
	return &AgeFile{path: path, identity: id, recipient: r, validator: v}, nil
}

func loadOrCreateKey(keyPath string) (*age.X25519Identity, error) {
	data, err := os.ReadFile(keyPath) //nolint:gosec // key path from the data directory
	if errors.Is(err, fs.ErrNotExist) {
		id, err := age.GenerateX25519Identity()
		if err != nil {
			return nil, fmt.Errorf("vault: %w", err)
		}
		if err := os.MkdirAll(filepath.Dir(keyPath), 0o700); err != nil {
			return nil, fmt.Errorf("vault: %w", err)
		}
		if err := jsonfile.WriteAtomic(keyPath, []byte(id.String()+"\n"), 0o600); err != nil {
			return nil, fmt.Errorf("vault: %w", err)
		}
		return id, nil
	}
	if err != nil {
		return nil, fmt.Errorf("vault: %w", err)
	}
	if fi, err := os.Stat(keyPath); err != nil || fi.Mode().Perm()&0o077 != 0 {
		return nil, fmt.Errorf("vault: key file %s must have mode 0600", keyPath)
	}
	ids, err := age.ParseIdentities(bytes.NewReader(data))
	if err != nil || len(ids) != 1 {
		return nil, fmt.Errorf("vault: key file %s: want one age identity", keyPath)
	}
	id, ok := ids[0].(*age.X25519Identity)
	if !ok {
		return nil, fmt.Errorf("vault: key file %s: want an X25519 identity", keyPath)
	}
	return id, nil
}

// Name implements Backend.
func (a *AgeFile) Name() string { return BackendAge }

// file is the decrypted content, keys in schema order.
type file struct {
	Entries map[string]fileEntry `json:"entries"`
}

type fileEntry struct {
	Secret    string  `json:"secret"`
	UpdatedAt string  `json:"updatedAt"`
	ExpiresAt *string `json:"expiresAt,omitempty"`
}

func (a *AgeFile) read() (map[string]Entry, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	st := stampOf(a.path)
	if a.cache != nil && st == a.stamp {
		return copyEntries(a.cache), nil
	}
	entries, err := a.decrypt()
	if err != nil {
		return nil, err
	}
	a.cache, a.stamp = copyEntries(entries), st
	return entries, nil
}

func (a *AgeFile) decrypt() (map[string]Entry, error) {
	enc, err := os.ReadFile(a.path)
	if errors.Is(err, fs.ErrNotExist) {
		return map[string]Entry{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("vault: %w", err)
	}
	r, err := age.Decrypt(bytes.NewReader(enc), a.identity)
	if err != nil {
		return nil, fmt.Errorf("vault: decrypt %s: %w", a.path, err)
	}
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("vault: decrypt %s: %w", a.path, err)
	}
	if a.validator != nil {
		if err := a.validator.ValidateJSON("vault", data); err != nil {
			return nil, fmt.Errorf("vault: %s: %w", a.path, err)
		}
	}
	var f file
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("vault: %s: %w", a.path, err)
	}
	out := make(map[string]Entry, len(f.Entries))
	for name, fe := range f.Entries {
		e := Entry{Secret: fe.Secret}
		if e.UpdatedAt, err = time.Parse(time.RFC3339, fe.UpdatedAt); err != nil {
			return nil, fmt.Errorf("vault: %s: %s: %w", a.path, name, err)
		}
		if fe.ExpiresAt != nil {
			t, err := time.Parse(time.RFC3339, *fe.ExpiresAt)
			if err != nil {
				return nil, fmt.Errorf("vault: %s: %s: %w", a.path, name, err)
			}
			e.ExpiresAt = &t
		}
		out[name] = e
	}
	return out, nil
}

func (a *AgeFile) write(entries map[string]Entry) error {
	f := file{Entries: make(map[string]fileEntry, len(entries))}
	for name, e := range entries {
		fe := fileEntry{Secret: e.Secret, UpdatedAt: e.UpdatedAt.UTC().Format(time.RFC3339)}
		if e.ExpiresAt != nil {
			s := e.ExpiresAt.UTC().Format(time.RFC3339)
			fe.ExpiresAt = &s
		}
		f.Entries[name] = fe
	}
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	if a.validator != nil {
		if err := a.validator.ValidateJSON("vault", data); err != nil {
			return fmt.Errorf("vault: %w", err)
		}
	}
	var buf bytes.Buffer
	w, err := age.Encrypt(&buf, a.recipient)
	if err != nil {
		return fmt.Errorf("vault: encrypt: %w", err)
	}
	if _, err := w.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("vault: encrypt: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("vault: encrypt: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(a.path), 0o700); err != nil {
		return fmt.Errorf("vault: %w", err)
	}
	if err := jsonfile.WriteAtomic(a.path, buf.Bytes(), 0o600); err != nil {
		return fmt.Errorf("vault: %w", err)
	}
	a.mu.Lock()
	a.cache, a.stamp = copyEntries(entries), stampOf(a.path)
	a.mu.Unlock()
	return nil
}

// lock serialises read-modify-write cycles across processes.
func (a *AgeFile) lock() (func(), error) {
	if err := os.MkdirAll(filepath.Dir(a.path), 0o700); err != nil {
		return nil, fmt.Errorf("vault: %w", err)
	}
	f, err := os.OpenFile(a.path+".lock", os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, fmt.Errorf("vault: %w", err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil { //nolint:gosec // file descriptors fit int
		_ = f.Close()
		return nil, fmt.Errorf("vault: lock: %w", err)
	}
	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN) //nolint:gosec // file descriptors fit int
		_ = f.Close()
	}, nil
}

// Get implements Backend.
func (a *AgeFile) Get(name string) (Entry, error) {
	entries, err := a.read()
	if err != nil {
		return Entry{}, err
	}
	e, ok := entries[name]
	if !ok {
		return Entry{}, ErrNotFound
	}
	return e, nil
}

// Set implements Backend.
func (a *AgeFile) Set(name string, e Entry) error {
	e, err := checkEntry(name, e)
	if err != nil {
		return err
	}
	return a.update(func(entries map[string]Entry) error {
		entries[name] = e
		return nil
	})
}

// Delete implements Backend.
func (a *AgeFile) Delete(name string) error {
	return a.update(func(entries map[string]Entry) error {
		if _, ok := entries[name]; !ok {
			return ErrNotFound
		}
		delete(entries, name)
		return nil
	})
}

func (a *AgeFile) update(change func(map[string]Entry) error) error {
	unlock, err := a.lock()
	if err != nil {
		return err
	}
	defer unlock()
	entries, err := a.read()
	if err != nil {
		return err
	}
	if err := change(entries); err != nil {
		return err
	}
	return a.write(entries)
}

// List implements Backend.
func (a *AgeFile) List() ([]string, error) {
	entries, err := a.read()
	if err != nil {
		return nil, err
	}
	return sortedKeys(entries), nil
}

// Options select and configure a backend (config key vault).
type Options struct {
	// Backend is auto, keyring or age; empty means auto.
	Backend string
	// DataDir is ~/.local/share/devbox, home of vault.age and vault.key.
	DataDir string
	// Passphrase, when set, derives the age key instead of using vault.key.
	Passphrase string
	Keyring    KeyringAPI
	Validator  *schema.Validator
}

// Open returns the backend for the options: auto takes the keyring when the session
// provides a Secret Service, otherwise the age file.
func Open(o Options) (Backend, error) {
	kr := NewKeyring(o.Keyring)
	switch strings.ToLower(o.Backend) {
	case "", BackendAuto:
		if o.Keyring != nil && kr.Available() {
			return kr, nil
		}
		return o.openAge()
	case BackendKeyring:
		if o.Keyring == nil || !kr.Available() {
			return nil, fmt.Errorf("%w: no Secret Service in this session", ErrUnavailable)
		}
		return kr, nil
	case BackendAge:
		return o.openAge()
	}
	return nil, fmt.Errorf("vault: unknown backend %q", o.Backend)
}

func (o Options) openAge() (*AgeFile, error) {
	path := filepath.Join(o.DataDir, "vault.age")
	if o.Passphrase != "" {
		return NewAgePassphrase(path, o.Passphrase, o.Validator)
	}
	return NewAgeKeyFile(path, filepath.Join(o.DataDir, "vault.key"), o.Validator)
}
