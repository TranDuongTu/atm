package tui

import (
	"fmt"
	"strings"

	"atm/internal/core"
)

// taskHistoryLines renders a task's audit-log history as plain lines: header
// "History  <taskID>", separator, "[seq] time actor action" rows, and a
// "(no history)" fallback. Extracted from the deleted task-detail history
// overlay (historyOverlayModel.render) — the redesigned spotlight's task
// preview calls this to show a hovered task's history in-pane, so the
// signature and output shape are load-bearing for that caller.
func taskHistoryLines(m *Model, code, taskID string, width int) []string {
	var b strings.Builder
	fmt.Fprintf(&b, "History  %s\n", taskID)
	b.WriteString(sepLine("─", 78, width, 2))
	b.WriteString("\n")
	hv := m.store.History(code, core.Subject{Kind: "task", ID: taskID})
	if len(hv) == 0 {
		b.WriteString(dashboardLine(width, " (no history)"))
		b.WriteString("\n")
	} else {
		for _, e := range hv {
			fmt.Fprintf(&b, "%s\n", dashboardLine(width, fmt.Sprintf("[%d] %s %s %s", e.Seq, core.RFC3339UTC(e.At), e.Actor, e.Action)))
		}
	}
	return strings.Split(b.String(), "\n")
}
