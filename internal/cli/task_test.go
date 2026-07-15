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
		t.Fatalf("exit = %d stderr=%s", code, h.stderr)
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
		t.Fatalf("exit = %d stderr=%s", code, h.stderr)
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
		t.Fatalf("exit = %d stderr=%s", code, h.stderr)
	}
	compareGolden(t, "task-list", out)
}

func TestGoldenTaskListFacets(t *testing.T) {
	h := newGoldenHarness(t)
	h.seedScenario1()
	out, _, code := h.run("task", "list", "--project", "ATM", "--label", "ATM:status:*", "--facets")
	if code != 0 {
		t.Fatalf("exit = %d stderr=%s", code, h.stderr)
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
		t.Fatalf("exit = %d stderr=%s", code, h.stderr)
	}
	compareGolden(t, "task-list-wildcard-label", out)
}

func TestGoldenTaskShow(t *testing.T) {
	h := newGoldenHarness(t)
	tk1, _ := h.seedScenario1()
	out, _, code := h.run("task", "show", "--id", tk1)
	if code != 0 {
		t.Fatalf("exit = %d stderr=%s", code, h.stderr)
	}
	compareGolden(t, "task-show", out)
}

func TestGoldenTaskSetTitle(t *testing.T) {
	h := newGoldenHarness(t)
	tk1, _ := h.seedScenario1()
	out, _, code := h.run("task", "set-title", "--id", tk1, "--title", "Reconciled title", "--actor", "admin@cli:unset")
	if code != 0 {
		t.Fatalf("exit = %d stderr=%s", code, h.stderr)
	}
	compareGolden(t, "task-set-title", out)
}

func TestGoldenTaskLabelAddRemove(t *testing.T) {
	h := newGoldenHarness(t)
	_, tk2 := h.seedScenario1()
	outAdd, _, code := h.run("task", "label", "add", "--id", tk2, "--label", "ATM:status:open", "--actor", "admin@cli:unset")
	if code != 0 {
		t.Fatalf("add exit = %d stderr=%s", code, h.stderr)
	}
	compareGolden(t, "task-label-add", outAdd)

	outRem, _, code := h.run("task", "label", "remove", "--id", tk2, "--label", "ATM:status:open", "--actor", "admin@cli:unset")
	if code != 0 {
		t.Fatalf("remove exit = %d stderr=%s", code, h.stderr)
	}
	compareGolden(t, "task-label-remove", outRem)
}

func TestGoldenTaskRemove(t *testing.T) {
	h := newGoldenHarness(t)
	tk1, _ := h.seedScenario1()
	out, _, code := h.run("task", "remove", "--id", tk1, "--actor", "admin@cli:unset")
	if code != 0 {
		t.Fatalf("exit = %d stderr=%s", code, h.stderr)
	}
	compareGolden(t, "task-remove", out)

	_, _, code = h.run("task", "show", "--id", tk1)
	if code != ExitNotFound {
		t.Fatalf("expected not-found after remove, got %d", code)
	}
}

// TestTaskIDFlagCanonicalTask verifies --task is the canonical task-id flag on
// every task-level subcommand and produces no deprecation warning on stderr.
func TestTaskIDFlagCanonicalTask(t *testing.T) {
	h := newGoldenHarness(t)
	tk1, tk2 := h.seedScenario1()

	// show
	_, stderr, code := h.run("task", "show", "--task", tk1)
	if code != 0 {
		t.Fatalf("show --task exit = %d stderr=%s", code, stderr)
	}
	if strings.Contains(stderr, "deprecated") {
		t.Fatalf("show --task emitted deprecation: %s", stderr)
	}

	// set-title
	_, stderr, code = h.run("task", "set-title", "--task", tk1, "--title", "Via task flag", "--actor", "admin@cli:unset")
	if code != 0 {
		t.Fatalf("set-title --task exit = %d stderr=%s", code, stderr)
	}
	if strings.Contains(stderr, "deprecated") {
		t.Fatalf("set-title --task emitted deprecation: %s", stderr)
	}

	// set-description
	_, stderr, code = h.run("task", "set-description", "--task", tk1, "--description", "desc via task flag", "--actor", "admin@cli:unset")
	if code != 0 {
		t.Fatalf("set-description --task exit = %d stderr=%s", code, stderr)
	}
	if strings.Contains(stderr, "deprecated") {
		t.Fatalf("set-description --task emitted deprecation: %s", stderr)
	}

	// label add
	_, stderr, code = h.run("task", "label", "add", "--task", tk2, "--label", "ATM:status:open", "--actor", "admin@cli:unset")
	if code != 0 {
		t.Fatalf("label add --task exit = %d stderr=%s", code, stderr)
	}
	if strings.Contains(stderr, "deprecated") {
		t.Fatalf("label add --task emitted deprecation: %s", stderr)
	}

	// label remove
	_, stderr, code = h.run("task", "label", "remove", "--task", tk2, "--label", "ATM:status:open", "--actor", "admin@cli:unset")
	if code != 0 {
		t.Fatalf("label remove --task exit = %d stderr=%s", code, stderr)
	}
	if strings.Contains(stderr, "deprecated") {
		t.Fatalf("label remove --task emitted deprecation: %s", stderr)
	}
}

// TestTaskIDFlagDeprecatedAlias verifies --id still works on task-level
// subcommands as a backwards-compatible alias and emits a deprecation notice.
func TestTaskIDFlagDeprecatedAlias(t *testing.T) {
	h := newGoldenHarness(t)
	tk1, _ := h.seedScenario1()

	_, stderr, code := h.run("task", "show", "--id", tk1)
	if code != 0 {
		t.Fatalf("show --id exit = %d stderr=%s", code, stderr)
	}
	if !strings.Contains(stderr, "deprecated") {
		t.Fatalf("show --id should warn deprecation, got stderr=%s", stderr)
	}
}

// TestTaskIDFlagNeitherSet verifies the missing-flag error when neither --task
// nor --id is supplied.
func TestTaskIDFlagNeitherSet(t *testing.T) {
	h := newGoldenHarness(t)
	h.seedScenario1()

	_, _, code := h.run("task", "show")
	if code == 0 {
		t.Fatalf("expected non-zero exit when neither --task nor --id set")
	}
}

func TestTaskListWithExpr(t *testing.T) {
	h := newGoldenHarness(t)
	sp := h.store.StorePath()
	h.run("init", "--store", sp, "--actor", "admin@cli:unset")
	h.run("project", "create", "--store", sp, "--code", "ATM", "--name", "x", "--actor", "admin@cli:unset")
	h.run("task", "create", "--store", sp, "--project", "ATM", "--title", "open-task", "--label", "ATM:status:open", "--actor", "admin@cli:unset")
	h.run("task", "create", "--store", sp, "--project", "ATM", "--title", "done-task", "--label", "ATM:status:done", "--actor", "admin@cli:unset")

	out, _, code := h.run("task", "list", "--store", sp, "--project", "ATM", "--expr", "NOT status:done")
	if code != 0 {
		t.Fatalf("exit = %d stderr=%s", code, h.stderr.String())
	}
	if !strings.Contains(out, "open-task") || strings.Contains(out, "done-task") {
		t.Fatalf("--expr must filter; got:\n%s", out)
	}
}
