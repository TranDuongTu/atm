package setup

import (
	"testing"

	"atm/internal/core"
	"atm/skills"
)

func seed(persona, name string, steps ...string) skills.ChecklistSeed {
	return skills.ChecklistSeed{Persona: persona, Name: name, Steps: steps}
}

func rec(persona, name string, steps ...string) core.ChecklistRecord {
	return core.ChecklistRecord{Persona: persona, Name: name, Steps: steps}
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

func TestPersonaWithNoSeedsHasNoStarterAccounting(t *testing.T) {
	seeds := []skills.ChecklistSeed{seed("concierge", "a", "1")}
	rows := BuildPersonas([]string{"developer"}, nil, seeds)
	if rows[0].StartersTotal != 0 {
		t.Fatalf("developer ships no starters; total = %d", rows[0].StartersTotal)
	}
}

func TestStepCountsAggregate(t *testing.T) {
	rows := BuildPersonas([]string{"developer"},
		[]core.ChecklistRecord{rec("developer", "x", "1", "2"), rec("developer", "y", "1")}, nil)
	if rows[0].Checklists != 2 || rows[0].Steps != 3 {
		t.Fatalf("row = %+v", rows[0])
	}
}
