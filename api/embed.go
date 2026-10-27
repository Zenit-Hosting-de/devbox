// Package api embeds the API contract (api/openapi.json, OpenAPI 3.1).
package api

import _ "embed"

// OpenAPI is the raw OpenAPI document.
//
//go:embed openapi.json
var OpenAPI []byte
