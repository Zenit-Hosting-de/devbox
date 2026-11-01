package main

import (
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/cgi" //nolint:gosec // Httpoxy needs a proxy header in a CGI script; this is a fake host of a test
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/rogpeppe/go-internal/testscript"
)

// fakeHost is a private Git host over https with a self-signed certificate: it
// serves tarballs below /dl/, Git over http (git http-backend) for paths with ".git/",
// and the verification path /api/v4/user. Everything but the verification path needs
// the accepted credential; without it, the host answers 401 (with a basic challenge
// for Git). It logs every request with the kind of authentication, never the secret.
type fakeHost struct {
	mu      sync.Mutex
	root    string
	user    string
	secret  string
	log     []string
	backend http.Handler
}

func (f *fakeHost) auth(r *http.Request) (kind string, ok bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.secret == "" {
		return "none", false
	}
	if u, p, basic := r.BasicAuth(); basic {
		return "basic:" + u, u == f.user && p == f.secret
	}
	if t := r.Header.Get("PRIVATE-TOKEN"); t != "" {
		return "private-token", t == f.secret
	}
	if h := r.Header.Get("Authorization"); h != "" {
		scheme, token, _ := strings.Cut(h, " ")
		return strings.ToLower(scheme), token == f.secret
	}
	return "none", false
}

func (f *fakeHost) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	kind, ok := f.auth(r)
	f.mu.Lock()
	f.log = append(f.log, fmt.Sprintf("%s %s auth=%s ok=%v", r.Method, r.URL.Path, kind, ok))
	f.mu.Unlock()
	if !ok {
		w.Header().Set("WWW-Authenticate", `Basic realm="host"`)
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	switch {
	case r.URL.Path == "/api/v4/user":
		w.WriteHeader(http.StatusOK)
	case strings.HasPrefix(r.URL.Path, "/dl/"):
		http.ServeFile(w, r, filepath.Join(f.root, filepath.FromSlash(strings.TrimPrefix(r.URL.Path, "/")))) //nolint:gosec // fake host of a test; the paths come from the script
	case strings.Contains(r.URL.Path, ".git/") && f.backend != nil:
		f.backend.ServeHTTP(w, r)
	default:
		http.NotFound(w, r)
	}
}

var (
	hostMu      sync.Mutex
	hostServers = map[string]*fakeHost{}
)

// cmdHost is the testscript command
//
//	host start ROOT              start the host, serve ROOT (dl/ for tarballs, bare
//	                             repositories for Git); sets $HOST and $HOSTCA, writes
//	                             the certificate to $HOSTCA and a .curlrc that trusts it
//	host accept USER SECRET      accept SECRET (with USER for basic authentication)
//	host log                     print the requests so far, one per line
func cmdHost(ts *testscript.TestScript, neg bool, args []string) {
	if neg || len(args) == 0 {
		ts.Fatalf("usage: host start ROOT | host accept USER SECRET | host log")
	}
	key := ts.Getenv("WORK")
	hostMu.Lock()
	defer hostMu.Unlock()
	f, started := hostServers[key]
	if args[0] != "start" && !started {
		ts.Fatalf("host %s: start the host first", args[0])
	}
	switch args[0] {
	case "start":
		if len(args) != 2 {
			ts.Fatalf("usage: host start ROOT")
		}
		f = &fakeHost{root: ts.MkAbs(args[1])}
		if git, err := exec.LookPath("git"); err == nil {
			if out, err := exec.Command(git, "--exec-path").Output(); err == nil { //nolint:gosec // git of the test machine
				backend := filepath.Join(strings.TrimSpace(string(out)), "git-http-backend")
				if _, err := os.Stat(backend); err == nil {
					f.backend = &cgi.Handler{Path: backend, Dir: f.root, Env: []string{"GIT_PROJECT_ROOT=" + f.root, "GIT_HTTP_EXPORT_ALL=1"}}
				}
			}
		}
		srv := httptest.NewTLSServer(f)
		hostServers[key] = f
		ts.Defer(srv.Close)
		ca := filepath.Join(ts.Getenv("WORK"), "host-ca.pem")
		block := &pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw}
		if _, err := x509.ParseCertificate(block.Bytes); err != nil {
			ts.Fatalf("host: %v", err)
		}
		if err := os.WriteFile(ca, pem.EncodeToMemory(block), 0o600); err != nil {
			ts.Fatalf("host: %v", err)
		}
		// curl of the plugins reads it: the verification of an instance trusts the host
		curlrc := filepath.Join(ts.Getenv("HOME"), ".curlrc")
		if err := os.MkdirAll(filepath.Dir(curlrc), 0o700); err != nil {
			ts.Fatalf("host: %v", err)
		}
		if err := os.WriteFile(curlrc, []byte("cacert = \""+ca+"\"\n"), 0o600); err != nil {
			ts.Fatalf("host: %v", err)
		}
		ts.Setenv("HOST", srv.URL)
		ts.Setenv("HOSTCA", ca)
	case "accept":
		if len(args) != 3 {
			ts.Fatalf("usage: host accept USER SECRET")
		}
		f.mu.Lock()
		f.user, f.secret = args[1], args[2]
		f.mu.Unlock()
	case "log":
		f.mu.Lock()
		_, _ = fmt.Fprintln(ts.Stdout(), strings.Join(f.log, "\n"))
		f.mu.Unlock()
	default:
		ts.Fatalf("host: unknown command %q", args[0])
	}
}
