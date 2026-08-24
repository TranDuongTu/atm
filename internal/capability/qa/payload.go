package qa

import (
	"encoding/json"
	"fmt"
)

// Payload wraps the capability's JSON object under Meta[CapabilityName].
// The source of truth is a generic map so UNKNOWN FIELDS SURVIVE every
// read-modify-write: an older binary must never destroy a newer binary's
// state. Typed accessors read and write only the fields this version owns:
// part_of (on a scaffold), scaffolds (on an original), covered_by.
//
// Both ends of the scaffold edge are stored here, and deliberately so: unlike
// scrum's parent/child, the original's roster IS the thing `pass` checks, and
// checking it must not depend on a project scan finding every scaffold.
type Payload struct {
	raw map[string]any
}

// DecodePayload parses a payload string; "" is a valid empty payload. A
// malformed payload is an ERROR — verbs fail rather than overwrite state they
// cannot read; only Annotate degrades silently.
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

// Encode serializes the payload, stamping the version. A payload with no field
// left besides the version encodes to "" — writing "" deletes the key, so
// presence of the key always means presence of state.
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

// PartOf returns the original this scaffold verifies, or "" on an original.
// Its presence is what MAKES a task a scaffold.
func (p *Payload) PartOf() string { return str(p.raw["part_of"]) }

func (p *Payload) SetPartOf(id string) { p.raw["part_of"] = id }

func (p *Payload) ClearPartOf() { delete(p.raw, "part_of") }

// Scaffolds returns the original's scaffold roster (order preserved).
func (p *Payload) Scaffolds() []string { return stringList(p.raw["scaffolds"]) }

// AddScaffold appends id, deduplicated; reports whether it was added.
func (p *Payload) AddScaffold(id string) bool {
	cur := p.Scaffolds()
	if containsString(cur, id) {
		return false
	}
	setStringList(p.raw, "scaffolds", append(cur, id))
	return true
}

// RemoveScaffold removes id; reports whether it was present.
func (p *Payload) RemoveScaffold(id string) bool {
	cur := p.Scaffolds()
	if !containsString(cur, id) {
		return false
	}
	next := make([]string, 0, len(cur))
	for _, x := range cur {
		if x != id {
			next = append(next, x)
		}
	}
	setStringList(p.raw, "scaffolds", next)
	return true
}

// CoveredBy returns the task whose verification covered this one, or "".
func (p *Payload) CoveredBy() string { return str(p.raw["covered_by"]) }

func (p *Payload) SetCoveredBy(id string) { p.raw["covered_by"] = id }

func (p *Payload) ClearCoveredBy() { delete(p.raw, "covered_by") }
