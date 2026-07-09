package cli

import (
	"strings"
	"testing"
)

func TestGoldenTaskCreate(t *testing.T) {
	h := newGoldenHarness(t)
	h.seedScenario1()
	out, _, code := h.run("task", "create", "--project", "ATM", "--title", "New task", "--actor", "admin@cli:unset")
	if code != 0 {
		t.Fatalf("exit = %d stderr=%s", code, h.stderr.String())
	}
	compareGolden(t, "task-create", out)
}

func TestGoldenTaskCreateAutoRegistersLabels(t *testing.T) {
	h := newGoldenHarness(t)
	sp := h.store.StorePath()
	h.run("init", "--store", sp, "--actor", "admin@cli:unset")
	h.run("project", "create", "--store", sp, "--code", "ATM", "--name", "x", "--actor", "admin@cli:unset")
	out, _, code := h.run("task", "create", "--store", sp, "--project", "ATM", "--title", "t",
		"--label", "ATM:type:feature", "--label", "ATM:priority:high", "--actor", "admin@cli:unset")
	if code != 0 {
		t.Fatalf("exit = %d stderr=%s", code, h.stderr.String())
	}
	compareGolden(t, "task-create-auto-registers-labels", out)

	ls, _, code := h.run("label", "list", "--project", "ATM")
	if code != 0 {
		t.Fatalf("label list exit = %d", code)
	}
	if !strings.Contains(ls, `"ATM:type:feature"`) {
		t.Fatalf("expected auto-registered ATM:type:feature: %s", ls)
	}
	if !strings.Contains(ls, `"ATM:priority:high"`) {
		t.Fatalf("expected auto-registered ATM:priority:high: %s", ls)
	}
}

func TestGoldenTaskList(t *testing.T) {
	h := newGoldenHarness(t)
	h.seedScenario1()
	out, _, code := h.run("task", "list", "--project", "ATM")
	if code != 0 {
		t.Fatalf("exit = %d stderr=%s", code, h.stderr.String())
	}
	compareGolden(t, "task-list", out)
}

func TestGoldenTaskListFacets(t *testing.T) {
	h := newGoldenHarness(t)
	h.seedScenario1()
	out, _, code := h.run("task", "list", "--project", "ATM", "--label", "ATM:status:*", "--facets")
	if code != 0 {
		t.Fatalf("exit = %d stderr=%s", code, h.stderr.String())
	}
	if !strings.Contains(out, `"groups"`) || !strings.Contains(out, `"others"`) {
		t.Fatalf("facets shape wrong: %s", out)
	}
	compareGolden(t, "task-list-facets", out)
}

func TestGoldenTaskListWildcardLabel(t *testing.T) {
	h := newGoldenHarness(t)
	h.seedScenario1()
	out, _, code := h.run("task", "list", "--project", "ATM", "--label", "ATM:status:*")
	if code != 0 {
		t.Fatalf("exit = %d stderr=%s", code, h.stderr.String())
	}
	compareGolden(t, "task-list-wildcard-label", out)
}

func TestGoldenTaskShow(t *testing.T) {
	h := newGoldenHarness(t)
	h.seedScenario1()
	out, _, code := h.run("task", "show", "--id", "ATM-0001")
	if code != 0 {
		t.Fatalf("exit = %d stderr=%s", code, h.stderr.String())
	}
	compareGolden(t, "task-show", out)
}

func TestGoldenTaskSetTitle(t *testing.T) {
	h := newGoldenHarness(t)
	h.seedScenario1()
	out, _, code := h.run("task", "set-title", "--id", "ATM-0001", "--title", "Reconciled title", "--actor", "admin@cli:unset")
	if code != 0 {
		t.Fatalf("exit = %d stderr=%s", code, h.stderr.String())
	}
	compareGolden(t, "task-set-title", out)
}

func TestGoldenTaskLabelAddRemove(t *testing.T) {
	h := newGoldenHarness(t)
	h.seedScenario1()
	outAdd, _, code := h.run("task", "label", "add", "--id", "ATM-0002", "--label", "ATM:status:open", "--actor", "admin@cli:unset")
	if code != 0 {
		t.Fatalf("add exit = %d stderr=%s", code, h.stderr.String())
	}
	compareGolden(t, "task-label-add", outAdd)

	outRem, _, code := h.run("task", "label", "remove", "--id", "ATM-0002", "--label", "ATM:status:open", "--actor", "admin@cli:unset")
	if code != 0 {
		t.Fatalf("remove exit = %d stderr=%s", code, h.stderr.String())
	}
	compareGolden(t, "task-label-remove", outRem)
}

func TestGoldenTaskRemove(t *testing.T) {
	h := newGoldenHarness(t)
	h.seedScenario1()
	out, _, code := h.run("task", "remove", "--id", "ATM-0001", "--actor", "admin@cli:unset")
	if code != 0 {
		t.Fatalf("exit = %d stderr=%s", code, h.stderr.String())
	}
	compareGolden(t, "task-remove", out)

	_, _, code = h.run("task", "show", "--id", "ATM-0001")
	if code != ExitNotFound {
		t.Fatalf("expected not-found after remove, got %d", code)
	}
}
