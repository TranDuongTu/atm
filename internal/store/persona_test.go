package store

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPersonaCRUD(t *testing.T) {
	s := newTestStore(t)

	if _, err := s.CreatePersona("Staff", "p", "", "tester"); !IsUsage(err) {
		t.Fatalf("uppercase name should be ErrUsage, got %v", err)
	}
	if _, err := s.CreatePersona("staff-engineer", "high bar", "reviewer", "tester"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreatePersona("staff-engineer", "dup", "", "tester"); !IsConflict(err) {
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
	if _, err := s.EditPersona("staff-engineer", &newPrompt, nil, "tester"); err != nil {
		t.Fatal(err)
	}
	got, _ = s.GetPersona("staff-engineer")
	if got.Prompt != "even higher bar" || got.Description != "reviewer" {
		t.Fatalf("edit left wrong state: %+v", got)
	}
	if _, err := s.EditPersona("ghost", &newPrompt, nil, "tester"); !IsNotFound(err) {
		t.Fatalf("edit missing should be ErrNotFound, got %v", err)
	}

	if err := s.RemovePersona("staff-engineer"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetPersona("staff-engineer"); !IsNotFound(err) {
		t.Fatalf("get after remove should be ErrNotFound, got %v", err)
	}
}

func TestSeedPersonasIdempotent(t *testing.T) {
	s := newTestStore(t)
	added, err := s.SeedPersonas("seed")
	if err != nil {
		t.Fatal(err)
	}
	if len(added) != 2 {
		t.Fatalf("first seed added %v, want 2", added)
	}
	// User edits a built-in.
	edited := "custom"
	if _, err := s.EditPersona("developer", &edited, nil, "u"); err != nil {
		t.Fatal(err)
	}
	added2, err := s.SeedPersonas("seed")
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
	if len(s.ListPersonas()) != 2 {
		t.Fatalf("list = %d, want 2", len(s.ListPersonas()))
	}
}
