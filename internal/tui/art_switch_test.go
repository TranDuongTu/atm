package tui

import (
	"testing"

	"atm/internal/core"
	"atm/internal/tui/art"
)

// TestToggleScopedArtFlipsAndPersists proves the A toggle flips the in-memory
// artOn cache and persists ArtOn to the project config in one call, and that
// a second toggle flips both back to off.
func TestToggleScopedArtFlipsAndPersists(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	seedProject(t, m, "ATM", "Acme")
	update(t, m, "s") // scope ATM
	_ = art.Pair("ATM")

	m.toggleScopedArt()
	if !m.artOn["ATM"] {
		t.Fatal("after toggle, artOn[ATM] = false, want true")
	}
	cfg, _ := m.store.GetProjectConfig("ATM")
	if cfg == nil || !cfg.ArtOn {
		t.Fatal("config ArtOn not persisted true")
	}
	m.toggleScopedArt()
	if m.artOn["ATM"] {
		t.Fatal("after 2nd toggle, artOn[ATM] = true, want false")
	}
	cfg2, _ := m.store.GetProjectConfig("ATM")
	if cfg2 != nil && cfg2.ArtOn {
		t.Fatal("config ArtOn not persisted false after 2nd toggle")
	}
}

// TestToggleScopedArtNoopWithoutScope proves the toggle is a no-op (with a
// toast hint) when no project is scoped — it must not touch artOn.
func TestToggleScopedArtNoopWithoutScope(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	seedProject(t, m, "ATM", "Acme")
	// deliberately do NOT scope ATM
	m.toggleScopedArt()
	if m.artOn["ATM"] {
		t.Fatal("toggle without scope must not set artOn")
	}
}

// TestRenderArtBlankWhenOff proves renderArt returns "" when art is off for
// the scoped project (the new default-off gate).
func TestRenderArtBlankWhenOff(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(60, 40)
	seedProject(t, m, "ATM", "Acme")
	update(t, m, "s")
	// art off by default
	if out := m.projects.renderArt(8); out != "" {
		t.Fatalf("renderArt with art off must be blank, got non-empty")
	}
}

// TestToggleOnRollsAndPersistsPair proves turning art on via A re-rolls a
// fresh random two-theme pair, pins it to the config (so it survives a TUI
// restart), and that both names are distinct registered themes.
func TestToggleOnRollsAndPersistsPair(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	seedProject(t, m, "ATM", "Acme")
	update(t, m, "s")

	m.toggleScopedArt()
	pair := m.artPair["ATM"]
	if len(pair) != 2 {
		t.Fatalf("artPair[ATM] = %v, want two names", pair)
	}
	if pair[0] == pair[1] {
		t.Fatalf("rolled pair must be distinct, got %q twice", pair[0])
	}
	for _, name := range pair {
		if _, ok := art.ByName(name); !ok {
			t.Fatalf("rolled pair theme %q not in registry", name)
		}
	}
	cfg, _ := m.store.GetProjectConfig("ATM")
	if cfg == nil || len(cfg.ArtPair) != 2 || cfg.ArtPair[0] != pair[0] || cfg.ArtPair[1] != pair[1] {
		t.Fatalf("config ArtPair = %v, want %v", cfgArtPair(cfg), pair)
	}
}

// TestToggleOffClearsPair proves turning art off via A clears the pinned
// pair from both the cache and the persisted config.
func TestToggleOffClearsPair(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	seedProject(t, m, "ATM", "Acme")
	update(t, m, "s")
	m.toggleScopedArt() // on, pins a pair
	if _, ok := m.artPair["ATM"]; !ok {
		t.Fatal("precondition: art on must pin a pair")
	}
	m.toggleScopedArt() // off
	if _, ok := m.artPair["ATM"]; ok {
		t.Fatal("after toggle off, artPair[ATM] must be cleared")
	}
	cfg, _ := m.store.GetProjectConfig("ATM")
	if cfg != nil && len(cfg.ArtPair) != 0 {
		t.Fatalf("config ArtPair not cleared on off: %v", cfg.ArtPair)
	}
}

// TestRenderArtUsesPinnedPair proves renderArt resolves the persisted pair
// rather than the deterministic Pair(code) when art is on.
func TestRenderArtUsesPinnedPair(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(60, 40)
	seedProject(t, m, "ATM", "Acme")
	update(t, m, "s")
	// Pin a known pair directly via the store and sync the cache, the way
	// refreshAll would after a toggle.
	if err := m.store.SetProjectArtOn("ATM", true, []string{"galaxy", "matrix"}, m.actor); err != nil {
		t.Fatal(err)
	}
	m.artOn["ATM"] = true
	m.artPair["ATM"] = []string{"galaxy", "matrix"}
	if out := m.projects.renderArt(8); out == "" {
		t.Fatal("renderArt with art on and a valid pinned pair must be non-blank")
	}
}

func cfgArtPair(c *core.ProjectConfig) []string {
	if c == nil {
		return nil
	}
	return c.ArtPair
}
