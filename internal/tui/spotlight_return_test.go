package tui

import (
	"testing"

	"atm/internal/capability/qa"
	"atm/internal/capability/scrum"

	tea "github.com/charmbracelet/bubbletea"
)

// Esc from a spotlight-spawned form returns to the launcher — to the level the
// entry was activated from, not to the root. "Add project" lives one level down
// inside the Project group, which is exactly the case the old bare-int return
// could not describe: an int is a root row index, so the launcher used to
// reopen at the root with a cursor that meant something else there.
func TestEscFromSpotlightSpawnedFormReopensSpotlight(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	m.spotlight.openSpotlight()
	walkTo(t, m, "Add project")
	row := m.spotlight.cursor
	m.spotlight.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if m.form == nil {
		t.Fatal("Enter must open the project-create form")
	}

	m.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	if !m.spotlight.open {
		t.Fatal("Esc from a spotlight-spawned form must reopen the spotlight")
	}
	if m.spotlight.level != levelGroup || m.spotlight.group != groupProject {
		t.Errorf("the launcher must reopen where it was left: level=%v group=%v",
			m.spotlight.level, m.spotlight.group)
	}
	if m.spotlight.cursor != row {
		t.Errorf("cursor must be restored to %d, got %d", row, m.spotlight.cursor)
	}
	if got := m.spotlight.selectedLabel(); got != "Add project" {
		t.Errorf("the restored cursor must be back on the activated row, got %q", got)
	}
	if m.spotlightReturn != nil {
		t.Errorf("the return must be cleared once used, got %+v", *m.spotlightReturn)
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
	m.spotlight.handleKey(tea.KeyMsg{Type: tea.KeyEnter})

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
	if m.spotlightReturn != nil {
		t.Errorf("submit must clear the return, got %+v", *m.spotlightReturn)
	}
}

// TestConfirmAcceptLandsOnTheWorkspace drives a spotlight-spawned confirm
// (Remove project) to acceptance: like a successful submit, it must clear
// spotlightReturn and land on the workspace rather than reopening the
// spotlight.
func TestConfirmAcceptLandsOnTheWorkspace(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	seedProject(t, m, "ATM", "Acme")
	m.spotlight.openSpotlight()
	walkTo(t, m, "Remove project")
	m.spotlight.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if m.confirm != confirmRemoveProject {
		t.Fatalf("Enter must open the remove-project confirm, confirm=%v", m.confirm)
	}

	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})

	if m.spotlight.open {
		t.Error("a completed confirm must land on the workspace, not the spotlight")
	}
	if m.spotlightReturn != nil {
		t.Errorf("confirm-accept must clear the return, got %+v", *m.spotlightReturn)
	}
}

func TestEscFromSpotlightSpawnedOverlayReopensSpotlight(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	seedChannels(t, m)
	m.spotlight.openSpotlight()
	walkTo(t, m, "Channels")
	m.spotlight.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if !m.channelsOv.open {
		t.Fatal("Enter must open the channels overlay")
	}
	m.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	if !m.spotlight.open {
		t.Error("Esc from a spotlight-spawned overlay must reopen the spotlight")
	}
}

// Regression for finding #4: dispatchModel.submit's success path set
// d.active = false but never cleared spotlightReturn, so the wrapper's
// "workspace idle again" check fires the moment the dialog closes and
// reopens the spotlight over the dispatch toast instead of landing on the
// workspace (spec decision 5).
func TestSuccessfulDispatchFromSpotlightLandsOnTheWorkspace(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	seedProject(t, m, "ATM", "Acme")
	m.projectScope = "ATM"
	m.dispatcher = &fakeDispatcher{preview: "tmux · new window"}
	m.agentOptionsFn = testAgents

	m.spotlight.openSpotlight()
	walkTo(t, m, "Dispatch a session")
	m.spotlight.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if !m.dispatchDlg.active {
		t.Fatal("Enter must open the dispatch dialog")
	}

	m.handleKey(tea.KeyMsg{Type: tea.KeyEnter}) // preselected agent is ready: submits directly

	if m.dispatchDlg.active {
		t.Fatal("a successful dispatch must close the dialog")
	}
	if m.spotlight.open {
		t.Error("a successful dispatch must land on the workspace, not the spotlight")
	}
	if m.spotlightReturn != nil {
		t.Errorf("a successful dispatch must clear the return, got %+v", *m.spotlightReturn)
	}
}

// Sibling of the dispatch regression above: capabilityModel.switchTo's
// success path set c.open = false but never cleared spotlightReturn.
func TestSuccessfulCapabilitySwitchFromSpotlightLandsOnTheWorkspace(t *testing.T) {
	m := newTestModelWithCaps(t, scrum.New(), qa.New())
	m.SetSize(120, 40)
	seedProject(t, m, "ATM", "Acme")
	m.projectScope = "ATM"
	m.focused = paneTasks

	m.spotlight.openSpotlight()
	walkTo(t, m, "Capabilities")
	m.spotlight.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if !m.capability.open {
		t.Fatal("Enter must open the capabilities switcher")
	}

	m.handleKey(tea.KeyMsg{Type: tea.KeyEnter}) // switch to the row under the cursor

	if m.capability.open {
		t.Fatal("a successful switch must close the capabilities switcher")
	}
	if m.spotlight.open {
		t.Error("a successful capability switch must land on the workspace, not the spotlight")
	}
	if m.spotlightReturn != nil {
		t.Errorf("a successful capability switch must clear the return, got %+v", *m.spotlightReturn)
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

// The deepest return there is: a per-task action, two levels down and targeting
// one task by ID. Dismissing its form must put the user back on that task's
// action list — same task, same row — and not at the root, which is all a row
// index could ever describe.
func TestSpotlightReturnRestoresTaskDrill(t *testing.T) {
	withInstantSpotSearch(t)
	m := newTestModel(t)
	m.SetSize(120, 40)
	seedProject(t, m, "ATM", "Acme")
	selectProject(t, m, "ATM")
	target := seedTask(t, m, "ATM", "wire the indexer")
	seedTask(t, m, "ATM", "decoy task")

	m.spotlight.openSpotlight()
	moveCursorToGroup(t, m, "Task")
	searchQuery(t, m, "indexer")
	moveCursorToTask(t, m, target.ID)
	m.spotlight.handleKey(tea.KeyMsg{Type: tea.KeyEnter}) // drill into the task's actions
	moveCursorToLabel(t, m, "Add comment")
	row := m.spotlight.cursor
	m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})

	wantFormKind(t, m, formCommentAdd)
	if m.spotlight.open {
		t.Fatal("activating a kindDialog task action must close the launcher")
	}

	m.handleKey(tea.KeyMsg{Type: tea.KeyEsc})

	if !m.spotlight.open {
		t.Fatal("Esc from a task action's form must reopen the spotlight")
	}
	if m.spotlight.level != levelTaskActions || m.spotlight.taskID != target.ID {
		t.Fatalf("the launcher must reopen on the same task's actions: level=%v taskID=%q",
			m.spotlight.level, m.spotlight.taskID)
	}
	if m.spotlight.taskTitle != target.Title {
		t.Errorf("taskTitle = %q, want %q", m.spotlight.taskTitle, target.Title)
	}
	if m.spotlight.cursor != row || m.spotlight.selectedLabel() != "Add comment" {
		t.Errorf("cursor = %d (%q), want %d (Add comment)",
			m.spotlight.cursor, m.spotlight.selectedLabel(), row)
	}
	if m.spotlightReturn != nil {
		t.Errorf("the return must be cleared once used, got %+v", *m.spotlightReturn)
	}
}

// The dialog the launcher spawned can outlive what the launcher was standing
// on: a task removed while its comment form is open leaves an action list with
// nothing to act on. The return falls back one level, to the Task group — and
// keeps the search, which is the one thing a fallback inherits: the query that
// found the vanished task describes exactly the list the Task group shows, so
// the user lands back on their (now shorter) results rather than on an empty
// group they must retype into after a step they never took.
func TestSpotlightReturnFallsBackWhenTheTaskIsGone(t *testing.T) {
	withInstantSpotSearch(t)
	m := newTestModel(t)
	m.SetSize(120, 40)
	seedProject(t, m, "ATM", "Acme")
	selectProject(t, m, "ATM")
	target := seedTask(t, m, "ATM", "wire the indexer")
	survivor := seedTask(t, m, "ATM", "indexer plumbing")

	m.spotlight.openSpotlight()
	moveCursorToGroup(t, m, "Task")
	searchQuery(t, m, "indexer")
	moveCursorToTask(t, m, target.ID)
	m.spotlight.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	moveCursorToLabel(t, m, "Add comment")
	m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	wantFormKind(t, m, formCommentAdd)

	if err := m.store.RemoveTask(target.ID, testActor); err != nil {
		t.Fatalf("RemoveTask: %v", err)
	}
	// The fallback's re-run of the inherited search is debounced like any
	// other, so it is landed before the rows are read.
	flushSpotSearch(t, m, m.handleKey(tea.KeyMsg{Type: tea.KeyEsc}))

	if !m.spotlight.open {
		t.Fatal("a stale return must still reopen the spotlight")
	}
	if m.spotlight.level != levelGroup || m.spotlight.group != groupTask {
		t.Fatalf("a gone task must fall back to the Task group: level=%v group=%v",
			m.spotlight.level, m.spotlight.group)
	}
	if m.spotlight.taskID != "" || m.spotlight.taskTitle != "" {
		t.Errorf("the vanished target must be dropped: id=%q title=%q",
			m.spotlight.taskID, m.spotlight.taskTitle)
	}
	if m.spotlight.query != "indexer" {
		t.Errorf("query after the fallback = %q, want the search that found the task", m.spotlight.query)
	}
	if got := rowLabels(m); !equalStrings(got, []string{"Add task", spotContentSection, survivor.Title}) {
		t.Errorf("rows after the fallback = %v, want the search re-run against the surviving tasks", got)
	}
	if r := m.spotlight.selectedRow(); r == nil || r.kind != rowTask || r.task.ID != survivor.ID {
		t.Errorf("the fallback must land on a surviving result, got %q", m.spotlight.selectedLabel())
	}
}

// The spec's "Esc returns to the results with the query intact" must survive a
// dialog in the middle of it, which is the whole four-step sequence: search a
// task, drill in, run a kindDialog action, Esc the form (the launcher returns
// to the action list), Esc again (the launcher owes the user their results).
// The second Esc reads sm.taskQuery, so the snapshot has to carry it — leaving
// it to whatever happens to still be on the closed launcher would make a spec
// behavior depend on an accident.
func TestSpotlightReturnKeepsTheTaskQueryAcrossADialog(t *testing.T) {
	withInstantSpotSearch(t)
	m := newTestModel(t)
	m.SetSize(120, 40)
	seedProject(t, m, "ATM", "Acme")
	selectProject(t, m, "ATM")
	target := seedTask(t, m, "ATM", "alpha one")
	seedTask(t, m, "ATM", "alpha two")
	seedTask(t, m, "ATM", "beta three")

	m.spotlight.openSpotlight()
	moveCursorToGroup(t, m, "Task")
	searchQuery(t, m, "alpha")
	before := rowLabels(m)
	moveCursorToTask(t, m, target.ID)
	m.spotlight.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	moveCursorToLabel(t, m, "Add comment")
	m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	wantFormKind(t, m, formCommentAdd)

	m.handleKey(tea.KeyMsg{Type: tea.KeyEsc}) // dismiss the form: back to the action list
	if m.spotlight.level != levelTaskActions {
		t.Fatalf("setup: the return must land on the action list, level=%v", m.spotlight.level)
	}
	// Peel the action level; the restored search lands through its tick.
	flushSpotSearch(t, m, m.spotlight.handleKey(tea.KeyMsg{Type: tea.KeyEsc}))

	if m.spotlight.query != "alpha" {
		t.Errorf("query after Esc = %q, want the search intact", m.spotlight.query)
	}
	if got := rowLabels(m); !equalStrings(got, before) {
		t.Errorf("rows after Esc =\n%v\nwant the results the user drilled from\n%v", got, before)
	}
	if r := m.spotlight.selectedRow(); r == nil || r.kind != rowTask {
		t.Errorf("the restored results must select a task, got %q", m.spotlight.selectedLabel())
	}
}

// The search is carried by the snapshot, not by residual state: openAt restores
// a launcher that knows nothing about the drill it is being put back into.
func TestSpotlightOpenAtRestoresTheTaskQueryFromTheSnapshot(t *testing.T) {
	withInstantSpotSearch(t)
	m := newTestModel(t)
	m.SetSize(120, 40)
	seedProject(t, m, "ATM", "Acme")
	selectProject(t, m, "ATM")
	target := seedTask(t, m, "ATM", "alpha one")
	seedTask(t, m, "ATM", "beta three")

	m.spotlight.openAt(spotlightSnapshot{
		level: levelTaskActions, group: groupTask,
		taskID: target.ID, taskTitle: target.Title, taskQuery: "alpha",
	})
	if m.spotlight.level != levelTaskActions || m.spotlight.taskID != target.ID {
		t.Fatalf("setup: level=%v taskID=%q", m.spotlight.level, m.spotlight.taskID)
	}
	if m.spotlight.taskQuery != "alpha" {
		t.Errorf("taskQuery = %q, want the snapshot's search", m.spotlight.taskQuery)
	}

	// The Esc restores the snapshot's search, which lands through its tick like
	// any other.
	flushSpotSearch(t, m, m.spotlight.handleKey(tea.KeyMsg{Type: tea.KeyEsc}))
	if m.spotlight.query != "alpha" {
		t.Errorf("query after Esc = %q, want the snapshot's search restored", m.spotlight.query)
	}
	if r := m.spotlight.selectedRow(); r == nil || r.kind != rowTask || r.task.ID != target.ID {
		t.Errorf("Esc must land on the results the snapshot's search produces, got %q",
			m.spotlight.selectedLabel())
	}
}

// A snapshot is read back against a world that may have moved on. An
// unrecoverable one lands at the root rather than panicking or restoring a
// level that cannot be built.
func TestSpotlightOpenAtUnrecoverableSnapshot(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)

	m.spotlight.openAt(spotlightSnapshot{
		level: levelGroup, group: menuGroupID(99), query: "proj", cursor: 42,
	})

	if !m.spotlight.open {
		t.Fatal("openAt must open the launcher even for a stale snapshot")
	}
	if m.spotlight.level != levelRoot || m.spotlight.group != groupNone {
		t.Errorf("an unknown group must fall back to the root: level=%v group=%v",
			m.spotlight.level, m.spotlight.group)
	}
	if m.spotlight.query != "" {
		t.Errorf("a fallback level never inherits the snapshot's query, got %q", m.spotlight.query)
	}
	if r := m.spotlight.selectedRow(); r == nil {
		t.Errorf("the fallback must leave a landable cursor, cursor=%d rows=%v",
			m.spotlight.cursor, rowLabels(m))
	}
}

// The restored cursor obeys the -1 no-selection sentinel: a query that matched
// something when the dialog opened can match nothing when it closes, leaving
// one hint row the cursor may not land on.
func TestSpotlightOpenAtWithNothingSelectable(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)

	m.spotlight.openAt(spotlightSnapshot{level: levelRoot, query: "zzzzz", cursor: 4})

	if got := rowLabels(m); !equalStrings(got, []string{"no matches"}) {
		t.Fatalf("rows = %v, want the no-matches hint", got)
	}
	if m.spotlight.cursor != -1 {
		t.Errorf("cursor = %d, want the -1 no-selection sentinel", m.spotlight.cursor)
	}
	if r := m.spotlight.selectedRow(); r != nil {
		t.Errorf("nothing may be selected, got %q", r.label())
	}
}
