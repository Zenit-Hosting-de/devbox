package core

import (
	"errors"
	"path/filepath"
	"strings"

	"github.com/Zenit-Hosting-de/devbox/internal/config"
	"github.com/Zenit-Hosting-de/devbox/internal/vault"
)

// ErrVaultPassphrase is returned when the vault needs a passphrase and the process
// cannot ask for one (Env.VaultPassphrase is nil, e.g. in the service).
var ErrVaultPassphrase = errors.New("the vault is protected by a passphrase, which this process cannot ask for; run the command in a terminal")

// OpenVault opens the vault backend (empty: vault.backend of the configuration). A
// vault with a passphrase asks Env.VaultPassphrase.
func (st *State) OpenVault(backend string) (vault.Backend, error) {
	passphrase := false
	configured := ""
	for _, f := range []*config.File{st.System, st.User} {
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
	var keyring vault.KeyringAPI = vault.SystemKeyring{}
	if st.Env.Keyring != nil {
		keyring = st.Env.Keyring
	}
	opts := vault.Options{
		Backend: backend, DataDir: filepath.Join(st.Ctx.XDG.Data, "devbox"),
		Keyring: keyring, Validator: st.Validator,
	}
	if passphrase {
		switch strings.ToLower(backend) {
		case vault.BackendKeyring:
			passphrase = false
		case "", vault.BackendAuto:
			// the Secret Service wins when there is one; the passphrase then has no use
			passphrase = !vault.NewKeyring(opts.Keyring).Available()
		}
	}
	if passphrase {
		if st.Env.VaultPassphrase == nil {
			return nil, &config.Error{Code: config.CodePrerequisite, Detail: ErrVaultPassphrase.Error()}
		}
		p, err := st.Env.VaultPassphrase()
		if err != nil {
			return nil, err
		}
		opts.Passphrase = p
	}
	return vault.Open(opts)
}
