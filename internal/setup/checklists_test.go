package setup

import (
	"testing"

	"atm/internal/core"
	"atm/skills"
)

func seedSteps(texts ...string) []skills.SeedStep {
	out := make([]skills.SeedStep, len(texts))
	for i, t := range texts {
		out[i] = skills.SeedStep{Text: t}
	}
	return out
}

func recSteps(texts ...string) []core.ChecklistStep {
	out := make([]core.ChecklistStep, len(texts))
	for i, t := range texts {
		out[i] = core.ChecklistStep{Text: t}
	}
	return out
}

func seed(persona, name string, steps ...string) skills.ChecklistSeed {
	return skills.ChecklistSeed{Suits: []string{persona}, Name: name, Steps: seedSteps(steps...)}
}

func rec(persona, name string, steps ...string) core.ChecklistRecord {
	return core.ChecklistRecord{Suits: []string{persona}, Name: name, Steps: recSteps(steps...)}
}

func TestMissingStarterIsActionable(t *testing.T) {
	seeds := []skills.ChecklistSeed{seed("concierge", "a", "1"), seed("concierge", "b", "1")}
	rows := BuildPersonas([]string{"concierge"}, []core.ChecklistRecord{rec("concierge", "a", "1")}, seeds)
	r := rows[0]
	if r.StartersSeeded != 1 || r.StartersTotal != 2 {
		t.Fatalf("starters = %d/%d", r.StartersSeeded, r.StartersTotal)
	}
	if len(r.MissingStarters) != 1 || r.MissingStarters[0] != "b" {
		t.Fatalf("missing = %v", r.MissingStarters)
	}
}

// Editing a seeded starter is the intended workflow. It is NOT a fault and
// must never appear as something to fix.
func TestEditedStarterIsCustomisedNotMissing(t *testing.T) {
	seeds := []skills.ChecklistSeed{seed("concierge", "a", "1", "2")}
	rows := BuildPersonas([]string{"concierge"}, []core.ChecklistRecord{rec("concierge", "a", "1", "2", "3 mine")}, seeds)
	r := rows[0]
	if len(r.MissingStarters) != 0 {
		t.Fatalf("a customised starter is not missing: %v", r.MissingStarters)
	}
	if len(r.Customised) != 1 || r.Customised[0] != "a" {
		t.Fatalf("customised = %v", r.Customised)
	}
	if r.StartersSeeded != 1 {
		t.Fatalf("a customised starter is still seeded: %d", r.StartersSeeded)
	}
}

// A deeper tree with the same top level still counts as customised: equality
// is over the whole recursive tree, not the flat top level.
func TestNestedTreeDifferenceIsCustomised(t *testing.T) {
	seeds := []skills.ChecklistSeed{seed("concierge", "a", "1")}
	record := rec("concierge", "a", "1")
	record.Steps[0].Children = []core.ChecklistStep{{Text: "extra child"}}
	rows := BuildPersonas([]string{"concierge"}, []core.ChecklistRecord{record}, seeds)
	if len(rows[0].Customised) != 1 {
		t.Fatalf("customised = %v", rows[0].Customised)
	}
}

func TestPersonaWithNoSeedsHasNoStarterAccounting(t *testing.T) {
	seeds := []skills.ChecklistSeed{seed("concierge", "a", "1")}
	rows := BuildPersonas([]string{"developer"}, nil, seeds)
	if rows[0].StartersTotal != 0 {
		t.Fatalf("developer ships no starters; total = %d", rows[0].StartersTotal)
	}
}

func TestStepCountsAggregate(t *testing.T) {
	nested := rec("developer", "x", "1", "2")
	nested.Steps[0].Children = []core.ChecklistStep{{Text: "1a"}}
	rows := BuildPersonas([]string{"developer"},
		[]core.ChecklistRecord{nested, rec("developer", "y", "1")}, nil)
	if rows[0].Checklists != 2 || rows[0].Steps != 4 {
		t.Fatalf("row = %+v", rows[0])
	}
}

// A record suiting two personas counts under each row; a suit-less record
// counts under none.
func TestSuitsMembershipDrivesRows(t *testing.T) {
	shared := rec("manager", "shared", "1")
	shared.Suits = []string{"manager", "developer"}
	suitless := core.ChecklistRecord{Name: "loose", Steps: recSteps("1")}
	rows := BuildPersonas([]string{"manager", "developer"},
		[]core.ChecklistRecord{shared, suitless}, nil)
	for _, r := range rows {
		if r.Checklists != 1 {
			t.Fatalf("row %s = %+v", r.Persona, r)
		}
	}
}
