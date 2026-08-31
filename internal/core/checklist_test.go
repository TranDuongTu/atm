// internal/core/checklist_test.go
package core

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestChecklistLabelHelpers(t *testing.T) {
	if got := ChecklistLabel("ATM"); got != "ATM:checklist" {
		t.Fatalf("ChecklistLabel = %q", got)
	}
	if got := ChecklistPersonaLabelPrefix("ATM"); got != "ATM:checklist:" {
		t.Fatalf("ChecklistPersonaLabelPrefix = %q", got)
	}
}

func TestValidChecklistOrigin(t *testing.T) {
	for _, ok := range []string{"user", "shipped:atm", "shipped:scrum", "shipped:my-cap"} {
		if !ValidChecklistOrigin(ok) {
			t.Fatalf("%q should be valid", ok)
		}
	}
	for _, bad := range []string{"", "shipped:", "shipped:Bad Name", "vendor", "user:x"} {
		if ValidChecklistOrigin(bad) {
			t.Fatalf("%q should be invalid", bad)
		}
	}
}

func TestChecklistStepCountRecursive(t *testing.T) {
	steps := []ChecklistStep{
		{Text: "a", Children: []ChecklistStep{{Text: "a1"}, {Text: "a2", Children: []ChecklistStep{{Text: "a2i"}}}}},
		{Text: "b"},
	}
	if got := ChecklistStepCount(steps); got != 5 {
		t.Fatalf("count = %d, want 5", got)
	}
}

func TestEncodeStampsV2AndPreservesUnknownFields(t *testing.T) {
	m := map[string]any{"name": "x", "future": "field", "v": 1}
	enc, err := EncodeChecklistPayload(m)
	if err != nil {
		t.Fatal(err)
	}
	var back map[string]any
	if err := json.Unmarshal([]byte(enc), &back); err != nil {
		t.Fatal(err)
	}
	if back["v"] != float64(2) {
		t.Fatalf("v = %v, want 2", back["v"])
	}
	if back["future"] != "field" {
		t.Fatalf("unknown field dropped: %s", enc)
	}
	if m["v"] != 1 {
		t.Fatalf("input mutated: %v", m)
	}
}

func TestChecklistFromTaskV2(t *testing.T) {
	payload := `{"v":2,"name":"scrum-backlog","purpose":"p","steps":[{"text":"a","children":[{"text":"a1"}]}],"suits":["manager"],"requires":{"capabilities":["scrum"]},"origin":"shipped:scrum"}`
	task := Task{ID: "ATM-1", Title: "scrum-backlog",
		Labels: []string{"ATM:checklist"}, Meta: map[string]string{ChecklistMetaKey: payload}}
	rec, err := ChecklistFromTask("ATM", task)
	if err != nil {
		t.Fatal(err)
	}
	want := &ChecklistRecord{
		TaskID:  "ATM-1",
		Name:    "scrum-backlog",
		Purpose: "p",
		Steps:   []ChecklistStep{{Text: "a", Children: []ChecklistStep{{Text: "a1"}}}},
		Suits:   []string{"manager"},
		Requires: ChecklistRequires{
			Capabilities: []string{"scrum"},
		},
		Origin: "shipped:scrum",
	}
	if !reflect.DeepEqual(rec, want) {
		t.Fatalf("got %+v, want %+v", rec, want)
	}
}

func TestChecklistFromTaskV1ReadCompat(t *testing.T) {
	payload := `{"v":1,"persona":"developer","name":"pr-convention","purpose":"p","steps":["one","two"]}`
	task := Task{ID: "ATM-2", Title: "developer/pr-convention",
		Labels: []string{"ATM:checklist:developer"}, Meta: map[string]string{ChecklistMetaKey: payload}}
	rec, err := ChecklistFromTask("ATM", task)
	if err != nil {
		t.Fatal(err)
	}
	want := &ChecklistRecord{
		TaskID:  "ATM-2",
		Name:    "pr-convention",
		Purpose: "p",
		Steps:   []ChecklistStep{{Text: "one"}, {Text: "two"}},
		Suits:   []string{"developer"},
		Origin:  "user",
	}
	if !reflect.DeepEqual(rec, want) {
		t.Fatalf("got %+v, want %+v", rec, want)
	}
}

func TestChecklistFromTaskV1NameFallsBackToTitle(t *testing.T) {
	payload := `{"v":1,"persona":"developer","steps":["one"]}`
	task := Task{ID: "ATM-3", Title: "developer/pr-convention",
		Labels: []string{"ATM:checklist:developer"}, Meta: map[string]string{ChecklistMetaKey: payload}}
	rec, err := ChecklistFromTask("ATM", task)
	if err != nil {
		t.Fatal(err)
	}
	if rec.Name != "pr-convention" {
		t.Fatalf("Name = %q, want title-derived pr-convention", rec.Name)
	}
}

func TestChecklistFromTaskNonChecklistIsNil(t *testing.T) {
	got, err := ChecklistFromTask("ATM", Task{ID: "ATM-4", Labels: []string{"ATM:status:open"}})
	if err != nil || got != nil {
		t.Fatalf("want nil,nil; got %v,%v", got, err)
	}
}

func TestChecklistFromTaskMalformedPayloadErrors(t *testing.T) {
	for _, labels := range [][]string{
		{"ATM:checklist"},
		{"ATM:checklist:developer"},
	} {
		task := Task{ID: "ATM-5", Labels: labels, Meta: map[string]string{ChecklistMetaKey: "{not json"}}
		_, err := ChecklistFromTask("ATM", task)
		if err == nil || !strings.Contains(err.Error(), "ATM-5") {
			t.Fatalf("labels %v: want decode error naming the task, got %v", labels, err)
		}
	}
}

func TestMigrateChecklistMapV2(t *testing.T) {
	m := map[string]any{"v": float64(1), "persona": "developer", "name": "x", "purpose": "p",
		"steps": []any{"one", "two"}, "future": "kept"}
	out := MigrateChecklistMapV2(m, "developer")
	if _, ok := out["persona"]; ok {
		t.Fatalf("persona key must be dropped: %v", out)
	}
	if !reflect.DeepEqual(out["suits"], []any{"developer"}) {
		t.Fatalf("suits = %v", out["suits"])
	}
	wantSteps := []any{
		map[string]any{"text": "one"},
		map[string]any{"text": "two"},
	}
	if !reflect.DeepEqual(out["steps"], wantSteps) {
		t.Fatalf("steps = %v", out["steps"])
	}
	if out["origin"] != "user" {
		t.Fatalf("origin = %v", out["origin"])
	}
	if out["future"] != "kept" {
		t.Fatalf("unknown field dropped: %v", out)
	}
	if _, ok := m["suits"]; ok {
		t.Fatalf("input mutated: %v", m)
	}
	// argument wins over the payload's own persona key
	out2 := MigrateChecklistMapV2(map[string]any{"persona": "old"}, "newer")
	if !reflect.DeepEqual(out2["suits"], []any{"newer"}) {
		t.Fatalf("suits = %v, want label persona to win", out2["suits"])
	}
	// payload persona is the fallback when no label persona is known
	out3 := MigrateChecklistMapV2(map[string]any{"persona": "old"}, "")
	if !reflect.DeepEqual(out3["suits"], []any{"old"}) {
		t.Fatalf("suits = %v, want payload persona fallback", out3["suits"])
	}
}

func TestChecklistPayloadUnknownFieldsSurvive(t *testing.T) {
	m, err := DecodeChecklistPayload(`{"v":1,"name":"main","future":"x"}`)
	if err != nil {
		t.Fatal(err)
	}
	enc, err := EncodeChecklistPayload(m)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(enc, `"future":"x"`) {
		t.Fatalf("unknown field dropped: %s", enc)
	}
}

func TestChecklistPayloadFromRoundTrips(t *testing.T) {
	rec := ChecklistRecord{Name: "n", Purpose: "p",
		Steps: []ChecklistStep{{Text: "a", Children: []ChecklistStep{{Text: "a1"}}}},
		Suits: []string{"manager"}, Requires: ChecklistRequires{Capabilities: []string{"scrum"}},
		Origin: "user"}
	enc, err := EncodeChecklistPayload(ChecklistPayloadFrom(rec))
	if err != nil {
		t.Fatal(err)
	}
	task := Task{ID: "ATM-6", Title: "n", Labels: []string{"ATM:checklist"}, Meta: map[string]string{ChecklistMetaKey: enc}}
	got, err := ChecklistFromTask("ATM", task)
	if err != nil {
		t.Fatal(err)
	}
	rec.TaskID = "ATM-6"
	if !reflect.DeepEqual(got, &rec) {
		t.Fatalf("round trip: got %+v, want %+v", got, rec)
	}
}
