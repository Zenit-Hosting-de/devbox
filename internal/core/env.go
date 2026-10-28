package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Zenit-Hosting-de/devbox/internal/config"
	"github.com/Zenit-Hosting-de/devbox/internal/runner"
	"github.com/Zenit-Hosting-de/devbox/internal/schema"
)

// NewRunner returns a runner for the paths and the environment of this state.
func (st *State) NewRunner() (*runner.Runner, error) {
	dirs := st.Ctx.XDG
	bin, err := SelfPath(st.Env.Getenv("PATH"))
	if err != nil {
		return nil, err
	}
	runtimeDir := dirs.Runtime
	if runtimeDir == "" {
		runtimeDir = filepath.Join(os.TempDir(), "devbox-"+strconv.Itoa(os.Getuid()))
	} else {
		runtimeDir = filepath.Join(runtimeDir, "devbox")
	}
	termName := st.Env.Getenv("TERM")
	if termName == "" {
		termName = "xterm-256color"
	}
	env := runner.Env{
		Version:    st.Build.Version,
		Bin:        bin,
		Home:       st.Env.Getenv("HOME"),
		User:       st.Ctx.Host.User,
		Path:       st.Env.Getenv("PATH"),
		Lang:       st.Env.Getenv("LANG"),
		Term:       termName,
		RuntimeDir: runtimeDir,
		StateDir:   st.StateDir(),
		CacheDir:   filepath.Join(dirs.Cache, "devbox"),
	}
	return runner.New(env, st.Validator), nil
}

// SelfPath returns how this binary was started (absolute), so plugins can call it as
// $DEVBOX_BIN under the same name; it falls back to the executable path.
func SelfPath(pathEnv string) (string, error) {
	if arg := os.Args[0]; filepath.IsAbs(arg) {
		return arg, nil
	} else if strings.Contains(arg, "/") {
		return filepath.Abs(arg)
	} else if p, err := LookPath(arg, pathEnv); err == nil {
		return p, nil
	}
	return os.Executable()
}

// UILanguage returns ui.language of the user file, else of the system file.
func (st *State) UILanguage() string {
	return st.UI()["language"]
}

// UI returns the ui section of the configuration; the user file wins over the system
// file, key by key. Keys that no file sets are absent.
func (st *State) UI() map[string]string {
	out := map[string]string{}
	for _, f := range []*config.File{st.System, st.User} {
		ui, _ := f.Plain()["ui"].(map[string]any)
		for _, key := range []string{"language", "scheme", "accent"} {
			if s, ok := ui[key].(string); ok && s != "" {
				out[key] = s
			}
		}
	}
	return out
}

// Host returns the facts about the machine as the plugin protocol and the API show them.
func (st *State) Host() map[string]any {
	h := st.Ctx.Host
	m := map[string]any{"name": h.Name, "user": h.User, "distro": h.Distro, "distroVersion": h.DistroVersion, "arch": h.Arch}
	if h.Codename != "" {
		m["codename"] = h.Codename
	}
	if d := strings.ToLower(strings.Split(st.Env.Getenv("XDG_CURRENT_DESKTOP"), ":")[0]); d != "" {
		m["desktop"] = d
	}
	return m
}

// ProxyEnv passes the network settings of the configuration to plugins.
func (st *State) ProxyEnv() []string {
	var network map[string]any
	for _, f := range []*config.File{st.System, st.User} {
		if n, ok := f.Plain()["network"].(map[string]any); ok {
			network = n
		}
	}
	expand := func(key string) string {
		s, _ := network[key].(string)
		v, err := config.Expand(s, func(r string) (any, error) { return config.ContextLookup(st.Ctx, r) })
		if err != nil {
			return ""
		}
		out, _ := v.(string)
		return out
	}
	var env []string
	if p := expand("proxy"); p != "" {
		env = append(env, "HTTPS_PROXY="+p, "HTTP_PROXY="+p, "https_proxy="+p, "http_proxy="+p)
	}
	if n := expand("noProxy"); n != "" {
		env = append(env, "NO_PROXY="+n, "no_proxy="+n)
	}
	if c := expand("caFile"); c != "" {
		env = append(env, "SSL_CERT_FILE="+c)
	}
	return env
}

// intValue reads a JSON integer as parsed by jsonfile (json.Number).
func intValue(v any) (int, bool) {
	n, ok := v.(json.Number)
	if !ok {
		return 0, false
	}
	i, err := n.Int64()
	return int(i), err == nil
}

// Retention reads history.keepDays and history.keepSessions; the user file wins over
// the system file, zero means the schema default.
func (st *State) Retention() (keepDays, keepSessions int) {
	for _, f := range []*config.File{st.System, st.User} {
		h, _ := f.Plain()["history"].(map[string]any)
		if n, ok := intValue(h["keepDays"]); ok {
			keepDays = n
		}
		if n, ok := intValue(h["keepSessions"]); ok {
			keepSessions = n
		}
	}
	return keepDays, keepSessions
}

// ServerConfig is the server section of the configuration, defaults from the schema.
type ServerConfig struct {
	Listen         string
	Auth           string
	AllowedOrigins []string
}

// Server reads server.listen, server.auth and server.allowedOrigins; the user file
// wins over the system file, the schema provides the defaults.
func (st *State) Server() (ServerConfig, error) {
	listen, err := schema.DefaultString("config", "/properties/server/properties/listen/default")
	if err != nil {
		return ServerConfig{}, err
	}
	auth, err := schema.DefaultString("config", "/properties/server/properties/auth/default")
	if err != nil {
		return ServerConfig{}, err
	}
	sc := ServerConfig{Listen: listen, Auth: auth}
	for _, f := range []*config.File{st.System, st.User} {
		server, _ := f.Plain()["server"].(map[string]any)
		if s, ok := server["listen"].(string); ok && s != "" {
			sc.Listen = s
		}
		if s, ok := server["auth"].(string); ok && s != "" {
			sc.Auth = s
		}
		if o := AnyStrings(server["allowedOrigins"]); len(o) > 0 {
			sc.AllowedOrigins = o
		}
	}
	return sc, nil
}

// ErrNotFound builds a not-found configuration error.
func ErrNotFound(key, detail string) error {
	return &config.Error{Code: config.CodeNotFound, Key: key, Detail: detail}
}

// ErrInvalid builds a validation configuration error.
func ErrInvalid(key, detail string) error {
	return &config.Error{Code: config.CodeValidation, Key: key, Detail: detail}
}
