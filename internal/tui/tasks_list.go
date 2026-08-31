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

// listContentHeight is the single source of truth for how many lines the
// scrollable task list gets in the list view, once the fixed lane strip is
// subtracted. The strip is a CONSTANT laneStripHeight lines regardless of
// what the lanes contain, so the list height never moves under the user.
// renderListWithStrip and listPageSize both derive from this single value, so
// the renderer and the pgup/pgdown page jumps always agree on the page boundary.
func (t *tasksModel) listContentHeight() int {
	h := t.contentHeight - laneStripHeight
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
		t.m.lanes.move(dir)
	case "pgdown":
		t.cursor += t.listPageSize()
		t.clampCursor()
	case "pgup":
		t.cursor -= t.listPageSize()
		t.clampCursor()
	case "shift+right", "shift+left", "shift+up", "shift+down", "p",
		"!", "@", "#", "$", "%", "^", "&", "*", "(", ")":
		// Drill, pins and pin jumps addressed a ring of arbitrary boards.
		// There is no ring: three lanes are the whole surface and [ / ] reach
		// all of them. Consumed so the keys do nothing rather than fall
		// through to a global handler.
	case "s":
		// cycle sort
		t.sortMode = (t.sortMode + 1) % sortModeCount
		t.refresh()
	case "a":
		if t.m.projectScope == "" {
			return nil
		}
		t.openCreateForm()
	case "A":
		t.m.toggleScopedArt()
	case "S":
		// Vocabulary seeding survives the board-authoring cull: it converges
		// the project's labels, which is not authoring a board.
		return t.m.seedDefaults()
	case "n", "e", "d", "l":
		// Board authoring and label editing addressed boards the user owned.
		// The lanes are the flow's, not the user's.
	case "enter":
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

// flatLineCount returns the number of rows the list view presents — used for
// cursor bounds and paging. One lane is one flat list, so it is just the row
// count; the grouped tree went with the board ring that produced it.
func (t *tasksModel) flatLineCount() int { return len(t.rows) }

func (t *tasksModel) openDetailAtCursor() tea.Cmd {
	if t.cursor >= 0 && t.cursor < len(t.rows) {
		return t.openDetail(t.rows[t.cursor].id)
	}
	return nil
}

// selectedRow returns the task row under the cursor.
func (t *tasksModel) selectedRow() (taskRow, bool) {
	if t.cursor >= 0 && t.cursor < len(t.rows) {
		return t.rows[t.cursor], true
	}
	return taskRow{}, false
}

// renderListWithStrip renders the list view top to bottom: the task list
// renderListWithStrip composes the list view: the lane strip on top, then
// the task list. The strip leads because it captions what follows — which
// lane these rows are — and a caption read after its rows is not a caption.
// It reuses renderList() by temporarily shrinking t.contentHeight/t.pageSize
// to the list's sub-height (listContentHeight()) rather than refactoring
// renderList itself; the outer padToHeight below clamps any rounding.
func (t *tasksModel) renderListWithStrip() string {
	// Both computed BEFORE the shrink: listPageSize derives from the full
	// pane height, and reading it afterwards would subtract the strip twice.
	listH, pageSize := t.listContentHeight(), t.listPageSize()
	savedH, savedPageSize := t.contentHeight, t.pageSize
	t.contentHeight = listH
	t.pageSize = pageSize
	listOut := t.renderList()
	t.contentHeight, t.pageSize = savedH, savedPageSize

	var b strings.Builder
	b.WriteString(t.m.lanes.render(t.width))
	b.WriteString("\n")
	b.WriteString(listOut)
	return padToHeight(b.String(), t.contentHeight)
}

// fillGapWithArt replaces the task table's trailing blank padding (the dead
// space between the last rendered row and the footer divider) with
// background art for the scoped project. The block keeps its exact height —
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

// renderList draws the pane body. There is no capability/total/sort header
// row: the capability names itself in the pane title, the counts live on the
// lane cards, and the sort is readable where it is applied — on the column
// header it sorts by.
func (t *tasksModel) renderList() string {
	var b strings.Builder

	if t.m.projectScope == "" {
		t.renderEmptyState(&b, []string{
			t.m.styles.EmptyHead.Render("no project selected"),
			"",
			t.m.styles.EmptyText.Render(fmt.Sprintf("press %s in the Projects pane to scope this view", t.m.styles.EmptyKey.Render("[s]"))),
		})
		return padToHeight(b.String(), t.contentHeight)
	}

	footer := t.renderFlatList(&b)
	if footer == "" {
		// An empty state is centered in the whole block and has no count to
		// report, so there is no footer to pin and no gap to fill.
		return padToHeight(b.String(), t.contentHeight)
	}
	// The footer sits on the block's last line, not directly under the rows:
	// a count that floats up to meet a short list reads as part of the list.
	// Everything between the last row and it is the gap the art fills.
	return t.fillGapWithArt(t.closeList(b.String())) + "\n" + footer
}

// footerHeight is what dashboardFooter draws: its divider and its one line
// of text.
const footerHeight = 2

// closeList pads the list body to its block height and, when the rows leave
// dead space behind, rules it off. Without the rule a short list trails into
// the background art and reads as a list that ran out mid-render; with it,
// the space below is plainly not part of the list. A full list gets no rule —
// it would sit directly above the footer's own divider.
func (t *tasksModel) closeList(body string) string {
	h := t.contentHeight - footerHeight
	used := len(strings.Split(strings.TrimRight(body, "\n"), "\n"))
	if h-used >= 2 {
		body = strings.TrimRight(body, "\n") + "\n" +
			dashboardLine(t.width, repeat("─", dashboardContentWidth(t.width))) + "\n"
	}
	return padToHeight(body, h)
}

// renderEmptyState appends a vertically+horizontally centered empty-state
// block (each line center-aligned independently) into b.
func (t *tasksModel) renderEmptyState(b *strings.Builder, lines []string) {
	b.WriteString(centerLinesBoth(lines, t.width, t.contentHeight))
}

// taskColumnWidths returns fixed widths for ID/UPDATED and a flexible TITLE
// width that absorbs the remaining pane width. The format string used by both
// the header and data rows is " %-*s %-*s %*s" (leading space + 2
// inter-column spaces = 3 extra columns of padding). idW sizing note as
// before (IDs are "<CODE>-<hash>"). When the contextual column is present,
// metaW = metaColumnWidth and the padding grows by one (four columns).
func (t *tasksModel) taskColumnWidths() (idW, metaW, updatedW, titleW int) {
	idW, updatedW = 9, 9
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

// metaColumnName returns the annotation column's header, or "" when the
// column is absent (no scoped project). The header is the ROLE, not the
// capability's name: the columns are fixed, and which capability is reading
// is already answered by the pane title.
func (t *tasksModel) metaColumnName() string {
	if t.m.projectScope == "" {
		return ""
	}
	return "ANNOTATE"
}

// sortIndicator returns the arrow to hang off a column header, or "" when
// that column is not the sorted one. The indicator lives on the column it
// acts on — a sort caption elsewhere makes the user map a mode name onto a
// column every time they read it.
func (t *tasksModel) sortIndicator(col string) string {
	want := ""
	arrow := "↑"
	switch t.sortMode {
	case sortUpdatedDesc:
		want, arrow = "UPDATED", "↓"
	case sortUpdatedAsc:
		want = "UPDATED"
	case sortIDAsc:
		want = "ID"
	case sortTitleAsc:
		want = "TITLE"
	case sortAnnotate:
		want = "ANNOTATE"
	}
	if col != want {
		return ""
	}
	return " " + arrow
}

// columnHead is a column header with its sort indicator attached.
func (t *tasksModel) columnHead(col string) string { return col + t.sortIndicator(col) }

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

// renderFlatList writes the column header and the visible rows into b and
// RETURNS the footer, which the caller pins to the bottom of the block.
// Empty states return "" — they have nothing to count.
func (t *tasksModel) renderFlatList(b *strings.Builder) string {
	if len(t.rows) == 0 {
		t.renderEmptyState(b, []string{
			t.m.styles.EmptyHead.Render("no tasks match this focus"),
			"",
			t.m.styles.EmptyText.Render("switch lanes with [ / ]"),
		})
		return ""
	}
	idW, metaW, updatedW, titleW := t.taskColumnWidths()
	var header string
	if metaW > 0 {
		header = fmt.Sprintf(" %-*s %-*s %-*s %*s", idW, t.columnHead("ID"), titleW, t.columnHead("TITLE"), metaW, t.columnHead(t.metaColumnName()), updatedW, t.columnHead("UPDATED"))
	} else {
		header = fmt.Sprintf(" %-*s %-*s %*s", idW, t.columnHead("ID"), titleW, t.columnHead("TITLE"), updatedW, t.columnHead("UPDATED"))
	}
	b.WriteString(dashboardLine(t.width, t.m.styles.HeaderLabel.Render(header)))
	b.WriteString("\n")
	b.WriteString(dashboardLine(t.width, repeat("─", dashboardContentWidth(t.width))))
	b.WriteString("\n")

	start, end := t.pageWindow(len(t.rows))
	for i := start; i < end; i++ {
		r := t.rows[i]
		// A nil Cell is the capability saying nothing about this task. The
		// dash says that out loud: an empty column reads as a rendering bug.
		cellTxt := "—"
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
	return dashboardFooter(t.width, t.m.styles.Muted.Render(fmt.Sprintf("showing %d-%d of %d", start+1, end, len(t.rows))))
}

func (t *tasksModel) pageWindow(total int) (int, int) {
	return windowLines(total, t.cursor, t.pageSize)
}

// listChromeHeight is what the list block spends on anything that is not a
// row: the column header, its rule, and the footer's divider and count line —
// plus the blank line the row loop's trailing newline leaves behind.
const listChromeHeight = 5

// listPageSize returns how many rows fit in the list block. It is the single
// source for both the renderer and the pgup/pgdn page jumps, so a jump always
// lands on the exact page boundary that was drawn.
func (t *tasksModel) listPageSize() int {
	size := t.listContentHeight() - listChromeHeight
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
