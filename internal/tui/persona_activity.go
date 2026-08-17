package tui

import (
	"fmt"
	"strings"

	"atm/internal/activity"
	"atm/internal/core"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// personaActivityModel is the read-only activity breakdown opened from the
// focused Projects chart. group is captured at open time so scrolling never
// re-aggregates or changes the activity window beneath the reader.
type personaActivityModel struct {
	m      *Model
	open   bool
	key    string
	spec   chartRangeSpec
	group  activity.Group
	offset int
}

func (p *personaActivityModel) openFor(key string, spec chartRangeSpec, entries []core.LogEntry) {
	p.key = key
	p.spec = spec
	p.group = aggregateWindow(entries, key, spec, core.Now())
	p.offset = 0
	p.open = true
}

func (p *personaActivityModel) handleKey(k tea.KeyMsg) tea.Cmd {
	p.clampOffset()
	switch k.String() {
	case "esc":
		p.open = false
	case "j", "down":
		if p.offset < p.maxOffset() {
			p.offset++
		}
	case "k", "up":
		if p.offset > 0 {
			p.offset--
		}
	case "g":
		p.offset = 0
	}
	return nil
}

func (p *personaActivityModel) overlayHeight() int {
	height := p.m.height - 8
	if height < 8 {
		return 8
	}
	return height
}

func (p *personaActivityModel) activityLines(width int) []string {
	lines := []string{fmt.Sprintf("%d events", p.group.Count), ""}
	lines = append(lines, breakdown("models", p.group.Models, width)...)
	lines = append(lines, breakdown("agents", p.group.Agents, width)...)
	lines = append(lines, breakdown("actions", p.group.Actions, width)...)
	return lines
}

func (p *personaActivityModel) visibleRows() int {
	// titledBoxHeight supplies height-2 interior rows; reserve two of those
	// for the blank separator and navigation footer below the activity body.
	rows := p.overlayHeight() - 4
	if rows < 1 {
		return 1
	}
	return rows
}

func (p *personaActivityModel) maxOffset() int {
	max := len(p.activityLines(0)) - p.visibleRows()
	if max < 0 {
		return 0
	}
	return max
}

func (p *personaActivityModel) clampOffset() {
	if p.offset > p.maxOffset() {
		p.offset = p.maxOffset()
	}
	if p.offset < 0 {
		p.offset = 0
	}
}

func (p *personaActivityModel) renderOverlay() string {
	styles := p.m.styles
	bw := p.m.width * 60 / 100
	if bw < 64 {
		bw = 64
	}
	if bw > p.m.width-4 {
		bw = p.m.width - 4
	}
	if bw < 3 {
		bw = 3
	}
	height := p.overlayHeight()

	innerW := bw - 4
	if innerW < 1 {
		innerW = 1
	}
	lines := p.activityLines(innerW)

	visible := p.visibleRows()
	offset := p.offset
	if offset > p.maxOffset() {
		offset = p.maxOffset()
	}
	if offset < 0 {
		offset = 0
	}
	end := offset + visible
	if end > len(lines) {
		end = len(lines)
	}
	body := make([]string, 0, end-offset+2)
	for _, line := range lines[offset:end] {
		body = append(body, fitLine(line, innerW))
	}
	body = append(body, "", styles.KeyMenuDim.Render("[j/k]scroll  [g]top  [Esc]close"))
	title := personaIcon(p.key) + " " + carouselName(p.key) + " \u00b7 " + p.spec.key
	return titledBoxHeight(styles.DialogBody, bw, title, strings.Join(body, "\n"), height)
}

// breakdown renders a deterministic proportional section for the supplied
// aggregate dimensions. Empty dimensions remain visible as an explicit row.
func breakdown(caption string, counts map[string]int, width int) []string {
	lines := []string{caption}
	if len(counts) == 0 {
		return append(lines, "  (none)")
	}

	rows := make([]kvRow, 0, len(counts))
	total, nameW := 0, 0
	for key, count := range counts {
		rows = append(rows, kvRow{k: key, v: count})
		total += count
		if w := lipgloss.Width(key); w > nameW {
			nameW = w
		}
	}
	sortKV(rows)
	meterW := width - nameW - 8
	if meterW < 0 {
		meterW = 0
	}
	for _, row := range rows {
		percent := 0
		if total > 0 {
			percent = (row.v*100 + total/2) / total
		}
		lines = append(lines, fmt.Sprintf("  %-*s %s %4d", nameW, row.k, meterBar(percent, meterW), row.v))
	}
	return lines
}
