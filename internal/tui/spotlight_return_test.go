package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestEscFromSpotlightSpawnedFormReopensSpotlight(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	m.spotlight.openSpotlight()
	walkTo(t, m, "Add project")
	row := m.spotlight.cursor
	m.spotlight.handleKey(tea.KeyMsg{Type: tea.KeyRight})
	if m.form == nil {
		t.Fatal("-> must open the project-create form")
	}

	m.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	if !m.spotlight.open {
		t.Fatal("Esc from a spotlight-spawned form must reopen the spotlight")
	}
	if m.spotlight.cursor != row {
		t.Errorf("cursor must be restored to %d, got %d", row, m.spotlight.cursor)
	}
	if m.spotlightReturn != -1 {
		t.Errorf("the return must be cleared once used, got %d", m.spotlightReturn)
	}
}

// TestSuccessfulSubmitLandsOnTheWorkspace drives the project-create form (code,
// name fields) to submission. With only two fields, Enter on the last field
// (cursor == len(Fields)-1) validates and submits directly — it never passes
// through the buttons zone, so a single Enter is the real submit key, not the
// Tab-into-buttons-then-Enter shape the brief guessed.
func TestSuccessfulSubmitLandsOnTheWorkspace(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	m.spotlight.openSpotlight()
	walkTo(t, m, "Add project")
	m.spotlight.handleKey(tea.KeyMsg{Type: tea.KeyRight})

	for _, r := range "ACME" {
		m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	m.handleKey(tea.KeyMsg{Type: tea.KeyTab})
	for _, r := range "Acme" {
		m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	m.handleKey(tea.KeyMsg{Type: tea.KeyEnter}) // last field: validates and submits directly

	if m.spotlight.open {
		t.Error("a successful submit must land on the workspace, not the spotlight")
	}
	if m.spotlightReturn != -1 {
		t.Errorf("submit must clear the return, got %d", m.spotlightReturn)
	}
}

func TestEscFromSpotlightSpawnedOverlayReopensSpotlight(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	seedChannels(t, m)
	m.spotlight.openSpotlight()
	walkTo(t, m, "Channels")
	m.spotlight.handleKey(tea.KeyMsg{Type: tea.KeyRight})
	if !m.channelsOv.open {
		t.Fatal("-> must open the channels overlay")
	}
	m.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	if !m.spotlight.open {
		t.Error("Esc from a spotlight-spawned overlay must reopen the spotlight")
	}
}

// A key pressed with no pending return must never resurrect the spotlight.
func TestNoReturnWithoutSpotlightActivation(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("E")})
	m.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	if m.spotlight.open {
		t.Error("a directly-opened overlay must not reopen the spotlight on Esc")
	}
}
