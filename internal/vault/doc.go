// Package vault stores secrets in the Secret Service of the desktop session (GNOME
// Keyring, KWallet) or, without one, in the age-encrypted file vault.age; Migrate
// moves all entries when vault.backend changes (docs/concept.md, "Zugangsdaten").
package vault
