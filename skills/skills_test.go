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
	if !strings.Contains(m.Body, "Converge") {
		t.Fatal("manager prompt should drive toward capability Converge sections")
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
		if s.Persona != "concierge" {
			t.Errorf("%s: persona = %q, want concierge", s.Name, s.Persona)
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

func TestBuiltinCapabilitiesLoad(t *testing.T) {
	cs := Capabilities()
	if len(cs) == 0 {
		t.Fatal("no built-in capabilities loaded")
	}
	for _, c := range cs {
		if strings.Contains(c.Body, "## Brief") || strings.Contains(c.Body, "## Autopilot") {
			t.Errorf("%s: persona-specific Brief/Autopilot sections must not appear in capability files", c.Name)
		}
	}
	// Named, not counted: the set grows and shrinks as capabilities ship and
	// retire, and a magic total only ever fails for the wrong reason.
	for _, want := range []string{"scrum", "qa", "codereview", "release"} {
		if _, ok := Capability(want); !ok {
			t.Errorf("%s missing", want)
		}
	}
}

// A registry capability owns labels but seeds no boards. Its frontmatter must
// be allowed to say so rather than list a lane it does not have.
func TestCapabilityMayDeclareNoBoards(t *testing.T) {
	src := []byte("---\nname: reg\ndescription: A registry capability.\nlabels: [reg:*]\n---\n" +
		"## Semantics\nx\n\n## Actions\nx\n\n## Converge\nx\n")
	c, err := ParseCapability("reg", src)
	if err != nil {
		t.Fatalf("ParseCapability: %v", err)
	}
	if len(c.Boards) != 0 {
		t.Fatalf("boards = %v", c.Boards)
	}
}
