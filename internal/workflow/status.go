// internal/workflow/status.go
package workflow

// StatusNamespace is the label suffix prefix this capability owns. It is the
// only place the string "status" appears in the capability; CLI verbs and the
// TUI reference these constants, never the literal.
const StatusNamespace = "status"

// Status values are the seeded lifecycle states the workflow capability
// transitions between. They match internal/seed's status:* labels.
const (
	StatusOpen       = "open"
	StatusTodo       = "todo"
	StatusInProgress = "in-progress"
	StatusBlocked    = "blocked"
	StatusDone       = "done"
)
