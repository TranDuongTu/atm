package release

import (
	"encoding/json"
	"fmt"
)

// Payload wraps the capability's JSON object under Meta[CapabilityName].
// The source of truth is a generic map so UNKNOWN FIELDS SURVIVE every
// read-modify-write. Typed accessors read and write only the fields this
// version owns: version and members on a CONTAINER, release_of on a MEMBER.
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
// left besides the version key encodes to "" — writing "" deletes the key.
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

// Version returns the container's sanitized version value, "" when this task
// is not a container.
func (p *Payload) Version() string { return str(p.raw["version"]) }

func (p *Payload) SetVersion(v string) { p.raw["version"] = v }

// Members returns the container's roster (order preserved).
func (p *Payload) Members() []string { return stringList(p.raw["members"]) }

// AddMember appends id, deduplicated; reports whether it was added.
func (p *Payload) AddMember(id string) bool {
	cur := p.Members()
	if containsString(cur, id) {
		return false
	}
	setStringList(p.raw, "members", append(cur, id))
	return true
}

// RemoveMember removes id; reports whether it was present.
func (p *Payload) RemoveMember(id string) bool {
	cur := p.Members()
	if !containsString(cur, id) {
		return false
	}
	next := make([]string, 0, len(cur))
	for _, x := range cur {
		if x != id {
			next = append(next, x)
		}
	}
	setStringList(p.raw, "members", next)
	return true
}

// ReleaseOf returns the container this member belongs to, "" when this task is
// not a member. Its presence is what MAKES a task a member.
func (p *Payload) ReleaseOf() string { return str(p.raw["release_of"]) }

func (p *Payload) SetReleaseOf(id string) { p.raw["release_of"] = id }

func (p *Payload) ClearReleaseOf() { delete(p.raw, "release_of") }
