package cli

import (
	"strings"
	"testing"
)

// TestCapabilityListShowsDisabled proves `atm capability list` enumerates
// the FULL registry and reports the per-project enabled flag: a project that
// removed contextmap must show it as enabled=false while workflow stays
// enabled=true. The list always enumerates every registered capability; the
// hard gate (unmount) is asserted separately.
func TestCapabilityListShowsDisabled(t *testing.T) {
	h := newGoldenHarness(t)
	h.seedScenario1()
	if _, _, code := h.run("project", "capability", "remove", "--project", "ATM", "--name", "contextmap", "--actor", "admin@cli:unset"); code != 0 {
		t.Fatalf("capability remove exit %d", code)
	}
	h.reset()
	stdout, _, code := h.run("capability", "list", "--project", "ATM")
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	compareGolden(t, "capability-list-contextmap-disabled", stdout)
	if !strings.Contains(stdout, `"contextmap"`) || !strings.Contains(stdout, `"workflow"`) {
		t.Fatalf("list must enumerate both capabilities: %s", stdout)
	}
}

// TestCapabilityMountHardGate proves disabled capabilities are unmounted under
// `atm capability` (the hard gate) while enabled ones stay mounted.
func TestCapabilityMountHardGate(t *testing.T) {
	h := newGoldenHarness(t)
	h.seedScenario1()
	if _, _, code := h.run("project", "capability", "remove", "--project", "ATM", "--name", "contextmap", "--actor", "admin@cli:unset"); code != 0 {
		t.Fatalf("capability remove exit %d", code)
	}
	h.reset()
	_, _, code := h.run("capability", "contextmap", "check", "--project", "ATM")
	if code == 0 {
		t.Fatal("disabled capability's subtree still mounted under atm capability")
	}
	h.reset()
	// workflow status requires --task, not --project; the point is the command
	// must be FOUND (mounted), so assert the failure is not "unknown command".
	_, stderr, code := h.run("capability", "workflow", "status", "--project", "ATM")
	if code == 0 {
		return // unexpectedly succeeded; still fine — it is mounted
	}
	if strings.Contains(stderr, "unknown command") {
		t.Fatalf("enabled capability (workflow) not mounted under atm capability: %s", stderr)
	}
}

// TestCapabilityGuideMountedByName proves each capability's guide subcommand
// is mounted under `atm capability <Name>`, not under a separate command name.
func TestCapabilityGuideMountedByName(t *testing.T) {
	h := newGoldenHarness(t)
	h.seedScenario1()
	stdout, _, code := h.run("capability", "contextmap", "guide")
	if code != 0 || !strings.Contains(stdout, "Context map capability") {
		t.Fatalf("atm capability contextmap guide: exit %d, out %q", code, stdout)
	}
}

// TestGoldenCapabilityUnmanaged: the manager's triage read — labels no
// enabled capability owns, with usage counts. Workflow-owned labels
// (status:open via the seeded vocabulary) must NOT appear.
func TestGoldenCapabilityUnmanaged(t *testing.T) {
	h := newGoldenHarness(t)
	h.run("project", "create", "--code", "PCX", "--name", "cap demo",
		"--capabilities", "workflow", "--actor", "admin@cli:unset")
	h.run("label", "add", "--name", "PCX:type:bug", "--actor", "admin@cli:unset")
	h.run("label", "add", "--name", "PCX:urgent", "--actor", "admin@cli:unset")
	h.run("task", "create", "--project", "PCX", "--title", "t1",
		"--label", "PCX:type:bug", "--label", "PCX:status:open", "--actor", "admin@cli:unset")
	out, _, code := h.run("--output", "json", "capability", "unmanaged", "--project", "PCX")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, out)
	}
	if strings.Contains(out, "status:open") {
		t.Fatalf("workflow-owned label leaked into unmanaged: %s", out)
	}
	compareGolden(t, "capability-unmanaged", out)
}