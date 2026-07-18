package tui

// keymap centralizes the global key bindings (mockup spec: "Global keymap
// summary"). Keys are matched as raw strings in handleKey; the strings here
// document the binding surface and drive the help overlay's Section 2.
//
// Per-tab semantics for dual-purpose keys (s, Enter, Esc, j/k) are dispatched
// by the active pane — this struct is the declarative reference.
type keymap struct{}

func defaultKeymap() keymap { return keymap{} }

// keyEntry is one row of the global keymap reference table (help Section 2).
// Columns: Key | Projects | Tasks | Boards | Detail.
type keyEntry struct {
	Key      string
	Projects string
	Tasks    string
	Boards   string
	Detail   string
}

// keymapRows is the verbatim Global keymap summary table from the mockup spec.
var keymapRows = []keyEntry{
	{"1/2", "focus pane", "focus pane", "-", "focus pane"},
	{"j/k", "move cursor", "move cursor", "-", "scroll"},
	{"g", "top of list", "top of list", "-", "top"},
	{"Enter", "open detail", "open detail", "-", "confirm overlay"},
	{"Esc", "back", "back", "-", "back / cancel overlay"},
	{"[ / ]", "prev/next page", "prev/next board", "-", "-"},
	{"Shift+Right/Left", "-", "drill board thumbnail in/out", "-", "-"},
	{"Shift+Up/Down", "-", "move thumbnail chart cursor", "-", "-"},
	{"PgDn/PgUp", "-", "page task list", "-", "scroll (detail)"},
	{"s", "select project", "cycle sort", "-", "-"},
	{"S", "-", "re-ensure capability vocabulary", "-", "-"},
	{"a", "add project", "add task", "-", "-"},
	{"n", "-", "new board", "-", "-"},
	{"e", "-", "edit board", "-", "edit title (task)"},
	{"d", "-", "describe label", "-", "edit description (task)"},
	{"l", "-", "remove label", "-", "-"},
	{"x", "remove project (confirm)", "-", "-", "remove task (confirm)"},
	{"b/B", "-", "-", "-", "add/remove label (task)"},
	{"p", "add persona", "pin board", "-", "-"},
	{"Shift-1..9", "-", "jump to pinned board", "-", "-"},
	{"Shift-0", "-", "focus center board", "-", "-"},
	{"N", "set name (project detail)", "-", "-", "-"},
	{"M", "-", "-", "-", "add comment (task)"},
	{"H", "toggle history (project detail)", "-", "-", "history overlay (task detail)"},
	{"P", "expand activity by persona", "-", "-", "-"},
	{"T", "cycle theme", "cycle theme", "cycle theme", "cycle theme"},
	{"?", "open keys help", "open keys help", "open keys help", "close help overlay"},
	{"C", "open conventions", "open conventions", "open conventions", "close help overlay"},
	{"g", "plugin prefix", "plugin prefix", "plugin prefix", "plugin prefix"},
	{"g 1", "open indexer overlay", "open indexer overlay", "open indexer overlay", "open indexer overlay"},
	{"q / ctrl+c", "quit", "quit", "quit", "quit"},
	{"Space", "-", "-", "-", "scroll down"},
}
