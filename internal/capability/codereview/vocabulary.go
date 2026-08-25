package codereview

import "atm/internal/core"

// Lane board name helpers: callers select lanes by name, never by expression.

// BoardInbox: work codereview has not decided about. This lane IS the warning
// surface — see the guide. Its expression is PROJECT WIRING.
func BoardInbox(code string) string { return code + ":codereview-inbox" }

// BoardPipeline: reviews scheduled, under way, and finished.
func BoardPipeline(code string) string { return code + ":codereview-pipeline" }

// BoardOut: settled evictions. The `-board` suffix keeps the board name out of
// the codereview-out:* namespace it reports on.
func BoardOut(code string) string { return code + ":codereview-out-board" }

// InboxSelfExclusion is the invariant tail of the inbox expression; the wiring
// writer re-appends it after whatever eligibility a project sets.
const InboxSelfExclusion = "NOT codereview:* AND NOT codereview-out:*"

// pipelineExpr carries the evict exclusion defensively: the verbs keep the
// axes exclusive, but a hand-assigned label must not put a task in two lanes.
func pipelineExpr() string { return "codereview:* AND NOT codereview-out:*" }
func outExpr() string      { return "codereview-out:*" }

// vocabulary is the single literal list every contract method derives from.
func vocabulary(code string) []core.Label {
	return []core.Label{
		{Name: code + ":codereview:*", Description: "codereview claim axis; presence means the task is in codereview's pipeline"},
		{Name: code + ":codereview:scheduled", Description: "codereview state: PR recorded, review scheduled, not yet started"},
		{Name: code + ":codereview:reviewing", Description: "codereview state: review under way; the conversation lives on the PR"},
		{Name: code + ":codereview:done", Description: "codereview FINISH SOCKET: this change has been reviewed. Downstream capabilities wire their inbox eligibility to this label."},
		{Name: code + ":codereview-out:*", Description: "codereview EVICT SOCKET: considered and declined by codereview; permanent until codereview release returns the task to the pool"},
		{Name: code + ":codereview-out:not-warranted", Description: "codereview evict reason: this change does not warrant a review"},
		{Name: code + ":codereview-out:superseded", Description: "codereview evict reason: superseded by another change before review"},
		{Name: BoardInbox(code), Description: "codereview Inbox lane: finished work with no review decision yet. A swelling count IS the warning that PRs are not being found.", Expr: InboxSelfExclusion},
		{Name: BoardPipeline(code), Description: "codereview Pipeline lane: reviews scheduled, under way, and finished.", Expr: pipelineExpr()},
		{Name: BoardOut(code), Description: "codereview Out lane: settled evictions; codereview release returns one to the pool.", Expr: outExpr()},
	}
}

// Vocabulary returns every label this capability owns for code. Pure.
func Vocabulary(code string) []core.Label { return vocabulary(code) }

// EnsureVocabulary seeds this capability's full vocabulary idempotently in one
// LabelSeedBatch transaction, and returns the lane boards it seeded.
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
