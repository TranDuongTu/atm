package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestMenuListsScopedActions(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	seedProject(t, m, "ATM", "Acme")
	m.projectScope = "ATM"
	m.focused = paneTasks

	m.menu.openMenu()
	if !m.menu.open {
		t.Fatal("openMenu must open")
	}
	view := m.menu.renderOverlay()
	for _, want := range []string{"Add task", "Channels", "Capabilities", "Keymap reference", "Conventions", "[D]", "[E]"} {
		if !strings.Contains(view, want) {
			t.Errorf("menu missing %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "Add project") {
		t.Errorf("projects-scope action leaked into tasks-focused menu:\n%s", view)
	}
	if strings.Contains(view, "Quit") {
		t.Errorf("hidden entries must not render as menu rows:\n%s", view)
	}
}

func TestMenuHidesProjectGatedViewsWithoutProject(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	m.menu.openMenu()
	if view := m.menu.renderOverlay(); strings.Contains(view, "Capabilities") {
		t.Errorf("needsProject entry shown without a project scope:\n%s", view)
	}
}

func TestMenuActivationReplaysKey(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	seedChannels(t, m)
	m.menu.openMenu()
	// Walk the cursor to the Channels entry, then activate with Enter.
	for i := 0; i < 50 && m.menu.selectedLabel() != "Channels"; i++ {
		m.menu.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	}
	if m.menu.selectedLabel() != "Channels" {
		t.Fatal("could not reach the Channels entry")
	}
	m.menu.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if m.menu.open {
		t.Error("activation must close the menu")
	}
	if !m.channelsOv.open {
		t.Error("activating Channels must open the channels overlay (replay through handleKey)")
	}
}

func TestMenuReferenceDrillAndBack(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	m.menu.openMenu()
	for i := 0; i < 50 && m.menu.selectedLabel() != "Conventions"; i++ {
		m.menu.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	}
	m.menu.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if !m.menu.open || m.menu.view != refConventions {
		t.Fatal("reference entry must open its detail view in-menu")
	}
	if view := m.menu.renderOverlay(); !strings.Contains(view, "What ATM is") {
		t.Errorf("conventions detail missing content:\n%s", view)
	}
	m.menu.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	if !m.menu.open || m.menu.view != refNone {
		t.Error("esc from detail must return to the menu list, not close")
	}
	m.menu.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	if m.menu.open {
		t.Error("esc from list must close the menu")
	}
}
