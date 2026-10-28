package vault

import (
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/zalando/go-keyring"
)

// keyringService is the Secret Service attribute "service" of every DevBox item.
const keyringService = "devbox"

// indexItem lists the stored names, because the Secret Service API of go-keyring
// cannot enumerate items. ":" is not allowed in names, so it cannot collide.
const indexItem = ":index"

// KeyringAPI is the subset of go-keyring the backend uses; tests pass a fake.
type KeyringAPI interface {
	Get(service, user string) (string, error)
	Set(service, user, password string) error
	Delete(service, user string) error
}

// SystemKeyring is the Secret Service of the session (GNOME Keyring, KWallet).
type SystemKeyring struct{}

// Get implements KeyringAPI.
func (SystemKeyring) Get(service, user string) (string, error) { return keyring.Get(service, user) }

// Set implements KeyringAPI.
func (SystemKeyring) Set(service, user, password string) error {
	return keyring.Set(service, user, password)
}

// Delete implements KeyringAPI.
func (SystemKeyring) Delete(service, user string) error { return keyring.Delete(service, user) }

// Keyring stores each secret as one Secret Service item.
type Keyring struct {
	api KeyringAPI
	mu  sync.Mutex
}

// NewKeyring returns the keyring backend. Available reports whether it works.
func NewKeyring(api KeyringAPI) *Keyring { return &Keyring{api: api} }

// Name implements Backend.
func (k *Keyring) Name() string { return BackendKeyring }

// Available probes the Secret Service: a missing item is fine, any other error
// (no session bus, no provider) is not.
func (k *Keyring) Available() bool {
	_, err := k.api.Get(keyringService, indexItem)
	return err == nil || errors.Is(err, keyring.ErrNotFound)
}

// item is the JSON stored as the password of a keyring item.
type item struct {
	Secret    string     `json:"secret"`
	UpdatedAt time.Time  `json:"updatedAt"`
	ExpiresAt *time.Time `json:"expiresAt"`
}

// Get implements Backend.
func (k *Keyring) Get(name string) (Entry, error) {
	raw, err := k.api.Get(keyringService, name)
	if errors.Is(err, keyring.ErrNotFound) {
		return Entry{}, ErrNotFound
	}
	if err != nil {
		return Entry{}, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	var it item
	if err := json.Unmarshal([]byte(raw), &it); err != nil || it.Secret == "" {
		return Entry{}, fmt.Errorf("vault: keyring item %s is not a DevBox entry", name)
	}
	return Entry(it), nil
}

// Set implements Backend.
func (k *Keyring) Set(name string, e Entry) error {
	e, err := checkEntry(name, e)
	if err != nil {
		return err
	}
	data, err := json.Marshal(item(e)) //nolint:gosec // the keyring item is where the secret belongs
	if err != nil {
		return err
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	if err := k.api.Set(keyringService, name, string(data)); err != nil {
		return fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	return k.updateIndex(func(names map[string]bool) { names[name] = true })
}

// Delete implements Backend.
func (k *Keyring) Delete(name string) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	err := k.api.Delete(keyringService, name)
	if err != nil && !errors.Is(err, keyring.ErrNotFound) {
		return fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	if ierr := k.updateIndex(func(names map[string]bool) { delete(names, name) }); ierr != nil {
		return ierr
	}
	if err != nil {
		return ErrNotFound
	}
	return nil
}

// List implements Backend.
func (k *Keyring) List() ([]string, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	names, err := k.index()
	if err != nil {
		return nil, err
	}
	return sortedKeys(names), nil
}

func (k *Keyring) index() (map[string]bool, error) {
	raw, err := k.api.Get(keyringService, indexItem)
	if errors.Is(err, keyring.ErrNotFound) {
		return map[string]bool{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	var list []string
	if err := json.Unmarshal([]byte(raw), &list); err != nil {
		return nil, fmt.Errorf("vault: keyring index: %w", err)
	}
	names := map[string]bool{}
	for _, n := range list {
		names[n] = true
	}
	return names, nil
}

func (k *Keyring) updateIndex(change func(map[string]bool)) error {
	names, err := k.index()
	if err != nil {
		return err
	}
	change(names)
	data, err := json.Marshal(sortedKeys(names))
	if err != nil {
		return err
	}
	if err := k.api.Set(keyringService, indexItem, string(data)); err != nil {
		return fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	return nil
}
