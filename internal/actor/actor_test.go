package actor

import "testing"

func id(p, a, m string) Identity { return Identity{Persona: p, Agent: a, Model: m} }

func TestResolve_Convention(t *testing.T) {
	cases := map[string]Identity{
		"staff-engineer@claude:opus-4.8": id("staff-engineer", "claude", "opus-4.8"),
		"staff@codex":                    id("staff", "codex", ""),
		"solo":                           id("solo", "", ""),
		"@claude:gpt-5":                  id(NonePersona, "claude", "gpt-5"),
		"p@a:b:c":                        id("p", "a", "b:c"), // model keeps extra colons
		"":                              id(NonePersona, "", ""),
	}
	for raw, want := range cases {
		if got := Resolve(raw, nil); got != want {
			t.Errorf("Resolve(%q) = %+v, want %+v", raw, got, want)
		}
	}
}

func TestResolve_AliasWins(t *testing.T) {
	aliases := AliasMap{
		"opencode-dev": {Persona: "developer", Agent: "opencode"},
		// alias overrides even a convention-formatted string:
		"x@y:z": {Persona: "manager", Agent: "ollama"},
	}
	if got := Resolve("opencode-dev", aliases); got != id("developer", "opencode", "") {
		t.Errorf("legacy alias: got %+v", got)
	}
	if got := Resolve("x@y:z", aliases); got != id("manager", "ollama", "") {
		t.Errorf("alias precedence: got %+v", got)
	}
}

func TestLegacyAlias(t *testing.T) {
	cases := map[string]AliasEntry{
		"opencode-dev":     {Persona: "developer", Agent: "opencode"},
		"ollama-onboard":   {Persona: "manager", Agent: "ollama"},
		"opencode-manager": {Persona: "manager", Agent: "opencode"},
		"codex":            {Persona: "developer", Agent: "codex"},
		"atm-manager":      {Persona: "manager"},
		"default":          {Persona: "developer"},
		"weird-name":       {Persona: "developer"},
	}
	for raw, want := range cases {
		got, ok := LegacyAlias(raw)
		if !ok || got != want {
			t.Errorf("LegacyAlias(%q) = %+v,%v want %+v", raw, got, ok, want)
		}
	}
	if _, ok := LegacyAlias("has@at"); ok {
		t.Errorf("convention-formatted string should not be legacy-aliased")
	}
}
