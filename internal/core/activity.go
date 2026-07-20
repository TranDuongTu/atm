package core

import (
	"encoding/json"
	"time"
)

type LogEntry struct {
	Seq     int             `json:"seq"`
	At      time.Time       `json:"at"`
	Actor   string          `json:"actor"`
	Action  string          `json:"action"`
	Subject Subject         `json:"subject"`
	Payload json.RawMessage `json:"payload,omitempty"`
	// ID is the v2 content-addressed event id ("sha256:…"); empty for v1
	// entries. Parents is the event's causal frontier (event ids); nil for
	// v1 entries. Both exist for DAG-aware display (the TUI events feed) —
	// the fold order in Seq remains the only ordering authority.
	ID      string   `json:"id,omitempty"`
	Parents []string `json:"parents,omitempty"`
}

type Subject struct {
	Kind string `json:"kind"`
	ID   string `json:"id,omitempty"`
	Code string `json:"code,omitempty"`
	Name string `json:"name,omitempty"`
}

type HistoryView struct {
	Seq    int       `json:"seq"`
	Action string    `json:"action"`
	Actor  string    `json:"actor"`
	At     time.Time `json:"at"`
}
