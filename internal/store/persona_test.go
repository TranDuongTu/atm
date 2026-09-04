package store

import (
	"strings"
	"testing"

	"atm/internal/core"
)

// The machine-global custom-persona store is pruned (plan §7): personas are
// PROJECT records (import with `atm persona set`), and the binary carries the
// built-ins as the pre-profile fallback. These tests pin that shape.

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
}

func TestGetPersonaRejectsTraversal(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.GetPersona("../evil"); !core.IsUsage(err) {
		t.Fatalf("GetPersona traversal should be core.ErrUsage, got %v", err)
	}
}

// An unknown name names nothing: no project record is in scope from here
// (that surface lives on persona_record.go), and no custom store exists to
// fall back to. The error must say how to get a persona at all.
func TestGetPersonaUnknownIsNotFound(t *testing.T) {
	s := newTestStore(t)
	_, err := s.GetPersona("staff-engineer")
	if !core.IsNotFound(err) {
		t.Fatalf("unknown persona should be core.ErrNotFound, got %v", err)
	}
	if !strings.Contains(err.Error(), "persona set") {
		t.Fatalf("error must point at `persona set`: %v", err)
	}
}

func TestListPersonasIsBuiltinsOnly(t *testing.T) {
	s := newTestStore(t)
	var names []string
	for _, p := range s.ListPersonas() {
		names = append(names, p.Name)
	}
	joined := strings.Join(names, ",")
	for _, want := range []string{"developer", "manager", "admin"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("list %v missing %s", names, want)
		}
	}
	if len(names) != 3 {
		t.Fatalf("built-ins only: %v", names)
	}
}

func TestListPersonasCarriesProjectOptional(t *testing.T) {
	s := newTestStore(t)
	byName := map[string]bool{}
	for _, p := range s.ListPersonas() {
		byName[p.Name] = p.ProjectOptional
	}
	if byName["manager"] || byName["developer"] {
		t.Error("manager/developer should be project-required")
	}
	if !byName["admin"] {
		t.Error("admin should be project-optional")
	}
}

func TestPersonaLaunchRoundTrip(t *testing.T) {
	s := newTestStore(t)
	a, err := s.GetPersona("admin")
	if err != nil || a.Launch != "tui" {
		t.Fatalf("admin launch = %+v (%v), want tui", a, err)
	}
	d, err := s.GetPersona("developer")
	if err != nil || d.Launch != "hook" {
		t.Fatalf("developer launch = %+v (%v), want hook", d, err)
	}
}