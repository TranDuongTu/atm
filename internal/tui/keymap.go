package tui

// keymap centralizes the global key bindings (mockup spec: "Global keymap
// summary"). Keys are matched as raw strings in handleKey; the strings here
// document the binding surface and drive the Help tab's Section 2.
//
// Per-tab semantics for dual-purpose keys (s, Enter, Esc, j/k) are dispatched
// by the active pane — this struct is the declarative reference.
type keymap struct{}

func defaultKeymap() keymap { return keymap{} }

// keyEntry is one row of the global keymap reference table (Help Section 2).
// Columns: Key | Projects | Tasks | Labels | Help | Detail.
type keyEntry struct {
	Key      string
	Projects string
	Tasks    string
	Labels   string
	Help     string
	Detail   string
}

// keymapRows is the verbatim Global keymap summary table from the mockup spec.
var keymapRows = []keyEntry{
	{"1/2/3/4", "switch tab", "switch tab", "switch tab", "switch tab", "switch tab"},
	{"j/k", "move cursor", "move cursor", "move cursor", "scroll", "scroll"},
	{"g", "top of list", "top of list", "top of list", "top", "top"},
	{"Enter", "open detail", "open detail / toggle group", "open label detail", "-", "confirm overlay"},
	{"Esc", "back", "back / cancel filter", "back", "-", "back / cancel overlay"},
	{"/", "-", "edit filter", "-", "-", "-"},
	{"s", "select project", "cycle sort", "-", "-", "-"},
	{"S", "-", "-", "seed default labels", "-", "-"},
	{"a", "add project", "add task", "add label", "-", "-"},
	{"d", "-", "-", "describe label", "-", "edit description (task)"},
	{"l", "-", "-", "remove label", "-", "-"},
	{"x", "remove project (confirm)", "-", "-", "-", "remove task (confirm)"},
	{"e", "-", "-", "-", "-", "edit title (task)"},
	{"b/B", "-", "-", "-", "-", "add/remove label (task)"},
	{"N", "set name (project detail)", "-", "-", "-", "-"},
	{"H", "toggle history (project detail)", "-", "-", "-", "-"},
	{"T", "cycle theme", "cycle theme", "cycle theme", "cycle theme", "cycle theme"},
	{"?", "toggle keymap overlay", "toggle keymap overlay", "toggle keymap overlay", "-", "toggle keymap overlay"},
	{"q / ctrl+c", "quit", "quit", "quit", "quit", "quit"},
	{"PgDn/Space", "-", "next page", "next page", "next page", "scroll down"},
	{"PgUp", "-", "prev page", "prev page", "-", "-"},
}
