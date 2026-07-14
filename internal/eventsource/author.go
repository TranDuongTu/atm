package eventsource

import (
	"encoding/json"
	"fmt"
	"slices"
	"time"
)

// Draft is the caller-supplied part of a new event. Payload values must
// marshal to JSON; the keys the fold reads per action are defined by
// writesOf in fold.go.
type Draft struct {
	At      time.Time
	Actor   string
	Action  string
	Subject Subject
	Payload map[string]any
}

// NewEvent authors a v2 event: it ticks the clock, sorts and dedupes
// parents (array order must not fork identities, spec decision 12), and
// derives the identity by funnelling through Parse — one code path for
// every id in the system. replica must be a minted id; ReplicaV1 is
// reserved for the D6 upgrade.
func NewEvent(clock *Clock, replica string, parents []string, d Draft) (*Event, error) {
	if replica == "" || replica == ReplicaV1 {
		return nil, fmt.Errorf("eventsource: invalid replica id %q", replica)
	}
	if d.Action == "" || d.Subject.Kind == "" {
		return nil, fmt.Errorf("eventsource: action and subject.kind are required")
	}
	ps := slices.Clone(parents)
	slices.Sort(ps)
	ps = slices.Compact(ps)
	if ps == nil {
		ps = []string{}
	}
	at := d.At
	if at.IsZero() {
		at = time.Now()
	}
	obj := map[string]any{
		"v":       2,
		"parents": ps,
		"hlc":     clock.Tick(),
		"replica": replica,
		"at":      at.UTC().Format(time.RFC3339Nano),
		"actor":   d.Actor,
		"action":  d.Action,
		"subject": d.Subject,
	}
	if len(d.Payload) > 0 {
		obj["payload"] = d.Payload
	}
	raw, err := json.Marshal(obj)
	if err != nil {
		return nil, fmt.Errorf("eventsource: marshal draft: %w", err)
	}
	return Parse(raw)
}
