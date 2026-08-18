package tui

import (
	"os"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
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

// An error describes the query that produced it, so it must not outlive that
// query. Clearing sm.searchErr only on the next SUCCESS pinned the launcher on
// "search error: …" through every later query, level change and reopen — hiding
// good hits behind a failure the user had already typed past. Both of
// scheduleSearch's paths retire it: the one that schedules a tick, and the
// early return for a cleared query.
//
// Driven the same way TestSpotlightSearchSurfacesAStoreErrorAsARow drives it,
// and for the same reason: runSearch sends no vector, so no query can provoke a
// store error from the outside.
func TestSpotlightSearchStaleErrorDoesNotSurviveANewQuery(t *testing.T) {
	withInstantSpotSearch(t)
	m := newTestModel(t)
	m.SetSize(120, 40)
	seedProject(t, m, "ATM", "Acme")
	selectProject(t, m, "ATM")
	seedTask(t, m, "ATM", "wire the indexer")

	m.spotlight.openSpotlight()
	moveCursorToGroup(t, m, "Task")
	searchQuery(t, m, "ind") // a real search, with a real hit behind it
	m.spotlight.searchErr = "cache.db is unreadable"
	m.spotlight.buildRows()
	mustContain(t, strings.Join(rowLabels(m), "\n"), "cache.db is unreadable")

	// The scheduling path: the next keystroke retires the error with the query
	// it described, so the rebuild beneath it shows the new query's state and
	// the landed search shows its hits.
	cmd := typeQuery(t, m, "e") // "inde"
	if m.spotlight.searchErr != "" {
		t.Errorf("searchErr = %q after a new keystroke, want it retired with its query", m.spotlight.searchErr)
	}
	mustNotContain(t, strings.Join(rowLabels(m), "\n"), "search error")
	flushSpotSearch(t, m, cmd)
	if len(m.spotlight.hits) != 1 {
		t.Fatalf("hits = %d, want the matching task once the error is gone; rows = %v", len(m.spotlight.hits), rowLabels(m))
	}

	// The early-return path: clearing the query retires it too, so a launcher
	// peeled back and typed into afresh never starts on last time's failure.
	m.spotlight.searchErr = "cache.db is unreadable"
	m.spotlight.handleKey(tea.KeyMsg{Type: tea.KeyEsc}) // peels the query
	if m.spotlight.searchErr != "" {
		t.Errorf("searchErr = %q after the query was cleared, want it retired", m.spotlight.searchErr)
	}
	mustNotContain(t, strings.Join(rowLabels(m), "\n"), "search error")
}

// The section is what the spec asks for: one labelled block of content rows,
// with a comment hit indented under the task that owns it — including when the
// task itself did not match, which is the case that would otherwise orphan the
// comment at the top level.
func TestSpotlightSearchNestsCommentsUnderTheirTask(t *testing.T) {
	withInstantSpotSearch(t)
	m := newTestModel(t)
	m.SetSize(120, 40)
	seedProject(t, m, "ATM", "Acme")
	selectProject(t, m, "ATM")
	tk := seedTask(t, m, "ATM", "wire the indexer")
	if _, err := m.store.CreateComment(tk.ID, "the debounce interval is 150ms", nil, "", testActor); err != nil {
		t.Fatalf("CreateComment: %v", err)
	}
	m.refreshAll()

	m.spotlight.openSpotlight()
	moveCursorToGroup(t, m, "Task")
	searchQuery(t, m, "debounce") // matches the comment only

	rows := m.spotlight.rows
	if len(rows) != 4 {
		t.Fatalf("rows = %v, want Add task / section / task / comment", rowLabels(m))
	}
	if rows[1].kind != rowSection || rows[1].text != spotContentSection {
		t.Errorf("row 1 = %+v, want the %q header", rows[1], spotContentSection)
	}
	if rows[2].kind != rowTask || rows[2].task.ID != tk.ID {
		t.Errorf("row 2 = %q, want the parent task synthesized above its comment", rows[2].label())
	}
	if rows[3].kind != rowComment || rows[3].comment == nil || rows[3].task.ID != tk.ID {
		t.Errorf("row 3 = %+v, want the comment carrying its parent task", rows[3])
	}
	if !strings.Contains(rows[3].label(), "debounce") {
		t.Errorf("comment row label = %q, want its snippet", rows[3].label())
	}
}

// Criterion 6: the arrows traverse every section, and the section header is
// copy rather than a choice — the cursor steps over it.
func TestSpotlightSearchArrowsTraverseAllSections(t *testing.T) {
	withInstantSpotSearch(t)
	m := newTestModel(t)
	m.SetSize(120, 40)
	seedProject(t, m, "ATM", "Acme")
	selectProject(t, m, "ATM")
	tk := seedTask(t, m, "ATM", "wire the indexer")
	if _, err := m.store.CreateComment(tk.ID, "indexer notes", nil, "", testActor); err != nil {
		t.Fatalf("CreateComment: %v", err)
	}
	m.refreshAll()

	m.spotlight.openSpotlight()
	moveCursorToGroup(t, m, "Task")
	searchQuery(t, m, "indexer")

	// Homed on the top result, never on the header.
	if r := m.spotlight.selectedRow(); r == nil || r.kind != rowTask {
		t.Fatalf("selection = %q, want the top task row", m.spotlight.selectedLabel())
	}
	m.spotlight.handleKey(tea.KeyMsg{Type: tea.KeyDown})
	if r := m.spotlight.selectedRow(); r == nil || r.kind != rowComment {
		t.Fatalf("down = %q, want the comment row", m.spotlight.selectedLabel())
	}
	// Back up past the header and onto the ACTIONS entry above it.
	m.spotlight.handleKey(tea.KeyMsg{Type: tea.KeyUp})
	m.spotlight.handleKey(tea.KeyMsg{Type: tea.KeyUp})
	if got := m.spotlight.selectedLabel(); got != "Add task" {
		t.Errorf("up past the section = %q, want the Add task entry", got)
	}
}

// Enter on a comment row drills into its TASK's actions: a comment is how you
// found the task, not a thing with actions of its own.
func TestSpotlightSearchCommentRowDrillsIntoItsTask(t *testing.T) {
	withInstantSpotSearch(t)
	m := newTestModel(t)
	m.SetSize(120, 40)
	seedProject(t, m, "ATM", "Acme")
	selectProject(t, m, "ATM")
	tk := seedTask(t, m, "ATM", "wire the indexer")
	if _, err := m.store.CreateComment(tk.ID, "the debounce interval is 150ms", nil, "", testActor); err != nil {
		t.Fatalf("CreateComment: %v", err)
	}
	m.refreshAll()

	m.spotlight.openSpotlight()
	moveCursorToGroup(t, m, "Task")
	searchQuery(t, m, "debounce")
	moveCursorToComment(t, m)
	m.spotlight.handleKey(tea.KeyMsg{Type: tea.KeyEnter})

	if m.spotlight.level != levelTaskActions || m.spotlight.taskID != tk.ID {
		t.Errorf("level=%v taskID=%q, want the comment's task actions", m.spotlight.level, m.spotlight.taskID)
	}
}

// A brand-new task is findable before anything has embedded it: the text path
// reads live store entities, which is the whole reason this sub-task needs no
// ollama.
func TestSpotlightSearchFindsATaskCreatedSecondsAgo(t *testing.T) {
	withInstantSpotSearch(t)
	m := newTestModel(t)
	m.SetSize(120, 40)
	seedProject(t, m, "ATM", "Acme")
	selectProject(t, m, "ATM")

	m.spotlight.openSpotlight()
	moveCursorToGroup(t, m, "Task")
	fresh := seedTask(t, m, "ATM", "just created")
	searchQuery(t, m, "created")

	if len(m.spotlight.hits) != 1 || m.spotlight.hits[0].task.ID != fresh.ID {
		t.Errorf("hits = %v, want the unembedded task", rowLabels(m))
	}
}

// A pasted ID still finds its task — through the store now, not through a
// matcher of the launcher's own. This is the behavior Task 1 exists to keep.
func TestSpotlightSearchFindsATaskByPastedID(t *testing.T) {
	withInstantSpotSearch(t)
	m := newTestModel(t)
	m.SetSize(120, 40)
	seedProject(t, m, "ATM", "Acme")
	selectProject(t, m, "ATM")
	seedTask(t, m, "ATM", "placeholder one")
	target := seedTask(t, m, "ATM", "no keyword here")

	m.spotlight.openSpotlight()
	moveCursorToGroup(t, m, "Task")
	searchQuery(t, m, target.ID)

	if r := m.spotlight.selectedRow(); r == nil || r.kind != rowTask || r.task.ID != target.ID {
		t.Errorf("a pasted ID selected %q, want the named task", m.spotlight.selectedLabel())
	}
}

// The launcher owns no matcher of its own any more. A grep-style pin: the
// deleted symbols must not come back, because the moment they do the ACTIONS
// and content paths diverge again.
func TestSpotlightHasNoInMemoryContentMatcher(t *testing.T) {
	src, err := os.ReadFile("spotlight.go")
	if err != nil {
		t.Fatalf("read spotlight.go: %v", err)
	}
	for _, gone := range []string{"taskMatches", "spotTaskMatches"} {
		if strings.Contains(string(src), gone) {
			t.Errorf("%s is back: content rows must come from store.Search alone", gone)
		}
	}
}

// The Ask row's layout, pinned now so sub-task 4 inherits it rather than
// inventing it: one band across the bottom of the box, below both columns and
// above the footer, quoting the query it would ask about.
func TestSpotlightAskRowLayout(t *testing.T) {
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

	lines := strings.Split(view, "\n")
	ask, footer := -1, -1
	for i, l := range lines {
		if strings.Contains(l, "Ask ATM:") {
			ask = i
		}
		if strings.Contains(l, "[Enter] open") {
			footer = i
		}
	}
	if ask < 0 || footer < 0 {
		t.Fatalf("ask=%d footer=%d in:\n%s", ask, footer, view)
	}
	if ask >= footer {
		t.Errorf("the Ask row (line %d) must sit above the footer (line %d)", ask, footer)
	}
	// It spans the box rather than sitting in the left column.
	if got := lipgloss.Width(strings.TrimRight(lines[ask], " ")); got <= m.spotlight.leftPaneWidth() {
		t.Errorf("the Ask row is %d wide, want it spanning past the %d-column list",
			got, m.spotlight.leftPaneWidth())
	}
}

// An empty query has nothing to ask about, so the row does not render.
func TestSpotlightAskRowNeedsANonEmptyQuery(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	seedProject(t, m, "ATM", "Acme")
	selectProject(t, m, "ATM")

	m.spotlight.openSpotlight()
	moveCursorToGroup(t, m, "Task")

	mustNotContain(t, stripANSI(m.spotlight.renderOverlay()), "Ask ATM:")
}
