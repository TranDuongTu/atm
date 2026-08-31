package setup

import (
	"reflect"
	"slices"

	"atm/internal/core"
	"atm/skills"
)

func coreStepsOf(in []skills.SeedStep) []core.ChecklistStep {
	if len(in) == 0 {
		return nil
	}
	out := make([]core.ChecklistStep, len(in))
	for i, s := range in {
		out[i] = core.ChecklistStep{Text: s.Text, Children: coreStepsOf(s.Children)}
	}
	return out
}

// BuildPersonas accounts each persona's suited checklists against the starters
// ATM ships. A seeded starter is MEANT to be edited afterwards, so a record
// whose step tree differs is Customised (informational) — only an absent one
// is actionable. There is no readiness verdict here by design.
func BuildPersonas(personas []string, records []core.ChecklistRecord, seeds []skills.ChecklistSeed) []PersonaRow {
	rows := make([]PersonaRow, 0, len(personas))
	for _, p := range personas {
		row := PersonaRow{Persona: p}
		have := map[string]core.ChecklistRecord{}
		for _, r := range records {
			if !slices.Contains(r.Suits, p) {
				continue
			}
			have[r.Name] = r
			row.Checklists++
			row.Steps += core.ChecklistStepCount(r.Steps)
		}
		for _, s := range seeds {
			if !slices.Contains(s.Suits, p) {
				continue
			}
			row.StartersTotal++
			r, ok := have[s.Name]
			if !ok {
				row.MissingStarters = append(row.MissingStarters, s.Name)
				continue
			}
			row.StartersSeeded++
			if !reflect.DeepEqual(r.Steps, coreStepsOf(s.Steps)) {
				row.Customised = append(row.Customised, s.Name)
			}
		}
		rows = append(rows, row)
	}
	return rows
}
