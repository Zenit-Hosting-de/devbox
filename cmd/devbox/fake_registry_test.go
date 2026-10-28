package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"

	"github.com/rogpeppe/go-internal/testscript"
)

// fakeRegistry answers the verification paths of the registry plugin for every layout
// (any path, in fact): 200 when the request carries the accepted credential, else 401.
// It logs every request with the kind of authentication it used.
type fakeRegistry struct {
	mu     sync.Mutex
	user   string
	secret string
	log    []string
}

func (f *fakeRegistry) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	auth, ok := "none", false
	if u, p, basic := r.BasicAuth(); basic {
		auth, ok = "basic:"+u, u == f.user && p == f.secret
	} else if t := r.Header.Get("PRIVATE-TOKEN"); t != "" {
		auth, ok = "private-token", t == f.secret
	} else if h := r.Header.Get("Authorization"); h != "" {
		scheme, token, _ := strings.Cut(h, " ")
		auth, ok = strings.ToLower(scheme), token == f.secret
	}
	f.log = append(f.log, fmt.Sprintf("%s %s auth=%s", r.Method, r.URL.Path, auth))
	if !ok || f.secret == "" {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	w.WriteHeader(http.StatusOK)
}

var (
	registryMu      sync.Mutex
	registryServers = map[string]*fakeRegistry{}
)

// cmdRegistry is the testscript command
//
//	registry start               start a server, set $REGISTRY to its URL
//	registry accept USER SECRET  accept SECRET (with USER for basic authentication)
//	registry log                 print the requests so far, one per line
func cmdRegistry(ts *testscript.TestScript, neg bool, args []string) {
	if neg || len(args) == 0 {
		ts.Fatalf("usage: registry start | registry accept USER SECRET | registry log")
	}
	key := ts.Getenv("WORK")
	registryMu.Lock()
	defer registryMu.Unlock()
	f, started := registryServers[key]
	if args[0] != "start" && !started {
		ts.Fatalf("registry %s: start the server first", args[0])
	}
	switch args[0] {
	case "start":
		f = &fakeRegistry{}
		srv := httptest.NewServer(f)
		registryServers[key] = f
		ts.Defer(srv.Close)
		ts.Setenv("REGISTRY", srv.URL)
	case "accept":
		if len(args) != 3 {
			ts.Fatalf("usage: registry accept USER SECRET")
		}
		f.mu.Lock()
		f.user, f.secret = args[1], args[2]
		f.mu.Unlock()
	case "log":
		f.mu.Lock()
		_, _ = fmt.Fprintln(ts.Stdout(), strings.Join(f.log, "\n"))
		f.mu.Unlock()
	default:
		ts.Fatalf("registry: unknown command %q", args[0])
	}
}
