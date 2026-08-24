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
	// (ATM-4c476c). Nil when no scope is set or the unmanaged
	// pseudo-capability is current.
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
)

// sortModeCount bounds the [s] cycle. Deriving the cycle from this constant
// rather than a literal is what keeps adding a mode from silently making the
// last one unreachable.
const sortModeCount = 4

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
	// header line + blank + column header + rule + footer + margin, plus the
	// board strip reserved in the list view. This is only a placeholder value
	// for t.pageSize until the first render — listPageSize() and
	// renderListWithStrip() both recompute the real page size from
	// listContentHeight(), which also accounts for the fixed tabbed pinned box
	// (SetSize never re-runs on a pin toggle and would otherwise leave this
	// value stale).
	t.pageSize = h - laneStripHeight - 6
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
	if !t.m.capability.unmanagedCurrent() {
		t.annReg = t.m.regFor(scope)
	}
	filters := core.ParseFilter(t.filter)
	for _, tk := range t.applySort(t.m.store.ListTasks(core.QueryFilters{Project: scope, Labels: filters})) {
		t.rows = append(t.rows, t.toRow(tk))
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
// no project is scoped or the unmanaged pseudo-capability is current. Uses
// the registry refresh resolved once (annReg), never regFor per row.
func (t *tasksModel) annotate(tk *core.Task) *capability.Cell {
	if t.annReg == nil {
		return nil
	}
	return t.annReg.Annotate(t.m.capability.current, *tk)
}

func (t *tasksModel) applySort(ts []*core.Task) []*core.Task {
	out := make([]*core.Task, len(ts))
	copy(out, ts)
	switch t.sortMode {
	case sortUpdatedDesc:
		// stable: most recent first
		// Use insertion-stable by index after a manual compare.
		for i := 1; i < len(out); i++ {
			for j := i; j > 0; j-- {
				if out[j].UpdatedAt.After(out[j-1].UpdatedAt) {
					out[j], out[j-1] = out[j-1], out[j]
				}
			}
		}
	case sortUpdatedAsc:
		for i := 1; i < len(out); i++ {
			for j := i; j > 0; j-- {
				if out[j].UpdatedAt.Before(out[j-1].UpdatedAt) {
					out[j], out[j-1] = out[j-1], out[j]
				}
			}
		}
	case sortIDAsc:
		// store already returns id-asc; no-op
	case sortTitleAsc:
		sort.SliceStable(out, func(i, j int) bool {
			return strings.ToLower(out[i].Title) < strings.ToLower(out[j].Title)
		})
	}
	return out
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
