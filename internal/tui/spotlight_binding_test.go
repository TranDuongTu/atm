package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// TestBackslashOpensSpotlightAndQuestionMarkIsUnbound pins the binding swap:
// `\` is the only key that opens the spotlight; `?` is a plain, inert
// keypress everywhere in the workspace.
func TestBackslashOpensSpotlightAndQuestionMarkIsUnbound(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	seedProject(t, m, "ATM", "Acme")
	m.projectScope = "ATM"

	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("\\")})
	if !m.spotlight.open {
		t.Fatal("\\ must open the spotlight")
	}
	m.handleKey(tea.KeyMsg{Type: tea.KeyEsc})

	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})
	if m.spotlight.open {
		t.Error("? must be unbound")
	}
}

// TestStatusBarAdvertisesTheSpotlight pins the status bar's right cluster:
// [\]spotlight replaces the old [?]menu in both panes.
func TestStatusBarAdvertisesTheSpotlight(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	seedProject(t, m, "ATM", "Acme")
	m.projectScope = "ATM"
	for _, focused := range []workspacePane{paneProjects, paneTasks} {
		m.focused = focused
		line := m.renderStatusLine()
		if !strings.Contains(line, "[\\]spotlight") {
			t.Errorf("status line must advertise [\\]spotlight: %s", line)
		}
		if strings.Contains(line, "[?]menu") {
			t.Errorf("status line still advertises the old menu: %s", line)
		}
	}
}

// TestBackslashOpensSpotlightFromThePluginOverlay pins the plugin-overlay `\`
// path: the overlay must close first so the spotlight's replayed keys target
// the workspace instead of being swallowed by the still-open plugin overlay.
// Mirrors the plugin-overlay setup used elsewhere (fakePlugin + pluginOverlay
// = 0, see plugin_test.go / indexer_test.go) rather than a helper.
func TestBackslashOpensSpotlightFromThePluginOverlay(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	m.plugins = []plugin{&fakePlugin{}}
	m.pluginOverlay = 0

	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("\\")})
	if m.pluginOverlay != -1 {
		t.Error("the plugin overlay must close first so replayed keys reach the workspace")
	}
	if !m.spotlight.open {
		t.Error("\\ must open the spotlight from the plugin overlay")
	}
}
