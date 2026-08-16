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
	spotMinBoxWidth  = 64 // narrowest overlay before content starts truncating
	spotMinLeftPane  = 28 // the action list never squeezes below this
	spotDividerWidth = 3  // " │ ": the gap, the rule, the gap
	spotKeyCol       = 4  // "[D] " — the key column entries align their icons after
)

// menuBoxWidth is the overlay width: 80% of the terminal, at least 64 columns,
// and never wider than the terminal minus the two side columns. The split
// needs more room than the old vertical stack did, hence 80% rather than the
// 60% the other overlays use.
func (sm *spotlightModel) menuBoxWidth() int {
	bw := sm.m.width * 80 / 100
	if bw < spotMinBoxWidth {
		bw = spotMinBoxWidth
	}
	if bw > sm.m.width-4 {
		bw = sm.m.width - 4
	}
	return bw
}

// innerWidth is the width the two panes and the divider share: the box minus
// its two borders and one column of padding on each side.
func (sm *spotlightModel) innerWidth() int { return sm.menuBoxWidth() - 4 }

// leftPaneWidth is the action list's column. The list is short and its rows
// are short, so it takes the smaller share — but never less than 28 columns,
// below which a keyed row's label starts truncating.
func (sm *spotlightModel) leftPaneWidth() int {
	w := sm.innerWidth() * 40 / 100
	if w < spotMinLeftPane {
		w = spotMinLeftPane
	}
	return w
}

// previewWidth is everything the list and the divider leave. It floors at 1 so
// a terminal too narrow for the minimum left pane still renders a box: the
// content overflows into fitLine's truncation rather than into a negative
// width.
func (sm *spotlightModel) previewWidth() int {
	w := sm.innerWidth() - sm.leftPaneWidth() - spotDividerWidth
	if w < 1 {
		w = 1
	}
	return w
}

// spotlightHeight is the overlay's total height: most of the terminal, so the
// list and its preview both have room.
func (sm *spotlightModel) spotlightHeight() int {
	h := sm.m.height - 6
	if h < 12 {
		h = 12
	}
	return h
}

// previewHeight is the body region's height — the full inner height less the
// search input, the pane headers, and the footer. Both panes are that tall:
// the preview is no longer the bottom half of the box, it is the right half.
func (sm *spotlightModel) previewHeight() int {
	h := sm.spotlightHeight() - 5
	if h < 3 {
		h = 3
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
// dim when it does not. Both headers ask for it, so the two can never both
// come out bright.
func (sm *spotlightModel) paneStyle(f spotFocus) lipgloss.Style {
	if sm.focus == f {
		return sm.m.styles.RowCursor
	}
	return sm.m.styles.KeyMenuDim
}

// cursorStyle is the ▸ glyph's style. The glyph stays put when focus moves to
// the preview — the cursor row is still where Tab will return to — but it
// dims, because a bright selection in a pane that is not taking keystrokes is
// exactly the confusion the redesign is meant to remove.
func (sm *spotlightModel) cursorStyle() lipgloss.Style {
	if sm.focus == focusList {
		return sm.m.styles.RowCursor
	}
	return sm.m.styles.Muted
}

// dividerStyle styles one cell of the rule between the panes. row 0 is the
// pane-header row and row n sits beside the nth body row, so the accented run
// starts at the header and stops where the focused pane's content stops: the
// length of the bright segment is itself the cue for which pane is live.
//
// The accent is KeyMenu, not the RowCursor the headers use: RowCursor is
// reverse video, and a reversed rule tens of rows tall reads as a solid bar
// that would drown out every other cue — the opposite of the one-bright-
// element rule this accent exists to serve.
func (sm *spotlightModel) dividerStyle(row, bodyH int) lipgloss.Style {
	if row <= sm.focusedRun(bodyH) {
		return sm.m.styles.KeyMenu
	}
	return sm.m.styles.KeyMenuDim
}

// focusedRun is how many body rows the focused pane actually fills.
func (sm *spotlightModel) focusedRun(bodyH int) int {
	n := len(sm.lines) - sm.offset
	if sm.focus == focusList {
		n = len(sm.rows) - sm.listStart(bodyH)
	}
	if n > bodyH {
		n = bodyH
	}
	if n < 0 {
		n = 0
	}
	return n
}

func (sm *spotlightModel) dividerCell(row, bodyH int) string {
	return " " + sm.dividerStyle(row, bodyH).Render("│") + " "
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

// searchLine is the query input above both panes. The caret is the list's
// alone: it appears only while the list owns focus, so a focused preview can
// never look like it is taking typed text.
func (sm *spotlightModel) searchLine(w int) string {
	st := sm.m.styles
	q := sm.query
	// The unfocused line draws "> " + q with no caret, so it only needs 2
	// columns of overhead; the focused line adds the caret, needing 3. Using
	// the focused reservation for both used to truncate the unfocused line
	// one column earlier than it had to.
	overhead := 2
	if sm.focus == focusList {
		overhead = 3
	}
	if room := w - overhead; lipgloss.Width(q) > room {
		q = fitLineFrom(q, lipgloss.Width(q)-room, room)
	}
	if sm.focus != focusList {
		return st.KeyMenuDim.Render("> " + q)
	}
	return st.KeyMenu.Render(">") + " " + st.Body.Render(q) + st.KeyMenu.Render("▏")
}

// paneHeaderLine names the two panes and, on a focused preview that overflows,
// says where in it you are.
func (sm *spotlightModel) paneHeaderLine(leftW, prevW int) string {
	const actions, preview = "Actions", "Preview"
	left := sm.paneStyle(focusList).Render(actions) + spaces(leftW-lipgloss.Width(actions))
	right := sm.paneStyle(focusPreview).Render(preview)
	if mark := sm.scrollMark(); mark != "" {
		pad := prevW - lipgloss.Width(preview) - lipgloss.Width(mark)
		if pad < 1 {
			pad = 1
		}
		right += spaces(pad) + sm.m.styles.KeyMenuDim.Render(mark)
	}
	return left + sm.dividerCell(0, sm.previewHeight()) + right
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
// renders "[a] + Add project", its icon aligned after the key column; a hint
// is dim copy at the label column with no cursor.
func (sm *spotlightModel) renderListRow(r spotRow, cursor bool, w int) string {
	st := sm.m.styles
	glyph, glyphStyle := "  ", st.Body
	if cursor && r.selectable() {
		glyph, glyphStyle = "▸ ", sm.cursorStyle()
	}
	text, style := r.label(), st.Body
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
	case rowHint:
		text, style = spaces(spotKeyCol)+r.text, st.KeyMenuDim
	}
	text = fitLine(text, w-lipgloss.Width(glyph))
	pad := w - lipgloss.Width(glyph) - lipgloss.Width(text)
	return glyphStyle.Render(glyph) + style.Render(text) + spaces(pad)
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
	leftW, prevW, bodyH := sm.leftPaneWidth(), sm.previewWidth(), sm.previewHeight()
	sm.clampOffset()

	list := sm.listPaneLines(bodyH, leftW)
	preview := sm.previewPaneLines(bodyH, prevW)

	var body strings.Builder
	body.WriteString(" " + sm.searchLine(sm.innerWidth()) + "\n")
	body.WriteString(" " + sm.paneHeaderLine(leftW, prevW) + "\n")
	for i := 0; i < bodyH; i++ {
		body.WriteString(" " + list[i] + sm.dividerCell(i+1, bodyH) + preview[i] + "\n")
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
