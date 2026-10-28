// Package plugin embeds the templates for `devbox plugin new` (one directory per
// language). Files may contain {{id}} and {{name}}, replaced when a plugin is created.
package plugin

import "embed"

// FS holds the plugin templates.
//
//go:embed all:bash
var FS embed.FS
