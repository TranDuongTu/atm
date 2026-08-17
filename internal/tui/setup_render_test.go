package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// activateEntryByKey finds the menu entry for key, drills into its owning
// group if it has one, and activates it — the same tree-walk walkTo does for
// a label (spotlight_test.go), just keyed on the replay key instead: a
// registry-row invariant test only has the key to go on, not the label.
func activateEntryByKey(t *testing.T, m *Model, key string) {
	t.Helper()
	for i := range menuEntries {
		e := &menuEntries[i]
		if e.hidden || e.key != key {
			continue
		}
		walkTo(t, m, e.label)
		m.spotlight.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
		return
	}
	t.Fatalf("no menu entry with key %q", key)
}

func TestSetupRegistryRowSatisfiesInvariants(t *testing.T) {
	var found bool
	for _, e := range menuEntries {
		if e.key != "W" {
			continue
		}
		found = true
		if e.group != groupNone {
			t.Fatalf("the wizard is a root-level global, got group %v", e.group)
		}
		if lipgloss.Width(e.icon) != 1 {
			t.Fatalf("icon %q measures %d, must be 1", e.icon, lipgloss.Width(e.icon))
		}
		if e.needsProject {
			t.Fatal("the wizard is global; it must show with no project selected")
		}
	}
	if !found {
		t.Fatal("no W row in menuEntries")
	}
}

func TestActivateSetupLeavesItOpenAndSpotlightClosed(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	m.spotlight.openSpotlight()
	activateEntryByKey(t, m, "W")
	if !m.setup.active {
		t.Fatal("activation must leave the setup view open")
	}
	if m.spotlight.open {
		t.Fatal("the spotlight must not reopen over the setup view")
	}
}

func TestSetupRenderNarrowDropsColumnsRightToLeft(t *testing.T) {
	m := newTestModel(t)
	m.setup.open()
	wide := m.setup.render(120, 30)
	narrow := m.setup.render(46, 30)
	if !strings.Contains(wide, "MODEL") {
		t.Fatal("wide render should carry the MODEL column")
	}
	if strings.Contains(narrow, "MODEL") {
		t.Fatal("narrow render should have dropped MODEL")
	}
	if !strings.Contains(narrow, "claude") {
		t.Fatal("the agent name must never be dropped")
	}
}

func TestSetupRenderShowsEllipsisBeforeAsyncTierLands(t *testing.T) {
	m := newTestModel(t)
	m.setup.open() // async has not resolved
	if !strings.Contains(m.setup.render(120, 30), "…") {
		t.Fatal("pending async facts render as …, never as blank or as a guess")
	}
}
