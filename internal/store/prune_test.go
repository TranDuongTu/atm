package store

import (
	"os"
	"path/filepath"
	"testing"
)

// newV1ActiveProject plants a raw v1 log with no ProjectFormats entry, so the
// project reads as v1-active through the fresh-store default (StoreFormatV1)
// and is NOT upgraded. prune-v1 must refuse to touch it: it is still
// legitimately v1, not a leftover from an upgrade.
func newV1ActiveProject(t *testing.T) (*Store, string) {
	t.Helper()
	s := testStore(t)
	plantV1Project(t, s, "ATM", v1RawLogFixture(t))
	return s, s.StorePath()
}

// newUpgradedProject plants a raw v1 log and upgrades it to v2. The upgrade
// cuts the project over to v2 media but PRESERVES log.jsonl on disk (that
// preserved file is exactly what prune-v1 retires).
func newUpgradedProject(t *testing.T) (*Store, string) {
	t.Helper()
	s := testStore(t)
	plantV1Project(t, s, "ATM", v1RawLogFixture(t))
	if _, err := s.UpgradeProjectToV2("ATM"); err != nil {
		t.Fatal(err)
	}
	return s, s.StorePath()
}

// newBornV2Project creates a project through the normal (v2-only) public API:
// no log.jsonl is ever written for it, so prune-v1 must skip it as born-v2.
func newBornV2Project(t *testing.T) (*Store, string) {
	t.Helper()
	s := testStore(t)
	if _, err := s.CreateProject("ATM", "Agent Tasks Management", testActor); err != nil {
		t.Fatal(err)
	}
	return s, s.StorePath()
}

func TestPruneV1_RefusesNonV2(t *testing.T) {
	s, _ := newV1ActiveProject(t) // a project still on v1
	rep, err := s.PruneProjectV1("ATM", false)
	if err != nil {
		t.Fatalf("PruneProjectV1: %v", err)
	}
	if rep.Pruned {
		t.Fatalf("expected skip on a v1-active project, got %+v", rep)
	}
	if rep.Reason != "not v2-active" {
		t.Fatalf("reason = %q, want %q", rep.Reason, "not v2-active")
	}
}

func TestPruneV1_ArchivesByDefault(t *testing.T) {
	s, dir := newUpgradedProject(t) // v2-active, log.jsonl still on disk
	rep, err := s.PruneProjectV1("ATM", false)
	if err != nil {
		t.Fatalf("PruneProjectV1: %v", err)
	}
	if !rep.Pruned || rep.Archived == "" {
		t.Fatalf("expected archive, got %+v", rep)
	}
	if _, err := os.Stat(filepath.Join(dir, "projects", "ATM", "log.jsonl")); !os.IsNotExist(err) {
		t.Fatalf("log.jsonl should be gone after archive")
	}
	if _, err := os.Stat(rep.Archived); err != nil {
		t.Fatalf("archive missing: %v", err)
	}
}

func TestPruneV1_DeleteRemoves(t *testing.T) {
	s, dir := newUpgradedProject(t)
	rep, err := s.PruneProjectV1("ATM", true)
	if err != nil {
		t.Fatalf("PruneProjectV1: %v", err)
	}
	if !rep.Deleted {
		t.Fatalf("expected delete, got %+v", rep)
	}
	if _, err := os.Stat(filepath.Join(dir, "projects", "ATM", "log.jsonl")); !os.IsNotExist(err) {
		t.Fatalf("log.jsonl should be gone after delete")
	}
}

func TestPruneV1_SkipsBornV2(t *testing.T) {
	s, _ := newBornV2Project(t) // no log.jsonl
	rep, err := s.PruneProjectV1("ATM", false)
	if err != nil {
		t.Fatalf("PruneProjectV1: %v", err)
	}
	if rep.Pruned {
		t.Fatalf("expected skip for born-v2, got %+v", rep)
	}
	if rep.Reason != "born v2 (no v1 log)" {
		t.Fatalf("reason = %q, want %q", rep.Reason, "born v2 (no v1 log)")
	}
}

func TestPruneV1_RefusesDivergedProject(t *testing.T) {
	s, dir := newUpgradedProject(t)
	// Corrupt the v2 file so VerifyProject reports diverged/not-ok, then
	// confirm prune refuses rather than archiving/deleting the v1 log out
	// from under a project that doesn't verify clean.
	v2Path := filepath.Join(dir, "projects", "ATM", "events.v2.jsonl")
	if err := os.WriteFile(v2Path, []byte("not valid jsonl\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rep, err := s.PruneProjectV1("ATM", false)
	if err == nil {
		t.Fatalf("expected error on diverged project, got rep=%+v", rep)
	}
	if !IsIntegrity(err) {
		t.Fatalf("err = %v, want ErrIntegrity", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "projects", "ATM", "log.jsonl")); err != nil {
		t.Fatalf("log.jsonl should survive a refused prune: %v", err)
	}
}
