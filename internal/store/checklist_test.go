// internal/store/checklist_test.go
package store

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"atm/internal/core"
)

// mkV1Checklist hand-builds a v1-generation record: persona value label,
// persona/name title, and a literal v1 payload — the shape old binaries wrote.
func mkV1Checklist(t *testing.T, s *Store, code, persona, name, payload string) *core.Task {
	t.Helper()
	label := core.ChecklistPersonaLabelPrefix(code) + persona
	if err := s.LabelSeed(label, "checklists for persona "+persona, "", chActor); err != nil {
		t.Fatal(err)
	}
	tk, err := s.CreateTask(code, persona+"/"+name, "v1 checklist", []string{label}, chActor)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetTaskCapabilityMeta(tk.ID, core.ChecklistMetaKey, payload, chActor); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetTask(tk.ID)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestChecklistCreateListGetV2(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateProject("ATM", "Agent Tasks Management", chActor); err != nil {
		t.Fatal(err)
	}
	rec := core.ChecklistRecord{
		Name:    "main",
		Purpose: "day to day",
		Steps:   []core.ChecklistStep{{Text: "a", Children: []core.ChecklistStep{{Text: "a1"}}}, {Text: "b"}},
		Suits:   []string{"developer"},
		Requires: core.ChecklistRequires{
			Capabilities: []string{"scrum"},
		},
	}
	tk, err := s.CreateChecklist("ATM", rec, chActor)
	if err != nil {
		t.Fatal(err)
	}
	if tk.Title != "main" {
		t.Fatalf("title = %q, want the bare name", tk.Title)
	}
	hasBare := false
	for _, l := range tk.Labels {
		if l == "ATM:checklist" {
			hasBare = true
		}
		if strings.HasPrefix(l, "ATM:checklist:") {
			t.Fatalf("v2 create must not use the persona label: %v", tk.Labels)
		}
	}
	if !hasBare {
		t.Fatalf("labels = %v, want ATM:checklist", tk.Labels)
	}
	got, err := s.GetChecklist("ATM", "main")
	if err != nil {
		t.Fatal(err)
	}
	rec.TaskID = tk.ID
	rec.Origin = "user" // defaulted at create
	if !reflect.DeepEqual(got, &rec) {
		t.Fatalf("get: %+v want %+v", got, rec)
	}
	all, _ := s.ChecklistRecords("ATM")
	suited, _ := s.SuitedChecklists("ATM", "developer")
	other, _ := s.SuitedChecklists("ATM", "manager")
	if len(all) != 1 || len(suited) != 1 || len(other) != 0 {
		t.Fatalf("lists: all %d suited %d other %d", len(all), len(suited), len(other))
	}
	if _, err := s.CreateChecklist("ATM", rec, chActor); !errors.Is(err, core.ErrUsage) {
		t.Fatalf("duplicate name must be rejected: %v", err)
	}
}

func TestChecklistCreateValidation(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateProject("ATM", "Agent Tasks Management", chActor); err != nil {
		t.Fatal(err)
	}
	step := []core.ChecklistStep{{Text: "a"}}
	for _, rec := range []core.ChecklistRecord{
		{Steps: step},                                              // no name
		{Name: "ma/in", Steps: step},                               // slash in name
		{Name: "main"},                                             // no steps
		{Name: "main", Steps: step, Suits: []string{"dev/eloper"}}, // slash in suit
		{Name: "main", Steps: step, Origin: "vendor"},              // bad origin
	} {
		if _, err := s.CreateChecklist("ATM", rec, chActor); !errors.Is(err, core.ErrUsage) {
			t.Fatalf("rec %+v: want ErrUsage, got %v", rec, err)
		}
	}
}

func TestChecklistV1Visibility(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateProject("ATM", "Agent Tasks Management", chActor); err != nil {
		t.Fatal(err)
	}
	v1 := mkV1Checklist(t, s, "ATM", "developer", "x",
		`{"v":1,"persona":"developer","name":"x","purpose":"old","steps":["one","two"]}`)
	if _, err := s.CreateChecklist("ATM", core.ChecklistRecord{Name: "y", Steps: []core.ChecklistStep{{Text: "a"}}, Suits: []string{"manager"}}, chActor); err != nil {
		t.Fatal(err)
	}
	all, err := s.ChecklistRecords("ATM")
	if err != nil || len(all) != 2 {
		t.Fatalf("records: %+v %v", all, err)
	}
	got, err := s.GetChecklist("ATM", "x")
	if err != nil {
		t.Fatal(err)
	}
	if got.TaskID != v1.ID || !reflect.DeepEqual(got.Suits, []string{"developer"}) || got.Origin != "user" {
		t.Fatalf("v1 mapped: %+v", got)
	}
	suited, _ := s.SuitedChecklists("ATM", "developer")
	if len(suited) != 1 || suited[0].Name != "x" {
		t.Fatalf("suited: %+v", suited)
	}
	// same name as a v1 record is still a duplicate
	if _, err := s.CreateChecklist("ATM", core.ChecklistRecord{Name: "x", Steps: []core.ChecklistStep{{Text: "a"}}}, chActor); !errors.Is(err, core.ErrUsage) {
		t.Fatalf("duplicate against v1: %v", err)
	}
	// read-only access never migrated anything
	raw, err := s.GetTask(v1.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(raw.Labels, v1.Labels) || raw.Meta[core.ChecklistMetaKey] != v1.Meta[core.ChecklistMetaKey] {
		t.Fatalf("read-only access migrated the record:\n old %v %q\n new %v %q",
			v1.Labels, v1.Meta[core.ChecklistMetaKey], raw.Labels, raw.Meta[core.ChecklistMetaKey])
	}
}

func TestChecklistNameCollision(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateProject("ATM", "Agent Tasks Management", chActor); err != nil {
		t.Fatal(err)
	}
	a := mkV1Checklist(t, s, "ATM", "developer", "x", `{"v":1,"persona":"developer","name":"x","steps":["one"]}`)
	b := mkV1Checklist(t, s, "ATM", "manager", "x", `{"v":1,"persona":"manager","name":"x","steps":["two"]}`)
	all, _ := s.ChecklistRecords("ATM")
	if len(all) != 2 {
		t.Fatalf("list must show both colliding rows: %+v", all)
	}
	_, err := s.GetChecklist("ATM", "x")
	if err == nil || !strings.Contains(err.Error(), a.ID) || !strings.Contains(err.Error(), b.ID) {
		t.Fatalf("ambiguous get must name both task IDs: %v", err)
	}
	if err := s.EditChecklist("ATM", "x", core.ChecklistEdit{}, chActor); err == nil {
		t.Fatal("ambiguous edit must fail")
	}
	if err := s.RemoveChecklist("ATM", "x", "", chActor); err == nil {
		t.Fatal("ambiguous remove without --task must fail")
	}
	if err := s.RemoveChecklist("ATM", "x", a.ID, chActor); err != nil {
		t.Fatalf("disambiguated remove: %v", err)
	}
	got, err := s.GetChecklist("ATM", "x")
	if err != nil || got.TaskID != b.ID {
		t.Fatalf("survivor: %+v %v", got, err)
	}
}

func TestChecklistEditV2(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateProject("ATM", "Agent Tasks Management", chActor); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateChecklist("ATM", core.ChecklistRecord{Name: "main", Steps: []core.ChecklistStep{{Text: "a"}}, Suits: []string{"developer"}}, chActor); err != nil {
		t.Fatal(err)
	}
	p := "new purpose"
	if err := s.EditChecklist("ATM", "main", core.ChecklistEdit{Purpose: &p}, chActor); err != nil {
		t.Fatal(err)
	}
	steps := []core.ChecklistStep{{Text: "x", Children: []core.ChecklistStep{{Text: "x1"}}}}
	if err := s.EditChecklist("ATM", "main", core.ChecklistEdit{Steps: steps}, chActor); err != nil {
		t.Fatal(err)
	}
	req := core.ChecklistRequires{Channels: []string{"journal"}}
	if err := s.EditChecklist("ATM", "main", core.ChecklistEdit{Requires: &req}, chActor); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetChecklist("ATM", "main")
	if got.Purpose != "new purpose" || !reflect.DeepEqual(got.Steps, steps) ||
		!reflect.DeepEqual(got.Requires, req) || !reflect.DeepEqual(got.Suits, []string{"developer"}) {
		t.Fatalf("edit: %+v", got)
	}
	// all-nil edit is a no-op
	if err := s.EditChecklist("ATM", "main", core.ChecklistEdit{}, chActor); err != nil {
		t.Fatal(err)
	}
	// non-nil empty suits clears
	if err := s.EditChecklist("ATM", "main", core.ChecklistEdit{Suits: []string{}}, chActor); err != nil {
		t.Fatal(err)
	}
	got, _ = s.GetChecklist("ATM", "main")
	if len(got.Suits) != 0 {
		t.Fatalf("suits not cleared: %+v", got)
	}
	// empty non-nil steps is rejected
	if err := s.EditChecklist("ATM", "main", core.ChecklistEdit{Steps: []core.ChecklistStep{}}, chActor); !errors.Is(err, core.ErrUsage) {
		t.Fatalf("empty steps edit: want ErrUsage, got %v", err)
	}
}

func TestChecklistEditMigratesV1(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateProject("ATM", "Agent Tasks Management", chActor); err != nil {
		t.Fatal(err)
	}
	v1 := mkV1Checklist(t, s, "ATM", "developer", "x",
		`{"v":1,"persona":"developer","name":"x","purpose":"old","steps":["one","two"],"future":"kept"}`)
	p := "new"
	if err := s.EditChecklist("ATM", "x", core.ChecklistEdit{Purpose: &p}, chActor); err != nil {
		t.Fatal(err)
	}
	raw, err := s.GetTask(v1.ID)
	if err != nil {
		t.Fatal(err)
	}
	hasBare, hasPersona := false, false
	for _, l := range raw.Labels {
		if l == "ATM:checklist" {
			hasBare = true
		}
		if strings.HasPrefix(l, "ATM:checklist:") {
			hasPersona = true
		}
	}
	if !hasBare || hasPersona {
		t.Fatalf("labels after migrating edit: %v", raw.Labels)
	}
	payload := raw.Meta[core.ChecklistMetaKey]
	for _, want := range []string{`"v":2`, `"suits":["developer"]`, `"origin":"user"`, `"future":"kept"`, `"purpose":"new"`} {
		if !strings.Contains(payload, want) {
			t.Fatalf("payload missing %s: %s", want, payload)
		}
	}
	if strings.Contains(payload, `"persona"`) {
		t.Fatalf("persona key must be gone: %s", payload)
	}
	got, err := s.GetChecklist("ATM", "x")
	if err != nil || got.Purpose != "new" || len(got.Steps) != 2 {
		t.Fatalf("migrated record: %+v %v", got, err)
	}
}

// One hand-corrupted record must not disable every OTHER checklist's verbs,
// and must remain removable through its own name.
func TestChecklistCorruptRecordDoesNotPoisonNeighbours(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateProject("ATM", "Agent Tasks Management", chActor); err != nil {
		t.Fatal(err)
	}
	bad, err := s.CreateChecklist("ATM", core.ChecklistRecord{Name: "broken", Steps: []core.ChecklistStep{{Text: "a"}}}, chActor)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateChecklist("ATM", core.ChecklistRecord{Name: "good", Steps: []core.ChecklistStep{{Text: "a"}}}, chActor); err != nil {
		t.Fatal(err)
	}
	if err := s.SetTaskCapabilityMeta(bad.ID, core.ChecklistMetaKey, "garbage", chActor); err != nil {
		t.Fatal(err)
	}
	// the healthy neighbour still resolves and edits
	p := "still works"
	if err := s.EditChecklist("ATM", "good", core.ChecklistEdit{Purpose: &p}, chActor); err != nil {
		t.Fatalf("corrupt neighbour poisoned lookup: %v", err)
	}
	// the broken one is reachable by title and reports its own decode error
	if err := s.EditChecklist("ATM", "broken", core.ChecklistEdit{Purpose: &p}, chActor); err == nil || errors.Is(err, core.ErrNotFound) {
		t.Fatalf("want a decode error for the corrupt record itself, got %v", err)
	}
	// list degrades rather than failing
	recs, err := s.ChecklistRecords("ATM")
	if err != nil || len(recs) != 1 || recs[0].Name != "good" {
		t.Fatalf("records: %+v %v", recs, err)
	}
	// and the corrupt record is removable through its own noun
	if err := s.RemoveChecklist("ATM", "broken", "", chActor); err != nil {
		t.Fatalf("remove corrupt record: %v", err)
	}
	if err := s.RemoveChecklist("ATM", "nope", "", chActor); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("unknown name: %v", err)
	}
}
