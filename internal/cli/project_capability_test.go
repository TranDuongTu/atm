package cli

import (
	"strings"
	"testing"
)

func TestGoldenProjectCreateWithCapabilities(t *testing.T) {
	h := newGoldenHarness(t)
	out, _, code := h.run("--output", "json", "project", "create",
		"--code", "PCX", "--name", "cap demo",
		"--capabilities", "workflow",
		"--actor", "admin@cli:unset")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, out)
	}
	compareGolden(t, "project-create-with-capabilities", out)
}

func TestProjectCreateRejectsUnknownCapability(t *testing.T) {
	h := newGoldenHarness(t)
	_, stderr, code := h.run("project", "create", "--code", "PXX", "--name", "x",
		"--capabilities", "nosuch", "--actor", "admin@cli:unset")
	if code == 0 {
		t.Fatal("expected usage error")
	}
	if !strings.Contains(stderr, "nosuch") || !strings.Contains(stderr, "workflow") {
		t.Errorf("error must name the unknown capability and the valid names, got %q", stderr)
	}
}

func TestGoldenProjectCapabilityListAddRemove(t *testing.T) {
	h := newGoldenHarness(t)
	h.run("project", "create", "--code", "PCX", "--name", "cap demo",
		"--capabilities", "workflow", "--actor", "admin@cli:unset")
	out, _, code := h.run("--output", "json", "project", "capability", "list", "--project", "PCX")
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	compareGolden(t, "project-capability-list", out)

	if _, _, code := h.run("project", "capability", "add", "--project", "PCX", "--name", "contextmap", "--actor", "admin@cli:unset"); code != 0 {
		t.Fatalf("add exit %d", code)
	}
	if _, _, code := h.run("project", "capability", "remove", "--project", "PCX", "--name", "workflow", "--actor", "admin@cli:unset"); code != 0 {
		t.Fatalf("remove exit %d", code)
	}
	out2, _, _ := h.run("--output", "json", "project", "capability", "list", "--project", "PCX")
	compareGolden(t, "project-capability-list-after-add-remove", out2)
}

// TestProjectCapabilityListLegacyNil proves the list command's legacy branch:
// a project with no recorded capability choice (Capabilities == nil) reads as
// "all capabilities enabled, no explicit choice recorded". The plan's
// original version of this test built the fixture via seedScenario1 (through
// `project create`), but after this task `project create` always records an
// explicit capability choice (Step 3(a): CreateProject is followed by one
// EnableProjectCapability per chosen name) — so a CLI-created project can
// never be legacy-nil anymore. Legacy-nil is now only reachable by projects
// that predate capability enablement, which in this codebase means going
// straight through the store facade (CreateProject alone, with no
// EnableProjectCapability calls) rather than through the CLI. Building the
// fixture that way still proves the branch: p.Capabilities stays nil, and the
// list command must fall back to "every registered capability, unmarked".
func TestProjectCapabilityListLegacyNil(t *testing.T) {
	h := newGoldenHarness(t)
	if _, err := h.store.CreateProject("LEG", "legacy", "admin@cli:unset"); err != nil {
		t.Fatalf("create legacy project: %v", err)
	}
	h.output = outputText
	out, _, code := h.run("project", "capability", "list", "--project", "LEG")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, out)
	}
	if !strings.Contains(out, "all") {
		t.Errorf("legacy project must report the all-enabled default, got %q", out)
	}
}
