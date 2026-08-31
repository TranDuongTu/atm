package checklist

import "atm/internal/core"

const CapabilityName = "checklist"

// BoardChecklists is the one board this capability owns.
func BoardChecklists(code string) string { return code + ":checklists" }

// boardExpr matches both record generations: the bare v2 label by stored-label
// lookup, and v1's persona labels via the namespace predicate (a prefix test
// on "checklist:", which the bare label does not match — both terms needed).
const boardExpr = "checklist OR checklist:*"

// legacyBoardExpr is what v1 EnsureVocabulary wrote; only this exact value is
// upgraded in place, so a user-customized board expression is never clobbered.
const legacyBoardExpr = "checklist:*"

// vocabulary is the single literal list every contract method derives from.
func vocabulary(code string) []core.Label {
	return []core.Label{
		{Name: code + ":checklist", Description: "checklist records (v2): free-standing, name-keyed standing operating procedures. Each member task is one checklist — its title is the name, its payload the purpose, recursive steps, suits, requires, and origin. Managed by `atm checklist`; do not hand-edit the payload."},
		{Name: code + ":checklist:*", Description: "legacy v1 checklist records, persona-keyed. Read-compatible; rewritten to the bare checklist label on first edit."},
		{Name: BoardChecklists(code), Description: "every checklist record in the project, both generations. Browse with `atm checklist list`; this board exists so queries and other boards can see them.", Expr: boardExpr},
	}
}

func Vocabulary(code string) []core.Label { return vocabulary(code) }

func EnsureVocabulary(s core.LabelService, code, actor string) ([]core.Label, error) {
	// LabelSeed keeps existing expressions create-only, so a project seeded by
	// a v1 binary still carries the legacy board expr, which cannot see v2
	// records. Upgrade exactly that value via LabelAdd (the sanctioned
	// explicit-expression path) before converging the rest.
	if l, err := s.LabelShow(BoardChecklists(code)); err == nil && l.Expr == legacyBoardExpr {
		if err := s.LabelAdd(BoardChecklists(code), "", boardExpr, actor); err != nil {
			return nil, err
		}
	}
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
