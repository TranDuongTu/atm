package answer

import (
	"strings"
	"testing"
	"unicode/utf8"

	"atm/internal/core"
)

// The whole point of the redistribution: a long source gets the slack the
// short ones did not use, instead of being cut at a flat budget/N cap.
func TestAllotRedistributesUnusedShare(t *testing.T) {
	got := allot([]int{10, 10, 10, 5000}, 1000)
	want := []int{10, 10, 10, 970}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("allot = %v, want %v", got, want)
		}
	}
}

func TestAllotEverythingFits(t *testing.T) {
	got := allot([]int{5, 5}, 1000)
	if got[0] != 5 || got[1] != 5 {
		t.Errorf("allot = %v, want [5 5] — nothing should be clipped when all of it fits", got)
	}
}

func TestAllotAllOversizedSplitsEvenly(t *testing.T) {
	got := allot([]int{9000, 9000}, 1000)
	if got[0] != 500 || got[1] != 500 {
		t.Errorf("allot = %v, want [500 500]", got)
	}
}

func TestClipIsRuneSafeAndMarked(t *testing.T) {
	got := clip(strings.Repeat("あ", 100), 10)
	if !utf8.ValidString(got) {
		t.Errorf("clip produced invalid UTF-8: %q", got)
	}
	if !strings.HasSuffix(got, "…[truncated]") {
		t.Errorf("clip = %q, want a visible truncation marker so the model knows it holds a fragment", got)
	}
}

// Hydration is an improvement, never a requirement: a hit with no document
// falls back to its snippet rather than losing its source block.
func TestBuildSourcesFallsBackToSnippet(t *testing.T) {
	hits := []core.Hit{{ID: "ATM-1", Snippet: "snippet one"}, {ID: "ATM-2", Snippet: "snippet two"}}
	got := buildSources(hits, map[string]string{"ATM-1": "the full body of one"}, 0)
	if got[0].text != "the full body of one" {
		t.Errorf("hydrated source text = %q, want the full body", got[0].text)
	}
	if got[1].text != "snippet two" {
		t.Errorf("un-hydrated source text = %q, want the snippet fallback", got[1].text)
	}
}

func TestBuildSourcesWithNilDocsUsesEverySnippet(t *testing.T) {
	hits := []core.Hit{{ID: "ATM-1", Snippet: "only this"}}
	got := buildSources(hits, nil, 0)
	if len(got) != 1 || got[0].text != "only this" {
		t.Errorf("buildSources with nil docs = %+v, want the snippet", got)
	}
}
