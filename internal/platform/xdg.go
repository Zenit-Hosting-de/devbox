package platform

import (
	"errors"
	"path/filepath"
)

// Dirs are the XDG base directories of the user (XDG Base Directory Specification).
type Dirs struct {
	Config string
	Data   string
	State  string
	Cache  string
	// Runtime is $XDG_RUNTIME_DIR; empty when the session does not provide one.
	Runtime string
}

// Getenv looks up an environment variable, like os.Getenv.
type Getenv func(string) string

// XDG resolves the base directories from the environment. Relative values are
// ignored, as the specification requires.
func XDG(getenv Getenv) (Dirs, error) {
	home := getenv("HOME")
	if home == "" {
		return Dirs{}, errors.New("HOME is not set")
	}
	pick := func(name string, fallback ...string) string {
		if v := getenv(name); filepath.IsAbs(v) {
			return v
		}
		if len(fallback) == 0 {
			return ""
		}
		return filepath.Join(append([]string{home}, fallback...)...)
	}
	return Dirs{
		Config:  pick("XDG_CONFIG_HOME", ".config"),
		Data:    pick("XDG_DATA_HOME", ".local", "share"),
		State:   pick("XDG_STATE_HOME", ".local", "state"),
		Cache:   pick("XDG_CACHE_HOME", ".cache"),
		Runtime: pick("XDG_RUNTIME_DIR"),
	}, nil
}
