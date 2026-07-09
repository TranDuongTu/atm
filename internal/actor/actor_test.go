package actor

import "testing"

func id(p, a, m string) Identity { return Identity{Persona: p, Agent: a, Model: m} }

func TestResolveConvention(t *testing.T) {
	got := Resolve("developer@claude:opus-4.8")
	want := Identity{Persona: "developer", Agent: "claude", Model: "opus-4.8"}
	if got != want {
		t.Errorf("Resolve = %+v, want %+v", got, want)
	}
}

func TestResolveLegacyInference(t *testing.T) {
	cases := map[string]Identity{
		"default":        {Persona: "developer"},
		"claude":         {Persona: "developer", Agent: "claude"},
		"ollama-dev":     {Persona: "developer", Agent: "ollama"},
		"atm-manager":    {Persona: "manager"},
		"codex-manager":  {Persona: "manager", Agent: "codex"},
		"ollama-onboard": {Persona: "manager", Agent: "ollama"},
		"somethingelse":  {Persona: "developer"},
		"":               {Persona: NonePersona},
	}
	for raw, want := range cases {
		if got := Resolve(raw); got != want {
			t.Errorf("Resolve(%q) = %+v, want %+v", raw, got, want)
		}
	}
}

// Convention strings still parse with the same rules the old resolver had:
// model keeps extra colons, empty persona -> (none).
func TestResolveConventionEdgeCases(t *testing.T) {
	cases := map[string]Identity{
		"staff@codex":   id("staff", "codex", ""),
		"@claude:gpt-5": id(NonePersona, "claude", "gpt-5"),
		"p@a:b:c":       id("p", "a", "b:c"),
	}
	for raw, want := range cases {
		if got := Resolve(raw); got != want {
			t.Errorf("Resolve(%q) = %+v, want %+v", raw, got, want)
		}
	}
}
