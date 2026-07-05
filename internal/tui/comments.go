package tui

import (
	"fmt"
	"strings"

	"atm/internal/store"
	"github.com/charmbracelet/bubbletea"
)

type commentOverlayModel struct {
	id          string
	comment     *store.Comment
	historyOpen bool
	offset      int
	lines       []string
}

func (co *commentOverlayModel) view(m *Model) string {
	end := co.offset + m.tasks.contentHeight
	if end > len(co.lines) {
		end = len(co.lines)
	}
	var b strings.Builder
	for i := co.offset; i < end; i++ {
		b.WriteString(co.lines[i])
		b.WriteString("\n")
	}
	return padToHeight(b.String(), m.tasks.contentHeight)
}

func (co *commentOverlayModel) render(m *Model) {
	var b strings.Builder
	c := co.comment
	if c == nil {
		return
	}
	fmt.Fprintf(&b, "Comment %s\n", c.ID)
	b.WriteString(sepLine("─", 78, m.tasks.width, 2))
	b.WriteString("\n")
	fmt.Fprintf(&b, "%s\n", dashboardLine(m.tasks.width, fmt.Sprintf("id       %s", c.ID)))
	fmt.Fprintf(&b, "%s\n", dashboardLine(m.tasks.width, fmt.Sprintf("task     %s", c.TaskID)))
	if c.ReplyTo != "" {
		fmt.Fprintf(&b, "%s\n", dashboardLine(m.tasks.width, fmt.Sprintf("reply-to %s", c.ReplyTo)))
	}
	fmt.Fprintf(&b, "%s\n", dashboardLine(m.tasks.width, fmt.Sprintf("actor    %s", c.CreatedBy)))
	fmt.Fprintf(&b, "%s\n", dashboardLine(m.tasks.width, fmt.Sprintf("created  %s", store.RFC3339UTC(c.CreatedAt))))
	fmt.Fprintf(&b, "%s\n", dashboardLine(m.tasks.width, fmt.Sprintf("updated  %s by %s", store.RFC3339UTC(c.UpdatedAt), c.UpdatedBy)))
	fmt.Fprintf(&b, "%s\n", dashboardLine(m.tasks.width, fmt.Sprintf("labels   %s", formatLabelsTUI(c.Labels))))
	b.WriteString("\n")
	b.WriteString(sectionDivider(m.styles, m.tasks.width, "Body"))
	b.WriteString("\n")
	for _, line := range strings.Split(c.Body, "\n") {
		fmt.Fprintf(&b, "%s\n", dashboardLine(m.tasks.width, line))
	}
	b.WriteString("\n")
	if co.historyOpen {
		b.WriteString(sectionDivider(m.styles, m.tasks.width, "History"))
		b.WriteString("\n")
		code, _, _, _ := store.ParseCommentID(c.ID)
		hv := m.store.History(code, store.Subject{Kind: "comment", ID: c.ID})
		if len(hv) == 0 {
			b.WriteString(dashboardLine(m.tasks.width, " (no history)"))
			b.WriteString("\n")
		} else {
			for _, e := range hv {
				fmt.Fprintf(&b, "%s\n", dashboardLine(m.tasks.width, fmt.Sprintf("[%d] %s %s %s", e.Seq, store.RFC3339UTC(e.At), e.Actor, e.Action)))
			}
		}
		b.WriteString("\n")
	}
	b.WriteString(dashboardLine(m.tasks.width, m.styles.KeyMenuDim.Render("[H] history   [Esc] back")))
	co.lines = strings.Split(b.String(), "\n")
}

func formatLabelsTUI(labels []string) string {
	if len(labels) == 0 {
		return "(no labels)"
	}
	return strings.Join(labels, " ")
}

// handleCommentOverlayKey dispatches a key pressed while the comment overlay
// is open. The overlay is a read-only peek: scrolling, in-overlay history
// toggle, and Esc to return to the task detail. Comment mutations (body,
// labels, remove, reply) are CLI-only (`atm task comment ...`).
func (t *tasksModel) handleCommentOverlayKey(k tea.KeyMsg) tea.Cmd {
	co := &t.commentOverlay
	if co.comment == nil {
		return nil
	}
	switch k.String() {
	case "j", "down":
		co.offset++
		t.clampCommentOverlay()
	case "k", "up":
		if co.offset > 0 {
			co.offset--
		}
	case "g":
		co.offset = 0
	case "H":
		co.historyOpen = !co.historyOpen
		co.render(t.m)
	case "esc":
		t.commentOverlay = commentOverlayModel{}
		t.renderDetail()
	}
	return nil
}

func (t *tasksModel) clampCommentOverlay() {
	maxOff := len(t.commentOverlay.lines) - t.contentHeight
	if maxOff < 0 {
		maxOff = 0
	}
	if t.commentOverlay.offset > maxOff {
		t.commentOverlay.offset = maxOff
	}
}

// historyOverlayModel is a read-only overlay showing the task's audit-log
// history. Opened with [H] from task detail; closed with Esc.
type historyOverlayModel struct {
	active bool
	lines  []string
	offset int
}

func (ho *historyOverlayModel) view(m *Model) string {
	end := ho.offset + m.tasks.contentHeight
	if end > len(ho.lines) {
		end = len(ho.lines)
	}
	var b strings.Builder
	for i := ho.offset; i < end; i++ {
		b.WriteString(ho.lines[i])
		b.WriteString("\n")
	}
	return padToHeight(b.String(), m.tasks.contentHeight)
}

func (ho *historyOverlayModel) render(m *Model, code, taskID string) {
	var b strings.Builder
	fmt.Fprintf(&b, "History  %s\n", taskID)
	b.WriteString(sepLine("─", 78, m.tasks.width, 2))
	b.WriteString("\n")
	hv := m.store.History(code, store.Subject{Kind: "task", ID: taskID})
	if len(hv) == 0 {
		b.WriteString(dashboardLine(m.tasks.width, " (no history)"))
		b.WriteString("\n")
	} else {
		for _, e := range hv {
			fmt.Fprintf(&b, "%s\n", dashboardLine(m.tasks.width, fmt.Sprintf("[%d] %s %s %s", e.Seq, store.RFC3339UTC(e.At), e.Actor, e.Action)))
		}
	}
	b.WriteString("\n")
	b.WriteString(dashboardLine(m.tasks.width, m.styles.KeyMenuDim.Render("[Esc] back")))
	ho.lines = strings.Split(b.String(), "\n")
}

func (t *tasksModel) handleHistoryOverlayKey(k tea.KeyMsg) tea.Cmd {
	ho := &t.historyOverlay
	switch k.String() {
	case "j", "down":
		ho.offset++
		t.clampHistoryOverlay()
	case "k", "up":
		if ho.offset > 0 {
			ho.offset--
		}
	case "g":
		ho.offset = 0
	case "esc":
		t.historyOverlay = historyOverlayModel{}
	}
	return nil
}

func (t *tasksModel) clampHistoryOverlay() {
	maxOff := len(t.historyOverlay.lines) - t.contentHeight
	if maxOff < 0 {
		maxOff = 0
	}
	if t.historyOverlay.offset > maxOff {
		t.historyOverlay.offset = maxOff
	}
}
