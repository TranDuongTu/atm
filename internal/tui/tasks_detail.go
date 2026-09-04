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

const detailFooterHint = "e edit title · d description · b add label · B remove label · M comment · v view · esc back"

// handleDrillKey routes a key at whatever level the drill stack is on.
// Scrolling and esc are common to every level; anything else belongs to the
// level's own handler, so a drill-in cannot inherit the DETAILS mutations.
func (t *tasksModel) handleDrillKey(k tea.KeyMsg) tea.Cmd {
	level := t.currentDrill()
	if level == nil {
		return nil
	}
	switch k.String() {
	case "j", "down":
		level.offset++
		t.clampDrill()
	case "k", "up":
		if level.offset > 0 {
			level.offset--
		}
	case "g":
		level.offset = 0
	case "pgdown", " ":
		level.offset += t.drillContentHeight() / 2
		t.clampDrill()
	case "pgup":
		if level.offset > t.drillContentHeight()/2 {
			level.offset -= t.drillContentHeight() / 2
		} else {
			level.offset = 0
		}
	case "esc":
		t.popDrill()
	default:
		if level.kind == drillDetail {
			return t.handleDetailActionKey(k)
		}
	}
	return nil
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
		t.pushDrill(drillLevel{kind: drillDescription, id: t.detailID()})
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
	t.pushDrill(drillLevel{kind: drillDetail, id: id})
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

// detailLines builds the DETAILS page. The order is the reading order: what
// this task IS (title), how it is going (status), what it says (description),
// then the bookkeeping (facts, labels) and the keymap.
func (t *tasksModel) detailLines() []string {
	tk, err := t.m.store.GetTask(t.detailID())
	if err != nil {
		return nil
	}
	w := t.detailContentWidth()
	var b strings.Builder

	title := truncateRunes(tk.Title, w-len(taskDetailIndent))
	// DialogTitle pads a column either side; the heading owns its own indent
	// here, and the rule under it has to start at the same column.
	heading := t.m.styles.DialogTitle.Padding(0, 0)
	fmt.Fprintf(&b, "\n%s%s\n", taskDetailIndent, heading.Render(title))
	fmt.Fprintf(&b, "%s%s\n\n", taskDetailIndent, t.m.styles.HeaderLine.Render(repeat("=", lipgloss.Width(title))))

	fmt.Fprintf(&b, "%s\n\n", detailRow(t.m.styles, "STATUS", t.statusLine(tk)))

	for _, row := range t.descriptionRows(tk) {
		fmt.Fprintf(&b, "%s\n", row)
	}
	b.WriteString("\n")

	for _, row := range t.commentRows(tk) {
		fmt.Fprintf(&b, "%s\n", row)
	}
	b.WriteString("\n")

	for _, row := range t.factsRows(tk) {
		fmt.Fprintf(&b, "%s\n", row)
	}
	b.WriteString("\n")
	fmt.Fprintf(&b, "%s%s\n", taskDetailIndent, t.m.styles.KeyMenuDim.Render(detailFooterHint))

	return strings.Split(strings.TrimRight(b.String(), "\n"), "\n")
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

// commentRows is the COMMENTS section as it stood before this page was
// reshaped. The latest-N collapsed rows, the cursor and the thread drill-in
// replace it in the next commit of this plan; it stays verbatim here so this
// commit changes one thing at a time.
func (t *tasksModel) commentRows(tk *core.Task) []string {
	rows := t.captionRows("COMMENTS")
	cs, _ := t.m.store.ListComments(tk.ID)
	if len(cs) == 0 {
		return append(rows, taskDetailIndent+t.m.styles.Muted.Render("(no comments)"))
	}
	now := core.Now()
	for _, c := range cs {
		labels := "(no labels)"
		if len(c.Labels) > 0 {
			labels = strings.Join(c.Labels, " ")
		}
		rows = append(rows, fmt.Sprintf("%s%s   %s   %s", taskDetailIndent,
			c.CreatedBy, relTime(c.CreatedAt, now), truncateRunes(labels, 36)))
		bodyLines := strings.Split(c.Body, "\n")
		const maxLines = 6
		for i := 0; i < len(bodyLines) && i < maxLines; i++ {
			rows = append(rows, taskDetailIndent+"    "+bodyLines[i])
		}
		if len(bodyLines) > maxLines {
			rows = append(rows, taskDetailIndent+"    …")
		}
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

// drillLines is the content of whatever level the stack is on.
func (t *tasksModel) drillLines(level *drillLevel) []string {
	switch level.kind {
	case drillDetail:
		return t.detailLines()
	case drillDescription:
		return t.descriptionDrillLines(level.id)
	}
	return nil
}

func (t *tasksModel) clampDrill() {
	level := t.currentDrill()
	if level == nil {
		return
	}
	maxOff := len(t.drillLines(level)) - t.drillContentHeight()
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
	lines := t.drillLines(level)
	t.clampDrill()
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
	}
	return level.id, ""
}
