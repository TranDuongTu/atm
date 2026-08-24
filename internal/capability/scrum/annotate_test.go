package scrum

import (
	"strings"
	"testing"

	"atm/internal/capability"
	"atm/internal/core"
)

func cellFor(labels []string, meta string) *capability.Cell {
	t := core.Task{ID: "ATM-abc123", Labels: labels}
	if meta != "" {
		t.Meta = map[string]string{CapabilityName: meta}
	}
	return Cap{}.Annotate(t)
}

func TestAnnotateSaysNothingAboutUnclaimedWork(t *testing.T) {
	if c := cellFor(nil, ""); c != nil {
		t.Fatalf("cell = %+v, want nil for a task scrum has not claimed", c)
	}
	if c := cellFor([]string{"ATM:status:open"}, ""); c != nil {
		t.Fatalf("cell = %+v, want nil", c)
	}
}

func TestAnnotateShowsTypeAndStage(t *testing.T) {
	c := cellFor([]string{"ATM:scrum:task", "ATM:scrum-stage:implementing"}, "")
	if c == nil || c.Text != "task · implementing" || c.Tone != capability.ToneNeutral {
		t.Fatalf("cell = %+v", c)
	}
	c = cellFor([]string{"ATM:scrum:bug", "ATM:scrum-stage:review"}, "")
	if c == nil || c.Text != "bug · review" {
		t.Fatalf("cell = %+v", c)
	}
}

func TestAnnotateMarksTheFinishSocket(t *testing.T) {
	c := cellFor([]string{"ATM:scrum:task", "ATM:scrum-stage:done"}, "")
	if c == nil || c.Text != "task · ✓ done" || c.Tone != capability.ToneOK {
		t.Fatalf("cell = %+v", c)
	}
}

func TestAnnotateMarksAClaimWithNoStage(t *testing.T) {
	c := cellFor([]string{"ATM:scrum:story"}, "")
	if c == nil || c.Text != "story · —" || c.Tone != capability.ToneNeutral {
		t.Fatalf("cell = %+v", c)
	}
}

func TestAnnotateMarksEvictions(t *testing.T) {
	c := cellFor([]string{"ATM:scrum-out:duplicate"}, "")
	if c == nil || c.Text != "out · duplicate" || c.Tone != capability.ToneStale {
		t.Fatalf("cell = %+v", c)
	}
}

func TestAnnotateDegradesOnAnUnreadablePayload(t *testing.T) {
	c := cellFor([]string{"ATM:scrum:task", "ATM:scrum-stage:planned"}, "not json")
	if c == nil || !strings.Contains(c.Text, "unreadable") || c.Tone != capability.ToneAttention {
		t.Fatalf("cell = %+v", c)
	}
	if strings.Contains(c.Text, "not json") {
		t.Fatalf("raw payload leaked into the cell: %q", c.Text)
	}
}

// Annotate is pure over the task VALUE: no store, so no child rollups. A
// story's "3/5 done" belongs to the reporter, not this cell.
func TestAnnotateReadsNoStore(t *testing.T) {
	c := cellFor([]string{"ATM:scrum:epic", "ATM:scrum-stage:implementing"}, `{"v":1,"part_of":"ATM-parent"}`)
	if c == nil || c.Text != "epic · implementing" {
		t.Fatalf("cell = %+v", c)
	}
}
