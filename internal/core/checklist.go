package core

import (
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// ChecklistMetaKey is the Task.Meta key the checklist capability owns.
const ChecklistMetaKey = "checklist"

// ChecklistLabel is the stored label a v2 checklist task carries: bare and
// name-keyed — the record's identity lives in the payload, not the label.
func ChecklistLabel(code string) string { return code + ":checklist" }

// ChecklistPersonaLabelPrefix is the v1 label prefix (<code>:checklist:<persona>).
// v1 records stay readable under it and are relabelled on first edit.
func ChecklistPersonaLabelPrefix(code string) string { return code + ":checklist:" }

// ChecklistStep is one node of the recursive step tree.
type ChecklistStep struct {
	Text     string          `json:"text"`
	Children []ChecklistStep `json:"children,omitempty"`
}

// ChecklistRequires declares what a checklist needs to be runnable. Unmet
// requirements warn, never block (spec: DispatchV2 decision 4).
type ChecklistRequires struct {
	Capabilities []string `json:"capabilities,omitempty"`
	Channels     []string `json:"channels,omitempty"`
}

// ChecklistRecord is the v2 ledger record decoded from a checklist task.
// Name is the unique key within a project; Suits is a default-bind hint, not
// ownership; Origin is reset provenance (user | shipped:atm | shipped:<cap>).
// The json tags are the agent endpoint contract of `atm checklist --output json`.
type ChecklistRecord struct {
	TaskID   string            `json:"task_id"`
	Name     string            `json:"name"`
	Purpose  string            `json:"purpose,omitempty"`
	Steps    []ChecklistStep   `json:"steps"`
	Suits    []string          `json:"suits,omitempty"`
	Requires ChecklistRequires `json:"requires,omitzero"`
	Origin   string            `json:"origin"`
}

var checklistOriginRe = regexp.MustCompile(`^shipped:[a-z0-9]([a-z0-9_-]*[a-z0-9])?$`)

// ValidChecklistOrigin reports whether origin is a legal provenance value.
func ValidChecklistOrigin(o string) bool {
	return o == "user" || o == "shipped:atm" || checklistOriginRe.MatchString(o)
}

// DefaultChecklistSet narrows a session's suited checklists by capability
// scope: a scoped session keeps the checklists whose required capabilities
// are empty (capability-neutral) or include the scope; an unscoped session
// (capability "") keeps them all. Order is preserved; the input is copied,
// never aliased.
func DefaultChecklistSet(recs []ChecklistRecord, capability string) []ChecklistRecord {
	if recs == nil {
		return nil
	}
	out := make([]ChecklistRecord, 0, len(recs))
	for _, r := range recs {
		if capability == "" || len(r.Requires.Capabilities) == 0 || slices.Contains(r.Requires.Capabilities, capability) {
			out = append(out, r)
		}
	}
	return out
}

// ChecklistRequireWarnings evaluates one checklist's requires against the
// project's enabled capabilities and channels (matched by NAME; a channel
// that exists but has no wiring is unmet too). Unmet requirements WARN,
// never block (DispatchV2 decision 4). Order: capabilities, then channels,
// each in declaration order. Nil when everything is satisfied.
func ChecklistRequireWarnings(rec ChecklistRecord, enabled []string, channels []ChannelView) []string {
	var out []string
	for _, c := range rec.Requires.Capabilities {
		if !slices.Contains(enabled, c) {
			out = append(out, fmt.Sprintf("checklist %s: requires capability %s (not enabled)", rec.Name, c))
		}
	}
	for _, ch := range rec.Requires.Channels {
		i := slices.IndexFunc(channels, func(v ChannelView) bool { return v.Name == ch })
		switch {
		case i < 0:
			out = append(out, fmt.Sprintf("checklist %s: requires channel %s (none exists)", rec.Name, ch))
		case channels[i].Wiring == nil:
			out = append(out, fmt.Sprintf("checklist %s: requires channel %s (unwired)", rec.Name, ch))
		}
	}
	return out
}

// RenderChecklistSteps renders the recursive step tree as the numbered
// nested list every surface shares (context file v2, TUI detail, profile
// markdown): top level "1.", children "1.1", "1.2.1", indented three
// spaces per depth.
func RenderChecklistSteps(steps []ChecklistStep) string {
	var b strings.Builder
	renderSteps(&b, steps, "", 0)
	return b.String()
}

func renderSteps(b *strings.Builder, steps []ChecklistStep, prefix string, depth int) {
	for i, s := range steps {
		n := prefix + strconv.Itoa(i+1)
		dot := ""
		if depth == 0 {
			dot = "."
		}
		fmt.Fprintf(b, "%s%s%s %s\n", strings.Repeat("   ", depth), n, dot, s.Text)
		renderSteps(b, s.Children, n+".", depth+1)
	}
}

// ChecklistStepCount is the total node count of the tree — the number every
// surface that says "N steps" uses.
func ChecklistStepCount(steps []ChecklistStep) int {
	n := 0
	for _, s := range steps {
		n += 1 + ChecklistStepCount(s.Children)
	}
	return n
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

// EncodeChecklistPayload serializes, stamping v:2. Unknown fields survive
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
	out["v"] = 2
	b, err := json.Marshal(out)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func checklistStepsToAny(steps []ChecklistStep) []any {
	out := make([]any, len(steps))
	for i, s := range steps {
		m := map[string]any{"text": s.Text}
		if len(s.Children) > 0 {
			m["children"] = checklistStepsToAny(s.Children)
		}
		out[i] = m
	}
	return out
}

// checklistStepsFromAny tolerates BOTH payload generations per element: a
// plain string (v1 flat step) becomes a leaf; a map decodes text + children.
func checklistStepsFromAny(v any) []ChecklistStep {
	raw, ok := v.([]any)
	if !ok {
		return nil
	}
	var out []ChecklistStep
	for _, e := range raw {
		switch n := e.(type) {
		case string:
			out = append(out, ChecklistStep{Text: n})
		case map[string]any:
			out = append(out, ChecklistStep{Text: checklistStr(n["text"]), Children: checklistStepsFromAny(n["children"])})
		}
	}
	return out
}

func checklistStrsToAny(ss []string) []any {
	out := make([]any, len(ss))
	for i, s := range ss {
		out[i] = s
	}
	return out
}

func checklistStrsFromAny(v any) []string {
	raw, ok := v.([]any)
	if !ok {
		return nil
	}
	var out []string
	for _, e := range raw {
		if s, ok := e.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// ChecklistPayloadFrom builds the v2 payload map for a record.
func ChecklistPayloadFrom(rec ChecklistRecord) map[string]any {
	m := map[string]any{"name": rec.Name, "origin": rec.Origin}
	if rec.Purpose != "" {
		m["purpose"] = rec.Purpose
	}
	if len(rec.Steps) > 0 {
		m["steps"] = checklistStepsToAny(rec.Steps)
	}
	if len(rec.Suits) > 0 {
		m["suits"] = checklistStrsToAny(rec.Suits)
	}
	if len(rec.Requires.Capabilities) > 0 || len(rec.Requires.Channels) > 0 {
		req := map[string]any{}
		if len(rec.Requires.Capabilities) > 0 {
			req["capabilities"] = checklistStrsToAny(rec.Requires.Capabilities)
		}
		if len(rec.Requires.Channels) > 0 {
			req["channels"] = checklistStrsToAny(rec.Requires.Channels)
		}
		m["requires"] = req
	}
	return m
}

func checklistStr(v any) string { s, _ := v.(string); return s }

// ChecklistFromTask decodes a checklist task of either generation. (nil, nil)
// when the task carries no checklist label; an error when it does but the
// payload is unreadable.
func ChecklistFromTask(code string, t Task) (*ChecklistRecord, error) {
	bare := ChecklistLabel(code)
	prefix := ChecklistPersonaLabelPrefix(code)
	isV2Label, persona := false, ""
	for _, l := range t.Labels {
		if l == bare {
			isV2Label = true
			break
		}
		if strings.HasPrefix(l, prefix) {
			persona = strings.TrimPrefix(l, prefix)
			break
		}
	}
	if !isV2Label && persona == "" {
		return nil, nil
	}
	m, err := DecodeChecklistPayload(t.Meta[ChecklistMetaKey])
	if err != nil {
		return nil, fmt.Errorf("task %s: %w", t.ID, err)
	}
	rec := &ChecklistRecord{TaskID: t.ID, Name: checklistStr(m["name"]), Purpose: checklistStr(m["purpose"])}
	if v, _ := m["v"].(float64); v == 2 {
		if rec.Name == "" {
			rec.Name = t.Title
		}
		rec.Steps = checklistStepsFromAny(m["steps"])
		rec.Suits = checklistStrsFromAny(m["suits"])
		if req, ok := m["requires"].(map[string]any); ok {
			rec.Requires = ChecklistRequires{
				Capabilities: checklistStrsFromAny(req["capabilities"]),
				Channels:     checklistStrsFromAny(req["channels"]),
			}
		}
		rec.Origin = checklistStr(m["origin"])
		if rec.Origin == "" {
			rec.Origin = "user"
		}
		return rec, nil
	}
	// v1: persona-keyed record read as v2 (spec §4 migration mapping).
	if persona == "" {
		persona = checklistStr(m["persona"])
	}
	if rec.Name == "" {
		rec.Name = strings.TrimPrefix(t.Title, persona+"/")
	}
	rec.Steps = checklistStepsFromAny(m["steps"])
	if persona != "" {
		rec.Suits = []string{persona}
	}
	rec.Origin = "user"
	return rec, nil
}
