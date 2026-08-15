// internal/store/checklist_test.go
package store

import (
	"errors"
	"testing"

	"atm/internal/core"
)

func TestChecklistCreateListGet(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateProject("ATM", "Agent Tasks Management", chActor); err != nil {
		t.Fatal(err)
	}
	rec := core.ChecklistRecord{Persona: "developer", Name: "main", Purpose: "day to day", Steps: []string{"a", "b"}}
	tk, err := s.CreateChecklist("ATM", rec, "concierge@test:unit")
	if err != nil {
		t.Fatal(err)
	}
	if tk.Title != "developer/main" {
		t.Fatalf("title = %q", tk.Title)
	}
	got, err := s.GetChecklist("ATM", "developer", "main")
	if err != nil || got.Purpose != "day to day" || len(got.Steps) != 2 {
		t.Fatalf("get: %+v err %v", got, err)
	}
	all, _ := s.ChecklistRecords("ATM")
	mine, _ := s.PersonaChecklists("ATM", "developer")
	if len(all) != 1 || len(mine) != 1 {
		t.Fatalf("lists: %d %d", len(all), len(mine))
	}
	if _, err := s.CreateChecklist("ATM", rec, "concierge@test:unit"); !errors.Is(err, core.ErrUsage) {
		t.Fatalf("duplicate (persona,name) must be rejected: %v", err)
	}
	other := core.ChecklistRecord{Persona: "manager", Name: "main", Steps: []string{"x"}}
	if _, err := s.CreateChecklist("ATM", other, "concierge@test:unit"); err != nil {
		t.Fatalf("same name under another persona must be allowed: %v", err)
	}
}

func TestChecklistEditAndRemove(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateProject("ATM", "Agent Tasks Management", chActor); err != nil {
		t.Fatal(err)
	}
	_, _ = s.CreateChecklist("ATM", core.ChecklistRecord{Persona: "developer", Name: "main", Steps: []string{"a"}}, chActor)
	p := "new purpose"
	if err := s.EditChecklist("ATM", "developer", "main", &p, []string{"x", "y"}, chActor); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetChecklist("ATM", "developer", "main")
	if got.Purpose != "new purpose" || len(got.Steps) != 2 {
		t.Fatalf("edit: %+v", got)
	}
	if err := s.EditChecklist("ATM", "developer", "main", nil, nil, chActor); err != nil {
		t.Fatal(err) // nil purpose + nil steps = no-op, not a clear
	}
	got, _ = s.GetChecklist("ATM", "developer", "main")
	if got.Purpose != "new purpose" || len(got.Steps) != 2 {
		t.Fatalf("no-op edit changed record: %+v", got)
	}
	if err := s.RemoveChecklist("ATM", "developer", "main", chActor); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetChecklist("ATM", "developer", "main"); err == nil {
		t.Fatal("removed checklist must not resolve")
	}
}

func TestChecklistCreateValidation(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateProject("ATM", "Agent Tasks Management", chActor); err != nil {
		t.Fatal(err)
	}
	for _, rec := range []core.ChecklistRecord{
		{Name: "main", Steps: []string{"a"}},
		{Persona: "developer", Steps: []string{"a"}},
		{Persona: "dev/eloper", Name: "main", Steps: []string{"a"}},
		{Persona: "developer", Name: "ma/in", Steps: []string{"a"}},
		{Persona: "developer", Name: "main"},
	} {
		if _, err := s.CreateChecklist("ATM", rec, chActor); !errors.Is(err, core.ErrUsage) {
			t.Fatalf("rec %+v: want ErrUsage, got %v", rec, err)
		}
	}
	// edit with an empty steps list is also rejected, not a clear
	s2 := newTestStore(t)
	if _, err := s2.CreateProject("ATM", "Agent Tasks Management", chActor); err != nil {
		t.Fatal(err)
	}
	if _, err := s2.CreateChecklist("ATM", core.ChecklistRecord{Persona: "developer", Name: "main", Steps: []string{"a"}}, chActor); err != nil {
		t.Fatal(err)
	}
	empty := []string{}
	if err := s2.EditChecklist("ATM", "developer", "main", nil, empty, chActor); !errors.Is(err, core.ErrUsage) {
		t.Fatalf("empty steps edit: want ErrUsage, got %v", err)
	}
}

// One hand-corrupted record must not disable every OTHER checklist's verbs,
// and must remain removable through its own (persona, name) noun.
func TestChecklistCorruptRecordDoesNotPoisonNeighbours(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateProject("ATM", "Agent Tasks Management", chActor); err != nil {
		t.Fatal(err)
	}
	bad, err := s.CreateChecklist("ATM", core.ChecklistRecord{Persona: "developer", Name: "broken", Steps: []string{"a"}}, chActor)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateChecklist("ATM", core.ChecklistRecord{Persona: "developer", Name: "good", Steps: []string{"a"}}, chActor); err != nil {
		t.Fatal(err)
	}
	if err := s.SetTaskCapabilityMeta(bad.ID, core.ChecklistMetaKey, "garbage", chActor); err != nil {
		t.Fatal(err)
	}
	// the healthy neighbour still resolves and edits
	p := "still works"
	if err := s.EditChecklist("ATM", "developer", "good", &p, nil, chActor); err != nil {
		t.Fatalf("corrupt neighbour poisoned lookup: %v", err)
	}
	// the broken one is reachable by title and reports its own decode error
	if err := s.EditChecklist("ATM", "developer", "broken", &p, nil, chActor); err == nil {
		t.Fatal("want a decode error for the corrupt record itself")
	}
	// list degrades rather than failing
	recs, err := s.ChecklistRecords("ATM")
	if err != nil || len(recs) != 1 || recs[0].Name != "good" {
		t.Fatalf("records: %+v %v", recs, err)
	}
	// and the corrupt record is removable through its own noun
	if err := s.RemoveChecklist("ATM", "developer", "broken", chActor); err != nil {
		t.Fatalf("remove corrupt record: %v", err)
	}
	if err := s.RemoveChecklist("ATM", "developer", "nope", chActor); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("unknown persona/name: %v", err)
	}
}
