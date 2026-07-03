package cli

import (
	"strings"
	"testing"
)

func TestGoldenLabelAdd(t *testing.T) {
	h := newGoldenHarness(t)
	sp := h.store.StorePath()
	h.run("init", "--store", sp, "--actor", "claude")
	h.run("project", "create", "--store", sp, "--code", "ATM", "--name", "Agent Tasks Management", "--actor", "claude")
	out, _, code := h.run("label", "add", "--store", sp, "--name", "ATM:type:bug", "--description", "Bug fix", "--actor", "claude")
	if code != 0 {
		t.Fatalf("exit = %d stderr=%s", code, h.stderr.String())
	}
	compareGolden(t, "label-add", out)
}

func TestGoldenLabelAddUpsertPreservesDescription(t *testing.T) {
	h := newGoldenHarness(t)
	sp := h.store.StorePath()
	h.run("init", "--store", sp, "--actor", "claude")
	h.run("project", "create", "--store", sp, "--code", "ATM", "--name", "x", "--actor", "claude")
	h.run("label", "add", "--store", sp, "--name", "ATM:type:bug", "--description", "Bug fix", "--actor", "claude")
	out, _, code := h.run("label", "add", "--store", sp, "--name", "ATM:type:bug", "--actor", "claude")
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
	h.run("init", "--store", sp, "--actor", "claude")
	h.run("project", "create", "--store", sp, "--code", "ATM", "--name", "x", "--actor", "claude")
	h.run("task", "create", "--store", sp, "--project", "ATM", "--title", "t",
		"--label", "ATM:type:bug", "--actor", "claude")
	out, _, code := h.run("label", "remove", "--store", sp, "--name", "ATM:type:bug", "--actor", "claude")
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
	h.run("init", "--store", sp, "--actor", "claude")
	h.run("project", "create", "--store", sp, "--code", "ATM", "--name", "x", "--actor", "claude")
	_, _, code := h.run("label", "show", "--name", "ATM:type:missing")
	if code != ExitNotFound {
		t.Fatalf("expected exit %d (not-found), got %d", ExitNotFound, code)
	}
}
