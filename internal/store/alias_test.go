package store

import (
	"testing"

	"atm/internal/actor"
)

func TestAliasSetLoadRemove(t *testing.T) {
	s := newTestStore(t)
	if m, err := s.LoadAliases(); err != nil || len(m) != 0 {
		t.Fatalf("empty load = %v, %v", m, err)
	}
	if err := s.SetAlias("opencode-dev", actor.AliasEntry{Persona: "developer", Agent: "opencode"}); err != nil {
		t.Fatal(err)
	}
	m, err := s.LoadAliases()
	if err != nil || m["opencode-dev"].Persona != "developer" || m["opencode-dev"].Agent != "opencode" {
		t.Fatalf("load = %+v, %v", m, err)
	}
	if err := s.RemoveAlias("opencode-dev"); err != nil {
		t.Fatal(err)
	}
	if m, _ := s.LoadAliases(); len(m) != 0 {
		t.Fatalf("after remove = %+v", m)
	}
}

func TestMigrateActors(t *testing.T) {
	s := newTestStore(t)
	// Two projects with legacy actors on their logs.
	if _, err := s.CreateProject("AAA", "A", "opencode-dev"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateProject("BBB", "B", "ollama-manager"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateTask("AAA", "t", "", nil, "default"); err != nil {
		t.Fatal(err)
	}

	res, err := s.MigrateActors(true) // dry-run
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Seeded) != 2 {
		t.Fatalf("dry-run seeded %v, want developer+manager", res.Seeded)
	}
	if got := res.Added["opencode-dev"]; got.Persona != "developer" || got.Agent != "opencode" {
		t.Fatalf("opencode-dev -> %+v", got)
	}
	if got := res.Added["ollama-manager"]; got.Persona != "manager" || got.Agent != "ollama" {
		t.Fatalf("ollama-manager -> %+v", got)
	}
	if got := res.Added["default"]; got.Persona != "developer" {
		t.Fatalf("default -> %+v", got)
	}
	if m, _ := s.LoadAliases(); len(m) != 0 {
		t.Fatalf("dry-run must not write aliases, got %+v", m)
	}

	// Real run writes; second run is a no-op and preserves a user override.
	if _, err := s.MigrateActors(false); err != nil {
		t.Fatal(err)
	}
	if err := s.SetAlias("default", actor.AliasEntry{Persona: "manager"}); err != nil {
		t.Fatal(err)
	}
	res2, err := s.MigrateActors(false)
	if err != nil {
		t.Fatal(err)
	}
	if len(res2.Added) != 0 {
		t.Fatalf("second migrate added %+v, want none", res2.Added)
	}
	m, _ := s.LoadAliases()
	if m["default"].Persona != "manager" {
		t.Fatalf("migrate clobbered user override: %+v", m["default"])
	}
}
