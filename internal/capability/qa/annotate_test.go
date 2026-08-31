package qa

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
		t.Fatalf("cell = %+v, want nil", c)
	}
}

func TestAnnotateDistinguishesOriginalsFromScaffolds(t *testing.T) {
	c := cellFor([]string{"ATM:qa:testing"}, "")
	if c == nil || c.Text != "qa · testing" || c.Tone != capability.ToneNeutral {
		t.Fatalf("original cell = %+v", c)
	}
	c = cellFor([]string{"ATM:qa:testing"}, `{"v":1,"part_of":"ATM-orig11"}`)
	if c == nil || c.Text != "scaffold · testing" {
		t.Fatalf("scaffold cell = %+v", c)
	}
}

func TestAnnotateMarksTheFinishSocket(t *testing.T) {
	c := cellFor([]string{"ATM:qa:done"}, "")
	if c == nil || c.Text != "✓ qa done" || c.Tone != capability.ToneOK {
		t.Fatalf("cell = %+v", c)
	}
}

// A failed verification is a verdict the manager must route, not a settled
// matter — it reads differently from the other evictions on purpose.
func TestAnnotateGivesFailedEvictionsAttention(t *testing.T) {
	c := cellFor([]string{"ATM:qa-out:failed"}, "")
	if c == nil || c.Text != "out · failed" || c.Tone != capability.ToneAttention {
		t.Fatalf("cell = %+v", c)
	}
	c = cellFor([]string{"ATM:qa-out:not-relevant"}, "")
	if c == nil || c.Tone != capability.ToneStale {
		t.Fatalf("cell = %+v", c)
	}
}

func TestAnnotateDegradesOnAnUnreadablePayload(t *testing.T) {
	c := cellFor([]string{"ATM:qa:testing"}, "not json")
	if c == nil || !strings.Contains(c.Text, "unreadable") || c.Tone != capability.ToneAttention {
		t.Fatalf("cell = %+v", c)
	}
	if strings.Contains(c.Text, "not json") {
		t.Fatalf("raw payload leaked: %q", c.Text)
	}
}

// Rank orders the ANNOTATE column: attention first (a broken cell, then a
// failed verdict the manager must route), then active testing, then finished,
// then the settled evictions.
func TestAnnotateRanksCellClasses(t *testing.T) {
	cases := []struct {
		name   string
		labels []string
		meta   string
		rank   int
	}{
		{"unreadable payload", []string{"ATM:qa:testing"}, "not json", 1},
		{"out failed", []string{"ATM:qa-out:failed"}, "", 2},
		{"testing original", []string{"ATM:qa:testing"}, "", 3},
		{"testing scaffold", []string{"ATM:qa:testing"}, `{"v":1,"part_of":"ATM-orig11"}`, 3},
		{"done", []string{"ATM:qa:done"}, "", 4},
		{"done scaffold", []string{"ATM:qa:done"}, `{"v":1,"part_of":"ATM-orig11"}`, 4},
		{"out not-relevant", []string{"ATM:qa-out:not-relevant"}, "", 5},
		{"out covered-by", []string{"ATM:qa-out:covered-by"}, "", 5},
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

// The ordering property behind the table: the failed verdict outranks active
// work, and both sit between the attention cell and the finished/settled tail.
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
		{"unreadable", rank([]string{"ATM:qa:testing"}, "not json")},
		{"out failed", rank([]string{"ATM:qa-out:failed"}, "")},
		{"testing", rank([]string{"ATM:qa:testing"}, "")},
		{"done", rank([]string{"ATM:qa:done"}, "")},
		{"out settled", rank([]string{"ATM:qa-out:not-relevant"}, "")},
	}
	for i := 1; i < len(order); i++ {
		prev, cur := order[i-1], order[i]
		if prev.rank >= cur.rank {
			t.Fatalf("%s (rank %d) should sort before %s (rank %d)", prev.name, prev.rank, cur.name, cur.rank)
		}
	}
}
