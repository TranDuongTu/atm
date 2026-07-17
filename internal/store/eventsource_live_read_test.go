package store

import (
	"os"
	"testing"
)

func TestV2ActiveReadRebuildsMissingCache(t *testing.T) {
	s := testStore(t)
	_, _ = s.CreateProject("ATM", "x", "admin@cli:unset")
	tk, _ := s.CreateTask("ATM", "before", "", nil, "admin@cli:unset")
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
	// Simulate a writer that died between the append commit point and the
	// cache projection: the event line is truth, the cache is legitimately
	// stale, and ONLY the freshness gate can save the list read.
	alias := authorTaskViaEngine(t, s, "ATM", "external", "admin@cli:unset")
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

// TestListTasksSurfacesV2IntegrityErrorNotEmptyList pins Fix-round-1
// Important-1: a corrupt/partial tail in a v2 project's event file (the
// normal shape of a crash mid-mutation, which writes several events per
// call) must make ListTasksErr fail with ErrIntegrity, never silently
// return an empty list with a nil error. Before the fix, the freshness
// gate's `continue` treated an on-disk integrity failure the same as a
// cache-DB hiccup, so `atm task list` reported "no tasks" with exit 0 while
// `atm task show` on the same project correctly failed.
func TestListTasksSurfacesV2IntegrityErrorNotEmptyList(t *testing.T) {
	s := testStore(t)
	if _, err := s.CreateProject("ATM", "x", testActor); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateTask("ATM", "t1", "", nil, testActor); err != nil {
		t.Fatal(err)
	}
	// Simulate a crashed writer leaving a complete-but-unparseable line: same
	// technique as TestReadV2FileRejectsMalformedCompleteLine. Appending
	// (rather than overwriting) also bumps the newline count, so the
	// freshness probe sees the cache as stale and actually re-verifies.
	f, err := os.OpenFile(s.eventsV2Path("ATM"), os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("{not-json}\n"); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	tasks, err := s.ListTasksErr(QueryFilters{Project: "ATM"})
	if !IsIntegrity(err) {
		t.Fatalf("ListTasksErr err = %v, want ErrIntegrity", err)
	}
	if tasks != nil {
		t.Fatalf("ListTasksErr tasks = %v, want nil alongside the integrity error", tasks)
	}
}

// TestListCommentsSeesV2AppendWithoutCacheProjection is the comment analogue
// of TestListTasksSeesV2AppendWithoutCacheProjection, pinning Fix-round-1
// Important-2: ListComments had no freshness gate at all, so a v2 comment
// append the cache hasn't projected (a crashed writer, or a second process)
// was invisible to it.
func TestListCommentsSeesV2AppendWithoutCacheProjection(t *testing.T) {
	s := testStore(t)
	_, _ = s.CreateProject("ATM", "x", testActor)
	tk, _ := s.CreateTask("ATM", "t1", "", nil, testActor)
	alias := authorCommentViaEngine(t, s, "ATM", tk.ID, "external", testActor)
	comments, err := s.ListComments(tk.ID)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, c := range comments {
		if c.ID == alias {
			found = true
		}
	}
	if !found {
		t.Fatalf("ListComments = %d comments without %q: comment list read is not freshness-gated", len(comments), alias)
	}
}

// TestListTasksErrPropagatesFormatLookupError and
// TestListCommentsPropagatesFormatLookupError pin the final-review Important-3:
// both list reads used to do `f, _ := s.projectFormat(code)`. With store.json
// unreadable, f == "", the v2 branch (and with it the freshness gate) is skipped,
// and the caller gets stale cache rows with a NIL error. Same reasoning as
// TestTextSearchPropagatesFormatLookupError / TestReindexOnceOnV2Propagates-
// FormatLookupError: a swallowed lookup silently degrades a v2 project to the
// ungated v1 path.
func TestListTasksErrPropagatesFormatLookupError(t *testing.T) {
	s := testStore(t)
	if _, err := s.CreateProject("ATM", "x", "admin@cli:unset"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateTask("ATM", "t", "", nil, "admin@cli:unset"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.storeMetaPath(), []byte("{ not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := s.ListTasksErr(QueryFilters{Project: "ATM"})
	if err == nil {
		t.Fatalf("ListTasksErr = %d tasks, nil: a failed format lookup silently served ungated cache rows", len(got))
	}
}

func TestListCommentsPropagatesFormatLookupError(t *testing.T) {
	s := testStore(t)
	if _, err := s.CreateProject("ATM", "x", "admin@cli:unset"); err != nil {
		t.Fatal(err)
	}
	tk, err := s.CreateTask("ATM", "t", "", nil, "admin@cli:unset")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateComment(tk.ID, "body", nil, "", "admin@cli:unset"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.storeMetaPath(), []byte("{ not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := s.ListComments(tk.ID)
	if err == nil {
		t.Fatalf("ListComments = %d comments, nil: a failed format lookup silently served ungated cache rows", len(got))
	}
}
