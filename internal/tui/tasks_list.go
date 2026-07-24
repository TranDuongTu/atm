package tui

import (
	"fmt"
	"strings"

	"atm/internal/capability"
	"atm/internal/core"
	"atm/internal/tui/art"
	"github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type taskRow struct {
	id      string
	title   string
	labels  []string
	updated string
	cell    *capability.Cell // current capability's annotation, computed at refresh time
	task    *core.Task
}

// stripHeight is the fixed height of the board thumbnail strip rendered
// above the task list (list view only; clamps down on short terminals).
const stripHeight = 8

// listContentHeight is the single source of truth for how many lines the
// scrollable task list gets in the list view, once the fixed board strip and
// the FIXED pinned slot are subtracted. The pinned slot is a single tabbed box
// that always reserves pinnedBoxHeight lines (renderPinnedTabs draws exactly
// that many, regardless of how many boards are pinned), so the subtraction is a
// CONSTANT — the list height never changes as pins are added or removed.
// renderListWithStrip and listPageSize both derive from this single value, so
// the renderer and the pgup/pgdown page jumps always agree on the page boundary.
func (t *tasksModel) listContentHeight() int {
	h := t.contentHeight - stripHeight - pinnedBoxHeight
	if h < 4 {
		h = 4
	}
	return h
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
	case "shift+right", "shift+left":
		// Drill the SELECTED thumbnail in / out via boardsModel's level navigation.
		if k.String() == "shift+right" {
			t.m.boards.drillIn()
		} else {
			t.m.boards.drillOut()
		}
	case "shift+up", "shift+down":
		// Move the SELECTED thumbnail's chart cursor (the member that d, l target).
		dir := -1
		if k.String() == "shift+down" {
			dir = 1
		}
		t.m.boards.chartCursorMove(dir)
	case "p":
		t.m.boards.togglePin()
	case "!", "@", "#", "$", "%", "^", "&", "*", "(":
		n := shiftDigitToInt(k.String())
		t.m.boards.jumpPin(n)
	case ")":
		// Shift+0: return the strong current-filter highlight from a pin box
		// to the strip's SELECTED (center) board, the inverse of Shift-1..9.
		t.m.boards.focusCenter()
	case "s":
		// cycle sort
		t.sortMode = (t.sortMode + 1) % 3
		t.refresh()
	case "a":
		if t.m.projectScope == "" {
			return nil
		}
		t.openCreateForm()
	case "A":
		t.m.toggleScopedArt()
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

func (t *tasksModel) openDetailAtCursor() tea.Cmd {
	if !t.grouped() {
		if t.cursor >= 0 && t.cursor < len(t.rows) {
			return t.openDetail(t.rows[t.cursor].id)
		}
	}
	return nil
}

// selectedRow returns the task row under the cursor in either the flat or
// the grouped view. The flat/grouped branching mirrors openDetailAtCursor
// (flat) and rowAtCursor (grouped): grouped delegates to rowAtCursor, flat
// indexes t.rows with a bounds check.
func (t *tasksModel) selectedRow() (taskRow, bool) {
	if t.grouped() {
		return t.rowAtCursor()
	}
	if t.cursor >= 0 && t.cursor < len(t.rows) {
		return t.rows[t.cursor], true
	}
	return taskRow{}, false
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

// renderListWithStrip renders the list view top to bottom: the task list
// (fills), then the board thumbnail strip, then the single tabbed pinned box
// pinned at the very bottom of the pane (the detail view keeps the full pane
// since the strip is contextual to browsing). It reuses the existing
// renderList() by temporarily shrinking t.contentHeight/t.pageSize to the
// list's sub-height (from listContentHeight()) rather than refactoring
// renderList itself — renderList already ends with padToHeight(...,
// t.contentHeight), so the shrink makes it pad to the sub-height, and the
// outer padToHeight below clamps any rounding.
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
	listOut = t.fillGapWithArt(listOut)

	pinned := t.m.boards.renderPinnedTabs(t.width)
	strip := t.m.boards.renderStrip(t.width, stripHeight)

	var b strings.Builder
	b.WriteString(listOut)
	b.WriteString("\n")
	b.WriteString(strip)
	if pinned != "" {
		b.WriteString("\n")
		b.WriteString(pinned)
	}
	return padToHeight(b.String(), t.contentHeight)
}

// fillGapWithArt replaces the task table's trailing blank padding (the dead
// space between the last rendered row and the boards ring) with background
// art for the scoped project. The table block keeps its exact height —
// padToHeight pads with empty lines ("" — see padToHeight), so trailing
// lines that trim to empty are exactly the reclaimable gap. Below art.MinH
// blank lines the gap stays as-is (spec collapse threshold). No project
// scope means the gap is the empty-state screen's centered blank padding, so
// art is skipped entirely.
func (t *tasksModel) fillGapWithArt(listOut string) string {
	code := t.m.projectScope
	if code == "" || !t.m.artOn[code] {
		return listOut
	}
	lines := strings.Split(listOut, "\n")
	gap := 0
	for i := len(lines) - 1; i >= 0 && strings.TrimSpace(lines[i]) == ""; i-- {
		gap++
	}
	if gap < art.MinH {
		return listOut
	}
	theme := art.EffectivePair(t.m.artPair[code], code)[1]
	artLines := art.Render(theme, t.width, gap, art.Seed(code), t.m.artPhase,
		t.m.styles.ArtBase, t.m.styles.ArtAccent)
	if artLines == nil {
		return listOut
	}
	copy(lines[len(lines)-gap:], artLines)
	return strings.Join(lines, "\n")
}

func (t *tasksModel) headerLine() string {
	capName := t.m.capability.current
	if capName == "" {
		capName = "(none)"
	}
	return fmt.Sprintf("CAPABILITY: %s    TOTAL: %d/%d tasks    SORT: %s", capName, t.capCount, t.totalCount, t.sortMode)
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

// taskColumnWidths returns fixed widths for ID/UPDATED and a flexible TITLE
// width that absorbs the remaining pane width. The format string used by both
// the header and data rows is " %-*s %-*s %*s" (leading space + 2
// inter-column spaces = 3 extra columns of padding). idW sizing note as
// before (IDs are "<CODE>-<hash>"). When the contextual column is present,
// metaW = metaColumnWidth and the padding grows by one (four columns).
func (t *tasksModel) taskColumnWidths() (idW, metaW, updatedW, titleW int) {
	idW, updatedW = 9, 8
	for _, r := range t.rows {
		if w := len(r.id); w > idW {
			idW = w
		}
	}
	if idW > 14 {
		idW = 14
	}
	if t.metaColumnName() != "" && t.width >= metaColumnMinPaneWidth {
		metaW = metaColumnWidth
	}
	pad := 3
	if metaW > 0 {
		pad = 4
	}
	titleW = t.width - idW - metaW - updatedW - pad
	if titleW < 16 {
		titleW = 16
	}
	return
}

// metaColumnName returns the contextual column's header (the current
// capability's name, upper-cased), or "" when the column is absent: no scoped
// project, or the unmanaged pseudo-capability (which annotates nothing).
func (t *tasksModel) metaColumnName() string {
	if t.m.projectScope == "" || t.m.capability.unmanagedCurrent() || t.m.capability.current == "" {
		return ""
	}
	return strings.ToUpper(t.m.capability.current)
}

const metaColumnWidth = 18

// metaColumnMinPaneWidth is the minimum pane width that can fit all four
// columns (idW + metaW + updatedW + pad + titleW). Below this, the contextual
// column is hidden (metaW = 0) so narrow panes fall back to the three-column
// layout instead of overflowing: idW=9, metaW=18, updatedW=8, pad=4, titleW=16
// → minimum 55.
const metaColumnMinPaneWidth = 55

// toneStyle maps a Cell's semantic tone to a theme color. The capability
// says what a value means; this is the single place meaning becomes pixels.
func toneStyle(tone capability.Tone) lipgloss.Style {
	switch tone {
	case capability.ToneOK:
		return lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "28", Dark: "42"})
	case capability.ToneAttention:
		return lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "130", Dark: "214"})
	case capability.ToneStale:
		return lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "245", Dark: "246"})
	}
	return lipgloss.NewStyle()
}

func (t *tasksModel) renderFlatList(b *strings.Builder) {
	if t.focus.mode == focusUmbrellaIdle {
		t.renderEmptyState(b, []string{
			t.m.styles.EmptyHead.Render("unmanaged labels"),
			"",
			t.m.styles.EmptyText.Render("select a label below (Shift-↑/↓) to see its tasks"),
		})
		return
	}
	if len(t.rows) == 0 {
		t.renderEmptyState(b, []string{
			t.m.styles.EmptyHead.Render("no tasks match this focus"),
			"",
			t.m.styles.EmptyText.Render("switch boards with [ / ] to change focus"),
		})
		return
	}
	idW, metaW, updatedW, titleW := t.taskColumnWidths()
	var header string
	if metaW > 0 {
		header = fmt.Sprintf(" %-*s %-*s %-*s %*s", idW, "ID", titleW, "TITLE", metaW, t.metaColumnName(), updatedW, "UPDATED")
	} else {
		header = fmt.Sprintf(" %-*s %-*s %*s", idW, "ID", titleW, "TITLE", updatedW, "UPDATED")
	}
	b.WriteString(dashboardLine(t.width, t.m.styles.HeaderLabel.Render(header)))
	b.WriteString("\n")
	b.WriteString(dashboardLine(t.width, repeat("─", dashboardContentWidth(t.width))))
	b.WriteString("\n")

	start, end := t.pageWindow(len(t.rows))
	for i := start; i < end; i++ {
		r := t.rows[i]
		cellTxt := ""
		cellTone := capability.ToneNeutral
		if r.cell != nil {
			cellTxt, cellTone = r.cell.Text, r.cell.Tone
		}
		var line string
		if metaW > 0 {
			plain := fmt.Sprintf(" %-*s %-*s %-*s %*s", idW, truncateRunes(r.id, idW), titleW, truncateRunes(r.title, titleW), metaW, truncateRunes(cellTxt, metaW), updatedW, r.updated)
			if i == t.cursor {
				line = " " + t.m.styles.RowCursor.Render(strings.TrimPrefix(plain, " "))
			} else {
				line = fmt.Sprintf(" %-*s %-*s ", idW, truncateRunes(r.id, idW), titleW, truncateRunes(r.title, titleW)) +
					toneStyle(cellTone).Render(fmt.Sprintf("%-*s", metaW, truncateRunes(cellTxt, metaW))) +
					fmt.Sprintf(" %*s", updatedW, r.updated)
			}
		} else {
			line = fmt.Sprintf(" %-*s %-*s %*s", idW, truncateRunes(r.id, idW), titleW, truncateRunes(r.title, titleW), updatedW, r.updated)
			if i == t.cursor {
				line = " " + t.m.styles.RowCursor.Render(strings.TrimPrefix(line, " "))
			}
		}
		b.WriteString(dashboardLine(t.width, line))
		b.WriteString("\n")
	}
	b.WriteString(dashboardFooter(t.width, t.m.styles.Muted.Render(fmt.Sprintf("showing %d-%d of %d", start+1, end, len(t.rows)))))
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
		// flat (no nesting): t.others holds tasks carrying no label that
		// matches wildcards[0] (splitUnmatchedTop), a strict superset of the
		// store's own others once the filter carries 2+ wildcards.
		header := t.m.styles.GroupHeader.Render(fmt.Sprintf("▾ (no matching labels) (%d)", len(t.others)))
		if idx == t.cursor {
			header = t.m.styles.RowCursor.Render(header)
		}
		body.WriteString(dashboardLine(t.width, header))
		body.WriteString("\n")
		idx++
		for _, r := range t.others {
			titleW := t.width - 6
			if titleW < 20 {
				titleW = 20
			}
			if titleW > 32 {
				titleW = 32
			}
			line := fmt.Sprintf("  %s   id %s   updated %s", truncateRunes(r.title, titleW), r.id, r.updated)
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
	b.WriteString(dashboardFooter(t.width, t.m.styles.Muted.Render(fmt.Sprintf("showing %d-%d of %d", start+1, end, len(lines)))))
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
			titleW := t.width - 6 - len(rowIndent)
			if titleW < 20 {
				titleW = 20
			}
			if titleW > 32 {
				titleW = 32
			}
			line := fmt.Sprintf("%s%s   id %s   updated %s", rowIndent, truncateRunes(r.title, titleW), r.id, r.updated)
			if idx == t.cursor {
				if r.cell != nil {
					line += "   " + truncateRunes(r.cell.Text, metaColumnWidth)
				}
				line = t.m.styles.RowCursor.Render(line)
			} else {
				if r.cell != nil {
					line += "   " + toneStyle(r.cell.Tone).Render(truncateRunes(r.cell.Text, metaColumnWidth))
				}
			}
			b.WriteString(dashboardLine(t.width, line))
			b.WriteString("\n")
			idx++
		}
	}
	return idx
}

func (t *tasksModel) pageWindow(total int) (int, int) {
	return windowLines(total, t.cursor, t.pageSize)
}

// groupPageSize returns the number of lines that fit in the grouped/tree list
// body. Fixed overhead is 4 lines: the header line + blank line written by
// renderList, PLUS the footer divider + "showing X of Y" footer line
// renderGroupedList writes after the body. Reserving only 3 makes the body
// one line too tall, so padToHeight truncates the footer (and the pinned
// stack then renders where it would be).
// Called ONLY during render, where renderListWithStrip has already shrunk
// t.contentHeight to the list sub-height (listContentHeight), so
// t.contentHeight-4 == listH-4 here. The keypress side (listPageSize)
// reconstructs the same listH-4 from listContentHeight() directly, since at
// keypress time t.contentHeight is the full pane height.
func (t *tasksModel) groupPageSize() int {
	size := t.contentHeight - 4
	if size < 1 {
		size = 1
	}
	return size
}

// listPageSize returns the page size for whichever list mode is active, used by
// the pgdown / pgup page-jump keys. Both modes derive from listContentHeight()
// (the list sub-height) so a jump always lands on the exact page boundary the
// renderer draws: the flat body reserves 7 lines of chrome, the grouped body 4
// (header + blank + footer divider + "showing" footer).
func (t *tasksModel) listPageSize() int {
	if t.grouped() {
		size := t.listContentHeight() - 4
		if size < 1 {
			size = 1
		}
		return size
	}
	size := t.listContentHeight() - 7
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
		return ""
	}
	if t.view == tViewDetail {
		return "[e]title [d]desc [b]add label [B]remove label [M]comment [H]history [x]remove [Esc]back"
	}
	if t.m.capability.unmanagedCurrent() {
		return "[C]apabilities  [↑/↓]tasks  [Shift-↑/↓]labels  [Shift-→]drill  [s]ort  [Enter]detail"
	}
	return "[C]apabilities  [↑/↓]tasks  [ [ / ] ]board  [s]ort  [a]dd  [p]pin/unpin  [Enter]detail"
}
