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

func TestGoldenProjectSetEmbedding(t *testing.T) {
	h := newGoldenHarness(t)
	sp := h.store.StorePath()
	h.run("init", "--store", sp, "--actor", "tester")
	h.run("project", "create", "--store", sp, "--code", "FOO", "--name", "Foo", "--actor", "tester")
	out, _, code := h.run("project", "set-embedding", "--store", sp, "--project", "FOO", "--model", "nomic-embed-text", "--endpoint", "http://localhost:11434/v1", "--dim", "768", "--threshold", "0.55", "--actor", "tester", "--output", "json")
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, h.stderr.String())
	}
	compareGolden(t, "project-set-embedding", out)
}

func TestGoldenProjectSetEmbeddingRequiresActor(t *testing.T) {
	h := newGoldenHarness(t)
	sp := h.store.StorePath()
	h.run("init", "--store", sp, "--actor", "tester")
	h.run("project", "create", "--store", sp, "--code", "FOO", "--name", "Foo", "--actor", "tester")
	_, _, code := h.run("project", "set-embedding", "--store", sp, "--project", "FOO", "--model", "m", "--endpoint", "http://x", "--dim", "4", "--threshold", "0.5")
	if code != ExitUsage {
		t.Errorf("exit=%d, want %d (missing actor)", code, ExitUsage)
	}
}

func TestGoldenProjectShowEmbedding(t *testing.T) {
	h := newGoldenHarness(t)
	sp := h.store.StorePath()
	h.run("init", "--store", sp, "--actor", "tester")
	h.run("project", "create", "--store", sp, "--code", "FOO", "--name", "Foo", "--actor", "tester")
	h.run("project", "set-embedding", "--store", sp, "--project", "FOO", "--model", "nomic-embed-text", "--endpoint", "http://localhost:11434/v1", "--dim", "768", "--threshold", "0.55", "--actor", "tester")
	out, _, code := h.run("project", "show", "--store", sp, "--code", "FOO", "--output", "json")
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, h.stderr.String())
	}
	compareGolden(t, "project-show-embedding", out)
	if !strings.Contains(out, "embedding") {
		t.Errorf("project show output missing embedding field: %s", out)
	}
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
