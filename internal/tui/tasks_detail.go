package tui

import (
	"fmt"
	"strings"

	"atm/internal/core"
	"github.com/charmbracelet/bubbletea"
)

type taskDetailState struct {
	id     string
	task   *core.Task
	lines  []string
	offset int
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
	fmt.Fprintf(&b, "%s\n", dashboardLine(t.width, fmt.Sprintf("created %s   by %s", core.RFC3339UTC(tk.CreatedAt), tk.CreatedBy)))
	fmt.Fprintf(&b, "%s\n", dashboardLine(t.width, fmt.Sprintf("updated %s   by %s", core.RFC3339UTC(tk.UpdatedAt), tk.UpdatedBy)))
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
			fmt.Fprintf(&b, "%s\n", dashboardLine(t.width, fmt.Sprintf(" %s   %s   %s", c.CreatedBy, relTime(c.CreatedAt, core.Now()), truncateRunes(labels, 36))))
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
