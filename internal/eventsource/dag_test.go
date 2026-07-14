package eventsource

import (
	"slices"
	"testing"
)

// diamond builds: base ← a (replica A), base ← b (replica B), a,b ← tip.
func diamond(t *testing.T) (base, a, b, tip *Event) {
	t.Helper()
	ca, cb := testClock(1000), testClock(2000)
	base = testEvent(t, ca, replicaA, nil, ActionTaskCreated, Subject{Kind: "task"}, map[string]any{"alias": "T-1", "title": "t"})
	a = testEvent(t, ca, replicaA, []string{base.ID}, ActionTaskTitleChanged, Subject{Kind: "task", ID: base.ID}, map[string]any{"title": "from A"})
	b = testEvent(t, cb, replicaB, []string{base.ID}, ActionTaskTitleChanged, Subject{Kind: "task", ID: base.ID}, map[string]any{"title": "from B"})
	tip = testEvent(t, ca, replicaA, []string{a.ID, b.ID}, ActionTaskDescChanged, Subject{Kind: "task", ID: base.ID}, map[string]any{"description": "d"})
	return base, a, b, tip
}

func TestBuildDAGReachability(t *testing.T) {
	base, a, b, tip := diamond(t)
	d, err := BuildDAG([]*Event{tip, b, a, base}) // arrival order shuffled on purpose
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		anc, desc *Event
		want      bool
	}{
		{base, a, true}, {base, b, true}, {base, tip, true},
		{a, tip, true}, {b, tip, true},
		{a, b, false}, {b, a, false},
		{tip, base, false}, {a, base, false},
		{a, a, false}, // strict: an event does not happen-before itself
	} {
		if got := d.Reaches(tc.anc.ID, tc.desc.ID); got != tc.want {
			t.Errorf("Reaches(%s→%s) = %v, want %v", tc.anc.ID[:14], tc.desc.ID[:14], got, tc.want)
		}
	}
	if !d.Concurrent(a.ID, b.ID) || d.Concurrent(a.ID, tip.ID) || d.Concurrent(a.ID, a.ID) {
		t.Error("Concurrent wrong")
	}
}

func TestBuildDAGFrontier(t *testing.T) {
	base, a, b, tip := diamond(t)
	d, err := BuildDAG([]*Event{base, a, b})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{a.ID, b.ID}
	slices.Sort(want)
	if got := d.Frontier(); !slices.Equal(got, want) {
		t.Errorf("frontier = %v, want %v", got, want)
	}
	d2, err := BuildDAG([]*Event{base, a, b, tip})
	if err != nil {
		t.Fatal(err)
	}
	if got := d2.Frontier(); !slices.Equal(got, []string{tip.ID}) {
		t.Errorf("frontier = %v, want [tip]", got)
	}
}

func TestBuildDAGDedupesAndOrdersDeterministically(t *testing.T) {
	base, a, b, tip := diamond(t)
	d1, err := BuildDAG([]*Event{base, a, b, tip, a, base})
	if err != nil {
		t.Fatal(err)
	}
	if len(d1.Events()) != 4 {
		t.Fatalf("dedup failed: %d events", len(d1.Events()))
	}
	d2, err := BuildDAG([]*Event{tip, b, a, base})
	if err != nil {
		t.Fatal(err)
	}
	for i := range d1.Events() {
		if d1.Events()[i].ID != d2.Events()[i].ID {
			t.Fatalf("topo order depends on arrival order at index %d", i)
		}
	}
	// Parents always precede children.
	pos := map[string]int{}
	for i, e := range d1.Events() {
		pos[e.ID] = i
	}
	for _, e := range d1.Events() {
		for _, p := range e.Parents {
			if pos[p] >= pos[e.ID] {
				t.Errorf("parent %s after child %s", p[:14], e.ID[:14])
			}
		}
	}
}

func TestBuildDAGRejectsMissingParent(t *testing.T) {
	base, a, _, _ := diamond(t)
	_ = base
	if _, err := BuildDAG([]*Event{a}); err == nil {
		t.Error("expected error for missing parent")
	}
}

func TestBuildDAGRejectsCycle(t *testing.T) {
	// A parent cycle is impossible for honest hashes; forge one to prove
	// the builder terminates with an error instead of hanging.
	x := &Event{ID: "sha256:x", Parents: []string{"sha256:y"}}
	y := &Event{ID: "sha256:y", Parents: []string{"sha256:x"}}
	if _, err := BuildDAG([]*Event{x, y}); err == nil {
		t.Error("expected error for cycle")
	}
}

func TestDAGGet(t *testing.T) {
	base, a, b, tip := diamond(t)
	d, err := BuildDAG([]*Event{base, a, b, tip})
	if err != nil {
		t.Fatal(err)
	}
	if d.Get(a.ID) != a {
		t.Error("Get returned wrong event")
	}
	if d.Get("sha256:nope") != nil {
		t.Error("Get on unknown id should be nil")
	}
}
