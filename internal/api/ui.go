package api

import (
	"embed"
	"errors"
	"io/fs"
	"net/http"
	"path"
	"regexp"
	"strings"
	"time"
)

// dist holds the Angular build (internal/api/dist/ui). Only .gitkeep is committed, so
// Go builds and tests work without a web build.
//
//go:embed all:dist
var dist embed.FS

// uiRoot is the directory of the build inside dist.
const uiRoot = "dist/ui"

// placeholder is served while the UI is not built.
const placeholder = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>DevBox</title>
<style>
body { font-family: system-ui, sans-serif; margin: 0; display: grid; min-height: 100vh; place-items: center; background: #f5f5f5; color: #1a1a1a; }
main { max-width: 36rem; padding: 2rem; }
code { background: #e4e4e4; padding: 0.1rem 0.4rem; border-radius: 0.25rem; }
@media (prefers-color-scheme: dark) { body { background: #1a1a1a; color: #f0f0f0; } code { background: #333; } }
</style>
</head>
<body>
<main>
<h1>DevBox</h1>
<p>The web interface is not built in this binary. Run <code>make build</code> to build it.</p>
<p>The API is available at <code>/api/v1</code>.</p>
</main>
</body>
</html>
`

// hashedName matches build output with a content hash in the name, e.g. main-5ZJYEXWY.js.
var hashedName = regexp.MustCompile(`[-.][A-Za-z0-9]{8,}\.[a-z0-9]+$`)

type uiFiles struct {
	fsys fs.FS
}

func newUIFiles() *uiFiles {
	sub, err := fs.Sub(dist, uiRoot)
	if err != nil {
		return &uiFiles{}
	}
	return &uiFiles{fsys: sub}
}

// built reports whether the build output contains index.html.
func (u *uiFiles) built() bool {
	if u.fsys == nil {
		return false
	}
	_, err := fs.Stat(u.fsys, "index.html")
	return err == nil
}

// serveUI answers every path outside /api: an existing file, index.html for paths
// without an extension (client side routing), else 404. Without a build it serves a
// placeholder page.
func (s *Server) serveUI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
	if name == "" {
		name = "index.html"
	}
	if !s.ui.built() {
		if path.Ext(name) != "" && name != "index.html" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		if r.Method == http.MethodGet {
			_, _ = w.Write([]byte(placeholder))
		}
		return
	}
	if info, err := fs.Stat(s.ui.fsys, name); err != nil || info.IsDir() {
		if path.Ext(name) != "" {
			http.NotFound(w, r)
			return
		}
		name = "index.html" // client side route
	}
	f, err := s.ui.fsys.Open(name)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			http.NotFound(w, r)
			return
		}
		s.fail(w, r, err)
		return
	}
	defer func() { _ = f.Close() }()
	seeker, ok := f.(interface {
		Read([]byte) (int, error)
		Seek(int64, int) (int64, error)
	})
	if !ok {
		s.fail(w, r, errors.New("ui: embedded file cannot seek"))
		return
	}
	if name == "index.html" {
		w.Header().Set("Cache-Control", "no-cache")
	} else if hashedName.MatchString(name) {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	}
	http.ServeContent(w, r, name, time.Time{}, seeker)
}
