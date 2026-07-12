package cli

import (
	"testing"

	"atm/internal/store"
)

func TestResolveAgentNameOrder(t *testing.T) {
	t.Setenv("ATM_AGENT", "")
	cfg := store.AgentsConfig{Selected: "codex"}

	name, err := resolveAgentName("opencode", cfg)
	if err != nil || name != "opencode" {
		t.Fatalf("flag should win: %q %v", name, err)
	}

	t.Setenv("ATM_AGENT", "claude")
	name, err = resolveAgentName("", cfg)
	if err != nil || name != "claude" {
		t.Fatalf("env should win over selected: %q %v", name, err)
	}

	t.Setenv("ATM_AGENT", "")
	name, err = resolveAgentName("", cfg)
	if err != nil || name != "codex" {
		t.Fatalf("selected should be used: %q %v", name, err)
	}

	name, err = resolveAgentName("", store.AgentsConfig{})
	if err == nil {
		t.Fatalf("expected usage error, got %q", name)
	}
}

func TestResolveEntryValidatesCatalog(t *testing.T) {
	t.Setenv("ATM_AGENT", "")
	cfg := store.AgentsConfig{Selected: "ollama:opencode", Args: map[string][]string{"ollama:opencode": {"--yolo"}}}
	e, args, err := resolveEntry("", cfg)
	if err != nil {
		t.Fatalf("resolveEntry: %v", err)
	}
	if e.Launcher != "ollama" || e.Integration != "opencode" {
		t.Fatalf("entry = %+v", e)
	}
	if len(args) != 1 || args[0] != "--yolo" {
		t.Fatalf("args = %v", args)
	}

	if _, _, err := resolveEntry("gemini", cfg); err == nil {
		t.Fatal("expected error for unknown agent name")
	}
}

func TestDevAndManageLauncherFor(t *testing.T) {
	e, _, err := resolveEntry("ollama:codex", store.AgentsConfig{Selected: "ollama:codex"})
	if err != nil {
		t.Fatal(err)
	}
	if dl, ok := devLauncherFor(e); !ok || dl.Name() != "ollama" {
		t.Fatalf("dev launcher ok=%v name=%v", ok, dl)
	}
	if ml, ok := manageLauncherFor(e); !ok || ml.Name() != "ollama" {
		t.Fatalf("manage launcher ok=%v name=%v", ok, ml)
	}
}
