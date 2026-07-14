package store

import "testing"

func TestV2ActiveReadRebuildsMissingCache(t *testing.T) {
	s := testStore(t)
	_, _ = s.CreateProject("ATM", "x", "admin@cli:unset")
	tk, _ := s.CreateTask("ATM", "before", "", nil, "admin@cli:unset")
	_, _ = s.UpgradeProjectToV2("ATM")
	db, _ := s.cacheDB()
	_, _ = db.Exec(`DELETE FROM tasks`)
	got, err := s.GetTask(tk.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "before" {
		t.Fatalf("title = %q", got.Title)
	}
}

// TestV2ActiveReadRebuildsFromV2NotV1 is the sharper form of the test above: on
// an UPGRADED project the frozen v1 log can still answer a cache miss, so a
// rebuild that silently fell back to rebuildTaskFromLog would look correct.
// Mutating the task through v2 FIRST makes the two sources disagree, so only a
// rebuild from the v2 fold produces the right answer.
func TestV2ActiveReadRebuildsFromV2NotV1(t *testing.T) {
	s := testStore(t)
	if _, err := s.CreateProject("ATM", "x", "admin@cli:unset"); err != nil {
		t.Fatal(err)
	}
	tk, err := s.CreateTask("ATM", "before", "", nil, "admin@cli:unset")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpgradeProjectToV2("ATM"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetTitle(tk.ID, "after", "admin@cli:unset"); err != nil {
		t.Fatal(err)
	}
	db, _ := s.cacheDB()
	if _, err := db.Exec(`DELETE FROM tasks`); err != nil {
		t.Fatal(err)
	}
	if err := cacheClearV2Freshness(db, "ATM"); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetTask(tk.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "after" {
		t.Fatalf("title = %q, want %q (the read rebuilt from the frozen v1 log, not the v2 fold)", got.Title, "after")
	}
}

// TestV2ActiveMissingEntityReadsReturnErrNotFound pins the sentinel contract:
// a v2 read of an entity that does not exist must be ErrNotFound, exactly as v1
// is — the CLI's exit codes key on IsNotFound.
func TestV2ActiveMissingEntityReadsReturnErrNotFound(t *testing.T) {
	s := testStore(t)
	if err := s.SetActiveFormat(StoreFormatV2); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateProject("ATM", "x", "admin@cli:unset"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetTask("ATM-999999"); !IsNotFound(err) {
		t.Fatalf("GetTask on a missing v2 task = %v, want ErrNotFound", err)
	}
	if _, err := s.GetComment("ATM-999999-c1"); !IsNotFound(err) {
		t.Fatalf("GetComment on a missing v2 comment = %v, want ErrNotFound", err)
	}
	if _, err := s.GetProject("XXX"); !IsNotFound(err) {
		t.Fatalf("GetProject on a missing project = %v, want ErrNotFound", err)
	}
}

func TestListTasksSeesV2AppendWithoutCacheProjection(t *testing.T) {
	s := testStore(t)
	_, _ = s.CreateProject("ATM", "x", "admin@cli:unset")
	_, _ = s.UpgradeProjectToV2("ATM")
	// Simulate a writer that died between the append commit point and the
	// cache projection: the event line is truth, the cache is legitimately
	// stale, and ONLY the freshness gate can save the list read.
	var alias string
	if err := s.WithLock("ATM", func() error {
		_, a, err := s.appendV2TaskCreatedLocked("ATM", "external", "", nil, "admin@cli:unset")
		alias = a
		return err
	}); err != nil {
		t.Fatal(err)
	}
	tasks := s.ListTasks(QueryFilters{Project: "ATM"})
	found := false
	for _, tk := range tasks {
		if tk.ID == alias {
			found = true
		}
	}
	if !found {
		t.Fatalf("ListTasks = %d tasks without %q: project-scoped list read is not freshness-gated", len(tasks), alias)
	}
}
