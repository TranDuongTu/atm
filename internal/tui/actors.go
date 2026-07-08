package tui

import (
	"fmt"
	"strings"

	"atm/internal/activity"

	"github.com/charmbracelet/bubbletea"
)

// --- Actors pane model ---
//
// [4] Actors is a maximized pane: when focused it replaces the whole
// workspace area instead of sharing the persistent 3-pane grid (see
// app.go renderWorkspace). It shows a persona activity chart (list view)
// with drill-down into per-persona agents/models/actions breakdowns.

type actorsModel struct {
	m             *Model
	width         int
	contentHeight int
	groups        []activity.Group
	cursor        int
	detail        bool // false = list, true = per-persona detail
}

func newActorsModel(m *Model) actorsModel {
	return actorsModel{m: m}
}

func (a *actorsModel) SetSize(w, h int) {
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	a.width = w
	a.contentHeight = h
}

// refresh reloads activity for the current project scope.
func (a *actorsModel) refresh() {
	a.groups = nil
	a.cursor = 0
	a.detail = false
	code := a.m.projectScope
	if code == "" {
		return
	}
	entries, err := a.m.store.ReadLog(code)
	if err != nil {
		return
	}
	aliases, _ := a.m.store.LoadAliases()
	a.groups = activity.Aggregate(activity.Build(entries, aliases), "persona")
}

func (a *actorsModel) handleKey(k tea.KeyMsg) tea.Cmd {
	switch k.String() {
	case "up", "k":
		if a.cursor > 0 {
			a.cursor--
		}
	case "down", "j":
		if a.cursor < len(a.groups)-1 {
			a.cursor++
		}
	case "enter":
		if len(a.groups) > 0 {
			a.detail = true
		}
	case "esc":
		a.detail = false
	}
	return nil
}

func (a *actorsModel) View() string {
	if a.m.projectScope == "" {
		return padToHeight(dashboardLine(a.width, a.m.styles.Muted.Render("select a project to see actor activity")), a.contentHeight)
	}
	if a.detail && a.cursor < len(a.groups) {
		return a.renderDetail(a.groups[a.cursor])
	}
	return a.renderList()
}

func (a *actorsModel) renderList() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n", dashboardLine(a.width, fmt.Sprintf("actor activity: %s", a.m.projectScope)))
	b.WriteString("\n")
	if len(a.groups) == 0 {
		b.WriteString(dashboardLine(a.width, a.m.styles.Muted.Render("no activity")))
		return padToHeight(b.String(), a.contentHeight)
	}
	total := 0
	nameW := 0
	for _, g := range a.groups {
		total += g.Count
		if len(g.Key) > nameW {
			nameW = len(g.Key)
		}
	}
	meterW := a.width - nameW - 12
	if meterW < 10 {
		meterW = 10
	}
	for i, g := range a.groups {
		percent := 0
		if total > 0 {
			percent = (g.Count*100 + total/2) / total
		}
		cursor := " "
		if i == a.cursor {
			cursor = ">"
		}
		line := fmt.Sprintf("%s%-*s %s %3d%% %4d", cursor, nameW, g.Key, meterBar(percent, meterW), percent, g.Count)
		b.WriteString(dashboardLine(a.width, line))
		b.WriteString("\n")
	}
	return padToHeight(b.String(), a.contentHeight)
}

func (a *actorsModel) renderDetail(g activity.Group) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n", dashboardLine(a.width, fmt.Sprintf("persona: %s   (%d events)", g.Key, g.Count)))
	b.WriteString("\n")
	writeBreakdown(&b, a, "agents", g.Agents)
	writeBreakdown(&b, a, "models", g.Models)
	writeBreakdown(&b, a, "actions", g.Actions)
	b.WriteString("\n")
	b.WriteString(dashboardLine(a.width, a.m.styles.Muted.Render("[Esc] back")))
	return padToHeight(b.String(), a.contentHeight)
}

// kvRow is a sortable (key, count) pair used for the agents/models/actions
// breakdown rows in the persona detail view.
type kvRow struct {
	k string
	v int
}

func writeBreakdown(b *strings.Builder, a *actorsModel, title string, counts map[string]int) {
	b.WriteString(dashboardLine(a.width, a.m.styles.Muted.Render(title)))
	b.WriteString("\n")
	if len(counts) == 0 {
		b.WriteString(dashboardLine(a.width, "  (none)"))
		b.WriteString("\n")
		return
	}
	var rows []kvRow
	total := 0
	for k, v := range counts {
		rows = append(rows, kvRow{k, v})
		total += v
	}
	sortKV(rows)
	meterW := a.width - 24
	if meterW < 8 {
		meterW = 8
	}
	for _, r := range rows {
		percent := 0
		if total > 0 {
			percent = (r.v*100 + total/2) / total
		}
		line := fmt.Sprintf("  %-14s %s %4d", r.k, meterBar(percent, meterW), r.v)
		b.WriteString(dashboardLine(a.width, line))
		b.WriteString("\n")
	}
}

// sortKV sorts rows by count desc, then key asc (deterministic display order).
func sortKV(rows []kvRow) {
	for i := 1; i < len(rows); i++ {
		for j := i; j > 0; j-- {
			x, y := rows[j-1], rows[j]
			if y.v > x.v || (y.v == x.v && y.k < x.k) {
				rows[j-1], rows[j] = rows[j], rows[j-1]
			} else {
				break
			}
		}
	}
}
