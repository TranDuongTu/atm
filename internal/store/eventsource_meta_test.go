package store

import (
	"os"
	"path/filepath"
	"testing"
)

// testStore is an alias for newTestStore (defined in project_test.go),
// matching the brief's naming for this new test file.
func testStore(t *testing.T) *Store {
	t.Helper()
	return newTestStore(t)
}

func TestEventsourcePaths(t *testing.T) {
	s := testStore(t)
	if got, want := s.eventsV2Path("ATM"), filepath.Join(s.StorePath(), "projects", "ATM", "events.v2.jsonl"); got != want {
		t.Fatalf("eventsV2Path = %q, want %q", got, want)
	}
	if got, want := s.eventsourceMetaPath("ATM"), filepath.Join(s.StorePath(), "projects", "ATM", "eventsource.json"); got != want {
		t.Fatalf("eventsourceMetaPath = %q, want %q", got, want)
	}
	if got, want := s.storeMetaPath(), filepath.Join(s.StorePath(), "store.json"); got != want {
		t.Fatalf("storeMetaPath = %q, want %q", got, want)
	}
}

func TestProjectFormatDefaultsToV1(t *testing.T) {
	s := testStore(t)
	f, err := s.projectFormat("ATM")
	if err != nil {
		t.Fatal(err)
	}
	if f != StoreFormatV1 {
		t.Fatalf("format = %q, want v1", f)
	}
}

func TestSetProjectFormatPersists(t *testing.T) {
	s := testStore(t)
	if err := s.setProjectFormat("ATM", StoreFormatV2); err != nil {
		t.Fatal(err)
	}
	again, err := Open(s.StorePath())
	if err != nil {
		t.Fatal(err)
	}
	f, err := again.projectFormat("ATM")
	if err != nil {
		t.Fatal(err)
	}
	if f != StoreFormatV2 {
		t.Fatalf("format after reopen = %q, want v2", f)
	}
	if _, err := os.Stat(filepath.Join(s.StorePath(), "store.json")); err != nil {
		t.Fatalf("store.json missing: %v", err)
	}
}

func TestSetActiveFormatV2RefusesWhileProjectsLackEntries(t *testing.T) {
	s := testStore(t)
	if _, err := s.CreateProject("ATM", "x", "admin@cli:unset"); err != nil {
		t.Fatal(err)
	}
	if err := s.removeProjectFormat("ATM"); err != nil { // simulate a legacy entry-less project
		t.Fatal(err)
	}
	if err := s.SetActiveFormat(StoreFormatV2); err == nil {
		t.Fatal("SetActiveFormat(v2) must refuse while ATM lacks an explicit ProjectFormats entry")
	}
	if err := s.setProjectFormat("ATM", StoreFormatV1); err != nil {
		t.Fatal(err)
	}
	if err := s.SetActiveFormat(StoreFormatV2); err != nil {
		t.Fatalf("SetActiveFormat(v2) with all entries explicit: %v", err)
	}
	if err := s.SetActiveFormat(StoreFormatV1); err != nil {
		t.Fatalf("SetActiveFormat(v1) must always be allowed: %v", err)
	}
}
