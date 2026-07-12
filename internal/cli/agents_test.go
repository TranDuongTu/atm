package cli

import (
	"strings"
	"testing"
)

func TestAgentsSelectThenList(t *testing.T) {
	h := newGoldenHarness(t)

	// select an entry
	if _, _, code := h.run("agents", "select", "opencode"); code != ExitSuccess {
		t.Fatalf("agents select exit=%d", code)
	}
	cfg, err := h.store.GetAgentsConfig()
	if err != nil || cfg.Selected != "opencode" {
		t.Fatalf("selected not persisted: %q %v", cfg.Selected, err)
	}

	// unknown name errors
	if _, _, code := h.run("agents", "select", "gemini"); code == ExitSuccess {
		t.Fatal("expected non-zero exit selecting unknown agent")
	}

	// list mentions the selected entry
	stdout, _, code := h.run("agents", "list")
	if code != ExitSuccess {
		t.Fatalf("agents list exit=%d", code)
	}
	if !strings.Contains(stdout, "opencode") {
		t.Fatalf("list output missing opencode: %s", stdout)
	}
}

func TestAgentsArgsGetSet(t *testing.T) {
	h := newGoldenHarness(t)
	if _, _, code := h.run("agents", "args", "codex", "--", "--foo", "--bar"); code != ExitSuccess {
		t.Fatalf("set args exit=%d", code)
	}
	cfg, _ := h.store.GetAgentsConfig()
	if got := cfg.Args["codex"]; len(got) != 2 || got[0] != "--foo" || got[1] != "--bar" {
		t.Fatalf("args not stored: %v", got)
	}
}
