package tui

import (
	"fmt"
	"strings"

	"atm/internal/core"
	"github.com/charmbracelet/bubbletea"
)

func (t *tasksModel) handleDetailKey(k tea.KeyMsg) tea.Cmd {
	level := t.currentDrill()
	if level == nil {
		return nil
	}
	switch k.String() {
	case "j", "down":
		level.offset++
		t.clampDetail()
	case "k", "up":
		if level.offset > 0 {
			level.offset--
		}
	case "g":
		level.offset = 0
	case "pgdown", " ":
		level.offset += t.contentHeight / 2
		t.clampDetail()
	case "pgup":
		if level.offset > t.contentHeight/2 {
			level.offset -= t.contentHeight / 2
		} else {
			level.offset = 0
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
	case "esc":
		t.popDrill()
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

func (t *tasksModel) detailLines() []string {
	var b strings.Builder
	tk, err := t.m.store.GetTask(t.detailID())
	if err != nil {
		return nil
	}
	b.WriteString(t.m.styles.Muted.Render(tk.Title))
	b.WriteString("\n\n")
	b.WriteString(sectionCaption(t.m.styles, t.width, "FACTS"))
	b.WriteString("\n")
	fmt.Fprintf(&b, "%s\n", dashboardLine(t.width, fmt.Sprintf("id      %s", tk.ID)))
	fmt.Fprintf(&b, "%s\n", dashboardLine(t.width, fmt.Sprintf("project %s", tk.ProjectCode)))
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
	return strings.Split(b.String(), "\n")
}

func (t *tasksModel) clampDetail() {
	level := t.currentDrill()
	if level == nil {
		return
	}
	maxOff := len(t.detailLines()) - t.detailModalContentHeight()
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

func (t *tasksModel) detailModalContentHeight() int {
	h := t.contentHeight - 4
	if h < 1 {
		return 1
	}
	return h
}

func (t *tasksModel) renderDetailModal() string {
	level := t.currentDrill()
	if level == nil || level.kind != drillDetail {
		return ""
	}
	lines := t.detailLines()
	t.clampDetail()
	end := level.offset + t.detailModalContentHeight()
	if end > len(lines) {
		end = len(lines)
	}
	width := t.m.width - 6
	if width < 20 {
		width = 20
	}
	var b strings.Builder
	for i := level.offset; i < end; i++ {
		b.WriteString(fitLine(lines[i], width-4))
		b.WriteString("\n")
	}
	return titledBoxHeight(t.m.styles.DialogBody, width, "Task "+level.id, b.String(), t.contentHeight-2)
}
