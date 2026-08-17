package setup

import (
	"slices"

	"atm/internal/core"
	"atm/skills"
)

// BuildPersonas accounts each persona's checklists against the starters ATM
// ships. A seeded starter is MEANT to be edited afterwards, so a record whose
// steps differ is Customised (informational) — only an absent one is
// actionable. There is no readiness verdict here by design.
func BuildPersonas(personas []string, records []core.ChecklistRecord, seeds []skills.ChecklistSeed) []PersonaRow {
	rows := make([]PersonaRow, 0, len(personas))
	for _, p := range personas {
		row := PersonaRow{Persona: p}
		have := map[string]core.ChecklistRecord{}
		for _, r := range records {
			if r.Persona != p {
				continue
			}
			have[r.Name] = r
			row.Checklists++
			row.Steps += len(r.Steps)
		}
		for _, s := range seeds {
			if s.Persona != p {
				continue
			}
			row.StartersTotal++
			r, ok := have[s.Name]
			if !ok {
				row.MissingStarters = append(row.MissingStarters, s.Name)
				continue
			}
			row.StartersSeeded++
			if !slices.Equal(r.Steps, s.Steps) {
				row.Customised = append(row.Customised, s.Name)
			}
		}
		rows = append(rows, row)
	}
	return rows
}
