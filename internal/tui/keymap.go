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

// menuGroupID identifies which spotlight group a menu entry belongs to.
// groupNone marks the inline root entries (sectionViews) that render
// directly at the spotlight's top level rather than under a group.
type menuGroupID int

const (
	groupNone menuGroupID = iota // inline root entries (sectionViews)
	groupProject
	groupTask
	groupBoard
	groupReference
)

// menuGroup is one entry in the spotlight's top-level group list.
type menuGroup struct {
	id    menuGroupID
	icon  string
	label string
	hint  string // one-line summary shown in the group's preview header
}

var menuGroups = []menuGroup{
	{id: groupProject, icon: "▤", label: "Project", hint: "Create, select, rename, dispatch, remove projects."},
	{id: groupTask, icon: "☰", label: "Task", hint: "Add a task, or search a task to act on it."},
	{id: groupBoard, icon: "▦", label: "Board", hint: "Author boards and labels, pin jump slots, seed vocabulary."},
	{id: groupReference, icon: "§", label: "Reference", hint: "Keymap, CLI parity, conventions."},
}

// groupByID returns the menuGroup for id, or nil for groupNone (the inline
// root) or any id not present in menuGroups.
func groupByID(id menuGroupID) *menuGroup {
	for i := range menuGroups {
		if menuGroups[i].id == id {
			return &menuGroups[i]
		}
	}
	return nil
}

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
	group        menuGroupID // groupNone for sectionViews (inline root); else a registered group
	icon         string      // single-width glyph; unset only for hidden rows
	hidden       bool // keymap-reference-only; never a spotlight row
	needsProject bool // shown only when a project scope exists (capabilities switcher)
}

// menuEntries is the single declarative menu entry table. Transcribed from
// the old keymapRows and the three deleted per-pane status hints: the
// keymap reference and the \ spotlight both render from this table, so the
// advertised surface can never drift from the real bindings.
var menuEntries = []menuEntry{
	// Views (global overlays; needsProject gates the capabilities switcher)
	// Dispatch a session's icon is ↯ (not the brief's ⚡): lipgloss measures
	// ⚡ (U+26A1) at width 2, which would fail the single-width invariant.
	{key: "D", label: "Dispatch a session", summary: "Launch an agent session — pick persona, agent, and target.", kind: kindDialog, section: sectionViews, group: groupNone, icon: "↯"},
	{key: "E", label: "Channels", summary: "Channel health for the selected project: records, wiring, stamps.", kind: kindDialog, section: sectionViews, group: groupNone, icon: "⇄"},
	{key: "V", label: "Personas", summary: "The registered personas and the prompt each one launches with.", kind: kindDialog, section: sectionViews, group: groupNone, icon: "◉"},
	// scopes here does not filter the list (Views entries are always shown
	// per needsProject alone) — it feeds preludeFor so activation focuses the
	// Tasks pane before replaying "C", matching the handler's own guard
	// (app.go's "C" case requires paneTasks focus and a project scope).
	// Without it, activating from the Projects pane would replay a bare "C"
	// into whatever pane is focused and silently no-op.
	{key: "C", label: "Capabilities", summary: "Enable, disable, and switch the project's capabilities.", kind: kindDialog, scopes: []menuScope{scopeTasksList}, section: sectionViews, needsProject: true, group: groupNone, icon: "⚙"},
	{key: "T", label: "Cycle theme", summary: "Step to the next colour theme.", kind: kindAction, section: sectionViews, group: groupNone, icon: "◐"},

	// Actions — projects list
	{key: "a", label: "Add project", summary: "Create a project from a 3-6 letter code and a display name.", kind: kindDialog, scopes: []menuScope{scopeProjectsList}, section: sectionActions, group: groupProject, icon: "+"},
	{key: "s", label: "Select project", summary: "Scope the Tasks pane and the status bar to the highlighted project.", kind: kindAction, scopes: []menuScope{scopeProjectsList}, section: sectionActions, group: groupProject, icon: "✓"},
	{key: "x", label: "Remove project", summary: "Delete the highlighted project after a confirm.", kind: kindDialog, scopes: []menuScope{scopeProjectsList}, section: sectionActions, group: groupProject, icon: "✗"},

	// Actions — projects detail
	{key: "n", label: "Set project name", summary: "Rename the open project; the code is immutable.", kind: kindDialog, scopes: []menuScope{scopeProjectsDetail}, section: sectionActions, group: groupProject, icon: "✎"},

	// Actions — projects persona drill
	// Dispatch this persona's icon is ↯ for the same reason as "Dispatch a
	// session" above — see the note on the Views entry.
	{key: "d", label: "Dispatch this persona", summary: "Open the dispatch dialog preset to the drilled persona.", kind: kindDialog, scopes: []menuScope{scopeProjectsDrill}, section: sectionActions, group: groupProject, icon: "↯"},

	// Actions — tasks list
	{key: "a", label: "Add task", summary: "Create a task with a title, optional description, and labels.", kind: kindDialog, scopes: []menuScope{scopeTasksList}, section: sectionActions, group: groupTask, icon: "+"},

	// Actions — task detail
	{key: "e", label: "Edit title", summary: "Rewrite the open task's title.", kind: kindDialog, scopes: []menuScope{scopeTasksDetail}, section: sectionActions, group: groupTask, icon: "✎"},
	{key: "d", label: "Edit description", summary: "Rewrite the open task's running description.", kind: kindDialog, scopes: []menuScope{scopeTasksDetail}, section: sectionActions, group: groupTask, icon: "¶"},
	{key: "b", label: "Add label", summary: "Attach a label to the open task.", kind: kindDialog, scopes: []menuScope{scopeTasksDetail}, section: sectionActions, group: groupTask, icon: "⚑"},
	{key: "B", label: "Remove label", summary: "Detach a label from the open task.", kind: kindDialog, scopes: []menuScope{scopeTasksDetail}, section: sectionActions, group: groupTask, icon: "⚐"},
	{key: "M", label: "Add comment", summary: "Append a classified comment to the open task's thread.", kind: kindDialog, scopes: []menuScope{scopeTasksDetail}, section: sectionActions, group: groupTask, icon: "✉"},
	{key: "x", label: "Remove task", summary: "Delete the open task after a confirm.", kind: kindDialog, scopes: []menuScope{scopeTasksDetail}, section: sectionActions, group: groupTask, icon: "✗"},

	// Actions — boards. Pin/unpin board lives here for table readability
	// (grouped with the rest of the boards block) even though its scope and
	// prelude are still scopeTasksList — it routes from the tasks list, not
	// the boards ring, and that behavior is unchanged.
	{key: "p", label: "Pin/unpin board", summary: "Pin the selected board to a !1..!9 jump slot, or unpin it.", kind: kindAction, scopes: []menuScope{scopeTasksList}, section: sectionActions, group: groupBoard, icon: "#"},
	{key: "n", label: "New board", summary: "Author a board from a label expression.", kind: kindDialog, scopes: []menuScope{scopeBoards}, section: sectionActions, group: groupBoard, icon: "+"},
	{key: "e", label: "Edit board", summary: "Edit the selected board's name and expression.", kind: kindDialog, scopes: []menuScope{scopeBoards}, section: sectionActions, group: groupBoard, icon: "✎"},
	{key: "d", label: "Describe label", summary: "Record what the selected label is for.", kind: kindDialog, scopes: []menuScope{scopeBoards}, section: sectionActions, group: groupBoard, icon: "❝"},
	{key: "l", label: "Remove label", summary: "Delete the selected label from the project.", kind: kindDialog, scopes: []menuScope{scopeBoards}, section: sectionActions, group: groupBoard, icon: "✗"},
	{key: "S", label: "Seed vocabulary", summary: "Ensure the enabled capabilities' labels and boards exist.", kind: kindAction, scopes: []menuScope{scopeBoards}, section: sectionActions, group: groupBoard, icon: "↻"},

	// Reference (no keys; -> focuses the preview)
	{label: "Keymap reference", summary: "Every binding, flat: Key | Where | Action.", kind: kindReference, section: sectionReference, ref: refKeymap, group: groupReference, icon: "⌨"},
	{label: "CLI ↔ TUI parity", summary: "Which CLI command each TUI affordance corresponds to.", kind: kindReference, section: sectionReference, ref: refParity, group: groupReference, icon: "⇌"},
	{label: "Conventions", summary: "The substrate primer: what ATM is, labels, capabilities, actors.", kind: kindReference, section: sectionReference, ref: refConventions, group: groupReference, icon: "§"},

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
	{key: "space", label: "Scroll detail / page", hidden: true},
	{key: "!1..!9 / !0", label: "Jump to pinned / center board", hidden: true},
	{key: "q / ctrl+c", label: "Quit", hidden: true},
	{key: "s", label: "Cycle task sort (tasks list)", hidden: true},
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
