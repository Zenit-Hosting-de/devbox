// Package schema embeds the DevBox JSON Schemas (schema/*.v1.json), the source of
// truth for every file DevBox reads or writes. Validation lives in internal/schema.
package schema

import "embed"

// FS holds every *.v1.json schema of this directory.
//
//go:embed *.v1.json
var FS embed.FS
