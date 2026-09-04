package tui

import (
	"fmt"
	"strings"

	"atm/internal/capability"
	"atm/internal/core"
	"github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/reflow/wordwrap"
)

const (
	// taskDetailIndent insets every content row from the modal border.
	taskDetailIndent = "  "
	// detailLabelW is the width of the label column ("STATUS", "DESCRIPTION")
	// the value column hangs off. Wide enough for the longest label plus a
	// gap, so the values line up in one ragged-right column.
	detailLabelW = 13
	// detailDescriptionLines is how much of a description the page shows
	// before it stops and advertises the drill-in. A fixed head, not a
	// fit-to-content section: the sections below it must stay on screen.
	detailDescriptionLines = 8
	// detailViewHint advertises the description drill-in, and appears only
	// when the page actually cut the description short.
	detailViewHint = "[v] view"
)

// detailFooterHints is the DETAILS keymap, one hint per entry so the footer
// can break BETWEEN hints instead of through one.
var detailFooterHints = []string{
	"e edit title", "d description", "b add label", "B remove label",
	"M comment", "v view", "C thread", "j/k move", "enter drill in", "esc back",
}

// drillRow is one cursor target on a page: the line it starts at, and the
// level enter opens from it.
type drillRow struct {
	line int
	kind drillKind
	id   string
}

// drillPage is a rendered level: its lines, and the rows a cursor can walk.
// Rows are registered through addRow so a row's line index is always the
// line it was actually written at — the two cannot drift apart.
type drillPage struct {
	lines []string
	rows  []drillRow
}

func (p *drillPage) addRow(r drillRow, lines []string) {
	r.line = len(p.lines)
	p.rows = append(p.rows, r)
	p.lines = append(p.lines, lines...)
}

// handleDrillKey routes a key at whatever level the drill stack is on.
// Scrolling, row navigation and esc are common to every level; anything else
// belongs to the level's own handler, so a drill-in cannot inherit the
// DETAILS mutations.
func (t *tasksModel) handleDrillKey(k tea.KeyMsg) tea.Cmd {
	level := t.currentDrill()
	if level == nil {
		return nil
	}
	page := t.drillPage(level)
	switch k.String() {
	case "j", "down":
		t.drillCursorDown(level, page)
	case "k", "up":
		t.drillCursorUp(level, page)
	case "g":
		level.offset = 0
	case "pgdown", " ":
		level.offset += t.drillContentHeight() / 2
		t.clampOffset(level, page)
	case "pgup":
		if level.offset > t.drillContentHeight()/2 {
			level.offset -= t.drillContentHeight() / 2
		} else {
			level.offset = 0
		}
	case "enter":
		t.drillIntoCursorRow(level, page)
	case "esc":
		t.popDrill()
	default:
		if level.kind == drillDetail {
			return t.handleDetailActionKey(k)
		}
	}
	return nil
}

// drillCursorDown is the one key that both scrolls and selects: with no cursor it
// walks the page down until the first row is on screen and then takes it,
// which is what "j moves toward the comments, then through them" means.
func (t *tasksModel) drillCursorDown(level *drillLevel, page drillPage) {
	if len(page.rows) == 0 {
		level.offset++
		t.clampOffset(level, page)
		return
	}
	switch {
	case level.cursor < 0:
		if t.rowVisible(level, page, 0) {
			level.cursor = 0
			return
		}
		level.offset++
		t.clampOffset(level, page)
	case level.cursor+1 < len(page.rows):
		level.cursor++
		t.scrollToRow(level, page, level.cursor)
	default:
		// Past the last row the page keeps scrolling, so the sections below
		// the comments stay reachable from the same key.
		level.offset++
		t.clampOffset(level, page)
	}
}

// drillCursorUp reverses it: back up the rows, off the top of them, then scroll.
func (t *tasksModel) drillCursorUp(level *drillLevel, page drillPage) {
	switch {
	case level.cursor > 0:
		level.cursor--
		t.scrollToRow(level, page, level.cursor)
	case level.cursor == 0:
		level.cursor = -1
	default:
		if level.offset > 0 {
			level.offset--
		}
	}
}

func (t *tasksModel) drillIntoCursorRow(level *drillLevel, page drillPage) {
	if level.cursor < 0 || level.cursor >= len(page.rows) {
		return
	}
	r := page.rows[level.cursor]
	t.pushDrill(drillLevel{kind: r.kind, id: r.id, cursor: initialCursor(r.kind)})
}

// initialCursor decides whether a level opens with a row selected. A list of
// rows opens on its first one; a page of prose opens with no cursor at all,
// so its j/k scroll rather than select.
func initialCursor(kind drillKind) int {
	if kind == drillThread {
		return 0
	}
	return -1
}

func (t *tasksModel) rowVisible(level *drillLevel, page drillPage, i int) bool {
	if i < 0 || i >= len(page.rows) {
		return false
	}
	line := page.rows[i].line
	return line >= level.offset && line < level.offset+t.drillContentHeight()
}

func (t *tasksModel) scrollToRow(level *drillLevel, page drillPage, i int) {
	if i < 0 || i >= len(page.rows) {
		return
	}
	line := page.rows[i].line
	h := t.drillContentHeight()
	if line < level.offset {
		level.offset = line
	}
	// The row's body line has to clear the bottom edge too, or selecting a
	// row would scroll its own preview off the page.
	if bottom := line + detailCommentRowLines; bottom > level.offset+h {
		level.offset = bottom - h
	}
	t.clampOffset(level, page)
}

func (t *tasksModel) clampOffset(level *drillLevel, page drillPage) {
	maxOff := len(page.lines) - t.drillContentHeight()
	if maxOff < 0 {
		maxOff = 0
	}
	if level.offset > maxOff {
		level.offset = maxOff
	}
	if level.offset < 0 {
		level.offset = 0
	}
}

// handleDetailActionKey is the DETAILS level's own keymap: the mutations,
// which act on the task the level names, plus the drill-ins it can open.
func (t *tasksModel) handleDetailActionKey(k tea.KeyMsg) tea.Cmd {
	switch k.String() {
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
	case "v":
		t.pushDrill(drillLevel{kind: drillDescription, id: t.detailID(), cursor: -1})
	case "C":
		t.pushDrill(drillLevel{kind: drillThread, id: t.detailID(), cursor: 0})
	}
	return nil
}

func (t *tasksModel) openDetail(id string) tea.Cmd {
	if _, err := t.m.store.GetTask(id); err != nil {
		t.m.showToast("error: " + err.Error())
		return nil
	}
	if t.detailID() == id {
		return nil
	}
	t.pushDrill(drillLevel{kind: drillDetail, id: id, cursor: -1})
	return nil
}

func (t *tasksModel) backToList() {
	t.drillStack = nil
}

// detailRow renders one `LABEL  value` row. The pad is computed off the
// PLAIN label so a styled label still lines its value up with the others.
func detailRow(styles Styles, label, value string) string {
	pad := detailLabelW - lipgloss.Width(label)
	if pad < 1 {
		pad = 1
	}
	return taskDetailIndent + styles.HeaderLabel.Render(label) + spaces(pad) + value
}

// detailContinuationRow is a row under a label, aligned to the value column.
func detailContinuationRow(value string) string {
	return taskDetailIndent + spaces(detailLabelW) + value
}

// footerRows renders the keymap, wrapped rather than truncated: a keymap cut
// off at the modal edge silently stops advertising its last keys, which are
// the ones a reader is least likely to already know. It breaks between
// hints, never through one — "esc" on one line and "back" on the next is
// worse than either.
func (t *tasksModel) footerRows(hints []string) []string {
	w := t.detailContentWidth() - len(taskDetailIndent)
	var out []string
	line := ""
	for _, h := range hints {
		next := h
		if line != "" {
			next = line + " · " + h
		}
		if line != "" && lipgloss.Width(next) > w {
			out = append(out, taskDetailIndent+t.m.styles.KeyMenuDim.Render(line))
			line = h
			continue
		}
		line = next
	}
	if line != "" {
		out = append(out, taskDetailIndent+t.m.styles.KeyMenuDim.Render(line))
	}
	return out
}

// captionRows renders a section caption at the page's own indent — the
// helper writes flush-left, and a caption hanging off the modal border while
// every row under it is indented reads as a broken frame.
func (t *tasksModel) captionRows(title string) []string {
	rows := strings.Split(sectionCaption(t.m.styles, t.detailContentWidth()-len(taskDetailIndent), title), "\n")
	for i := range rows {
		rows[i] = taskDetailIndent + rows[i]
	}
	return rows
}

// detailValueWidth is how many columns the value column has.
func (t *tasksModel) detailValueWidth() int {
	w := t.detailContentWidth() - len(taskDetailIndent) - detailLabelW
	if w < 10 {
		w = 10
	}
	return w
}

// statusCell is the current capability's reading of this task — the same
// Annotate call the list's ANNOTATE column renders, so the two can never
// disagree. Nil when no project is scoped or the capability has nothing to
// say about this task.
func (t *tasksModel) statusCell(tk *core.Task) *capability.Cell {
	return t.annotate(tk)
}

// statusBadge is the glanceable form of the cell, parked on the modal's top
// border. Empty when there is no cell: an empty hint is dropped by the box
// renderer, which is what "nothing to say" should look like on a border.
func (t *tasksModel) statusBadge(tk *core.Task) string {
	cell := t.statusCell(tk)
	if cell == nil {
		return ""
	}
	return toneStyle(cell.Tone).Render(cell.Text)
}

// detailPage builds the DETAILS page. The order is the reading order: what
// this task IS (title), how it is going (status), what it says
// (description), where it sits (part-of), what was said about it (comments),
// then the bookkeeping and the keymap.
//
// The page is assembled head-then-tail so the comments digest knows how many
// lines everything else costs before it decides how many rows it can afford.
func (t *tasksModel) detailPage(level *drillLevel) drillPage {
	tk, err := t.m.store.GetTask(level.id)
	if err != nil {
		return drillPage{}
	}
	w := t.detailContentWidth()
	title := truncateRunes(tk.Title, w-len(taskDetailIndent))
	// DialogTitle pads a column either side; the heading owns its own indent
	// here, and the rule under it has to start at the same column.
	heading := t.m.styles.DialogTitle.Padding(0, 0)

	head := []string{
		"",
		taskDetailIndent + heading.Render(title),
		taskDetailIndent + t.m.styles.HeaderLine.Render(repeat("=", lipgloss.Width(title))),
		"",
		detailRow(t.m.styles, "STATUS", t.statusLine(tk)),
		"",
	}
	head = append(head, t.descriptionRows(tk)...)
	head = append(head, "")
	if rows := t.partOfRows(tk); len(rows) > 0 {
		head = append(head, rows...)
		head = append(head, "")
	}

	tail := []string{""}
	tail = append(tail, t.factsRows(tk)...)
	tail = append(tail, "")
	tail = append(tail, t.footerRows(detailFooterHints)...)

	page := drillPage{lines: head}
	t.commentsSection(&page, level, tk, len(head)+len(tail))
	page.lines = append(page.lines, tail...)
	return page
}

// statusLine is the cell's full text, tone-styled. It repeats the border
// badge on purpose: the badge is the glanceable form and can be crowded off
// a narrow border, while this row always carries the whole reading.
func (t *tasksModel) statusLine(tk *core.Task) string {
	cell := t.statusCell(tk)
	if cell == nil {
		return t.m.styles.Muted.Render("—")
	}
	return toneStyle(cell.Tone).Render(truncateRunes(cell.Text, t.detailValueWidth()))
}

// descriptionRows renders the head of the description: at most
// detailDescriptionLines rows, with the drill-in hint on the first row when
// (and only when) there was more to show.
func (t *tasksModel) descriptionRows(tk *core.Task) []string {
	body := strings.TrimSpace(tk.Description)
	if body == "" {
		return []string{detailRow(t.m.styles, "DESCRIPTION", t.m.styles.Muted.Render("(none)"))}
	}
	valueW := t.detailValueWidth()
	// Wrapped twice on purpose. The first pass answers "is there more than
	// the page shows"; only if there is does the hint need room, and only
	// then does the text wrap narrower to clear it. Reserving the gap up
	// front would waste those columns on every description that fits.
	wrapped := strings.Split(wordwrap.String(body, valueW), "\n")
	truncated := len(wrapped) > detailDescriptionLines
	if truncated {
		wrapW := valueW - lipgloss.Width(detailViewHint) - 2
		if wrapW < 10 {
			wrapW = 10
		}
		wrapped = strings.Split(wordwrap.String(body, wrapW), "\n")
		wrapped = wrapped[:detailDescriptionLines]
	}
	rows := make([]string, 0, len(wrapped))
	for i, line := range wrapped {
		if i > 0 {
			rows = append(rows, detailContinuationRow(line))
			continue
		}
		value := line
		if truncated {
			pad := valueW - lipgloss.Width(line) - lipgloss.Width(detailViewHint)
			if pad < 1 {
				pad = 1
			}
			value += spaces(pad) + t.m.styles.FieldHint.Render(detailViewHint)
		}
		rows = append(rows, detailRow(t.m.styles, "DESCRIPTION", value))
	}
	return rows
}

// factsRows is the bookkeeping tail: one compact facts line and the label
// chips. Timestamps are relative here — the same form the list column uses,
// and the only form that fits six facts on one line; the exact stamps live
// in the task's history.
func (t *tasksModel) factsRows(tk *core.Task) []string {
	rows := t.captionRows("FACTS")
	now := core.Now()
	rows = append(rows, fmt.Sprintf("%sid %s · project %s · created %s by %s · updated %s by %s",
		taskDetailIndent, tk.ID, tk.ProjectCode,
		relTime(tk.CreatedAt, now), tk.CreatedBy,
		relTime(tk.UpdatedAt, now), tk.UpdatedBy))
	if len(tk.Labels) == 0 {
		return append(rows, detailRow(t.m.styles, "LABELS", t.m.styles.Muted.Render("(none)")))
	}
	return append(rows, detailRow(t.m.styles, "LABELS", renderLabelChips(t.m.styles, tk.Labels, t.detailValueWidth())))
}

// descriptionDrillLines is the DESCRIPTION drill-in: the whole description,
// wrapped to the modal. Markdown rendering lands here in the last commit of
// this plan.
func (t *tasksModel) descriptionDrillLines(id string) []string {
	tk, err := t.m.store.GetTask(id)
	if err != nil {
		return nil
	}
	body := strings.TrimSpace(tk.Description)
	if body == "" {
		return []string{"", taskDetailIndent + t.m.styles.Muted.Render("(no description)")}
	}
	w := t.detailContentWidth() - len(taskDetailIndent)
	out := []string{""}
	for _, line := range strings.Split(wordwrap.String(body, w), "\n") {
		out = append(out, taskDetailIndent+line)
	}
	return out
}

// drillPage is the content of whatever level the stack is on, with the
// cursor rows that level offers.
func (t *tasksModel) drillPage(level *drillLevel) drillPage {
	switch level.kind {
	case drillDetail:
		return t.detailPage(level)
	case drillThread:
		return t.threadPage(level)
	case drillDescription:
		return drillPage{lines: t.descriptionDrillLines(level.id)}
	case drillComment:
		return drillPage{lines: t.commentDrillLines(level.id)}
	}
	return drillPage{}
}

func (t *tasksModel) drillContentHeight() int {
	h := t.contentHeight - 4
	if h < 1 {
		return 1
	}
	return h
}

// detailModalWidth is the modal's outer width: near-full-screen, with a
// gutter either side so the dimmed workspace still frames it.
func (t *tasksModel) detailModalWidth() int {
	w := t.m.width - 6
	if w < 20 {
		w = 20
	}
	return w
}

// detailContentWidth is the width content is written to: the modal minus its
// border and the fit margin the renderer applies.
func (t *tasksModel) detailContentWidth() int {
	w := t.detailModalWidth() - 4
	if w < 16 {
		w = 16
	}
	return w
}

// renderDrillModal renders the top drill level as the near-full-screen modal
// View layers over the workspace. "" when the stack is empty, which is what
// keeps the overlay chain off the plain list.
func (t *tasksModel) renderDrillModal() string {
	level := t.currentDrill()
	if level == nil {
		return ""
	}
	title, hint := t.drillTitle(level)
	page := t.drillPage(level)
	t.clampOffset(level, page)
	lines := page.lines
	end := level.offset + t.drillContentHeight()
	if end > len(lines) {
		end = len(lines)
	}
	width := t.detailModalWidth()
	var b strings.Builder
	for i := level.offset; i < end; i++ {
		b.WriteString(fitLine(lines[i], width-4))
		b.WriteString("\n")
	}
	return titledBoxHint(t.m.styles.DialogBody, width, title, hint, b.String(), t.contentHeight-2)
}

// drillTitle names the level on the modal's top border, with the status
// badge as the DETAILS level's right-hand hint.
func (t *tasksModel) drillTitle(level *drillLevel) (title, hint string) {
	switch level.kind {
	case drillDetail:
		if tk, err := t.m.store.GetTask(level.id); err == nil {
			return "Task " + level.id, t.statusBadge(tk)
		}
		return "Task " + level.id, ""
	case drillDescription:
		return "Description · " + level.id, ""
	case drillThread:
		return "Thread · " + level.id, ""
	case drillComment:
		return "Comment " + level.id, ""
	}
	return level.id, ""
}
