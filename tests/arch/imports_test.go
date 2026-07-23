// Package arch enforces the import-rules table of
// docs/architecture/logical-components.md for the packages refactor step 4
// (ATM-b9d83a) put on their target boundaries. A change that violates one of
// these rules is wrong even if it compiles and every other test passes.
package arch

import (
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// internalImports returns the "atm/internal/..." and "atm/libs/..." import
// paths of every non-test .go file in dir (relative to the repo root).
func internalImports(t *testing.T, dir string) map[string][]string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join("..", "..", dir, "*.go"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatalf("no .go files under %s — directory moved?", dir)
	}
	out := map[string][]string{}
	fset := token.NewFileSet()
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		src, err := parser.ParseFile(fset, f, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parse %s: %v", f, err)
		}
		for _, imp := range src.Imports {
			p, _ := strconv.Unquote(imp.Path.Value)
			if strings.HasPrefix(p, "atm/internal/") || strings.HasPrefix(p, "atm/libs/") {
				out[f] = append(out[f], p)
			}
		}
	}
	return out
}

func TestCoreIsAPureLeaf(t *testing.T) {
	for f, imps := range internalImports(t, "internal/core") {
		t.Errorf("%s imports %v; internal/core may import nothing from this repository", f, imps)
	}
}

func TestVersionImportsNoInternalPackage(t *testing.T) {
	for f, imps := range internalImports(t, "internal/version") {
		t.Errorf("%s imports %v; internal/version must be a pure leaf", f, imps)
	}
}

// TestTUIDoesNotImportStore is refactor step 4's actual boundary win: the TUI
// no longer knows the concrete persistence adapter, it consumes core.Service.
//
// The architecture doc's table says tui may import "core, tui/components —
// nothing else". That is the TARGET, and it does not hold yet: tui production
// files also import internal/{workflow, activity, seed, embed}. Step 5
// (ATM-08db6e) relocated the capabilities and put the TUI on the registry;
// the satellites (activity, seed, embed) remain acknowledged thin leaves.
// Purging them is out of scope, so this test asserts the edge that was
// actually removed rather than a rule the tree does not satisfy. Step 6
// (ATM-3b873c) has since landed: TestCLIDoesNotImportStore below pins the
// matching cli->store cut, and TestOnlyEventlogImportsEventsourceLib pins the
// event-sourcing library behind internal/store/eventlog.
func TestTUIDoesNotImportStore(t *testing.T) {
	for f, imps := range internalImports(t, "internal/tui") {
		for _, p := range imps {
			if p == "atm/internal/store" {
				t.Errorf("%s imports %q; internal/tui production files must consume core.Service, not the concrete store", f, p)
			}
		}
	}
}

// TestCapabilityRegistryImportsOnlyCore pins the registry package as a
// near-leaf: it may import only the domain core (plus cobra externally).
func TestCapabilityRegistryImportsOnlyCore(t *testing.T) {
	for f, imps := range internalImports(t, "internal/capability") {
		for _, p := range imps {
			if p != "atm/internal/core" {
				t.Errorf("%s imports %q; internal/capability may import only atm/internal/core", f, p)
			}
		}
	}
}

// TestCapabilityPackagesImportOnlyRegistryAndCore is refactor step 5's
// boundary: a capability owns its label slice and its cobra command, and
// reaches nothing but the registry seam and the domain leaf — never the
// store, the cli, or the tui.
func TestCapabilityPackagesImportOnlyRegistryAndCore(t *testing.T) {
	for _, dir := range []string{"internal/capability/contextmap", "internal/capability/workflow", "internal/capability/workflowai"} {
		for f, imps := range internalImports(t, dir) {
			for _, p := range imps {
				if p != "atm/internal/capability" && p != "atm/internal/core" {
					t.Errorf("%s imports %q; capability packages may import only the registry and core", f, p)
				}
			}
		}
	}
}

// TestAdaptersDoNotImportCapabilityPackages pins the other side of the
// seam: cli and tui consume capabilities only through the registry the
// composition root assembles — neither adapter names a capability.
func TestAdaptersDoNotImportCapabilityPackages(t *testing.T) {
	for _, dir := range []string{"internal/cli", "internal/tui"} {
		for f, imps := range internalImports(t, dir) {
			for _, p := range imps {
				if strings.HasPrefix(p, "atm/internal/capability/") {
					t.Errorf("%s imports %q; adapters consume only the registry (atm/internal/capability)", f, p)
				}
			}
		}
	}
}

// TestSkillsIsAPureLeaf pins the prompt-hosting package as importable by
// every layer: it may import nothing from this repository.
func TestSkillsIsAPureLeaf(t *testing.T) {
	for f, imps := range internalImports(t, "skills") {
		t.Errorf("%s imports %v; skills may import nothing from this repository", f, imps)
	}
}

func TestCLIDoesNotImportTUI(t *testing.T) {
	for f, imps := range internalImports(t, "internal/cli") {
		for _, p := range imps {
			if p == "atm/internal/tui" {
				t.Errorf("%s imports the tui package; the runner seam (Deps.RunTUI) is the only allowed edge", f)
			}
		}
	}
}

// TestCLIDoesNotImportStore is refactor step 6's boundary: the CLI consumes
// core.Service + core.StorageAdmin, both injected by cmd/atm. Neither the
// concrete store nor any of its subpackages may be named by cli production
// files.
func TestCLIDoesNotImportStore(t *testing.T) {
	for f, imps := range internalImports(t, "internal/cli") {
		for _, p := range imps {
			if p == "atm/internal/store" || strings.HasPrefix(p, "atm/internal/store/") {
				t.Errorf("%s imports %q; internal/cli consumes core interfaces injected by the composition root", f, p)
			}
		}
	}
}

// TestOnlyEventlogImportsEventsourceLib pins the carve: the event-sourcing
// library is an implementation detail of internal/store/eventlog. Nothing
// else in the module — the store facade included — may name it.
func TestOnlyEventlogImportsEventsourceLib(t *testing.T) {
	for _, dir := range []string{
		"cmd/atm", "internal/activity", "internal/actor", "internal/agent",
		"internal/capability", "internal/capability/contextmap", "internal/capability/workflow",
		"internal/cli", "internal/core", "internal/developing", "internal/embed",
		"internal/dispatch",
		"internal/manager", "internal/session", "internal/store", "internal/store/fsio",
		"internal/tui", "internal/tui/components", "internal/version",
	} {
		for f, imps := range internalImports(t, dir) {
			for _, p := range imps {
				if strings.HasPrefix(p, "atm/libs/eventsource") {
					t.Errorf("%s imports %q; only internal/store/eventlog may import the eventsource library", f, p)
				}
			}
		}
	}
}
