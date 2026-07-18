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

// BoardAllTasks returns the full name of the All Tasks board: every task in
// the project, including unlabeled naked jottings. Its membership predicate
// is the '*' tautology atom (internal/store/resolve.go). Surfaced as the
// TUI's default-selected board so the human's "browse recent activity"
// consult mode sees the whole project, not just status:open.
func BoardAllTasks(code string) string { return code + ":all-tasks" }

func openTasksExpr() string       { return "status:open" }
func backlogExpr() string         { return "NOT status:*" }
func inProgressTasksExpr() string { return "status:in-progress" }
func allTasksExpr() string        { return "*" }

// EnsureVocabulary seeds this capability's full vocabulary: the status:*
// namespace it owns (absorbed from the deleted internal/seed default set)
// and the four workflow boards. Idempotent: LabelSeed upserts only when the
// label is absent, so a human's curated description is never overwritten. It
// returns the board labels (Expr != "") it owns, in the documented order.
func EnsureVocabulary(s core.LabelService, code, actor string) ([]core.Label, error) {
	stored := []struct{ suffix, desc string }{
		{"status:*", "lifecycle state of a task; exactly one status label should be present"},
		{"status:open", "workflow state: open; task is not started or is being considered"},
		{"status:in-progress", "workflow state: in-progress; someone is actively working on this"},
		{"status:blocked", "workflow state: blocked; task cannot proceed pending something else"},
		{"status:done", "workflow state: done; task is complete"},
	}
	for _, l := range stored {
		if err := s.LabelSeed(code+":"+l.suffix, l.desc, "", actor); err != nil {
			return nil, err
		}
	}
	boards := []struct{ name, desc, expr string }{
		{BoardBacklog(code), "tasks with no status label: incoming jottings awaiting triage. Review queue alongside open-tasks.", backlogExpr()},
		{BoardOpenTasks(code), "every open task: the project's active work.", openTasksExpr()},
		{BoardInProgressTasks(code), "tasks someone is actively working on (status:in-progress).", inProgressTasksExpr()},
		{BoardAllTasks(code), "every task in the project, ordered by recent activity. Default board in the TUI.", allTasksExpr()},
	}
	out := make([]core.Label, 0, len(boards))
	for _, b := range boards {
		if err := s.LabelSeed(b.name, b.desc, b.expr, actor); err != nil {
			return nil, err
		}
		out = append(out, core.Label{Name: b.name, Description: b.desc, Expr: b.expr})
	}
	return out, nil
}
