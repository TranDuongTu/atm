// Package workflow owns the vocabulary for the TUI's default board surface
// and the status-transition paved road. It is a capability mirroring
// internal/contextmap: it ensures its own vocabulary idempotently, exposes
// intent-level verbs (see recorder.go / reporter.go), and owns the status
// label namespace. The store enforces nothing; this capability is a paved
// road, not a fence. A human may edit or delete any board or status label;
// the next project-select / label-seed re-ensures the vocabulary.
package workflow

import "atm/internal/core"

// BoardOpenTasks returns the full name of the Open Tasks board for a project.
// Callers select this board by name; they never reference the expression.
func BoardOpenTasks(code string) string { return code + ":open-tasks" }

// BoardBacklog returns the full name of the Backlog board: untriaged tasks
// carrying no status label. Surfaced so quick jottings (created with no
// labels) do not vanish from the default board ring.
func BoardBacklog(code string) string { return code + ":backlog" }

// BoardInProgressTasks returns the full name of the In-Progress board.
func BoardInProgressTasks(code string) string { return code + ":in-progress-tasks" }

func openTasksExpr() string       { return "status:open" }
func backlogExpr() string         { return "NOT status:*" }
func inProgressTasksExpr() string { return "status:in-progress" }

// EnsureVocabulary creates the three workflow boards with descriptions, if
// absent. Idempotent: LabelSeed upserts only when the label is absent, so a
// human's curated description is never overwritten. Self-bootstrapping: it
// does not assume `atm label seed` ran.
func EnsureVocabulary(s core.LabelService, code, actor string) error {
	boards := []struct{ name, desc, expr string }{
		{BoardBacklog(code), "tasks with no status label: incoming jottings awaiting triage. Review queue alongside open-tasks.", backlogExpr()},
		{BoardOpenTasks(code), "every open task: the project's active work. Default board in the TUI.", openTasksExpr()},
		{BoardInProgressTasks(code), "tasks someone is actively working on (status:in-progress).", inProgressTasksExpr()},
	}
	for _, b := range boards {
		if err := s.LabelSeed(b.name, b.desc, b.expr, actor); err != nil {
			return err
		}
	}
	return nil
}
