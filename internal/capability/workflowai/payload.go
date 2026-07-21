package workflowai

import (
	"encoding/json"
	"fmt"
)

// PlanRecord is the typed plan locator stored in the payload: where the
// task's implementation plan lives. Kind is one of the PlanKind constants.
type PlanRecord struct {
	Kind       string
	Ref        string
	RecordedAt string
	Actor      string
}

// Demotion is the breadcrumb of the most recent demote. The full history
// lives in the event log and the task's comment thread; the payload keeps
// only the latest.
type Demotion struct {
	At, By, Reason string
}

// Payload wraps the capability's JSON object under Meta[CapabilityName].
// The source of truth is a generic map so UNKNOWN FIELDS SURVIVE every
// read-modify-write: an older binary must never destroy a newer binary's
// state (degrade-never-reject applied to ourselves). Typed accessors read
// and write only the fields this version owns.
type Payload struct {
	raw map[string]any
}

// DecodePayload parses a payload string; "" is a valid empty payload. A
// malformed payload is an ERROR — verbs fail rather than overwrite state
// they cannot read; only Annotate degrades silently.
func DecodePayload(s string) (*Payload, error) {
	if s == "" {
		return &Payload{raw: map[string]any{}}, nil
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		return nil, fmt.Errorf("%s payload is not a JSON object (hand-repair needed): %w", CapabilityName, err)
	}
	return &Payload{raw: m}, nil
}

// Encode serializes the payload, stamping the version. A payload with no
// field left besides the version encodes to "" — writing "" through
// SetTaskCapabilityMeta deletes the key, so presence of the key always
// means presence of state. json.Marshal of a map sorts keys: output is
// deterministic.
func (p *Payload) Encode() (string, error) {
	rest := 0
	for k := range p.raw {
		if k != "v" {
			rest++
		}
	}
	if rest == 0 {
		return "", nil
	}
	p.raw["v"] = 1
	b, err := json.Marshal(p.raw)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func str(v any) string {
	s, _ := v.(string)
	return s
}

// Plan returns the recorded plan locator, or nil when none is recorded.
func (p *Payload) Plan() *PlanRecord {
	m, ok := p.raw["plan"].(map[string]any)
	if !ok {
		return nil
	}
	return &PlanRecord{Kind: str(m["kind"]), Ref: str(m["ref"]), RecordedAt: str(m["recorded_at"]), Actor: str(m["actor"])}
}

func (p *Payload) SetPlan(r PlanRecord) {
	p.raw["plan"] = map[string]any{"kind": r.Kind, "ref": r.Ref, "recorded_at": r.RecordedAt, "actor": r.Actor}
}

func (p *Payload) ClearPlan() { delete(p.raw, "plan") }

// RevisionOf returns the parent task ID, or "" when this task is not a
// revision follow-up. At most one parent (spec §3).
func (p *Payload) RevisionOf() string { return str(p.raw["revision_of"]) }

func (p *Payload) SetRevisionOf(id string) { p.raw["revision_of"] = id }

func (p *Payload) ClearRevisionOf() { delete(p.raw, "revision_of") }

// RelatesTo returns the generic related-task list (order preserved).
func (p *Payload) RelatesTo() []string {
	arr, ok := p.raw["relates_to"].([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(arr))
	for _, v := range arr {
		out = append(out, str(v))
	}
	return out
}

// AddRelatesTo appends id, deduplicated; reports whether it was added.
func (p *Payload) AddRelatesTo(id string) bool {
	cur := p.RelatesTo()
	if containsString(cur, id) {
		return false
	}
	next := make([]any, 0, len(cur)+1)
	for _, v := range cur {
		next = append(next, v)
	}
	p.raw["relates_to"] = append(next, id)
	return true
}

// RemoveRelatesTo removes id; reports whether it was present. An emptied
// list removes the key entirely (never a stored []).
func (p *Payload) RemoveRelatesTo(id string) bool {
	cur := p.RelatesTo()
	if !containsString(cur, id) {
		return false
	}
	var next []any
	for _, v := range cur {
		if v != id {
			next = append(next, v)
		}
	}
	if len(next) == 0 {
		delete(p.raw, "relates_to")
	} else {
		p.raw["relates_to"] = next
	}
	return true
}

func (p *Payload) SetDemoted(d Demotion) {
	p.raw["demoted"] = map[string]any{"at": d.At, "by": d.By, "reason": d.Reason}
}
