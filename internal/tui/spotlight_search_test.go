package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// withInstantSpotSearch collapses the debounce for the duration of one test.
// tea.Tick's Cmd sleeps for its interval when invoked, so a test that drives
// the real 150ms per query pays it per keystroke; the package has no
// t.Parallel() tests, which is what makes that safe.
func withInstantSpotSearch(t *testing.T) {
	t.Helper()
	prev := spotSearchDebounce
	spotSearchDebounce = 0
	t.Cleanup(func() { spotSearchDebounce = prev })
}

// flushSpotSearch runs the debounced search a keystroke scheduled and delivers
// its tick through the real Update path — the same route production takes.
func flushSpotSearch(t *testing.T, m *Model, cmd tea.Cmd) {
	t.Helper()
	if cmd == nil {
		t.Fatal("the keystroke scheduled no search")
	}
	msg := cmd()
	tick, ok := msg.(spotSearchTickMsg)
	if !ok {
		t.Fatalf("scheduled msg = %T, want spotSearchTickMsg", msg)
	}
	m.Update(tick)
}

// The store is queried once, after the typing stops — not once per keystroke.
// The intervening ticks are stale and must be dropped, which is what the
// generation counter is for.
func TestSpotlightSearchDebouncesToTheLastKeystroke(t *testing.T) {
	withInstantSpotSearch(t)
	m := newTestModel(t)
	m.SetSize(120, 40)
	seedProject(t, m, "ATM", "Acme")
	selectProject(t, m, "ATM")
	seedTask(t, m, "ATM", "wire the indexer")

	m.spotlight.openSpotlight()
	moveCursorToGroup(t, m, "Task")

	// One bump per keystroke. Measured as a delta, not a total: the launcher
	// also retires searches on every level transition (openSpotlight and the
	// drill into Task each bump the counter), so a hardcoded total would pin
	// how many transitions this test's setup happens to perform rather than
	// the invariant it cares about.
	genBefore := m.spotlight.searchGen
	var cmds []tea.Cmd
	for _, r := range "ind" {
		cmds = append(cmds, m.spotlight.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}))
	}
	if got := m.spotlight.searchGen - genBefore; got != 3 {
		t.Fatalf("searchGen advanced by %d over 3 keystrokes, want one bump per keystroke", got)
	}
	// The first two ticks belong to superseded queries: landing them must not
	// publish results for a query the user has already typed past.
	for _, c := range cmds[:2] {
		m.Update(c())
		if m.spotlight.searchQuery != "" {
			t.Fatalf("a stale tick published %q", m.spotlight.searchQuery)
		}
	}
	flushSpotSearch(t, m, cmds[2])
	if m.spotlight.searchQuery != "ind" {
		t.Errorf("searchQuery = %q, want the last keystroke's query", m.spotlight.searchQuery)
	}
	if len(m.spotlight.hits) != 1 {
		t.Fatalf("hits = %d, want the one matching task", len(m.spotlight.hits))
	}
}

// The debounce is the spec's ~150ms, and it is what the launcher actually
// schedules — a zero interval would make every other test here pass while
// production hammered the store per keystroke.
func TestSpotlightSearchDebounceInterval(t *testing.T) {
	if spotSearchDebounce < 100*time.Millisecond || spotSearchDebounce > 250*time.Millisecond {
		t.Errorf("spotSearchDebounce = %v, want the spec's ~150ms", spotSearchDebounce)
	}
}

// Content search is the Task group's alone: the registry levels filter menu
// entries in memory and must never reach the store.
func TestSpotlightSearchOnlyTheTaskGroupQueriesTheStore(t *testing.T) {
	withInstantSpotSearch(t)
	m := newTestModel(t)
	m.SetSize(120, 40)
	seedProject(t, m, "ATM", "Acme")
	selectProject(t, m, "ATM")
	seedTask(t, m, "ATM", "wire the indexer")

	m.spotlight.openSpotlight() // levelRoot: registry search
	if cmd := m.spotlight.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}}); cmd != nil {
		t.Error("a registry-level query must schedule no store search")
	}
	if !m.spotlight.filtering() {
		t.Error("setup: the root level still filters menu entries in memory")
	}
}

// No project scope, no store query: the group has nothing to search and says
// so, exactly as it did before the store took over.
func TestSpotlightSearchIsInertWithoutAScope(t *testing.T) {
	withInstantSpotSearch(t)
	m := newTestModel(t)
	m.SetSize(120, 40)
	seedProject(t, m, "ATM", "Acme")
	seedTask(t, m, "ATM", "wire the indexer")
	m.projectScope = ""

	m.spotlight.openSpotlight()
	moveCursorToGroup(t, m, "Task")
	if cmd := m.spotlight.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}}); cmd != nil {
		t.Error("an unscoped launcher must schedule no store search")
	}
	if len(m.spotlight.hits) != 0 {
		t.Errorf("hits = %d, want none without a scope", len(m.spotlight.hits))
	}
}

// Leaving the level invalidates whatever was in flight: a tick that lands
// after the user has drilled elsewhere must not publish into the new level.
func TestSpotlightSearchLevelChangeDropsAnInFlightTick(t *testing.T) {
	withInstantSpotSearch(t)
	m := newTestModel(t)
	m.SetSize(120, 40)
	seedProject(t, m, "ATM", "Acme")
	selectProject(t, m, "ATM")
	seedTask(t, m, "ATM", "wire the indexer")

	m.spotlight.openSpotlight()
	moveCursorToGroup(t, m, "Task")
	cmd := m.spotlight.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
	m.spotlight.setLevel(levelRoot, groupNone)

	m.Update(cmd())
	if m.spotlight.searchQuery != "" || len(m.spotlight.hits) != 0 {
		t.Errorf("a tick outliving its level published searchQuery=%q hits=%d",
			m.spotlight.searchQuery, len(m.spotlight.hits))
	}
}

// A store error is reported, never rendered as "no results" — the launcher
// inherits the store's never-swallow rule. runSearch sends no vector, so no
// query can provoke a store error from the outside; the error surface is
// therefore driven directly, and what this pins is that the row builder shows
// it rather than the reassuring "no tasks match".
//
// The write comes AFTER the level transition on purpose: setLevel rebuilds, so
// an error set before it would be describing a search that had not run.
func TestSpotlightSearchSurfacesAStoreErrorAsARow(t *testing.T) {
	t.Skip("appendContentRows lands in Task 3")
	withInstantSpotSearch(t)
	m := newTestModel(t)
	m.SetSize(120, 40)
	seedProject(t, m, "ATM", "Acme")
	selectProject(t, m, "ATM")

	m.spotlight.openSpotlight()
	moveCursorToGroup(t, m, "Task")
	searchQuery(t, m, "indexer") // lands a real search first
	m.spotlight.searchErr = "cache.db is unreadable"
	m.spotlight.buildRows()

	labels := rowLabels(m)
	for _, l := range labels {
		if strings.Contains(l, "no tasks match") {
			t.Fatalf("a store error rendered as an empty result: %v", labels)
		}
		if strings.Contains(l, "cache.db is unreadable") {
			return
		}
	}
	t.Errorf("rows = %v, want the store error reported in one of them", labels)
}
