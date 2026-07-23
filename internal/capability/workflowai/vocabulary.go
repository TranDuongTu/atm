package workflowai

import "atm/internal/core"

// Board name helpers: callers select boards by name, never by expression.

// BoardToBrainstorm: tasks queued for brainstorming (stage:queued).
func BoardToBrainstorm(code string) string { return code + ":to-brainstorm" }

// BoardToClarify: tasks brainstormed, ready to clarify (stage:brainstormed).
func BoardToClarify(code string) string { return code + ":to-clarify" }

// BoardToPlan: tasks clarified, ready to plan (stage:clarified).
func BoardToPlan(code string) string { return code + ":to-plan" }

// BoardToImplement: tasks planned, cleared for implementation (stage:planned).
func BoardToImplement(code string) string { return code + ":to-implement" }

// BoardRevisions: open revision follow-ups of a bigger planned task.
func BoardRevisions(code string) string { return code + ":revisions" }

// BoardDoneTasks: tasks that completed the cycle.
func BoardDoneTasks(code string) string { return code + ":done-tasks" }

func toBrainstormExpr() string { return "stage:queued" }
func toClarifyExpr() string    { return "stage:brainstormed" }
func toPlanExpr() string       { return "stage:clarified" }
func toImplementExpr() string  { return "stage:planned" }
func revisionsExpr() string    { return "wfai:revision AND NOT stage:done" }
func doneTasksExpr() string    { return "stage:done" }

// vocabulary is the single literal list every contract method derives from:
// stored/namespace labels first (Expr == ""), then the six boards, in seed
// order. Ownership (Vocabulary), ring display (Exposed), and seeding
// (EnsureVocabulary) all read this list, so they cannot diverge.
func vocabulary(code string) []core.Label {
	return []core.Label{
		{Name: code + ":stage:*", Description: "workflow_ai cycle stage; at most one stage label per task, absence means not in the cycle (not yet queued)"},
		{Name: code + ":stage:queued", Description: "workflow_ai stage: entry — in the cycle, not yet brainstormed"},
		{Name: code + ":stage:brainstormed", Description: "workflow_ai stage: the idea has been explored; ready to clarify"},
		{Name: code + ":stage:clarified", Description: "workflow_ai stage: scope and success criteria settled; a spec locator is recorded in the capability metadata"},
		{Name: code + ":stage:planned", Description: "workflow_ai stage: a plan locator is recorded in the capability metadata; cleared for implementation"},
		{Name: code + ":stage:done", Description: "workflow_ai stage: completed the queue→brainstorm→clarify→plan→implement cycle"},
		{Name: code + ":wfai:*", Description: "workflow_ai markers; machine topology lives in the capability metadata, markers only make it board-visible"},
		{Name: code + ":wfai:revision", Description: "revision follow-up of a bigger planned task (revision_of link in workflow_ai metadata)"},
		{Name: code + ":wfai:framework", Description: "project framework conventions (written during a Semantics pass, read at session start); the description is the note"},
		{Name: BoardToBrainstorm(code), Description: "tasks queued for brainstorming.", Expr: toBrainstormExpr()},
		{Name: BoardToClarify(code), Description: "tasks brainstormed, ready to clarify (write a spec).", Expr: toClarifyExpr()},
		{Name: BoardToPlan(code), Description: "tasks clarified, ready to plan.", Expr: toPlanExpr()},
		{Name: BoardToImplement(code), Description: "tasks planned, cleared for implementation.", Expr: toImplementExpr()},
		{Name: BoardRevisions(code), Description: "open revision follow-ups of a bigger planned task, still needing refinement.", Expr: revisionsExpr()},
		{Name: BoardDoneTasks(code), Description: "tasks that completed the workflow_ai cycle.", Expr: doneTasksExpr()},
	}
}

// Vocabulary returns every label this capability owns for code. Pure.
func Vocabulary(code string) []core.Label { return vocabulary(code) }

// Exposed returns the labels this capability surfaces in the TUI ring, in
// preferred ring order: the six boards, then the two namespaces it owns.
func Exposed(code string) []core.Label {
	byName := map[string]core.Label{}
	for _, l := range vocabulary(code) {
		byName[l.Name] = l
	}
	names := []string{
		BoardToBrainstorm(code), BoardToClarify(code), BoardToPlan(code),
		BoardToImplement(code), BoardRevisions(code), BoardDoneTasks(code),
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
// returns the six board labels it owns. Old boards from the prior vocabulary
// (new-tasks, brainstormed-tasks, planned-tasks) are removed so a reseed
// over an existing project converges to the new board set.
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
	for _, gone := range []string{
		code + ":new-tasks", code + ":brainstormed-tasks", code + ":planned-tasks",
	} {
		_, _ = s.LabelRemove(gone, actor)
	}
	return boards, nil
}