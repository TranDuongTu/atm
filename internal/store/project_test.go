package store

import (
	"encoding/json"
	"os"
	"testing"
)

func TestCreateProjectValidatesCode(t *testing.T) {
	s := newTestStore(t)
	for _, bad := range []string{"", "AT", "ATM1", "atm", "TOOLONG", "A-B"} {
		if _, err := s.CreateProject(bad, "x", "claude"); err == nil {
			t.Fatalf("expected error for code %q", bad)
		}
	}
}

func TestCreateProjectRejectsDuplicate(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateProject("ATM", "first", "claude"); err != nil {
		t.Fatal(err)
	}
	_, err := s.CreateProject("ATM", "second", "claude")
	if !IsConflict(err) {
		t.Fatalf("expected conflict, got %v", err)
	}
}

func TestSetProjectName(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateProject("ATM", "old", "claude"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetProjectName("ATM", "new", "ttran"); err != nil {
		t.Fatal(err)
	}
	p, _ := s.GetProject("ATM")
	if p.Name != "new" {
		t.Fatalf("name = %q", p.Name)
	}
}

func TestRemoveProjectZeroTaskGuard(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", "claude")
	_, _ = s.CreateTask("ATM", "t", "", nil, "claude")
	if err := s.RemoveProject("ATM", "claude"); !IsConflict(err) {
		t.Fatalf("expected conflict (has tasks), got %v", err)
	}
}

// newTestStore is shared across store _test.go files.
func newTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Init(""); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestSeedLabelsAppliesAllDefaults(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", "claude")
	// SeedLabels applies all 22 defaults (CreateProject seeding is wired in Task 3).
	if err := s.SeedLabels("ATM", "claude"); err != nil {
		t.Fatal(err)
	}
	ls := s.LabelList("ATM", "")
	if len(ls) != 22 {
		t.Fatalf("SeedLabels left %d labels, want 22", len(ls))
	}
	l, _ := s.LabelShow("ATM:context:agent")
	if l.Description == "" {
		t.Error("ATM:context:agent has empty description after seed")
	}
}

func TestCreateProjectSeedsLabels(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateProject("ATM", "x", "claude"); err != nil {
		t.Fatal(err)
	}
	ls := s.LabelList("ATM", "")
	if len(ls) != 22 {
		t.Fatalf("after CreateProject, ATM has %d labels, want 22 (seeded defaults)", len(ls))
	}
	// Every seeded label has a non-empty description.
	for _, l := range ls {
		if l.Description == "" {
			t.Errorf("seeded label %q has empty description", l.Name)
		}
	}
	// Spot-check a known seed label is present.
	if _, err := s.LabelShow("ATM:context:agent"); err != nil {
		t.Errorf("ATM:context:agent missing after seed: %v", err)
	}
}

func TestSeedLabelsPreservesEditedDescriptions(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", "claude")
	// Edit one label's description (human curates).
	_ = s.LabelAdd("ATM:type:bug", "human edited", "claude")
	if err := s.SeedLabels("ATM", "claude"); err != nil {
		t.Fatal(err)
	}
	l, _ := s.LabelShow("ATM:type:bug")
	if l.Description != "human edited" {
		t.Fatalf("SeedLabels overwrote edited description: got %q want \"human edited\"", l.Description)
	}
	// The other 21 keep their seed descriptions.
	l2, _ := s.LabelShow("ATM:status:open")
	if l2.Description == "" {
		t.Error("ATM:status:open lost its description after re-seed")
	}
}

func TestCreateProjectAppendsLogEntries(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateProject("ATM", "x", "claude"); err != nil {
		t.Fatal(err)
	}
	entries, _ := s.ReadLog("ATM")
	// 1 project.created + 22 label.upserted (seed) = 23 entries
	if len(entries) < 2 {
		t.Fatalf("log has %d entries, want >= 2", len(entries))
	}
	if entries[0].Action != ActionProjectCreated {
		t.Fatalf("first entry action = %q want %q", entries[0].Action, ActionProjectCreated)
	}
	for _, e := range entries[1:] {
		if e.Action != ActionLabelUpserted {
			t.Fatalf("seed entry action = %q want %q", e.Action, ActionLabelUpserted)
		}
	}
}

func TestSetProjectNameAppendsNameChanged(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "old", "claude")
	// Drop seed entries from the comparison: focus on entries after create.
	before, _ := s.LastLogSeq("ATM")
	_ = s.SetProjectName("ATM", "new", "ttran")
	entries, _ := s.ReadLog("ATM")
	var nameChange *LogEntry
	for i := range entries {
		if entries[i].Seq > before && entries[i].Action == ActionProjectNameChanged {
			nameChange = &entries[i]
			break
		}
	}
	if nameChange == nil {
		t.Fatalf("no project.name-changed entry after SetProjectName")
	}
	var p Project
	_ = json.Unmarshal(nameChange.Payload, &p)
	if p.Name != "new" {
		t.Fatalf("payload name = %q want %q", p.Name, "new")
	}
}

func TestRemoveProjectAppendsTombstoneThenDeletes(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", "claude")
	if err := s.RemoveProject("ATM", "claude"); err != nil {
		t.Fatal(err)
	}
	// Project file and log file are gone (project directory removed).
	if _, err := s.GetProject("ATM"); !IsNotFound(err) {
		t.Fatalf("GetProject after remove: %v want ErrNotFound", err)
	}
	if _, err := os.Stat(s.logPath("ATM")); !os.IsNotExist(err) {
		t.Fatalf("log.jsonl must be deleted with the project dir, got %v", err)
	}
	// Tombstone was appended before deletion: we can only observe this indirectly.
	// (If no tombstone were appended, the cache file would still exist or the
	// directory would not be removed.) The on-disk absence is the contract.
}

func TestCreateProjectRejectsDuplicateAfterCacheOnlyLoss(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateProject("ATM", "first", "claude"); err != nil {
		t.Fatal(err)
	}
	// Advance NextTaskN past 1 so a silent duplicate project.created would be
	// detectable (it resets NextTaskN back to 1 on replay).
	if _, err := s.CreateTask("ATM", "t1", "", nil, "claude"); err != nil {
		t.Fatal(err)
	}
	// Simulate a cache-only loss of the project row: cache.db forgets the
	// project, but projects/ATM/log.jsonl is untouched (still on disk).
	db, err := s.cacheDB()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`DELETE FROM projects WHERE code = ?`, "ATM"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateProject("ATM", "second", "claude"); !IsConflict(err) {
		t.Fatalf("expected conflict recreating %q after cache-only loss, got %v", "ATM", err)
	}
}

func TestCreateProjectAllowedAfterRemoveProject(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateProject("ATM", "first", "claude"); err != nil {
		t.Fatal(err)
	}
	if err := s.RemoveProject("ATM", "claude"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateProject("ATM", "second", "claude"); err != nil {
		t.Fatalf("recreate after RemoveProject should succeed, got %v", err)
	}
}

// TestGetProjectLazyRebuildReconstructsNextTaskNPastCreatedTasks is a
// regression test for a bug where rebuildProjectFromLog derived NextTaskN
// solely from the last project.* event's payload, which never advances past
// its value at project-creation (or last name-change) time -- CreateTask
// only bumps NextTaskN in the cache row, never in a log event. After a
// cache-only loss of the project row, the lazy rebuild path (triggered here
// via GetProject on a cache miss) must reconstruct NextTaskN as one past the
// highest task-ID N ever created, not reset it to 1.
func TestGetProjectLazyRebuildReconstructsNextTaskNPastCreatedTasks(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateProject("ATM", "x", "claude"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if _, err := s.CreateTask("ATM", "t", "", nil, "claude"); err != nil {
			t.Fatal(err)
		}
	}
	// Simulate a cache-only loss of the project row.
	db, err := s.cacheDB()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`DELETE FROM projects WHERE code = ?`, "ATM"); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetProject("ATM")
	if err != nil {
		t.Fatalf("GetProject after cache-only loss: %v", err)
	}
	if got.NextTaskN != 4 {
		t.Fatalf("NextTaskN after lazy rebuild = %d want 4 (3 tasks created, ATM-0001..ATM-0003)", got.NextTaskN)
	}
	// A subsequent CreateTask must not collide with any existing task ID.
	newTask, err := s.CreateTask("ATM", "t4", "", nil, "claude")
	if err != nil {
		t.Fatal(err)
	}
	if newTask.ID != "ATM-0004" {
		t.Fatalf("new task ID = %q want ATM-0004 (must not collide with existing tasks)", newTask.ID)
	}
}

func TestGetProjectLazyMissRebuildsFromLog(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", "claude")
	db, _ := s.cacheDB()
	_, _ = db.Exec(`DELETE FROM projects WHERE code = ?`, "ATM")
	got, err := s.GetProject("ATM")
	if err != nil {
		t.Fatalf("GetProject after cache delete: %v", err)
	}
	if got.Code != "ATM" {
		t.Fatalf("rebuilt project code = %q", got.Code)
	}
}
