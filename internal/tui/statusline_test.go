package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestQuestionMarkOpensMenuAndStatusBarIsClean(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	seedProject(t, m, "ATM", "Acme")
	m.projectScope = "ATM"

	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})
	if !m.menu.open {
		t.Fatal("? must open the menu")
	}
	m.menu.handleKey(tea.KeyMsg{Type: tea.KeyEsc})

	for _, focused := range []workspacePane{paneProjects, paneTasks} {
		m.focused = focused
		line := m.renderStatusLine()
		if !strings.Contains(line, "[?]menu") {
			t.Errorf("status line must advertise [?]menu: %s", line)
		}
		for _, stale := range []string{"[C]conv", "[T]theme", "[?]help", "[a]dd", "[e]title", "Ctrl+Shift"} {
			if strings.Contains(line, stale) {
				t.Errorf("status line still advertises %q: %s", stale, line)
			}
		}
	}
}

func TestConventionsKeyRemoved(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	// No project scope, projects pane: C used to open conventions help.
	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("C")})
	if m.menu.open {
		t.Error("C must not open any overlay outside a project-scoped tasks context")
	}
	if m.capability.open {
		t.Error("C without project scope must not open the capabilities switcher")
	}
}
