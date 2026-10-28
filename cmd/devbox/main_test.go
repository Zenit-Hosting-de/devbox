package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/rogpeppe/go-internal/testscript"

	"github.com/Zenit-Hosting-de/devbox/internal/cli"
)

func TestMain(m *testing.M) {
	testscript.Main(m, map[string]func(){
		"devbox": func() { os.Exit(cli.Main(os.Args[1:], build(), cli.OSIO())) },
	})
}

// repoRoot is the repository root, for scripts that use the bundled plugins.
var repoRoot, _ = filepath.Abs("../..")

// TestScripts runs the CLI flows in test/testscript (*.txtar).
//
// The scripts start the CLI hundreds of times; with the race detector every start
// takes seconds and finds nothing (each call is its own process), so `make test` runs
// them in a separate step without -race.
func TestScripts(t *testing.T) {
	if raceEnabled {
		t.Skip("CLI flows run without the race detector (make test)")
	}
	testscript.Run(t, testscript.Params{
		Dir: "../../test/testscript",
		Cmds: map[string]func(*testscript.TestScript, bool, []string){
			"adoptium": cmdAdoptium,
		},
		Setup: func(e *testscript.Env) error {
			e.Setenv("HOME", e.WorkDir+"/home")
			e.Setenv("DEVBOX_SYSTEM_CONFIG", e.WorkDir+"/etc/devbox/config.json")
			e.Setenv("REPO", repoRoot)
			for _, k := range []string{"XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_STATE_HOME", "XDG_CACHE_HOME", "XDG_RUNTIME_DIR"} {
				e.Setenv(k, "")
			}
			return nil
		},
	})
}
