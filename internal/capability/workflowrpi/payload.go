package workflowrpi

import (
	"encoding/json"
	"fmt"
)

// Payload wraps the capability's JSON object under Meta[CapabilityName].
// The source of truth is a generic map so UNKNOWN FIELDS SURVIVE every
// read-modify-write: an older binary must never destroy a newer binary's
// state. Typed accessors read and write only the fields this version owns:
// product_of, depends_on, relates_to, covered_by.
//
// Only OUTBOUND facts are stored. Inbound views (a product's pipeline
// children, a task's blocked dependents) are derived by the reporter
// scanning project tasks.
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

// ProductOf returns the pipeline task's product-roadmap parent, or "" when
// none is recorded. At most one parent.
func (p *Payload) ProductOf() string { return str(p.raw["product_of"]) }

func (p *Payload) SetProductOf(id string) { p.raw["product_of"] = id }

func (p *Payload) ClearProductOf() { delete(p.raw, "product_of") }

// DependsOn returns the outbound execution dependencies (order preserved).
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

// RelatesTo returns the generic association list (order preserved). No
// workflow semantics attach to it.
func (p *Payload) RelatesTo() []string { return stringList(p.raw["relates_to"]) }

// AddRelatesTo appends id, deduplicated; reports whether it was added.
func (p *Payload) AddRelatesTo(id string) bool {
	cur := p.RelatesTo()
	if containsString(cur, id) {
		return false
	}
	setStringList(p.raw, "relates_to", append(cur, id))
	return true
}

// RemoveRelatesTo removes id; reports whether it was present.
func (p *Payload) RemoveRelatesTo(id string) bool {
	cur := p.RelatesTo()
	if !containsString(cur, id) {
		return false
	}
	next := make([]string, 0, len(cur))
	for _, x := range cur {
		if x != id {
			next = append(next, x)
		}
	}
	setStringList(p.raw, "relates_to", next)
	return true
}

// CoveredBy returns the task that covers this one, or "" when none is
// recorded. Used mostly by rejected tasks (duplicate, covered-by).
func (p *Payload) CoveredBy() string { return str(p.raw["covered_by"]) }

func (p *Payload) SetCoveredBy(id string) { p.raw["covered_by"] = id }

func (p *Payload) ClearCoveredBy() { delete(p.raw, "covered_by") }
