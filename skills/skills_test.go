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
	if m.DefaultMode != "autopilot" {
		t.Fatalf("default mode = %q", m.DefaultMode)
	}
	if got := strings.Join(m.ModeNames(), ","); got != "brief,autopilot,ask" {
		t.Fatalf("modes = %s", got)
	}
	for _, banned := range []string{"\"Brief\" section", "\"Autopilot\" section"} {
		if strings.Contains(m.Body, banned) {
			t.Fatalf("manager prompt must not reference capability guide sections by the old names: %s", banned)
		}
	}
	if !strings.Contains(m.Body, "Converge") {
		t.Fatal("manager modes should drive toward capability Converge sections")
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
	if len(d.Modes) != 0 {
		t.Fatalf("developer declares no modes: %v", d.ModeNames())
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

func TestBuiltinCapabilitiesLoad(t *testing.T) {
	cs := Capabilities()
	if len(cs) != 3 {
		t.Fatalf("want 3 built-in capabilities, got %d", len(cs))
	}
	for _, c := range cs {
		if strings.Contains(c.Body, "## Brief") || strings.Contains(c.Body, "## Autopilot") {
			t.Errorf("%s: persona-specific Brief/Autopilot sections must not appear in capability files", c.Name)
		}
	}
	if _, ok := Capability("workflow_ai"); !ok {
		t.Fatal("workflow_ai missing")
	}
}
