package tui

import (
	"fmt"
	"strings"
	"testing"

	"atm/internal/capability/contextmap"
	"atm/internal/capability/workflow"
)

// Every keyed entry's replay string must round-trip through bubbletea: the
// KeyMsg we synthesize for it must .String() back to the same string the
// real key produces in handleKey. This is the no-phantom-bindings guard
// (the old status bar advertised [Ctrl+Shift+→]dispatch, which was never a
// real binding).
func TestMenuEntriesReplayRoundTrip(t *testing.T) {
	for _, e := range menuEntries {
		if e.key == "" || e.hidden { // hidden entries are display-only rows in the reference table
			continue
		}
		if got := keyMsgFromString(e.key).String(); got != e.key {
			t.Errorf("entry %q: keyMsgFromString(%q).String() = %q", e.label, e.key, got)
		}
	}
}

// Hidden entries never carry a section other than Actions-invisible
// documentation, and reference entries never carry a key.
func TestMenuEntriesShape(t *testing.T) {
	for _, e := range menuEntries {
		if e.ref != refNone && e.key != "" {
			t.Errorf("reference entry %q must not carry a key", e.label)
		}
		if e.section == sectionViews && len(e.scopes) != 0 {
			t.Errorf("views entry %q must not be scope-filtered (use needsProject)", e.label)
		}
	}
}

// Every non-hidden keyed entry must carry a summary (the preview fallback)
// and a kind (which decides what -> does). A missing summary would render an
// empty preview region; a missing kind would default kindAction and fire a
// dialog entry without recording the return.
func TestMenuEntriesCarrySummaryAndKind(t *testing.T) {
	for _, e := range menuEntries {
		if e.hidden || e.key == "" {
			continue
		}
		if strings.TrimSpace(e.summary) == "" {
			t.Errorf("entry %q (key %q) has no summary", e.label, e.key)
		}
		if e.kind != kindAction && e.kind != kindDialog {
			t.Errorf("entry %q (key %q) has kind %v, want kindAction or kindDialog", e.label, e.key, e.kind)
		}
	}
	for _, e := range menuEntries {
		if e.section == sectionReference && e.kind != kindReference {
			t.Errorf("reference entry %q must be kindReference", e.label)
		}
	}
}

// Every prelude segment must round-trip through bubbletea exactly like the
// entry keys do, and every action scope must have a non-empty prelude — an
// empty one would replay the entry's key into whatever pane happens to be
// focused, which is the contextual-menu bug this redesign removes.
func TestPreludesRoundTripAndCoverEveryScope(t *testing.T) {
	scopes := []menuScope{scopeProjectsList, scopeProjectsDetail, scopeProjectsDrill, scopeTasksList, scopeTasksDetail, scopeBoards}
	for _, s := range scopes {
		chain := preludeFor(s)
		if len(chain) == 0 {
			t.Errorf("scope %d has an empty prelude", s)
		}
		for _, seg := range chain {
			if got := keyMsgFromString(seg).String(); got != seg {
				t.Errorf("prelude segment %q round-trips to %q", seg, got)
			}
		}
		if sectionTitleFor(s) == "" {
			t.Errorf("scope %d has no section title", s)
		}
	}
	if len(preludeFor(scopeGlobal)) != 0 {
		t.Error("global entries must have an empty prelude")
	}
}

// The pane-focus and chart-drill keys are advertised on the pane and chart
// borders already; they stay in the keymap reference but must never be
// spotlight rows.
func TestBorderHintedKeysAreHidden(t *testing.T) {
	for _, e := range menuEntries {
		switch e.key {
		case "1", "2", "ctrl+right", "ctrl+left":
			if !e.hidden {
				t.Errorf("border-hinted key %q (%s) must be hidden", e.key, e.label)
			}
		}
	}
}

// A global list renders every entry exactly once; the old table carried
// "Remove project" twice (once per projects scope).
func TestNoDuplicateVisibleEntries(t *testing.T) {
	seen := map[string]bool{}
	for _, e := range menuEntries {
		if e.hidden || e.key == "" {
			continue
		}
		id := e.key + "|" + e.label
		if seen[id] {
			t.Errorf("duplicate visible entry %s", id)
		}
		seen[id] = true
	}
}

func TestKeymapReferenceTextCoversAllEntries(t *testing.T) {
	ref := keymapReferenceText()
	for _, e := range menuEntries {
		if e.key == "" {
			continue
		}
		if !strings.Contains(ref, e.key) {
			t.Errorf("keymap reference missing key %q (%s)", e.key, e.label)
		}
	}
}

// parityProbe sets up a Model in a menu entry's scope so that replaying the
// entry's key through Model.handleKey produces an observable effect, then
// asserts that effect. setup must reach a state where the entry applies; a
// probe whose setup cannot reach such a state (or whose key leaves the model
// unchanged) is a test failure, not a skip — that is how a phantom binding
// (a key the table advertises but no handler consumes) is caught.
type parityProbe struct {
	model func(t *testing.T) *Model // optional factory; defaults to newTestModel
	setup func(t *testing.T, m *Model)
	check func(t *testing.T, m *Model)
}

// probeID names the parity probe for an action entry by its key and primary
// scope. The same key means different things in different scopes (a/x/s/S in
// the projects and tasks panes, e/d in task detail vs. the boards ring), so
// the scope disambiguates.
func probeID(key string, scope menuScope) string {
	return fmt.Sprintf("%s|%d", key, scope)
}

// scopeTasksPane (tasks_boards_authoring_test.go) selects the seeded project
// and focuses the Tasks pane, so the entry's key routes through
// tasksModel.handleListKey (the boards keys route to boardsModel from there).

// parityTasksScope is the tasks-pane setup used by the tasks-list and boards
// parity probes: seed the project so the select key has a row to pick, then
// scopeTasksPane (which also ensures the workflow vocabulary and picks the
// all-tasks board).
func parityTasksScope(t *testing.T, m *Model) {
	t.Helper()
	seedProject(t, m, "ATM", "Acme")
	scopeTasksPane(t, m, "ATM")
}

// parityTaskDetail drills the tasks pane into a seeded task's detail: the
// context every task-detail entry (e/d/b/B/M/H/x) applies in.
func parityTaskDetail(t *testing.T, m *Model) {
	t.Helper()
	parityTasksScope(t, m)
	seedTask(t, m, "ATM", "parity task")
	update(t, m, "enter")
	if m.tasks.view != tViewDetail {
		t.Fatalf("enter must open task detail, view=%v", m.tasks.view)
	}
}

// parityBoardsChart drills the tasks pane into the status namespace's chart,
// where the boards describe/remove keys (d/l) apply at chart level.
func parityBoardsChart(t *testing.T, m *Model) {
	t.Helper()
	parityTasksScope(t, m)
	for i := 0; m.boards.selected != "ATM:status:*"; i++ {
		if i > len(m.boards.rows) {
			t.Fatalf("status namespace never became selected; rows=%v", m.boards.rowNames())
		}
		m.boards.cycleBoard(1)
	}
	update(t, m, "shift+right")
	if m.boards.level != lLevelChart {
		t.Fatalf("drill-in must reach the chart, level=%v", m.boards.level)
	}
}

// TestMenuEntriesConsumedByHandlers is the spec's parity test (decision 9):
// every keyed, menu-visible entry's replay sequence is consumed by the
// corresponding handler in its scope and produces the entry's advertised
// effect. It supplements TestMenuEntriesReplayRoundTrip, which only proves the
// key string round-trips through bubbletea — a phantom binding (like the
// historical [Ctrl+Shift+→]dispatch, which was advertised but never handled)
// round-trips fine but leaves the model unchanged, which this test rejects.
func TestMenuEntriesConsumedByHandlers(t *testing.T) {
	probes := map[string]parityProbe{
		// Views — global overlays and panes (no scope filtering).
		"D|views": {
			setup: func(t *testing.T, m *Model) {
				m.dispatcher = &fakeDispatcher{preview: "tmux · new window"}
				m.agentOptionsFn = testAgents
			},
			check: func(t *testing.T, m *Model) {
				if !m.dispatchDlg.active {
					t.Error("D must open the dispatch dialog")
				}
			},
		},
		"E|views": {
			check: func(t *testing.T, m *Model) {
				if !m.channelsOv.open {
					t.Error("E must open the channels overlay")
				}
			},
		},
		"V|views": {
			check: func(t *testing.T, m *Model) {
				if !m.personasOv.open {
					t.Error("V must open the personas overlay")
				}
			},
		},
		"C|views": {
			setup: func(t *testing.T, m *Model) {
				parityTasksScope(t, m)
			},
			check: func(t *testing.T, m *Model) {
				if !m.capability.open {
					t.Error("C in a project-scoped tasks pane must open the capabilities switcher")
				}
			},
		},
		"T|views": {
			setup: func(t *testing.T, m *Model) {
				m.themeName = themeGraphite
			},
			check: func(t *testing.T, m *Model) {
				if m.themeName == themeGraphite {
					t.Error("T must cycle the theme")
				}
			},
		},
		"1|views": {
			setup: func(t *testing.T, m *Model) {
				m.focused = paneTasks
			},
			check: func(t *testing.T, m *Model) {
				if m.focused != paneProjects {
					t.Errorf("1 must focus the Projects pane, focused=%v", m.focused)
				}
			},
		},
		"2|views": {
			setup: func(t *testing.T, m *Model) {
				m.focused = paneProjects
			},
			check: func(t *testing.T, m *Model) {
				if m.focused != paneTasks {
					t.Errorf("2 must focus the Tasks pane, focused=%v", m.focused)
				}
			},
		},

		// Actions — projects list.
		probeID("a", scopeProjectsList): {
			check: func(t *testing.T, m *Model) {
				if m.form == nil || m.formKind != formProjectCreate {
					t.Errorf("a on the projects list must open the project-create form, formKind=%v", m.formKind)
				}
			},
		},
		probeID("s", scopeProjectsList): {
			setup: func(t *testing.T, m *Model) {
				seedProject(t, m, "ATM", "Acme")
			},
			check: func(t *testing.T, m *Model) {
				if m.projectScope != "ATM" {
					t.Errorf("s must select the highlighted project, projectScope=%q", m.projectScope)
				}
			},
		},
		probeID("x", scopeProjectsList): {
			setup: func(t *testing.T, m *Model) {
				seedProject(t, m, "ATM", "Acme")
			},
			check: func(t *testing.T, m *Model) {
				if m.confirm != confirmRemoveProject {
					t.Errorf("x on the projects list must open the remove-project confirm, confirm=%v", m.confirm)
				}
			},
		},
		probeID("ctrl+right", scopeProjectsList): {
			setup: func(t *testing.T, m *Model) {
				seedProject(t, m, "ATM", "Acme")
				seedTask(t, m, "ATM", "activity for the persona chart")
				m.projectScope = "ATM"
				m.focused = paneProjects
			},
			check: func(t *testing.T, m *Model) {
				if !m.projects.personaDrilled {
					t.Error("ctrl+right on the projects list must drill into the persona detail")
				}
			},
		},

		// Actions — projects detail.
		probeID("n", scopeProjectsDetail): {
			setup: func(t *testing.T, m *Model) {
				seedProject(t, m, "ATM", "Acme")
				m.projects.openDetail("ATM")
			},
			check: func(t *testing.T, m *Model) {
				if m.form == nil || m.formKind != formProjectSetName {
					t.Errorf("n in project detail must open the set-name form, formKind=%v", m.formKind)
				}
			},
		},
		probeID("H", scopeProjectsDetail): {
			setup: func(t *testing.T, m *Model) {
				seedProject(t, m, "ATM", "Acme")
				m.projects.openDetail("ATM")
			},
			check: func(t *testing.T, m *Model) {
				if !m.projects.detail.historyOn {
					t.Error("H in project detail must toggle the history section on")
				}
			},
		},
		probeID("c", scopeProjectsDetail): {
			model: func(t *testing.T) *Model {
				// Two capabilities so the capability-switcher cursor has
				// somewhere to move (a one-capability registry cycles back to 0).
				return newTestModelWithCaps(t, workflow.New(), contextmap.New())
			},
			setup: func(t *testing.T, m *Model) {
				seedProject(t, m, "ATM", "Acme")
				m.projects.openDetail("ATM")
			},
			check: func(t *testing.T, m *Model) {
				if m.projects.capCursor != 1 {
					t.Errorf("c in project detail must cycle the capability cursor, capCursor=%d want 1", m.projects.capCursor)
				}
			},
		},
		probeID("x", scopeProjectsDetail): {
			setup: func(t *testing.T, m *Model) {
				seedProject(t, m, "ATM", "Acme")
				m.projects.openDetail("ATM")
			},
			check: func(t *testing.T, m *Model) {
				if m.confirm != confirmRemoveProject {
					t.Errorf("x in project detail must open the remove-project confirm, confirm=%v", m.confirm)
				}
			},
		},

		// Actions — projects persona drill.
		probeID("d", scopeProjectsDrill): {
			setup: func(t *testing.T, m *Model) {
				seedProject(t, m, "ATM", "Acme")
				seedTask(t, m, "ATM", "activity for the persona chart")
				m.projectScope = "ATM"
				m.focused = paneProjects
				m.dispatcher = &fakeDispatcher{preview: "tmux · new window"}
				m.agentOptionsFn = testAgents
				update(t, m, "ctrl+right")
				if !m.projects.personaDrilled {
					t.Fatal("precondition: ctrl+right must drill into the persona detail")
				}
			},
			check: func(t *testing.T, m *Model) {
				if !m.dispatchDlg.active {
					t.Error("d in the persona drill must open the dispatch dialog")
				}
			},
		},
		probeID("ctrl+left", scopeProjectsDrill): {
			setup: func(t *testing.T, m *Model) {
				seedProject(t, m, "ATM", "Acme")
				seedTask(t, m, "ATM", "activity for the persona chart")
				m.projectScope = "ATM"
				m.focused = paneProjects
				update(t, m, "ctrl+right")
				if !m.projects.personaDrilled {
					t.Fatal("precondition: ctrl+right must drill into the persona detail")
				}
			},
			check: func(t *testing.T, m *Model) {
				if m.projects.personaDrilled {
					t.Error("ctrl+left in the persona drill must back out to the list")
				}
			},
		},

		// Actions — tasks list.
		probeID("a", scopeTasksList): {
			setup: func(t *testing.T, m *Model) {
				parityTasksScope(t, m)
			},
			check: func(t *testing.T, m *Model) {
				if m.form == nil || m.formKind != formTaskCreate {
					t.Errorf("a on the tasks list must open the task-create form, formKind=%v", m.formKind)
				}
			},
		},
		probeID("s", scopeTasksList): {
			setup: func(t *testing.T, m *Model) {
				parityTasksScope(t, m)
			},
			check: func(t *testing.T, m *Model) {
				if m.tasks.sortMode != 1 {
					t.Errorf("s on the tasks list must cycle the sort, sortMode=%d want 1", m.tasks.sortMode)
				}
			},
		},
		probeID("S", scopeTasksList): {
			setup: func(t *testing.T, m *Model) {
				parityTasksScope(t, m)
			},
			check: func(t *testing.T, m *Model) {
				if !strings.Contains(m.toastMsg, "ensured capability vocabulary in ATM") {
					t.Errorf("S on the tasks list must re-ensure the capability vocabulary (toast), toast=%q", m.toastMsg)
				}
			},
		},
		probeID("p", scopeTasksList): {
			setup: func(t *testing.T, m *Model) {
				parityTasksScope(t, m)
			},
			check: func(t *testing.T, m *Model) {
				if len(m.boards.pins) != 1 || m.boards.pins[0] != "ATM:all-tasks" {
					t.Errorf("p on the tasks list must pin the selected board, pins=%v want [ATM:all-tasks]", m.boards.pins)
				}
			},
		},

		// Actions — task detail.
		probeID("e", scopeTasksDetail): {
			setup: parityTaskDetail,
			check: func(t *testing.T, m *Model) {
				if m.form == nil || m.formKind != formTaskSetTitle {
					t.Errorf("e in task detail must open the title form, formKind=%v", m.formKind)
				}
			},
		},
		probeID("d", scopeTasksDetail): {
			setup: parityTaskDetail,
			check: func(t *testing.T, m *Model) {
				if m.form == nil || m.formKind != formTaskSetDescription {
					t.Errorf("d in task detail must open the description form, formKind=%v", m.formKind)
				}
			},
		},
		probeID("b", scopeTasksDetail): {
			setup: parityTaskDetail,
			check: func(t *testing.T, m *Model) {
				if m.form == nil || m.formKind != formTaskLabelAdd {
					t.Errorf("b in task detail must open the add-label form, formKind=%v", m.formKind)
				}
			},
		},
		probeID("B", scopeTasksDetail): {
			setup: parityTaskDetail,
			check: func(t *testing.T, m *Model) {
				if m.form == nil || m.formKind != formTaskLabelRemove {
					t.Errorf("B in task detail must open the remove-label form, formKind=%v", m.formKind)
				}
			},
		},
		probeID("M", scopeTasksDetail): {
			setup: parityTaskDetail,
			check: func(t *testing.T, m *Model) {
				if m.form == nil || m.formKind != formCommentAdd {
					t.Errorf("M in task detail must open the add-comment form, formKind=%v", m.formKind)
				}
			},
		},
		probeID("H", scopeTasksDetail): {
			setup: parityTaskDetail,
			check: func(t *testing.T, m *Model) {
				if !m.tasks.historyOverlay.active {
					t.Error("H in task detail must open the history overlay")
				}
			},
		},
		probeID("x", scopeTasksDetail): {
			setup: parityTaskDetail,
			check: func(t *testing.T, m *Model) {
				if m.confirm != confirmRemoveTask {
					t.Errorf("x in task detail must open the remove-task confirm, confirm=%v", m.confirm)
				}
			},
		},

		// Actions — boards ring (routed from the tasks pane).
		probeID("n", scopeBoards): {
			setup: func(t *testing.T, m *Model) {
				parityTasksScope(t, m)
			},
			check: func(t *testing.T, m *Model) {
				if m.form == nil || m.formKind != formBoardEditor {
					t.Errorf("n on the boards ring must open the board editor, formKind=%v", m.formKind)
				}
			},
		},
		probeID("e", scopeBoards): {
			setup: func(t *testing.T, m *Model) {
				parityTasksScope(t, m)
			},
			check: func(t *testing.T, m *Model) {
				if m.form == nil || m.formKind != formBoardEditor {
					t.Errorf("e on the boards ring must open the board editor, formKind=%v", m.formKind)
				}
				if m.boardEd == nil || m.boardEd.Name != "all-tasks" {
					t.Errorf("e on the boards ring must edit the selected board all-tasks, boardEd=%v", m.boardEd)
				}
			},
		},
		probeID("d", scopeBoards): {
			setup: parityBoardsChart,
			check: func(t *testing.T, m *Model) {
				if m.form == nil || m.formKind != formLabelDescribe {
					t.Errorf("d at chart level must open the describe-label form, formKind=%v", m.formKind)
				}
			},
		},
		probeID("l", scopeBoards): {
			setup: parityBoardsChart,
			check: func(t *testing.T, m *Model) {
				if m.form == nil || m.formKind != formLabelRemove {
					t.Errorf("l at chart level must open the remove-label form, formKind=%v", m.formKind)
				}
			},
		},
		probeID("S", scopeBoards): {
			setup: func(t *testing.T, m *Model) {
				parityTasksScope(t, m)
			},
			check: func(t *testing.T, m *Model) {
				if !strings.Contains(m.toastMsg, "ensured capability vocabulary in ATM") {
					t.Errorf("S on the boards ring must seed the capability vocabulary (toast), toast=%q", m.toastMsg)
				}
			},
		},
	}

	for _, e := range menuEntries {
		if e.key == "" || e.hidden { // reference rows and keymap-only navigation pairs are not menu entries
			continue
		}
		var id string
		if e.section == sectionViews {
			id = e.key + "|views"
		} else if len(e.scopes) > 0 {
			id = probeID(e.key, e.scopes[0])
		} else {
			t.Errorf("entry %q carries a key but no scope", e.label)
			continue
		}
		p, ok := probes[id]
		if !ok {
			// A keyed entry with no handler reachable in any scope is exactly
			// the phantom-binding bug this test exists to catch.
			t.Errorf("no parity probe for menu entry %q (key %q, id %s) — is it bound anywhere?", e.label, e.key, id)
			continue
		}
		e := e
		t.Run(id, func(t *testing.T) {
			m := newTestModel(t)
			if p.model != nil {
				m = p.model(t)
			}
			m.SetSize(120, 40)
			if p.setup != nil {
				p.setup(t, m)
			}
			m.handleKey(keyMsgFromString(e.key))
			p.check(t, m)
		})
	}
}
