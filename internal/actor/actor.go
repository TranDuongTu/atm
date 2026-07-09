// Package actor resolves free-form actor strings into a (persona, agent,
// model) identity. It is a pure leaf package (no store, no alias table) so
// the read-side activity aggregation can share it. Legacy strings are
// inferred to a persona at read time; the store validates the convention at
// write time.
package actor

import "strings"

type Identity struct {
	Persona string
	Agent   string
	Model   string
}

const NonePersona = "(none)"

var Agents = []string{"claude", "codex", "opencode", "ollama"}

// Resolve maps a raw actor string to an Identity. Convention strings
// (persona@agent:model) are parsed; legacy strings are inferred to a persona
// at read time. Pure function — no alias table, no store dependency.
func Resolve(raw string) Identity {
	if strings.Contains(raw, "@") {
		persona, rest, _ := strings.Cut(raw, "@")
		agent, model, _ := strings.Cut(rest, ":")
		return normalize(Identity{Persona: persona, Agent: agent, Model: model})
	}
	return inferLegacy(raw)
}

func normalize(i Identity) Identity {
	if i.Persona == "" {
		i.Persona = NonePersona
	}
	return i
}

// inferLegacy maps a pre-convention actor string (no '@') to an Identity using
// ATM's own generated-default patterns. The empty string is the empty-persona
// case and resolves to (none); any other unrecognized string defaults to the
// developer working persona.
func inferLegacy(raw string) Identity {
	if raw == "" {
		return Identity{Persona: NonePersona}
	}
	switch raw {
	case "default":
		return Identity{Persona: "developer"}
	case "atm-manager":
		return Identity{Persona: "manager"}
	}
	for _, a := range Agents {
		switch raw {
		case a, a + "-dev":
			return Identity{Persona: "developer", Agent: a}
		case a + "-manager", a + "-onboard":
			return Identity{Persona: "manager", Agent: a}
		}
	}
	// Anything else without '@': default working persona, unknown agent.
	return Identity{Persona: "developer"}
}
