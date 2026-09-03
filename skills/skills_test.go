package skills

import (
	"strings"
	"testing"
)

func TestBuiltinPersonasLoad(t *testing.T) {
	ps := Personas()
	names := make([]string, 0, len(ps))
	for _, p := range ps {
		names = append(names, p.Name)
	}
	joined := strings.Join(names, ",")
	for _, want := range []string{"developer", "manager", "admin", "concierge"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("built-ins %v missing %s", names, want)
		}
	}
}

func TestManagerPersonaShape(t *testing.T) {
	m, ok := Persona("manager")
	if !ok {
		t.Fatal("manager not found")
	}
	if len(m.Expects) == 0 {
		t.Fatal("manager must declare expects")
	}
	if len(m.Optional) == 0 {
		t.Fatal("manager must declare optional params")
	}
	if !strings.Contains(m.Body, "## Duty") || !strings.Contains(m.Body, "sweep") {
		t.Fatal("manager prompt should run the sweep over guide Duty sections")
	}
}

func TestDeveloperPersonaShape(t *testing.T) {
	d, ok := Persona("developer")
	if !ok {
		t.Fatal("developer not found")
	}
	if d.Launch != "hook" {
		t.Fatalf("developer launches via plugin hook, got %q", d.Launch)
	}
	if len(d.Expects) == 0 {
		t.Fatal("developer must declare expects")
	}
	if !strings.Contains(d.Body, "Working Principles") {
		t.Fatal("developer prompt must contain Working Principles")
	}
}

func TestPersonaUnknown(t *testing.T) {
	if _, ok := Persona("nope"); ok {
		t.Fatal("unknown persona must report !ok")
	}
}

func TestConciergePersonaShape(t *testing.T) {
	c, ok := Persona("concierge")
	if !ok {
		t.Fatal("concierge not found")
	}
	if !c.ProjectOptional {
		t.Fatal("concierge must be launchable without --project")
	}
	if c.Launch != "prompt" {
		t.Fatalf("concierge launches prompt-style, got %q", c.Launch)
	}
	if c.Personality == "" {
		t.Fatal("concierge ships a default personality (the customization showcase)")
	}
	for _, jargon := range []string{"label substrate"} {
		if strings.Contains(c.Body, jargon) {
			t.Fatalf("concierge speaks the user's language; found %q", jargon)
		}
	}
}

func TestBuiltinChecklistSeedsLoad(t *testing.T) {
	ss := ChecklistSeeds()
	if len(ss) != 3 {
		t.Fatalf("want 3 built-in checklist seeds, got %d", len(ss))
	}
	for _, s := range ss {
		if len(s.Suits) != 1 || s.Suits[0] != "concierge" {
			t.Errorf("%s: suits = %v, want [concierge] (legacy persona key)", s.Name, s.Suits)
		}
		if s.Origin != "shipped:atm" {
			t.Errorf("%s: origin = %q, want loader default shipped:atm", s.Name, s.Origin)
		}
		if len(s.Steps) == 0 {
			t.Errorf("%s: no steps", s.Name)
		}
	}
	names := make(map[string]bool, len(ss))
	for _, s := range ss {
		names[s.Name] = true
	}
	for _, want := range []string{"empty-project", "setup-channels", "setup-agent-launcher"} {
		if !names[want] {
			t.Errorf("seed %s missing (have %v)", want, names)
		}
	}
}

func TestAdminLaunchesTUI(t *testing.T) {
	a, ok := Persona("admin")
	if !ok {
		t.Fatal("admin built-in missing")
	}
	if a.Launch != "tui" {
		t.Fatalf("admin launch = %q, want tui", a.Launch)
	}
}
