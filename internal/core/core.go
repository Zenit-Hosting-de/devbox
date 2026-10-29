// Package core holds the logic that the CLI and the API share: loading the
// configuration and the plugins, resolving action parameters, running sessions and
// checks, and reading settings, credentials and translations. The CLI keeps flag
// parsing and text rendering, the API keeps HTTP.
package core

import (
	"net/http"
	"path/filepath"

	"github.com/Zenit-Hosting-de/devbox/internal/config"
	"github.com/Zenit-Hosting-de/devbox/internal/platform"
	"github.com/Zenit-Hosting-de/devbox/internal/schema"
)

// SystemConfigEnv overrides the path of the system configuration, e.g. for tests and
// images that keep it elsewhere.
const SystemConfigEnv = "DEVBOX_SYSTEM_CONFIG"

// DefaultSystemConfig is the system configuration (layer 2).
const DefaultSystemConfig = "/etc/devbox/config.json"

// Build describes the binary.
type Build struct {
	Vendor   string `json:"vendor"`
	Version  string `json:"version"`
	Commit   string `json:"commit"`
	Date     string `json:"date"`
	Protocol int    `json:"protocol"`
}

// Env is the process environment of a call, so that CLI and server can run in tests.
type Env struct {
	Getenv  func(string) string
	Environ func() []string
	// ConfigPath is the user configuration file; empty means the default location.
	ConfigPath string
	// Sets are the --set key=value layers of the call.
	Sets []string
	// Validator is reused when set (the server loads the state for every request);
	// otherwise each call creates one.
	Validator *schema.Validator
	// HTTPClient downloads plugin repositories (tarballs); nil uses the proxy of the
	// environment. Tests set it to reach a local server.
	HTTPClient *http.Client
}

// Base is what every command needs before plugins are loaded: the validator, the
// directories, the host and the configuration paths.
type Base struct {
	Validator  *schema.Validator
	Ctx        config.Context
	UserPath   string
	SystemPath string
}

// NewBase detects the host and the directories.
func NewBase(env Env) (*Base, error) {
	v := env.Validator
	if v == nil {
		var err error
		if v, err = schema.New(); err != nil {
			return nil, err
		}
	}
	dirs, err := platform.XDG(env.Getenv)
	if err != nil {
		return nil, err
	}
	host, err := platform.DetectHost(platform.OSReleasePaths)
	if err != nil {
		return nil, err
	}
	b := &Base{
		Validator:  v,
		Ctx:        config.Context{Getenv: env.Getenv, XDG: dirs, Host: host},
		UserPath:   env.ConfigPath,
		SystemPath: env.Getenv(SystemConfigEnv),
	}
	if b.UserPath == "" {
		b.UserPath = filepath.Join(dirs.Config, "devbox", "config.json")
	}
	if b.SystemPath == "" {
		b.SystemPath = DefaultSystemConfig
	}
	return b, nil
}

// StateDir is the directory of history, sessions and backups.
func (b *Base) StateDir() string { return filepath.Join(b.Ctx.XDG.State, "devbox") }

// DataDir is the directory of credentials and the bundled plugins.
func (b *Base) DataDir() string { return filepath.Join(b.Ctx.XDG.Data, "devbox") }
