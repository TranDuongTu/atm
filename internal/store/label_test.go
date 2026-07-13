package store

import (
	"errors"
	"testing"
)

func TestLabelAddValidatesRegexAndProjectPrefix(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", testActor)
	for _, bad := range []string{"type:bug", "xyz:type:bug", "ATM:", "ATM:type:", "ATM:Type:Bug"} {
		if err := s.LabelAdd(bad, "", "", testActor); err == nil {
			t.Fatalf("expected error for label %q", bad)
		}
	}
}

func TestLabelAddRejectsUnknownProjectPrefix(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", testActor)
	if err := s.LabelAdd("XYZ:type:bug", "", "", testActor); err == nil {
		t.Fatal("expected error for unknown project prefix XYZ")
	}
}

func TestLabelAddUpsertPreservesDescription(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", testActor)
	_ = s.LabelAdd("ATM:type:bug", "first", "", testActor)
	_ = s.LabelAdd("ATM:type:bug", "", "", testActor) // empty desc preserves
	l, _ := s.LabelShow("ATM:type:bug")
	if l.Description != "first" {
		t.Fatalf("description = %q want first", l.Description)
	}
	_ = s.LabelAdd("ATM:type:bug", "second", "", testActor) // non-empty updates
	l, _ = s.LabelShow("ATM:type:bug")
	if l.Description != "second" {
		t.Fatalf("description = %q want second", l.Description)
	}
}

func TestLabelRemoveSoftRetainsUsage(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", testActor)
	_, _ = s.CreateTask("ATM", "t", "", []string{"ATM:type:bug"}, testActor)
	r, err := s.LabelRemove("ATM:type:bug", testActor)
	if err != nil {
		t.Fatal(err)
	}
	if r.RetainedUsage != 1 {
		t.Fatalf("retained_usage = %d want 1", r.RetainedUsage)
	}
	// Removed label is gone from the registry (soft removal drops the entry).
	if _, err := s.LabelShow("ATM:type:bug"); err == nil {
		t.Fatal("expected ErrNotFound for removed label")
	}
	// Existing task still carries the label string (soft removal).
	tk, _ := s.GetTask("ATM-0001")
	if !containsLabel(tk.Labels, "ATM:type:bug") {
		t.Fatal("existing task must retain the label string after registry removal")
	}
}

func containsLabel(labels []string, want string) bool {
	for _, l := range labels {
		if l == want {
			return true
		}
	}
	return false
}

func TestLabelListFiltersByProjectAndNamespace(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", testActor)
	_, _ = s.CreateProject("SCY", "y", testActor)
	_ = s.LabelAdd("ATM:custom:a", "", "", testActor)
	_ = s.LabelAdd("ATM:custom:b", "", "", testActor)
	_ = s.LabelAdd("SCY:custom:a", "", "", testActor)
	// ATM has 16 seeded + 2 custom = 18.
	if got := len(s.LabelList("ATM", "")); got != 18 {
		t.Fatalf("ATM labels = %d want 18", got)
	}
	// Filter to the custom namespace.
	if got := len(s.LabelList("ATM", "custom")); got != 2 {
		t.Fatalf("ATM:custom labels = %d want 2", got)
	}
}

func TestNamespacesDistinctSorted(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", testActor)
	_ = s.LabelAdd("ATM:hot", "", "", testActor) // unnamespaced tag
	_ = s.LabelAdd("ATM:custom:x", "", "", testActor)
	got := s.Namespaces("ATM")
	want := []string{"comment", "context", "custom", "priority", "status"}
	if len(got) != 5 || got[0] != "comment" || got[4] != "status" {
		t.Fatalf("Namespaces = %v want %v", got, want)
	}
}

func TestLabelSeedSetsDescriptionOnCreate(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", testActor)
	if err := s.LabelSeed("ATM:custom:x", "seed desc", "", testActor); err != nil {
		t.Fatal(err)
	}
	l, _ := s.LabelShow("ATM:custom:x")
	if l.Description != "seed desc" {
		t.Fatalf("description = %q want \"seed desc\"", l.Description)
	}
}

func TestLabelSeedPreservesExistingDescription(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", testActor)
	_ = s.LabelAdd("ATM:type:bug", "human edited", "", testActor)
	if err := s.LabelSeed("ATM:type:bug", "seed default", "", testActor); err != nil {
		t.Fatal(err)
	}
	l, _ := s.LabelShow("ATM:type:bug")
	if l.Description != "human edited" {
		t.Fatalf("LabelSeed overwrote description: got %q want \"human edited\"", l.Description)
	}
}

func TestLabelAddAppendsLogEntry(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", testActor)
	before, _ := s.LastLogSeq("ATM")
	if err := s.LabelAdd("ATM:new:thing", "desc", "", testActor); err != nil {
		t.Fatal(err)
	}
	after, _ := s.LastLogSeq("ATM")
	if after != before+1 {
		t.Fatalf("LabelAdd seq jumped %d → %d, want +1", before, after)
	}
}

func TestLabelRemoveAppendsTombstone(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", testActor)
	_ = s.LabelAdd("ATM:type:bug", "found bug", "", testActor)
	before, _ := s.LastLogSeq("ATM")
	res, err := s.LabelRemove("ATM:type:bug", testActor)
	if err != nil {
		t.Fatal(err)
	}
	if res == nil {
		t.Fatal("LabelRemoveResult nil")
	}
	after, _ := s.LastLogSeq("ATM")
	if after != before+1 {
		t.Fatalf("LabelRemove seq jumped %d → %d, want +1 (tombstone)", before, after)
	}
	// Replay excludes the removed label.
	st, _ := s.Replay("ATM")
	for _, l := range st.Labels {
		if l.Name == "ATM:type:bug" {
			t.Fatal("removed label appeared in replay live set")
		}
	}
}

func TestRebuildRegeneratesLabelCache(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", testActor)
	_ = s.LabelAdd("ATM:type:bug", "d", "", testActor)
	db, _ := s.cacheDB()
	_, _ = db.Exec(`DELETE FROM labels WHERE name = ?`, "ATM:type:bug")
	if _, err := s.Rebuild(); err != nil {
		t.Fatal(err)
	}
	l, _ := s.LabelShow("ATM:type:bug")
	if l.Description != "d" {
		t.Fatalf("rebuilt label desc = %q want %q", l.Description, "d")
	}
}

func TestLabelUsageCountsOnlyProjectMatchingTasks(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", testActor)
	tk1, _ := s.CreateTask("ATM", "a", "", []string{"ATM:type:bug"}, testActor)
	_, _ = s.CreateTask("ATM", "b", "", nil, testActor)
	_ = tk1
	n, err := s.LabelUsage("ATM", "ATM:type:bug")
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("LabelUsage = %d, want 1", n)
	}
}

func TestLabelExprSurvivesReplayAndRebuild(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", testActor)
	if err := s.LabelAdd("ATM:next-sprint", "the sprint board", "status:open AND sprint:next", testActor); err != nil {
		t.Fatalf("LabelAdd: %v", err)
	}
	if _, err := s.Rebuild(); err != nil {
		t.Fatalf("Rebuild: %v", err)
	}
	got, err := s.LabelShow("ATM:next-sprint")
	if err != nil {
		t.Fatalf("LabelShow: %v", err)
	}
	if got.Expr != "status:open AND sprint:next" {
		t.Fatalf("Expr = %q, want it to survive rebuild", got.Expr)
	}
	if !got.IsComputed() {
		t.Error("label with an Expr must report IsComputed")
	}
}

func TestLabelAddRejectsInvalidExpr(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", testActor)
	if err := s.LabelAdd("ATM:broken", "d", "status:open AND", testActor); err == nil {
		t.Fatal("malformed expression must be rejected at write time")
	}
}

// I2, write half. (The read half - a cycle arriving from a merge - is
// guarded in resolve.go and tested by TestResolverRejectsMergeInducedCycle.)
func TestLabelAddRejectsCycle(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", testActor)
	if err := s.LabelAdd("ATM:a", "d", "status:open", testActor); err != nil {
		t.Fatalf("seed a: %v", err)
	}
	if err := s.LabelAdd("ATM:b", "d", "a", testActor); err != nil {
		t.Fatalf("seed b: %v", err)
	}
	// Now point a at b -> a -> b -> a.
	err := s.LabelAdd("ATM:a", "d", "b", testActor)
	if !errors.Is(err, ErrCyclicExpr) {
		t.Fatalf("err = %v, want ErrCyclicExpr", err)
	}
}

func TestLabelAddRejectsSelfReference(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", testActor)
	err := s.LabelAdd("ATM:loop", "d", "loop", testActor)
	if !errors.Is(err, ErrCyclicExpr) {
		t.Fatalf("err = %v, want ErrCyclicExpr", err)
	}
}

func TestSeedAddsNamespaceDescriptors(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", testActor)
	l, err := s.LabelShow("ATM:status:*")
	if err != nil {
		t.Fatalf("namespace descriptor must be seeded: %v", err)
	}
	if l.Description == "" {
		t.Error("seeded namespace descriptor must carry a description")
	}
	if !l.IsComputed() {
		t.Error("a namespace label is computed")
	}
}

// I4 — a namespace with no descriptor still works; the descriptor is
// optional metadata, never a gate.
func TestUnseededNamespaceStillUsable(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", testActor)
	_, err := s.CreateTask("ATM", "t", "", []string{"ATM:sprint:next"}, testActor)
	if err != nil {
		t.Fatalf("using an undescribed namespace must work: %v", err)
	}
	got := s.ListTasks(QueryFilters{Project: "ATM", Expr: "sprint:*"})
	if len(got) != 1 {
		t.Fatalf("got %d tasks, want 1", len(got))
	}
}

// I3: ATM:status and ATM:status:* are distinct strings but both display as
// "status" in the Boards pane. CreateTask auto-registers ATM:status:open
// (a stored value) but not the namespace descriptor ATM:status:*; the
// collision check keys off a live ATM:status:* label, so seed one first.
func TestLabelAddRejectsBoardNameCollidingWithNamespace(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", testActor)
	if err := s.LabelAdd("ATM:status:*", "namespace", "", testActor); err != nil {
		t.Fatalf("seed namespace: %v", err)
	}
	_, _ = s.CreateTask("ATM", "t", "", []string{"ATM:status:open"}, testActor)
	err := s.LabelAdd("ATM:status", "d", "priority:high", testActor)
	if !errors.Is(err, ErrBoardNameCollision) {
		t.Fatalf("err = %v, want ErrBoardNameCollision", err)
	}
}
