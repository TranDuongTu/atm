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

// vocabulary is the single literal list every contract method derives from:
// stored/namespace labels first (Expr == ""), then the four boards, in seed
// order. Ownership (Vocabulary), ring display (Exposed), and seeding
// (EnsureVocabulary) all read this list, so they cannot diverge.
func vocabulary(code string) []core.Label {
	return []core.Label{
		{Name: code + ":status:*", Description: "lifecycle state of a task; exactly one status label should be present"},
		{Name: code + ":status:open", Description: "workflow state: open; task is not started or is being considered"},
		{Name: code + ":status:in-progress", Description: "workflow state: in-progress; someone is actively working on this"},
		{Name: code + ":status:blocked", Description: "workflow state: blocked; task cannot proceed pending something else"},
		{Name: code + ":status:done", Description: "workflow state: done; task is complete"},
		{Name: code + ":priority:*", Description: "urgency ranking for planning; at most one priority label per task, absent means default priority"},
		{Name: code + ":priority:high", Description: "planning priority: high; do this first, everything untagged is default priority"},
		{Name: code + ":priority:medium", Description: "planning priority: medium; do after high-priority work"},
		{Name: code + ":priority:low", Description: "planning priority: low; do when no higher-priority work remains"},
		{Name: BoardBacklog(code), Description: "tasks with no status label: incoming jottings awaiting triage. Review queue alongside open-tasks.", Expr: backlogExpr()},
		{Name: BoardOpenTasks(code), Description: "every open task: the project's active work.", Expr: openTasksExpr()},
		{Name: BoardInProgressTasks(code), Description: "tasks someone is actively working on (status:in-progress).", Expr: inProgressTasksExpr()},
		{Name: BoardAllTasks(code), Description: "every task in the project, ordered by recent activity. Default board in the TUI.", Expr: allTasksExpr()},
	}
}

// Vocabulary returns every label this capability owns for code. Pure.
func Vocabulary(code string) []core.Label { return vocabulary(code) }

// Exposed returns the labels this capability surfaces in the TUI ring, in
// preferred ring order: the four boards, then the two namespaces it owns.
func Exposed(code string) []core.Label {
	byName := map[string]core.Label{}
	for _, l := range vocabulary(code) {
		byName[l.Name] = l
	}
	names := []string{
		BoardAllTasks(code), BoardOpenTasks(code), BoardInProgressTasks(code), BoardBacklog(code),
		code + ":status:*", code + ":priority:*",
	}
	out := make([]core.Label, 0, len(names))
	for _, n := range names {
		out = append(out, byName[n])
	}
	return out
}

// EnsureVocabulary seeds this capability's full vocabulary: the status:*
// namespace it owns (absorbed from the deleted internal/seed default set),
// the priority:* namespace it owns (priority is a planning concern, and
// workflow is the planning/status capability), and the four workflow boards.
// Idempotent: LabelSeed upserts only when the label is absent, so a human's
// curated description is never overwritten. It returns the board labels
// (Expr != "") it owns, in the documented order; status and priority labels
// are stored/namespace labels (Expr == "") and are not returned.
func EnsureVocabulary(s core.LabelService, code, actor string) ([]core.Label, error) {
	var boards []core.Label
	for _, l := range vocabulary(code) {
		if err := s.LabelSeed(l.Name, l.Description, l.Expr, actor); err != nil {
			return nil, err
		}
		if l.Expr != "" {
			boards = append(boards, l)
		}
	}
	return boards, nil
}
