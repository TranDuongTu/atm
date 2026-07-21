package workflowai

import "atm/internal/core"

// Board name helpers: callers select boards by name, never by expression.

// BoardNewTasks: tasks not yet brainstormed (no stage:* label at all).
func BoardNewTasks(code string) string { return code + ":new-tasks" }

// BoardBrainstormedTasks: tasks in refinement (brainstormed or clarified).
func BoardBrainstormedTasks(code string) string { return code + ":brainstormed-tasks" }

// BoardPlannedTasks: tasks with a recorded plan (planned or implementable).
func BoardPlannedTasks(code string) string { return code + ":planned-tasks" }

// BoardRevisions: open revision follow-ups of a bigger planned task.
func BoardRevisions(code string) string { return code + ":revisions" }

// BoardDoneTasks: tasks that completed the cycle.
func BoardDoneTasks(code string) string { return code + ":done-tasks" }

func newTasksExpr() string          { return "NOT stage:*" }
func brainstormedTasksExpr() string { return "stage:brainstormed OR stage:clarified" }
func plannedTasksExpr() string      { return "stage:planned OR stage:implementable" }
func revisionsExpr() string         { return "wfai:revision AND NOT stage:done" }
func doneTasksExpr() string         { return "stage:done" }

// vocabulary is the single literal list every contract method derives from:
// stored/namespace labels first (Expr == ""), then the five boards, in seed
// order. Ownership (Vocabulary), ring display (Exposed), and seeding
// (EnsureVocabulary) all read this list, so they cannot diverge.
func vocabulary(code string) []core.Label {
	return []core.Label{
		{Name: code + ":stage:*", Description: "workflow_ai cycle stage; at most one stage label per task, absence means new (not yet brainstormed)"},
		{Name: code + ":stage:brainstormed", Description: "workflow_ai stage: the idea has been brainstormed; requirements explored"},
		{Name: code + ":stage:clarified", Description: "workflow_ai stage: scope and success criteria are settled; ready to plan"},
		{Name: code + ":stage:planned", Description: "workflow_ai stage: a plan locator is recorded in the capability metadata"},
		{Name: code + ":stage:implementable", Description: "workflow_ai stage: planned AND sized for one implementation session; cleared for implementation"},
		{Name: code + ":stage:done", Description: "workflow_ai stage: completed the brainstorm→implement cycle"},
		{Name: code + ":wfai:*", Description: "workflow_ai markers; machine topology lives in the capability metadata, markers only make it board-visible"},
		{Name: code + ":wfai:revision", Description: "revision follow-up of a bigger planned task (revision_of link in workflow_ai metadata)"},
		{Name: code + ":wfai:framework", Description: "project framework conventions (written during Brief, read at session start); the description is the note"},
		{Name: BoardNewTasks(code), Description: "tasks not yet brainstormed: no stage label. The workflow_ai intake queue.", Expr: newTasksExpr()},
		{Name: BoardBrainstormedTasks(code), Description: "tasks in refinement: brainstormed or clarified.", Expr: brainstormedTasksExpr()},
		{Name: BoardPlannedTasks(code), Description: "tasks with a recorded plan: planned or implementable.", Expr: plannedTasksExpr()},
		{Name: BoardRevisions(code), Description: "open revision follow-ups of a bigger planned task, still needing refinement.", Expr: revisionsExpr()},
		{Name: BoardDoneTasks(code), Description: "tasks that completed the workflow_ai cycle.", Expr: doneTasksExpr()},
	}
}

// Vocabulary returns every label this capability owns for code. Pure.
func Vocabulary(code string) []core.Label { return vocabulary(code) }

// Exposed returns the labels this capability surfaces in the TUI ring, in
// preferred ring order: the five boards, then the two namespaces it owns.
func Exposed(code string) []core.Label {
	byName := map[string]core.Label{}
	for _, l := range vocabulary(code) {
		byName[l.Name] = l
	}
	names := []string{
		BoardNewTasks(code), BoardBrainstormedTasks(code), BoardPlannedTasks(code),
		BoardRevisions(code), BoardDoneTasks(code),
		code + ":stage:*", code + ":wfai:*",
	}
	out := make([]core.Label, 0, len(names))
	for _, n := range names {
		out = append(out, byName[n])
	}
	return out
}

// EnsureVocabulary seeds this capability's full vocabulary idempotently
// (LabelSeed upserts only when absent, so curated descriptions survive) and
// returns the five board labels it owns.
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
