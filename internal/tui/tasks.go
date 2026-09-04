package tui

import (
	"sort"
	"strings"

	"atm/internal/capability"
	"atm/internal/core"
	"github.com/charmbracelet/bubbletea"
)

type tasksModel struct {
	m             *Model
	width         int
	contentHeight int

	// list state (flat + grouped)
	rows     []taskRow
	cursor   int
	offset   int
	pageSize int

	// filter / sort / focus
	filter   string
	sortMode sortMode
	grouped  bool
	focus    taskFocus

	// annReg is the capability registry annotate renders cells from,
	// resolved ONCE at the top of refresh — regFor runs a GetProject
	// freshness probe, and per-row resolution made refresh O(rows) probes
	// (ATM-4c476c). Nil when no scope is set.
	annReg *capability.Registry

	// drillStack holds modal navigation state above the task list. Task 1 only
	// opens details; later tasks add comment, description, and thread levels.
	drillStack []drillLevel
}

type drillKind int

// The drill kinds, in the order a reader meets them: the details page, then
// the levels it can open over itself.
const (
	drillDetail drillKind = iota
	drillDescription
	drillThread
	drillComment
)

type drillLevel struct {
	kind   drillKind
	id     string
	offset int
	cursor int
}

type sortMode int

const (
	sortUpdatedDesc sortMode = iota
	sortUpdatedAsc
	sortIDAsc
	sortTitleAsc
	sortAnnotate
)

// sortSpec is one entry of the [s] cycle: the name the mode reports, the
// column its arrow hangs off, the arrow, and the comparator that orders the
// rows. Order and indicator sit in ONE row because they used to sit in two
// switches that nothing forced to agree — a mode could sort by one column
// while pointing its arrow at another and still compile.
type sortSpec struct {
	name   string
	column string
	arrow  string
	less   func(a, b taskRow) bool
}

// sortSpecs is the [s] cycle itself, indexed by sortMode and in cycle order.
// Its length IS the cycle length, so a mode added here is reachable by the
// key without a second edit to keep in step.
var sortSpecs = [...]sortSpec{
	sortUpdatedDesc: {
		name: "updated-desc", column: "UPDATED", arrow: "↓",
		less: func(a, b taskRow) bool { return a.task.UpdatedAt.After(b.task.UpdatedAt) },
	},
	sortUpdatedAsc: {
		name: "updated-asc", column: "UPDATED", arrow: "↑",
		less: func(a, b taskRow) bool { return a.task.UpdatedAt.Before(b.task.UpdatedAt) },
	},
	sortIDAsc: {
		name: "id-asc", column: "ID", arrow: "↑",
		// Compares the IDs rather than trusting the store's order. That order
		// is id-asc only under v1, where the alias is a zero-padded creation
		// counter; a v2 alias is a content hash, so ListTasks sorts v2
		// projects by creation ordinal instead (internal/store/query.go) and
		// this mode sorted nothing at all while it was a no-op.
		less: func(a, b taskRow) bool { return a.id < b.id },
	},
	sortTitleAsc: {
		name: "title-asc", column: "TITLE", arrow: "↑",
		less: func(a, b taskRow) bool { return strings.ToLower(a.title) < strings.ToLower(b.title) },
	},
	sortAnnotate: {
		name: "annotate", column: "ANNOTATE", arrow: "↑",
		less: lessByAnnotateRank,
	},
}

// sortModeCount bounds the [s] cycle. Derived from the table rather than
// maintained by hand, so adding a mode cannot silently leave it unreachable.
const sortModeCount = len(sortSpecs)

type taskFocusMode int

const (
	// focusOff renders whatever t.filter yields: an empty filter -> all the
	// project's tasks, a lane board's FullName -> that lane. It is the only
	// mode: the grouped, negated and umbrella-idle views belonged to the
	// board ring, and pane [2] shows one lane at a time.
	focusOff taskFocusMode = iota
)

// taskFocus is the Tasks-pane view state the board strip sets on each level
// entry. ns names a real namespace for present/absent; bareTags switches
// present/absent to operate on unnamespaced (bare) labels instead.
type taskFocus struct {
	mode     taskFocusMode
	ns       string
	bareTags bool
}

func (s sortMode) String() string {
	if !s.valid() {
		return "?"
	}
	return sortSpecs[s].name
}

func (s sortMode) valid() bool { return s >= 0 && int(s) < len(sortSpecs) }

// spec is the current mode's row. An out-of-range mode falls back to the
// default rather than panicking: the cycle keeps sortMode in range, and a
// corrupted one should degrade to the list's normal order.
func (t *tasksModel) spec() sortSpec {
	if !t.sortMode.valid() {
		return sortSpecs[sortUpdatedDesc]
	}
	return sortSpecs[t.sortMode]
}

func newTasksModel(m *Model) tasksModel {
	return tasksModel{m: m, sortMode: sortUpdatedDesc}
}

func (t *tasksModel) pushDrill(level drillLevel) {
	t.drillStack = append(t.drillStack, level)
}

func (t *tasksModel) popDrill() {
	if n := len(t.drillStack); n > 0 {
		t.drillStack = t.drillStack[:n-1]
	}
}

func (t *tasksModel) currentDrill() *drillLevel {
	if len(t.drillStack) == 0 {
		return nil
	}
	return &t.drillStack[len(t.drillStack)-1]
}

func (t *tasksModel) detailID() string {
	level := t.currentDrill()
	if level == nil || level.kind != drillDetail {
		return ""
	}
	return level.id
}

func (t *tasksModel) SetSize(w, h int) {
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	t.width = w
	t.contentHeight = h
	// A placeholder until the first render: listPageSize() and
	// renderListWithStrip() both recompute the real page size from
	// listContentHeight(), which is the single source both agree on.
	t.pageSize = h - laneStripHeight - listChromeHeight
	if t.pageSize < 1 {
		t.pageSize = 1
	}
}

func (t *tasksModel) refresh() {
	t.rows = nil
	t.annReg = nil
	if t.m.projectScope == "" {
		t.clampCursor()
		return
	}
	scope := t.m.projectScope
	t.annReg = t.m.regFor(scope)
	filters := core.ParseFilter(t.filter)
	// Rows first, sort second: the cell has to exist before a comparator can
	// order by it.
	tasks := t.m.store.ListTasks(core.QueryFilters{Project: scope, Labels: filters})
	rows := make([]taskRow, 0, len(tasks))
	for _, tk := range tasks {
		rows = append(rows, t.toRow(tk))
	}
	if t.grouped {
		t.rows = t.applyGrouping(t.applySort(rows))
	} else {
		t.rows = t.applySort(rows)
	}
	t.clampCursor()
}

func (t *tasksModel) toRow(tk *core.Task) taskRow {
	return taskRow{
		id:      tk.ID,
		title:   tk.Title,
		labels:  tk.Labels,
		updated: relTime(tk.UpdatedAt, core.Now()),
		cell:    t.annotate(tk),
		task:    tk,
	}
}

// annotate renders the current capability's cell at refresh time so the
// per-frame render path stays pure formatting. Nil (no cell, no column) when
// no project is scoped. Uses the registry refresh resolved once (annReg),
// never regFor per row.
func (t *tasksModel) annotate(tk *core.Task) *capability.Cell {
	if t.annReg == nil {
		return nil
	}
	return t.annReg.Annotate(t.m.capability.current, *tk)
}

// applySort orders whole rows, so a cell travels with its row and a
// comparator may read either. Stable throughout: same-second timestamps are
// common, and rows the comparator cannot separate keep the store's order.
func (t *tasksModel) applySort(rows []taskRow) []taskRow {
	out := make([]taskRow, len(rows))
	copy(out, rows)
	less := t.spec().less
	sort.SliceStable(out, func(i, j int) bool { return less(out[i], out[j]) })
	return out
}

// lessByAnnotateRank orders the ANNOTATE column by what the current
// capability said about each row.
func lessByAnnotateRank(a, b taskRow) bool {
	ga, gb := annotateGroup(a.cell), annotateGroup(b.cell)
	if ga != gb {
		return ga < gb
	}
	if ga == annotateGroupRanked && a.cell.Rank != b.cell.Rank {
		return a.cell.Rank < b.cell.Rank
	}
	// Rank cannot separate them: fall back to the list's own default so a
	// group still reads newest-first rather than arbitrarily.
	return a.task.UpdatedAt.After(b.task.UpdatedAt)
}

// The ANNOTATE sort's three groups. Rank is only meaningful inside the
// ranked group: an unranked cell (Rank 0) still says something about the
// task, while a nil cell means the capability had nothing to say at all.
const (
	annotateGroupRanked = iota
	annotateGroupUnranked
	annotateGroupNone
)

// annotateGroup places a row's cell in the sort's group order. Ranks are
// per-capability, so this never compares a rank to anything but a sibling
// rank from the same annotator.
func annotateGroup(c *capability.Cell) int {
	switch {
	case c == nil:
		return annotateGroupNone
	case c.Rank == 0:
		return annotateGroupUnranked
	default:
		return annotateGroupRanked
	}
}

// setFocus applies a complete Tasks-pane view state (focus + filter) in one
// step, resets the cursor, and refreshes. This is the single channel the
// board ring/strip drives; the Tasks pane never edits its own filter.
func (t *tasksModel) setFocus(f taskFocus, filter string) {
	t.focus = f
	t.filter = filter
	t.cursor = 0
	t.offset = 0
	t.refresh()
}

func (t *tasksModel) handleKey(k tea.KeyMsg) tea.Cmd {
	if t.currentDrill() != nil {
		return t.handleDrillKey(k)
	}
	return t.handleListKey(k)
}

func (t *tasksModel) View() string {
	return t.renderListWithStrip()
}
