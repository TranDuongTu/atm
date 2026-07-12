package cli

import "testing"

func TestInitSelectsFirstInstalledAgent(t *testing.T) {
	h := newGoldenHarness(t)
	// Install writes plugin files under HOME; point it at a temp dir so the
	// install is real and isolated.
	t.Setenv("HOME", t.TempDir())

	if _, _, code := h.run("init", "--agent", "opencode"); code != ExitSuccess {
		t.Fatalf("init exit=%d stderr=%s", code, h.stderr.String())
	}
	cfg, err := h.store.GetAgentsConfig()
	if err != nil {
		t.Fatalf("GetAgentsConfig: %v", err)
	}
	if cfg.Selected != "opencode" {
		t.Fatalf("init did not select installed agent: %q", cfg.Selected)
	}
}

func TestInitNoAgentLeavesSelectionEmpty(t *testing.T) {
	h := newGoldenHarness(t)
	t.Setenv("HOME", t.TempDir())
	// No --agent and non-interactive: nothing installed, nothing selected.
	if _, _, code := h.run("init"); code != ExitSuccess {
		t.Fatalf("init exit=%d", code)
	}
	cfg, _ := h.store.GetAgentsConfig()
	if cfg.Selected != "" {
		t.Fatalf("expected empty selection, got %q", cfg.Selected)
	}
}
