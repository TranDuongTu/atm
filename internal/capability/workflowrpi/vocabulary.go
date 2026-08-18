package workflowrpi

import "atm/internal/core"

// Board name helpers: callers select boards by name, never by expression.

// BoardBacklog: RPI intake — every task RPI has not yet decided about. It is
// the unset set (NOT rpi:*), never a stored label.
func BoardBacklog(code string) string { return code + ":rpi-backlog" }

// BoardProduct: product-roadmap tasks under RPI consideration.
func BoardProduct(code string) string { return code + ":rpi-product" }

// BoardPipeline: build-pipeline tasks, each linked to a product task.
func BoardPipeline(code string) string { return code + ":rpi-pipeline" }

// BoardReject: tasks considered and rejected from the RPI perspective.
func BoardReject(code string) string { return code + ":rpi-reject" }

func backlogExpr() string  { return "NOT rpi:*" }
func productExpr() string  { return "rpi:product" }
func pipelineExpr() string { return "rpi:pipeline" }
func rejectExpr() string   { return "rpi:reject" }

// vocabulary is the single literal list every contract method derives from:
// stored/namespace labels first (Expr == ""), then the four boards, in seed
// order. Ownership (Vocabulary), ring display (Exposed), and seeding
// (EnsureVocabulary) all read this list, so they cannot diverge.
func vocabulary(code string) []core.Label {
	return []core.Label{
		{Name: code + ":rpi:*", Description: "workflow_rpi lane; absence means RPI backlog/intake"},
		{Name: code + ":rpi:product", Description: "workflow_rpi lane: product roadmap item under consideration"},
		{Name: code + ":rpi:pipeline", Description: "workflow_rpi lane: build pipeline item linked to a product task"},
		{Name: code + ":rpi:reject", Description: "workflow_rpi lane: considered and rejected from the RPI perspective"},
		{Name: code + ":rpi-product:*", Description: "workflow_rpi product-roadmap clarification status"},
		{Name: code + ":rpi-product:unclarified", Description: "workflow_rpi product status: needs business/product clarification"},
		{Name: code + ":rpi-product:clarified", Description: "workflow_rpi product status: clarified enough for breakdown into buildable work"},
		{Name: code + ":rpi-dev:*", Description: "workflow_rpi pipeline development status"},
		{Name: code + ":rpi-dev:clarified", Description: "workflow_rpi dev status: concrete work item is clarified"},
		{Name: code + ":rpi-dev:brainstormed", Description: "workflow_rpi dev status: implementation approach has been brainstormed"},
		{Name: code + ":rpi-dev:planned", Description: "workflow_rpi dev status: implementation plan exists"},
		{Name: code + ":rpi-dev:implementing", Description: "workflow_rpi dev status: implementation is in progress"},
		{Name: code + ":rpi-dev:review", Description: "workflow_rpi dev status: implementation is ready for or under review"},
		{Name: code + ":rpi-dev:done", Description: "workflow_rpi dev status: pipeline work is done from RPI perspective"},
		{Name: code + ":rpi-reject:*", Description: "workflow_rpi reject reason"},
		{Name: code + ":rpi-reject:duplicate", Description: "workflow_rpi reject reason: duplicate"},
		{Name: code + ":rpi-reject:out-of-scope", Description: "workflow_rpi reject reason: out of scope"},
		{Name: code + ":rpi-reject:not-worth-it", Description: "workflow_rpi reject reason: not worth pursuing"},
		{Name: code + ":rpi-reject:covered-by", Description: "workflow_rpi reject reason: covered by another task"},
		{Name: BoardBacklog(code), Description: "RPI backlog/intake: tasks with no rpi:* lane label.", Expr: backlogExpr()},
		{Name: BoardProduct(code), Description: "RPI product roadmap tasks.", Expr: productExpr()},
		{Name: BoardPipeline(code), Description: "RPI build pipeline tasks.", Expr: pipelineExpr()},
		{Name: BoardReject(code), Description: "RPI rejected tasks.", Expr: rejectExpr()},
	}
}

// Vocabulary returns every label this capability owns for code. Pure.
func Vocabulary(code string) []core.Label { return vocabulary(code) }

// Exposed returns the labels this capability surfaces in the TUI ring, in
// preferred ring order: the four boards, then the four namespaces it owns.
func Exposed(code string) []core.Label {
	byName := map[string]core.Label{}
	for _, l := range vocabulary(code) {
		byName[l.Name] = l
	}
	names := []string{
		BoardBacklog(code), BoardProduct(code), BoardPipeline(code), BoardReject(code),
		code + ":rpi:*", code + ":rpi-product:*", code + ":rpi-dev:*", code + ":rpi-reject:*",
	}
	out := make([]core.Label, 0, len(names))
	for _, n := range names {
		out = append(out, byName[n])
	}
	return out
}

// EnsureVocabulary seeds this capability's full vocabulary idempotently in one
// LabelSeedBatch transaction, and returns the boards it seeded.
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
