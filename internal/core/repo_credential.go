package core

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/Zenit-Hosting-de/devbox/internal/config"
	"github.com/Zenit-Hosting-de/devbox/internal/credentials"
	"github.com/Zenit-Hosting-de/devbox/internal/repo"
	"github.com/Zenit-Hosting-de/devbox/internal/vault"
)

// Field names a credential type of a repository defines (plugins/registry and
// plugins/githost): the address of the host and the user name.
const (
	credFieldURL      = "url"
	credFieldUsername = "username"
)

// credentialInstance returns the instance and the definition of its type, if the
// instance may be used for a repository.
func (st *State) credentialInstance(name string) (*credentials.Instance, map[string]any, error) {
	list, err := Credentials(st)
	if err != nil {
		return nil, nil, err
	}
	inst, ok := credentials.Find(list, name)
	if !ok {
		return nil, nil, &config.Error{Code: config.CodeNotFound, Key: name, Detail: "credential " + name + " does not exist; create it with devbox cred add"}
	}
	if !repo.AllowedCredentialType(inst.Type) {
		return nil, nil, &config.Error{Code: config.CodeValidation, Key: name, Detail: fmt.Sprintf("credential %s is of the type %s; repositories take %s or %s", name, inst.Type, repo.CredentialTypeGitHost, repo.CredentialTypeRegistry)}
	}
	for _, m := range st.Manifests { // search order: the first definition of a type wins
		types, _ := m.Doc["credentialTypes"].(map[string]any)
		if def, ok := types[inst.Type].(map[string]any); ok {
			return inst, def, nil
		}
	}
	return nil, nil, &config.Error{Code: config.CodeNotFound, Key: name, Detail: fmt.Sprintf("no plugin defines the credential type %s", inst.Type)}
}

// CheckRepositoryCredential reports whether the instance exists and is of a type a
// repository may use. It does not read the secret.
func (st *State) CheckRepositoryCredential(name string) error {
	_, _, err := st.credentialInstance(name)
	return err
}

// repositoryCredential resolves the credential instance of a repository for one sync:
// address, authentication scheme and user name from the instance and its type, the
// secret from the vault. The result goes only to the sync; the secret is in no log,
// no state and no event.
func (st *State) repositoryCredential(_ context.Context, name string) (*repo.Credential, error) {
	inst, def, err := st.credentialInstance(name)
	if err != nil {
		return nil, err
	}
	if inst.Secret == nil {
		return nil, &config.Error{Code: config.CodePrerequisite, Key: name, Detail: "credential " + name + " has no secret; set one with devbox cred set " + name}
	}
	fields := st.credentialFields(inst, def)
	address, _ := fields[credFieldURL].(string)
	u, err := url.Parse(address)
	if err != nil || u.Hostname() == "" {
		return nil, &config.Error{Code: config.CodeValidation, Key: name, Detail: "credential " + name + " has no valid address (field url)"}
	}
	username, _ := fields[credFieldUsername].(string)
	c := &repo.Credential{
		Name: name, Type: inst.Type, Host: strings.ToLower(u.Hostname()), Port: u.Port(),
		Scheme: httpScheme(def, fields), Username: username,
	}
	backend, secretName, err := vault.ParseRef(inst.Secret.Ref)
	if err != nil {
		return nil, &config.Error{Code: config.CodeValidation, Key: name, Detail: "credential " + name + ": " + err.Error()}
	}
	b, err := st.OpenVault(backend)
	if err != nil {
		return nil, fmt.Errorf("credential %s: %w", name, err)
	}
	entry, err := b.Get(secretName)
	if err != nil {
		return nil, &config.Error{Code: config.CodePrerequisite, Key: name, Detail: fmt.Sprintf("credential %s: the secret cannot be read from the vault: %v", name, err)}
	}
	c.Secret = entry.Secret
	return c, nil
}

// credentialFields returns the fields of the instance with the defaults of the type
// for the fields it does not set.
func (st *State) credentialFields(inst *credentials.Instance, def map[string]any) map[string]any {
	out := map[string]any{}
	defs, _ := def["fields"].(map[string]any)
	for key, raw := range defs {
		field, _ := raw.(map[string]any)
		if d, ok := field["default"]; ok {
			if v, err := config.ExpandValue(d, st.Lookup()); err == nil {
				out[key] = v
			}
		}
	}
	for key, v := range inst.Fields {
		if s, ok := v.(string); ok && s == "" {
			continue
		}
		out[key] = v
	}
	return out
}

// httpScheme is the HTTP authentication scheme the credential type defines
// (httpAuth in schema/plugin.v1.json) for the fields of an instance.
func httpScheme(def map[string]any, fields map[string]any) string {
	auth, _ := def["httpAuth"].(map[string]any)
	scheme, _ := auth["scheme"].(string)
	if by, _ := auth["by"].(string); by != "" {
		schemes, _ := auth["schemes"].(map[string]any)
		if s, ok := schemes[fmt.Sprint(fields[by])].(string); ok {
			scheme = s
		}
	}
	if userField, _ := auth["userField"].(string); userField != "" && scheme != repo.SchemePrivateToken {
		if u, _ := fields[userField].(string); u != "" {
			scheme = repo.SchemeBasic
		}
	}
	if scheme == "" {
		scheme = repo.SchemeBearer
	}
	return scheme
}
