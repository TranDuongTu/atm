package tui

import (
	"fmt"
	"sort"
	"strings"

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

type taskRow struct {
	id      string
	title   string
	labels  []string
	updated string
	task    *store.Task
}

type taskGroup struct {
	label string
	rows  []taskRow
	// subgroups holds nested facets for multi-wildcard filters (depth =
	// number of wildcards). Empty for the single-wildcard flat path and for
	// the deepest level. A task appears in every sub-group whose key it
	// carries (multi-membership preserved). Tasks in this group that match
	// no deeper wildcard land in a sub-`(no matching labels)` bucket (label
	// == "").
	subgroups []taskGroup
	// collapsed controls group-header expand/collapse.
	collapsed bool
}

type taskDetailState struct {
	id     string
	task   *store.Task
	lines  []string
	offset int
}

func newTasksModel(m *Model) tasksModel {
	return tasksModel{m: m, sortMode: sortUpdatedDesc}
}

// stripHeight is the fixed height of the board thumbnail strip rendered
// above the task list (list view only; clamps down on short terminals).
const stripHeight = 8

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
	// listContentHeight(), which also accounts for the live pinned stack
	// (SetSize never re-runs on a pin toggle and would otherwise leave this
	// value stale).
	t.pageSize = h - stripHeight - 6
	if t.pageSize < 1 {
		t.pageSize = 1
	}
}

// listContentHeight is the single source of truth for how many lines the
// scrollable task list gets in the list view, once the fixed board strip and
// the FIXED pinned slot are subtracted. The pinned slot always reserves
// 3*maxPins lines (renderPinnedStack draws exactly maxPins boxes, filled or
// placeholder), so the subtraction is a CONSTANT — the list height never
// changes as pins are added or removed. renderListWithStrip and listPageSize
// both derive from this single value, so the renderer and the pgup/pgdown page
// jumps always agree on the page boundary.
func (t *tasksModel) listContentHeight() int {
	h := t.contentHeight - stripHeight - 3*maxPins
	if h < 4 {
		h = 4
	}
	return h
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
				has := taskHasBareTag(scope, tk)
				if (t.focus.mode == focusPresent) == has {
					t.rows = append(t.rows, t.toRow(tk))
				}
			}
			break
		}
		filters := t.parseFilter()
		groups, others := t.m.store.GroupTasks(store.QueryFilters{Project: scope, Labels: filters})
		if t.focus.mode == focusPresent {
			wildcards := wildcardTokens(filters)
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
		if wildcards := wildcardTokens(filters); len(wildcards) > 0 {
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

// parseFilter splits the filter string on spaces; tokens ending `:*` are
// wildcards (facets), others are exact restrictors.
func (t *tasksModel) parseFilter() []string {
	s := strings.TrimSpace(t.filter)
	if s == "" {
		return nil
	}
	return strings.Fields(s)
}

// taskHasBareTag reports whether t carries at least one unnamespaced (bare)
// label — a label whose suffix after the "<scope>:" prefix contains no colon.
func taskHasBareTag(scope string, t *store.Task) bool {
	for _, full := range t.Labels {
		suffix := strings.TrimPrefix(full, scope+":")
		if !strings.Contains(suffix, ":") {
			return true
		}
	}
	return false
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

func (t *tasksModel) hasWildcard() bool {
	for _, tok := range t.parseFilter() {
		if isWildcardTUI(tok) {
			return true
		}
	}
	return false
}

// grouped reports whether the list should render as grouped facets (vs a flat
// row list). Only a real-namespace present focus (or a legacy focusOff wildcard
// filter) groups; absent/unlabeled/bare-tag focuses are always flat, even
// though their filter may still carry a wildcard token.
func (t *tasksModel) grouped() bool {
	switch t.focus.mode {
	case focusPresent:
		return !t.focus.bareTags
	case focusOff:
		return t.hasWildcard()
	default:
		return false
	}
}

func isWildcardTUI(l string) bool { return strings.HasSuffix(l, ":*") }

// facetToken returns the full wildcard label used to facet the Tasks pane by
// a namespace, e.g. facetToken("ATM","status") == "ATM:status:*".
func facetToken(scope, ns string) string { return scope + ":" + ns + ":*" }

// filterHasToken reports whether token is one of the space-separated fields of
// filter.
func filterHasToken(filter, token string) bool {
	for _, f := range strings.Fields(filter) {
		if f == token {
			return true
		}
	}
	return false
}

// filterAddToken appends token to filter (single-space separated) unless it is
// already present.
func filterAddToken(filter, token string) string {
	if filterHasToken(filter, token) {
		return filter
	}
	if strings.TrimSpace(filter) == "" {
		return token
	}
	return filter + " " + token
}

// filterRemoveToken removes every occurrence of token from filter and rejoins
// the remaining fields with single spaces.
func filterRemoveToken(filter, token string) string {
	var kept []string
	for _, f := range strings.Fields(filter) {
		if f != token {
			kept = append(kept, f)
		}
	}
	return strings.Join(kept, " ")
}

func wildcardTokens(labels []string) []string {
	var out []string
	for _, l := range labels {
		if isWildcardTUI(l) {
			out = append(out, l)
		}
	}
	return out
}

// buildNestedGroups buckets `tasks` by the concrete labels they carry that
// match the given wildcards, recursing for each remaining wildcard. This is
// the TUI-side nesting pass that turns the store's flat per-concrete-label
// groups into the nested facet tree (mockup Screen 7, two-wildcard case).
//
// Multi-membership: a task appears in every sub-group whose key it carries.
// Tasks matching no label for the current wildcard land in a sub-
// `(no matching labels)` bucket (label == ""), consistent with the top-level
// pattern. At the deepest level (no remaining wildcards), the caller already
// holds the leaf rows; this helper only recurses while wildcards remain.
func buildNestedGroups(tasks []*store.Task, wildcards []string, toRow func(*store.Task) taskRow) []taskGroup {
	if len(wildcards) == 0 {
		return nil
	}
	w := wildcards[0]
	// Bucket tasks by each concrete label they carry matching w, preserving
	// discovery order then alphabetical (store.GroupTasks already sorts;
	// we sort here for determinism independent of input order).
	buckets := map[string][]*store.Task{}
	var keys []string
	matched := map[*store.Task]bool{}
	for _, t := range tasks {
		for _, l := range t.Labels {
			if labelMatchesWildcardTUI(l, w) {
				if _, exists := buckets[l]; !exists {
					keys = append(keys, l)
				}
				buckets[l] = append(buckets[l], t)
				matched[t] = true
			}
		}
	}
	sort.Strings(keys)
	// (no matching labels) sub-bucket: tasks matching no label for w.
	var noneMatched []*store.Task
	for _, t := range tasks {
		if !matched[t] {
			noneMatched = append(noneMatched, t)
		}
	}
	var groups []taskGroup
	for _, k := range keys {
		rows := make([]taskRow, 0, len(buckets[k]))
		for _, tk := range buckets[k] {
			rows = append(rows, toRow(tk))
		}
		g := taskGroup{label: k}
		if len(wildcards) >= 2 {
			g.subgroups = buildNestedGroups(buckets[k], wildcards[1:], toRow)
			// Leaf rows live only at the deepest level.
			g.rows = nil
		} else {
			g.rows = rows
		}
		groups = append(groups, g)
	}
	// Sub-`(no matching labels)` bucket, rendered last within this level.
	if len(noneMatched) > 0 {
		rows := make([]taskRow, 0, len(noneMatched))
		for _, tk := range noneMatched {
			rows = append(rows, toRow(tk))
		}
		g := taskGroup{label: ""} // "" == (no matching labels)
		if len(wildcards) >= 2 {
			g.subgroups = buildNestedGroups(noneMatched, wildcards[1:], toRow)
		} else {
			g.rows = rows
		}
		groups = append(groups, g)
	}
	return groups
}

// labelMatchesWildcardTUI reports whether label matches the wildcard (e.g.
// "ATM:status:open" matches "ATM:status:*"). Mirrors store.labelMatchesWildcard
// without exposing the unexported helper.
func labelMatchesWildcardTUI(label, wildcard string) bool {
	prefix := strings.TrimSuffix(wildcard, "*")
	return strings.HasPrefix(label, prefix)
}

func (t *tasksModel) clampCursor() {
	if t.cursor < 0 {
		t.cursor = 0
	}
	// For grouped view, the cursor indexes into a flattened list of
	// (group header, group rows, others header, others rows). We compute that
	// lazily in render; clamp to total line count.
	total := t.flatLineCount()
	if t.cursor >= total {
		t.cursor = total - 1
	}
	if t.cursor < 0 {
		t.cursor = 0
	}
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

func (t *tasksModel) handleListKey(k tea.KeyMsg) tea.Cmd {
	switch k.String() {
	case "j", "down":
		t.cursorDown()
	case "k", "up":
		t.cursorUp()
	case "g":
		t.cursor = 0
		t.offset = 0
	case "[", "]":
		dir := -1
		if k.String() == "]" {
			dir = 1
		}
		t.m.boards.cycleBoard(dir)
	case "pgdown":
		t.cursor += t.listPageSize()
		t.clampCursor()
	case "pgup":
		t.cursor -= t.listPageSize()
		t.clampCursor()
	case ">", "<":
		// Drill the SELECTED thumbnail in / out via boardsModel's level navigation.
		if k.String() == ">" {
			t.m.boards.drillIn()
		} else {
			t.m.boards.drillOut()
		}
	case "{", "}":
		// Move the SELECTED thumbnail's chart cursor (the member that >, d, l target).
		dir := -1
		if k.String() == "}" {
			dir = 1
		}
		t.m.boards.chartCursorMove(dir)
	case "p":
		t.m.boards.togglePin()
	case "!", "@", "#", "$", "%", "^", "&", "*", "(":
		n := shiftDigitToInt(k.String())
		t.m.boards.jumpPin(n)
	case "s":
		// cycle sort
		t.sortMode = (t.sortMode + 1) % 3
		t.refresh()
	case "a":
		if t.m.projectScope == "" {
			return nil
		}
		t.openCreateForm()
	case "n", "e", "S", "d", "l":
		// Board-authoring keys, scoped to the SELECTED board at its current
		// drill level. Delegated to a selection-aware handler on boardsModel
		// (not handleTableKey, whose e targets b.cursor — wrong in the merged
		// pane, where cycleBoard resets b.cursor to 0 and the selection lives
		// at b.ringIndex()).
		return t.m.boards.handleAuthoringKey(k)
	case "enter":
		if t.grouped() {
			// Enter is context-sensitive: toggle a header, or open detail
			// on a leaf row (spec Screen 7). The (no matching labels)
			// header is not collapsible but its rows are openable.
			if r, ok := t.rowAtCursor(); ok {
				return t.openDetail(r.id)
			}
			return t.toggleGroupAtCursor()
		}
		return t.openDetailAtCursor()
	}
	return nil
}

func (t *tasksModel) handleDetailKey(k tea.KeyMsg) tea.Cmd {
	if t.commentOverlay.id != "" {
		return t.handleCommentOverlayKey(k)
	}
	if t.historyOverlay.active {
		return t.handleHistoryOverlayKey(k)
	}
	switch k.String() {
	case "j", "down":
		t.detail.offset++
		t.clampDetail()
	case "k", "up":
		if t.detail.offset > 0 {
			t.detail.offset--
		}
	case "g":
		t.detail.offset = 0
	case "pgdown", " ":
		t.detail.offset += t.contentHeight / 2
		t.clampDetail()
	case "pgup":
		if t.detail.offset > t.contentHeight/2 {
			t.detail.offset -= t.contentHeight / 2
		} else {
			t.detail.offset = 0
		}
	case "e":
		t.openTitleForm()
	case "d":
		t.openDescriptionForm()
	case "b":
		t.openLabelAddForm()
	case "B":
		t.openLabelRemoveForm()
	case "x":
		return t.requestRemoveTask()
	case "M":
		t.openCommentAddForm()
	case "H":
		return t.openHistoryOverlay()
	case "enter":
		cs, _ := t.m.store.ListComments(t.detail.id)
		if len(cs) > 0 {
			return t.openCommentOverlay(cs[0].ID)
		}
	case "esc":
		t.backToList()
	}
	return nil
}

// --- cursor navigation ---

func (t *tasksModel) cursorDown() {
	total := t.flatLineCount()
	if t.cursor < total-1 {
		t.cursor++
	}
}

func (t *tasksModel) cursorUp() {
	if t.cursor > 0 {
		t.cursor--
	}
}

// flatLineCount returns the number of logical lines (headers + rows) the
// list view presents — used for cursor bounds and paging.
func (t *tasksModel) flatLineCount() int {
	if t.grouped() {
		n := 0
		for _, g := range t.groups {
			n += groupLineCount(g)
		}
		if t.focus.mode == focusOff {
			n++ // (no matching labels) header
			n += len(t.others)
		}
		return n
	}
	return len(t.rows)
}

// groupLineCount returns the logical lines contributed by one group and its
// (possibly nested) sub-groups: 1 for the header, plus its leaf rows or the
// recursive count of expanded sub-groups. A collapsed group contributes only
// its header.
func groupLineCount(g taskGroup) int {
	n := 1 // header
	if g.collapsed {
		return n
	}
	if len(g.subgroups) > 0 {
		for _, sg := range g.subgroups {
			n += groupLineCount(sg)
		}
	} else {
		n += len(g.rows)
	}
	return n
}

func (t *tasksModel) openDetailAtCursor() tea.Cmd {
	if !t.grouped() {
		if t.cursor >= 0 && t.cursor < len(t.rows) {
			return t.openDetail(t.rows[t.cursor].id)
		}
	}
	return nil
}

// rowAtCursor returns the leaf row the cursor currently sits on in the
// grouped view, or (zero, false) if the cursor is on a group/bucket header
// (or out of range). Used to make `Enter` context-sensitive per the spec.
func (t *tasksModel) rowAtCursor() (taskRow, bool) {
	if !t.grouped() {
		return taskRow{}, false
	}
	idx := 0
	for _, g := range t.groups {
		if r, ok, next := rowInGroup(g, idx, t.cursor); ok {
			return r, true
		} else {
			idx = next
		}
	}
	if t.focus.mode == focusOff {
		// (no matching labels) bucket: header then rows.
		if idx == t.cursor {
			return taskRow{}, false
		}
		idx++
		// rows in the top-level (no matching labels) bucket are not nested.
		if t.cursor >= idx && t.cursor < idx+len(t.others) {
			return t.others[t.cursor-idx], true
		}
	}
	return taskRow{}, false
}

// rowInGroup walks one group's flattened lines looking for a leaf row at the
// flattened index `cursor` (relative to `start`). Returns (row, true, _) when
// the cursor sits on a leaf row; (zero, false, next) otherwise, where next is
// the flattened index after this group's contribution.
func rowInGroup(g taskGroup, start, cursor int) (row taskRow, ok bool, next int) {
	idx := start
	if idx == cursor {
		return taskRow{}, false, idx // header, not a row
	}
	idx++ // header
	if g.collapsed {
		return taskRow{}, false, idx
	}
	if len(g.subgroups) > 0 {
		for _, sg := range g.subgroups {
			if r, ok, next := rowInGroup(sg, idx, cursor); ok {
				return r, true, next
			} else {
				idx = next
			}
		}
	} else {
		if cursor >= idx && cursor < idx+len(g.rows) {
			return g.rows[cursor-idx], true, idx + len(g.rows)
		}
		idx += len(g.rows)
	}
	return taskRow{}, false, idx
}

func (t *tasksModel) toggleGroupAtCursor() tea.Cmd {
	if !t.grouped() {
		return nil
	}
	idx := 0
	for gi := range t.groups {
		if done, next := toggleInGroup(&t.groups[gi], idx, t.cursor); done {
			return nil
		} else {
			idx = next
		}
	}
	// (no matching labels) header is not collapsible.
	return nil
}

// toggleInGroup walks one group (and its nested sub-groups) looking for the
// header at the flattened index `cursor` (relative to `start`). If found it
// toggles collapse and returns (true, _). Otherwise it returns (false, nextIdx)
// where nextIdx is the flattened index after this group's contribution.
func toggleInGroup(g *taskGroup, start, cursor int) (done bool, next int) {
	idx := start
	if idx == cursor {
		g.collapsed = !g.collapsed
		return true, idx
	}
	idx++ // header
	if g.collapsed {
		return false, idx
	}
	if len(g.subgroups) > 0 {
		for i := range g.subgroups {
			if done, next := toggleInGroup(&g.subgroups[i], idx, cursor); done {
				return true, next
			} else {
				idx = next
			}
		}
	} else {
		idx += len(g.rows)
	}
	return false, idx
}

func (t *tasksModel) openDetail(id string) tea.Cmd {
	tk, err := t.m.store.GetTask(id)
	if err != nil {
		t.m.showToast("error: " + err.Error())
		return nil
	}
	t.commentOverlay = commentOverlayModel{}
	t.historyOverlay = historyOverlayModel{}
	t.detail = taskDetailState{id: id, task: tk}
	t.view = tViewDetail
	t.renderDetail()
	return nil
}

func (t *tasksModel) backToList() {
	t.view = tViewList
	t.detail = taskDetailState{}
	t.commentOverlay = commentOverlayModel{}
	t.historyOverlay = historyOverlayModel{}
}

func (t *tasksModel) renderDetail() {
	var b strings.Builder
	tk := t.detail.task
	if tk == nil {
		return
	}
	fmt.Fprintf(&b, "Task %s\n", tk.ID)
	b.WriteString(sepLine("─", 78, t.width, 2))
	b.WriteString("\n")
	b.WriteString(t.m.styles.Muted.Render(tk.Title))
	b.WriteString("\n\n")
	b.WriteString(sectionCaption(t.m.styles, t.width, "FACTS"))
	b.WriteString("\n")
	fmt.Fprintf(&b, "%s\n", dashboardLine(t.width, fmt.Sprintf("id      %s", tk.ID)))
	fmt.Fprintf(&b, "%s\n", dashboardLine(t.width, fmt.Sprintf("project %s", tk.ProjectCode)))
	fmt.Fprintf(&b, "%s\n", dashboardLine(t.width, fmt.Sprintf("title   %s", tk.Title)))
	if tk.Description == "" {
		b.WriteString(dashboardLine(t.width, "description (none)"))
		b.WriteString("\n")
	} else {
		for i, line := range strings.Split(tk.Description, "\n") {
			if i == 0 {
				fmt.Fprintf(&b, "%s\n", dashboardLine(t.width, fmt.Sprintf("description %s", line)))
			} else {
				fmt.Fprintf(&b, "%s\n", dashboardLine(t.width, fmt.Sprintf("            %s", line)))
			}
		}
	}
	fmt.Fprintf(&b, "%s\n", dashboardLine(t.width, fmt.Sprintf("created %s   by %s", store.RFC3339UTC(tk.CreatedAt), tk.CreatedBy)))
	fmt.Fprintf(&b, "%s\n", dashboardLine(t.width, fmt.Sprintf("updated %s   by %s", store.RFC3339UTC(tk.UpdatedAt), tk.UpdatedBy)))
	b.WriteString("\n")

	b.WriteString(sectionCaption(t.m.styles, t.width, "LABELS"))
	b.WriteString("\n")
	if len(tk.Labels) == 0 {
		b.WriteString(dashboardLine(t.width, " (no labels)"))
		b.WriteString("\n")
	} else {
		chips := renderLabelChips(t.m.styles, tk.Labels, t.width-2)
		b.WriteString(dashboardLine(t.width, " "+chips))
		b.WriteString("\n")
	}
	b.WriteString("\n")

	b.WriteString(sectionCaption(t.m.styles, t.width, "COMMENTS"))
	b.WriteString("\n")
	cs, _ := t.m.store.ListComments(tk.ID)
	if len(cs) == 0 {
		b.WriteString(dashboardLine(t.width, " (no comments)"))
		b.WriteString("\n")
	} else {
		for _, c := range cs {
			labels := "(no labels)"
			if len(c.Labels) > 0 {
				labels = strings.Join(c.Labels, " ")
			}
			fmt.Fprintf(&b, "%s\n", dashboardLine(t.width, fmt.Sprintf(" %s   %s   %s", c.CreatedBy, relTime(c.CreatedAt, store.Now()), truncateRunes(labels, 36))))
			bodyLines := strings.Split(c.Body, "\n")
			maxLines := 6
			for i := 0; i < len(bodyLines) && i < maxLines; i++ {
				fmt.Fprintf(&b, "%s\n", dashboardLine(t.width, fmt.Sprintf("     %s", bodyLines[i])))
			}
			if len(bodyLines) > maxLines {
				fmt.Fprintf(&b, "%s\n", dashboardLine(t.width, "     …"))
			}
		}
	}
	t.detail.lines = strings.Split(b.String(), "\n")
	t.clampDetail()
}

func (t *tasksModel) clampDetail() {
	maxOff := len(t.detail.lines) - t.contentHeight
	if maxOff < 0 {
		maxOff = 0
	}
	if t.detail.offset > maxOff {
		t.detail.offset = maxOff
	}
	if t.detail.offset < 0 {
		t.detail.offset = 0
	}
}

// --- view ---

func (t *tasksModel) View() string {
	switch t.view {
	case tViewList:
		return t.renderListWithStrip()
	case tViewDetail:
		return t.renderDetailView()
	}
	return ""
}

// renderListWithStrip renders the list view top to bottom: the task list
// (fills), then the pinned-boards stack, then the board thumbnail strip at
// the bottom (the detail view keeps the full pane since the strip is
// contextual to browsing). It reuses the existing renderList() by temporarily
// shrinking t.contentHeight/t.pageSize to the list's sub-height (from
// listContentHeight()) rather than refactoring renderList itself —
// renderList already ends with padToHeight(..., t.contentHeight), so the
// shrink makes it pad to the sub-height, and the outer padToHeight below
// clamps any rounding.
func (t *tasksModel) renderListWithStrip() string {
	listH := t.listContentHeight()
	savedH, savedPageSize := t.contentHeight, t.pageSize
	t.contentHeight = listH
	t.pageSize = listH - 6
	if t.pageSize < 1 {
		t.pageSize = 1
	}
	listOut := t.renderList()
	t.contentHeight, t.pageSize = savedH, savedPageSize

	pinned := t.m.boards.renderPinnedStack(t.width)
	strip := t.m.boards.renderStrip(t.width, stripHeight)

	var b strings.Builder
	b.WriteString(listOut)
	if pinned != "" {
		b.WriteString("\n")
		b.WriteString(pinned)
	}
	b.WriteString("\n")
	b.WriteString(strip)
	return padToHeight(b.String(), t.contentHeight)
}

func (t *tasksModel) headerLine() string {
	proj := t.m.projectScope
	if proj == "" {
		proj = "(none)"
	}
	return fmt.Sprintf("PROJECT: %s    FOCUS: %s    SORT: %s", proj, t.focusCaption(), t.sortMode)
}

// focusCaption is a read-only description of why the Tasks list is scoped,
// derived from the focus set by the board strip. Empty focus reads "(all)".
func (t *tasksModel) focusCaption() string {
	switch t.focus.mode {
	case focusPresent:
		if t.focus.bareTags {
			return "bare tags"
		}
		return t.focus.ns
	case focusAbsent:
		if t.focus.bareTags {
			return "no bare tags"
		}
		return "no " + t.focus.ns
	case focusUnlabeled:
		return "unlabeled"
	default: // focusOff
		if f := strings.TrimSpace(t.filter); f != "" {
			return f // exact-label token at L2
		}
		return "(all)"
	}
}

func (t *tasksModel) renderList() string {
	var b strings.Builder
	b.WriteString(dashboardLine(t.width, t.m.styles.HeaderLine.Render(t.headerLine())))
	b.WriteString("\n")
	b.WriteString("\n")

	if t.m.projectScope == "" {
		t.renderEmptyState(&b, []string{
			t.m.styles.EmptyHead.Render("no project selected"),
			"",
			t.m.styles.EmptyText.Render(fmt.Sprintf("press %s in the Projects pane to scope this view", t.m.styles.EmptyKey.Render("[s]"))),
		})
		return padToHeight(b.String(), t.contentHeight)
	}

	if t.grouped() {
		t.renderGroupedList(&b)
	} else {
		t.renderFlatList(&b)
	}
	return padToHeight(b.String(), t.contentHeight)
}

// renderEmptyState appends a vertically+horizontally centered empty-state
// block (each line center-aligned independently) into b. The block is
// centered within contentHeight-1 to account for the header line already
// written by the caller.
func (t *tasksModel) renderEmptyState(b *strings.Builder, lines []string) {
	b.WriteString(centerLinesBoth(lines, t.width, t.contentHeight-1))
}

// taskColumnWidths returns fixed widths for ID/LABELS/UPDATED and a flexible
// TITLE width that absorbs the remaining pane width. The format string used
// by both the header and data rows is " %-*s %-*s %-*s %*s" (leading space +
// 3 inter-column spaces = 4 extra columns of padding).
//
// idW is sized to the widest ID actually present (clamped to [9, 14]): task
// IDs are "<CODE>-<hash>" (e.g. DEMO-f7d632, 11 chars), and Go's %-9s does NOT
// truncate a longer value — an under-sized idW would let every row overflow
// its column and push the trailing UPDATED value off the pane, clipping it
// (the "1d ago" -> "1d ag" bug). Sizing to the real IDs keeps the row within
// the pane; renderFlatList still truncates defensively.
func (t *tasksModel) taskColumnWidths() (idW, labelsW, updatedW, titleW int) {
	idW, labelsW, updatedW = 9, 18, 8
	for _, r := range t.rows {
		if w := len(r.id); w > idW {
			idW = w
		}
	}
	if idW > 14 {
		idW = 14
	}
	titleW = t.width - idW - labelsW - updatedW - 4
	if titleW < 16 {
		titleW = 16
	}
	return
}

func (t *tasksModel) renderFlatList(b *strings.Builder) {
	if len(t.rows) == 0 {
		t.renderEmptyState(b, []string{
			t.m.styles.EmptyHead.Render("no tasks match this focus"),
			"",
			t.m.styles.EmptyText.Render("switch boards with [ / ] to change focus"),
		})
		return
	}
	idW, labelsW, updatedW, titleW := t.taskColumnWidths()
	header := fmt.Sprintf(" %-*s %-*s %-*s %*s", idW, "ID", titleW, "TITLE", labelsW, "LABELS", updatedW, "UPDATED")
	b.WriteString(dashboardLine(t.width, t.m.styles.HeaderLabel.Render(header)))
	b.WriteString("\n")
	b.WriteString(dashboardLine(t.width, repeat("─", dashboardContentWidth(t.width))))
	b.WriteString("\n")

	start, end := t.pageWindow(len(t.rows))
	for i := start; i < end; i++ {
		r := t.rows[i]
		labels := "-"
		if len(r.labels) > 0 {
			labels = strings.Join(r.labels, " ")
		}
		line := fmt.Sprintf(" %-*s %-*s %-*s %*s", idW, truncateRunes(r.id, idW), titleW, truncateRunes(r.title, titleW), labelsW, truncateRunes(labels, labelsW), updatedW, r.updated)
		if i == t.cursor {
			line = " " + t.m.styles.RowCursor.Render(strings.TrimPrefix(line, " "))
		}
		b.WriteString(dashboardLine(t.width, line))
		b.WriteString("\n")
	}
	b.WriteString(dashboardLine(t.width, fmt.Sprintf(" showing %d-%d of %d", start+1, end, len(t.rows))))
}

func (t *tasksModel) renderGroupedList(b *strings.Builder) {
	if t.focus.mode == focusPresent && len(t.groups) == 0 {
		t.renderEmptyState(b, []string{
			t.m.styles.EmptyHead.Render("no tasks match this focus"),
			"",
			t.m.styles.EmptyText.Render("switch boards with [ / ] to change focus"),
		})
		return
	}

	// Check the wildcard-yields-no-labels state.
	if len(t.groups) == 0 {
		b.WriteString(centerLinesBoth([]string{
			t.m.styles.EmptyHead.Render("no labels match wildcard — add labels to tasks"),
		}, t.width, t.contentHeight-1))
		b.WriteString("\n")
	}

	// Build the full group/row tree into `body` first (idx mirrors the exact
	// flattened line-index scheme flatLineCount/rowAtCursor use), then window
	// it to the visible page so the cursor's row stays in view and "[" / "]"
	// have something well-defined to jump.
	var body strings.Builder
	idx := 0
	for _, g := range t.groups {
		idx = t.renderGroup(&body, g, 0, idx)
	}
	if t.focus.mode == focusOff {
		// (no matching labels) bucket is legacy focusOff behavior. It stays
		// flat (no nesting): these tasks matched no wildcard.
		header := t.m.styles.GroupHeader.Render(fmt.Sprintf("▾ (no matching labels) (%d)", len(t.others)))
		if idx == t.cursor {
			header = t.m.styles.RowCursor.Render(header)
		}
		body.WriteString(dashboardLine(t.width, header))
		body.WriteString("\n")
		idx++
		for _, r := range t.others {
			labels := "(no labels)"
			if len(r.labels) > 0 {
				labels = strings.Join(r.labels, " ")
			}
			titleW := t.width - 6
			if titleW < 20 {
				titleW = 20
			}
			if titleW > 32 {
				titleW = 32
			}
			line := fmt.Sprintf("  %s   id %s   labels %s   updated %s", truncateRunes(r.title, titleW), r.id, truncateRunes(labels, 36), r.updated)
			if idx == t.cursor {
				line = " " + t.m.styles.RowCursor.Render(strings.TrimPrefix(line, " "))
			}
			body.WriteString(dashboardLine(t.width, line))
			body.WriteString("\n")
			idx++
		}
	}

	lines := strings.Split(strings.TrimSuffix(body.String(), "\n"), "\n")
	start, end := windowLines(len(lines), t.cursor, t.groupPageSize())
	for i := start; i < end; i++ {
		b.WriteString(lines[i])
		b.WriteString("\n")
	}
	b.WriteString(dashboardLine(t.width, fmt.Sprintf(" showing %d-%d of %d", start+1, end, len(lines))))
}

// renderGroup renders one group (header + its leaf rows or its expanded
// sub-groups) at the given indentation depth, returning the next flattened
// line index after this group's contribution. `depth` is the nesting level
// (0 = top); each level indents rows by two spaces. The header count is the
// total leaf tasks under this group. A label of "" denotes the per-level
// `(no matching labels)` sub-bucket.
func (t *tasksModel) renderGroup(b *strings.Builder, g taskGroup, depth, idx int) int {
	marker := "▾"
	if g.collapsed {
		marker = "▸"
	}
	count := groupLeafCount(g)
	name := g.label
	if name == "" {
		name = "(no matching labels)"
	}
	indent := strings.Repeat("  ", depth)
	header := t.m.styles.GroupHeader.Render(fmt.Sprintf("%s%s %s (%d)", indent, marker, name, count))
	if idx == t.cursor {
		header = t.m.styles.RowCursor.Render(header)
	}
	b.WriteString(dashboardLine(t.width, header))
	b.WriteString("\n")
	idx++
	if g.collapsed {
		return idx
	}
	if len(g.subgroups) > 0 {
		for _, sg := range g.subgroups {
			idx = t.renderGroup(b, sg, depth+1, idx)
		}
	} else {
		rowIndent := strings.Repeat("  ", depth+1)
		for _, r := range g.rows {
			labels := "(no labels)"
			if len(r.labels) > 0 {
				labels = strings.Join(r.labels, " ")
			}
			titleW := t.width - 6 - len(rowIndent)
			if titleW < 20 {
				titleW = 20
			}
			if titleW > 32 {
				titleW = 32
			}
			line := fmt.Sprintf("%s%s   id %s   labels %s   updated %s", rowIndent, truncateRunes(r.title, titleW), r.id, truncateRunes(labels, 36), r.updated)
			if idx == t.cursor {
				line = t.m.styles.RowCursor.Render(line)
			}
			b.WriteString(dashboardLine(t.width, line))
			b.WriteString("\n")
			idx++
		}
	}
	return idx
}

// groupLeafCount returns the total leaf rows reachable from a group, summing
// across nested sub-groups (expanded or not — collapse hides rows from the
// view but the header count still reflects the true bucket size).
func groupLeafCount(g taskGroup) int {
	if len(g.subgroups) > 0 {
		n := 0
		for _, sg := range g.subgroups {
			n += groupLeafCount(sg)
		}
		return n
	}
	return len(g.rows)
}

func (t *tasksModel) renderDetailView() string {
	if t.commentOverlay.id != "" {
		return t.commentOverlay.view(t.m)
	}
	if t.historyOverlay.active {
		return t.historyOverlay.view(t.m)
	}
	end := t.detail.offset + t.contentHeight
	if end > len(t.detail.lines) {
		end = len(t.detail.lines)
	}
	var b strings.Builder
	for i := t.detail.offset; i < end; i++ {
		b.WriteString(t.detail.lines[i])
		b.WriteString("\n")
	}
	return padToHeight(b.String(), t.contentHeight)
}

func (t *tasksModel) pageWindow(total int) (int, int) {
	return windowLines(total, t.cursor, t.pageSize)
}

// groupPageSize returns the number of lines that fit in the grouped/tree list
// body. Fixed overhead is 3 lines: the header line + blank line written by
// renderList, PLUS the "showing X of Y" footer renderGroupedList writes after
// the body. Reserving only 2 makes the body one line too tall, so padToHeight
// truncates the footer (and the pinned stack then renders where it would be).
// Called ONLY during render, where renderListWithStrip has already shrunk
// t.contentHeight to the list sub-height (listContentHeight), so
// t.contentHeight-3 == listH-3 here. The keypress side (listPageSize)
// reconstructs the same listH-3 from listContentHeight() directly, since at
// keypress time t.contentHeight is the full pane height.
func (t *tasksModel) groupPageSize() int {
	size := t.contentHeight - 3
	if size < 1 {
		size = 1
	}
	return size
}

// listPageSize returns the page size for whichever list mode is active, used by
// the pgdown / pgup page-jump keys. Both modes derive from listContentHeight()
// (the list sub-height) so a jump always lands on the exact page boundary the
// renderer draws: the flat body reserves 6 lines of chrome, the grouped body 3
// (header + blank + "showing" footer).
func (t *tasksModel) listPageSize() int {
	if t.grouped() {
		size := t.listContentHeight() - 3
		if size < 1 {
			size = 1
		}
		return size
	}
	size := t.listContentHeight() - 6
	if size < 1 {
		size = 1
	}
	return size
}

// shiftDigitToInt maps a shifted-digit key (US keyboard row: ! @ # $ % ^ & * ()
// to the 1-9 pin slot it jumps to. Returns 0 for anything else.
func shiftDigitToInt(k string) int {
	switch k {
	case "!":
		return 1
	case "@":
		return 2
	case "#":
		return 3
	case "$":
		return 4
	case "%":
		return 5
	case "^":
		return 6
	case "&":
		return 7
	case "*":
		return 8
	case "(":
		return 9
	}
	return 0
}

func (t *tasksModel) statusHint() string {
	if t.commentOverlay.id != "" {
		return "[H]istory   [Esc]back"
	}
	if t.historyOverlay.active {
		return "[Esc]back"
	}
	if t.m.projectScope == "" {
		return "[?]keys"
	}
	if t.view == tViewDetail {
		return "[e]title [d]desc [b]add label [B]remove label [M]comment [H]history [x]remove [Esc]back"
	}
	return "[↑/↓]tasks  [ [ / ] ]board  [s]ort  [a]dd  [p]pin/unpin  [Enter]detail  [>]inspect board  [?]keys"
}

// --- form openers ---

func (t *tasksModel) openCreateForm() {
	labelsValidator := func(field, value string) error {
		if value == "" {
			return nil
		}
		for _, tok := range strings.Fields(value) {
			if !labelSuffixRe.MatchString(tok) {
				return fmt.Errorf("bad label %q: use <namespace>:<value> or <tag>", tok)
			}
		}
		return nil
	}
	fields := []formField{
		{Label: "title", Required: true, Hint: "task title"},
		{Label: "description", Required: false, Hint: "optional; multi-line later"},
		{Label: "labels", Required: false, Hint: "space-separated suffixes, e.g. 'status:open type:bug' (prefix auto-added)", Validator: labelsValidator},
	}
	f := NewForm("New task  "+t.m.projectScope+":", fields)
	f.Title = "New task  " + t.m.projectScope + ":"
	t.m.form = f
	t.m.formKind = formTaskCreate
}

func (t *tasksModel) openTitleForm() {
	tk := t.detail.task
	if tk == nil {
		return
	}
	fields := []formField{
		{Label: "title", Required: true, Value: tk.Title, Hint: "new task title"},
	}
	f := NewForm("Edit title", fields)
	t.m.form = f
	t.m.formKind = formTaskSetTitle
}

func (t *tasksModel) openDescriptionForm() {
	tk := t.detail.task
	if tk == nil {
		return
	}
	fields := []formField{
		{Label: "description", Required: false, Value: tk.Description, Hint: "new description (empty clears)"},
	}
	f := NewForm("Edit description", fields)
	t.m.form = f
	t.m.formKind = formTaskSetDescription
}

func (t *tasksModel) openLabelAddForm() {
	tk := t.detail.task
	if tk == nil {
		return
	}
	validator := func(field, value string) error {
		if value == "" {
			return nil
		}
		if !labelSuffixRe.MatchString(value) {
			return fmt.Errorf("use <namespace>:<value> or <tag>, e.g. status:open")
		}
		return nil
	}
	fields := []formField{
		{Label: "name", Required: true, Hint: "<namespace>:<value> or <tag>", Validator: validator},
	}
	f := NewForm("Add label  "+t.m.projectScope+":", fields)
	f.Title = "Add label  " + t.m.projectScope + ":"
	t.m.form = f
	t.m.formKind = formTaskLabelAdd
}

func (t *tasksModel) openLabelRemoveForm() {
	tk := t.detail.task
	if tk == nil {
		return
	}
	validator := func(field, value string) error {
		if value == "" {
			return nil
		}
		if !labelSuffixRe.MatchString(value) {
			return fmt.Errorf("use <namespace>:<value> or <tag>")
		}
		return nil
	}
	fields := []formField{
		{Label: "name", Required: true, Hint: "<namespace>:<value> or <tag>", Validator: validator},
	}
	f := NewForm("Remove label  "+t.m.projectScope+":", fields)
	f.Title = "Remove label  " + t.m.projectScope + ":"
	t.m.form = f
	t.m.formKind = formTaskLabelRemove
}

func (t *tasksModel) requestRemoveTask() tea.Cmd {
	t.m.confirm = confirmRemoveTask
	t.m.confirmMsg = fmt.Sprintf("Remove task %s?", t.detail.id)
	t.m.confirmArg = "History is lost. Registry labels are unaffected."
	return nil
}

func (t *tasksModel) openCommentAddForm() {
	tk := t.detail.task
	if tk == nil {
		return
	}
	labelsValidator := func(field, value string) error {
		if value == "" {
			return nil
		}
		for _, tok := range strings.Fields(value) {
			if !labelSuffixRe.MatchString(tok) {
				return fmt.Errorf("bad label %q: use <namespace>:<value> or <tag>", tok)
			}
		}
		return nil
	}
	fields := []formField{
		{Label: "body", Required: true, Hint: "comment body (free-form prose)"},
		{Label: "labels", Required: false, Hint: "space-separated suffixes, e.g. 'comment:open-question' (prefix auto-added)", Validator: labelsValidator},
		{Label: "reply-to", Required: false, Hint: "optional comment id this replies to (same task)"},
	}
	f := NewForm("New comment  "+tk.ID+":", fields)
	f.Title = "New comment  " + tk.ID + ":"
	t.m.form = f
	t.m.formKind = formCommentAdd
}

func (t *tasksModel) openCommentOverlay(id string) tea.Cmd {
	c, err := t.m.store.GetComment(id)
	if err != nil {
		t.m.showToast("error: " + err.Error())
		return nil
	}
	t.commentOverlay = commentOverlayModel{id: id, comment: c}
	t.commentOverlay.render(t.m)
	return nil
}

func (t *tasksModel) openHistoryOverlay() tea.Cmd {
	tk := t.detail.task
	if tk == nil {
		return nil
	}
	t.historyOverlay = historyOverlayModel{active: true}
	t.historyOverlay.render(t.m, tk.ProjectCode, tk.ID)
	return nil
}

// --- mutations ---

func (m *Model) doTaskCreate(vals map[string]string) tea.Cmd {
	title := vals["title"]
	desc := vals["description"]
	var labels []string
	for _, tok := range strings.Fields(vals["labels"]) {
		labels = append(labels, m.projectScope+":"+tok)
	}
	tk, err := m.store.CreateTask(m.projectScope, title, desc, labels, m.actor)
	if err != nil {
		m.showToast("error: " + err.Error())
		return nil
	}
	m.refreshAll()
	if tk != nil {
		m.tasks.openDetail(tk.ID)
	}
	return nil
}

func (m *Model) doTaskSetTitle(vals map[string]string) tea.Cmd {
	id := m.tasks.detail.id
	title := vals["title"]
	if err := m.store.SetTitle(id, title, m.actor); err != nil {
		m.showToast("error: " + err.Error())
		return nil
	}
	m.refreshAll()
	m.tasks.openDetail(id)
	return nil
}

func (m *Model) doTaskSetDescription(vals map[string]string) tea.Cmd {
	id := m.tasks.detail.id
	desc := vals["description"]
	if err := m.store.SetDescription(id, desc, m.actor); err != nil {
		m.showToast("error: " + err.Error())
		return nil
	}
	m.refreshAll()
	m.tasks.openDetail(id)
	return nil
}

func (m *Model) doTaskLabelAdd(vals map[string]string) tea.Cmd {
	id := m.tasks.detail.id
	suffix := vals["name"]
	full := m.projectScope + ":" + suffix
	if err := m.store.TaskLabelAdd(id, full, m.actor); err != nil {
		m.showToast("error: " + err.Error())
		return nil
	}
	m.refreshAll()
	m.tasks.openDetail(id)
	return nil
}

func (m *Model) doTaskLabelRemove(vals map[string]string) tea.Cmd {
	id := m.tasks.detail.id
	suffix := vals["name"]
	full := m.projectScope + ":" + suffix
	if err := m.store.TaskLabelRemove(id, full, m.actor); err != nil {
		m.showToast("error: " + err.Error())
		return nil
	}
	m.refreshAll()
	m.tasks.openDetail(id)
	return nil
}
