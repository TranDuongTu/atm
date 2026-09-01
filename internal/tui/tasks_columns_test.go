package tui

import (
	"sort"
	"strings"
	"testing"
	"time"

	"atm/internal/capability"
	"atm/internal/core"
)

// columnHeader returns the ID/TITLE/ANNOTATE/UPDATED header row of the task
// list, which is the line naming the ID column.
func columnHeader(t *testing.T, m *Model) string {
	t.Helper()
	for _, ln := range strings.Split(stripANSI(m.tasks.View()), "\n") {
		if strings.Contains(ln, "ID") && strings.Contains(ln, "TITLE") {
			return ln
		}
	}
	t.Fatalf("no column header in the task list:\n%s", stripANSI(m.tasks.View()))
	return ""
}

func newColumnsTestModel(t *testing.T) *Model {
	t.Helper()
	m := newLanesTestModel(t)
	setupLanesProject(t, m, true)
	m.SetSize(120, 40)
	seedTask(t, m, "ATM", "older claimed", "ATM:scrum:task", "ATM:scrum-stage:building")
	seedTask(t, m, "ATM", "newer claimed", "ATM:scrum:bug", "ATM:scrum-stage:building")
	m.refreshAll()
	m.lanes.selectDefault()
	return m
}

func TestTaskListSortIndicatorSitsOnTheSortedColumn(t *testing.T) {
	m := newColumnsTestModel(t)

	if got := columnHeader(t, m); !strings.Contains(got, "UPDATED ↓") {
		t.Fatalf("default header = %q, want the UPDATED column marked ↓", got)
	}
	m.tasks.handleKey(keyMsg("s"))
	if got := columnHeader(t, m); !strings.Contains(got, "UPDATED ↑") {
		t.Fatalf("after one [s] header = %q, want UPDATED ↑", got)
	}
	m.tasks.handleKey(keyMsg("s"))
	got := columnHeader(t, m)
	if !strings.Contains(got, "ID ↑") {
		t.Fatalf("after two [s] header = %q, want the indicator on ID", got)
	}
	if strings.Contains(got, "UPDATED ↑") || strings.Contains(got, "UPDATED ↓") {
		t.Fatalf("the indicator stayed on UPDATED after the sort moved: %q", got)
	}
	m.tasks.handleKey(keyMsg("s"))
	if got := columnHeader(t, m); !strings.Contains(got, "TITLE ↑") {
		t.Fatalf("after three [s] header = %q, want the indicator on TITLE", got)
	}
	m.tasks.handleKey(keyMsg("s"))
	if got := columnHeader(t, m); !strings.Contains(got, "ANNOTATE ↑") {
		t.Fatalf("after four [s] header = %q, want the indicator on ANNOTATE", got)
	}
	m.tasks.handleKey(keyMsg("s"))
	if got := columnHeader(t, m); !strings.Contains(got, "UPDATED ↓") {
		t.Fatalf("the sort cycle did not return to updated-desc: %q", got)
	}
}

func TestTaskListHasTheFourFixedColumns(t *testing.T) {
	m := newColumnsTestModel(t)
	head := columnHeader(t, m)
	for _, col := range []string{"ID", "TITLE", "ANNOTATE", "UPDATED"} {
		if !strings.Contains(head, col) {
			t.Fatalf("column header %q missing %s", head, col)
		}
	}
}

func TestTaskListDropsTheOldCapabilityHeaderRow(t *testing.T) {
	m := newColumnsTestModel(t)
	v := stripANSI(m.tasks.View())
	for _, gone := range []string{"CAPABILITY:", "TOTAL:", "SORT:"} {
		if strings.Contains(v, gone) {
			t.Fatalf("task list still renders the old %q header row:\n%s", gone, v)
		}
	}
}

func TestPaneTitleCarriesTheCurrentCapability(t *testing.T) {
	m := newColumnsTestModel(t)
	if got := m.tasksPaneTitle(); !strings.Contains(got, "scrum") {
		t.Fatalf("pane title = %q, want the current capability named", got)
	}
}

func TestTaskListRendersDashForAnUnannotatedTask(t *testing.T) {
	m := newLanesTestModel(t)
	setupLanesProject(t, m, true)
	m.SetSize(120, 40)
	// Unclaimed work: scrum has nothing to say about it, so its Cell is nil.
	seedTask(t, m, "ATM", "undecided")
	m.refreshAll()
	m.lanes.selectDefault()
	m.lanes.move(-1) // Inbox

	var row string
	for _, ln := range strings.Split(stripANSI(m.tasks.View()), "\n") {
		if strings.Contains(ln, "undecided") {
			row = ln
			break
		}
	}
	if row == "" {
		t.Fatalf("the unclaimed task is not in the Inbox lane:\n%s", stripANSI(m.tasks.View()))
	}
	if !strings.Contains(row, "—") {
		t.Fatalf("row %q does not render the nil-annotation dash", row)
	}
}

// TestTaskListDefaultsToMostRecentlyUpdatedFirst pins the DEFAULT sort mode
// against applySort rather than against a seeded fixture: two tasks created
// in the same test land in the same persisted second, so a fixture could only
// prove the tie-break, not the ordering.
func TestTaskListDefaultsToMostRecentlyUpdatedFirst(t *testing.T) {
	m := newColumnsTestModel(t)
	in := sortFixtureRows()
	if got := m.tasks.applySort(in); got[0].id != "ATM-new" {
		t.Fatalf("first row = %s, want the most recently updated task first", got[0].id)
	}
	m.tasks.sortMode = sortTitleAsc
	if got := m.tasks.applySort(in); got[0].title != "newer" {
		t.Fatalf("title sort first row = %q, want %q", got[0].title, "newer")
	}
}

// sortFixtureRows is the shared comparator fixture: an older and a newer row,
// each with a cell, built the way refresh builds them.
func sortFixtureRows() []taskRow {
	base := time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC)
	older := &core.Task{ID: "ATM-old", Title: "older", UpdatedAt: base}
	newer := &core.Task{ID: "ATM-new", Title: "newer", UpdatedAt: base.Add(time.Hour)}
	return []taskRow{
		{id: older.ID, title: older.Title, cell: &capability.Cell{Text: "older cell"}, task: older},
		{id: newer.ID, title: newer.Title, cell: &capability.Cell{Text: "newer cell"}, task: newer},
	}
}

// Tasks written in the same second share a timestamp, so the UPDATED sorts
// must leave rows they cannot separate in the store's order.
func TestApplySortIsStableAcrossEqualTimestamps(t *testing.T) {
	m := newColumnsTestModel(t)
	same := time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC)
	var in []taskRow
	for _, id := range []string{"ATM-a", "ATM-b", "ATM-c", "ATM-d"} {
		in = append(in, taskRow{id: id, title: id, task: &core.Task{ID: id, Title: id, UpdatedAt: same}})
	}

	for _, mode := range []sortMode{sortUpdatedDesc, sortUpdatedAsc} {
		m.tasks.sortMode = mode
		for i, got := range m.tasks.applySort(in) {
			if got.id != in[i].id {
				t.Fatalf("%v: row[%d] = %s, want %s (equal timestamps must keep store order)", mode, i, got.id, in[i].id)
			}
		}
	}
}

// The seam the ANNOTATE sort needs: a cell travels with its row through the
// sort, so a rank comparator has something to read.
func TestApplySortMovesWholeRowsWithTheirCells(t *testing.T) {
	m := newColumnsTestModel(t)

	got := m.tasks.applySort(sortFixtureRows())

	if got[0].id != "ATM-new" {
		t.Fatalf("first row = %s, want the most recently updated row first", got[0].id)
	}
	if got[0].cell == nil || got[0].cell.Text != "newer cell" {
		t.Fatalf("the cell did not travel with its row: %+v", got[0].cell)
	}
}

// id-asc must order by the ID COLUMN its arrow points at. It used to be a
// no-op leaning on the store's own order, which was only ever id-asc under
// v1 (the alias was a zero-padded creation counter); a v2 alias is a content
// hash and ListTasks deliberately orders v2 projects by creation ordinal
// instead (internal/store/query.go), so the no-op sorted nothing at all.
// The input order is a real one: a v2 store hands back creation order, and
// these hashes were minted in exactly this sequence by an earlier run. Built
// as literal rows rather than seeded through the store so the fixture cannot
// arrive already sorted by hash luck and quietly prove nothing.
func TestTaskListIDSortOrdersByID(t *testing.T) {
	m := newColumnsTestModel(t)
	inCreationOrder := []string{"ATM-418130", "ATM-ac1b2e", "ATM-f9addb", "ATM-d8224e"}
	if sort.StringsAreSorted(inCreationOrder) {
		t.Fatalf("fixture %v is already ID-ordered; it cannot prove the comparator ran", inCreationOrder)
	}
	var in []taskRow
	for _, id := range inCreationOrder {
		in = append(in, annotateSortRow(id, nil, time.Time{}))
	}

	m.tasks.sortMode = sortIDAsc
	got := rowIDs(m.tasks.applySort(in))

	want := []string{"ATM-418130", "ATM-ac1b2e", "ATM-d8224e", "ATM-f9addb"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("id-asc row[%d] = %s, want %s (order: %v)", i, got[i], want[i], got)
		}
	}
}

// The mode table is the enforcement the two hand-coordinated switches never
// had: a mode that sorted by one column while pointing its arrow at another
// used to compile and pass everything.
func TestEverySortModeIsCompleteAndNamesARealColumn(t *testing.T) {
	columns := map[string]bool{"ID": true, "TITLE": true, "ANNOTATE": true, "UPDATED": true}
	for mode, spec := range sortSpecs {
		if spec.name == "" || spec.column == "" || spec.arrow == "" || spec.less == nil {
			t.Errorf("sortSpecs[%d] is incomplete: %+v", mode, spec)
			continue
		}
		if !columns[spec.column] {
			t.Errorf("sortSpecs[%d] (%s) sorts by %q, which is not a column the list renders", mode, spec.name, spec.column)
		}
		if spec.arrow != "↑" && spec.arrow != "↓" {
			t.Errorf("sortSpecs[%d] (%s) arrow = %q, want an up or down arrow", mode, spec.name, spec.arrow)
		}
	}
}

// annotateSortRow builds a row the way refresh does — cell attached, task
// carrying the timestamp the tie-break reads.
func annotateSortRow(id string, cell *capability.Cell, at time.Time) taskRow {
	return taskRow{id: id, title: id, cell: cell, task: &core.Task{ID: id, Title: id, UpdatedAt: at}}
}

// The ANNOTATE sort's three groups, in order: ranked cells by Rank ascending,
// then unranked cells (Rank 0), then rows the capability said nothing about.
func TestApplySortAnnotateOrdersRankedThenUnrankedThenNil(t *testing.T) {
	m := newColumnsTestModel(t)
	at := time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC)
	in := []taskRow{
		annotateSortRow("ATM-nil", nil, at),
		annotateSortRow("ATM-unranked", &capability.Cell{Text: "unranked"}, at),
		annotateSortRow("ATM-rank3", &capability.Cell{Text: "review", Rank: 3}, at),
		annotateSortRow("ATM-rank1", &capability.Cell{Text: "unreadable", Rank: 1}, at),
	}

	m.tasks.sortMode = sortAnnotate
	got := m.tasks.applySort(in)

	want := []string{"ATM-rank1", "ATM-rank3", "ATM-unranked", "ATM-nil"}
	for i, id := range want {
		if got[i].id != id {
			t.Fatalf("annotate sort row[%d] = %s, want %s (order: %v)", i, got[i].id, id, rowIDs(got))
		}
	}
}

// Within a group the annotate sort falls back to most-recently-updated first,
// so rows the rank cannot separate still read newest-first.
func TestApplySortAnnotateTieBreaksOnUpdatedDesc(t *testing.T) {
	m := newColumnsTestModel(t)
	old := time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC)
	recent := old.Add(time.Hour)
	in := []taskRow{
		annotateSortRow("ATM-ranked-old", &capability.Cell{Text: "review", Rank: 2}, old),
		annotateSortRow("ATM-ranked-new", &capability.Cell{Text: "review", Rank: 2}, recent),
		annotateSortRow("ATM-unranked-old", &capability.Cell{Text: "unranked"}, old),
		annotateSortRow("ATM-unranked-new", &capability.Cell{Text: "unranked"}, recent),
		annotateSortRow("ATM-nil-old", nil, old),
		annotateSortRow("ATM-nil-new", nil, recent),
	}

	m.tasks.sortMode = sortAnnotate
	got := m.tasks.applySort(in)

	want := []string{
		"ATM-ranked-new", "ATM-ranked-old",
		"ATM-unranked-new", "ATM-unranked-old",
		"ATM-nil-new", "ATM-nil-old",
	}
	for i, id := range want {
		if got[i].id != id {
			t.Fatalf("annotate tie-break row[%d] = %s, want %s (order: %v)", i, got[i].id, id, rowIDs(got))
		}
	}
}

// The [s] cycle must actually reach the new mode: a mode nothing can select
// is a mode that does not exist.
func TestTaskSortCycleReachesAnnotate(t *testing.T) {
	m := newColumnsTestModel(t)
	for i := 0; i < sortModeCount; i++ {
		if m.tasks.sortMode == sortAnnotate {
			if got := m.tasks.sortMode.String(); got != "annotate" {
				t.Fatalf("sortAnnotate.String() = %q, want %q", got, "annotate")
			}
			return
		}
		m.tasks.handleKey(keyMsg("s"))
	}
	t.Fatalf("[s] never reached sortAnnotate in %d presses (mode=%v)", sortModeCount, m.tasks.sortMode)
}

func rowIDs(rows []taskRow) []string {
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.id)
	}
	return out
}

func TestAnnotateColumnWidensToFitCells(t *testing.T) {
	m := newLanesTestModel(t)
	setupLanesProject(t, m, true)
	m.SetSize(120, 40)
	seedTask(t, m, "ATM", "widest cell", "ATM:scrum:task", "ATM:scrum-stage:brainstormed")
	m.refreshAll()
	m.lanes.selectDefault()

	out := stripANSI(m.tasks.View())
	if !strings.Contains(out, "task · brainstormed") {
		t.Fatalf("ANNOTATE cell truncated on a 120-wide pane:\n%s", out)
	}
}

func TestAnnotateColumnNeverStarvesTitle(t *testing.T) {
	m := newLanesTestModel(t)
	setupLanesProject(t, m, true)
	m.SetSize(60, 40) // just above metaColumnMinPaneWidth
	seedTask(t, m, "ATM", "title floor stays", "ATM:scrum:task", "ATM:scrum-stage:brainstormed")
	m.refreshAll()
	m.lanes.selectDefault()

	_, metaW, _, titleW := m.tasks.taskColumnWidths()
	if titleW < 16 {
		t.Fatalf("titleW = %d, want >= 16", titleW)
	}
	if metaW > 28 {
		t.Fatalf("metaW = %d, want <= 28", metaW)
	}
}

func TestAnnotateColumnClampProtectsTitleFloor(t *testing.T) {
	m := newLanesTestModel(t)
	setupLanesProject(t, m, true)
	m.SetSize(60, 40) // just above metaColumnMinPaneWidth
	m.refreshAll()
	m.lanes.selectDefault()

	// Construct a row with a very wide cell (23 runes) to force the clamp.
	// At width 60: max = 60 - 9 - 9 - 4 - 16 = 22
	// A 23-rune cell should trigger the clamp, limiting metaW to 22 and titleW to 16.
	longCellRow := taskRow{
		id:    "ATM-clamped",
		title: "should not be starved",
		cell:  &capability.Cell{Text: "very long annotation text here"},
		task:  &core.Task{ID: "ATM-clamped", Title: "should not be starved"},
	}
	m.tasks.rows = []taskRow{longCellRow}

	idW, metaW, updatedW, titleW := m.tasks.taskColumnWidths()
	if titleW < 16 {
		t.Fatalf("titleW = %d, want = 16 when clamp triggers", titleW)
	}
	if metaW > 22 {
		t.Fatalf("metaW = %d, want <= 22 (clamped to protect title)", metaW)
	}

	// Verify the row still fits: idW + metaW + updatedW + 4 + titleW <= pane width
	totalW := idW + metaW + updatedW + 4 + titleW
	if totalW > 60 {
		t.Fatalf("rendered line width = %d, exceeds pane width 60 (idW=%d, metaW=%d, updatedW=%d, titleW=%d)",
			totalW, idW, metaW, updatedW, titleW)
	}
}
