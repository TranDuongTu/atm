package qa

import "atm/internal/core"

// Lane board name helpers: callers select lanes by name, never by expression.

// BoardInbox: work qa has not decided about. Its expression is PROJECT WIRING
// — typically the upstream finish socket narrowed to the types qa verifies.
func BoardInbox(code string) string { return code + ":qa-inbox" }

// BoardPipeline: what qa is verifying, certified originals included.
func BoardPipeline(code string) string { return code + ":qa-pipeline" }

// BoardOut: settled evictions. The `-board` suffix keeps the board name out of
// the qa-out:* namespace it reports on.
func BoardOut(code string) string { return code + ":qa-out-board" }

// InboxSelfExclusion is the invariant tail of the inbox expression; the wiring
// writer re-appends it after whatever eligibility a project sets.
const InboxSelfExclusion = "NOT qa:* AND NOT qa-out:*"

// pipelineExpr carries `AND NOT qa-out:*` defensively: the verbs keep the axes
// exclusive, but a hand-assigned evict label must not leave a task in two lanes.
func pipelineExpr() string { return "qa:* AND NOT qa-out:*" }
func outExpr() string      { return "qa-out:*" }

// vocabulary is the single literal list every contract method derives from.
func vocabulary(code string) []core.Label {
	return []core.Label{
		{Name: code + ":qa:*", Description: "qa claim axis; presence means the task is in qa's pipeline"},
		{Name: code + ":qa:testing", Description: "qa state: under verification (originals and their test scaffolds alike)"},
		{Name: code + ":qa:done", Description: "qa FINISH SOCKET: this original is verified. Only ever stamped on an absorbed original, NEVER on a test scaffold. Downstream capabilities wire their inbox eligibility to this label."},
		{Name: code + ":qa-out:*", Description: "qa EVICT SOCKET: considered and declined by qa; permanent until qa release returns the task to the pool"},
		{Name: code + ":qa-out:failed", Description: "qa evict reason: verification failed — the backward-flow signal the manager routes"},
		{Name: code + ":qa-out:not-relevant", Description: "qa evict reason: nothing here to verify"},
		{Name: code + ":qa-out:covered-by", Description: "qa evict reason: verified as part of another task (see the covered_by payload field)"},
		{Name: BoardInbox(code), Description: "qa Inbox lane: finished work qa has not decided about. Non-empty means dispatch the manager.", Expr: InboxSelfExclusion},
		{Name: BoardPipeline(code), Description: "qa Pipeline lane: originals under verification and their scaffolds; certified originals stay visible.", Expr: pipelineExpr()},
		{Name: BoardOut(code), Description: "qa Out lane: settled evictions; qa release returns one to the pool.", Expr: outExpr()},
	}
}

// Vocabulary returns every label this capability owns for code. Pure.
func Vocabulary(code string) []core.Label { return vocabulary(code) }

// Exposed returns the labels this capability surfaces in the TUI ring: its
// three lanes, in lane order.
func Exposed(code string) []core.Label {
	byName := map[string]core.Label{}
	for _, l := range vocabulary(code) {
		byName[l.Name] = l
	}
	names := []string{BoardInbox(code), BoardPipeline(code), BoardOut(code)}
	out := make([]core.Label, 0, len(names))
	for _, n := range names {
		out = append(out, byName[n])
	}
	return out
}

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
