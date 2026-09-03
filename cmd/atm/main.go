package main

import (
	"os"
	"path/filepath"

	"atm/internal/capability"
	"atm/internal/capability/channel"
	"atm/internal/capability/checklist"
	"atm/internal/capability/codereview"
	"atm/internal/capability/qa"
	"atm/internal/capability/release"
	"atm/internal/capability/scrum"
	"atm/internal/cli"
	"atm/internal/core"
	"atm/internal/dispatch"
	"atm/internal/profile"
	"atm/internal/store"
	"atm/internal/tui"
	"atm/profiles"
)

// main is the composition root: it constructs the concrete store, assembles
// the capability registry, and hands the adapters their dependencies. No
// domain or presentation logic here.
func main() {
	reg := capability.NewRegistry(
		channel.New(), checklist.New(),
		scrum.New(), qa.New(), codereview.New(), release.New(),
	)
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
	// The profile store is machine-global state alongside the ledger, laid
	// out here rather than in an adapter — the same treatment dispatch.json
	// gets below. The profiles embedded in this binary are served from it
	// as pre-installed entries, never written to disk.
	openProfileStore := func(storePath string) (*profile.Store, error) {
		s, err := open(storePath)
		if err != nil {
			return nil, err
		}
		return profile.NewStore(filepath.Join(s.StorePath(), "profiles"), profiles.Embedded()), nil
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
		d, err := dispatch.NewService(filepath.Join(s.StorePath(), "dispatch.json"))
		if err != nil {
			return err
		}
		return tui.Run(s, actor, reg, d)
	}
	os.Exit(cli.Execute(cli.Deps{RunTUI: runTUI, Registry: reg, OpenService: openService, OpenAdmin: openAdmin, OpenProfileStore: openProfileStore}))
}
