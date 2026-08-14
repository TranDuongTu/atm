package tui

import (
	"github.com/charmbracelet/bubbletea"
)

// menuScope identifies a context whose actions the menu shows under the
// Actions section. An entry's scopes gate which contexts display it.
type menuScope int

const (
	scopeGlobal menuScope = iota
	scopeProjectsList
	scopeProjectsDetail
	scopeProjectsDrill
	scopeTasksList
	scopeTasksDetail
	scopeBoards
)

// menuSection groups entries in the menu overlay: Actions are contextual,
// Views are global overlays and panes, Reference opens menu detail views.
type menuSection int

const (
	sectionActions menuSection = iota
	sectionViews
	sectionReference
)

// refKind identifies which read-only reference view a menu entry opens.
type refKind int

const (
	refNone refKind = iota
	refKeymap
	refParity
	refConventions
)

// menuEntry is one row of the single declarative menu entry table: the
// source of truth for key labels and menu structure. Activating a keyed
// entry replays that key through handleKey, so menu and direct keypress
// share one behavior path; the table never executes behavior itself.
type menuEntry struct {
	key          string // display + replay string; "" for reference entries
	label        string
	scopes       []menuScope // which contexts show it under Actions; nil for Views/Reference
	section      menuSection
	ref          refKind
	hidden       bool // keymap-reference-only (navigation pairs); never a menu row
	needsProject bool // shown only when a project scope exists (capabilities switcher)
}

// menuEntries is the single declarative menu entry table. Transcribed from
// the old keymapRows and the three deleted per-pane status hints: the
// keymap reference and the [?] menu both render from this table, so the
// advertised surface can never drift from the real bindings.
var menuEntries = []menuEntry{
	// Views (global overlays and panes; needsProject gates the capabilities switcher)
	{key: "D", label: "Dispatch a session", section: sectionViews},
	{key: "E", label: "Channels", section: sectionViews},
	{key: "V", label: "Personas", section: sectionViews},
	{key: "C", label: "Capabilities", section: sectionViews, needsProject: true},
	{key: "T", label: "Cycle theme", section: sectionViews},
	{key: "1", label: "Projects pane", section: sectionViews},
	{key: "2", label: "Tasks pane", section: sectionViews},

	// Actions — projects list
	{key: "a", label: "Add project", scopes: []menuScope{scopeProjectsList}, section: sectionActions},
	{key: "s", label: "Select project", scopes: []menuScope{scopeProjectsList}, section: sectionActions},
	{key: "x", label: "Remove project", scopes: []menuScope{scopeProjectsList}, section: sectionActions},
	{key: "ctrl+right", label: "Drill into persona activity", scopes: []menuScope{scopeProjectsList}, section: sectionActions},

	// Actions — projects detail
	{key: "N", label: "Set project name", scopes: []menuScope{scopeProjectsDetail}, section: sectionActions},
	{key: "H", label: "Toggle history", scopes: []menuScope{scopeProjectsDetail}, section: sectionActions},
	{key: "c", label: "Toggle capability (switcher)", scopes: []menuScope{scopeProjectsDetail}, section: sectionActions},
	{key: "x", label: "Remove project", scopes: []menuScope{scopeProjectsDetail}, section: sectionActions},

	// Actions — projects persona drill
	{key: "d", label: "Dispatch this persona", scopes: []menuScope{scopeProjectsDrill}, section: sectionActions},
	{key: "ctrl+left", label: "Back from persona detail", scopes: []menuScope{scopeProjectsDrill}, section: sectionActions},

	// Actions — tasks list
	{key: "a", label: "Add task", scopes: []menuScope{scopeTasksList}, section: sectionActions},
	{key: "s", label: "Cycle sort", scopes: []menuScope{scopeTasksList}, section: sectionActions},
	{key: "S", label: "Re-ensure capability vocabulary", scopes: []menuScope{scopeTasksList}, section: sectionActions},
	{key: "p", label: "Pin/unpin board", scopes: []menuScope{scopeTasksList}, section: sectionActions},

	// Actions — task detail
	{key: "e", label: "Edit title", scopes: []menuScope{scopeTasksDetail}, section: sectionActions},
	{key: "d", label: "Edit description", scopes: []menuScope{scopeTasksDetail}, section: sectionActions},
	{key: "b", label: "Add label", scopes: []menuScope{scopeTasksDetail}, section: sectionActions},
	{key: "B", label: "Remove label", scopes: []menuScope{scopeTasksDetail}, section: sectionActions},
	{key: "M", label: "Add comment", scopes: []menuScope{scopeTasksDetail}, section: sectionActions},
	{key: "H", label: "History overlay", scopes: []menuScope{scopeTasksDetail}, section: sectionActions},
	{key: "x", label: "Remove task", scopes: []menuScope{scopeTasksDetail}, section: sectionActions},

	// Actions — boards
	{key: "n", label: "New board", scopes: []menuScope{scopeBoards}, section: sectionActions},
	{key: "e", label: "Edit board", scopes: []menuScope{scopeBoards}, section: sectionActions},
	{key: "d", label: "Describe label", scopes: []menuScope{scopeBoards}, section: sectionActions},
	{key: "l", label: "Remove label", scopes: []menuScope{scopeBoards}, section: sectionActions},
	{key: "S", label: "Seed vocabulary", scopes: []menuScope{scopeBoards}, section: sectionActions},

	// Reference (no keys; open menu detail views)
	{label: "Keymap reference", section: sectionReference, ref: refKeymap},
	{label: "CLI ↔ TUI parity", section: sectionReference, ref: refParity},
	{label: "Conventions", section: sectionReference, ref: refConventions},

	// Hidden: documented in the keymap reference, never menu rows.
	{key: "j/k", label: "Move cursor / scroll", hidden: true},
	{key: "g", label: "Top of list · plugin leader prefix", hidden: true},
	{key: "enter", label: "Open detail / confirm", hidden: true},
	{key: "esc", label: "Back / close overlay", hidden: true},
	{key: "[ / ]", label: "Prev/next board or page", hidden: true},
	{key: "shift+up/down", label: "Feed scroll / thumbnail cursor", hidden: true},
	{key: "shift+right/left", label: "Feed page / thumbnail drill", hidden: true},
	{key: "pgup/pgdown", label: "Page list / scroll detail", hidden: true},
	{key: "ctrl+up/down", label: "Scroll persona chart", hidden: true},
	{key: "A", label: "Toggle project art", hidden: true},
	{key: "space", label: "Toggle capability / scroll", hidden: true},
	{key: "!1..!9 / !0", label: "Jump to pinned / center board", hidden: true},
	{key: "q / ctrl+c", label: "Quit", hidden: true},
}

// keyMsgFromString synthesizes the tea.KeyMsg whose String() equals s, for
// menu replay. Only keys that appear in menuEntries need handling; the
// round-trip test enforces the mapping against the vendored bubbletea.
func keyMsgFromString(s string) tea.KeyMsg {
	switch s {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "ctrl+right":
		return tea.KeyMsg{Type: tea.KeyCtrlRight}
	case "ctrl+left":
		return tea.KeyMsg{Type: tea.KeyCtrlLeft}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}
