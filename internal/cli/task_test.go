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

// TestTaskListRejectsFacetTokenWithoutFacets covers ATM-8289dc: the --label
// flag advertises wildcard suffixes but silently drops them, returning the
// full roster. A facet token (ending in :*) is a facet declaration, not a
// filter token; without --facets it has no meaning, so the CLI must reject it
// and point the user at --expr (which accepts namespace predicates like
// "status:*"). The bare "*" tautology atom is NOT a facet token (it has no ":"
// prefix) and remains a valid filter token — covered separately by
// TestTaskListAllTasksBoardAndStarFilterPinsTautology.
func TestTaskListRejectsFacetTokenWithoutFacets(t *testing.T) {
	h := newGoldenHarness(t)
	h.seedScenario1()

	// A facet token (namespace wildcard) without --facets is a usage error.
	_, stderr, code := h.run("task", "list", "--project", "ATM", "--label", "ATM:status:*")
	if code == ExitSuccess {
		t.Fatalf("ATM:status:* without --facets: got exit 0, want non-zero; stderr=%s", h.stderr.String())
	}
	if !strings.Contains(stderr, "--expr") {
		t.Errorf("error must point at --expr for namespace predicates; stderr=%s", stderr)
	}

	// A board-style facet token (whole-namespace wildcard) is also rejected.
	_, stderr, code = h.run("task", "list", "--project", "ATM", "--label", "ATM:*")
	if code == ExitSuccess {
		t.Fatalf("ATM:* without --facets: got exit 0, want non-zero; stderr=%s", h.stderr.String())
	}
	if !strings.Contains(stderr, "--expr") {
		t.Errorf("error must point at --expr; stderr=%s", stderr)
	}

	// The same token WITH --facets stays valid: it is a facet declaration.
	_, _, code = h.run("task", "list", "--project", "ATM", "--label", "ATM:status:*", "--facets")
	if code != ExitSuccess {
		t.Fatalf("ATM:status:* with --facets must still work; exit=%d stderr=%s", code, h.stderr.String())
	}
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

// TestTaskListAllTasksBoardAndStarFilterPinsTautology covers the all-tasks
// board (expr '*') and the standalone '*' filter token end-to-end through the
// CLI. Both must return every task, including an unlabeled naked jotting. The
// unlabeled task is the load-bearing assertion: a board expression that used
// the <CODE>:* namespace predicate as its membership would miss it
// (qualify('*') in evalAtom yields <CODE>:* and reads as "has any label"),
// which is exactly the bug the '*' tautology atom exists to fix — see
// internal/store/resolve.go and TestResolverStarTautologyMatchesEveryTask.
//
// Note: this test does NOT contrast with `--label ATM:*` at the CLI, because
// that token is a facet token (IsWildcard true), not a filter token — it
// declares a facet for grouping rather than filtering, so unlabeled tasks
// land in its `others` bucket. The CLI rejects `--label ATM:*` without
// --facets (see TestTaskListRejectsFacetTokenWithoutFacets); the '*' vs
// <CODE>:* distinction lives in the expression evaluator (evalAtom), where
// the tautology short-circuits, and the unit test there pins the contrast.
func TestTaskListAllTasksBoardAndStarFilterPinsTautology(t *testing.T) {
	h := newGoldenHarness(t)
	sp := h.store.StorePath()
	h.run("init", "--store", sp, "--actor", "admin@cli:unset")
	h.run("project", "create", "--store", sp, "--code", "ATM", "--name", "x", "--actor", "admin@cli:unset")
	// No capability seeds a catch-all board any more, so the tautology this
	// test pins gets its board authored the substrate way.
	h.run("label", "add", "--store", sp, "--name", "ATM:all-tasks", "--expr", "*",
		"--description", "every task", "--actor", "admin@cli:unset")
	h.run("task", "create", "--store", sp, "--project", "ATM", "--title", "open-task", "--label", "ATM:status:open", "--actor", "admin@cli:unset")
	h.run("task", "create", "--store", sp, "--project", "ATM", "--title", "done-task", "--label", "ATM:status:done", "--actor", "admin@cli:unset")
	h.run("task", "create", "--store", sp, "--project", "ATM", "--title", "wip-task", "--label", "ATM:status:in-progress", "--actor", "admin@cli:unset")
	// The naked jotting: no labels at all.
	h.run("task", "create", "--store", sp, "--project", "ATM", "--title", "naked-jotting", "--actor", "admin@cli:unset")

	want := []string{"open-task", "done-task", "wip-task", "naked-jotting"}

	// The all-tasks board resolves through its expr '*'.
	outBoard, _, code := h.run("task", "list", "--store", sp, "--project", "ATM", "--label", "ATM:all-tasks")
	if code != 0 {
		t.Fatalf("all-tasks board exit = %d stderr=%s", code, h.stderr.String())
	}
	for _, title := range want {
		if !strings.Contains(outBoard, title) {
			t.Errorf("all-tasks board missing %q:\n%s", title, outBoard)
		}
	}

	// The standalone '*' filter token reaches evalAtom with Name="*"
	// and short-circuits to true. Passed as a literal Go arg so no shell
	// glob interferes. ('*' is a filter token, not a facet token: no ":*"
	// suffix, so it passes the CLI reject gate.)
	outStar, _, code := h.run("task", "list", "--store", sp, "--project", "ATM", "--label", "*")
	if code != 0 {
		t.Fatalf("'*' filter exit = %d stderr=%s", code, h.stderr.String())
	}
	for _, title := range want {
		if !strings.Contains(outStar, title) {
			t.Errorf("'*' filter missing %q:\n%s", title, outStar)
		}
	}
}
