package tui

import (
	"sort"
	"strings"
	"unicode"

	"atm/internal/core"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/muesli/reflow/wordwrap"
)

// spotlightModel is the `\` launcher: a curated drill-in tree over the menu
// entry table rather than one exhaustive list. The root offers the four
// groups plus the inline global views; Enter drills into a group (and, for a
// task, into that task's actions); Esc peels one layer at a time. Content
// never varies with pane focus — activating an entry replays its scope
// prelude before its key, so a cross-pane action works from anywhere without
// the user navigating there first. A per-task action replaces that prelude
// with a select-by-ID step, which is the same guarantee for a target the
// prelude could not name.
type spotlightModel struct {
	m         *Model
	open      bool
	level     spotLevel
	group     menuGroupID
	taskID    string // levelTaskActions target: the task every action acts on
	taskTitle string
	taskQuery string // the Task-group search Esc restores on the way back out
	query     string
	cursor    int
	rows      []spotRow
	focus     spotFocus // focusList / focusPreview as today
	offset    int       // preview scroll (focusPreview only)
	lines     []string  // preview content, one string per line
}

// spotLevel is which layer of the tree is on screen.
type spotLevel int

const (
	levelRoot        spotLevel = iota
	levelGroup                 // a static group's entries (Project, Board, Reference)
	levelTaskActions           // per-task actions for sm.taskID
)

type spotFocus int

const (
	focusList spotFocus = iota
	focusPreview
)

// spotRowKind is what a row stands for; it decides what Enter does with it
// and whether the cursor can land on it at all.
type spotRowKind int

const (
	rowGroup spotRowKind = iota
	rowEntry
	rowTask // one matched task in the Task group's search
	rowHint // non-selectable helper line
)

// spotRow is one line of the current level. Exactly one of group/entry/task
// is set, except for rowHint, which carries only its copy.
type spotRow struct {
	kind  spotRowKind
	group *menuGroup
	entry *menuEntry
	task  *core.Task
	text  string // rowHint copy
}

// label is the row's display text: a group's name, an entry's label, a task's
// title, or the hint copy itself.
func (r spotRow) label() string {
	switch r.kind {
	case rowGroup:
		if r.group != nil {
			return r.group.label
		}
	case rowEntry:
		if r.entry != nil {
			return r.entry.label
		}
	case rowTask:
		if r.task != nil {
			return r.task.Title
		}
	case rowHint:
		return r.text
	}
	return ""
}

// selectable reports whether the cursor may land on the row. Hint lines are
// copy, not choices, so navigation skips over them.
func (r spotRow) selectable() bool { return r.kind != rowHint }

// openSpotlight opens the launcher at its root: no group, no query, cursor on
// the first row, list focus.
func (sm *spotlightModel) openSpotlight() {
	sm.open = true
	sm.setLevel(levelRoot, groupNone)
}

// openAt reopens the launcher with the cursor restored to row. A row that no
// longer exists (or is not landable) falls back to the first selectable row.
//
// row is a ROOT row index: spotlightReturn is still a bare int, so a return
// recorded inside a group cannot describe the level it came from. Task 10
// replaces it with a snapshot; until then reopening always lands at the root.
func (sm *spotlightModel) openAt(row int) {
	sm.openSpotlight()
	if row >= 0 && row < len(sm.rows) && sm.rows[row].selectable() {
		sm.cursor = row
		sm.refreshPreview()
	}
}

// setLevel is the single place a level transition happens: every push and
// every pop resets the query, rehomes the cursor, returns focus to the list,
// and rebuilds. Keeping the reset discipline here is what stops a new
// transition from forgetting half of it. A level other than levelTaskActions
// also drops the task the action level was targeting, and the saved search it
// was reached from — a caller that means to restore that search (escPeel)
// reads it into a local before transitioning, so the reset here stays
// unconditional rather than growing an exception.
func (sm *spotlightModel) setLevel(l spotLevel, g menuGroupID) {
	sm.level = l
	sm.group = g
	if l != levelTaskActions {
		sm.taskID = ""
		sm.taskTitle = ""
		sm.taskQuery = ""
	}
	sm.query = ""
	sm.cursor = 0
	sm.focus = focusList
	sm.buildRows()
	sm.refreshPreview()
}

// buildRows derives the rows for the current (level, group, query). A
// non-empty query at a searchable level replaces the tree entirely with the
// flat, ranked match list (buildSearchRows); otherwise the level's tree is
// built as before. Keeping the two branches separate is what lets the tree
// shape stay exactly as Tasks 5/6 left it — filtering never reaches into it.
func (sm *spotlightModel) buildRows() {
	sm.rows = nil
	if sm.filtering() {
		sm.buildSearchRows()
		sm.clampCursor()
		return
	}
	switch sm.level {
	case levelRoot:
		for i := range menuGroups {
			sm.rows = append(sm.rows, spotRow{kind: rowGroup, group: &menuGroups[i]})
		}
		// The global views render inline at the root: they belong to no group
		// (groupNone) and are one keystroke away from the launcher's entry.
		for i := range menuEntries {
			e := &menuEntries[i]
			if e.section != sectionViews || !sm.entryAvailable(e) {
				continue
			}
			sm.rows = append(sm.rows, spotRow{kind: rowEntry, entry: e})
		}
	case levelGroup:
		for i := range menuEntries {
			e := &menuEntries[i]
			if e.group != sm.group || !sm.entryAvailable(e) {
				continue
			}
			// Per-task actions act on one open task, not on the Task group as
			// a whole: they render at levelTaskActions.
			if sm.group == groupTask && isTaskAction(e) {
				continue
			}
			sm.rows = append(sm.rows, spotRow{kind: rowEntry, entry: e})
		}
		if sm.group == groupTask {
			sm.appendTaskRows()
		}
	case levelTaskActions:
		// The per-task actions for sm.taskID: the group's entries that act on
		// one open task, in table order. They are the rows the levelGroup
		// branch above deliberately skips.
		for i := range menuEntries {
			e := &menuEntries[i]
			if e.group != groupTask || !isTaskAction(e) || !sm.entryAvailable(e) {
				continue
			}
			sm.rows = append(sm.rows, spotRow{kind: rowEntry, entry: e})
		}
	}
	sm.clampCursor()
}

// appendTaskRows is the Task group's contextual half: the live search over the
// scoped project's tasks that follows the static Add-task row. Every state
// ends in either task rows or exactly one hint, so the group can never look
// like it lost its rows — and the hint says which state it is in.
func (sm *spotlightModel) appendTaskRows() {
	if sm.m.projectScope == "" || sm.query == "" {
		sm.rows = append(sm.rows, spotRow{kind: rowHint, text: sm.taskHint()})
		return
	}
	hits := sm.taskMatches(sm.query)
	if len(hits) == 0 {
		sm.rows = append(sm.rows, spotRow{kind: rowHint, text: "no tasks match"})
		return
	}
	for _, tk := range hits {
		sm.rows = append(sm.rows, spotRow{kind: rowTask, task: tk})
	}
}

// spotTaskMatches caps the Task group's result list. The rows share the left
// pane with the static Add-task entry and the launcher is a launcher, not a
// task list: five is enough to recognise the task you meant and short enough
// that refining the query is the obvious next move.
const spotTaskMatches = 5

// taskMatches is the Task group's search: the scoped project's tasks whose ID
// or title contains q, case-insensitively, ID matches ranked first — a user
// who pastes an ID means that one task. Substring only: no fuzzy matching, no
// recency, and never across projects (an unscoped launcher has nothing to
// search, which is what the group's hint says).
func (sm *spotlightModel) taskMatches(q string) []*core.Task {
	q = strings.ToLower(strings.TrimSpace(q))
	if q == "" || sm.m.projectScope == "" {
		return nil
	}
	type hit struct {
		task *core.Task
		rank int
	}
	var hits []hit
	for _, tk := range sm.m.store.ListTasks(core.QueryFilters{Project: sm.m.projectScope}) {
		switch {
		case strings.Contains(strings.ToLower(tk.ID), q):
			hits = append(hits, hit{tk, 0})
		case strings.Contains(strings.ToLower(tk.Title), q):
			hits = append(hits, hit{tk, 1})
		}
	}
	// Stable over a store-ordered list, so ties inside a rank keep the order
	// the Tasks pane would list them in.
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].rank < hits[j].rank })
	if len(hits) > spotTaskMatches {
		hits = hits[:spotTaskMatches]
	}
	out := make([]*core.Task, 0, len(hits))
	for _, h := range hits {
		out = append(out, h.task)
	}
	return out
}

// entryAvailable reports whether an entry may become a row at all: hidden
// entries are keymap-reference documentation, and the capabilities switcher
// is only reachable when a project scope exists (advertising it otherwise
// would replay into a no-op).
func (sm *spotlightModel) entryAvailable(e *menuEntry) bool {
	if e.hidden {
		return false
	}
	return !e.needsProject || sm.m.projectScope != ""
}

// isTaskAction reports whether e acts on one open task rather than on the
// Task group as a whole.
func isTaskAction(e *menuEntry) bool {
	return len(e.scopes) > 0 && e.scopes[0] == scopeTasksDetail
}

// taskHint is the Task group's helper line: the group is a search surface,
// so it tells the user what to type — or why they cannot yet.
func (sm *spotlightModel) taskHint() string {
	if sm.m.projectScope == "" {
		return "select a project first"
	}
	return "type to find a task…"
}

// searchable reports whether the current level supports type-to-filter.
// levelGroup(groupTask) is excluded on purpose: there the query searches
// tasks, not registry entries — appendTaskRows owns that surface.
func (sm *spotlightModel) searchable() bool {
	switch sm.level {
	case levelRoot, levelTaskActions:
		return true
	case levelGroup:
		return sm.group != groupTask
	}
	return false
}

// filtering reports whether a non-empty query at the current level should
// replace the tree with the flat match list. Derived rather than cached, so
// it can never go stale against sm.query or a level/group change.
func (sm *spotlightModel) filtering() bool {
	return sm.query != "" && sm.searchable()
}

// searchCandidates is the entry set a query is matched against: every
// non-hidden, currently-available entry in the registry at levelRoot and
// levelGroup ("registry-wide" — search reaches entries outside the group the
// user drilled into), or just the open task's action entries at
// levelTaskActions ("the current action list"). entryAvailable keeps the
// Capabilities project gate in force under search exactly as it is in the
// tree.
func (sm *spotlightModel) searchCandidates() []*menuEntry {
	var out []*menuEntry
	for i := range menuEntries {
		e := &menuEntries[i]
		if e.hidden || !sm.entryAvailable(e) {
			continue
		}
		if sm.level == levelTaskActions {
			if e.group == groupTask && isTaskAction(e) {
				out = append(out, e)
			}
			continue
		}
		out = append(out, e)
	}
	return out
}

// buildSearchRows replaces the tree with a flat, ranked list of matching
// entries: matchRank first, then table order within a rank (sort.SliceStable
// over table-ordered candidates is what makes ties keep table order). A
// query matching nothing is not an empty list — it is one rowHint saying so,
// which the cursor cannot land on.
func (sm *spotlightModel) buildSearchRows() {
	type hit struct {
		entry *menuEntry
		rank  int
	}
	var hits []hit
	for _, e := range sm.searchCandidates() {
		if rank := matchRank(*e, sm.query); rank >= 0 {
			hits = append(hits, hit{e, rank})
		}
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].rank < hits[j].rank })
	for _, h := range hits {
		sm.rows = append(sm.rows, spotRow{kind: rowEntry, entry: h.entry})
	}
	if len(sm.rows) == 0 {
		sm.rows = append(sm.rows, spotRow{kind: rowHint, text: "no matches"})
	}
}

// matchRank scores e against query q, case-insensitively: 0 when q prefixes
// the label, 1 when q appears anywhere else in the label, 2 when q appears
// only in the summary, -1 for no match at all. No fuzzy matching, no
// recency, no usage-frequency — the three tiers are the whole ranking.
func matchRank(e menuEntry, q string) int {
	q = strings.ToLower(q)
	label := strings.ToLower(e.label)
	if strings.HasPrefix(label, q) {
		return 0
	}
	if strings.Contains(label, q) {
		return 1
	}
	if strings.Contains(strings.ToLower(e.summary), q) {
		return 2
	}
	return -1
}

// searchLabel is a search row's display text: "Group · Label" for an entry
// filed under a real group, or the bare label for a groupNone (root) entry,
// which has no group name to prefix.
func searchLabel(e menuEntry) string {
	if g := groupByID(e.group); g != nil {
		return g.label + " · " + e.label
	}
	return e.label
}

// clampCursor keeps the cursor on a landable row after a rebuild: a hint line
// is never landable, and a shrinking row set (a typed query) must not leave
// the cursor past the end.
func (sm *spotlightModel) clampCursor() {
	if sm.cursor < 0 || sm.cursor >= len(sm.rows) || !sm.rows[sm.cursor].selectable() {
		sm.cursor = sm.firstSelectableRow()
	}
}

// firstSelectableRow is the first landable row, or -1 when none of the
// current rows are selectable — a query matching nothing leaves exactly one
// rowHint ("no matches") and nothing else, which is exactly this case. -1 is
// a real "no selection" state, not a placeholder: clampCursor stores it as
// sm.cursor, moveCursor's bounds check leaves it alone (i := -1 + step never
// re-enters the row range), selectedRow() already treats any cursor < 0 as
// no selection, and the renderer's `row == sm.cursor` cursor-glyph check
// never matches a real row index against it — so no glyph is drawn anywhere.
func (sm *spotlightModel) firstSelectableRow() int {
	for i, r := range sm.rows {
		if r.selectable() {
			return i
		}
	}
	return -1
}

// moveCursor steps one landable row in dir, skipping hint lines and stopping
// at the ends of the list.
func (sm *spotlightModel) moveCursor(dir int) {
	step := 1
	if dir < 0 {
		step = -1
	}
	for i := sm.cursor + step; i >= 0 && i < len(sm.rows); i += step {
		if sm.rows[i].selectable() {
			sm.cursor = i
			return
		}
	}
}

// selectedRow is the row under the cursor, or nil when there is none.
func (sm *spotlightModel) selectedRow() *spotRow {
	if sm.cursor < 0 || sm.cursor >= len(sm.rows) {
		return nil
	}
	r := &sm.rows[sm.cursor]
	if !r.selectable() {
		return nil
	}
	return r
}

// selectedEntry is the menu entry under the cursor, or nil when the cursor is
// on a group, task, or hint row.
func (sm *spotlightModel) selectedEntry() *menuEntry {
	if r := sm.selectedRow(); r != nil {
		return r.entry
	}
	return nil
}

// selectedLabel is the display text of the row under the cursor.
func (sm *spotlightModel) selectedLabel() string {
	if r := sm.selectedRow(); r != nil {
		return r.label()
	}
	return ""
}

// handleKey routes the launcher's keys. The key column the list renders is
// documentation of the real TUI binding, never an accelerator: inside the
// launcher every printable key types into the query. `\` is the one
// exception — the key that opens the launcher also closes it, from any level.
func (sm *spotlightModel) handleKey(k tea.KeyMsg) tea.Cmd {
	if k.String() == "\\" {
		sm.open = false
		return nil
	}
	if sm.focus == focusPreview {
		sm.handlePreviewKey(k)
		return nil
	}
	switch k.String() {
	case "down":
		sm.moveCursor(1)
		sm.refreshPreview()
	case "up":
		sm.moveCursor(-1)
		sm.refreshPreview()
	case "enter":
		return sm.activate()
	case "tab":
		// An empty preview is not worth focusing: Tab would strand the user in
		// a pane with nothing to scroll, where every key but Tab/Esc/`\` does
		// nothing. The pane says "(no preview)" rather than going blank, so
		// the ignored Tab reads as "nothing to see" and not as a dead key.
		if len(sm.lines) > 0 {
			sm.focus = focusPreview
		}
	case "pgup":
		sm.scrollPreview(-sm.previewHeight())
	case "pgdown":
		sm.scrollPreview(sm.previewHeight())
	case "esc":
		sm.escPeel()
	case "backspace":
		if r := []rune(sm.query); len(r) > 0 {
			sm.setQuery(string(r[:len(r)-1]))
		}
	default:
		if r, ok := printableRune(k); ok {
			sm.setQuery(sm.query + string(r))
		}
	}
	return nil
}

// handlePreviewKey is the focused-preview mode: the arrows and j/k scroll a
// line, PgUp/PgDn a screenful (both advertised in the footer); Tab and Esc —
// the only two exits — hand focus back to the list. Printable keys do not type
// here: the query belongs to the list.
func (sm *spotlightModel) handlePreviewKey(k tea.KeyMsg) {
	switch k.String() {
	case "j", "down":
		sm.scrollPreview(1)
	case "k", "up":
		sm.scrollPreview(-1)
	case "pgdown":
		sm.scrollPreview(sm.previewHeight())
	case "pgup":
		sm.scrollPreview(-sm.previewHeight())
	case "tab", "esc":
		sm.escPeel()
	}
}

// printableRune is the single-rune key a printable keypress carries, or
// ok=false for anything that is not typed text. Space arrives as its own key
// type (with empty Runes), so it is matched explicitly.
func printableRune(k tea.KeyMsg) (rune, bool) {
	if k.Alt {
		return 0, false
	}
	if k.Type == tea.KeySpace {
		return ' ', true
	}
	if k.Type != tea.KeyRunes || len(k.Runes) != 1 || !unicode.IsPrint(k.Runes[0]) {
		return 0, false
	}
	return k.Runes[0], true
}

// setQuery replaces the query and rebuilds around it, then rehomes the
// cursor: a changed match set makes wherever the old cursor pointed
// meaningless, and clearing the query must land back on the tree's first
// selectable row exactly.
func (sm *spotlightModel) setQuery(q string) {
	sm.query = q
	sm.cursor = 0
	sm.buildRows()
	sm.cursor = sm.queryHome()
	sm.clampCursor()
	sm.refreshPreview()
}

// queryHome is the row a query-driven rebuild selects. Row 0 everywhere the
// list is nothing but matches (clampCursor bumps off it when it is a hint) —
// but the Task group's row 0 is the static Add-task entry, which no query
// ever matches, so a task search homes onto its top result instead. Homing on
// Add task there would mean every keystroke selected (and previewed)
// something unrelated to what the user was typing, and undid their arrow keys
// on the very next character.
func (sm *spotlightModel) queryHome() int {
	for i, r := range sm.rows {
		if r.kind == rowTask {
			return i
		}
	}
	return 0
}

// escPeel unwinds exactly one layer per Esc, most recent first: a focused
// preview, then a typed query, then the task-action level, then the group
// level. Only a bare root closes the launcher — so a user who drilled in and
// typed is never thrown all the way out by one keypress.
//
// Leaving the task-action level restores the search the task was found by, so
// the user lands back on their results rather than on an empty group. The
// save/restore is explicit here rather than inside setLevel: the transition
// point keeps its one meaning (reset everything), and the one rung that owes
// the user something back says so where it happens. The next Esc then peels
// the restored query, exactly as if it had never been left.
func (sm *spotlightModel) escPeel() {
	switch {
	case sm.focus == focusPreview:
		sm.focus = focusList
		sm.offset = 0
	case sm.query != "":
		sm.setQuery("")
	case sm.level == levelTaskActions:
		q := sm.taskQuery // setLevel clears it; read it first
		sm.setLevel(levelGroup, groupTask)
		if q != "" {
			sm.setQuery(q)
		}
	case sm.level == levelGroup:
		sm.setLevel(levelRoot, groupNone)
	default:
		sm.open = false
	}
}

// activate is Enter: a group row drills in, an entry row runs (or focuses its
// reference preview), a task row drills into that task's actions.
func (sm *spotlightModel) activate() tea.Cmd {
	r := sm.selectedRow()
	if r == nil {
		return nil
	}
	switch r.kind {
	case rowGroup:
		if r.group != nil {
			sm.setLevel(levelGroup, r.group.id)
		}
	case rowEntry:
		if r.entry != nil {
			return sm.activateEntry(r.entry)
		}
	case rowTask:
		if r.task != nil {
			// Recorded before setLevel, which builds the action rows around
			// them; setLevel keeps the target precisely because the level it
			// is entering is the one that acts on it. The search that found
			// the task is saved with it: Esc owes the user their results
			// back, not an empty group to retype into.
			sm.taskID, sm.taskTitle, sm.taskQuery = r.task.ID, r.task.Title, sm.query
			sm.setLevel(levelTaskActions, groupTask)
		}
	}
	return nil
}

// activateEntry runs one menu entry: a reference entry focuses its preview
// for scrolling; anything else closes the launcher and replays prelude + key
// through Model.handleKey, so the launcher and the raw keypress share one
// path.
func (sm *spotlightModel) activateEntry(e *menuEntry) tea.Cmd {
	if e.kind == kindReference {
		sm.focus = focusPreview
		sm.offset = 0
		return nil
	}
	if sm.level == levelTaskActions {
		return sm.activateTaskAction(e)
	}
	row := sm.cursor
	sm.open = false
	var chain []string
	if len(e.scopes) > 0 {
		chain = preludeFor(e.scopes[0])
	}
	chain = append(append([]string{}, chain...), e.key)
	var cmds []tea.Cmd
	for _, seg := range chain {
		if c := sm.m.handleKey(keyMsgFromString(seg)); c != nil {
			cmds = append(cmds, c)
		}
	}
	if e.kind == kindDialog {
		// Set only after the replay loop above finishes, so spotlightReturn
		// stays -1 for every segment replayed through m.handleKey — the
		// wrapper's check (app.go's handleKey) cannot fire mid-replay and
		// cannot mistake the spawned overlay's own opening key for a
		// dismissal. But this call itself runs beneath the outer handleKey
		// invocation for the user's Enter keypress: once that outer call's
		// dispatchKey (this whole activate()) returns, its own wrapper check
		// sees spotlightReturn freshly set here and fires immediately — the
		// reopen happens within this same keystroke, not on the user's next
		// one. That is exactly right when the chain opened a real
		// overlay/form/confirm (workspaceIdle() is false, so the check
		// doesn't fire yet); it is a bug if the chain's kindDialog entry
		// opens something workspaceIdle() does not gate on (an in-pane view),
		// which is why every kindDialog entry must leave one of
		// workspaceIdle's eight states open.
		sm.m.spotlightReturn = row
	}
	return tea.Batch(cmds...)
}

// activateTaskAction runs one per-task action against sm.taskID. Where a
// static entry replays its scope prelude, a per-task action cannot: the
// scopeTasksDetail prelude ({"2", "enter"}) opens whatever the Tasks list
// cursor happens to be on, which is exactly the task the user did NOT pick.
// Selecting the target by ID instead — focus the pane, open that task's
// detail, then replay the key — is what makes the action independent of the
// pane, the list cursor, and any board filter hiding the task from the list.
//
// The launcher's rows are a snapshot of a store another process can write to,
// so the target is re-read first: a task removed between the search and the
// Enter is reported, not replayed against.
func (sm *spotlightModel) activateTaskAction(e *menuEntry) tea.Cmd {
	m := sm.m
	if _, err := m.store.GetTask(sm.taskID); err != nil {
		m.showToast("task " + sm.taskID + " is gone")
		sm.buildRows()
		sm.refreshPreview()
		return nil
	}
	row := sm.cursor
	sm.open = false
	m.focused = paneTasks
	var cmds []tea.Cmd
	if c := m.tasks.openDetail(sm.taskID); c != nil {
		cmds = append(cmds, c)
	}
	if c := m.handleKey(keyMsgFromString(e.key)); c != nil {
		cmds = append(cmds, c)
	}
	// Same ordering rule as activateEntry: the return is recorded only after
	// the replay, so the wrapper cannot fire mid-chain.
	if e.kind == kindDialog {
		m.spotlightReturn = row
	}
	return tea.Batch(cmds...)
}

// refreshPreview rebuilds the preview content for the cursor row: a group row
// gets its contents (the hint plus one line per member entry); a task row gets
// that task itself, history included; a reference entry gets its full text
// (falling back to the summary if its refKind is somehow unrecognized);
// anything else tries the live renderer registry, falling back to the summary
// line when no renderer is registered or the registry renderer produced
// nothing. Rebuilding from scratch (rather than caching) is what keeps this
// safe to call again on resize — SetSize calls it whenever the spotlight is
// open, so a stale wrap never survives a terminal resize.
func (sm *spotlightModel) refreshPreview() {
	sm.offset = 0
	sm.lines = nil
	r := sm.selectedRow()
	if r == nil {
		return
	}
	w, h := sm.previewWidth(), sm.previewHeight()
	switch r.kind {
	case rowGroup:
		sm.lines = groupPreviewLines(r.group, w)
		return
	case rowTask:
		// The task preview is the launcher's replacement for the deleted
		// task-detail history overlay: hovering a result is how a task's
		// history is read now.
		sm.lines = taskPreviewLines(sm.m, r.task, w)
		return
	}
	e := r.entry
	if e == nil {
		return
	}
	if e.kind == kindReference {
		if text := sm.referenceText(e.ref, w); text != "" {
			sm.lines = strings.Split(strings.TrimRight(text, "\n"), "\n")
			return
		}
	} else if fn, ok := previewRegistry[previewKeyFor(*e)]; ok {
		if out := fn(sm.m, w, h); strings.TrimSpace(out) != "" {
			sm.lines = strings.Split(strings.TrimRight(out, "\n"), "\n")
			return
		}
	}
	sm.lines = strings.Split(wordwrap.String(e.summary, w), "\n")
}

// referenceText returns kind's full reference content at width w, or "" for
// an unrecognized kind so refreshPreview falls through to the summary line
// instead of showing a blank preview. Lifted from the deleted openDetail:
// refreshPreview rebuilds it fresh on every call (including on resize),
// preserving openDetail's old guarantee that content is built at drill/hover
// time rather than cached against a width that can go stale.
func (sm *spotlightModel) referenceText(kind refKind, w int) string {
	switch kind {
	case refKeymap:
		return keymapReferenceText()
	case refParity:
		return parityTable
	case refConventions:
		return renderConventionsText(sm.m.styles, w, conventionsTextTUI)
	}
	return ""
}

// maxPreviewOffset is the highest scroll offset that still fills the preview
// region: enough to reach the end of sm.lines, but never past it, so the
// last screenful stays full instead of shrinking toward a single remaining
// line.
func (sm *spotlightModel) maxPreviewOffset() int {
	top := len(sm.lines) - sm.previewHeight()
	if top < 0 {
		top = 0
	}
	return top
}

// clampOffset keeps the preview scroll inside its content. Every scroll goes
// through it, and so does the renderer, so a stale offset (a resize that
// re-wrapped sm.lines shorter, say) can never survive to be drawn.
func (sm *spotlightModel) clampOffset() {
	if top := sm.maxPreviewOffset(); sm.offset > top {
		sm.offset = top
	}
	if sm.offset < 0 {
		sm.offset = 0
	}
}

// scrollPreview moves the preview window by delta lines. PgUp/PgDn reach it
// from either focus: the preview is on screen whether or not it owns the
// keystrokes, so paging it from the list is useful and costs the list nothing
// (neither key types or navigates there).
func (sm *spotlightModel) scrollPreview(delta int) {
	sm.offset += delta
	sm.clampOffset()
}
