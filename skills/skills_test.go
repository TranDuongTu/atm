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
	for _, want := range []string{"developer", "manager", "admin"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("built-ins %v missing %s", names, want)
		}
	}
	for _, gone := range []string{"concierge"} {
		if strings.Contains(joined, gone) {
			t.Fatalf("built-ins still carry the pruned %s persona: %v", gone, names)
		}
	}
}

func TestManagerPersonaShape(t *testing.T) {
	m, ok := Persona("manager")
	if !ok {
		t.Fatal("manager not found")
	}
	if !strings.Contains(m.Body, "sweep") {
		t.Fatal("manager prompt should run the sweep over capability guides")
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
	if !strings.Contains(d.Body, "Working Principles") {
		t.Fatal("developer prompt must contain Working Principles")
	}
}

func TestPersonaUnknown(t *testing.T) {
	if _, ok := Persona("nope"); ok {
		t.Fatal("unknown persona must report !ok")
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