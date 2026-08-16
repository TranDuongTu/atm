package tui

import (
	"strings"
	"unicode"

	"atm/internal/core"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/reflow/wordwrap"
)

// spotlightModel is the `\` launcher: a curated drill-in tree over the menu
// entry table rather than one exhaustive list. The root offers the four
// groups plus the inline global views; Enter drills into a group (and, for a
// task, into that task's actions); Esc peels one layer at a time. Content
// never varies with pane focus — activating an entry replays its scope
// prelude before its key, so a cross-pane action works from anywhere without
// the user navigating there first.
type spotlightModel struct {
	m         *Model
	open      bool
	level     spotLevel
	group     menuGroupID
	taskID    string // levelTaskActions target (Task 9)
	taskTitle string
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
	levelTaskActions           // per-task actions for sm.taskID (Task 9)
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
	rowTask // Task 9
	rowHint // non-selectable helper line
)

// spotRow is one line of the current level. Exactly one of group/entry/task
// is set, except for rowHint, which carries only its copy.
type spotRow struct {
	kind  spotRowKind
	group *menuGroup
	entry *menuEntry
	task  *core.Task // Task 9
	text  string     // rowHint copy
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
// also drops the task the action level was targeting.
func (sm *spotlightModel) setLevel(l spotLevel, g menuGroupID) {
	sm.level = l
	sm.group = g
	if l != levelTaskActions {
		sm.taskID = ""
		sm.taskTitle = ""
	}
	sm.query = ""
	sm.cursor = 0
	sm.focus = focusList
	sm.buildRows()
	sm.refreshPreview()
}

// buildRows derives the rows for the current (level, group, query). The query
// does not filter yet — Task 8 owns filtering; every printable key already
// lands here so wiring the match is all that remains.
func (sm *spotlightModel) buildRows() {
	sm.rows = nil
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
			// a whole: they render at levelTaskActions (Task 9).
			if sm.group == groupTask && isTaskAction(e) {
				continue
			}
			sm.rows = append(sm.rows, spotRow{kind: rowEntry, entry: e})
		}
		if sm.group == groupTask {
			sm.rows = append(sm.rows, spotRow{kind: rowHint, text: sm.taskHint()})
		}
	case levelTaskActions:
		// Task 9 builds the per-task action rows for sm.taskID.
	}
	sm.clampCursor()
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

// clampCursor keeps the cursor on a landable row after a rebuild: a hint line
// is never landable, and a shrinking row set (a typed query) must not leave
// the cursor past the end.
func (sm *spotlightModel) clampCursor() {
	if sm.cursor < 0 || sm.cursor >= len(sm.rows) || !sm.rows[sm.cursor].selectable() {
		sm.cursor = sm.firstSelectableRow()
	}
}

func (sm *spotlightModel) firstSelectableRow() int {
	for i, r := range sm.rows {
		if r.selectable() {
			return i
		}
	}
	return 0
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
		sm.focus = focusPreview
		sm.offset = 0
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

// handlePreviewKey is the focused-preview mode: the arrows (advertised in the
// footer) and j/k scroll; Tab and Esc — the only two exits — hand focus back
// to the list. Printable keys do not type here: the query belongs to the list.
func (sm *spotlightModel) handlePreviewKey(k tea.KeyMsg) {
	switch k.String() {
	case "j", "down":
		if sm.offset < sm.maxPreviewOffset() {
			sm.offset++
		}
	case "k", "up":
		if sm.offset > 0 {
			sm.offset--
		}
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

// setQuery replaces the query and rebuilds around it.
func (sm *spotlightModel) setQuery(q string) {
	sm.query = q
	sm.buildRows()
	sm.refreshPreview()
}

// escPeel unwinds exactly one layer per Esc, most recent first: a focused
// preview, then a typed query, then the task-action level, then the group
// level. Only a bare root closes the launcher — so a user who drilled in and
// typed is never thrown all the way out by one keypress.
func (sm *spotlightModel) escPeel() {
	switch {
	case sm.focus == focusPreview:
		sm.focus = focusList
		sm.offset = 0
	case sm.query != "":
		sm.setQuery("")
	case sm.level == levelTaskActions:
		sm.setLevel(levelGroup, groupTask)
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
		// Task 9: drilling a task row pushes levelTaskActions for r.task.
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

// refreshPreview rebuilds the preview content for the cursor row: a group row
// gets the group's one-line hint; a reference entry gets its full text
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
	w, h := sm.menuBoxWidth()-4, sm.previewHeight()
	if r.kind == rowGroup {
		if r.group != nil {
			sm.lines = strings.Split(wordwrap.String(r.group.hint, w), "\n")
		}
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

// spotlightKeyCol is the fixed width of the key column. Key-first with a
// fixed column keeps the labels aligned, so the list reads as a keymap the
// user can learn, not as a menu with a key footnote.
const spotlightKeyCol = 8

// renderRow draws "  [a]     Add project" — cursor glyph, padded key, label.
// A group, task, or hint row has no key, so the column pads to blank and its
// label still lines up with the entry labels. (Task 7 redesigns this half.)
func (sm *spotlightModel) renderRow(r spotRow, cursor bool, w int) string {
	glyph := "  "
	if cursor {
		glyph = "▸ "
	}
	key := ""
	if r.entry != nil && r.entry.key != "" {
		key = "[" + r.entry.key + "]"
	}
	if lipgloss.Width(key) < spotlightKeyCol {
		key += spaces(spotlightKeyCol - lipgloss.Width(key))
	} else {
		key += " "
	}
	return fitLine(glyph+key+r.label(), w)
}

// menuBoxWidth is the overlay width: 60% of the terminal, at least 64, and
// never wider than the terminal minus the two side columns. Mirrors
// channelsModel.renderOverlay.
func (sm *spotlightModel) menuBoxWidth() int {
	bw := sm.m.width * 60 / 100
	if bw < 64 {
		bw = 64
	}
	if bw > sm.m.width-4 {
		bw = sm.m.width - 4
	}
	return bw
}

// spotlightHeight is the overlay's total height: most of the terminal, so the
// global list and its preview both have room.
func (sm *spotlightModel) spotlightHeight() int {
	h := sm.m.height - 6
	if h < 12 {
		h = 12
	}
	return h
}

// previewHeight is the bottom region's content height — roughly half the box,
// minus the divider and the footer.
func (sm *spotlightModel) previewHeight() int {
	h := sm.spotlightHeight()/2 - 2
	if h < 3 {
		h = 3
	}
	return h
}

// renderOverlay draws the list above and the preview below, separated by a
// divider. The list scrolls around the cursor when it exceeds its region;
// the preview shows sm.lines from sm.offset.
func (sm *spotlightModel) renderOverlay() string {
	styles := sm.m.styles
	bw := sm.menuBoxWidth()
	inner := bw - 4
	total := sm.spotlightHeight()
	previewH := sm.previewHeight()
	listH := total - previewH - 4 // divider + footer + the box's two border rows
	if listH < 3 {
		listH = 3
	}

	// Scroll the list so the cursor stays inside its region.
	start := 0
	if sm.cursor >= listH {
		start = sm.cursor - listH + 1
	}
	end := start + listH
	if end > len(sm.rows) {
		end = len(sm.rows)
	}

	var body strings.Builder
	for i := start; i < end; i++ {
		line := sm.renderRow(sm.rows[i], i == sm.cursor, inner)
		switch {
		case i == sm.cursor:
			line = styles.RowCursor.Render(line)
		case sm.rows[i].kind == rowHint:
			line = styles.KeyMenuDim.Render(line)
		default:
			line = styles.Body.Render(line)
		}
		body.WriteString(line + "\n")
	}
	// padToHeight pads a rendered block to a total *line count*, not a count
	// of blank lines to append — passing "" through it would swallow the
	// trailing newline and merge the next divider onto this line, so the
	// remaining rows are padded directly with newlines instead.
	body.WriteString(strings.Repeat("\n", listH-(end-start)))

	label := "Preview"
	if sm.focus == focusPreview {
		label = "Preview ·"
	}
	body.WriteString(sectionDivider(styles, inner, label) + "\n")

	if sm.offset > sm.maxPreviewOffset() {
		sm.offset = sm.maxPreviewOffset()
	}
	if sm.offset < 0 {
		sm.offset = 0
	}
	shown := 0
	for i := sm.offset; i < len(sm.lines) && shown < previewH; i++ {
		body.WriteString(fitLine(sm.lines[i], inner) + "\n")
		shown++
	}
	body.WriteString(strings.Repeat("\n", previewH-shown))

	footer := "[↑/↓]move  [Enter]open  [Esc]back"
	if sm.focus == focusPreview {
		footer = "[j/k]scroll  [Esc]back"
	}
	body.WriteString(styles.KeyMenuDim.Render(footer))

	return titledBoxHeight(styles.DialogBody, bw, "Spotlight", body.String(), total)
}
