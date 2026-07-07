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
// Columns: Key | Projects | Tasks | Labels | Detail.
type keyEntry struct {
	Key      string
	Projects string
	Tasks    string
	Labels   string
	Detail   string
}

// keymapRows is the verbatim Global keymap summary table from the mockup spec.
var keymapRows = []keyEntry{
	{"1/2/3", "focus pane", "focus pane", "focus pane", "focus pane"},
	{"j/k", "move cursor", "move cursor", "move cursor", "scroll"},
	{"g", "top of list", "top of list", "top of list", "top"},
	{"Enter", "open detail", "open detail / toggle group", "select namespace / open label detail", "confirm overlay"},
	{"Esc", "back", "back / cancel filter", "back", "back / cancel overlay"},
	{"/", "-", "edit filter", "-", "-"},
	{"c", "-", "clear filter", "-", "-"},
	{"s", "select project", "cycle sort", "-", "-"},
	{"S", "-", "-", "seed default labels", "-"},
	{"a", "add project", "add task", "add label", "-"},
	{"d", "-", "-", "describe label", "edit description (task)"},
	{"l", "-", "-", "remove label", "-"},
	{"x", "remove project (confirm)", "-", "-", "remove task (confirm)"},
	{"e", "-", "-", "-", "edit title (task)"},
	{"b/B", "-", "-", "-", "add/remove label (task)"},
	{"N", "set name (project detail)", "-", "-", "-"},
	{"M", "-", "-", "-", "add comment (task)"},
	{"H", "toggle history (project detail)", "-", "-", "history overlay (task detail)"},
	{"T", "cycle theme", "cycle theme", "cycle theme", "cycle theme"},
	{"?", "open keys help", "open keys help", "open keys help", "close help overlay"},
	{"C", "open conventions", "open conventions", "open conventions", "close help overlay"},
	{"q / ctrl+c", "quit", "quit", "quit", "quit"},
	{"[ / ]", "prev/next page", "prev/next page", "prev/next page", "-"},
	{"PgDn/Space", "-", "-", "-", "scroll down"},
	{"PgUp", "-", "-", "-", "-"},
}
