// Package workflow owns the vocabulary for the TUI's default board surface.
// It is a minimal capability: it ensures the Open Tasks board exists
// idempotently (mirroring internal/contextmap), exposes no verbs, and owns no
// private data format. The board is a normal label with an expression; a human
// may edit or delete it (capability = paved road, not a fence). The next
// project-select re-ensures it.
package workflow

import "atm/internal/core"

// BoardOpenTasks returns the full name of the Open Tasks board for a project.
// Callers select this board by name; they never reference the expression.
func BoardOpenTasks(code string) string { return code + ":open-tasks" }

// openTasksExpr is the membership expression for the Open Tasks board. It lives
// here, not in the TUI render path, so the TUI never hardcodes a namespace.
func openTasksExpr() string { return "status:open" }

// EnsureVocabulary creates the Open Tasks board with a description, if absent.
// Idempotent: LabelSeed upserts only when the label is absent, so a human's
// curated description is never overwritten. Self-bootstrapping: it does not
// assume `atm label seed` ran.
func EnsureVocabulary(s core.LabelService, code, actor string) error {
	return s.LabelSeed(
		BoardOpenTasks(code),
		"every open task: the project's active work. Default board in the TUI.",
		openTasksExpr(),
		actor,
	)
}
