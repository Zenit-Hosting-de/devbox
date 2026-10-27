package platform

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/user"
	"runtime"
	"strings"
)

// Host holds facts about the machine, used for ${host:…} placeholders and requests.
type Host struct {
	Name          string `json:"name"`
	User          string `json:"user"`
	Arch          string `json:"arch"`
	Distro        string `json:"distro"`
	DistroVersion string `json:"distroVersion"`
	Codename      string `json:"codename,omitempty"`
}

// OSReleasePaths are the standard locations of os-release(5), in lookup order.
var OSReleasePaths = []string{"/etc/os-release", "/usr/lib/os-release"}

// DetectHost reads the host facts. osRelease lists os-release files to try.
func DetectHost(osRelease []string) (Host, error) {
	h := Host{Arch: runtime.GOARCH}
	name, err := os.Hostname()
	if err != nil {
		return h, fmt.Errorf("host name: %w", err)
	}
	h.Name = name
	if u, err := user.Current(); err == nil {
		h.User = u.Username
	}
	for _, p := range osRelease {
		f, err := os.Open(p) //nolint:gosec // fixed list of os-release locations
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return h, fmt.Errorf("read %s: %w", p, err)
		}
		values, err := ParseOSRelease(f)
		_ = f.Close()
		if err != nil {
			return h, fmt.Errorf("parse %s: %w", p, err)
		}
		h.Distro = values["ID"]
		h.DistroVersion = values["VERSION_ID"]
		h.Codename = values["VERSION_CODENAME"]
		break
	}
	return h, nil
}

// ParseOSRelease parses the KEY=value lines of an os-release file.
func ParseOSRelease(r io.Reader) (map[string]string, error) {
	values := map[string]string{}
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		values[key] = unquote(value)
	}
	return values, sc.Err()
}

func unquote(v string) string {
	if len(v) >= 2 && (v[0] == '"' || v[0] == '\'') && v[len(v)-1] == v[0] {
		v = v[1 : len(v)-1]
	}
	return strings.NewReplacer(`\"`, `"`, `\\`, `\`, `\$`, `$`, "\\`", "`").Replace(v)
}
