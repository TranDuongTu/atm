package scrum

import "atm/internal/core"

// Lane board name helpers: callers select lanes by name, never by expression.

// BoardInbox: eligible work scrum has not decided about. Its expression is the
// project's WIRING — `atm project wiring` rewrites the eligibility half; the
// self-exclusion tail below is invariant and always re-appended.
func BoardInbox(code string) string { return code + ":scrum-inbox" }

// BoardPipeline: what scrum is building.
func BoardPipeline(code string) string { return code + ":scrum-pipeline" }

// BoardOut: settled evictions. The `-board` suffix keeps the board name out of
// the scrum-out:* namespace it reports on — a bare `scrum-out` label would be
// read as a member of that namespace by LabelSet.Contains.
func BoardOut(code string) string { return code + ":scrum-out-board" }

// InboxSelfExclusion is the invariant tail of the inbox expression: an inbox
// never shows work this capability has already claimed or evicted. Seeded as
// the whole default expression (no eligibility yet); the wiring writer
// re-appends it after whatever eligibility a project sets.
const InboxSelfExclusion = "NOT scrum:* AND NOT scrum-out:*"

// pipelineExpr carries `AND NOT scrum-out:*` defensively. The verbs keep the
// claim and evict axes exclusive, but a hand-assigned evict label must not
// leave a task standing in two lanes at once.
func pipelineExpr() string { return "scrum:* AND NOT scrum-out:*" }
func outExpr() string      { return "scrum-out:*" }

// vocabulary is the single literal list every contract method derives from:
// stored/namespace labels first (Expr == ""), then the three lanes, in seed
// order. Ownership (Vocabulary) and seeding (EnsureVocabulary) both read
// this list, so they cannot diverge.
func vocabulary(code string) []core.Label {
	return []core.Label{
		{Name: code + ":scrum:*", Description: "scrum claim/type axis; presence means the task is in scrum's pipeline; exactly one type per claimed task"},
		{Name: code + ":scrum:epic", Description: "scrum type: product-level requirement decomposed into stories"},
		{Name: code + ":scrum:story", Description: "scrum type: user story — a portion of an epic's design, decomposed into tasks"},
		{Name: code + ":scrum:task", Description: "scrum type: PR-sized implementation unit"},
		{Name: code + ":scrum:bug", Description: "scrum type: defect to fix"},
		{Name: code + ":scrum:design", Description: "scrum type: design/spec work whose deliverable is an executable plan; never flows downstream"},
		{Name: code + ":scrum-stage:*", Description: "scrum working stage for claimed units"},
		{Name: code + ":scrum-stage:brainstormed", Description: "scrum stage: approach has been brainstormed"},
		{Name: code + ":scrum-stage:planned", Description: "scrum stage: an implementation plan exists but has not been approved yet"},
		{Name: code + ":scrum-stage:implementable", Description: "scrum stage: the plan is approved and the unit is ready to be built — design approval is the planned -> implementable transition"},
		{Name: code + ":scrum-stage:implementing", Description: "scrum stage: implementation is in progress"},
		{Name: code + ":scrum-stage:review", Description: "scrum stage: ready for or under review"},
		{Name: code + ":scrum-stage:done", Description: "scrum FINISH SOCKET: this unit is built and converged (task built; story/epic = every child done). Downstream capabilities wire their inbox eligibility to this label."},
		{Name: code + ":scrum-out:*", Description: "scrum EVICT SOCKET: considered and declined by scrum; permanent until scrum release returns the task to the pool"},
		{Name: code + ":scrum-out:duplicate", Description: "scrum evict reason: duplicate of another task"},
		{Name: code + ":scrum-out:out-of-scope", Description: "scrum evict reason: not this project's dev work"},
		{Name: code + ":scrum-out:not-worth-it", Description: "scrum evict reason: not worth pursuing"},
		{Name: code + ":scrum-out:covered-by", Description: "scrum evict reason: covered by another task (see the covered_by payload field)"},
		{Name: BoardInbox(code), Description: "scrum Inbox lane: eligible work scrum has not decided about. Non-empty means dispatch the manager.", Expr: InboxSelfExclusion},
		{Name: BoardPipeline(code), Description: "scrum Pipeline lane: what scrum is building; finished units stay visible.", Expr: pipelineExpr()},
		{Name: BoardOut(code), Description: "scrum Out lane: settled evictions; scrum release returns one to the pool.", Expr: outExpr()},
	}
}

// Vocabulary returns every label this capability owns for code. Pure.
func Vocabulary(code string) []core.Label { return vocabulary(code) }

// EnsureVocabulary seeds this capability's full vocabulary idempotently in one
// LabelSeedBatch transaction, and returns the lane boards it seeded. Seeding is
// create-only for expressions, so it never overwrites a project's wiring.
func EnsureVocabulary(s core.LabelService, code, actor string) ([]core.Label, error) {
	vocab := vocabulary(code)
	if err := s.LabelSeedBatch(vocab, actor); err != nil {
		return nil, err
	}
	var boards []core.Label
	for _, l := range vocab {
		if l.Expr != "" {
			boards = append(boards, l)
		}
	}
	return boards, nil
}
