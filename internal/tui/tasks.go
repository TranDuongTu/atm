package tui

import (
	"atm/internal/core"
	"atm/internal/store"
	"github.com/charmbracelet/bubbletea"
)

type tasksModel struct {
	m             *Model
	width         int
	contentHeight int
	view          tView

	// list state (flat + grouped)
	rows     []taskRow
	groups   []taskGroup
	others   []taskRow
	cursor   int
	offset   int
	pageSize int

	// filter / sort / focus
	filter   string
	sortMode sortMode
	focus    taskFocus

	// detail
	detail taskDetailState

	// comment read-only overlay (peek) and history overlay; both clear on
	// backToList / openDetail so stale overlay state never leaks across
	// detail sessions.
	commentOverlay commentOverlayModel
	historyOverlay historyOverlayModel
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
)

type taskFocusMode int

const (
	// focusOff renders whatever t.filter yields: empty filter -> all tasks
	// flat (L0); an exact label token -> that label's tasks flat (L2).
	focusOff taskFocusMode = iota
	// focusPresent renders tasks that carry the namespace. Real namespace:
	// grouped via GroupTasks with others hidden. bareTags: flat predicate.
	focusPresent
	// focusAbsent renders tasks that do NOT carry the namespace. Real
	// namespace: the GroupTasks others bucket, flat. bareTags: flat predicate.
	focusAbsent
	// focusUnlabeled renders tasks with zero labels.
	focusUnlabeled
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
	t.pageSize = h - stripHeight - 6
	if t.pageSize < 1 {
		t.pageSize = 1
	}
}

func (t *tasksModel) refresh() {
	t.rows = nil
	t.groups = nil
	t.others = nil
	if t.m.projectScope == "" {
		t.clampCursor()
		return
	}
	scope := t.m.projectScope
	switch t.focus.mode {
	case focusUnlabeled:
		for _, tk := range t.applySort(t.m.store.ListTasks(store.QueryFilters{Project: scope})) {
			if len(tk.Labels) == 0 {
				t.rows = append(t.rows, t.toRow(tk))
			}
		}
	case focusPresent, focusAbsent:
		if t.focus.bareTags {
			for _, tk := range t.applySort(t.m.store.ListTasks(store.QueryFilters{Project: scope})) {
				has := core.HasBareTag(scope, tk.Labels)
				if (t.focus.mode == focusPresent) == has {
					t.rows = append(t.rows, t.toRow(tk))
				}
			}
			break
		}
		filters := t.parseFilter()
		groups, others := t.m.store.GroupTasks(store.QueryFilters{Project: scope, Labels: filters})
		if t.focus.mode == focusPresent {
			wildcards := core.WildcardTokens(filters)
			for _, g := range groups {
				rows := make([]taskRow, 0, len(g.Tasks))
				for _, tk := range g.Tasks {
					rows = append(rows, t.toRow(tk))
				}
				tg := taskGroup{label: g.Label, rows: rows}
				if len(wildcards) >= 2 {
					tg.subgroups = buildNestedGroups(g.Tasks, wildcards[1:], t.toRow)
					tg.rows = nil
				}
				t.groups = append(t.groups, tg)
			}
		} else {
			for _, tk := range t.applySort(others) {
				t.rows = append(t.rows, t.toRow(tk))
			}
		}
	default: // focusOff
		filters := t.parseFilter()
		ts := t.applySort(t.m.store.ListTasks(store.QueryFilters{Project: scope, Labels: filters}))
		if wildcards := core.WildcardTokens(filters); len(wildcards) > 0 {
			groups, others := t.m.store.GroupTasks(store.QueryFilters{Project: scope, Labels: filters})
			for _, g := range groups {
				rows := make([]taskRow, 0, len(g.Tasks))
				for _, tk := range g.Tasks {
					rows = append(rows, t.toRow(tk))
				}
				tg := taskGroup{label: g.Label, rows: rows}
				if len(wildcards) >= 2 {
					tg.subgroups = buildNestedGroups(g.Tasks, wildcards[1:], t.toRow)
					tg.rows = nil
				}
				t.groups = append(t.groups, tg)
			}
			for _, tk := range others {
				t.others = append(t.others, t.toRow(tk))
			}
		} else {
			for _, tk := range ts {
				t.rows = append(t.rows, t.toRow(tk))
			}
		}
	}
	t.clampCursor()
}

func (t *tasksModel) toRow(tk *store.Task) taskRow {
	return taskRow{
		id:      tk.ID,
		title:   tk.Title,
		labels:  tk.Labels,
		updated: relTime(tk.UpdatedAt, store.Now()),
		task:    tk,
	}
}

func (t *tasksModel) applySort(ts []*store.Task) []*store.Task {
	out := make([]*store.Task, len(ts))
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
