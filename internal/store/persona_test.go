package store

import (
	"atm/internal/core"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPersonaCRUD(t *testing.T) {
	s := newTestStore(t)

	if _, err := s.CreatePersona("Staff", "p", "", testActor); !core.IsUsage(err) {
		t.Fatalf("uppercase name should be core.ErrUsage, got %v", err)
	}
	if _, err := s.CreatePersona("staff-engineer", "high bar", "reviewer", testActor); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreatePersona("staff-engineer", "dup", "", testActor); !core.IsConflict(err) {
		t.Fatalf("duplicate should be core.ErrConflict, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(s.Root, "personas", "staff-engineer.md")); err != nil {
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
	if _, err := s.EditPersona("ghost", &newPrompt, nil, testActor); !core.IsNotFound(err) {
		t.Fatalf("edit missing should be core.ErrNotFound, got %v", err)
	}

	if err := s.RemovePersona("staff-engineer"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetPersona("staff-engineer"); !core.IsNotFound(err) {
		t.Fatalf("get after remove should be core.ErrNotFound, got %v", err)
	}
}

func TestPersonaNameTraversalRejected(t *testing.T) {
	s := newTestStore(t)

	if _, err := s.GetPersona("../evil"); !core.IsUsage(err) {
		t.Fatalf("GetPersona traversal should be core.ErrUsage, got %v", err)
	}
	newPrompt := "pwned"
	if _, err := s.EditPersona("../evil", &newPrompt, nil, testActor); !core.IsUsage(err) {
		t.Fatalf("EditPersona traversal should be core.ErrUsage, got %v", err)
	}
	if err := s.RemovePersona("../evil"); !core.IsUsage(err) {
		t.Fatalf("RemovePersona traversal should be core.ErrUsage, got %v", err)
	}
}

func TestRemovePersonaRejectsBuiltins(t *testing.T) {
	s := newTestStore(t)
	for _, name := range []string{"developer", "manager", "admin"} {
		if err := s.RemovePersona(name); !errors.Is(err, core.ErrUsage) {
			t.Errorf("RemovePersona(%q) = %v, want core.ErrUsage", name, err)
		}
		if _, err := s.GetPersona(name); err != nil {
			t.Errorf("built-in %q was removed: %v", name, err)
		}
	}
}

func TestBuiltinPersonasResolveWithoutSeeding(t *testing.T) {
	s := newTestStore(t)
	for _, name := range []string{"developer", "manager", "admin"} {
		p, err := s.GetPersona(name)
		if err != nil {
			t.Fatalf("GetPersona(%s): %v", name, err)
		}
		if p.Prompt == "" || p.Description == "" {
			t.Fatalf("built-in %s empty: %+v", name, p)
		}
	}
	if entries, _ := os.ReadDir(filepath.Join(s.Root, "personas")); len(entries) != 0 {
		t.Fatalf("built-ins must not be materialized in the store; found %d files", len(entries))
	}
}

func TestCustomPersonaMarkdownRoundTrip(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreatePersona("reviewer", "Review things.", "Reviews PRs.", "admin@cli:unset"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(s.Root, "personas", "reviewer.md")); err != nil {
		t.Fatalf("custom persona must persist as markdown: %v", err)
	}
	p, err := s.GetPersona("reviewer")
	if err != nil {
		t.Fatal(err)
	}
	if p.Prompt != "Review things." || p.Description != "Reviews PRs." {
		t.Fatalf("round trip: %+v", p)
	}
	doc, err := s.PersonaDoc("reviewer")
	if err != nil || !strings.HasPrefix(doc, "---\n") {
		t.Fatalf("PersonaDoc: %q err=%v", doc, err)
	}
}

func TestLegacyJSONPersonaMigrates(t *testing.T) {
	s := newTestStore(t)
	old := core.Persona{Name: "legacy", Prompt: "Old prompt.", Description: "Old desc.",
		CreatedAt: core.Now(), UpdatedAt: core.Now(), CreatedBy: "a@b:c", UpdatedBy: "a@b:c"}
	if err := os.MkdirAll(filepath.Join(s.Root, "personas"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := WriteFileAtomic(filepath.Join(s.Root, "personas", "legacy.json"), &old); err != nil {
		t.Fatal(err)
	}
	p, err := s.GetPersona("legacy")
	if err != nil || p.Prompt != "Old prompt." {
		t.Fatalf("migrated read: %+v err=%v", p, err)
	}
	if _, err := os.Stat(filepath.Join(s.Root, "personas", "legacy.md")); err != nil {
		t.Fatalf("json must convert to md: %v", err)
	}
	if _, err := os.Stat(filepath.Join(s.Root, "personas", "legacy.json")); !os.IsNotExist(err) {
		t.Fatal("json must be removed after migration")
	}
}

func TestBuiltinEditRefusedOverlayWorks(t *testing.T) {
	s := newTestStore(t)
	prompt := "x"
	if _, err := s.EditPersona("manager", &prompt, nil, "admin@cli:unset"); err == nil {
		t.Fatal("editing a built-in must fail; personality overlay is the customization path")
	}
	if err := s.SetPersonality("manager", "Dry wit.", "admin@cli:unset"); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetPersonality("manager")
	if err != nil || got != "Dry wit." {
		t.Fatalf("overlay: %q err=%v", got, err)
	}
	if err := s.ClearPersonality("manager"); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.GetPersonality("manager"); got != "" {
		t.Fatalf("cleared overlay still returns %q", got)
	}
	if err := s.SetPersonality("ghost", "x", "admin@cli:unset"); err == nil {
		t.Fatal("overlay for unknown persona must fail")
	}
}

func TestListPersonasMergesBuiltinsAndCustoms(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreatePersona("zed", "p", "d", "admin@cli:unset"); err != nil {
		t.Fatal(err)
	}
	names := []string{}
	for _, p := range s.ListPersonas() {
		names = append(names, p.Name)
	}
	joined := strings.Join(names, ",")
	for _, want := range []string{"developer", "manager", "admin", "zed"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("list %v missing %s", names, want)
		}
	}
}

func TestCreatePersonaRefusesBuiltinName(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreatePersona("manager", "p", "d", "admin@cli:unset"); err == nil {
		t.Fatal("shadowing a built-in must fail")
	}
}

// writeLegacyJSONPersona seeds a .json-only persona (no .md) so callers can
// exercise the migration path under EditPersona/RemovePersona without a prior
// GetPersona.
func writeLegacyJSONPersona(t *testing.T, s *Store, name string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(s.Root, "personas"), 0o755); err != nil {
		t.Fatal(err)
	}
	old := core.Persona{Name: name, Prompt: "Old prompt.", Description: "Old desc.",
		CreatedAt: core.Now(), UpdatedAt: core.Now(), CreatedBy: "a@b:c", UpdatedBy: "a@b:c"}
	if err := WriteFileAtomic(filepath.Join(s.Root, "personas", name+".json"), &old); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(s.Root, "personas", name+".md")); err == nil {
		t.Fatalf("precondition: %s.md must not exist", name)
	}
}

// TestEditPersonaLegacyJSONNoDeadlock exercises the re-entrant WithLock bug:
// a .json-only persona edited without a prior GetPersona used to deadlock
// because GetPersona's migration path re-acquires the "personas" lock.
func TestEditPersonaLegacyJSONNoDeadlock(t *testing.T) {
	s := newTestStore(t)
	writeLegacyJSONPersona(t, s, "legacy-edit")

	type result struct {
		p   *core.Persona
		err error
	}
	done := make(chan result, 1)
	go func() {
		newPrompt := "edited prompt"
		p, err := s.EditPersona("legacy-edit", &newPrompt, nil, testActor)
		done <- result{p, err}
	}()
	select {
	case r := <-done:
		if r.err != nil {
			t.Fatalf("EditPersona on legacy json: %v", r.err)
		}
		if r.p.Prompt != "edited prompt" {
			t.Fatalf("prompt not updated: %+v", r.p)
		}
		if _, err := os.Stat(filepath.Join(s.Root, "personas", "legacy-edit.md")); err != nil {
			t.Fatalf("md not written by migration: %v", err)
		}
		if _, err := os.Stat(filepath.Join(s.Root, "personas", "legacy-edit.json")); !os.IsNotExist(err) {
			t.Fatalf("json not cleaned: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("EditPersona on .json-only persona deadlocked (re-entrant WithLock)")
	}
}

// TestRemovePersonaLegacyJSONNoDeadlock is the remove-path analogue of the
// edit test above.
func TestRemovePersonaLegacyJSONNoDeadlock(t *testing.T) {
	s := newTestStore(t)
	writeLegacyJSONPersona(t, s, "legacy-rm")

	done := make(chan error, 1)
	go func() {
		done <- s.RemovePersona("legacy-rm")
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("RemovePersona on legacy json: %v", err)
		}
		if _, err := os.Stat(filepath.Join(s.Root, "personas", "legacy-rm.md")); !os.IsNotExist(err) {
			t.Fatalf("md not removed: %v", err)
		}
		if _, err := os.Stat(filepath.Join(s.Root, "personas", "legacy-rm.json")); !os.IsNotExist(err) {
			t.Fatalf("json not removed: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("RemovePersona on .json-only persona deadlocked (re-entrant WithLock)")
	}
}
