package cli

import (
	"strings"
	"testing"
)

func TestGoldenLabelAdd(t *testing.T) {
	h := newGoldenHarness(t)
	sp := h.store.StorePath()
	h.run("init", "--store", sp, "--actor", "admin@cli:unset")
	h.run("project", "create", "--store", sp, "--code", "ATM", "--name", "Agent Tasks Management", "--actor", "admin@cli:unset")
	out, _, code := h.run("label", "add", "--store", sp, "--name", "ATM:type:bug", "--description", "Bug fix", "--actor", "admin@cli:unset")
	if code != 0 {
		t.Fatalf("exit = %d stderr=%s", code, h.stderr.String())
	}
	compareGolden(t, "label-add", out)
}

func TestGoldenLabelAddUpsertPreservesDescription(t *testing.T) {
	h := newGoldenHarness(t)
	sp := h.store.StorePath()
	h.run("init", "--store", sp, "--actor", "admin@cli:unset")
	h.run("project", "create", "--store", sp, "--code", "ATM", "--name", "x", "--actor", "admin@cli:unset")
	h.run("label", "add", "--store", sp, "--name", "ATM:type:bug", "--description", "Bug fix", "--actor", "admin@cli:unset")
	out, _, code := h.run("label", "add", "--store", sp, "--name", "ATM:type:bug", "--actor", "admin@cli:unset")
	if code != 0 {
		t.Fatalf("exit = %d stderr=%s", code, h.stderr.String())
	}
	compareGolden(t, "label-add-upsert-preserves-description", out)
	if !strings.Contains(out, `"description": "Bug fix"`) {
		t.Fatalf("expected description preserved, got %s", out)
	}
}

func TestGoldenLabelRemoveRetainedUsage(t *testing.T) {
	h := newGoldenHarness(t)
	sp := h.store.StorePath()
	h.run("init", "--store", sp, "--actor", "admin@cli:unset")
	h.run("project", "create", "--store", sp, "--code", "ATM", "--name", "x", "--actor", "admin@cli:unset")
	h.run("task", "create", "--store", sp, "--project", "ATM", "--title", "t",
		"--label", "ATM:type:bug", "--actor", "admin@cli:unset")
	out, _, code := h.run("label", "remove", "--store", sp, "--name", "ATM:type:bug", "--actor", "admin@cli:unset")
	if code != 0 {
		t.Fatalf("exit = %d stderr=%s", code, h.stderr.String())
	}
	if !strings.Contains(out, `"retained_usage": 1`) {
		t.Fatalf("missing retained_usage: %s", out)
	}
	compareGolden(t, "label-remove-retained", out)
}

func TestGoldenLabelListByProject(t *testing.T) {
	h := newGoldenHarness(t)
	h.seedScenario1()
	out, _, code := h.run("label", "list", "--project", "ATM")
	if code != 0 {
		t.Fatalf("exit = %d stderr=%s", code, h.stderr.String())
	}
	compareGolden(t, "label-list-by-project", out)
}

func TestGoldenLabelListByNamespace(t *testing.T) {
	h := newGoldenHarness(t)
	h.seedScenario1()
	out, _, code := h.run("label", "list", "--project", "ATM", "--namespace", "type")
	if code != 0 {
		t.Fatalf("exit = %d stderr=%s", code, h.stderr.String())
	}
	compareGolden(t, "label-list-by-namespace", out)
}

func TestGoldenLabelListNamespaceRequiresProject(t *testing.T) {
	h := newGoldenHarness(t)
	_, _, code := h.run("label", "list", "--namespace", "type")
	if code != ExitUsage {
		t.Fatalf("expected exit %d (usage) for --namespace without --project, got %d", ExitUsage, code)
	}
}

func TestGoldenLabelShowNotFound(t *testing.T) {
	h := newGoldenHarness(t)
	sp := h.store.StorePath()
	h.run("init", "--store", sp, "--actor", "admin@cli:unset")
	h.run("project", "create", "--store", sp, "--code", "ATM", "--name", "x", "--actor", "admin@cli:unset")
	_, _, code := h.run("label", "show", "--name", "ATM:type:missing")
	if code != ExitNotFound {
		t.Fatalf("expected exit %d (not-found), got %d", ExitNotFound, code)
	}
}

func TestGoldenLabelSeed(t *testing.T) {
	h := newGoldenHarness(t)
	sp := h.store.StorePath()
	h.run("init", "--store", sp, "--actor", "admin@cli:unset")
	h.run("project", "create", "--store", sp, "--code", "ATM", "--name", "x", "--actor", "admin@cli:unset")
	// Remove one seed label, then re-seed to confirm idempotency.
	h.run("label", "remove", "--store", sp, "--name", "ATM:context:question", "--actor", "admin@cli:unset")
	out, _, code := h.run("label", "seed", "--store", sp, "--project", "ATM", "--actor", "admin@cli:unset")
	if code != 0 {
		t.Fatalf("exit = %d stderr=%s", code, h.stderr.String())
	}
	if !strings.Contains(out, `"seeded": 16`) {
		t.Fatalf("missing seeded: 16 in JSON output: %s", out)
	}
	if !strings.Contains(out, `"ATM:context:question"`) {
		t.Fatalf("missing ATM:context:question in seed output: %s", out)
	}
	compareGolden(t, "label-seed", out)
}

func TestLabelSeedTextOutput(t *testing.T) {
	h := newGoldenHarness(t)
	h.output = outputText
	sp := h.store.StorePath()
	h.run("init", "--store", sp, "--actor", "admin@cli:unset")
	h.run("project", "create", "--store", sp, "--code", "ATM", "--name", "x", "--actor", "admin@cli:unset")
	out, _, code := h.run("label", "seed", "--store", sp, "--project", "ATM", "--actor", "admin@cli:unset")
	if code != 0 {
		t.Fatalf("exit = %d stderr=%s", code, h.stderr.String())
	}
	if !strings.Contains(out, "seeded 16 labels into ATM") {
		t.Fatalf("text output missing 'seeded 16 labels into ATM': %s", out)
	}
}

func TestLabelAddWithExprCreatesBoard(t *testing.T) {
	h := newGoldenHarness(t)
	sp := h.store.StorePath()
	h.run("init", "--store", sp, "--actor", "admin@cli:unset")
	h.run("project", "create", "--store", sp, "--code", "ATM", "--name", "x", "--actor", "admin@cli:unset")
	h.run("label", "add", "--store", sp, "--name", "ATM:status:open", "--actor", "admin@cli:unset")
	h.run("label", "add", "--store", sp, "--name", "ATM:sprint:next", "--actor", "admin@cli:unset")
	_, _, code := h.run("label", "add", "--store", sp, "--name", "ATM:next-sprint",
		"--description", "the sprint board", "--expr", "status:open AND sprint:next",
		"--actor", "admin@cli:unset")
	if code != 0 {
		t.Fatalf("label add --expr exit = %d stderr=%s", code, h.stderr.String())
	}

	out, _, code := h.run("label", "show", "--store", sp, "--name", "ATM:next-sprint")
	if code != 0 {
		t.Fatalf("label show exit = %d stderr=%s", code, h.stderr.String())
	}
	if !strings.Contains(out, "status:open AND sprint:next") {
		t.Fatalf("label show must render the expression; got:\n%s", out)
	}
}

func TestLabelAddRejectsBadExpr(t *testing.T) {
	h := newGoldenHarness(t)
	sp := h.store.StorePath()
	h.run("init", "--store", sp, "--actor", "admin@cli:unset")
	h.run("project", "create", "--store", sp, "--code", "ATM", "--name", "x", "--actor", "admin@cli:unset")
	_, _, code := h.run("label", "add", "--store", sp, "--name", "ATM:bad",
		"--description", "d", "--expr", "status:open AND", "--actor", "admin@cli:unset")
	if code == 0 {
		t.Fatal("a malformed expression must fail the command")
	}
}
