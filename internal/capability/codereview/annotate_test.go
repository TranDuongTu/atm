package codereview

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

func TestAnnotateShowsTheStateAndAShortPR(t *testing.T) {
	c := cellFor([]string{"ATM:codereview:scheduled"}, `{"v":1,"pr":"#142"}`)
	if c == nil || c.Text != "review · scheduled · #142" {
		t.Fatalf("cell = %+v", c)
	}
	c = cellFor([]string{"ATM:codereview:reviewing"}, `{"v":1,"pr":"#142"}`)
	if c == nil || c.Text != "reviewing · #142" || c.Tone != capability.ToneNeutral {
		t.Fatalf("cell = %+v", c)
	}
}

// A full URL would crowd the state out of the cell, so it stays in the detail
// view instead.
func TestAnnotateOmitsALongPR(t *testing.T) {
	c := cellFor([]string{"ATM:codereview:reviewing"}, `{"v":1,"pr":"https://github.com/o/r/pull/142"}`)
	if c == nil || c.Text != "reviewing" {
		t.Fatalf("cell = %+v", c)
	}
}

func TestAnnotateMarksTheFinishSocket(t *testing.T) {
	c := cellFor([]string{"ATM:codereview:done"}, `{"v":1,"pr":"#142"}`)
	if c == nil || c.Text != "✓ reviewed" || c.Tone != capability.ToneOK {
		t.Fatalf("cell = %+v", c)
	}
}

func TestAnnotateMarksEvictions(t *testing.T) {
	c := cellFor([]string{"ATM:codereview-out:not-warranted"}, "")
	if c == nil || c.Text != "out · not-warranted" || c.Tone != capability.ToneStale {
		t.Fatalf("cell = %+v", c)
	}
}

// The verbs cannot produce a claim without a PR; only a hand-assigned label
// can, and the cell says so rather than showing a plausible-looking review.
func TestAnnotateFlagsAClaimWithNoPR(t *testing.T) {
	c := cellFor([]string{"ATM:codereview:scheduled"}, "")
	if c == nil || !strings.Contains(c.Text, "no PR") || c.Tone != capability.ToneAttention {
		t.Fatalf("cell = %+v", c)
	}
}

func TestAnnotateDegradesOnAnUnreadablePayload(t *testing.T) {
	c := cellFor([]string{"ATM:codereview:scheduled"}, "not json")
	if c == nil || !strings.Contains(c.Text, "unreadable") || c.Tone != capability.ToneAttention {
		t.Fatalf("cell = %+v", c)
	}
	if strings.Contains(c.Text, "not json") {
		t.Fatalf("raw payload leaked: %q", c.Text)
	}
}

// Rank orders the ANNOTATE column: attention first (a broken cell, then a
// claim with no PR), then reviews in flight ahead of merely scheduled ones,
// then finished, then the settled evictions.
func TestAnnotateRanksCellClasses(t *testing.T) {
	cases := []struct {
		name   string
		labels []string
		meta   string
		rank   int
	}{
		{"unreadable payload", []string{"ATM:codereview:scheduled"}, "not json", 1},
		{"scheduled no PR", []string{"ATM:codereview:scheduled"}, "", 2},
		{"reviewing no PR", []string{"ATM:codereview:reviewing"}, "", 2},
		{"reviewing", []string{"ATM:codereview:reviewing"}, `{"v":1,"pr":"#142"}`, 3},
		{"reviewing long PR", []string{"ATM:codereview:reviewing"}, `{"v":1,"pr":"https://github.com/o/r/pull/142"}`, 3},
		{"scheduled", []string{"ATM:codereview:scheduled"}, `{"v":1,"pr":"#142"}`, 4},
		{"done", []string{"ATM:codereview:done"}, `{"v":1,"pr":"#142"}`, 5},
		{"out not-warranted", []string{"ATM:codereview-out:not-warranted"}, "", 6},
		{"out superseded", []string{"ATM:codereview-out:superseded"}, "", 6},
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

// The ordering property behind the table: the attention cells lead, a review
// in flight outranks a scheduled one, and the finished/settled tail closes.
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
		{"unreadable", rank([]string{"ATM:codereview:scheduled"}, "not json")},
		{"no PR", rank([]string{"ATM:codereview:scheduled"}, "")},
		{"reviewing", rank([]string{"ATM:codereview:reviewing"}, `{"v":1,"pr":"#142"}`)},
		{"scheduled", rank([]string{"ATM:codereview:scheduled"}, `{"v":1,"pr":"#142"}`)},
		{"done", rank([]string{"ATM:codereview:done"}, `{"v":1,"pr":"#142"}`)},
		{"out", rank([]string{"ATM:codereview-out:not-warranted"}, "")},
	}
	for i := 1; i < len(order); i++ {
		prev, cur := order[i-1], order[i]
		if prev.rank >= cur.rank {
			t.Fatalf("%s (rank %d) should sort before %s (rank %d)", prev.name, prev.rank, cur.name, cur.rank)
		}
	}
}
