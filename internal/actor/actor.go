// Package actor resolves free-form actor strings into a (persona, agent,
// model) identity. It is a pure leaf package (no store dependency) so both
// the store's migration and the read-side activity aggregation can share it.
package actor

import "strings"

type Identity struct {
	Persona string
	Agent   string
	Model   string
}

type AliasEntry struct {
	Persona string `json:"persona"`
	Agent   string `json:"agent,omitempty"`
	Model   string `json:"model,omitempty"`
}

type AliasMap = map[string]AliasEntry

const NonePersona = "(none)"

var Agents = []string{"claude", "codex", "opencode", "ollama"}

// Resolve maps a raw actor string to an Identity. Resolution order:
// alias table (exact match, wins over everything) -> convention parse -> (none).
func Resolve(raw string, aliases AliasMap) Identity {
	if e, ok := aliases[raw]; ok {
		return normalize(Identity{Persona: e.Persona, Agent: e.Agent, Model: e.Model})
	}
	persona, rest, hasAt := strings.Cut(raw, "@")
	if !hasAt {
		return normalize(Identity{Persona: raw})
	}
	agent, model, _ := strings.Cut(rest, ":")
	return normalize(Identity{Persona: persona, Agent: agent, Model: model})
}

func normalize(i Identity) Identity {
	if i.Persona == "" {
		i.Persona = NonePersona
	}
	return i
}

// LegacyAlias derives an alias entry for a pre-convention actor string using
// ATM's own generated-default patterns. Returns ok=false for strings that
// already use the convention (contain '@').
func LegacyAlias(raw string) (AliasEntry, bool) {
	if strings.Contains(raw, "@") {
		return AliasEntry{}, false
	}
	if raw == "default" {
		return AliasEntry{Persona: "developer"}, true
	}
	if raw == "atm-manager" {
		return AliasEntry{Persona: "manager"}, true
	}
	for _, a := range Agents {
		switch raw {
		case a:
			return AliasEntry{Persona: "developer", Agent: a}, true
		case a + "-dev":
			return AliasEntry{Persona: "developer", Agent: a}, true
		case a + "-manager", a + "-onboard":
			return AliasEntry{Persona: "manager", Agent: a}, true
		}
	}
	// Anything else without '@': default working persona, unknown agent.
	return AliasEntry{Persona: "developer"}, true
}
