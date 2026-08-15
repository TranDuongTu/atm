// internal/core/checklist_test.go
package core

import (
	"strings"
	"testing"
)

func TestChecklistPayloadRoundTrip(t *testing.T) {
	rec := ChecklistRecord{Persona: "developer", Name: "main", Purpose: "day to day", Steps: []string{"a", "b"}}
	enc, err := EncodeChecklistPayload(ChecklistPayloadFrom(rec))
	if err != nil {
		t.Fatal(err)
	}
	task := Task{ID: "ATM-1", Title: "developer/main", Labels: []string{"ATM:checklist:developer"}, Meta: map[string]string{ChecklistMetaKey: enc}}
	got, err := ChecklistFromTask("ATM", task)
	if err != nil {
		t.Fatal(err)
	}
	if got.Persona != "developer" || got.Name != "main" || got.Purpose != "day to day" || len(got.Steps) != 2 || got.Steps[1] != "b" {
		t.Fatalf("round trip: %+v", got)
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

func TestChecklistFromTaskNonChecklistIsNil(t *testing.T) {
	got, err := ChecklistFromTask("ATM", Task{ID: "ATM-2", Labels: []string{"ATM:status:open"}})
	if err != nil || got != nil {
		t.Fatalf("want nil,nil; got %v,%v", got, err)
	}
}

func TestChecklistFromTaskMalformedPayloadErrors(t *testing.T) {
	task := Task{ID: "ATM-3", Labels: []string{"ATM:checklist:developer"}, Meta: map[string]string{ChecklistMetaKey: "{not json"}}
	if _, err := ChecklistFromTask("ATM", task); err == nil {
		t.Fatal("want decode error")
	}
}
