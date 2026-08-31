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
	view          tView

	// list state (flat + grouped)
	rows     []taskRow
	cursor   int
	offset   int
	pageSize int

	// filter / sort / focus
	filter   string
	sortMode sortMode
	focus    taskFocus

	// annReg is the capability registry annotate renders cells from,
	// resolved ONCE at the top of refresh — regFor runs a GetProject
	// freshness probe, and per-row resolution made refresh O(rows) probes
	// (ATM-4c476c). Nil when no scope is set.
	annReg *capability.Registry

	// detail
	detail taskDetailState

	// comment read-only overlay (peek); clears on backToList / openDetail so
	// stale overlay state never leaks across detail sessions.
	commentOverlay commentOverlayModel
}

type tView int

const (
	tViewList tView = iota
	tViewDetail
)

type sortMode int

const (
	sortUpdatedDesc sortMode = iota
	sortUpdatedAsc
	sortIDAsc
	sortTitleAsc
	sortAnnotate
)

// sortModeCount bounds the [s] cycle. Deriving the cycle from this constant
// rather than a literal is what keeps adding a mode from silently making the
// last one unreachable.
const sortModeCount = 5

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
	switch s {
	case sortUpdatedDesc:
		return "updated-desc"
	case sortUpdatedAsc:
		return "updated-asc"
	case sortIDAsc:
		return "id-asc"
	case sortTitleAsc:
		return "title-asc"
	case sortAnnotate:
		return "annotate"
	}
	return "?"
}

func newTasksModel(m *Model) tasksModel {
	return tasksModel{m: m, sortMode: sortUpdatedDesc}
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
	t.rows = t.applySort(rows)
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
	switch t.sortMode {
	case sortUpdatedDesc:
		sort.SliceStable(out, func(i, j int) bool {
			return out[i].task.UpdatedAt.After(out[j].task.UpdatedAt)
		})
	case sortUpdatedAsc:
		sort.SliceStable(out, func(i, j int) bool {
			return out[i].task.UpdatedAt.Before(out[j].task.UpdatedAt)
		})
	case sortIDAsc:
		// store already returns id-asc; no-op
	case sortTitleAsc:
		sort.SliceStable(out, func(i, j int) bool {
			return strings.ToLower(out[i].title) < strings.ToLower(out[j].title)
		})
	case sortAnnotate:
		sort.SliceStable(out, func(i, j int) bool {
			gi, gj := annotateGroup(out[i].cell), annotateGroup(out[j].cell)
			if gi != gj {
				return gi < gj
			}
			if gi == annotateGroupRanked && out[i].cell.Rank != out[j].cell.Rank {
				return out[i].cell.Rank < out[j].cell.Rank
			}
			// Rank cannot separate them: fall back to the list's own default
			// so a group still reads newest-first rather than arbitrarily.
			return out[i].task.UpdatedAt.After(out[j].task.UpdatedAt)
		})
	}
	return out
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
	switch t.view {
	case tViewList:
		return t.handleListKey(k)
	case tViewDetail:
		return t.handleDetailKey(k)
	}
	return nil
}

func (t *tasksModel) View() string {
	switch t.view {
	case tViewList:
		return t.renderListWithStrip()
	case tViewDetail:
		return t.renderDetailView()
	}
	return ""
}
