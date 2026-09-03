package core

import (
	"encoding/json"
	"fmt"
	"strings"
)

// PersonaMetaKey is the Task.Meta key a persona record owns.
const PersonaMetaKey = "persona"

// PersonaLabel is the stored label a persona task carries: bare and
// name-keyed, exactly like a checklist — the record's identity lives in the
// payload and the title, not in the label.
func PersonaLabel(code string) string { return code + ":persona" }

// A persona record is a PROJECT record, not machine-global state. Two
// projects may run the same-named persona from different profiles, and each
// project's operating model has to be self-contained — otherwise applying a
// profile in one project silently rewrites another's identity.
//
// The PROMPT is stored as the task's description rather than in the payload.
// It is the document a reader actually wants: `atm search` indexes task
// descriptions, so a persona's principles are findable, and the TUI's task
// detail shows it without a special case. The payload carries only what the
// description cannot say about itself — the one-line summary and the
// provenance. Channels store their purpose the same way.
//
// Launch, kickoff, and project_optional are deliberately absent: autonomy
// moved onto the checklist as the mode axis, so identity carries no
// dispatch plumbing.

// PersonaPayloadFrom builds the payload map for a persona record.
func PersonaPayloadFrom(p Persona) map[string]any {
	m := map[string]any{"name": p.Name}
	if p.Description != "" {
		m["description"] = p.Description
	}
	if p.Origin != "" {
		m["origin"] = p.Origin
	}
	return m
}

// DecodePersonaPayload parses a payload string; "" is a valid empty
// payload. A malformed payload is an ERROR — verbs fail rather than
// overwrite state they cannot read, the same doctrine as every capability
// payload.
func DecodePersonaPayload(s string) (map[string]any, error) {
	if s == "" {
		return map[string]any{}, nil
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		return nil, fmt.Errorf("%s payload is not a JSON object (hand-repair needed): %w", PersonaMetaKey, err)
	}
	return m, nil
}

// EncodePersonaPayload serializes, stamping v:1. Unknown fields survive
// because the map is the source of truth; the argument is copied, never
// mutated.
func EncodePersonaPayload(m map[string]any) (string, error) {
	out := make(map[string]any, len(m)+1)
	for k, v := range m {
		if k != "v" {
			out[k] = v
		}
	}
	if len(out) == 0 {
		return "", nil
	}
	out["v"] = 1
	b, err := json.Marshal(out)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// PersonaFromTask decodes a persona task. (nil, nil) when the task carries
// no persona label; an error when it does but the payload is unreadable.
func PersonaFromTask(code string, t Task) (*Persona, error) {
	label := PersonaLabel(code)
	found := false
	for _, l := range t.Labels {
		if l == label {
			found = true
			break
		}
	}
	if !found {
		return nil, nil
	}
	m, err := DecodePersonaPayload(t.Meta[PersonaMetaKey])
	if err != nil {
		return nil, fmt.Errorf("task %s: %w", t.ID, err)
	}
	p := &Persona{
		TaskID:      t.ID,
		Name:        personaStr(m["name"]),
		Description: personaStr(m["description"]),
		Prompt:      strings.TrimSpace(t.Description),
		Origin:      personaStr(m["origin"]),
	}
	if p.Name == "" {
		p.Name = t.Title
	}
	if p.Origin == "" {
		p.Origin = "user"
	}
	return p, nil
}

func personaStr(v any) string { s, _ := v.(string); return s }
