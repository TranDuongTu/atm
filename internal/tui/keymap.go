package tui

import (
	"github.com/charmbracelet/bubbletea"
)

// menuScope identifies a context an action entry belongs to. The spotlight
// list itself is global and never filters on it; scopes instead feed the
// keymap reference's Where column, the parity probe IDs, and — via
// preludeFor — the replay prelude and the section header a row renders
// under.
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

// menuSection groups entries in the spotlight overlay: Actions are the
// per-pane action rows (each carries a scope), Views are global overlays and
// panes, Reference focuses a scrollable reference preview.
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

// entryKind decides what the spotlight's -> does with an entry and how the
// preview region renders it.
type entryKind int

const (
	kindAction    entryKind = iota // replay executes immediately (theme, sort, pin, select)
	kindDialog                     // replay leaves an overlay, form, or confirm open
	kindReference                  // no key; -> focuses a scrollable reference preview
)

// menuEntry is one row of the single declarative menu entry table: the
// source of truth for key labels and menu structure. Activating a keyed
// entry replays its scope's prelude plus that key through handleKey, so the
// spotlight and a direct keypress share one behavior path; the table never
// executes behavior itself.
type menuEntry struct {
	key          string // display + replay string; "" for reference entries
	label        string
	summary      string // one line; the preview when no live renderer is registered
	kind         entryKind
	scopes       []menuScope // the contexts the entry belongs to; nil for Views/Reference
	section      menuSection
	ref          refKind
	hidden       bool // keymap-reference-only; never a spotlight row
	needsProject bool // shown only when a project scope exists (capabilities switcher)
}

// menuEntries is the single declarative menu entry table. Transcribed from
// the old keymapRows and the three deleted per-pane status hints: the
// keymap reference and the \ spotlight both render from this table, so the
// advertised surface can never drift from the real bindings.
var menuEntries = []menuEntry{
	// Views (global overlays; needsProject gates the capabilities switcher)
	{key: "D", label: "Dispatch a session", summary: "Launch an agent session — pick persona, agent, and target.", kind: kindDialog, section: sectionViews},
	{key: "E", label: "Channels", summary: "Channel health for the selected project: records, wiring, stamps.", kind: kindDialog, section: sectionViews},
	{key: "V", label: "Personas", summary: "The registered personas and the prompt each one launches with.", kind: kindDialog, section: sectionViews},
	// scopes here does not filter the list (Views entries are always shown
	// per needsProject alone) — it feeds preludeFor so activation focuses the
	// Tasks pane before replaying "C", matching the handler's own guard
	// (app.go's "C" case requires paneTasks focus and a project scope).
	// Without it, activating from the Projects pane would replay a bare "C"
	// into whatever pane is focused and silently no-op.
	{key: "C", label: "Capabilities", summary: "Enable, disable, and switch the project's capabilities.", kind: kindDialog, scopes: []menuScope{scopeTasksList}, section: sectionViews, needsProject: true},
	{key: "T", label: "Cycle theme", summary: "Step to the next colour theme.", kind: kindAction, section: sectionViews},

	// Actions — projects list
	{key: "a", label: "Add project", summary: "Create a project from a 3-6 letter code and a display name.", kind: kindDialog, scopes: []menuScope{scopeProjectsList}, section: sectionActions},
	{key: "s", label: "Select project", summary: "Scope the Tasks pane and the status bar to the highlighted project.", kind: kindAction, scopes: []menuScope{scopeProjectsList}, section: sectionActions},
	{key: "x", label: "Remove project", summary: "Delete the highlighted project after a confirm.", kind: kindDialog, scopes: []menuScope{scopeProjectsList}, section: sectionActions},

	// Actions — projects detail
	{key: "n", label: "Set project name", summary: "Rename the open project; the code is immutable.", kind: kindDialog, scopes: []menuScope{scopeProjectsDetail}, section: sectionActions},
	{key: "c", label: "Toggle capability", summary: "Cycle the capability cursor to the next registered capability; space toggles it enabled/disabled.", kind: kindAction, scopes: []menuScope{scopeProjectsDetail}, section: sectionActions},

	// Actions — projects persona drill
	{key: "d", label: "Dispatch this persona", summary: "Open the dispatch dialog preset to the drilled persona.", kind: kindDialog, scopes: []menuScope{scopeProjectsDrill}, section: sectionActions},

	// Actions — tasks list
	{key: "a", label: "Add task", summary: "Create a task with a title, optional description, and labels.", kind: kindDialog, scopes: []menuScope{scopeTasksList}, section: sectionActions},
	{key: "s", label: "Cycle sort", summary: "Step the task list through its sort orders.", kind: kindAction, scopes: []menuScope{scopeTasksList}, section: sectionActions},
	{key: "S", label: "Re-ensure capability vocabulary", summary: "Re-seed the enabled capabilities' labels and boards for this project.", kind: kindAction, scopes: []menuScope{scopeTasksList}, section: sectionActions},
	{key: "p", label: "Pin/unpin board", summary: "Pin the selected board to a !1..!9 jump slot, or unpin it.", kind: kindAction, scopes: []menuScope{scopeTasksList}, section: sectionActions},

	// Actions — task detail
	{key: "e", label: "Edit title", summary: "Rewrite the open task's title.", kind: kindDialog, scopes: []menuScope{scopeTasksDetail}, section: sectionActions},
	{key: "d", label: "Edit description", summary: "Rewrite the open task's running description.", kind: kindDialog, scopes: []menuScope{scopeTasksDetail}, section: sectionActions},
	{key: "b", label: "Add label", summary: "Attach a label to the open task.", kind: kindDialog, scopes: []menuScope{scopeTasksDetail}, section: sectionActions},
	{key: "B", label: "Remove label", summary: "Detach a label from the open task.", kind: kindDialog, scopes: []menuScope{scopeTasksDetail}, section: sectionActions},
	{key: "M", label: "Add comment", summary: "Append a classified comment to the open task's thread.", kind: kindDialog, scopes: []menuScope{scopeTasksDetail}, section: sectionActions},
	{key: "H", label: "History overlay", summary: "Show the open task's full event history inside the task detail view.", kind: kindAction, scopes: []menuScope{scopeTasksDetail}, section: sectionActions},
	{key: "x", label: "Remove task", summary: "Delete the open task after a confirm.", kind: kindDialog, scopes: []menuScope{scopeTasksDetail}, section: sectionActions},

	// Actions — boards
	{key: "n", label: "New board", summary: "Author a board from a label expression.", kind: kindDialog, scopes: []menuScope{scopeBoards}, section: sectionActions},
	{key: "e", label: "Edit board", summary: "Edit the selected board's name and expression.", kind: kindDialog, scopes: []menuScope{scopeBoards}, section: sectionActions},
	{key: "d", label: "Describe label", summary: "Record what the selected label is for.", kind: kindDialog, scopes: []menuScope{scopeBoards}, section: sectionActions},
	{key: "l", label: "Remove label", summary: "Delete the selected label from the project.", kind: kindDialog, scopes: []menuScope{scopeBoards}, section: sectionActions},
	{key: "S", label: "Seed vocabulary", summary: "Ensure the enabled capabilities' labels and boards exist.", kind: kindAction, scopes: []menuScope{scopeBoards}, section: sectionActions},

	// Reference (no keys; -> focuses the preview)
	{label: "Keymap reference", summary: "Every binding, flat: Key | Where | Action.", kind: kindReference, section: sectionReference, ref: refKeymap},
	{label: "CLI ↔ TUI parity", summary: "Which CLI command each TUI affordance corresponds to.", kind: kindReference, section: sectionReference, ref: refParity},
	{label: "Conventions", summary: "The substrate primer: what ATM is, labels, capabilities, actors.", kind: kindReference, section: sectionReference, ref: refConventions},

	// Hidden: documented in the keymap reference, never spotlight rows.
	{key: "1", label: "Projects pane", hidden: true},
	{key: "2", label: "Tasks pane", hidden: true},
	{key: "ctrl+right", label: "Drill into persona activity", hidden: true},
	{key: "ctrl+left", label: "Back from persona detail", hidden: true},
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

// preludeFor is the key chain that establishes a scope before an entry's own
// key is replayed. Deriving it from the scope (rather than storing it per
// entry) makes it impossible for an entry to carry a prelude that contradicts
// the scope it is filed under.
func preludeFor(s menuScope) []string {
	switch s {
	case scopeProjectsList:
		return []string{"1"}
	case scopeProjectsDetail:
		return []string{"1", "enter"}
	case scopeProjectsDrill:
		return []string{"1", "ctrl+right"}
	case scopeTasksList, scopeBoards:
		// The boards ring is part of the Tasks pane's list view: the board
		// keys route to boardsModel from there, so both scopes share a prelude.
		return []string{"2"}
	case scopeTasksDetail:
		return []string{"2", "enter"}
	}
	return nil
}

// sectionTitleFor is the spotlight header a scope's entries render under.
func sectionTitleFor(s menuScope) string {
	switch s {
	case scopeProjectsList, scopeProjectsDetail, scopeProjectsDrill:
		return "Projects"
	case scopeTasksList, scopeTasksDetail:
		return "Tasks"
	case scopeBoards:
		return "Boards"
	}
	return ""
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
