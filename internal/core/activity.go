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
