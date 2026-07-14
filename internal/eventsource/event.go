package eventsource

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Subject names the entity an event writes. Creation events carry no ID —
// the entity's identity IS the creation event's id, which cannot appear
// inside itself (spec decision 1). Labels are identified by Name (their
// name is their identity, L1); projects keep Code alongside the identity
// for readability.
type Subject struct {
	Kind string `json:"kind"`
	ID   string `json:"id,omitempty"`
	Name string `json:"name,omitempty"`
	Code string `json:"code,omitempty"`
}

// Event is the v2 envelope (L0). Raw holds the canonical RFC 8785 bytes —
// the source of truth, in which unknown fields survive byte-faithfully
// (L0-2). The struct fields are a read-only decoded view and are never
// re-encoded.
type Event struct {
	ID  string `json:"-"` // "sha256:" + hex SHA-256 of Raw; derived, never serialized
	Raw []byte `json:"-"`

	V       int             `json:"v"`
	Parents []string        `json:"parents"`
	HLC     HLC             `json:"hlc"`
	Replica string          `json:"replica"`
	At      time.Time       `json:"at"`
	Actor   string          `json:"actor"`
	Action  string          `json:"action"`
	Subject Subject         `json:"subject"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

// Parse canonicalizes one serialized v2 event, derives its identity, and
// decodes the known fields. The input may be arbitrarily formatted JSON;
// Raw is always the canonical form, so the id is stable however the event
// was formatted in transit. This is the single identity code path: later
// authoring (NewEvent) funnels through Parse too.
func Parse(line []byte) (*Event, error) {
	canon, err := Canonicalize(line)
	if err != nil {
		return nil, fmt.Errorf("eventsource: canonicalize: %w", err)
	}
	e := &Event{}
	if err := json.Unmarshal(canon, e); err != nil {
		return nil, fmt.Errorf("eventsource: decode: %w", err)
	}
	if e.V != 2 {
		return nil, fmt.Errorf("eventsource: unsupported event version %d", e.V)
	}
	if e.Replica == "" || e.Action == "" || e.Subject.Kind == "" {
		return nil, fmt.Errorf("eventsource: event missing replica, action, or subject.kind")
	}
	if e.Parents == nil {
		return nil, fmt.Errorf("eventsource: event missing parents (a root event carries [])")
	}
	e.Raw = canon
	sum := sha256.Sum256(canon)
	e.ID = "sha256:" + hex.EncodeToString(sum[:])
	return e, nil
}

// payloadFields decodes the payload's top-level keys, leaving values raw.
func (e *Event) payloadFields() map[string]json.RawMessage {
	if len(e.Payload) == 0 {
		return nil
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(e.Payload, &m); err != nil {
		return nil
	}
	return m
}

// PayloadString returns payload[key] when it is a JSON string.
func (e *Event) PayloadString(key string) (string, bool) {
	raw, ok := e.payloadFields()[key]
	if !ok {
		return "", false
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return "", false
	}
	return s, true
}

// PayloadStringOrList returns payload[key] as a list, accepting either a
// single JSON string or an array of strings. The D6 membership delta may
// be either shape; a JSON null yields nil.
func (e *Event) PayloadStringOrList(key string) []string {
	raw, ok := e.payloadFields()[key]
	if !ok {
		return nil
	}
	// A JSON null is the absence of a list, not a one-element list holding
	// the empty string — and encoding/json unmarshals null into a string
	// without error. v1 writes "labels": null for an entity created with no
	// labels, so without this guard the fold would synthesize a phantom
	// membership slot for the "" label on every such entity.
	if string(bytes.TrimSpace(raw)) == "null" {
		return nil
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return []string{s}
	}
	var list []string
	if err := json.Unmarshal(raw, &list); err == nil {
		return list
	}
	return nil
}

// CompareEvents is the deterministic total order over events (L0-4):
// lexicographic on (hlc.p, hlc.l, replica), with the event id as a final
// defensive tiebreak — two _v1 chains from different upgraded projects can
// collide on the triple after a cross-project merge (spec decision 11).
func CompareEvents(a, b *Event) int {
	if c := a.HLC.Compare(b.HLC); c != 0 {
		return c
	}
	if c := strings.Compare(a.Replica, b.Replica); c != 0 {
		return c
	}
	return strings.Compare(a.ID, b.ID)
}
