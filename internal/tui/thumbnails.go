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
	if b.m.projectScope == "" {
		return titledBoxHeight(b.m.styles.PaneInactive, paneW, "Boards", "no project selected", stripH)
	}
	if b.inUnmanagedMode() {
		title := fmt.Sprintf("unmanaged · %d %s", len(b.unmanaged), pluralLabels(len(b.unmanaged)))
		savedW, savedH := b.width, b.contentHeight
		b.SetSize(paneW-2, stripH-2)
		var inner string
		switch b.level {
		case lLevelChart:
			inner = b.renderChart()
		case lLevelDetail:
			inner = b.renderDetail()
		default:
			inner = b.renderUmbrella()
		}
		b.SetSize(savedW, savedH)
		return titledBoxHeight(b.m.styles.PaneActive, paneW, title, inner, stripH)
	}
	if len(b.rows) == 0 {
		return titledBoxHeight(b.m.styles.PaneInactive, paneW, "Boards",
			fmt.Sprintf("%s exposes no boards", b.m.capability.current), stripH)
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
// as drillIn's first step, but without mutating drill state. Its title always
// carries a permanent "[Shift-0]" label — the key that (re)focuses this cell,
// mirroring the pinned boxes' permanent "[Shift-N]" labels — regardless of
// whether it currently holds the highlight. Its border is the strong
// current-filter highlight only while pinFocus == -1 (the strip IS the active
// filter); once Shift-N has moved the highlight to a pin box, this cell
// renders muted like any other strip cell, though the [Shift-0] label stays.
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
	case lLevelUmbrella:
		inner = b.renderUmbrella()
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
	// The strong current-filter highlight now lives solely on the tabbed pin
	// box's active tab (see renderPinnedTabs), so the strip's SELECTED cell
	// always uses the normal PaneActive border — never PaneActiveStrong — to
	// avoid double-highlighting. Its title still carries the permanent
	// "[Shift-0]" label, and while pinFocus == -1 it advertises the drill.
	style := b.m.styles.PaneActive
	title := fmt.Sprintf("[Shift-0] %s", r.Name)
	if b.pinFocus == -1 {
		// The center board is the active filter: advertise that shift+right
		// drills into it. Kept short (no repeated "Shift+" word, already
		// implied by the [Shift-0] label) so both fit the cell width together.
		title += "  · → to inspect"
	}
	return titledBoxHeight(style, w, title, inner, h)
}

// pinnedBoxHeight is the fixed height (in lines) of the single tabbed pinned
// box drawn above the board strip: a top border carrying the Shift-N tab row,
// one body line (the active board's name + description), and a bottom border.
// listContentHeight reserves exactly this many lines for it, so the task list
// height never changes as boards are pinned or unpinned.
const pinnedBoxHeight = 3

// renderPinnedTabs renders the pinned boards as a SINGLE full-width,
// fixed-height (pinnedBoxHeight) tabbed box:
//
//	╭ Shift-0 · Shift-1 · Shift-2 · Shift-3 ──────────────╮
//	│ open-tasks — every open task: the active work.       │
//	╰──────────────────────────────────────────────────────╯
//
// The top border carries four KEY tabs: Shift-0 is the center/selected board
// (always present); Shift-1..maxPins are the pin slots. A filled pin slot's tab
// is muted; an EMPTY slot's tab is dimmed (an available slot). Exactly one tab
// carries the strong (bold accent) highlight — the active filter: Shift-0 when
// pinFocus == -1, else Shift-(pinFocus+1). The body line names the ACTIVE board
// (b.selected — the pin jumpPin selected, or the center board) and shows its
// description, fit to the box width; a board with no description shows a muted
// "needs description" note. This box is the sole active-filter indicator (the
// strip's SELECTED cell no longer carries the strong border).
func (b *boardsModel) renderPinnedTabs(paneW int) string {
	innerW := paneW - 2
	if innerW < 1 {
		innerW = 1
	}
	styles := b.m.styles
	border := styles.PaneInactive
	dim := lipgloss.NewStyle().Foreground(themeByName(b.m.themeName).Subtle)

	// Four KEY tabs: Shift-0 (center board) then Shift-1..maxPins (pin slots).
	const sep = " · "
	var styled strings.Builder
	plainW := 0
	for i := 0; i <= maxPins; i++ {
		st := styles.Muted
		if i > 0 && i > len(b.pins) {
			// Shift-i names an empty pin slot: dim it as an available slot.
			st = dim
		}
		active := (i == 0 && b.pinFocus == -1) || (i > 0 && b.pinFocus == i-1)
		if active {
			st = styles.PaneActiveStrong
		}
		text := fmt.Sprintf("Shift-%d", i)
		if i > 0 {
			styled.WriteString(sep)
			plainW += lipgloss.Width(sep)
		}
		styled.WriteString(st.Render(text))
		plainW += lipgloss.Width(text)
	}
	row := styled.String()
	if plainW > innerW-2 {
		// Very narrow pane: truncate the whole styled row (ANSI-aware) so the
		// tab bar never overflows the box.
		row = fitLine(row, innerW-2)
		plainW = innerW - 2
	}
	fill := innerW - 2 - plainW
	if fill < 0 {
		fill = 0
	}
	top := border.Render("╭ ") + row + border.Render(" "+repeat("─", fill)+"╮")

	// Body: the ACTIVE board's name + description (b.selected drives the filter).
	name := strings.TrimPrefix(b.selected, b.m.projectScope+":")
	desc := b.boardDescription(b.selected)
	bodyStyle := styles.Body
	var bodyText string
	switch {
	case name == "":
		bodyText, bodyStyle = "no board selected", styles.Muted
	case strings.TrimSpace(desc) == "":
		bodyText, bodyStyle = name+" — needs description", styles.Muted
	default:
		bodyText = name + " — " + desc
	}
	bodyText = fitLine(bodyText, innerW)
	styledBody := bodyStyle.Render(bodyText)
	if w := lipgloss.Width(bodyText); w < innerW {
		styledBody += spaces(innerW - w)
	}
	body := border.Render("│") + styledBody + border.Render("│")

	bottom := border.Render("╰" + repeat("─", innerW) + "╯")
	return top + "\n" + body + "\n" + bottom
}

// boardDescription returns a board's description, looked up by FullName against
// the current L0 row list — the same computed rows buildBoardRows produces for
// both namespace and leaf boards, so this needs no separate store lookup.
func (b *boardsModel) boardDescription(full string) string {
	for _, r := range b.rows {
		if r.FullName == full {
			return r.Description
		}
	}
	return ""
}
