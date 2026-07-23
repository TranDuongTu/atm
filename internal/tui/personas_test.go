package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestPersonasOverlayListsAndViews(t *testing.T) {
	m := newTestModel(t)
	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("V")})
	if !m.personasOv.open {
		t.Fatal("V must open the personas overlay")
	}
	view := m.personasOv.renderOverlay()
	for _, want := range []string{"developer", "manager", "concierge", "admin"} {
		if !strings.Contains(view, want) {
			t.Errorf("overlay missing built-in %q:\n%s", want, view)
		}
	}

	// Move to a persona and open its prompt.
	m.personasOv.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if !m.personasOv.detail {
		t.Fatal("enter must open detail view")
	}
	detail := m.personasOv.renderOverlay()
	if !strings.Contains(detail, "Persona") {
		t.Errorf("detail must render the persona prompt:\n%s", detail)
	}

	// Esc: detail -> list -> closed.
	m.personasOv.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	if m.personasOv.detail || !m.personasOv.open {
		t.Fatal("first esc must return to list")
	}
	m.personasOv.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	if m.personasOv.open {
		t.Fatal("second esc must close the overlay")
	}
}

func TestPersonasOverlayIsReadOnly(t *testing.T) {
	m := newTestModel(t)
	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("V")})
	before := len(m.store.ListPersonas())
	for _, k := range []string{"e", "d", "a", "x", "p"} {
		m.personasOv.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)})
	}
	if got := len(m.store.ListPersonas()); got != before {
		t.Fatalf("personas changed %d -> %d; overlay must be read-only", before, got)
	}
}
