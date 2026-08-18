package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// Tab is the entry key. Sub-task 1 pinned the Ask row and deliberately left it
// hintless, because tab focused the preview pane and advertising it would have
// documented a binding that did something else (spotlight_render.go:489).
func TestSpotlightTabEntersAskMode(t *testing.T) {
	withInstantSpotSearch(t)
	m := newTestModel(t)
	m.SetSize(120, 40)
	seedProject(t, m, "ATM", "Acme")
	selectProject(t, m, "ATM")
	seedTask(t, m, "ATM", "wire the indexer")

	m.spotlight.openSpotlight()
	moveCursorToGroup(t, m, "Task")
	searchQuery(t, m, "indexer")
	m.spotlight.handleKey(tea.KeyMsg{Type: tea.KeyTab})

	if m.spotlight.level != levelAsk {
		t.Fatalf("level = %v, want levelAsk", m.spotlight.level)
	}
	if m.spotlight.ask == nil || m.spotlight.ask.question != "indexer" {
		t.Errorf("the query must arrive as the question, got %+v", m.spotlight.ask)
	}
}

// An empty query has nothing to ask about, and tab no longer means preview.
func TestSpotlightTabWithEmptyQueryDoesNothing(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	seedProject(t, m, "ATM", "Acme")
	selectProject(t, m, "ATM")

	m.spotlight.openSpotlight()
	m.spotlight.handleKey(tea.KeyMsg{Type: tea.KeyTab})

	if m.spotlight.level == levelAsk {
		t.Error("tab must not enter ask mode with nothing to ask")
	}
	if m.spotlight.focus == focusPreview {
		t.Error("tab no longer focuses the preview -- right-arrow does")
	}
}

// Preview focus moves to the arrow that points at the pane it focuses.
func TestSpotlightRightArrowFocusesPreview(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	seedProject(t, m, "ATM", "Acme")
	selectProject(t, m, "ATM")

	m.spotlight.openSpotlight()
	m.spotlight.handleKey(tea.KeyMsg{Type: tea.KeyRight})
	if m.spotlight.focus != focusPreview {
		t.Fatal("right-arrow must focus the preview")
	}
	m.spotlight.handleKey(tea.KeyMsg{Type: tea.KeyLeft})
	if m.spotlight.focus != focusList {
		t.Error("left-arrow must return focus to the list")
	}
}

// Peel restores the list exactly: the query is back, and so are its rows.
func TestAskEscPeelRestoresTheListExactly(t *testing.T) {
	withInstantSpotSearch(t)
	m := newTestModel(t)
	m.SetSize(120, 40)
	seedProject(t, m, "ATM", "Acme")
	selectProject(t, m, "ATM")
	seedTask(t, m, "ATM", "wire the indexer")

	m.spotlight.openSpotlight()
	moveCursorToGroup(t, m, "Task")
	searchQuery(t, m, "indexer")
	before := stripANSI(m.spotlight.renderOverlay())

	m.spotlight.handleKey(tea.KeyMsg{Type: tea.KeyTab})
	flushSpotSearch(t, m, m.spotlight.handleKey(tea.KeyMsg{Type: tea.KeyEsc}))

	if m.spotlight.level == levelAsk {
		t.Fatal("esc must peel out of the ask level")
	}
	if m.spotlight.query != "indexer" {
		t.Errorf("query = %q, want it restored to \"indexer\"", m.spotlight.query)
	}
	if got := stripANSI(m.spotlight.renderOverlay()); got != before {
		t.Errorf("peel must restore the list view exactly.\n--- before ---\n%s\n--- after ---\n%s", before, got)
	}
}

// The row now advertises its key, and the footer names the rebinding.
func TestSpotlightAskRowAdvertisesTab(t *testing.T) {
	withInstantSpotSearch(t)
	m := newTestModel(t)
	m.SetSize(120, 40)
	seedProject(t, m, "ATM", "Acme")
	selectProject(t, m, "ATM")
	seedTask(t, m, "ATM", "wire the indexer")

	m.spotlight.openSpotlight()
	moveCursorToGroup(t, m, "Task")
	searchQuery(t, m, "indexer")

	view := stripANSI(m.spotlight.renderOverlay())
	mustContain(t, view, `Ask ATM: "indexer"`)
	mustContain(t, view, "[Tab] ask")
	mustContain(t, view, "[→] preview")
}

// The gate is gone. A missing chat model is communicated by degraded mode with
// a hint naming the fix -- not by hiding the row, which would leave the user
// with nothing to discover the fix from.
func TestAskRowNeedsOnlyANonEmptyQuery(t *testing.T) {
	withInstantSpotSearch(t)
	m := newTestModel(t)
	m.SetSize(120, 40)
	seedProject(t, m, "ATM", "Acme")
	selectProject(t, m, "ATM")
	seedTask(t, m, "ATM", "wire the indexer")

	m.spotlight.openSpotlight()
	moveCursorToGroup(t, m, "Task")
	mustNotContain(t, stripANSI(m.spotlight.renderOverlay()), "Ask ATM:")
	searchQuery(t, m, "indexer")
	mustContain(t, stripANSI(m.spotlight.renderOverlay()), "Ask ATM:")
}
