package contextmap

import "atm/internal/core"

// ContextKinds are the pointer kinds this capability recognizes. They match the
// seeded context:* labels, but EnsureVocabulary does not assume seeding ran.
var ContextKinds = []string{"agent", "repository", "documentation", "convention"}

func LabelSuperseded(code string) string { return code + ":knowledge:superseded" }
func LabelProvenance(code string) string { return code + ":comment:provenance" }
func LabelContextKind(code, kind string) string {
	return code + ":context:" + kind
}
func BoardCurrent(code string) string { return code + ":context-current" }

// currentExpr computes membership of the context-current board: every context
// pointer that has not been superseded. Absence of the lifecycle label means
// current, so a human hand-writing a context task need not know the namespace
// exists.
func currentExpr() string { return "context:* AND NOT knowledge:superseded" }

// vocabulary is the single literal list every contract method derives from.
func vocabulary(code string) []core.Label {
	out := []core.Label{
		{Name: code + ":context:*", Description: "index tasks whose description is the payload: agent directions, repos, docs, conventions"},
		{Name: code + ":knowledge:*", Description: "lifecycle of a piece of recorded knowledge; absence means current"},
		{Name: LabelSuperseded(code), Description: "this context pointer is obsolete; its successor is named in the description. Kept for history -- it retains its kind, narrative, and provenance stamps. Applied by `atm capability contextmap supersede`."},
		{Name: LabelProvenance(code), Description: "task comment kind: a machine-written provenance stamp recording what a context pointer was derived from, and the evidence, at a moment in time. Written and read only by `atm capability contextmap` -- do not hand-edit."},
		{Name: BoardCurrent(code), Description: "every context pointer that has not been superseded: the project's current knowledge. Agents read this board rather than the raw context:* namespace, so a query always returns the latest.", Expr: currentExpr()},
	}
	kindDesc := map[string]string{
		"agent":         "the task's description captures agent-direction notes for this project: build/test/lint commands, conventions, and gotchas a working agent must know",
		"repository":    "the task's description names a code repository (path or URL), what it contains, and how to work in it; a later agent reads this to orient",
		"documentation": "the task's description points at a specific document (file path or URL) and summarizes what it covers, so a later agent can decide whether to read it",
		"convention":    "the task's description records a project convention the agent must follow: branch naming, commit style, PR template, build/test/lint hygiene, or any working rule inferred from the repo and confirmed by the human",
	}
	for _, kind := range ContextKinds {
		out = append(out, core.Label{Name: LabelContextKind(code, kind), Description: kindDesc[kind]})
	}
	return out
}

// Vocabulary returns every label this capability owns for code. Pure.
func Vocabulary(code string) []core.Label { return vocabulary(code) }

// Exposed returns the ring surface: exactly the context-current board. The
// context:*/knowledge:* namespaces are deliberately not exposed — they are
// capability bookkeeping, not browsing surfaces.
func Exposed(code string) []core.Label {
	for _, l := range vocabulary(code) {
		if l.Name == BoardCurrent(code) {
			return []core.Label{l}
		}
	}
	return nil
}

// EnsureVocabulary creates the labels and the board this capability uses, with
// descriptions, if they are absent. Idempotent, and it never overwrites a
// description a human already curated (the LabelSeed contract upserts only
// when the label is absent).
//
// This is what makes the capability self-bootstrapping: it works in any
// project, whether or not `atm label seed` ever ran. It returns the board
// labels (Expr != "") it owns — exactly the context-current board.
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
