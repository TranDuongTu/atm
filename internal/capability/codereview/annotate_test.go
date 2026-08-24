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
