package cli

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestActorMigrateAndAlias(t *testing.T) {
	h := newGoldenHarness(t)
	sp := h.store.StorePath()

	// Seed a project whose creator actor is a legacy string.
	if _, _, code := h.run("project", "create", "--store", sp, "--code", "AAA", "--name", "A", "--actor", "opencode-dev"); code != 0 {
		t.Fatalf("project create failed: code=%d", code)
	}

	out, _, code := h.run("actor", "migrate", "--dry-run", "--output", "json")
	if code != 0 {
		t.Fatalf("actor migrate --dry-run failed: code=%d out=%s", code, out)
	}
	if !strings.Contains(out, "opencode-dev") || !strings.Contains(out, "developer") {
		t.Fatalf("dry-run = %s", out)
	}

	if _, _, code := h.run("actor", "migrate"); code != 0 {
		t.Fatalf("actor migrate failed: code=%d", code)
	}

	out, _, _ = h.run("actor", "alias", "list", "--output", "json")
	var listed struct {
		Aliases map[string]struct{ Persona, Agent string }
	}
	if err := json.Unmarshal([]byte(out), &listed); err != nil {
		t.Fatal(err)
	}
	if listed.Aliases["opencode-dev"].Persona != "developer" {
		t.Fatalf("alias list = %s", out)
	}

	if _, _, code := h.run("actor", "alias", "set", "codex", "--persona", "staff-engineer", "--agent", "codex"); code != 0 {
		t.Fatalf("alias set failed: code=%d", code)
	}
	out, _, _ = h.run("actor", "alias", "list", "--output", "json")
	if !strings.Contains(out, "staff-engineer") {
		t.Fatalf("after set = %s", out)
	}

	if _, _, code := h.run("actor", "alias", "remove", "codex"); code != 0 {
		t.Fatalf("alias remove failed: code=%d", code)
	}
	out, _, _ = h.run("actor", "alias", "list", "--output", "json")
	if strings.Contains(out, "staff-engineer") {
		t.Fatalf("after remove = %s", out)
	}
}
