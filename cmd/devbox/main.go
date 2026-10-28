// Command devbox sets up and maintains Linux developer machines.
package main

import (
	"os"

	"github.com/Zenit-Hosting-de/devbox/internal/cli"
)

// Set at build time via -ldflags "-X main.version=… -X main.commit=… -X main.date=…".
var (
	version = "0.0.0-dev"
	commit  = ""
	date    = ""
)

// vendor identifies this DevBox among other tools named devbox (see docs/concept.md,
// "Name und Auftritt"); install.sh relies on it.
const vendor = "zenit-hosting"

// protocol is the plugin protocol version implemented by this binary.
const protocol = 1

func build() cli.Build {
	return cli.Build{Vendor: vendor, Version: version, Commit: commit, Date: date, Protocol: protocol}
}

func main() {
	os.Exit(cli.Main(os.Args[1:], build(), cli.OSIO()))
}
