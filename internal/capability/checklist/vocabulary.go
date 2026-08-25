package checklist

import "atm/internal/core"

const CapabilityName = "checklist"

// BoardChecklists is the one board this capability owns.
func BoardChecklists(code string) string { return code + ":checklists" }

// vocabulary is the single literal list every contract method derives from.
// Per-persona value labels (<code>:checklist:<persona>) are NOT here: personas
// are user-creatable, so the store seeds them lazily on first add.
func vocabulary(code string) []core.Label {
	return []core.Label{
		{Name: code + ":checklist:*", Description: "checklist records: named, per-persona standing operating procedures. Each member task is one checklist — its title is <persona>/<name>, its description a one-line pointer, and its payload the purpose and ordered steps. Managed by `atm checklist`; do not hand-edit the payload."},
		{Name: BoardChecklists(code), Description: "every checklist record in the project. Browse with `atm checklist list`; this board exists so queries and other boards can see them.", Expr: "checklist:*"},
	}
}

func Vocabulary(code string) []core.Label { return vocabulary(code) }

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
