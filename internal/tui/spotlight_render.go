package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/reflow/wordwrap"
)

// The launcher's fixed measurements. Everything else is derived from the
// terminal, so these are the only numbers the layout hard-codes.
const (
	spotMinLeftPane = 24 // the action list never squeezes below this
	spotPaneGap     = 1  // the single column between the list and the panel
	spotPreviewCols = 52 // the panel's prose measure: wide enough to read, narrow enough to scan
	spotMinBlock    = 4  // the block never collapses below this many rows
	spotKeyCol      = 4  // "[D] " — the key column entries align their icons after
)

// menuBoxWidth is the overlay width. It is derived from what the launcher
// actually holds — the list's widest row plus a fixed prose measure for the
// preview — not from a share of the terminal, so the box is the same size on a
// laptop and on a wide monitor and simply sits in more space. A percentage
// width grew to 160 columns on a 200-column terminal to show rows 26 columns
// wide.
func (sm *spotlightModel) menuBoxWidth() int {
	// leftPaneWidth + the gap + the panel (its prose measure, two borders and
	// a column of padding), then the outer border and one column of breathing
	// room each side.
	bw := sm.leftPaneWidth() + spotPaneGap + sm.previewPanelWidth() + 4
	if max := sm.m.width - 4; bw > max {
		bw = max
	}
	return bw
}

// blockHeight is the tall middle of the overlay: the list beneath its header on
// one side, the preview panel on the other, sized to whichever needs more rows
// and capped by the terminal. The launcher used to be the terminal's height
// less six whatever it held, so eight rows sat in a box with twenty-one empty
// ones.
func (sm *spotlightModel) blockHeight() int {
	h := 1 + len(sm.rows) // the Actions header sits above the rows
	if p := 2 + len(sm.lines); p > h {
		h = p // the panel's two borders around its content
	}
	if h < spotMinBlock {
		h = spotMinBlock
	}
	// The overlay is the block plus its chrome: two outer borders, the three
	// rows of the search box, and the footer.
	if max := sm.m.height - 4 - 6; h > max {
		h = max
	}
	if h < spotMinBlock {
		h = spotMinBlock
	}
	return h
}

// blockTop is the first line of the block: past the top border and the search
// box's three rows.
func (sm *spotlightModel) blockTop() int { return 4 }

// panelLeftColumn is the column the preview panel's border occupies on every
// block line — the outer border, the leading space, the list, and the gap.
func (sm *spotlightModel) panelLeftColumn() int {
	return 2 + sm.leftPaneWidth() + spotPaneGap
}

// innerWidth is the width the two panes and the divider share: the box minus
// its two borders and one column of padding on each side.
func (sm *spotlightModel) innerWidth() int { return sm.menuBoxWidth() - 4 }

// leftPaneWidth is the action list's column, sized to the widest row it
// actually holds so the list neither truncates nor trails empty space. Neither
// this nor previewWidth may consult menuBoxWidth: the box is derived from them.
func (sm *spotlightModel) leftPaneWidth() int {
	w := spotMinLeftPane
	for _, r := range sm.rows {
		// two columns for the cursor glyph, then the row's own text
		if rw := 2 + lipgloss.Width(sm.rowText(r)); rw > w {
			w = rw
		}
	}
	// On a terminal too narrow to hold the list, the gap, a one-column panel
	// and the chrome, the list gives way first — it truncates more gracefully
	// than prose does.
	if max := sm.m.width - 14; max > spotMinLeftPane && w > max {
		w = max
	}
	return w
}

// previewWidth is the panel's prose measure: fixed, because readability is a
// property of line length rather than of the terminal, and a preview stretched
// to 120 columns is harder to read than one held at 52. It shrinks only when
// the terminal cannot hold the list, the panel and the chrome together.
// previewPanelWidth is the whole panel: the prose measure, its two borders and
// the column of padding inside them. The measure is what preview content is
// built against, so the panel is derived from it rather than the reverse —
// deriving the measure from the panel is how a line's ellipsis gets eaten by
// the padding.
func (sm *spotlightModel) previewPanelWidth() int { return sm.previewWidth() + 3 }

func (sm *spotlightModel) previewWidth() int {
	w := spotPreviewCols
	if room := sm.m.width - 4 - 4 - 3 - spotPaneGap - sm.leftPaneWidth(); w > room {
		w = room
	}
	if w < 1 {
		w = 1
	}
	return w
}

// spotlightHeight is the overlay's total height: most of the terminal, so the
// list and its preview both have room.
func (sm *spotlightModel) spotlightHeight() int {
	// the block, plus two outer borders, the search box's three rows, and the
	// footer
	return sm.blockHeight() + 6
}

// previewHeight is the body region's height — the full inner height less the
// search input, the pane headers, and the footer. Both panes are that tall:
// the preview is no longer the bottom half of the box, it is the right half.
func (sm *spotlightModel) previewHeight() int {
	h := sm.blockHeight() - 2 // the panel's own top and bottom borders
	if h < 1 {
		h = 1
	}
	return h
}

// breadcrumb is the box title: where in the tree the launcher is. A drilled
// launcher must never look like the root.
func (sm *spotlightModel) breadcrumb() string {
	switch sm.level {
	case levelGroup:
		if g := groupByID(sm.group); g != nil {
			return "Spotlight · " + g.label
		}
	case levelTaskActions:
		if sm.taskID != "" {
			return "Spotlight · Task · " + sm.taskID
		}
		return "Spotlight · Task"
	}
	return "Spotlight"
}

// paneStyle is the chrome style for the pane f: accented when it owns focus,
// dim when it does not.
//
// The accent is KeyMenu — bold accent text — and not RowCursor, which is bare
// reverse video. A reversed word reads as a filled block rather than as
// emphasis, and with the frame now carrying the focus cue too, a block was one
// loud element more than the layout needs.
func (sm *spotlightModel) paneStyle(f spotFocus) lipgloss.Style {
	if sm.focus == f {
		return sm.m.styles.KeyMenu
	}
	return sm.m.styles.KeyMenuDim
}

// cursorStyle is the ▸ glyph's style. The glyph stays put when focus moves to
// the preview — the cursor row is still where Tab will return to — but it
// dims, because a bright selection in a pane that is not taking keystrokes is
// exactly the confusion the redesign is meant to remove.
func (sm *spotlightModel) cursorStyle() lipgloss.Style {
	if sm.focus == focusList {
		return sm.m.styles.KeyMenu
	}
	return sm.m.styles.Muted
}

// borderStyle is the frame style for the pane f: accented when it owns focus,
// dim when it does not. It replaces the free-floating rule that used to divide
// the panes — a rule that began below the search line, ran on past the content,
// and stopped short of the footer, so it never met anything at either end. The
// panel's own left border does the dividing now, and it cannot drift out of
// alignment with a box it is part of.
//
// Carrying the focus cue on the frame rather than on text is also what lets the
// header and cursor stay quiet: the loudest element on screen is a one-column
// line, not a block of reversed video.
func (sm *spotlightModel) borderStyle(f spotFocus) lipgloss.Style {
	if sm.focus == f {
		return sm.m.styles.KeyMenu
	}
	return sm.m.styles.KeyMenuDim
}

// scrollMark is the "12–34/120" position marker: which slice of the preview is
// on screen. Only a focused, overflowing preview shows it — an unfocused
// preview is not being scrolled, and one that fits has nowhere to scroll to.
func (sm *spotlightModel) scrollMark() string {
	h := sm.previewHeight()
	if sm.focus != focusPreview || len(sm.lines) <= h {
		return ""
	}
	last := sm.offset + h
	if last > len(sm.lines) {
		last = len(sm.lines)
	}
	return fmt.Sprintf("%d–%d/%d", sm.offset+1, last, len(sm.lines))
}

// footerHint is the key legend for whichever half owns the keystrokes. The
// Esc word tracks what Esc actually does: escPeel unwinds one layer per press,
// and only a bare root — no query, no drilled level — has no layer left to
// peel and closes instead.
func (sm *spotlightModel) footerHint() string {
	if sm.focus == focusPreview {
		return "[↑↓/PgUp/PgDn] scroll · [Tab/Esc] back to list"
	}
	esc := "back"
	if sm.level == levelRoot && sm.query == "" {
		esc = "close"
	}
	return "[↑↓] move · [Enter] open · [Tab] preview · [Esc] " + esc
}

// searchBox is the query input: its own bordered field across the top of the
// launcher. Bare text with a "> " prefix read as a caption rather than as
// somewhere you type, which is the whole problem with an always-on search you
// cannot see. The caret is the list's alone — it appears only while the list
// owns focus, so a focused preview can never look like it is taking typed text.
func (sm *spotlightModel) searchBox(w int) string {
	st := sm.m.styles
	// the two borders, the padding column, then "> " — and the caret too, but
	// only when the focused line actually draws one. Reserving it either way
	// truncates the unfocused query a column earlier than it has to.
	overhead := 2 + 1 + 2
	if sm.focus == focusList {
		overhead++
	}
	room := w - overhead
	q := sm.query
	if lipgloss.Width(q) > room {
		q = fitLineFrom(q, lipgloss.Width(q)-room, room)
	}
	line := st.KeyMenuDim.Render("> ") + st.Body.Render(q)
	if sm.focus == focusList {
		line = st.KeyMenu.Render("> ") + st.Body.Render(q) + st.KeyMenu.Render("▏")
	}
	return titledBoxHeight(sm.borderStyle(focusList), w, "Search", " "+line, 3)
}

// previewPanel is the preview as its own titled box rather than text floating
// beside a rule. Its title carries the scroll position when a focused preview
// overflows, which is where a box's title belongs; the pane header it replaces
// had to reserve a column for the same marker.
//
// A panel shorter than the block centres in it: the preview is the launcher's
// answer to "what is this row", and answering in a box pinned to the top of a
// tall column reads as content that failed to load the rest.
func (sm *spotlightModel) previewPanel(blockH, w int) []string {
	rows := len(sm.lines)
	if max := blockH - 2; rows > max {
		rows = max
	}
	if rows < 1 {
		rows = 1
	}
	title := "Preview"
	if mark := sm.scrollMark(); mark != "" {
		title += "  " + mark
	}
	// One column of padding inside the border, so prose does not touch the
	// frame the way a bare column of text next to a rule used to.
	padded := sm.previewPaneLines(rows, w-3)
	for i := range padded {
		padded[i] = " " + padded[i]
	}
	body := strings.Join(padded, "\n")
	panel := strings.Split(titledBoxHeight(sm.borderStyle(focusPreview), w, title, body, rows+2), "\n")

	blank := spaces(w)
	for len(panel) < blockH {
		if (blockH-len(panel))%2 == 1 {
			panel = append([]string{blank}, panel...)
			continue
		}
		panel = append(panel, blank)
	}
	return panel
}

// actionsHeader labels the list. The preview's label lives in its panel's
// border, so this is the only header line left.
func (sm *spotlightModel) actionsHeader(w int) string {
	const actions = "Actions"
	return sm.paneStyle(focusList).Render(actions) + spaces(w-lipgloss.Width(actions))
}

// listStart is the first visible list row: the window scrolls only far enough
// to keep the cursor inside it.
func (sm *spotlightModel) listStart(bodyH int) int {
	if sm.cursor >= bodyH {
		return sm.cursor - bodyH + 1
	}
	return 0
}

// listPaneLines is the left column: exactly bodyH lines of exactly w columns,
// blank-padded past the end of the list, so the divider stays plumb.
func (sm *spotlightModel) listPaneLines(bodyH, w int) []string {
	out := make([]string, bodyH)
	start := sm.listStart(bodyH)
	for i := range out {
		row := start + i
		if row >= len(sm.rows) {
			out[i] = spaces(w)
			continue
		}
		out[i] = sm.renderListRow(sm.rows[row], row == sm.cursor, w)
	}
	return out
}

// renderListRow draws one list row, padded to w. A group drills in rather than
// running a key, so it renders "▤ Project ›" with no key column; a keyed entry
// renders "[a] + Add project", its icon aligned after the key column; a task
// renders "ATM-1a2b3c  wire the indexer ›"; a hint is dim copy at the label
// column with no cursor.
func (sm *spotlightModel) renderListRow(r spotRow, cursor bool, w int) string {
	glyph, glyphStyle := "  ", sm.m.styles.Body
	if cursor && r.selectable() {
		glyph, glyphStyle = "▸ ", sm.cursorStyle()
	}
	text := sm.rowText(r)
	style := sm.m.styles.Body
	if r.kind == rowHint {
		style = sm.m.styles.KeyMenuDim
	}
	text = fitLine(text, w-lipgloss.Width(glyph))
	pad := w - lipgloss.Width(glyph) - lipgloss.Width(text)
	return glyphStyle.Render(glyph) + style.Render(text) + spaces(pad)
}

// rowText is a row's text without its cursor glyph or padding. leftPaneWidth
// measures the list with it and renderListRow draws with it, so the column can
// never be sized against a different string than the one that lands in it.
func (sm *spotlightModel) rowText(r spotRow) string {
	text := r.label()
	switch r.kind {
	case rowGroup:
		if r.group != nil {
			text = r.group.icon + " " + r.group.label + " ›"
		}
	case rowEntry:
		if e := r.entry; e != nil {
			key := ""
			if e.key != "" {
				key = "[" + e.key + "]"
			}
			// A key wider than the column keeps its one separating space
			// rather than running into the icon.
			pad := spotKeyCol - lipgloss.Width(key)
			if pad < 1 {
				pad = 1
			}
			// A search row shows "Group · Label" — a flattened list has no
			// tree context left to say which group an entry came from.
			label := e.label
			if sm.filtering() {
				label = searchLabel(*e)
			}
			text = key + spaces(pad) + e.icon + " " + label
		}
	case rowTask:
		// A task row drills in exactly as a group row does, so it wears the
		// same › marker and skips the key column — it has no key of its own.
		// The ID leads: it is what the query matched, what the actions target,
		// and the only thing telling two same-titled tasks apart.
		if tk := r.task; tk != nil {
			text = tk.ID + "  " + tk.Title + " ›"
		}
	case rowHint:
		text = spaces(spotKeyCol) + r.text
	}
	return text
}

// previewPaneLines is the right column: bodyH lines from sm.offset, each fit
// to w. Preview content arrives pre-styled from the live renderers, so it is
// placed rather than restyled. An empty preview says so — a blank column is
// indistinguishable from a broken one.
func (sm *spotlightModel) previewPaneLines(bodyH, w int) []string {
	out := make([]string, bodyH)
	if len(sm.lines) == 0 {
		out[0] = sm.m.styles.KeyMenuDim.Render(fitLine("(no preview)", w))
		return out
	}
	for i := range out {
		if j := sm.offset + i; j < len(sm.lines) {
			out[i] = fitLine(sm.lines[j], w)
		}
	}
	return out
}

// renderOverlay draws the whole launcher: a short action list on the left, the
// full-height preview of whatever the cursor is on to its right, one search
// input across the top and one footer across the bottom.
//
//	╭ Spotlight ──────────────────────────────────────────╮
//	│ > ▏                                                 │
//	│ Actions              │ Preview                      │
//	│ ▸ ▤ Project ›        │ Create, select, rename, …    │
//	│   ☰ Task ›           │                              │
//	│   [D] ↯ Dispatch …   │ + Add project — Create a …   │
//	│ [↑↓] move · [Enter] open · [Tab] preview · [Esc] …  │
//	╰─────────────────────────────────────────────────────╯
//
// Exactly one element is bright at a time, and every cue moves together: the
// focused pane's header, the divider segment running alongside that pane's
// content, the ▸ glyph (list only), the search caret (list only). A user who
// glances at the box can always tell which half owns their keystrokes.
func (sm *spotlightModel) renderOverlay() string {
	st := sm.m.styles
	leftW, blockH := sm.leftPaneWidth(), sm.blockHeight()
	sm.clampOffset()

	// The left block is its header and then the rows; the right block is the
	// panel, whose own borders make the two into one line each.
	left := append([]string{sm.actionsHeader(leftW)}, sm.listPaneLines(blockH-1, leftW)...)
	panel := sm.previewPanel(blockH, sm.previewPanelWidth())

	var body strings.Builder
	for _, line := range strings.Split(sm.searchBox(sm.innerWidth()), "\n") {
		body.WriteString(" " + line + "\n")
	}
	for i := 0; i < blockH; i++ {
		body.WriteString(" " + left[i] + spaces(spotPaneGap) + panel[i] + "\n")
	}
	body.WriteString(" " + st.KeyMenuDim.Render(fitLine(sm.footerHint(), sm.innerWidth())))

	return titledBoxHeight(st.DialogBody, sm.menuBoxWidth(), sm.breadcrumb(), body.String(), sm.spotlightHeight())
}

// groupPreviewLines is a group row's preview: the group's hint, a blank line,
// then one line per member entry. Hovering a group answers "what is in here?"
// — repeating the hint alone would only restate what the row already says.
//
// One line per entry is the shape, so a summary too long for the pane is cut
// rather than wrapped — and cut with an ellipsis (fitLineTail, not fitLine),
// because these are prose lines that overflow at every terminal width short of
// about 200 columns. A whole pane of lines stopping mid-word with no cue reads
// as broken; it is also the first thing the launcher shows, since the cursor
// opens on a group row.
func groupPreviewLines(g *menuGroup, w int) []string {
	if g == nil {
		return nil
	}
	out := strings.Split(wordwrap.String(g.hint, w), "\n")
	out = append(out, "")
	for i := range menuEntries {
		e := &menuEntries[i]
		if e.hidden || e.group != g.id {
			continue
		}
		out = append(out, fitLineTail(e.icon+" "+e.label+" — "+e.summary, w))
	}
	return out
}
