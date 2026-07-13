package contextmap

import "atm/internal/store"

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
// description a human already curated (store.LabelSeed upserts only when the
// label is absent).
//
// This is what makes the capability self-bootstrapping: it works in any
// project, whether or not `atm label seed` ever ran.
func EnsureVocabulary(s *store.Store, code, actor string) error {
	type lbl struct{ name, desc, expr string }
	want := []lbl{
		{code + ":knowledge:*", "lifecycle of a piece of recorded knowledge; absence means current", ""},
		{LabelSuperseded(code), "this context pointer is obsolete; its successor is named in the description. Kept for history -- it retains its kind, narrative, and provenance stamps. Applied by `atm context supersede`.", ""},
		{LabelProvenance(code), "task comment kind: a machine-written provenance stamp recording what a context pointer was derived from, and the evidence, at a moment in time. Written and read only by `atm context` -- do not hand-edit.", ""},
		{BoardCurrent(code), "every context pointer that has not been superseded: the project's current knowledge. Agents read this board rather than the raw context:* namespace, so a query always returns the latest.", currentExpr()},
	}
	for _, kind := range ContextKinds {
		want = append(want, lbl{LabelContextKind(code, kind), "context pointer kind: " + kind, ""})
	}
	for _, l := range want {
		if err := s.LabelSeed(l.name, l.desc, l.expr, actor); err != nil {
			return err
		}
	}
	return nil
}
