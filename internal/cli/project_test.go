package cli

import (
	"strings"
	"testing"
)

func TestGoldenProjectCreate(t *testing.T) {
	h := newGoldenHarness(t)
	sp := h.store.StorePath()
	h.run("init", "--store", sp, "--actor", "claude")
	out, _, code := h.run("project", "create", "--store", sp, "--code", "ATM", "--name", "Agent Tasks Management", "--actor", "claude")
	if code != 0 {
		t.Fatalf("exit = %d stderr=%s", code, h.stderr.String())
	}
	compareGolden(t, "project-create", out)
}

func TestGoldenProjectCreateInvalidCode(t *testing.T) {
	h := newGoldenHarness(t)
	sp := h.store.StorePath()
	h.run("init", "--store", sp, "--actor", "claude")
	_, _, code := h.run("project", "create", "--store", sp, "--code", "atm", "--name", "x", "--actor", "claude")
	if code != ExitUsage {
		t.Fatalf("expected usage exit for lowercase code, got %d", code)
	}
}

func TestGoldenProjectList(t *testing.T) {
	h := newGoldenHarness(t)
	h.seedScenario1()
	out, _, code := h.run("project", "list")
	if code != 0 {
		t.Fatalf("exit = %d stderr=%s", code, h.stderr.String())
	}
	compareGolden(t, "project-list", out)
	if !strings.Contains(out, `"ATM"`) {
		t.Fatalf("expected ATM in list: %s", out)
	}
}

func TestGoldenProjectShow(t *testing.T) {
	h := newGoldenHarness(t)
	h.seedScenario1()
	out, _, code := h.run("project", "show", "--code", "ATM")
	if code != 0 {
		t.Fatalf("exit = %d stderr=%s", code, h.stderr.String())
	}
	compareGolden(t, "project-show", out)
}

func TestGoldenProjectSetName(t *testing.T) {
	h := newGoldenHarness(t)
	h.seedScenario1()
	out, _, code := h.run("project", "set-name", "--code", "ATM", "--name", "Renamed Project", "--actor", "claude")
	if code != 0 {
		t.Fatalf("exit = %d stderr=%s", code, h.stderr.String())
	}
	compareGolden(t, "project-set-name", out)
}

func TestGoldenProjectRemoveZeroTaskGuard(t *testing.T) {
	h := newGoldenHarness(t)
	sp := h.store.StorePath()
	h.run("init", "--store", sp, "--actor", "claude")
	h.run("project", "create", "--store", sp, "--code", "EMP", "--name", "Empty", "--actor", "claude")
	out, _, code := h.run("project", "remove", "--code", "EMP", "--actor", "claude")
	if code != 0 {
		t.Fatalf("exit = %d stderr=%s", code, h.stderr.String())
	}
	compareGolden(t, "project-remove-zero-task", out)

	h.seedScenario1()
	_, _, code = h.run("project", "remove", "--code", "ATM", "--actor", "claude")
	if code != ExitConflict {
		t.Fatalf("expected conflict exit for project with tasks, got %d", code)
	}
}
