package checklist

import (
	"reflect"
	"testing"

	"atm/internal/core"
	"atm/skills"
)

func TestSeedRecord(t *testing.T) {
	seed := skills.ChecklistSeed{
		Name:    "scrum-backlog",
		Purpose: "sweep <CODE>'s scrum flow",
		Suits:   []string{"manager"},
		Requires: skills.SeedRequires{
			Capabilities: []string{"scrum"},
			Channels:     []string{"journal"},
		},
		Origin: "shipped:scrum",
		Steps: []skills.SeedStep{
			{Text: "list <CODE> inbox", Children: []skills.SeedStep{{Text: "atm task list --project <CODE>"}}},
		},
	}
	got := SeedRecord("ATM", seed)
	want := core.ChecklistRecord{
		Name:    "scrum-backlog",
		Purpose: "sweep ATM's scrum flow",
		Suits:   []string{"manager"},
		Requires: core.ChecklistRequires{
			Capabilities: []string{"scrum"},
			Channels:     []string{"journal"},
		},
		Origin: "shipped:scrum",
		Steps: []core.ChecklistStep{
			{Text: "list ATM inbox", Children: []core.ChecklistStep{{Text: "atm task list --project ATM"}}},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("SeedRecord:\n got %+v\nwant %+v", got, want)
	}
	if got.TaskID != "" {
		t.Fatalf("TaskID must be empty before creation: %q", got.TaskID)
	}
}
