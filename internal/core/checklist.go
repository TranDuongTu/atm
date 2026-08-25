package core

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ChecklistMetaKey is the Task.Meta key the checklist capability owns.
const ChecklistMetaKey = "checklist"

// ChecklistLabel is the stored label a checklist task for a persona carries.
func ChecklistLabel(code, persona string) string { return code + ":checklist:" + persona }

// ChecklistRecord is the ledger record decoded from a checklist task.
// (persona, name) is the unique key within a project; Purpose is the one-line
// selection surface `list` prints; Steps are the ordered prose imperatives.
// The json tags are the agent endpoint contract of `atm checklist --output json`.
type ChecklistRecord struct {
	TaskID  string   `json:"task_id"`
	Persona string   `json:"persona"`
	Name    string   `json:"name"`
	Purpose string   `json:"purpose,omitempty"`
	Steps   []string `json:"steps"`
}

// DecodeChecklistPayload parses a payload string; "" is a valid empty payload.
// A malformed payload is an ERROR — verbs fail rather than overwrite state
// they cannot read (the same doctrine as every capability payload).
func DecodeChecklistPayload(s string) (map[string]any, error) {
	if s == "" {
		return map[string]any{}, nil
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		return nil, fmt.Errorf("%s payload is not a JSON object (hand-repair needed): %w", ChecklistMetaKey, err)
	}
	return m, nil
}

// EncodeChecklistPayload serializes, stamping v:1. Unknown fields survive
// because the map is the source of truth; the argument is copied, never mutated.
func EncodeChecklistPayload(m map[string]any) (string, error) {
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

// ChecklistPayloadFrom builds the payload map for a record.
func ChecklistPayloadFrom(rec ChecklistRecord) map[string]any {
	m := map[string]any{"persona": rec.Persona, "name": rec.Name}
	if rec.Purpose != "" {
		m["purpose"] = rec.Purpose
	}
	if len(rec.Steps) > 0 {
		steps := make([]any, len(rec.Steps))
		for i, s := range rec.Steps {
			steps[i] = s
		}
		m["steps"] = steps
	}
	return m
}

func checklistStr(v any) string { s, _ := v.(string); return s }

// ChecklistFromTask decodes a checklist task. (nil, nil) when the task
// carries no checklist:<persona> label; an error when it does but the payload
// is unreadable.
func ChecklistFromTask(code string, t Task) (*ChecklistRecord, error) {
	prefix := code + ":checklist:"
	persona := ""
	for _, l := range t.Labels {
		if strings.HasPrefix(l, prefix) {
			persona = strings.TrimPrefix(l, prefix)
			break
		}
	}
	if persona == "" {
		return nil, nil
	}
	m, err := DecodeChecklistPayload(t.Meta[ChecklistMetaKey])
	if err != nil {
		return nil, fmt.Errorf("task %s: %w", t.ID, err)
	}
	rec := &ChecklistRecord{TaskID: t.ID, Persona: persona, Name: checklistStr(m["name"]), Purpose: checklistStr(m["purpose"])}
	if rec.Name == "" {
		rec.Name = strings.TrimPrefix(t.Title, persona+"/")
	}
	if raw, ok := m["steps"].([]any); ok {
		for _, s := range raw {
			rec.Steps = append(rec.Steps, checklistStr(s))
		}
	}
	return rec, nil
}
