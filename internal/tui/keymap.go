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
type keyEntry struct {
	Key      string
	Projects string
	Tasks    string
	Help     string
	Detail   string
}

// keymapRows is the verbatim Global keymap summary table from the mockup spec.
var keymapRows = []keyEntry{
	{"1/2/3", "switch tab", "switch tab", "switch tab", "switch tab"},
	{"j/k", "move cursor", "move cursor", "scroll", "scroll"},
	{"g", "top of list", "top of list", "top", "top"},
	{"Enter", "open detail", "open detail / toggle group", "-", "confirm overlay"},
	{"Esc", "back", "back / cancel filter", "-", "back / cancel overlay"},
	{"/", "-", "edit filter", "-", "-"},
	{"s", "select project", "cycle sort", "-", "-"},
	{"a", "add project", "add task", "-", "-"},
	{"x", "remove project (confirm)", "-", "-", "remove task (confirm)"},
	{"e", "-", "-", "-", "edit title (task)"},
	{"d", "-", "-", "-", "edit description (task)"},
	{"b/B", "-", "-", "-", "add/remove label (task)"},
	{"L/l", "add/remove label (project detail)", "-", "-", "-"},
	{"N", "set name (project detail)", "-", "-", "-"},
	{"H", "toggle history (project detail)", "-", "-", "-"},
	{"?", "toggle keymap overlay", "toggle keymap overlay", "-", "toggle keymap overlay"},
	{"PgDn/Space", "-", "next page", "next page", "scroll down"},
	{"PgUp/b", "-", "prev page", "prev page", "-"},
}