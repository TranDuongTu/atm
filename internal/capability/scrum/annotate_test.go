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

// Rank orders the ANNOTATE column: attention first, then active work in
// progression order (most in-flight first), then finished, then evictions.
func TestAnnotateRanksCellClasses(t *testing.T) {
	cases := []struct {
		name   string
		labels []string
		meta   string
		rank   int
	}{
		{"unreadable payload", []string{"ATM:scrum:task", "ATM:scrum-stage:planned"}, "not json", 1},
		{"implementing", []string{"ATM:scrum:task", "ATM:scrum-stage:implementing"}, "", 2},
		{"review", []string{"ATM:scrum:bug", "ATM:scrum-stage:review"}, "", 3},
		{"planned", []string{"ATM:scrum:task", "ATM:scrum-stage:planned"}, "", 4},
		{"brainstormed", []string{"ATM:scrum:epic", "ATM:scrum-stage:brainstormed"}, "", 5},
		{"unstamped claim", []string{"ATM:scrum:story"}, "", 6},
		{"done", []string{"ATM:scrum:task", "ATM:scrum-stage:done"}, "", 7},
		{"out duplicate", []string{"ATM:scrum-out:duplicate"}, "", 8},
		{"out out-of-scope", []string{"ATM:scrum-out:out-of-scope"}, "", 8},
		{"out not-worth-it", []string{"ATM:scrum-out:not-worth-it"}, "", 8},
		{"out covered-by", []string{"ATM:scrum-out:covered-by"}, "", 8},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := cellFor(tc.labels, tc.meta)
			if c == nil {
				t.Fatalf("cell = nil, want rank %d", tc.rank)
			}
			if c.Rank != tc.rank {
				t.Fatalf("rank = %d, want %d (cell %+v)", c.Rank, tc.rank, c)
			}
		})
	}
}

// The ordering property behind the table: each active stage outranks the
// next-less-in-flight one, and the whole active band sits between the
// attention cell and the finished/evicted tail.
func TestAnnotateRankOrderingProperty(t *testing.T) {
	rank := func(labels []string, meta string) int {
		c := cellFor(labels, meta)
		if c == nil {
			t.Fatalf("cell = nil for labels %v", labels)
		}
		return c.Rank
	}
	order := []struct {
		name string
		rank int
	}{
		{"unreadable", rank([]string{"ATM:scrum:task", "ATM:scrum-stage:planned"}, "not json")},
		{"implementing", rank([]string{"ATM:scrum:task", "ATM:scrum-stage:implementing"}, "")},
		{"review", rank([]string{"ATM:scrum:task", "ATM:scrum-stage:review"}, "")},
		{"planned", rank([]string{"ATM:scrum:task", "ATM:scrum-stage:planned"}, "")},
		{"brainstormed", rank([]string{"ATM:scrum:task", "ATM:scrum-stage:brainstormed"}, "")},
		{"unstamped", rank([]string{"ATM:scrum:task"}, "")},
		{"done", rank([]string{"ATM:scrum:task", "ATM:scrum-stage:done"}, "")},
		{"out", rank([]string{"ATM:scrum-out:duplicate"}, "")},
	}
	for i := 1; i < len(order); i++ {
		prev, cur := order[i-1], order[i]
		if prev.rank >= cur.rank {
			t.Fatalf("%s (rank %d) should sort before %s (rank %d)", prev.name, prev.rank, cur.name, cur.rank)
		}
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
