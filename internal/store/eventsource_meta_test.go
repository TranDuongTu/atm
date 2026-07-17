package store

import (
	"atm/internal/store/eventlog"
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

func TestProjectFormatDefaultsToV1(t *testing.T) {
	s := testStore(t)
	f, err := s.eng.ProjectFormat("ATM")
	if err != nil {
		t.Fatal(err)
	}
	if f != eventlog.StoreFormatV1 {
		t.Fatalf("format = %q, want v1", f)
	}
}

func TestSetProjectFormatPersists(t *testing.T) {
	s := testStore(t)
	if err := s.eng.SetProjectFormat("ATM", eventlog.StoreFormatV2); err != nil {
		t.Fatal(err)
	}
	again, err := Open(s.StorePath())
	if err != nil {
		t.Fatal(err)
	}
	f, err := again.eng.ProjectFormat("ATM")
	if err != nil {
		t.Fatal(err)
	}
	if f != eventlog.StoreFormatV2 {
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
	if err := s.eng.RemoveProjectFormat("ATM"); err != nil { // simulate a legacy entry-less project
		t.Fatal(err)
	}
	if err := s.SetActiveFormat(eventlog.StoreFormatV2); err == nil {
		t.Fatal("SetActiveFormat(v2) must refuse while ATM lacks an explicit ProjectFormats entry")
	}
	if err := s.eng.SetProjectFormat("ATM", eventlog.StoreFormatV1); err != nil {
		t.Fatal(err)
	}
	if err := s.SetActiveFormat(eventlog.StoreFormatV2); err != nil {
		t.Fatalf("SetActiveFormat(v2) with all entries explicit: %v", err)
	}
	if err := s.SetActiveFormat(eventlog.StoreFormatV1); err != nil {
		t.Fatalf("SetActiveFormat(v1) must always be allowed: %v", err)
	}
}
