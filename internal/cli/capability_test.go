package cli

import (
	"strings"
	"testing"
)

// TestCapabilityListShowsDisabled proves `atm capability list` enumerates
// the FULL registry and reports the per-project enabled flag: a project that
// removed scrum must show it as enabled=false while qa stays
// enabled=true. The list always enumerates every registered capability; the
// hard gate (unmount) is asserted separately.
func TestCapabilityListShowsDisabled(t *testing.T) {
	h := newGoldenHarness(t)
	h.seedScenario1()
	if _, _, code := h.run("project", "capability", "remove", "--project", "ATM", "--name", "scrum", "--actor", "admin@cli:unset"); code != 0 {
		t.Fatalf("capability remove exit %d", code)
	}
	h.reset()
	stdout, _, code := h.run("capability", "list", "--project", "ATM")
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	compareGolden(t, "capability-list-scrum-disabled", stdout)
	if !strings.Contains(stdout, `"scrum"`) || !strings.Contains(stdout, `"qa"`) {
		t.Fatalf("list must enumerate both capabilities: %s", stdout)
	}
}

// TestCapabilityMountHardGate proves disabled capabilities are unmounted under
// `atm capability` (the hard gate) while enabled ones stay mounted.
func TestCapabilityMountHardGate(t *testing.T) {
	h := newGoldenHarness(t)
	h.seedScenario1()
	if _, _, code := h.run("project", "capability", "remove", "--project", "ATM", "--name", "scrum", "--actor", "admin@cli:unset"); code != 0 {
		t.Fatalf("capability remove exit %d", code)
	}
	h.reset()
	_, _, code := h.run("capability", "scrum", "lanes", "--project", "ATM")
	if code == 0 {
		t.Fatal("disabled capability's subtree still mounted under atm capability")
	}
	h.reset()
	// qa is still enabled; the point is its subtree must be FOUND (mounted),
	// so assert the failure — if any — is not "unknown command".
	_, stderr, code := h.run("capability", "qa", "lanes", "--project", "ATM")
	if code == 0 {
		return // unexpectedly succeeded; still fine — it is mounted
	}
	if strings.Contains(stderr, "unknown command") {
		t.Fatalf("enabled capability (qa) not mounted under atm capability: %s", stderr)
	}
}

// TestCapabilityGuideMountedByName proves each capability's guide subcommand
// is mounted under `atm capability <Name>`, not under a separate command name.
func TestCapabilityGuideMountedByName(t *testing.T) {
	h := newGoldenHarness(t)
	h.seedScenario1()
	stdout, _, code := h.run("capability", "scrum", "guide")
	if code != 0 || !strings.Contains(stdout, "scrum") {
		t.Fatalf("atm capability scrum guide: exit %d, out %q", code, stdout)
	}
}

// TestGoldenCapabilityUnmanaged: the manager's triage read — labels no
// enabled capability owns, with usage counts. Scrum-owned labels
// (scrum:task via the seeded vocabulary) must NOT appear.
func TestGoldenCapabilityUnmanaged(t *testing.T) {
	h := newGoldenHarness(t)
	h.run("project", "create", "--code", "PCX", "--name", "cap demo",
		"--capabilities", "scrum", "--actor", "admin@cli:unset")
	h.run("label", "add", "--name", "PCX:type:bug", "--actor", "admin@cli:unset")
	h.run("label", "add", "--name", "PCX:urgent", "--actor", "admin@cli:unset")
	h.run("task", "create", "--project", "PCX", "--title", "t1",
		"--label", "PCX:type:bug", "--label", "PCX:scrum:task", "--actor", "admin@cli:unset")
	out, _, code := h.run("--output", "json", "capability", "unmanaged", "--project", "PCX")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, out)
	}
	if strings.Contains(out, "scrum:task") {
		t.Fatalf("scrum-owned label leaked into unmanaged: %s", out)
	}
	compareGolden(t, "capability-unmanaged", out)
}