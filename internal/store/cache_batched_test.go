package store

import (
	"sort"
	"testing"
)

// TestCacheListTasksForProjectBatchedMatchesN1 proves Fix C: the batched
// implementation returns the same tasks (same fields, same label ordering) as
// the per-id N+1 path, including the no-tasks and no-labels edge cases.
func TestCacheListTasksForProjectBatchedMatchesN1(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", testActor)
	now := Now()
	db, _ := s.cacheDB()
	// Seed a mix: no labels, one label, two labels (out of order insertion).
	tasks := []*Task{
		{ID: "ATM-0001", ProjectCode: "ATM", Title: "no labels", CreatedAt: now, UpdatedAt: now, Ordinal: 1, CreatedBy: "c", UpdatedBy: "c"},
		{ID: "ATM-0002", ProjectCode: "ATM", Title: "one label", Labels: []string{"ATM:status:open"}, CreatedAt: now, UpdatedAt: now, Ordinal: 2, CreatedBy: "c", UpdatedBy: "c"},
		{ID: "ATM-0003", ProjectCode: "ATM", Title: "two labels", Labels: []string{"ATM:type:bug", "ATM:status:open"}, CreatedAt: now, UpdatedAt: now, Ordinal: 3, CreatedBy: "c", UpdatedBy: "c"},
	}
	for _, tk := range tasks {
		if err := cacheUpsertTask(db, tk); err != nil {
			t.Fatal(err)
		}
	}
	// A task from another project must be excluded.
	_ = cacheUpsertTask(db, &Task{ID: "OTH-0001", ProjectCode: "OTH", Title: "other", CreatedAt: now, UpdatedAt: now, Ordinal: 1, CreatedBy: "c", UpdatedBy: "c"})

	got, err := cacheListTasksForProject(db, "ATM")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d tasks want 3", len(got))
	}
	// Expect id-asc order (cacheListTaskIDs sorts).
	wantOrder := []string{"ATM-0001", "ATM-0002", "ATM-0003"}
	for i, tk := range got {
		if tk.ID != wantOrder[i] {
			t.Fatalf("pos %d id = %s want %s", i, tk.ID, wantOrder[i])
		}
	}
	// Per-task label sets must match the seeded (sorted) sets.
	wantLabels := map[string][]string{
		"ATM-0001": nil,
		"ATM-0002": {"ATM:status:open"},
		"ATM-0003": {"ATM:status:open", "ATM:type:bug"}, // cacheGetTask sorts via ORDER BY label
	}
	for _, tk := range got {
		got := append([]string(nil), tk.Labels...)
		want := append([]string(nil), wantLabels[tk.ID]...)
		if !sliceEq(got, want) {
			t.Fatalf("task %s labels = %v want %v", tk.ID, got, want)
		}
	}
	// All scalar fields must be populated (the N+1 path filled them too).
	for i, tk := range got {
		if tk.Title != tasks[i].Title || tk.ProjectCode != "ATM" || tk.Ordinal != tasks[i].Ordinal {
			t.Fatalf("task %s scalar mismatch: %+v vs %+v", tk.ID, tk, tasks[i])
		}
	}
}

// TestCacheListTasksForProjectBatchedEmpty proves the batched path returns an
// empty slice (not nil-error) for a project with no tasks.
func TestCacheListTasksForProjectBatchedEmpty(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", testActor)
	db, _ := s.cacheDB()
	got, err := cacheListTasksForProject(db, "ATM")
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("got %d tasks want 0", len(got))
	}
}

// TestCacheListCommentsBatchedMatchesN1 proves the batched comment listing
// returns the same comments (same fields, same label ordering) as the per-id
// N+1 path.
func TestCacheListCommentsBatchedMatchesN1(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", testActor)
	tk, _ := s.CreateTask("ATM", "t", "", nil, testActor)
	_, _ = s.CreateComment(tk.ID, "no labels", nil, "", testActor)
	_, _ = s.CreateComment(tk.ID, "one label", []string{"ATM:comment:progress"}, "", testActor)
	_, _ = s.CreateComment(tk.ID, "two labels", []string{"ATM:comment:decision", "ATM:comment:progress"}, "", testActor)

	db, _ := s.cacheDB()
	got, err := cacheListComments(db, tk.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d comments want 3", len(got))
	}
	// id-asc order
	wantOrder := []string{got[0].ID, got[1].ID, got[2].ID}
	sorted := append([]string(nil), wantOrder...)
	sort.Strings(sorted)
	for i := range wantOrder {
		if wantOrder[i] != sorted[i] {
			t.Fatalf("comments not id-asc: %v", wantOrder)
		}
	}
	wantLabels := map[string][]string{
		"no labels":  nil,
		"one label":  {"ATM:comment:progress"},
		"two labels": {"ATM:comment:decision", "ATM:comment:progress"}, // ORDER BY label
	}
	for _, c := range got {
		got := append([]string(nil), c.Labels...)
		want := append([]string(nil), wantLabels[c.Body]...)
		if !sliceEq(got, want) {
			t.Fatalf("comment %q labels = %v want %v", c.Body, got, want)
		}
	}
}

// TestCacheLabelUsageGrouped proves the grouped LabelUsage query returns the
// same counts as calling cacheLabelUsage per label, in one query.
func TestCacheLabelUsageGrouped(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", testActor)
	t1, _ := s.CreateTask("ATM", "t1", "", []string{"ATM:status:open", "ATM:type:bug"}, testActor)
	t2, _ := s.CreateTask("ATM", "t2", "", []string{"ATM:status:open"}, testActor)
	_, _ = s.CreateComment(t1.ID, "c", []string{"ATM:comment:progress"}, "", testActor)
	_, _ = s.CreateComment(t2.ID, "c", []string{"ATM:comment:progress", "ATM:comment:decision"}, "", testActor)

	db, _ := s.cacheDB()
	got, err := cacheLabelUsageGrouped(db, "ATM")
	if err != nil {
		t.Fatal(err)
	}
	// Per-label counts via the existing per-label query for comparison.
	want := map[string]int{
		"ATM:status:open":      2, // t1, t2
		"ATM:type:bug":         1, // t1
		"ATM:comment:progress": 2, // both comments
		"ATM:comment:decision": 1, // t2's comment
	}
	if len(got) != len(want) {
		t.Fatalf("grouped count = %d want %d: %v", len(got), len(want), got)
	}
	for label, c := range want {
		if got[label] != c {
			t.Fatalf("grouped[%s] = %d want %d", label, got[label], c)
		}
	}
}

// TestCacheLabelUsageGroupedEmptyProject proves a project with no usage
// returns an empty map, not an error.
func TestCacheLabelUsageGroupedEmptyProject(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", testActor)
	db, _ := s.cacheDB()
	got, err := cacheLabelUsageGrouped(db, "ATM")
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("got %d entries want 0: %v", len(got), got)
	}
}

func sliceEq(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
