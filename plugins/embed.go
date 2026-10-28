// Package plugins embeds the plugins bundled with DevBox (one directory each). The
// binary unpacks them on first use (internal/plugin.ExtractBuiltin).
package plugins

import "embed"

// FS holds the bundled plugin directories.
//
//go:embed all:base all:credentials all:java all:node all:registry all:system
var FS embed.FS
