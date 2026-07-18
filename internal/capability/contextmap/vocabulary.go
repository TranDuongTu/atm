package contextmap

import "atm/internal/core"

// ContextKinds are the pointer kinds this capability recognizes. They match the
// seeded context:* labels, but EnsureVocabulary does not assume seeding ran.
var ContextKinds = []string{"agent", "repository", "documentation", "question"}

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

// EnsureVocabulary creates the labels and the board this capability uses, with
// descriptions, if they are absent. Idempotent, and it never overwrites a
// description a human already curated (the LabelSeed contract upserts only
// when the label is absent).
//
// This is what makes the capability self-bootstrapping: it works in any
// project, whether or not `atm label seed` ever ran.
func EnsureVocabulary(s core.LabelService, code, actor string) error {
	type lbl struct{ name, desc, expr string }
	want := []lbl{
		{code + ":context:*", "index tasks whose description is the payload: agent directions, repos, docs, questions", ""},
		{code + ":knowledge:*", "lifecycle of a piece of recorded knowledge; absence means current", ""},
		{LabelSuperseded(code), "this context pointer is obsolete; its successor is named in the description. Kept for history -- it retains its kind, narrative, and provenance stamps. Applied by `atm capability contextmap supersede`.", ""},
		{LabelProvenance(code), "task comment kind: a machine-written provenance stamp recording what a context pointer was derived from, and the evidence, at a moment in time. Written and read only by `atm capability contextmap` -- do not hand-edit.", ""},
		{BoardCurrent(code), "every context pointer that has not been superseded: the project's current knowledge. Agents read this board rather than the raw context:* namespace, so a query always returns the latest.", currentExpr()},
	}
	kindDesc := map[string]string{
		"agent":         "the task's description captures agent-direction notes for this project: build/test/lint commands, conventions, and gotchas a working agent must know",
		"repository":    "the task's description names a code repository (path or URL), what it contains, and how to work in it; a later agent reads this to orient",
		"documentation": "the task's description points at a specific document (file path or URL) and summarizes what it covers, so a later agent can decide whether to read it",
		"question":      "the task's description poses an open question or ambiguity about the project that a human or later agent should clarify; not a defect, not a work item, a gap in understanding",
	}
	for _, kind := range ContextKinds {
		want = append(want, lbl{LabelContextKind(code, kind), kindDesc[kind], ""})
	}
	for _, l := range want {
		if err := s.LabelSeed(l.name, l.desc, l.expr, actor); err != nil {
			return err
		}
	}
	return nil
}
