package tui

import (
	"testing"

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