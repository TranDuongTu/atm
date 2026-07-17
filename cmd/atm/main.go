package main

import (
	"os"

	"atm/internal/capability"
	"atm/internal/capability/contextmap"
	"atm/internal/capability/workflow"
	"atm/internal/cli"
	"atm/internal/core"
	"atm/internal/store"
	"atm/internal/tui"
)

// main is the composition root: it constructs the concrete store, assembles
// the capability registry, and hands the adapters their dependencies. No
// domain or presentation logic here.
func main() {
	reg := capability.NewRegistry(workflow.New(), contextmap.New())
	open := func(storePath string) (*store.Store, error) {
		return store.Open(store.ResolveStorePath(storePath))
	}
	openService := func(storePath string) (core.Service, error) {
		s, err := open(storePath)
		if err != nil {
			return nil, err
		}
		return s, nil
	}
	openAdmin := func(storePath string) (core.StorageAdmin, error) {
		s, err := open(storePath)
		if err != nil {
			return nil, err
		}
		return s, nil
	}
	runTUI := func(storePath, actor string) error {
		s, err := open(storePath)
		if err != nil {
			return err
		}
		return tui.Run(s, actor, reg)
	}
	os.Exit(cli.Execute(cli.Deps{RunTUI: runTUI, Registry: reg, OpenService: openService, OpenAdmin: openAdmin}))
}
