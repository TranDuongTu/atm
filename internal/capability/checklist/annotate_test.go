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
	// v2 record: bare label, nested steps counted recursively, name only.
	payload, _ := core.EncodeChecklistPayload(core.ChecklistPayloadFrom(core.ChecklistRecord{
		Name:   "scrum-backlog",
		Steps:  []core.ChecklistStep{{Text: "a", Children: []core.ChecklistStep{{Text: "a1"}, {Text: "a2", Children: []core.ChecklistStep{{Text: "a2i"}}}}}},
		Suits:  []string{"manager"},
		Origin: "shipped:scrum",
	}))
	cell := c.Annotate(core.Task{ID: "ATM-1", Labels: []string{"ATM:checklist"}, Meta: map[string]string{core.ChecklistMetaKey: payload}})
	if cell == nil || cell.Text != "checklist scrum-backlog · 4 steps" {
		t.Fatalf("v2 cell: %+v", cell)
	}
	if cell.Rank != 0 {
		t.Fatalf("healthy checklist cell must stay unranked, got %d", cell.Rank)
	}
	// v1 record: persona label, flat steps — name only, no persona prefix.
	v1 := core.Task{ID: "ATM-2", Labels: []string{"ATM:checklist:developer"},
		Meta: map[string]string{core.ChecklistMetaKey: `{"v":1,"persona":"developer","name":"routine","steps":["one","two"]}`}}
	cell = c.Annotate(v1)
	if cell == nil || cell.Text != "checklist routine · 2 steps" {
		t.Fatalf("v1 cell: %+v", cell)
	}
	bad := c.Annotate(core.Task{ID: "ATM-3", Labels: []string{"ATM:checklist:developer"}, Meta: map[string]string{core.ChecklistMetaKey: "garbage"}})
	if bad == nil || bad.Tone != 2 { // capability.ToneAttention
		t.Fatalf("unreadable payload must degrade to an attention cell: %+v", bad)
	}
	if bad.Rank != 1 {
		t.Fatalf("unreadable checklist cell must rank first, got %d", bad.Rank)
	}
}
