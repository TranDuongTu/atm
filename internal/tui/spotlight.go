package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/reflow/wordwrap"
)

// spotlightModel is the `\` launcher: one global list of every action the TUI
// offers, key-first, with a preview region below. Content never varies with
// pane focus — activation replays the entry's scope prelude before its key, so
// a cross-pane action works from anywhere without the user navigating first.
type spotlightModel struct {
	m      *Model
	open   bool
	cursor int
	rows   []menuRow
	focus  spotFocus
	offset int      // preview scroll (focusPreview only)
	lines  []string // preview content, one string per line
}

// minPreviewHeight is the smallest previewHeight() the live renderer
// registry is worth consulting at; below it, an entry falls back to its
// summary line rather than squeezing a real overlay/form render into a
// sliver.
const minPreviewHeight = 4

type spotFocus int

const (
	focusList spotFocus = iota
	focusPreview
)

// menuRow is one rendered line: a section header (entry == nil) or an entry.
type menuRow struct {
	header string
	entry  *menuEntry
}

// openSpotlight builds the global row list: Views, then one section per scope
// family in table order, then Reference. Hidden entries never become rows.
func (sm *spotlightModel) openSpotlight() {
	sm.rows = nil
	sm.focus = focusList
	sm.offset = 0
	sm.lines = nil
	sm.open = true

	sm.rows = append(sm.rows, menuRow{header: "Views"})
	for i := range menuEntries {
		e := menuEntries[i]
		if e.hidden || e.section != sectionViews {
			continue
		}
		// The capabilities switcher is only reachable when a project scope
		// exists; advertising it otherwise would replay into a no-op.
		if e.needsProject && sm.m.projectScope == "" {
			continue
		}
		sm.rows = append(sm.rows, menuRow{entry: &e})
	}

	for _, title := range []string{"Projects", "Tasks", "Boards"} {
		var section []menuRow
		for i := range menuEntries {
			e := menuEntries[i]
			if e.hidden || e.section != sectionActions || len(e.scopes) == 0 {
				continue
			}
			if sectionTitleFor(e.scopes[0]) != title {
				continue
			}
			section = append(section, menuRow{entry: &e})
		}
		if len(section) == 0 {
			continue
		}
		sm.rows = append(sm.rows, menuRow{header: title})
		sm.rows = append(sm.rows, section...)
	}

	sm.rows = append(sm.rows, menuRow{header: "Reference"})
	for i := range menuEntries {
		e := menuEntries[i]
		if e.hidden || e.section != sectionReference {
			continue
		}
		sm.rows = append(sm.rows, menuRow{entry: &e})
	}

	sm.cursor = sm.firstEntryRow()
	sm.refreshPreview()
}

// openAt reopens the spotlight with the cursor restored to row (Task 4's
// return path). A row that no longer exists falls back to the first entry.
func (sm *spotlightModel) openAt(row int) {
	sm.openSpotlight()
	if row >= 0 && row < len(sm.rows) && sm.rows[row].entry != nil {
		sm.cursor = row
		sm.refreshPreview()
	}
}

func (sm *spotlightModel) firstEntryRow() int {
	for i, r := range sm.rows {
		if r.entry != nil {
			return i
		}
	}
	return 0
}

func (sm *spotlightModel) moveCursor(dir int) {
	if dir > 0 {
		for i := sm.cursor + 1; i < len(sm.rows); i++ {
			if sm.rows[i].entry != nil {
				sm.cursor = i
				return
			}
		}
		return
	}
	for i := sm.cursor - 1; i >= 0; i-- {
		if sm.rows[i].entry != nil {
			sm.cursor = i
			return
		}
	}
}

func (sm *spotlightModel) selectedEntry() *menuEntry {
	if sm.cursor < 0 || sm.cursor >= len(sm.rows) {
		return nil
	}
	return sm.rows[sm.cursor].entry
}

func (sm *spotlightModel) selectedLabel() string {
	if e := sm.selectedEntry(); e != nil {
		return e.label
	}
	return ""
}

// handleKey routes list navigation and activation; preview scrolling is
// handled here too once -> has focused a reference preview.
func (sm *spotlightModel) handleKey(k tea.KeyMsg) tea.Cmd {
	switch k.String() {
	case "j", "down":
		if sm.focus == focusPreview {
			if sm.offset < sm.maxPreviewOffset() {
				sm.offset++
			}
			return nil
		}
		sm.moveCursor(1)
		sm.refreshPreview()
	case "k", "up":
		if sm.focus == focusPreview {
			if sm.offset > 0 {
				sm.offset--
			}
			return nil
		}
		sm.moveCursor(-1)
		sm.refreshPreview()
	case "g":
		sm.offset = 0
		if sm.focus == focusList {
			sm.cursor = sm.firstEntryRow()
			sm.refreshPreview()
		}
	case "right":
		if sm.focus == focusList {
			return sm.activate()
		}
	case "esc", "\\":
		if sm.focus == focusPreview {
			sm.focus = focusList
			sm.offset = 0
			return nil
		}
		sm.open = false
	case "left":
		// Inert in the list (it keeps its caret role in forms); returns from a
		// focused preview.
		if sm.focus == focusPreview {
			sm.focus = focusList
			sm.offset = 0
		}
	}
	return nil
}

// activate is ->: a reference entry focuses its preview for scrolling;
// anything else closes the spotlight and replays prelude + key through
// Model.handleKey, so the spotlight and the raw keypress share one path.
func (sm *spotlightModel) activate() tea.Cmd {
	e := sm.selectedEntry()
	if e == nil {
		return nil
	}
	if e.kind == kindReference {
		sm.focus = focusPreview
		sm.offset = 0
		return nil
	}
	row := sm.cursor
	sm.open = false
	var chain []string
	if len(e.scopes) > 0 {
		chain = preludeFor(e.scopes[0])
	}
	chain = append(append([]string{}, chain...), e.key)
	var cmds []tea.Cmd
	for _, seg := range chain {
		if c := sm.m.handleKey(keyMsgFromString(seg)); c != nil {
			cmds = append(cmds, c)
		}
	}
	if e.kind == kindDialog {
		sm.m.spotlightReturn = row // Task 4 consumes it; the field is declared below
	}
	return tea.Batch(cmds...)
}

// refreshPreview rebuilds the preview content for the cursor row: a
// reference entry gets its full text; anything else tries the live renderer
// registry, falling back to the summary line when no renderer is registered,
// the registry renderer produced nothing, or the region is too short to be
// worth it. Rebuilding from scratch (rather than caching) is what keeps this
// safe to call again on resize — SetSize calls it whenever the spotlight is
// open, so a stale wrap never survives a terminal resize.
func (sm *spotlightModel) refreshPreview() {
	sm.offset = 0
	sm.lines = nil
	e := sm.selectedEntry()
	if e == nil {
		return
	}
	w, h := sm.menuBoxWidth()-4, sm.previewHeight()
	if e.kind == kindReference {
		sm.lines = strings.Split(strings.TrimRight(sm.referenceText(e.ref, w), "\n"), "\n")
		return
	}
	if h >= minPreviewHeight {
		if fn, ok := previewRegistry()[previewKeyFor(*e)]; ok {
			if out := fn(sm.m, w, h); strings.TrimSpace(out) != "" {
				sm.lines = strings.Split(strings.TrimRight(out, "\n"), "\n")
				return
			}
		}
	}
	sm.lines = strings.Split(wordwrap.String(e.summary, w), "\n")
}

// referenceText returns kind's full reference content at width w. Lifted
// from the deleted openDetail: refreshPreview rebuilds it fresh on every
// call (including on resize), preserving openDetail's old guarantee that
// content is built at drill/hover time rather than cached against a width
// that can go stale.
func (sm *spotlightModel) referenceText(kind refKind, w int) string {
	switch kind {
	case refKeymap:
		return keymapReferenceText()
	case refParity:
		return parityTable
	case refConventions:
		return renderConventionsText(sm.m.styles, w, conventionsTextTUI)
	}
	return ""
}

// maxPreviewOffset is the highest scroll offset that still fills the preview
// region: enough to reach the end of sm.lines, but never past it, so the
// last screenful stays full instead of shrinking toward a single remaining
// line.
func (sm *spotlightModel) maxPreviewOffset() int {
	max := len(sm.lines) - sm.previewHeight()
	if max < 0 {
		max = 0
	}
	return max
}

// spotlightKeyCol is the fixed width of the key column. Key-first with a
// fixed column keeps the labels aligned, so the list reads as a keymap the
// user can learn, not as a menu with a key footnote.
const spotlightKeyCol = 8

// renderRow draws "  [a]     Add project" — cursor glyph, padded key, label.
func (sm *spotlightModel) renderRow(e *menuEntry, cursor bool, w int) string {
	glyph := "  "
	if cursor {
		glyph = "▸ "
	}
	key := ""
	if e.key != "" {
		key = "[" + e.key + "]"
	}
	if lipgloss.Width(key) < spotlightKeyCol {
		key += spaces(spotlightKeyCol - lipgloss.Width(key))
	} else {
		key += " "
	}
	return fitLine(glyph+key+e.label, w)
}

// menuBoxWidth is the overlay width: 60% of the terminal, at least 64, and
// never wider than the terminal minus the two side columns. Mirrors
// channelsModel.renderOverlay.
func (sm *spotlightModel) menuBoxWidth() int {
	bw := sm.m.width * 60 / 100
	if bw < 64 {
		bw = 64
	}
	if bw > sm.m.width-4 {
		bw = sm.m.width - 4
	}
	return bw
}

// spotlightHeight is the overlay's total height: most of the terminal, so the
// global list and its preview both have room.
func (sm *spotlightModel) spotlightHeight() int {
	h := sm.m.height - 6
	if h < 12 {
		h = 12
	}
	return h
}

// previewHeight is the bottom region's content height — roughly half the box,
// minus the divider and the footer.
func (sm *spotlightModel) previewHeight() int {
	h := sm.spotlightHeight()/2 - 2
	if h < 3 {
		h = 3
	}
	return h
}

// renderOverlay draws the list above and the preview below, separated by a
// divider. The list scrolls around the cursor when it exceeds its region;
// the preview shows sm.lines from sm.offset.
func (sm *spotlightModel) renderOverlay() string {
	styles := sm.m.styles
	bw := sm.menuBoxWidth()
	inner := bw - 4
	total := sm.spotlightHeight()
	previewH := sm.previewHeight()
	listH := total - previewH - 4 // divider + footer + the box's two border rows
	if listH < 3 {
		listH = 3
	}

	// Scroll the list so the cursor stays inside its region.
	start := 0
	if sm.cursor >= listH {
		start = sm.cursor - listH + 1
	}
	end := start + listH
	if end > len(sm.rows) {
		end = len(sm.rows)
	}

	var body strings.Builder
	for i := start; i < end; i++ {
		row := sm.rows[i]
		if row.entry == nil {
			body.WriteString(sectionDivider(styles, inner, row.header) + "\n")
			continue
		}
		line := sm.renderRow(row.entry, i == sm.cursor, inner)
		if i == sm.cursor {
			line = styles.RowCursor.Render(line)
		} else {
			line = styles.Body.Render(line)
		}
		body.WriteString(line + "\n")
	}
	// padToHeight pads a rendered block to a total *line count*, not a count
	// of blank lines to append — passing "" through it would swallow the
	// trailing newline and merge the next divider onto this line, so the
	// remaining rows are padded directly with newlines instead.
	body.WriteString(strings.Repeat("\n", listH-(end-start)))

	label := "Preview"
	if sm.focus == focusPreview {
		label = "Preview ·"
	}
	body.WriteString(sectionDivider(styles, inner, label) + "\n")

	if sm.offset > sm.maxPreviewOffset() {
		sm.offset = sm.maxPreviewOffset()
	}
	if sm.offset < 0 {
		sm.offset = 0
	}
	shown := 0
	for i := sm.offset; i < len(sm.lines) && shown < previewH; i++ {
		body.WriteString(fitLine(sm.lines[i], inner) + "\n")
		shown++
	}
	body.WriteString(strings.Repeat("\n", previewH-shown))

	footer := "[↑/↓]move  [→]open  [Esc]close"
	if sm.focus == focusPreview {
		footer = "[j/k]scroll  [Esc]back"
	}
	body.WriteString(styles.KeyMenuDim.Render(footer))

	return titledBoxHeight(styles.DialogBody, bw, "Spotlight", body.String(), total)
}
