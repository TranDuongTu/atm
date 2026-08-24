package codereview

import (
	"encoding/json"
	"fmt"
)

// Payload wraps the capability's JSON object under Meta[CapabilityName].
// The source of truth is a generic map so UNKNOWN FIELDS SURVIVE every
// read-modify-write: an older binary must never destroy a newer binary's
// state. Typed accessors read and write only the fields this version owns:
// pr and report.
//
// Both are LOCATORS, not content. The review conversation lives on the pull
// request and the report lives wherever the reviewer put it; this payload only
// says where to look.
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
// left besides the version encodes to "" — writing "" deletes the key.
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

// PR returns the pull-request locator — a URL or a number, as the team writes
// it. absorb requires one, so a claimed task always has it.
func (p *Payload) PR() string { return str(p.raw["pr"]) }

func (p *Payload) SetPR(ref string) { p.raw["pr"] = ref }

func (p *Payload) ClearPR() { delete(p.raw, "pr") }

// Report returns the review-report locator, or "" when none was recorded.
func (p *Payload) Report() string { return str(p.raw["report"]) }

func (p *Payload) SetReport(loc string) { p.raw["report"] = loc }

func (p *Payload) ClearReport() { delete(p.raw, "report") }
