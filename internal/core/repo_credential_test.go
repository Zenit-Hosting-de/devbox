package core

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Zenit-Hosting-de/devbox/internal/config"
	"github.com/Zenit-Hosting-de/devbox/internal/credentials"
	"github.com/Zenit-Hosting-de/devbox/internal/repo"
	"github.com/Zenit-Hosting-de/devbox/internal/vault"
)

const secretToken = "t0k3n-for-tests"

// credentialHome prepares a home directory with an age vault and these instances:
// company-git (git-host, with secret), gitlab-reg and nexus-reg (registry, with
// secret), empty-git (git-host without secret), ssh-key (not allowed).
func credentialHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	write(t, filepath.Join(home, ".config", "devbox", "config.json"), `{ "vault": { "backend": "age" } }`, 0o600)
	st := loadAt(t, home)
	b, err := st.OpenVault("age")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"company-git", "gitlab-reg", "nexus-reg"} {
		if err := b.Set(name, vault.Entry{Secret: secretToken + "-" + name}); err != nil {
			t.Fatal(err)
		}
	}
	secret := func(name string) *credentials.Secret { return &credentials.Secret{Ref: vault.Ref("age", name)} }
	f := &credentials.File{Instances: map[string]*credentials.Stored{
		"company-git": {Type: "git-host", Origin: "user", Fields: map[string]any{"url": "https://Git.Example.com:8443/", "flavor": "gitlab"}, Secret: secret("company-git")},
		"gitlab-reg":  {Type: "registry", Origin: "user", Fields: map[string]any{"url": "https://repo.example.com", "layout": "gitlab"}, Secret: secret("gitlab-reg")},
		"nexus-reg":   {Type: "registry", Origin: "user", Fields: map[string]any{"url": "https://repo.example.com", "layout": "nexus", "username": "alice"}, Secret: secret("nexus-reg")},
		"empty-git":   {Type: "git-host", Origin: "user", Fields: map[string]any{"url": "https://git.example.com"}},
		"ssh-key":     {Type: "ssh-key", Origin: "user", Fields: map[string]any{"path": "/x"}},
	}}
	if err := credentials.Save(st.Validator, st.CredentialsPath(), f); err != nil {
		t.Fatal(err)
	}
	return home
}

func TestRepositoryCredentialResolves(t *testing.T) {
	st := loadAt(t, credentialHome(t))
	ctx := context.Background()

	c, err := st.repositoryCredential(ctx, "company-git")
	if err != nil {
		t.Fatal(err)
	}
	want := repo.Credential{
		Name: "company-git", Type: "git-host", Host: "git.example.com", Port: "8443",
		Scheme: repo.SchemePrivateToken, Username: "oauth2", Secret: secretToken + "-company-git",
	}
	if *c != want {
		t.Errorf("git-host = %+v, want %+v", *c, want)
	}

	// a registry decides by layout and user name, like the registry plugin does
	for name, scheme := range map[string]string{"gitlab-reg": repo.SchemePrivateToken, "nexus-reg": repo.SchemeBasic} {
		c, err := st.repositoryCredential(ctx, name)
		if err != nil {
			t.Fatal(err)
		}
		if c.Scheme != scheme || c.Host != "repo.example.com" || c.Secret != secretToken+"-"+name {
			t.Errorf("%s = %+v", name, *c)
		}
	}
}

func TestRepositoryCredentialErrors(t *testing.T) {
	st := loadAt(t, credentialHome(t))
	ctx := context.Background()
	tests := []struct {
		name string
		code string
		want string
	}{
		{"no-such", config.CodeNotFound, "does not exist"},
		{"ssh-key", config.CodeValidation, "repositories take git-host or registry"},
		{"empty-git", config.CodePrerequisite, "has no secret"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := st.repositoryCredential(ctx, tt.name)
			if !config.IsCode(err, tt.code) || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error = %v, want code %s and %q", err, tt.code, tt.want)
			}
		})
	}

	// the vault lost the secret: the error names the instance, not a secret
	b, err := st.OpenVault("age")
	if err != nil {
		t.Fatal(err)
	}
	if err := b.Delete("company-git"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.repositoryCredential(ctx, "company-git"); !config.IsCode(err, config.CodePrerequisite) || strings.Contains(err.Error(), secretToken) {
		t.Errorf("secret gone: %v", err)
	}
}

func TestRepositoryCredentialWithPassphraseVault(t *testing.T) {
	home := t.TempDir()
	write(t, filepath.Join(home, ".config", "devbox", "config.json"), `{ "vault": { "backend": "age", "passphrase": true } }`, 0o600)
	st := loadAt(t, home)
	// the service cannot ask for a passphrase
	if _, err := st.OpenVault("age"); !config.IsCode(err, config.CodePrerequisite) || !strings.Contains(err.Error(), "passphrase") {
		t.Errorf("without a way to ask: %v", err)
	}
	asked := 0
	st.Env.VaultPassphrase = func() (string, error) { asked++; return "pass", nil }
	if _, err := st.OpenVault("age"); err != nil || asked != 1 {
		t.Errorf("OpenVault: %v, asked %d times", err, asked)
	}
}

func readRepository(t *testing.T, home, name string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(home, ".config", "devbox", "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Repositories map[string]map[string]any `json:"repositories"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	return doc.Repositories[name]
}

func TestRepositoryCredentialInConfiguration(t *testing.T) {
	home := credentialHome(t)
	spec := RepoSpec{Name: "private", Source: "git+https://git.example.com:8443/team/p.git", Credential: "company-git"}

	for name, tt := range map[string]struct {
		mut  func(*RepoSpec)
		code string
		want string
	}{
		"unknown instance": {func(s *RepoSpec) { s.Credential = "no-such" }, config.CodeNotFound, "does not exist"},
		"wrong type":       {func(s *RepoSpec) { s.Credential = "ssh-key" }, config.CodeValidation, "git-host or registry"},
		"ssh source":       {func(s *RepoSpec) { s.Source = "git+ssh://git@git.example.com/p.git" }, config.CodeValidation, "git+ssh"},
		"local source":     {func(s *RepoSpec) { s.Source = "file:///srv/plugins" }, config.CodeValidation, "only used with https"},
		"bad name":         {func(s *RepoSpec) { s.Credential = "Company Git" }, config.CodeValidation, "not the name"},
	} {
		t.Run(name, func(t *testing.T) {
			s := spec
			tt.mut(&s)
			err := loadAt(t, home).AddRepository(s)
			if !config.IsCode(err, tt.code) || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error = %v, want code %s and %q", err, tt.code, tt.want)
			}
			if got := readRepository(t, home, "private"); got != nil {
				t.Errorf("a refused entry was written: %v", got)
			}
		})
	}

	if err := loadAt(t, home).AddRepository(spec); err != nil {
		t.Fatal(err)
	}
	if got := readRepository(t, home, "private"); got["credential"] != "company-git" {
		t.Errorf("entry = %v", got)
	}
	view, err := loadAt(t, home).RepositoryView("private")
	if err != nil || view.Credential != "company-git" {
		t.Errorf("view = %+v, %v", view, err)
	}

	// change and remove it
	if changed, err := loadAt(t, home).UpdateRepository("private", RepoPatch{Credential: strp("gitlab-reg")}); err != nil || !changed {
		t.Fatalf("change: %v, %v", changed, err)
	}
	if got := readRepository(t, home, "private"); got["credential"] != "gitlab-reg" {
		t.Errorf("entry = %v", got)
	}
	if _, err := loadAt(t, home).UpdateRepository("private", RepoPatch{Credential: strp("no-such")}); !config.IsCode(err, config.CodeNotFound) {
		t.Errorf("unknown instance: %v", err)
	}
	if changed, err := loadAt(t, home).UpdateRepository("private", RepoPatch{Credential: strp("")}); err != nil || !changed {
		t.Fatalf("remove: %v, %v", changed, err)
	}
	if got := readRepository(t, home, "private"); got["credential"] != nil {
		t.Errorf("entry = %v", got)
	}
	// the source is checked against the credential, too
	if _, err := loadAt(t, home).UpdateRepository("private", RepoPatch{Credential: strp("company-git"), Source: strp("git+ssh://git@git.example.com/p.git")}); !config.IsCode(err, config.CodeValidation) {
		t.Errorf("ssh with credential: %v", err)
	}
}
