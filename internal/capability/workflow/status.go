package workflow

// StatusNamespace is the label suffix prefix this capability owns. It is the
// only place the string "status" appears in the capability; CLI verbs and the
// TUI reference these constants, never the literal.
const StatusNamespace = "status"

// Status values are the seeded lifecycle states the workflow capability
// transitions between. They match the seeded status:* labels.
// Note: status:todo is deliberately absent from the seed (see
// internal/capability/workflow/vocabulary_test.go
// TestEnsureVocabularySeedsStatusLabels), so there is no StatusTodo and no
// queue verb.
const (
	StatusOpen       = "open"
	StatusInProgress = "in-progress"
	StatusBlocked    = "blocked"
	StatusDone       = "done"
)
