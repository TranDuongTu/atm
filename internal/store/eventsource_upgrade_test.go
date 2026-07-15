package store

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// v1RawLogFixture returns the raw bytes of internal/eventsource/testdata/v1-log.jsonl,
// the same fixture internal/eventsource's own upgrade tests replay. Its
// project.created subject code is "ATM". The born-v2 flip made the public
// Create* API v2-only, so a v1-active project fixture can no longer be built
// by calling CreateTask; it must be planted as raw v1 log bytes instead.
func v1RawLogFixture(t *testing.T) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "eventsource", "testdata", "v1-log.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// plantV1Project writes raw v1 log bytes directly into the store's project
// directory as log.jsonl, WITHOUT going through CreateProject/CreateTask (the
// public API can no longer produce v1-born media) and WITHOUT calling
// SetActiveFormat or setProjectFormat: a fresh store defaults to v1
// (StoreFormatV1) and a project directory holding only log.jsonl (no
// ProjectFormats entry) reads as v1-active through that default, which is
// exactly the on-disk shape UpgradeProjectToV2 is built to consume.
func plantV1Project(t *testing.T, s *Store, code string, raw []byte) {
	t.Helper()
	if err := os.MkdirAll(s.projectDir(code), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.logPath(code), raw, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestUpgradeProjectToV2PreservesV1LogAndActivatesV2(t *testing.T) {
	s := testStore(t)
	raw := v1RawLogFixture(t)
	plantV1Project(t, s, "ATM", raw)

	if f, err := s.projectFormat("ATM"); err != nil {
		t.Fatal(err)
	} else if f != StoreFormatV1 {
		t.Fatalf("precondition: format = %q, want v1 (planted with no explicit entry)", f)
	}

	rep, err := s.UpgradeProjectToV2("ATM")
	if err != nil {
		t.Fatal(err)
	}
	if rep.Project != "ATM" || rep.Events == 0 || rep.Format != StoreFormatV2 {
		t.Fatalf("bad report: %#v", rep)
	}

	// log.jsonl is never rewritten by the upgrade: byte-identical before/after.
	after, err := os.ReadFile(s.logPath("ATM"))
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != string(after) {
		t.Fatal("v1 log changed during upgrade")
	}

	if _, err := os.Stat(s.eventsV2Path("ATM")); err != nil {
		t.Fatalf("events.v2.jsonl missing: %v", err)
	}

	if f, err := s.ProjectFormatForCLI("ATM"); err != nil {
		t.Fatal(err)
	} else if f != StoreFormatV2 {
		t.Fatalf("format after upgrade = %q, want v2", f)
	}

	// Upgraded tasks/comments read back correctly via the v2 read path.
	tasks := s.ListTasks(QueryFilters{Project: "ATM"})
	if len(tasks) != 1 {
		t.Fatalf("ListTasks after upgrade = %d tasks, want 1 (ATM-0002 was removed in the v1 log)", len(tasks))
	}
	tk := tasks[0]
	if tk.ID != "ATM-0001" {
		t.Fatalf("task id = %q, want ATM-0001", tk.ID)
	}
	if tk.Title != "First task, retitled" {
		t.Fatalf("task title = %q, want %q", tk.Title, "First task, retitled")
	}
	wantLabels := []string{"ATM:status:done"}
	if len(tk.Labels) != len(wantLabels) || tk.Labels[0] != wantLabels[0] {
		t.Fatalf("task labels = %v, want %v", tk.Labels, wantLabels)
	}

	comments, err := s.ListComments(tk.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(comments) != 2 {
		t.Fatalf("comments after upgrade = %d, want 2", len(comments))
	}
}

// TestUpgradeRefusesEffectiveV2Project covers the format guard (spec L3-5):
// upgrade reads FROM the frozen v1 log, so running it a second time against
// an already-v2-active project (with no rollback, upgrade is at most once)
// must refuse rather than rebuild from stale v1 bytes.
func TestUpgradeRefusesEffectiveV2Project(t *testing.T) {
	s := testStore(t)
	plantV1Project(t, s, "ATM", v1RawLogFixture(t))

	if _, err := s.UpgradeProjectToV2("ATM"); err != nil {
		t.Fatal(err)
	}
	_, err := s.UpgradeProjectToV2("ATM")
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("second upgrade of a v2-active project = %v, want ErrConflict", err)
	}
}

// TestUpgradeRefusesAPreexistingV2File covers the belt-and-braces disk check
// (step 4 of UpgradeProjectToV2): with no rollback, a project is upgraded at
// most once through the normal (format-gated) path, so the only way to reach
// an orphaned events.v2.jsonl ahead of a v1-active project is by direct disk
// manipulation. That must refuse, not displace, and must leave the v1
// project's format untouched.
func TestUpgradeRefusesAPreexistingV2File(t *testing.T) {
	s := testStore(t)
	plantV1Project(t, s, "ATM", v1RawLogFixture(t))

	if err := os.WriteFile(s.eventsV2Path("ATM"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := s.UpgradeProjectToV2("ATM")
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("upgrade with a pre-existing events.v2.jsonl = %v, want ErrConflict", err)
	}
	// The refused upgrade must leave the v1 project readable: the format
	// entry was never flipped.
	if f, _ := s.projectFormat("ATM"); f != StoreFormatV1 {
		t.Fatalf("format after refused upgrade = %q, want v1", f)
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
