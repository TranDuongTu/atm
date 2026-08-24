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
