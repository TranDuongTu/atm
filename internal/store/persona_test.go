package store

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestPersonaCRUD(t *testing.T) {
	s := newTestStore(t)

	if _, err := s.CreatePersona("Staff", "p", "", testActor); !IsUsage(err) {
		t.Fatalf("uppercase name should be ErrUsage, got %v", err)
	}
	if _, err := s.CreatePersona("staff-engineer", "high bar", "reviewer", testActor); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreatePersona("staff-engineer", "dup", "", testActor); !IsConflict(err) {
		t.Fatalf("duplicate should be ErrConflict, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(s.Root, "personas", "staff-engineer.json")); err != nil {
		t.Fatalf("persona file missing: %v", err)
	}

	got, err := s.GetPersona("staff-engineer")
	if err != nil || got.Prompt != "high bar" || got.Description != "reviewer" {
		t.Fatalf("get = %+v, %v", got, err)
	}

	newPrompt := "even higher bar"
	if _, err := s.EditPersona("staff-engineer", &newPrompt, nil, testActor); err != nil {
		t.Fatal(err)
	}
	got, _ = s.GetPersona("staff-engineer")
	if got.Prompt != "even higher bar" || got.Description != "reviewer" {
		t.Fatalf("edit left wrong state: %+v", got)
	}
	if _, err := s.EditPersona("ghost", &newPrompt, nil, testActor); !IsNotFound(err) {
		t.Fatalf("edit missing should be ErrNotFound, got %v", err)
	}

	if err := s.RemovePersona("staff-engineer"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetPersona("staff-engineer"); !IsNotFound(err) {
		t.Fatalf("get after remove should be ErrNotFound, got %v", err)
	}
}

func TestPersonaNameTraversalRejected(t *testing.T) {
	s := newTestStore(t)

	if _, err := s.GetPersona("../evil"); !IsUsage(err) {
		t.Fatalf("GetPersona traversal should be ErrUsage, got %v", err)
	}
	newPrompt := "pwned"
	if _, err := s.EditPersona("../evil", &newPrompt, nil, testActor); !IsUsage(err) {
		t.Fatalf("EditPersona traversal should be ErrUsage, got %v", err)
	}
	if err := s.RemovePersona("../evil"); !IsUsage(err) {
		t.Fatalf("RemovePersona traversal should be ErrUsage, got %v", err)
	}
}

func TestRemovePersonaRejectsBuiltins(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.SeedPersonas("admin@atm:seed"); err != nil {
		t.Fatalf("seed: %v", err)
	}
	for _, name := range []string{"developer", "manager", "admin"} {
		if err := s.RemovePersona(name); !errors.Is(err, ErrUsage) {
			t.Errorf("RemovePersona(%q) = %v, want ErrUsage", name, err)
		}
		if _, err := s.GetPersona(name); err != nil {
			t.Errorf("built-in %q was removed: %v", name, err)
		}
	}
}

func TestSeedPersonasIncludesAdmin(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.SeedPersonas("admin@atm:seed"); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := s.GetPersona("admin"); err != nil {
		t.Errorf("admin not seeded: %v", err)
	}
}

func TestSeedPersonasIdempotent(t *testing.T) {
	s := newTestStore(t)
	added, err := s.SeedPersonas(testActor)
	if err != nil {
		t.Fatal(err)
	}
	if len(added) != 3 {
		t.Fatalf("first seed added %v, want 3", added)
	}
	// User edits a built-in.
	edited := "custom"
	if _, err := s.EditPersona("developer", &edited, nil, testActor); err != nil {
		t.Fatal(err)
	}
	added2, err := s.SeedPersonas(testActor)
	if err != nil {
		t.Fatal(err)
	}
	if len(added2) != 0 {
		t.Fatalf("second seed added %v, want none", added2)
	}
	got, _ := s.GetPersona("developer")
	if got.Prompt != "custom" {
		t.Fatalf("seed clobbered user edit: %q", got.Prompt)
	}
	if len(s.ListPersonas()) != 3 {
		t.Fatalf("list = %d, want 3", len(s.ListPersonas()))
	}
}
