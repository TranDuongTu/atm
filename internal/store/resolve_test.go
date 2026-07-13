package store

import (
	"errors"
	"testing"
)

func resolverFor(labels ...Label) *resolver {
	return newResolver("ATM", labels)
}

func TestResolverAtomForms(t *testing.T) {
	r := resolverFor(
		Label{Name: "ATM:status:open"},
		Label{Name: "ATM:sprint:next"},
	)
	task := &Task{ID: "ATM-0001", Labels: []string{"ATM:status:open", "ATM:sprint:next"}}

	cases := map[string]bool{
		"status:open":                    true,  // stored label, present
		"status:done":                    false, // stored label, absent
		"status:*":                       true,  // namespace predicate: has SOME status label
		"priority:*":                     false, // namespace predicate: has NO priority label
		"NOT priority:*":                 true,  // "unprioritized"
		"status:open AND NOT priority:*": true,
		"status:done OR sprint:next":     true,
	}
	for src, want := range cases {
		n, err := ParseExpr(src)
		if err != nil {
			t.Fatalf("ParseExpr(%q): %v", src, err)
		}
		got, err := r.Matches(task, n)
		if err != nil {
			t.Fatalf("Matches(%q): %v", src, err)
		}
		if got != want {
			t.Errorf("Matches(%q) = %v, want %v", src, got, want)
		}
	}
}

func TestResolverComposesBoards(t *testing.T) {
	// release-blockers references release-v1.0.0, which is itself a board.
	r := resolverFor(
		Label{Name: "ATM:release-v1.0.0", Expr: "release:v1-0-0 AND NOT status:done"},
		Label{Name: "ATM:release-blockers", Expr: "release-v1.0.0 AND priority:high"},
	)
	blocker := &Task{ID: "ATM-0001", Labels: []string{"ATM:release:v1-0-0", "ATM:priority:high"}}
	shipped := &Task{ID: "ATM-0002", Labels: []string{"ATM:release:v1-0-0", "ATM:priority:high", "ATM:status:done"}}

	n, _ := ParseExpr("release-blockers")
	if got, err := r.Matches(blocker, n); err != nil || !got {
		t.Errorf("blocker: got %v (err %v), want true", got, err)
	}
	if got, err := r.Matches(shipped, n); err != nil || got {
		t.Errorf("shipped: got %v (err %v), want false — status:done excludes it", got, err)
	}
}

// A cycle cannot be produced through the write path (Task 5 rejects it), but
// a MERGE can synthesize one that no replica ever wrote: replica A points
// board a at b while replica B points b at a. See ATM-0105-c0004. So the
// resolver must catch it rather than recursing forever.
func TestResolverRejectsMergeInducedCycle(t *testing.T) {
	r := resolverFor(
		Label{Name: "ATM:a", Expr: "b"},
		Label{Name: "ATM:b", Expr: "a"},
	)
	n, _ := ParseExpr("a")
	_, err := r.Matches(&Task{ID: "ATM-0001"}, n)
	if !errors.Is(err, ErrCyclicExpr) {
		t.Fatalf("err = %v, want ErrCyclicExpr", err)
	}
}

func TestResolverUnknownAtomIsNotAMatch(t *testing.T) {
	// An atom naming no live label is simply absent — not an error. A label
	// removed while a board still references it must not break the board.
	r := resolverFor()
	n, _ := ParseExpr("ghost")
	got, err := r.Matches(&Task{ID: "ATM-0001"}, n)
	if err != nil || got {
		t.Fatalf("got %v (err %v), want false, nil", got, err)
	}
}
