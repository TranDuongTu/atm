package store

import (
	"os"
	"strings"
	"testing"

	"atm/internal/eventsource"
)

func TestUpgradeProjectToV2PreservesV1LogAndActivatesV2(t *testing.T) {
	s := testStore(t)
	if _, err := s.CreateProject("ATM", "Agent Tasks Management", "admin@cli:unset"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateTask("ATM", "First task", "desc", []string{"ATM:status:open"}, "admin@cli:unset"); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(s.logPath("ATM"))
	if err != nil {
		t.Fatal(err)
	}
	rep, err := s.UpgradeProjectToV2("ATM")
	if err != nil {
		t.Fatal(err)
	}
	if rep.Project != "ATM" || rep.Events == 0 || rep.Format != StoreFormatV2 {
		t.Fatalf("bad report: %#v", rep)
	}
	after, err := os.ReadFile(s.logPath("ATM"))
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("v1 log changed during upgrade")
	}
	if _, err := os.Stat(s.eventsV2Path("ATM")); err != nil {
		t.Fatalf("events.v2.jsonl missing: %v", err)
	}
	f, err := s.projectFormat("ATM")
	if err != nil {
		t.Fatal(err)
	}
	if f != StoreFormatV2 {
		t.Fatalf("format = %q, want v2", f)
	}
	if _, err := s.GetTask("ATM-0001"); err != nil {
		t.Fatalf("cache not rebuilt from v2: %v", err)
	}
}

// TestUpgradeRefusesAPreexistingV2File covers the belt-and-braces disk check
// (step 4 of UpgradeProjectToV2): with no rollback, a project is upgraded at
// most once through the normal (format-gated) path, so the only way to reach
// an orphaned events.v2.jsonl ahead of a v1-active project is by direct disk
// manipulation. That must refuse, not displace.
func TestUpgradeRefusesAPreexistingV2File(t *testing.T) {
	s := testStore(t)
	if _, err := s.CreateProject("ATM", "x", "admin@cli:unset"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.eventsV2Path("ATM"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpgradeProjectToV2("ATM"); !IsConflict(err) {
		t.Fatalf("upgrade with a pre-existing events.v2.jsonl = %v, want ErrConflict", err)
	}
	// The refused upgrade must leave the v1 project readable: the format
	// entry was never flipped.
	if f, _ := s.projectFormat("ATM"); f != StoreFormatV1 {
		t.Fatalf("format after refused upgrade = %q, want v1", f)
	}
}

func TestUpgradeRefusesEffectiveV2Project(t *testing.T) {
	s := testStore(t)
	if _, err := s.CreateProject("ATM", "x", "admin@cli:unset"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpgradeProjectToV2("ATM"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpgradeProjectToV2("ATM"); !IsConflict(err) {
		t.Fatalf("second upgrade of a v2-active project = %v, want ErrConflict (re-upgrade is legal only after rollback)", err)
	}
}

func TestUpgradeAllRetrySkipsV2ActiveAndPreservesPostCutoverWrites(t *testing.T) {
	s := testStore(t)
	if _, err := s.CreateProject("AAA", "first", "admin@cli:unset"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateProject("BBB", "second", "admin@cli:unset"); err != nil {
		t.Fatal(err)
	}
	good, err := os.ReadFile(s.logPath("BBB"))
	if err != nil {
		t.Fatal(err)
	}
	// Corrupt BBB so the first --all pass upgrades AAA (sorted first), then
	// fails on BBB and returns WITHOUT flipping ActiveFormat.
	if err := os.WriteFile(s.logPath("BBB"), append(append([]byte{}, good...), []byte("{malformed\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpgradeAllToV2(); err == nil {
		t.Fatal("expected --all to fail on the corrupted BBB log")
	}
	if f, _ := s.projectFormat("AAA"); f != StoreFormatV2 {
		t.Fatalf("AAA format = %q, want v2 after the partial pass", f)
	}
	// The user keeps working: a post-cutover write lands in AAA's LIVE v2
	// file. The mutator rewire is Task 8, so simulate it with the Task 2
	// primitives — author a causal descendant of the current frontier.
	snapA, err := s.readV2File("AAA", false)
	if err != nil {
		t.Fatal(err)
	}
	clock := eventsource.NewClock(func() int64 { return 2000 })
	ev, err := eventsource.NewEvent(clock, "r_0123456789abcdefghjkmnpqrs", snapA.Frontier, eventsource.Draft{
		Actor:   "admin@cli:unset",
		Action:  "project.name-changed",
		Subject: eventsource.Subject{Kind: "project", Code: "AAA"},
		Payload: map[string]any{"name": "post-cutover"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.WithLock("AAA", func() error { return s.appendV2EventLineLocked("AAA", ev.Raw) }); err != nil {
		t.Fatal(err)
	}
	liveBefore, err := os.ReadFile(s.eventsV2Path("AAA"))
	if err != nil {
		t.Fatal(err)
	}
	// Repair BBB and retry --all: AAA must be SKIPPED, never re-upgraded
	// from its frozen v1 log.
	if err := os.WriteFile(s.logPath("BBB"), good, 0o644); err != nil {
		t.Fatal(err)
	}
	reps, err := s.UpgradeAllToV2()
	if err != nil {
		t.Fatal(err)
	}
	sawSkip := false
	for _, r := range reps {
		if r.Project == "AAA" && r.AlreadyV2 {
			sawSkip = true
		}
	}
	if !sawSkip {
		t.Fatalf("reports = %#v: AAA must be reported as already-v2, not re-upgraded", reps)
	}
	liveAfter, err := os.ReadFile(s.eventsV2Path("AAA"))
	if err != nil {
		t.Fatal(err)
	}
	if string(liveBefore) != string(liveAfter) {
		t.Fatal("retry rewrote AAA's live v2 file — post-cutover writes were destroyed")
	}
	if !strings.Contains(string(liveAfter), "post-cutover") {
		t.Fatal("post-cutover event missing from AAA's live v2 file after retry")
	}
	dirEntries, err := os.ReadDir(s.projectDir("AAA"))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range dirEntries {
		if strings.HasPrefix(e.Name(), "events.v2.reupgrade.") {
			t.Fatalf("retry archived AAA's live v2 file as %s — archives are never auto-merged, so the post-cutover write would silently vanish", e.Name())
		}
	}
	if m, _ := s.readStoreMeta(); m.ActiveFormat != StoreFormatV2 {
		t.Fatalf("ActiveFormat = %q after the full retry, want v2 (skipped projects count as already-upgraded for the flip)", m.ActiveFormat)
	}
}

// Two archives with the same reason inside one UTC second must not collide:
// os.Rename overwrites its destination silently, so a naive timestamped name
// would destroy the earlier archive — the only surviving evidence of the
// events it held.
func TestArchiveV2FileNeverOverwritesAPreviousArchive(t *testing.T) {
	s := testStore(t)
	if _, err := s.CreateProject("ATM", "x", "admin@cli:unset"); err != nil {
		t.Fatal(err)
	}
	contents := []string{"first archive\n", "second archive\n"}
	var paths []string
	for _, body := range contents {
		if err := os.WriteFile(s.eventsV2Path("ATM"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		var dst string
		if err := s.WithLock("ATM", func() error {
			var err error
			dst, err = s.archiveV2FileLocked("ATM", "reupgrade")
			return err
		}); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, dst)
	}
	if paths[0] == paths[1] {
		t.Fatalf("both archives landed on %s — the first was overwritten", paths[0])
	}
	for i, p := range paths {
		got, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("archive %s missing: %v", p, err)
		}
		if string(got) != contents[i] {
			t.Fatalf("archive %s = %q, want %q", p, got, contents[i])
		}
	}
}

// A label that a task already carries can be turned INTO a board (`atm label
// add --expr` on the same name) and later removed. LabelRemove deletes only
// the label record — the name stays on the task — so the v1 replay still
// lists it while the v2 fold drops the membership slot as inert (the label
// state is still there, Tombstoned, and still computed). The compare oracle
// must resolve computed-ness against ALL fold label states, tombstoned
// included, or such a store can never be upgraded again.
func TestUpgradeToleratesMembershipOfARemovedBoardLabel(t *testing.T) {
	s := testStore(t)
	if _, err := s.CreateProject("ATM", "x", "admin@cli:unset"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateTask("ATM", "carries the label", "", []string{"ATM:hot"}, "admin@cli:unset"); err != nil {
		t.Fatal(err)
	}
	// The plain label the task carries becomes a board...
	if err := s.LabelAdd("ATM:hot", "now a board", "status:open", "admin@cli:unset"); err != nil {
		t.Fatal(err)
	}
	// ...and is then removed. The task keeps the name (RetainedUsage).
	res, err := s.LabelRemove("ATM:hot", "admin@cli:unset")
	if err != nil {
		t.Fatal(err)
	}
	if res.RetainedUsage == 0 {
		t.Fatal("precondition: the task must still carry the removed label name")
	}
	if _, err := s.UpgradeProjectToV2("ATM"); err != nil {
		t.Fatalf("upgrade refused a store whose task carries a removed board label: %v", err)
	}
	if f, _ := s.projectFormat("ATM"); f != StoreFormatV2 {
		t.Fatalf("format = %q, want v2", f)
	}
}

func TestUpgradeAllFlipsActiveFormatSoNewProjectsAreBornV2(t *testing.T) {
	s := testStore(t)
	if _, err := s.CreateProject("ATM", "x", "admin@cli:unset"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpgradeAllToV2(); err != nil {
		t.Fatal(err)
	}
	m, err := s.readStoreMeta()
	if err != nil {
		t.Fatal(err)
	}
	if m.ActiveFormat != StoreFormatV2 {
		t.Fatalf("ActiveFormat after upgrade --all = %q, want v2", m.ActiveFormat)
	}
	if f, _ := s.projectFormat("NEW"); f != StoreFormatV2 {
		t.Fatalf("birth format for a project with no entry = %q, want v2", f)
	}
}
