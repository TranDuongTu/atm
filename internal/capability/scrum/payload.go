package scrum

import (
	"encoding/json"
	"fmt"
)

// Payload wraps the capability's JSON object under Meta[CapabilityName].
// The source of truth is a generic map so UNKNOWN FIELDS SURVIVE every
// read-modify-write: an older binary must never destroy a newer binary's
// state. Typed accessors read and write only the fields this version owns:
// part_of, depends_on, covered_by, spec, plan.
//
// Only OUTBOUND facts are stored. Inbound views (an epic's stories, a story's
// tasks, a task's blocked dependents) are derived by the reporter scanning
// project tasks.
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

// stringList reads a JSON array of strings, preserving order. A non-array
// (or absent) value reads as nil — degrade rather than fail on shapes this
// version did not write.
func stringList(v any) []string {
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(arr))
	for _, item := range arr {
		out = append(out, str(item))
	}
	return out
}

// setStringList writes xs, deleting the key when the list empties out
// (never a stored []).
func setStringList(raw map[string]any, key string, xs []string) {
	if len(xs) == 0 {
		delete(raw, key)
		return
	}
	next := make([]any, 0, len(xs))
	for _, x := range xs {
		next = append(next, x)
	}
	raw[key] = next
}

// PartOf returns this unit's parent — a story's epic, a task's story — or ""
// when none is recorded. At most one parent.
func (p *Payload) PartOf() string { return str(p.raw["part_of"]) }

func (p *Payload) SetPartOf(id string) { p.raw["part_of"] = id }

func (p *Payload) ClearPartOf() { delete(p.raw, "part_of") }

// DependsOn returns the outbound execution dependencies (order preserved).
// Blocking is DERIVED from these by the reporter, never stored as a label: a
// unit can be planned and blocked at the same time.
func (p *Payload) DependsOn() []string { return stringList(p.raw["depends_on"]) }

// AddDependsOn appends id, deduplicated; reports whether it was added.
func (p *Payload) AddDependsOn(id string) bool {
	cur := p.DependsOn()
	if containsString(cur, id) {
		return false
	}
	setStringList(p.raw, "depends_on", append(cur, id))
	return true
}

// RemoveDependsOn removes id; reports whether it was present.
func (p *Payload) RemoveDependsOn(id string) bool {
	cur := p.DependsOn()
	if !containsString(cur, id) {
		return false
	}
	next := make([]string, 0, len(cur))
	for _, x := range cur {
		if x != id {
			next = append(next, x)
		}
	}
	setStringList(p.raw, "depends_on", next)
	return true
}

// CoveredBy returns the task that covers this one, or "" when none is
// recorded. Carried by evictions with reason duplicate or covered-by.
func (p *Payload) CoveredBy() string { return str(p.raw["covered_by"]) }

func (p *Payload) SetCoveredBy(id string) { p.raw["covered_by"] = id }

func (p *Payload) ClearCoveredBy() { delete(p.raw, "covered_by") }

// Spec returns the repo-relative spec locator recorded for this unit, or ""
// when none. A locator is a pointer, not content: the reporter checks it
// resolves, nothing here does.
func (p *Payload) Spec() string { return str(p.raw["spec"]) }

func (p *Payload) SetSpec(path string) { p.raw["spec"] = path }

// Plan returns the repo-relative implementation-plan locator, or "" when none.
func (p *Payload) Plan() string { return str(p.raw["plan"]) }

func (p *Payload) SetPlan(path string) { p.raw["plan"] = path }
