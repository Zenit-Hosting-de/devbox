// Package api serves the REST API of DevBox (api/openapi.json) and the embedded web UI.
//
// Every request loads the configuration and the plugins again, so the files stay the
// single source of truth. Checks are cached in memory, actions run as sessions that
// are recorded in the history exactly like those of the CLI. Authentication beyond
// the loopback check, TLS and the WebSocket arrive in later work packages; see
// docs/concept.md.
package api
