package tui

import (
	"sort"
	"strings"
	"time"
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

	// The content search's state. hits is what store.Search last returned for
	// searchQuery, grouped into rows; searchGen is bumped by every query change
	// so a debounce tick that lands after a newer keystroke can be recognised
	// and dropped. Holding the previous hits while a new search is in flight is
	// deliberate: blanking them makes the list flash empty between keystrokes.
	hits        []spotRow
	searchQuery string // the query sm.hits were produced for
	searchGen   int
	searching   bool   // a debounce tick is in flight
	searchErr   string // the last store error, surfaced as a row rather than swallowed

	// ask is the spotAsk level's whole state, non-nil exactly while level is
	// levelAsk. It lives behind a pointer in its own file because the level owns a
	// streaming goroutine, a transcript and an input of its own -- none of which
	// the tree levels have any use for.
	ask *askPane
}

// spotLevel is which layer of the tree is on screen.
type spotLevel int

const (
	levelRoot        spotLevel = iota
	levelGroup                 // a static group's entries (Project, Board, Reference)
	levelTaskActions           // per-task actions for sm.taskID
	levelAsk                   // the conversational answer level (ATM-f71b81)
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
	rowTask    // one matched task, from the store's search
	rowComment // one matched comment, indented under the task that owns it
	rowSection // non-selectable section label ("TASKS · COMMENTS")
	rowHint    // non-selectable helper line
)

// spotContentSection labels the store-backed half of the list. The ACTIONS
// header is drawn by the renderer above the rows; this one is a row, because
// it sits in the middle of them.
const spotContentSection = "TASKS · COMMENTS"

// spotRow is one line of the current level. Exactly one of group/entry/task is
// set, except for rowHint and rowSection, which carry only their copy — and
// rowComment, which carries the comment AND the task it belongs to, because
// Enter on a comment drills into its task's actions.
type spotRow struct {
	kind    spotRowKind
	group   *menuGroup
	entry   *menuEntry
	task    *core.Task
	comment *core.Comment
	text    string // rowHint/rowSection copy, and a comment row's snippet
}

// label is the row's display text: a group's name, an entry's label, a task's
// title, a comment's snippet, or the hint/section copy itself.
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
	case rowComment:
		return r.text
	case rowSection, rowHint:
		return r.text
	}
	return ""
}

// selectable reports whether the cursor may land on the row. Hint lines and
// section labels are copy, not choices, so navigation skips over them.
func (r spotRow) selectable() bool { return r.kind != rowHint && r.kind != rowSection }

// openSpotlight opens the launcher at its root: no group, no query, cursor on
// the first row, list focus.
func (sm *spotlightModel) openSpotlight() {
	sm.open = true
	sm.setLevel(levelRoot, groupNone)
}

// spotlightSnapshot is where the launcher was standing when a kindDialog entry
// closed it: everything needed to put the user back exactly there. A bare row
// index could only ever describe the root — a row number means something else
// one level down, and nothing at all on a per-task action list.
type spotlightSnapshot struct {
	level     spotLevel
	group     menuGroupID
	taskID    string
	taskTitle string // carried with the snapshot for completeness; not currently rendered (the breadcrumb uses taskID, per spec)
	taskQuery string
	query     string
	cursor    int
}

// snapshot records the current position. Taken before the launcher closes:
// closing is what the user is coming back from, and the replay that follows it
// may touch the very state being recorded.
func (sm *spotlightModel) snapshot() spotlightSnapshot {
	return spotlightSnapshot{
		level:     sm.level,
		group:     sm.group,
		taskID:    sm.taskID,
		taskTitle: sm.taskTitle,
		taskQuery: sm.taskQuery,
		query:     sm.query,
		cursor:    sm.cursor,
	}
}

// openAt reopens the launcher where s was taken: same level, same group, same
// task, same query, and the cursor back on the row that was activated. The
// rows themselves are always rebuilt, never restored — the snapshot describes
// a position, and the store behind it may have changed while the dialog was
// open.
//
// The cursor is the one part a content search overrides. A Task-group snapshot
// carrying a query re-runs that search, and the tick that lands homes onto the
// first result exactly as a keystroke's rebuild does (applySearchTick's
// queryHome), so Add task → form → Esc comes back to the top result rather than
// to the Add-task row the form was activated from. That is the rule every other
// query-driven rebuild follows; the alternative — holding a row index against
// results the store may have changed meanwhile — would point at the wrong task
// as often as the right one.
//
// The level is resolved against the world as it is now (restorableLevel), so a
// snapshot whose target is gone lands on the nearest level that can still be
// built. A fallback drops the query and the cursor with the level they
// belonged to — both describe a list this level is not showing — except for
// the task search, which describes the Task group's list exactly (see below).
func (sm *spotlightModel) openAt(s spotlightSnapshot) tea.Cmd {
	sm.open = true
	level, group := sm.restorableLevel(s)
	if level == levelTaskActions {
		// Set before the transition for the same reason activate does it:
		// setLevel builds the action rows around the target, and keeps both
		// the target and its search precisely because levelTaskActions is the
		// level that acts on them. Everything else setLevel resets stays reset
		// — the state openAt wants preserved is restored around the
		// transition, never excepted inside it.
		sm.taskID, sm.taskTitle, sm.taskQuery = s.taskID, s.taskTitle, s.taskQuery
	}
	sm.setLevel(level, group)
	if level != s.level || group != s.group {
		// The one thing a fallback inherits. Dropping out of a vanished task's
		// actions lands on the Task group, and taskQuery describes exactly the
		// list that level shows — it is the search the task was found by, so
		// the user gets their (now shorter) results rather than an empty group
		// to retype into after a step they never took. Every other fallback
		// ends at the root, where neither query nor cursor means anything.
		if s.level == levelTaskActions && level == levelGroup && s.taskQuery != "" {
			return sm.setQuery(s.taskQuery)
		}
		return nil
	}
	sm.query = s.query
	sm.cursor = s.cursor
	// The restored query owes the user its rows just as a typed one does —
	// otherwise a reopened launcher shows a query with nothing under it until
	// the next keystroke.
	cmd := sm.scheduleSearch()
	// clampCursor (inside buildRows) is what makes a stale cursor safe: an
	// out-of-range index or one landing on a hint rehomes to the first
	// selectable row, and to the -1 no-selection sentinel when the restored
	// query now matches nothing.
	sm.buildRows()
	sm.refreshPreview()
	return cmd
}

// restorableLevel is the level a snapshot can still be restored to, with the
// group it belongs at. A stale snapshot is ordinary rather than exceptional:
// the dialog the launcher spawned may have removed the very thing the launcher
// was standing on, and another process writing the store may have too. Each
// case falls back one level to the nearest surviving one.
func (sm *spotlightModel) restorableLevel(s spotlightSnapshot) (spotLevel, menuGroupID) {
	switch s.level {
	case levelTaskActions:
		// An action list for a task that no longer exists has nothing to act
		// on; one level out is the Task group it was reached through.
		if sm.taskAlive(s.taskID) {
			return levelTaskActions, groupTask
		}
		return levelGroup, groupTask
	case levelGroup:
		if groupByID(s.group) != nil {
			return levelGroup, s.group
		}
	}
	return levelRoot, groupNone
}

// taskAlive reports whether id is still a task in the store — the same
// re-read activateTaskAction does before replaying against a target, for the
// same reason: the launcher's rows are a snapshot of a store another process
// can write to.
func (sm *spotlightModel) taskAlive(id string) bool {
	if id == "" {
		return false
	}
	_, err := sm.m.store.GetTask(id)
	return err == nil
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
	// Retires any in-flight content search: the level it belonged to is gone.
	// Always nil (the query is empty), so nothing is dropped by ignoring it.
	sm.scheduleSearch()
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
			sm.appendContentRows()
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

// appendContentRows is the Task group's contextual half: the store-backed
// search results that follow the static Add-task row. Every state ends in
// either content rows or exactly one hint, so the group can never look like it
// lost its rows — and the hint says which state it is in.
//
// "Empty query" is trimmed here because space is a real keystroke inside the
// launcher: without the trim, one space replaces the invitation to type with
// "no tasks match".
//
// The previous hits survive an in-flight search on purpose. Blanking them on
// every keystroke made the list flash empty through the debounce; holding them
// means the rows only ever change to newer rows.
func (sm *spotlightModel) appendContentRows() {
	if sm.m.projectScope == "" || strings.TrimSpace(sm.query) == "" {
		sm.rows = append(sm.rows, spotRow{kind: rowHint, text: sm.taskHint()})
		return
	}
	if sm.searchErr != "" {
		sm.rows = append(sm.rows, spotRow{kind: rowHint, text: "search error: " + sm.searchErr})
		return
	}
	if len(sm.hits) == 0 {
		// Nothing yet vs nothing at all: only a landed search for THIS query
		// can say "no tasks match".
		if sm.searching || sm.searchQuery != strings.TrimSpace(sm.query) {
			sm.rows = append(sm.rows, spotRow{kind: rowHint, text: "searching…"})
			return
		}
		sm.rows = append(sm.rows, spotRow{kind: rowHint, text: "no tasks match"})
		return
	}
	// The header labels a section that exists; the empty and no-match states
	// say enough on their own.
	sm.rows = append(sm.rows, spotRow{kind: rowSection, text: spotContentSection})
	sm.rows = append(sm.rows, sm.hits...)
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
// tasks, not registry entries — appendContentRows owns that surface.
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

// selectedLabel is the display text of the row under the cursor.
func (sm *spotlightModel) selectedLabel() string {
	if r := sm.selectedRow(); r != nil {
		return r.label()
	}
	return ""
}

// handleKey routes the launcher's keys. The key column the list renders is
// documentation of the real TUI binding, never an accelerator: inside the
// launcher every printable key types into the query. `\` is the one exception
// — the key that opens the launcher also closes it.
//
// From any level of the TREE, that is. The ask level forks above the `\` case
// on purpose: it has a text input of its own, and a backslash typed into a
// question is a character, not a command. Esc is the exit there, and it peels
// back to the list this closes from.
func (sm *spotlightModel) handleKey(k tea.KeyMsg) tea.Cmd {
	if sm.level == levelAsk && sm.ask != nil {
		return sm.ask.handleKey(k)
	}
	if k.String() == "\\" {
		sm.open = false
		return nil
	}
	if sm.focus == focusPreview {
		return sm.handlePreviewKey(k)
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
		// Sub-task 1 pinned the Ask row and left it hintless because this key
		// focused the preview; it named this task as the owner of the rebinding
		// (spotlight_render.go). Preview focus moves to the arrow that points at
		// the pane it focuses, which reads better than tab ever did.
		return sm.enterAsk()
	case "right":
		// An empty preview is not worth focusing: the user would be stranded in a
		// pane with nothing to scroll, where every key but left/esc does nothing.
		if len(sm.lines) > 0 {
			sm.focus = focusPreview
		}
	// No "left" case: focusPreview is routed to handlePreviewKey above this
	// switch, so a left arm guarded on that focus could never run. Left out of
	// a focused preview is handlePreviewKey's, alongside esc.
	case "pgup":
		sm.scrollPreview(-sm.previewHeight())
	case "pgdown":
		sm.scrollPreview(sm.previewHeight())
	case "esc":
		return sm.escPeel()
	case "backspace":
		if r := []rune(sm.query); len(r) > 0 {
			return sm.setQuery(string(r[:len(r)-1]))
		}
	default:
		if r, ok := printableRune(k); ok {
			return sm.setQuery(sm.query + string(r))
		}
	}
	return nil
}

// handlePreviewKey is the focused-preview mode: the arrows and j/k scroll a
// line, PgUp/PgDn a screenful (both advertised in the footer); Left and Esc —
// the only two exits — hand focus back to the list. Printable keys do not type
// here: the query belongs to the list.
func (sm *spotlightModel) handlePreviewKey(k tea.KeyMsg) tea.Cmd {
	switch k.String() {
	case "j", "down":
		sm.scrollPreview(1)
	case "k", "up":
		sm.scrollPreview(-1)
	case "pgdown":
		sm.scrollPreview(sm.previewHeight())
	case "pgup":
		sm.scrollPreview(-sm.previewHeight())
	case "left", "esc":
		return sm.escPeel()
	}
	return nil
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
// selectable row exactly. Scheduling the search before the rebuild is what
// lets buildRows see the in-flight state (sm.searching) rather than a stale
// one.
func (sm *spotlightModel) setQuery(q string) tea.Cmd {
	sm.query = q
	sm.cursor = 0
	cmd := sm.scheduleSearch()
	sm.buildRows()
	sm.cursor = sm.queryHome()
	sm.clampCursor()
	sm.refreshPreview()
	return cmd
}

// queryHome is the row a query-driven rebuild selects. Row 0 everywhere the
// list is nothing but matches (clampCursor bumps off it when it is a hint) —
// but the Task group's row 0 is the static Add-task entry, which no query ever
// matches, so a content search homes onto its first result instead — either
// kind of content row, so the home does not depend on which kind of hit the
// query produced. Homing on Add task there would mean every keystroke selected
// (and previewed) something unrelated to what the user was typing, and undid
// their arrow keys on the very next character.
func (sm *spotlightModel) queryHome() int {
	for i, r := range sm.rows {
		if r.kind == rowTask || r.kind == rowComment {
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
func (sm *spotlightModel) escPeel() tea.Cmd {
	switch {
	case sm.focus == focusPreview:
		sm.focus = focusList
		sm.offset = 0
	case sm.query != "":
		return sm.setQuery("")
	case sm.level == levelTaskActions:
		q := sm.taskQuery // setLevel clears it; read it first
		sm.setLevel(levelGroup, groupTask)
		if q != "" {
			return sm.setQuery(q)
		}
	case sm.level == levelGroup:
		sm.setLevel(levelRoot, groupNone)
	default:
		sm.open = false
	}
	return nil
}

// activate is Enter: a group row drills in, an entry row runs (or focuses its
// reference preview), a task row drills into that task's actions — and so does
// a comment row, into the actions of the task it belongs to.
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
	case rowTask, rowComment:
		// A comment row carries the task it belongs to for exactly this: a
		// comment is how the user found the task, not a thing with actions of
		// its own, so Enter lands on the same action list either way.
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
	// Taken before the close: closing is what the user will be coming back
	// from, and the state describing where they were is gone afterwards.
	snap := sm.snapshot()
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
		// stays nil for every segment replayed through m.handleKey — the
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
		sm.m.spotlightReturn = &snap
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
	snap := sm.snapshot()
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
		m.spotlightReturn = &snap
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
	case rowComment:
		sm.lines = commentPreviewLines(sm.m, r.comment, r.task, w)
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

// spotSearchDebounce is how long the launcher waits after the last keystroke
// before querying the store. A var rather than a const so tests can collapse
// it to zero instead of sleeping it out per keystroke (tea.Tick's Cmd blocks
// for its interval when invoked); nothing in production writes it.
var spotSearchDebounce = 150 * time.Millisecond

// spotSearchK is how many hits the launcher asks the store for. The rows share
// the left pane with the ACTIONS section and a launcher is a launcher, not a
// task list: enough to recognise what you meant, short enough that refining
// the query is the obvious next move.
const spotSearchK = 8

// spotSearchTickMsg is a debounced search coming due. It carries the
// generation and query it was scheduled for, so applySearchTick can tell its
// own landing from a superseded one — the launcher may have been typed into,
// peeled, or drilled out of during the wait.
type spotSearchTickMsg struct {
	gen   int
	query string
}

// contentSearchable reports whether the current level's query searches store
// content rather than the menu registry. The Task group is the launcher's one
// content surface; every other level filters entries in memory and must never
// reach the store.
func (sm *spotlightModel) contentSearchable() bool {
	return sm.level == levelGroup && sm.group == groupTask
}

// scheduleSearch invalidates any in-flight search and, when the current level
// searches content and has something to search, schedules the debounced one.
// Bumping the generation unconditionally is what makes invalidation total: a
// level change, a cleared query and a new keystroke all retire the pending
// tick through the same counter.
//
// The last error is retired through the same counter, on both paths: it
// described the query that produced it, and that query is gone. Clearing it
// only on the next SUCCESS pinned the launcher on "search error: …" across
// every later query, level change and reopen — hiding good hits behind a
// failure the user had already typed past.
func (sm *spotlightModel) scheduleSearch() tea.Cmd {
	sm.searchGen++
	sm.searchErr = ""
	if !sm.contentSearchable() || sm.m.projectScope == "" || strings.TrimSpace(sm.query) == "" {
		sm.hits = nil
		sm.searchQuery = ""
		sm.searching = false
		return nil
	}
	sm.searching = true
	gen, q := sm.searchGen, sm.query
	return tea.Tick(spotSearchDebounce, func(time.Time) tea.Msg {
		return spotSearchTickMsg{gen: gen, query: q}
	})
}

// applySearchTick publishes a landed search, or drops it. The cursor rehomes
// onto the top result exactly as a keystroke's rebuild does: the row set has
// just changed under it, so where it pointed no longer means anything.
func (sm *spotlightModel) applySearchTick(msg spotSearchTickMsg) {
	if !sm.open || msg.gen != sm.searchGen {
		return
	}
	sm.searching = false
	sm.searchQuery = strings.TrimSpace(msg.query)
	sm.hits = sm.runSearch(msg.query)
	sm.buildRows()
	sm.cursor = sm.queryHome()
	sm.clampCursor()
	sm.refreshPreview()
}

// runSearch is the one place the launcher reads store content. QueryText only,
// no vector: that takes store.Search's token-overlap text path over live store
// entities, so a task created seconds ago is findable before anything has
// embedded it — and the launcher never depends on an endpoint being up.
func (sm *spotlightModel) runSearch(q string) []spotRow {
	if sm.m.projectScope == "" || strings.TrimSpace(q) == "" {
		return nil
	}
	hits, _, err := sm.m.store.Search(core.SearchParams{
		Project:   sm.m.projectScope,
		QueryText: q,
		Kind:      "all",
		K:         spotSearchK,
	})
	if err != nil {
		// Reported as a row, never as "no tasks match": the store's
		// never-swallow rule reaches the surface the user is looking at.
		sm.searchErr = err.Error()
		return nil
	}
	sm.searchErr = ""
	return sm.groupHits(hits)
}

// groupHits turns store.Search's flat hit list into the section's rows: one row
// per matched task, each followed by the matched comments that belong to it.
//
// A comment whose task did not match itself still renders under its task — the
// task row is synthesized rather than skipped, so a comment hit is never
// orphaned at the top level and the row above it always names what it is a
// comment on. Group order is best-hit-first: a group takes the position of its
// highest-scoring member, which is the order store.Search already returns.
//
// Which means spotSearchK caps HITS, not rows: a synthesized parent is a row
// the store never returned, so the real bound on the section is 2K+1 rows (K
// comment hits on K distinct non-matching tasks, plus the header) rather than
// K. The list scrolls, so the wider bound costs the layout nothing — but the
// cap is not a row budget and must not be read as one.
//
// Every hit is re-read from the store before it becomes a row, for the same
// reason activateTaskAction re-reads its target: the index and the cache are a
// snapshot of a store another process can write to, and a hit whose entity is
// gone is dropped rather than rendered as a row that cannot be opened.
func (sm *spotlightModel) groupHits(hits []core.Hit) []spotRow {
	var order []string
	tasks := map[string]*core.Task{}
	comments := map[string][]*core.Comment{}
	snippets := map[string]string{}
	remember := func(tk *core.Task) {
		if _, seen := tasks[tk.ID]; !seen {
			order = append(order, tk.ID)
			tasks[tk.ID] = tk
		}
	}
	for _, h := range hits {
		switch h.Kind {
		case "task":
			tk, err := sm.m.store.GetTask(h.ID)
			if err != nil {
				continue
			}
			remember(tk)
		case "comment":
			c, err := sm.m.store.GetComment(h.ID)
			if err != nil {
				continue
			}
			tk, err := sm.m.store.GetTask(c.TaskID)
			if err != nil {
				continue
			}
			remember(tk)
			comments[tk.ID] = append(comments[tk.ID], c)
			snippets[c.ID] = h.Snippet
		}
	}
	var out []spotRow
	for _, id := range order {
		tk := tasks[id]
		out = append(out, spotRow{kind: rowTask, task: tk})
		for _, c := range comments[id] {
			out = append(out, spotRow{kind: rowComment, task: tk, comment: c, text: snippets[c.ID]})
		}
	}
	return out
}
