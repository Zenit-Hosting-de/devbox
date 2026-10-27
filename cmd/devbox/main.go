// Command devbox sets up and maintains Linux developer machines.
//
// This is the AP-01 skeleton: it only implements `devbox version [--json]`.
// The full command line interface arrives with AP-11.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
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

// Exit codes follow the plugin protocol.
const (
	exitOK           = 0
	exitFailed       = 1
	exitInvalidUsage = 2
)

type versionInfo struct {
	Vendor   string `json:"vendor"`
	Version  string `json:"version"`
	Commit   string `json:"commit"`
	Date     string `json:"date"`
	Protocol int    `json:"protocol"`
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout))
}

func run(args []string, stdout io.Writer) int {
	if len(args) == 0 || args[0] != "version" {
		return exitInvalidUsage
	}
	info := versionInfo{Vendor: vendor, Version: version, Commit: commit, Date: date, Protocol: protocol}
	switch {
	case len(args) == 1:
		if _, err := fmt.Fprintln(stdout, info.Version); err != nil {
			return exitFailed
		}
	case len(args) == 2 && args[1] == "--json":
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(info); err != nil {
			return exitFailed
		}
	default:
		return exitInvalidUsage
	}
	return exitOK
}
