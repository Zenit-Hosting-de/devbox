package main

import (
	"os"
	"testing"

	"github.com/rogpeppe/go-internal/testscript"

	"github.com/Zenit-Hosting-de/devbox/internal/cli"
)

func TestMain(m *testing.M) {
	testscript.Main(m, map[string]func(){
		"devbox": func() { os.Exit(cli.Main(os.Args[1:], build(), cli.OSIO())) },
	})
}

// TestScripts runs the CLI flows in test/testscript (*.txtar).
func TestScripts(t *testing.T) {
	testscript.Run(t, testscript.Params{
		Dir: "../../test/testscript",
		Setup: func(e *testscript.Env) error {
			e.Setenv("HOME", e.WorkDir+"/home")
			e.Setenv("DEVBOX_SYSTEM_CONFIG", e.WorkDir+"/etc/devbox/config.json")
			for _, k := range []string{"XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_STATE_HOME", "XDG_CACHE_HOME", "XDG_RUNTIME_DIR"} {
				e.Setenv(k, "")
			}
			return nil
		},
	})
}
