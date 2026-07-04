package store

import "testing"

func TestLabelAddValidatesRegexAndProjectPrefix(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", "claude")
	for _, bad := range []string{"type:bug", "xyz:type:bug", "ATM:", "ATM:type:", "ATM:Type:Bug"} {
		if err := s.LabelAdd(bad, "", "claude"); err == nil {
			t.Fatalf("expected error for label %q", bad)
		}
	}
}

func TestLabelAddRejectsUnknownProjectPrefix(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", "claude")
	if err := s.LabelAdd("XYZ:type:bug", "", "claude"); err == nil {
		t.Fatal("expected error for unknown project prefix XYZ")
	}
}

func TestLabelAddUpsertPreservesDescription(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", "claude")
	_ = s.LabelAdd("ATM:type:bug", "first", "claude")
	_ = s.LabelAdd("ATM:type:bug", "", "claude") // empty desc preserves
	l, _ := s.LabelShow("ATM:type:bug")
	if l.Description != "first" {
		t.Fatalf("description = %q want first", l.Description)
	}
	_ = s.LabelAdd("ATM:type:bug", "second", "claude") // non-empty updates
	l, _ = s.LabelShow("ATM:type:bug")
	if l.Description != "second" {
		t.Fatalf("description = %q want second", l.Description)
	}
}

func TestLabelRemoveSoftRetainsUsage(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", "claude")
	_, _ = s.CreateTask("ATM", "t", "", []string{"ATM:type:bug"}, "claude")
	r, err := s.LabelRemove("ATM:type:bug", "claude")
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
	_, _ = s.CreateProject("ATM", "x", "claude")
	_, _ = s.CreateProject("SCY", "y", "claude")
	_ = s.LabelAdd("ATM:custom:a", "", "claude")
	_ = s.LabelAdd("ATM:custom:b", "", "claude")
	_ = s.LabelAdd("SCY:custom:a", "", "claude")
	// ATM has 18 seeded + 2 custom = 20.
	if got := len(s.LabelList("ATM", "")); got != 20 {
		t.Fatalf("ATM labels = %d want 20", got)
	}
	// Filter to the custom namespace.
	if got := len(s.LabelList("ATM", "custom")); got != 2 {
		t.Fatalf("ATM:custom labels = %d want 2", got)
	}
}

func TestNamespacesDistinctSorted(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", "claude")
	_ = s.LabelAdd("ATM:hot", "", "claude") // unnamespaced tag
	_ = s.LabelAdd("ATM:custom:x", "", "claude")
	got := s.Namespaces("ATM")
	want := []string{"context", "custom", "priority", "status", "type"}
	if len(got) != 5 || got[0] != "context" || got[4] != "type" {
		t.Fatalf("Namespaces = %v want %v", got, want)
	}
}

func TestLabelSeedSetsDescriptionOnCreate(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", "claude")
	if err := s.LabelSeed("ATM:custom:x", "seed desc", "claude"); err != nil {
		t.Fatal(err)
	}
	l, _ := s.LabelShow("ATM:custom:x")
	if l.Description != "seed desc" {
		t.Fatalf("description = %q want \"seed desc\"", l.Description)
	}
}

func TestLabelSeedPreservesExistingDescription(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", "claude")
	_ = s.LabelAdd("ATM:type:bug", "human edited", "claude")
	if err := s.LabelSeed("ATM:type:bug", "seed default", "claude"); err != nil {
		t.Fatal(err)
	}
	l, _ := s.LabelShow("ATM:type:bug")
	if l.Description != "human edited" {
		t.Fatalf("LabelSeed overwrote description: got %q want \"human edited\"", l.Description)
	}
}
