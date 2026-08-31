package release

import (
	"encoding/json"
	"strings"
	"testing"

	"atm/internal/capability"
	"atm/internal/core"
)

func releaseCellFor(labels []string, meta string) *capability.Cell {
	t := core.Task{ID: "ATM-abc123", Labels: labels}
	if meta != "" {
		t.Meta = map[string]string{CapabilityName: meta}
	}
	return Cap{}.Annotate(t)
}

func shipLabel() string { return ShippedLabel("ATM") }
func containerPayload() string {
	b, _ := json.Marshal(map[string]any{"v": 1, "version": "v1-2", "members": []string{"ATM-x1"}})
	return string(b)
}

func TestAnnotateSaysNothingAboutUntouchedWork(t *testing.T) {
	if c := releaseCellFor(nil, ""); c != nil {
		t.Fatalf("cell = %+v, want nil", c)
	}
}

// Rank orders the ANNOTATE column: attention first (the broken cell), then the
// open container being built, then the open member waiting on it, then the
// finished ships. Order has real meaning to a reader scanning a release lane.
func TestAnnotateRanksCellClasses(t *testing.T) {
	cases := []struct {
		name   string
		labels []string
		meta   string
		rank   int
	}{
		{"unreadable payload", []string{"ATM:release:v1-2"}, "not json", 1},
		{"unshipped container", []string{"ATM:release:v1-2"}, containerPayload(), 2},
		{"unshipped member", []string{"ATM:release:v1-2"}, `{"v":1,"release_of":"ATM-8b8493"}`, 3},
		{"shipped container", []string{shipLabel(), "ATM:release:v1-2"}, containerPayload(), 4},
		{"shipped member", []string{shipLabel(), "ATM:release:v1-2"}, `{"v":1,"release_of":"ATM-8b8493"}`, 4},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := releaseCellFor(tc.labels, tc.meta)
			if c == nil {
				t.Fatalf("cell = nil, want rank %d", tc.rank)
			}
			if c.Rank != tc.rank {
				t.Fatalf("rank = %d, want %d (cell %+v)", c.Rank, tc.rank, c)
			}
		})
	}
}

// The ordering property behind the table: the broken cell outranks the open
// work, the open member sits behind its container, and the shipped finish is
// the settled tail.
func TestAnnotateRankOrderingProperty(t *testing.T) {
	rank := func(labels []string, meta string) int {
		c := releaseCellFor(labels, meta)
		if c == nil {
			t.Fatalf("cell = nil for labels %v", labels)
		}
		return c.Rank
	}
	order := []struct {
		name string
		rank int
	}{
		{"unreadable", rank([]string{"ATM:release:v1-2"}, "not json")},
		{"unshipped container", rank([]string{"ATM:release:v1-2"}, containerPayload())},
		{"unshipped member", rank([]string{"ATM:release:v1-2"}, `{"v":1,"release_of":"ATM-8b8493"}`)},
		{"shipped", rank([]string{shipLabel(), "ATM:release:v1-2"}, containerPayload())},
	}
	for i := 1; i < len(order); i++ {
		prev, cur := order[i-1], order[i]
		if prev.rank >= cur.rank {
			t.Fatalf("%s (rank %d) should sort before %s (rank %d)", prev.name, prev.rank, cur.name, cur.rank)
		}
	}
}

func TestAnnotateDegradesOnAnUnreadablePayload(t *testing.T) {
	c := releaseCellFor([]string{"ATM:release:v1-2"}, "not json")
	if c == nil || !strings.Contains(c.Text, "unreadable") || c.Tone != capability.ToneAttention {
		t.Fatalf("cell = %+v", c)
	}
	if strings.Contains(c.Text, "not json") {
		t.Fatalf("raw payload leaked: %q", c.Text)
	}
}
