package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// splitStripWidths divides pane [2] inner width into prev (25%) / SELECTED (50%)
// / next (25%) with minimum clamps so narrow terminals still render a board name.
func splitStripWidths(paneW int) (prev, sel, next int) {
	prev = paneW * 25 / 100
	sel = paneW * 50 / 100
	next = paneW - prev - sel
	const minSide = 6
	const minSel = 8
	if prev < minSide {
		prev = minSide
	}
	if next < minSide {
		next = minSide
	}
	if sel < minSel {
		sel = minSel
	}
	// Re-fit if the minimums overflow: shrink sides first, then selected.
	for prev+sel+next > paneW && prev > minSide {
		prev--
	}
	for prev+sel+next > paneW && next > minSide {
		next--
	}
	for prev+sel+next > paneW && sel > minSel {
		sel--
	}
	if prev+sel+next > paneW {
		// Last resort: hard truncate to pane width keeping selected priority.
		next = paneW - prev - sel
		if next < 0 {
			next = 0
			sel = paneW - prev
			if sel < 0 {
				sel = 0
				prev = paneW
			}
		}
	}
	return
}

// renderStrip renders the horizontal board thumbnail strip: prev (25%) /
// SELECTED (50%) / next (25%). The SELECTED cell reuses boardsModel's level
// render (chart for a namespace board, detail for a leaf board) sized to its
// width. stripH is the fixed row height.
func (b *boardsModel) renderStrip(paneW, stripH int) string {
	if b.m.projectScope == "" || len(b.rows) == 0 {
		placeholder := titledBoxHeight(b.m.styles.PaneInactive, paneW, "Boards", "no project selected", stripH)
		return placeholder
	}
	prevW, selW, nextW := splitStripWidths(paneW)
	idx := b.ringIndex()
	if idx < 0 {
		idx = 0
	}
	selRow := b.rows[idx]

	// Small rings never duplicate a board across cells: one board -> both
	// sides blank; two boards -> the other board once, on the next side.
	blank := func(w int) string {
		return titledBoxHeight(b.m.styles.PaneInactive, w, "", "", stripH)
	}
	prevCell, nextCell := blank(prevW), blank(nextW)
	switch {
	case len(b.rows) >= 3:
		prevCell = b.renderSideCell(prevW, stripH, b.rows[(idx-1+len(b.rows))%len(b.rows)], "◂")
		nextCell = b.renderSideCell(nextW, stripH, b.rows[(idx+1)%len(b.rows)], "▸")
	case len(b.rows) == 2:
		nextCell = b.renderSideCell(nextW, stripH, b.rows[(idx+1)%len(b.rows)], "▸")
	}
	selCell := b.renderSelectedCell(selW, stripH, selRow)

	return lipgloss.JoinHorizontal(lipgloss.Top, prevCell, selCell, nextCell)
}

// renderSideCell renders a quiet prev/next thumbnail: board name + task count.
func (b *boardsModel) renderSideCell(w, h int, r boardRow, marker string) string {
	body := fmt.Sprintf("%s %s\n%d tasks", marker, r.Name, r.Count)
	return titledBoxHeight(b.m.styles.PaneInactive, w, r.Name, body, h)
}

// renderSelectedCell renders the SELECTED thumbnail at its current drill
// level (b.level), reusing the existing level renderers. L0 falls back to a
// namespace's chart or a leaf board's detail as the default view — the same
// as drillIn's first step, but without mutating drill state.
func (b *boardsModel) renderSelectedCell(w, h int, r boardRow) string {
	// Temporarily size boardsModel to the selected cell so the reused renderers
	// window correctly, then restore.
	savedW, savedH := b.width, b.contentHeight
	b.SetSize(w, h)
	defer func() {
		b.width, b.contentHeight = savedW, savedH
		b.pageSize = savedH - 2
		if b.pageSize < 1 {
			b.pageSize = 1
		}
	}()

	var inner string
	switch b.level {
	case lLevelChart:
		inner = b.renderChart()
	case lLevelDetail:
		inner = b.renderDetail()
	default: // lLevelTable
		if r.Expandable {
			// Default view for a namespace at L0 is its chart.
			savedLevel, savedNS, savedCursor := b.level, b.ns, b.cursor
			b.level = lLevelChart
			b.ns = r.Name
			b.cursor = 0
			defer func() { b.level, b.ns, b.cursor = savedLevel, savedNS, savedCursor }()
			inner = b.renderChart()
		} else {
			savedLevel, savedDetail := b.level, b.detail
			b.level = lLevelDetail
			b.detail = labelDetailState{row: labelRow{
				suffix:      strings.TrimPrefix(r.FullName, b.m.projectScope+":"),
				full:        r.FullName,
				description: r.Description,
				usage:       r.Count,
			}}
			defer func() { b.level, b.detail = savedLevel, savedDetail }()
			inner = b.renderDetail()
		}
	}
	return titledBoxHeight(b.m.styles.PaneActive, w, r.Name, inner, h)
}

// renderPinnedStack renders the pinned-boards stack: one full-width line per
// pinned board, each a single-line side-bordered pill (genuine rounded
// corners need >=3 lines, so this is the 1-line approximation — a rounded
// border with the top/bottom sides switched off). Each line shows
// "[N] name  description" in full, truncated with fitLine only if it
// overflows paneW. Returns "" when no pins exist.
func (b *boardsModel) renderPinnedStack(paneW int) string {
	if len(b.pins) == 0 {
		return ""
	}
	innerW := paneW - 2 // left + right border columns
	if innerW < 1 {
		innerW = 1
	}
	lines := make([]string, len(b.pins))
	for i, full := range b.pins {
		name := strings.TrimPrefix(full, b.m.projectScope+":")
		text := fmt.Sprintf("[%d] %s", i+1, name)
		if desc := b.pinDescription(full); desc != "" {
			text += "  " + desc
		}
		text = fitLine(text, innerW)
		if lw := lipgloss.Width(text); lw < innerW {
			text += spaces(innerW - lw)
		}
		lines[i] = b.m.styles.PinPill.Render(text)
	}
	return strings.Join(lines, "\n")
}

// pinDescription returns a pinned board's description, looked up by FullName
// against the current L0 row list — the same computed rows buildBoardRows
// produces for both namespace and leaf boards, so this needs no separate
// store lookup.
func (b *boardsModel) pinDescription(full string) string {
	for _, r := range b.rows {
		if r.FullName == full {
			return r.Description
		}
	}
	return ""
}
