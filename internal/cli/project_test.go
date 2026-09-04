package cli

import (
	"strings"
	"testing"
)

func TestGoldenProjectCreate(t *testing.T) {
	h := newGoldenHarness(t)
	sp := h.store.StorePath()
	h.run("init", "--store", sp, "--actor", "admin@cli:unset")
	out, _, code := h.run("project", "create", "--store", sp, "--code", "ATM", "--name", "Agent Tasks Management", "--actor", "admin@cli:unset")
	if code != 0 {
		t.Fatalf("exit = %d stderr=%s", code, h.stderr.String())
	}
	compareGolden(t, "project-create", out)
}

func TestProjectCreateEnsuresLaneBoards(t *testing.T) {
	h := newGoldenHarness(t)
	sp := h.store.StorePath()
	h.run("init", "--store", sp, "--actor", "admin@cli:unset")
	_, _, code := h.run("project", "create", "--store", sp, "--code", "FOO", "--name", "Foo", "--actor", "admin@cli:unset")
	if code != 0 {
		t.Fatalf("exit = %d stderr=%s", code, h.stderr.String())
	}
	l, err := h.store.LabelShow("FOO:scrum-inbox")
	if err != nil {
		t.Fatalf("scrum-inbox lane board missing after project create: %v", err)
	}
	if l.Expr == "" {
		t.Error("scrum-inbox board has no expression")
	}
}

func TestGoldenProjectCreateInvalidCode(t *testing.T) {
	h := newGoldenHarness(t)
	sp := h.store.StorePath()
	h.run("init", "--store", sp, "--actor", "admin@cli:unset")
	_, _, code := h.run("project", "create", "--store", sp, "--code", "atm", "--name", "x", "--actor", "admin@cli:unset")
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
	out, _, code := h.run("project", "set-name", "--code", "ATM", "--name", "Renamed Project", "--actor", "admin@cli:unset")
	if code != 0 {
		t.Fatalf("exit = %d stderr=%s", code, h.stderr.String())
	}
	compareGolden(t, "project-set-name", out)
}

func TestGoldenProjectSetEmbedding(t *testing.T) {
	h := newGoldenHarness(t)
	sp := h.store.StorePath()
	h.run("init", "--store", sp, "--actor", "admin@cli:unset")
	h.run("project", "create", "--store", sp, "--code", "FOO", "--name", "Foo", "--actor", "admin@cli:unset")
	out, _, code := h.run("project", "set-embedding", "--store", sp, "--project", "FOO", "--model", "nomic-embed-text", "--endpoint", "http://localhost:11434/v1", "--dim", "768", "--threshold", "0.55", "--actor", "admin@cli:unset", "--output", "json")
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, h.stderr.String())
	}
	compareGolden(t, "project-set-embedding", out)
}

// Actor validation is form-only (plan §7): a well-formed ghost actor writes
// like any other — persona registration is no longer its authority.
func TestGoldenProjectSetEmbeddingAcceptsUnregisteredPersona(t *testing.T) {
	h := newGoldenHarness(t)
	sp := h.store.StorePath()
	h.run("init", "--store", sp, "--actor", "admin@cli:unset")
	h.run("project", "create", "--store", sp, "--code", "FOO", "--name", "Foo", "--actor", "admin@cli:unset")
	_, _, code := h.run("project", "set-embedding", "--store", sp, "--project", "FOO", "--model", "m", "--endpoint", "http://x", "--dim", "4", "--threshold", "0.5", "--actor", "ghost@cli:unset")
	if code != 0 {
		t.Errorf("exit=%d, want 0 (form-only validation)", code)
	}
}

func TestGoldenProjectShowEmbedding(t *testing.T) {
	h := newGoldenHarness(t)
	sp := h.store.StorePath()
	h.run("init", "--store", sp, "--actor", "admin@cli:unset")
	h.run("project", "create", "--store", sp, "--code", "FOO", "--name", "Foo", "--actor", "admin@cli:unset")
	h.run("project", "set-embedding", "--store", sp, "--project", "FOO", "--model", "nomic-embed-text", "--endpoint", "http://localhost:11434/v1", "--dim", "768", "--threshold", "0.55", "--actor", "admin@cli:unset")
	out, _, code := h.run("project", "show", "--store", sp, "--code", "FOO", "--output", "json")
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, h.stderr.String())
	}
	compareGolden(t, "project-show-embedding", out)
	if !strings.Contains(out, "embedding") {
		t.Errorf("project show output missing embedding field: %s", out)
	}
}

func TestGoldenProjectSetChat(t *testing.T) {
	h := newGoldenHarness(t)
	sp := h.store.StorePath()
	h.run("init", "--store", sp, "--actor", "admin@cli:unset")
	h.run("project", "create", "--store", sp, "--code", "FOO", "--name", "Foo", "--actor", "admin@cli:unset")
	out, _, code := h.run("project", "set-chat", "--store", sp, "--project", "FOO", "--model", "qwen3:8b", "--endpoint", "http://localhost:11434/v1", "--actor", "admin@cli:unset", "--output", "json")
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, h.stderr.String())
	}
	compareGolden(t, "project-set-chat", out)
}

func TestGoldenProjectSetInquiryLog(t *testing.T) {
	h := newGoldenHarness(t)
	sp := h.store.StorePath()
	h.run("init", "--store", sp, "--actor", "admin@cli:unset")
	h.run("project", "create", "--store", sp, "--code", "FOO", "--name", "Foo", "--actor", "admin@cli:unset")
	out, _, code := h.run("project", "set-inquiry-log", "--store", sp, "--project", "FOO", "--enabled=false", "--actor", "admin@cli:unset", "--output", "json")
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, h.stderr.String())
	}
	compareGolden(t, "project-set-inquiry-log", out)
}

// Logging is on by default: an unset field must not read as disabled.
func TestProjectInquiryLogDefaultsToEnabled(t *testing.T) {
	h := newGoldenHarness(t)
	sp := h.store.StorePath()
	h.run("init", "--store", sp, "--actor", "admin@cli:unset")
	h.run("project", "create", "--store", sp, "--code", "FOO", "--name", "Foo", "--actor", "admin@cli:unset")
	cfg, err := h.store.GetProjectConfig("FOO")
	if err != nil {
		t.Fatal(err)
	}
	if cfg != nil && cfg.InquiryLog != nil {
		t.Errorf("InquiryLog = %v on a fresh project, want nil so the default is enabled", *cfg.InquiryLog)
	}
}

// The chat endpoint defaults to the embedding one: ollama serves both, so
// making the user repeat the URL would be ceremony.
func TestProjectSetChatBorrowsEmbeddingEndpoint(t *testing.T) {
	h := newGoldenHarness(t)
	sp := h.store.StorePath()
	h.run("init", "--store", sp, "--actor", "admin@cli:unset")
	h.run("project", "create", "--store", sp, "--code", "FOO", "--name", "Foo", "--actor", "admin@cli:unset")
	h.run("project", "set-embedding", "--store", sp, "--project", "FOO", "--model", "nomic-embed-text", "--endpoint", "http://localhost:11434/v1", "--dim", "768", "--threshold", "0.55", "--actor", "admin@cli:unset")
	out, _, code := h.run("project", "set-chat", "--store", sp, "--project", "FOO", "--model", "qwen3:8b", "--actor", "admin@cli:unset", "--output", "json")
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, h.stderr.String())
	}
	if !strings.Contains(out, "http://localhost:11434/v1") {
		t.Errorf("output = %s, want the borrowed embedding endpoint", out)
	}
}

// With no endpoint to borrow, the command says what to run rather than
// storing a chat config that points nowhere.
func TestProjectSetChatWithoutAnyEndpointIsUsage(t *testing.T) {
	h := newGoldenHarness(t)
	sp := h.store.StorePath()
	h.run("init", "--store", sp, "--actor", "admin@cli:unset")
	h.run("project", "create", "--store", sp, "--code", "FOO", "--name", "Foo", "--actor", "admin@cli:unset")
	_, stderr, code := h.run("project", "set-chat", "--store", sp, "--project", "FOO", "--model", "qwen3:8b", "--actor", "admin@cli:unset")
	if code != ExitUsage {
		t.Errorf("exit=%d, want %d", code, ExitUsage)
	}
	if !strings.Contains(stderr, "set-embedding") {
		t.Errorf("stderr = %q, want it to name set-embedding", stderr)
	}
}

func TestGoldenProjectShowChat(t *testing.T) {
	h := newGoldenHarness(t)
	sp := h.store.StorePath()
	h.run("init", "--store", sp, "--actor", "admin@cli:unset")
	h.run("project", "create", "--store", sp, "--code", "FOO", "--name", "Foo", "--actor", "admin@cli:unset")
	h.run("project", "set-chat", "--store", sp, "--project", "FOO", "--model", "qwen3:8b", "--endpoint", "http://localhost:11434/v1", "--actor", "admin@cli:unset")
	out, _, code := h.run("project", "show", "--store", sp, "--code", "FOO", "--output", "json")
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, h.stderr.String())
	}
	compareGolden(t, "project-show-chat", out)
	if !strings.Contains(out, "\"chat\"") {
		t.Errorf("project show output missing chat field: %s", out)
	}
}

func TestGoldenProjectRemoveZeroTaskGuard(t *testing.T) {
	h := newGoldenHarness(t)
	sp := h.store.StorePath()
	h.run("init", "--store", sp, "--actor", "admin@cli:unset")
	h.run("project", "create", "--store", sp, "--code", "EMP", "--name", "Empty", "--actor", "admin@cli:unset")
	out, _, code := h.run("project", "remove", "--code", "EMP", "--actor", "admin@cli:unset")
	if code != 0 {
		t.Fatalf("exit = %d stderr=%s", code, h.stderr.String())
	}
	compareGolden(t, "project-remove-zero-task", out)

	h.seedScenario1()
	_, _, code = h.run("project", "remove", "--code", "ATM", "--actor", "admin@cli:unset")
	if code != ExitConflict {
		t.Fatalf("expected conflict exit for project with tasks, got %d", code)
	}
}
