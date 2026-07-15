package tui

import (
	"strings"
	"testing"
)

// These tests cover Task 9b: the board-authoring keys (n/e/S/d/l) wired into
// the merged Tasks pane. They drive the keys through the full Model.Update
// path with the Tasks pane focused, so they exercise the live dispatch, not
// the (now production-unreachable) boardsModel.handleKey handlers.

// scopeTasksPane seeds and selects a project, then focuses the Tasks pane so
// the authoring keys route through tasksModel.handleListKey.
func scopeTasksPane(t *testing.T, m *Model, code string) {
	t.Helper()
	update(t, m, "s") // select the project under the Projects cursor
	update(t, m, "2") // focus the Tasks pane
}

func TestTasksPaneNKeyOpensNewBoardForm(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	scopeTasksPane(t, m, "ATM")

	update(t, m, "n")

	if m.form == nil || m.formKind != formBoardEditor {
		t.Fatalf("[n] must open the board editor; form=%v kind=%v", m.form, m.formKind)
	}
	// A NEW board starts empty.
	if m.boardEd == nil || m.boardEd.Name != "" {
		t.Fatalf("[n] must target a NEW (empty) board; boardEd=%v", m.boardEd)
	}
	if got := m.form.Fields[0].Value; got != "" {
		t.Errorf("new board name field = %q, want empty", got)
	}
}

// TestTasksPaneEKeyEditsSelectedBoard is the trap proof: cycleBoard resets
// b.cursor to 0, so the SELECTED board is b.rows[b.ringIndex()], NOT
// b.rows[b.cursor]. With the selected board off ring index 0, [e] must edit
// the selected board — not whatever board sits at index 0.
func TestTasksPaneEKeyEditsSelectedBoard(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	// Two real boards (labels with an Expr). Sorted by Name the ring is
	// [alpha-board, comment, context, omega-board, open-tasks, priority,
	// status]; alpha-board is index 0, omega-board is not.
	if err := m.store.LabelAdd("ATM:alpha-board", "a", "status:open", testActor); err != nil {
		t.Fatalf("LabelAdd alpha: %v", err)
	}
	if err := m.store.LabelAdd("ATM:omega-board", "z", "status:done", testActor); err != nil {
		t.Fatalf("LabelAdd omega: %v", err)
	}
	scopeTasksPane(t, m, "ATM")

	// Cycle the ring until omega-board is SELECTED (each cycle resets cursor=0,
	// the exact trap condition).
	for i := 0; m.boards.selected != "ATM:omega-board"; i++ {
		if i > len(m.boards.rows) {
			t.Fatalf("omega-board never became selected; rows=%v", m.boards.rowNames())
		}
		m.boards.cycleBoard(1)
	}
	if m.boards.rows[0].Name == "omega-board" {
		t.Fatalf("test setup broken: omega-board must not be at ring index 0")
	}

	update(t, m, "e")

	if m.form == nil || m.formKind != formBoardEditor {
		t.Fatalf("[e] must open the board editor; form=%v kind=%v", m.form, m.formKind)
	}
	if m.boardEd == nil || m.boardEd.Name != "omega-board" {
		t.Fatalf("[e] must edit the SELECTED board (omega-board), got boardEd=%v", m.boardEd)
	}
	if m.boardEd.Name == m.boards.rows[0].Name {
		t.Fatalf("[e] edited the index-0 board (%q), not the selected one", m.boards.rows[0].Name)
	}
}

// TestTasksPaneDKeyAtChartTargetsCursorMember proves chart-level [d] targets
// the {/}-moved chart cursor member, not the first chart row.
func TestTasksPaneDKeyAtChartTargetsCursorMember(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	scopeTasksPane(t, m, "ATM")

	// Select the status namespace board and drill into its chart.
	for i := 0; m.boards.selected != "ATM:status:*"; i++ {
		if i > len(m.boards.rows) {
			t.Fatalf("status namespace never became selected; rows=%v", m.boards.rowNames())
		}
		m.boards.cycleBoard(1)
	}
	update(t, m, ">") // drill into the namespace chart
	if m.boards.level != lLevelChart {
		t.Fatalf("expected chart level after drill-in, got %v", m.boards.level)
	}

	rows := m.boards.chartRows()
	if len(rows) < 2 {
		t.Fatalf("need >=2 chart rows to prove cursor targeting, got %d", len(rows))
	}
	wantSuffix := strings.TrimPrefix(rows[1].full, "ATM:")
	otherSuffix := strings.TrimPrefix(rows[0].full, "ATM:")

	update(t, m, "}") // move the chart cursor to row index 1
	update(t, m, "d") // describe the cursor member

	if m.form == nil || m.formKind != formLabelDescribe {
		t.Fatalf("[d] at chart must open the describe form; form=%v kind=%v", m.form, m.formKind)
	}
	if got := m.form.Fields[0].Value; got != wantSuffix {
		t.Fatalf("[d] described %q, want the chart-cursor member %q (not %q)", got, wantSuffix, otherSuffix)
	}
}

func TestTasksPaneSKeySeedsDefaults(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	scopeTasksPane(t, m, "ATM")

	update(t, m, "S")

	if !strings.Contains(m.toastMsg, "seeded") || !strings.Contains(m.toastMsg, "ATM") {
		t.Fatalf("[S] must seed default labels (toast); got toast=%q", m.toastMsg)
	}
}
