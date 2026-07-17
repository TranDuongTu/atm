package store

import (
	"atm/internal/core"
	"atm/internal/store/eventlog"
	"bytes"
	"os"
	"testing"
)

func TestCreateProjectValidatesCode(t *testing.T) {
	s := newTestStore(t)
	for _, bad := range []string{"", "AT", "ATM1", "atm", "TOOLONG", "A-B"} {
		if _, err := s.CreateProject(bad, "x", testActor); err == nil {
			t.Fatalf("expected error for code %q", bad)
		}
	}
}

func TestCreateProjectRejectsDuplicate(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateProject("ATM", "first", testActor); err != nil {
		t.Fatal(err)
	}
	_, err := s.CreateProject("ATM", "second", testActor)
	if !core.IsConflict(err) {
		t.Fatalf("expected conflict, got %v", err)
	}
}

func TestSetProjectName(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateProject("ATM", "old", testActor); err != nil {
		t.Fatal(err)
	}
	if err := s.SetProjectName("ATM", "new", testActor); err != nil {
		t.Fatal(err)
	}
	p, _ := s.GetProject("ATM")
	if p.Name != "new" {
		t.Fatalf("name = %q", p.Name)
	}
}

func TestRemoveProjectZeroTaskGuard(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", testActor)
	_, _ = s.CreateTask("ATM", "t", "", nil, testActor)
	if err := s.RemoveProject("ATM", testActor); !core.IsConflict(err) {
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

// testActor is the conforming actor (persona@agent:model with a registered
// persona) used by store tests. Built-ins are seeded lazily by validateActor
// on the first mutation, so admin is available without an explicit seed step.
const testActor = "admin@cli:test"

func TestSeedLabelsAppliesAllDefaults(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", testActor)
	// SeedLabels applies all 16 defaults (CreateProject seeding is wired in Task 3).
	if err := s.SeedLabels("ATM", testActor); err != nil {
		t.Fatal(err)
	}
	ls := s.LabelList("ATM", "")
	if len(ls) != 16 {
		t.Fatalf("SeedLabels left %d labels, want 16", len(ls))
	}
	l, _ := s.LabelShow("ATM:context:agent")
	if l.Description == "" {
		t.Error("ATM:context:agent has empty description after seed")
	}
}

func TestCreateProjectSeedsLabels(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateProject("ATM", "x", testActor); err != nil {
		t.Fatal(err)
	}
	ls := s.LabelList("ATM", "")
	if len(ls) != 16 {
		t.Fatalf("after CreateProject, ATM has %d labels, want 16 (seeded defaults)", len(ls))
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
	_, _ = s.CreateProject("ATM", "x", testActor)
	// Edit one label's description (human curates).
	_ = s.LabelAdd("ATM:type:bug", "human edited", "", testActor)
	if err := s.SeedLabels("ATM", testActor); err != nil {
		t.Fatal(err)
	}
	l, _ := s.LabelShow("ATM:type:bug")
	if l.Description != "human edited" {
		t.Fatalf("SeedLabels overwrote edited description: got %q want \"human edited\"", l.Description)
	}
	// The other 15 keep their seed descriptions.
	l2, _ := s.LabelShow("ATM:status:open")
	if l2.Description == "" {
		t.Error("ATM:status:open lost its description after re-seed")
	}
}
func TestRemoveProjectAppendsTombstoneThenDeletes(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", testActor)
	if err := s.RemoveProject("ATM", testActor); err != nil {
		t.Fatal(err)
	}
	// Project file and log file are gone (project directory removed).
	if _, err := s.GetProject("ATM"); !core.IsNotFound(err) {
		t.Fatalf("GetProject after remove: %v want core.ErrNotFound", err)
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
	if _, err := s.CreateProject("ATM", "first", testActor); err != nil {
		t.Fatal(err)
	}
	// Advance NextTaskN past 1 so a silent duplicate project.created would be
	// detectable (it resets NextTaskN back to 1 on replay).
	if _, err := s.CreateTask("ATM", "t1", "", nil, testActor); err != nil {
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
	if _, err := s.CreateProject("ATM", "second", testActor); !core.IsConflict(err) {
		t.Fatalf("expected conflict recreating %q after cache-only loss, got %v", "ATM", err)
	}
}

func TestCreateProjectAllowedAfterRemoveProject(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateProject("ATM", "first", testActor); err != nil {
		t.Fatal(err)
	}
	if err := s.RemoveProject("ATM", testActor); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateProject("ATM", "second", testActor); err != nil {
		t.Fatalf("recreate after RemoveProject should succeed, got %v", err)
	}
}
// TestCreateProjectDuplicateDoesNotCorruptExplicitV1Entry pins the birth
// preflight ordering: a duplicate CreateProject against a legacy v1 project
// (an explicit "v1" ProjectFormats entry plus a log.jsonl on disk — these
// exist in the wild; the removed `store rollback` command wrote such entries)
// must fail with ErrConflict WITHOUT touching store.json. Flipping the entry
// to "v2" would make the v1 log read as an empty v2 project (data hidden) and
// fork the next write into a fresh events.v2.jsonl.
func TestCreateProjectDuplicateDoesNotCorruptExplicitV1Entry(t *testing.T) {
	s := newTestStore(t)
	// Warm persona seeding (first-mutation lazy seed) so the failing
	// CreateProject below performs no incidental store.json write of its own.
	if _, err := s.CreateProject("OTH", "warm", testActor); err != nil {
		t.Fatal(err)
	}
	const code = "LEG"
	if err := s.eng.SetProjectFormat(code, eventlog.StoreFormatV1); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(s.projectDir(code), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.logPath(code), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Capture store.json exactly as the fixture left it.
	before, err := os.ReadFile(s.storeMetaPath())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateProject(code, "dup", testActor); !core.IsConflict(err) {
		t.Fatalf("CreateProject on existing v1 media: got %v, want ErrConflict", err)
	}
	// The entry must still be "v1" (present, not flipped, not deleted)...
	m, err := s.eng.ReadStoreMeta()
	if err != nil {
		t.Fatal(err)
	}
	if f, ok := m.ProjectFormats[code]; !ok || f != eventlog.StoreFormatV1 {
		t.Fatalf("ProjectFormats[%s] = (%q, present=%v) after failed duplicate create, want (v1, true)", code, f, ok)
	}
	// ...and store.json must be byte-identical (no UpdatedAt churn either).
	after, err := os.ReadFile(s.storeMetaPath())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatalf("store.json changed on failed duplicate create:\nbefore: %s\nafter:  %s", before, after)
	}
}

// TestCreateProjectDuplicateV2MediaWithoutEntryWritesNoEntry guards the plain
// case: v2 media on disk but no explicit ProjectFormats entry. The duplicate
// create must fail on the media check and leave no stray entry behind.
func TestCreateProjectDuplicateV2MediaWithoutEntryWritesNoEntry(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateProject("OTH", "warm", testActor); err != nil {
		t.Fatal(err)
	}
	const code = "ORP"
	if err := os.MkdirAll(s.projectDir(code), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.eng.EventsV2Path(code), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateProject(code, "dup", testActor); !core.IsConflict(err) {
		t.Fatalf("CreateProject on existing v2 media: got %v, want ErrConflict", err)
	}
	m, err := s.eng.ReadStoreMeta()
	if err != nil {
		t.Fatal(err)
	}
	if f, ok := m.ProjectFormats[code]; ok {
		t.Fatalf("failed duplicate create left ProjectFormats[%s] = %q, want no entry", code, f)
	}
}

func TestGetProjectLazyMissRebuildsFromLog(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", testActor)
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
