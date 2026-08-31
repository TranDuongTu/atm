package checklist

import (
	"testing"

	"atm/internal/core"
)

func TestAnnotate(t *testing.T) {
	c := Cap{}
	if cell := c.Annotate(core.Task{ID: "t", Labels: []string{"ATM:status:open"}}); cell != nil {
		t.Fatalf("non-checklist task: %+v", cell)
	}
	payload, _ := core.EncodeChecklistPayload(core.ChecklistPayloadFrom(core.ChecklistRecord{Persona: "developer", Name: "routine", Steps: []string{"one", "two"}}))
	cell := c.Annotate(core.Task{ID: "ATM-1", Labels: []string{"ATM:checklist:developer"}, Meta: map[string]string{core.ChecklistMetaKey: payload}})
	if cell == nil || cell.Text != "checklist developer/routine · 2 steps" {
		t.Fatalf("cell: %+v", cell)
	}
	if cell.Rank != 0 {
		t.Fatalf("healthy checklist cell must stay unranked, got %d", cell.Rank)
	}
	bad := c.Annotate(core.Task{ID: "ATM-2", Labels: []string{"ATM:checklist:developer"}, Meta: map[string]string{core.ChecklistMetaKey: "garbage"}})
	if bad == nil || bad.Tone != 2 { // capability.ToneAttention
		t.Fatalf("unreadable payload must degrade to an attention cell: %+v", bad)
	}
	if bad.Rank != 1 {
		t.Fatalf("unreadable checklist cell must rank first, got %d", bad.Rank)
	}
}
