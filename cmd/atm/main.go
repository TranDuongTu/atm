package main

import (
	"os"

	"atm/internal/capability"
	"atm/internal/cli"
	"atm/internal/store"
	"atm/internal/tui"
)

// main is the composition root: it constructs the concrete store, assembles
// the capability registry, and hands the adapters their dependencies. No
// domain or presentation logic here.
func main() {
	reg := capability.NewRegistry()
	runTUI := func(storePath, actor string) error {
		root := store.ResolveStorePath(storePath)
		s, err := store.Open(root)
		if err != nil {
			return err
		}
		return tui.Run(s, actor)
	}
	os.Exit(cli.Execute(cli.Deps{RunTUI: runTUI, Registry: reg}))
}
