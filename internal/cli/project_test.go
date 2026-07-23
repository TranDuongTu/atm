package cli

import (
	"encoding/json"
	"strings"
	"testing"

	"atm/internal/tui/art"
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

func TestProjectCreateEnsuresOpenTasksBoard(t *testing.T) {
	h := newGoldenHarness(t)
	sp := h.store.StorePath()
	h.run("init", "--store", sp, "--actor", "admin@cli:unset")
	_, _, code := h.run("project", "create", "--store", sp, "--code", "FOO", "--name", "Foo", "--actor", "admin@cli:unset")
	if code != 0 {
		t.Fatalf("exit = %d stderr=%s", code, h.stderr.String())
	}
	l, err := h.store.LabelShow("FOO:open-tasks")
	if err != nil {
		t.Fatalf("open-tasks board missing after project create: %v", err)
	}
	if l.Expr == "" {
		t.Error("open-tasks board has no expression")
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

func TestGoldenProjectSetEmbeddingRejectsUnregisteredPersona(t *testing.T) {
	h := newGoldenHarness(t)
	sp := h.store.StorePath()
	h.run("init", "--store", sp, "--actor", "admin@cli:unset")
	h.run("project", "create", "--store", sp, "--code", "FOO", "--name", "Foo", "--actor", "admin@cli:unset")
	_, _, code := h.run("project", "set-embedding", "--store", sp, "--project", "FOO", "--model", "m", "--endpoint", "http://x", "--dim", "4", "--threshold", "0.5", "--actor", "ghost@cli:unset")
	if code != ExitUsage {
		t.Errorf("exit=%d, want %d (unregistered persona)", code, ExitUsage)
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

// TestGoldenProjectBoardsHideShowReorder: display preferences round-trip
// through config.json.boards; reorder materializes the effective ring order.
func TestGoldenProjectBoardsHideShowReorder(t *testing.T) {
	h := newGoldenHarness(t)
	h.run("project", "create", "--code", "PBX", "--name", "boards demo",
		"--capabilities", "workflow", "--actor", "admin@cli:unset")

	out, _, code := h.run("--output", "json", "project", "boards", "hide",
		"--project", "PBX", "--name", "PBX:backlog", "--actor", "admin@cli:unset")
	if code != 0 {
		t.Fatalf("hide exit %d: %s", code, out)
	}
	compareGolden(t, "project-boards-hide", out)

	// Hiding is idempotent.
	if _, _, code := h.run("project", "boards", "hide", "--project", "PBX",
		"--name", "PBX:backlog", "--actor", "admin@cli:unset"); code != 0 {
		t.Fatalf("second hide exit %d", code)
	}

	out2, _, code := h.run("--output", "json", "project", "boards", "reorder",
		"--project", "PBX", "--name", "PBX:status:*", "--first", "--actor", "admin@cli:unset")
	if code != 0 {
		t.Fatalf("reorder exit %d: %s", code, out2)
	}
	compareGolden(t, "project-boards-reorder-first", out2)

	out3, _, code := h.run("--output", "json", "project", "boards", "show",
		"--project", "PBX", "--name", "PBX:backlog", "--actor", "admin@cli:unset")
	if code != 0 {
		t.Fatalf("show exit %d: %s", code, out3)
	}
	compareGolden(t, "project-boards-show", out3)
}

func TestProjectBoardsReorderValidation(t *testing.T) {
	h := newGoldenHarness(t)
	h.run("project", "create", "--code", "PBX", "--name", "boards demo",
		"--capabilities", "workflow", "--actor", "admin@cli:unset")
	// Exactly one placement flag required.
	if _, _, code := h.run("project", "boards", "reorder", "--project", "PBX",
		"--name", "PBX:backlog", "--actor", "admin@cli:unset"); code == 0 {
		t.Fatal("reorder with no placement flag must fail")
	}
	if _, _, code := h.run("project", "boards", "reorder", "--project", "PBX",
		"--name", "PBX:backlog", "--first", "--last", "--actor", "admin@cli:unset"); code == 0 {
		t.Fatal("reorder with two placement flags must fail")
	}
	// Unknown board name errors (nothing to move).
	if _, _, code := h.run("project", "boards", "reorder", "--project", "PBX",
		"--name", "PBX:nosuch", "--first", "--actor", "admin@cli:unset"); code == 0 {
		t.Fatal("reorder of a name not in the effective ring must fail")
	}
	// name == anchor is a usage error, not a panic.
	if _, _, code := h.run("project", "boards", "reorder", "--project", "PBX",
		"--name", "PBX:backlog", "--before", "PBX:backlog", "--actor", "admin@cli:unset"); code == 0 {
		t.Fatal("reorder --name X --before X must fail (not panic)")
	}
	if _, _, code := h.run("project", "boards", "reorder", "--project", "PBX",
		"--name", "PBX:backlog", "--after", "PBX:backlog", "--actor", "admin@cli:unset"); code == 0 {
		t.Fatal("reorder --name X --after X must fail (not panic)")
	}
}

// TestProjectTheme covers show (unpinned/auto), pin, clear-to-auto, and the
// invalid-name usage error. Display preference: config.json only, no
// store-event side effects to assert.
func TestProjectTheme(t *testing.T) {
	t.Run("show when unpinned reports auto plus available list", func(t *testing.T) {
		h := newGoldenHarness(t)
		h.run("project", "create", "--code", "ATM", "--name", "Agent Tasks Management", "--actor", "admin@cli:unset")

		out, _, code := h.run("project", "theme", "ATM")
		if code != 0 {
			t.Fatalf("exit = %d stderr=%s", code, h.stderr.String())
		}
		var shown struct {
			Project   string   `json:"project"`
			Theme     string   `json:"theme"`
			Mode      string   `json:"mode"`
			Available []string `json:"available"`
		}
		if err := json.Unmarshal([]byte(out), &shown); err != nil {
			t.Fatalf("unmarshal show output: %v: %s", err, out)
		}
		if shown.Mode != "auto" {
			t.Errorf("expected mode %q, got %q: %s", "auto", shown.Mode, out)
		}
		wantTheme := art.Effective("", "ATM").Name()
		if shown.Theme != wantTheme {
			t.Errorf("expected auto-assigned theme %q, got %q: %s", wantTheme, shown.Theme, out)
		}
		for _, name := range []string{"waves", "starfield", "circuit", "rain", "dunes"} {
			if !strings.Contains(out, name) {
				t.Errorf("expected available theme %q listed in output: %s", name, out)
			}
		}
	})

	t.Run("pinning a valid theme is reflected by a following show", func(t *testing.T) {
		h := newGoldenHarness(t)
		h.run("project", "create", "--code", "ATM", "--name", "Agent Tasks Management", "--actor", "admin@cli:unset")

		_, _, code := h.run("project", "theme", "ATM", "circuit", "--actor", "admin@cli:unset")
		if code != 0 {
			t.Fatalf("pin exit = %d stderr=%s", code, h.stderr.String())
		}

		out, _, code := h.run("project", "theme", "ATM")
		if code != 0 {
			t.Fatalf("show exit = %d stderr=%s", code, h.stderr.String())
		}
		if !strings.Contains(out, "circuit") {
			t.Errorf("expected pinned theme circuit in show output: %s", out)
		}
		if !strings.Contains(out, "pinned") {
			t.Errorf("expected mode pinned in show output: %s", out)
		}
	})

	t.Run("auto clears the pin", func(t *testing.T) {
		h := newGoldenHarness(t)
		h.run("project", "create", "--code", "ATM", "--name", "Agent Tasks Management", "--actor", "admin@cli:unset")
		h.run("project", "theme", "ATM", "circuit", "--actor", "admin@cli:unset")

		_, _, code := h.run("project", "theme", "ATM", "auto", "--actor", "admin@cli:unset")
		if code != 0 {
			t.Fatalf("auto exit = %d stderr=%s", code, h.stderr.String())
		}
		cfg, err := h.store.GetProjectConfig("ATM")
		if err != nil {
			t.Fatalf("GetProjectConfig: %v", err)
		}
		if cfg != nil && cfg.ArtTheme != "" {
			t.Errorf("expected ArtTheme cleared, got %q", cfg.ArtTheme)
		}
	})

	t.Run("invalid theme name errors and leaves config unchanged", func(t *testing.T) {
		h := newGoldenHarness(t)
		h.run("project", "create", "--code", "ATM", "--name", "Agent Tasks Management", "--actor", "admin@cli:unset")
		h.run("project", "theme", "ATM", "circuit", "--actor", "admin@cli:unset")

		out, stderr, code := h.run("project", "theme", "ATM", "bogus", "--actor", "admin@cli:unset")
		if code == 0 {
			t.Fatalf("expected non-zero exit for invalid theme name, got 0: out=%s", out)
		}
		combined := out + stderr
		for _, name := range []string{"waves", "starfield", "circuit", "rain", "dunes"} {
			if !strings.Contains(combined, name) {
				t.Errorf("expected valid theme %q named in error: %s", name, combined)
			}
		}
		cfg, err := h.store.GetProjectConfig("ATM")
		if err != nil {
			t.Fatalf("GetProjectConfig: %v", err)
		}
		if cfg == nil || cfg.ArtTheme != "circuit" {
			t.Errorf("expected config unchanged (still pinned circuit), got %+v", cfg)
		}
	})
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
