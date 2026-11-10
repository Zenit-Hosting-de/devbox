// Package plugins embeds the plugins bundled with DevBox (one directory each). The
// binary unpacks them on first use (internal/plugin.ExtractBuiltin).
package plugins

import "embed"

// FS holds the bundled plugin directories.
//
//go:embed all:base all:credentials all:docker all:dotnet all:git all:githost all:gradle all:java all:maven all:node all:npm all:nuget all:pip all:python all:registry all:system
var FS embed.FS
