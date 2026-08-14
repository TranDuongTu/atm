package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// menuModel is the [?] main menu overlay: the single discovery surface for
// every TUI action. Rows are built at open from menuEntries filtered by the
// model's current scope; activating a keyed entry closes the menu and
// replays that key through Model.handleKey so the menu can never drift from
// the real bindings. Reference entries drill into scrollable detail views.
type menuModel struct {
	m      *Model
	open   bool
	cursor int
	rows   []menuRow
	view   refKind // refNone = the list; otherwise the open detail view
	offset int
	lines  []string // detail content, one string per line
}

// menuRow is one rendered line: a section header (entry == nil) or an entry.
type menuRow struct {
	header string
	entry  *menuEntry
}

// currentScopes resolves the focused pane and its drill state into the menu
// scopes whose Actions apply right now.
func (m *Model) currentScopes() []menuScope {
	switch m.focused {
	case paneProjects:
		if m.projects.personaDrilled {
			return []menuScope{scopeProjectsDrill}
		}
		if m.projects.view == pViewDetail {
			return []menuScope{scopeProjectsDetail}
		}
		return []menuScope{scopeProjectsList}
	case paneTasks:
		if m.tasks.view == tViewDetail {
			return []menuScope{scopeTasksDetail}
		}
		return []menuScope{scopeTasksList}
	}
	return nil
}

// scopeIntersects reports whether two scope sets share any member.
func scopeIntersects(a, b []menuScope) bool {
	for _, x := range a {
		for _, y := range b {
			if x == y {
				return true
			}
		}
	}
	return false
}

// openMenu rebuilds the row list from menuEntries for the model's current
// scopes: Actions under a header (omitted when nothing applies), Views flat,
// Reference under a header. Hidden entries never become rows. The cursor
// starts on the first entry row.
func (mm *menuModel) openMenu() {
	scopes := mm.m.currentScopes()
	mm.rows = nil
	mm.cursor = 0
	mm.view = refNone
	mm.offset = 0
	mm.lines = nil
	mm.open = true

	var actions []menuEntry
	for _, e := range menuEntries {
		if e.hidden {
			continue
		}
		if e.section == sectionActions && scopeIntersects(e.scopes, scopes) {
			actions = append(actions, e)
		}
	}
	if len(actions) > 0 {
		mm.rows = append(mm.rows, menuRow{header: "Actions"})
		for _, e := range actions {
			mm.rows = append(mm.rows, menuRow{entry: &e})
		}
	}

	for _, e := range menuEntries {
		if e.hidden {
			continue
		}
		if e.section != sectionViews {
			continue
		}
		if e.needsProject && mm.m.projectScope == "" && mm.m.overlayProject() == "" {
			continue
		}
		mm.rows = append(mm.rows, menuRow{entry: &e})
	}

	mm.rows = append(mm.rows, menuRow{header: "Reference"})
	for _, e := range menuEntries {
		if e.hidden {
			continue
		}
		if e.section == sectionReference {
			mm.rows = append(mm.rows, menuRow{entry: &e})
		}
	}

	mm.cursor = mm.firstEntryRow()
}

func (mm *menuModel) firstEntryRow() int {
	for i, r := range mm.rows {
		if r.entry != nil {
			return i
		}
	}
	return 0
}

func (mm *menuModel) moveCursor(dir int) {
	if dir > 0 {
		for i := mm.cursor + 1; i < len(mm.rows); i++ {
			if mm.rows[i].entry != nil {
				mm.cursor = i
				return
			}
		}
		return
	}
	for i := mm.cursor - 1; i >= 0; i-- {
		if mm.rows[i].entry != nil {
			mm.cursor = i
			return
		}
	}
}

// handleKey routes keys in the list view (move/activate/navigate) and the
// detail views (scroll/back). Activating a keyed entry closes the menu and
// replays the entry's key through Model.handleKey so the menu shares one
// behavior path with the direct keypress.
func (mm *menuModel) handleKey(k tea.KeyMsg) tea.Cmd {
	switch k.String() {
	case "j", "down":
		if mm.view != refNone {
			mm.offset++
			return nil
		}
		mm.moveCursor(1)
	case "k", "up":
		if mm.view != refNone {
			if mm.offset > 0 {
				mm.offset--
			}
			return nil
		}
		mm.moveCursor(-1)
	case "g":
		// Top of the current view: the detail body's first line in a detail
		// view, the first entry row on the list.
		mm.offset = 0
		if mm.view == refNone {
			mm.cursor = mm.firstEntryRow()
		}
	case "enter", "right":
		if mm.view != refNone || len(mm.rows) == 0 {
			return nil
		}
		row := mm.rows[mm.cursor]
		if row.entry == nil {
			return nil
		}
		if row.entry.key != "" {
			mm.open = false
			return mm.m.handleKey(keyMsgFromString(row.entry.key))
		}
		mm.openDetail(row.entry.ref)
	case "esc", "left":
		if mm.view != refNone {
			mm.view = refNone
			mm.offset = 0
			mm.lines = nil
		} else {
			mm.open = false
		}
	}
	return nil
}

// openDetail switches the menu into one of the read-only reference views and
// snapshots its content lines. Content is built at drill time so a resize
// between open and drill is reflected.
func (mm *menuModel) openDetail(kind refKind) {
	mm.view = kind
	mm.offset = 0
	switch kind {
	case refKeymap:
		mm.lines = strings.Split(strings.TrimRight(keymapReferenceText(), "\n"), "\n")
	case refParity:
		mm.lines = strings.Split(strings.TrimRight(parityTable, "\n"), "\n")
	case refConventions:
		bw := mm.menuBoxWidth()
		mm.lines = strings.Split(strings.TrimRight(renderConventionsText(mm.m.styles, bw-4, conventionsTextTUI), "\n"), "\n")
	}
}

// selectedLabel is the cursor row's entry label ("" on a header row) — used
// by the tests to walk the cursor.
func (mm *menuModel) selectedLabel() string {
	if mm.cursor < 0 || mm.cursor >= len(mm.rows) || mm.rows[mm.cursor].entry == nil {
		return ""
	}
	return mm.rows[mm.cursor].entry.label
}

// menuBoxWidth is the overlay width: 60% of the terminal, at least 64, and
// never wider than the terminal minus the two side columns. Mirrors
// channelsModel.renderOverlay.
func (mm *menuModel) menuBoxWidth() int {
	bw := mm.m.width * 60 / 100
	if bw < 64 {
		bw = 64
	}
	if bw > mm.m.width-4 {
		bw = mm.m.width - 4
	}
	return bw
}

func (mm *menuModel) title() string {
	switch mm.view {
	case refKeymap:
		return "Menu — Keymap"
	case refParity:
		return "Menu — CLI ↔ TUI"
	case refConventions:
		return "Menu — Conventions"
	}
	return "Menu"
}

// renderOverlay draws the entry list or the scrolled detail view in the same
// box shape as channelsModel.renderOverlay (60% width, min 64).
func (mm *menuModel) renderOverlay() string {
	styles := mm.m.styles
	bw := mm.menuBoxWidth()
	if mm.view != refNone {
		return mm.renderDetail(bw)
	}

	var body strings.Builder
	for i, row := range mm.rows {
		if row.entry == nil {
			body.WriteString(sectionDivider(styles, bw-4, row.header) + "\n")
			continue
		}
		glyph := "  "
		if i == mm.cursor {
			glyph = "▸ "
		}
		key := ""
		if row.entry.key != "" {
			key = "[" + row.entry.key + "]"
		}
		line := mm.renderRow(glyph+row.entry.label, key, bw-4)
		if i == mm.cursor {
			line = styles.RowCursor.Render(line)
		} else {
			line = styles.Body.Render(line)
		}
		body.WriteString(line + "\n")
	}
	body.WriteString("\n" + styles.KeyMenuDim.Render("[↑/↓]move  [Enter/→]open  [Esc]close"))
	height := len(mm.rows) + 5
	if height > mm.m.height-2 {
		height = mm.m.height - 2
	}
	if height < 6 {
		height = 6
	}
	return titledBoxHeight(styles.DialogBody, bw, mm.title(), body.String(), height)
}

// renderRow pads a menu row so the [key] column right-aligns to the inner
// width; overlong labels are truncated first so the key never spills past the
// box edge.
func (mm *menuModel) renderRow(label, key string, w int) string {
	line := fitLine(label, w)
	if key == "" || lipgloss.Width(line)+1+lipgloss.Width(key) > w {
		return line
	}
	return line + spaces(w-lipgloss.Width(line)-lipgloss.Width(key)) + key
}

// renderDetail draws the open reference view scrolled by offset, with the
// footer pinned below the content window.
func (mm *menuModel) renderDetail(bw int) string {
	styles := mm.m.styles
	height := mm.m.height - 8
	if height < 8 {
		height = 8
	}
	innerH := height - 2
	if mm.offset > len(mm.lines)-1 {
		mm.offset = len(mm.lines) - 1
	}
	if mm.offset < 0 {
		mm.offset = 0
	}
	// Reserve the blank separator and footer lines so they never scroll away.
	end := mm.offset + innerH - 2
	if end > len(mm.lines) {
		end = len(mm.lines)
	}
	var body strings.Builder
	for _, ln := range mm.lines[mm.offset:end] {
		body.WriteString(fitLine(ln, bw-4) + "\n")
	}
	body.WriteString("\n" + styles.KeyMenuDim.Render("[j/k]scroll  [Esc]back"))
	return titledBoxHeight(styles.DialogBody, bw, mm.title(), padToHeight(body.String(), innerH), height)
}
