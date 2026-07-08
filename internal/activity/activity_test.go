package activity

import (
	"testing"

	"atm/internal/actor"
	"atm/internal/store"
)

func entry(a string) store.LogEntry { return store.LogEntry{Actor: a, Action: "task.created"} }

func TestBuildAndAggregateByPersona(t *testing.T) {
	aliases := actor.AliasMap{"opencode-dev": {Persona: "developer", Agent: "opencode"}}
	entries := []store.LogEntry{
		{Actor: "staff@claude:opus-4.8", Action: "task.created"},
		{Actor: "staff@claude:opus-4.8", Action: "comment.created"},
		{Actor: "staff@codex:gpt-5", Action: "task.created"},
		entry("opencode-dev"),
		entry(""), // empty -> (none)
	}
	recs := Build(entries, aliases)
	groups := Aggregate(recs, "persona")

	byKey := map[string]Group{}
	for _, g := range groups {
		byKey[g.Key] = g
	}
	if byKey["staff"].Count != 3 {
		t.Fatalf("staff count = %d, want 3", byKey["staff"].Count)
	}
	if byKey["staff"].Agents["claude"] != 2 || byKey["staff"].Models["gpt-5"] != 1 {
		t.Fatalf("staff breakdown = %+v", byKey["staff"])
	}
	if byKey["developer"].Count != 1 || byKey["developer"].Agents["opencode"] != 1 {
		t.Fatalf("developer = %+v", byKey["developer"])
	}
	if byKey[actor.NonePersona].Count != 1 {
		t.Fatalf("(none) count = %d", byKey[actor.NonePersona].Count)
	}
	// Ordering: staff (3) first.
	if groups[0].Key != "staff" {
		t.Fatalf("ordering: first = %s", groups[0].Key)
	}
}

func TestAggregateByAgentSkipsEmpty(t *testing.T) {
	recs := []Record{
		{Persona: "p", Agent: "claude", Action: "a"},
		{Persona: "p", Agent: "", Action: "a"}, // unknown agent — not its own group
	}
	groups := Aggregate(recs, "agent")
	if len(groups) != 1 || groups[0].Key != "claude" {
		t.Fatalf("agent groups = %+v", groups)
	}
}
