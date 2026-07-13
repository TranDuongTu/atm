package contextmap

import (
	"encoding/json"
	"fmt"
	"time"
)

// StampVersion is the provenance body schema version. Because the body is
// written and read only by this package, bumping it requires no change to any
// prompt, skill, or agent.
const StampVersion = 1

// Witness is a source plus the evidence recorded for it at stamp time. Value
// is empty when the source is not provable (external) or the agent supplied
// no version token.
type Witness struct {
	Source Source
	Value  string
}

// Stamp is one provenance record: what a pointer was derived from, and the
// evidence for each source, at a moment in time.
type Stamp struct {
	Version   int
	At        time.Time
	Head      string // repo HEAD commit at stamp time; empty when not in a repo
	Witnesses []Witness
}

// wire is the on-disk shape. Kept separate from Stamp so the exported type can
// evolve independently of the serialized format.
type wire struct {
	V       int          `json:"v"`
	At      time.Time    `json:"at"`
	Head    string       `json:"head,omitempty"`
	Sources []wireSource `json:"sources"`
}

type wireSource struct {
	Source  string `json:"source"`            // kinded locator, e.g. "git:internal/store"
	Witness string `json:"witness,omitempty"` // empty for unprovable sources
}

func MarshalStamp(s Stamp) (string, error) {
	w := wire{V: StampVersion, At: s.At.UTC(), Head: s.Head}
	w.Sources = make([]wireSource, 0, len(s.Witnesses))
	for _, wit := range s.Witnesses {
		w.Sources = append(w.Sources, wireSource{Source: wit.Source.String(), Witness: wit.Value})
	}
	b, err := json.MarshalIndent(w, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal provenance: %w", err)
	}
	return string(b), nil
}

func UnmarshalStamp(body string) (Stamp, error) {
	var w wire
	if err := json.Unmarshal([]byte(body), &w); err != nil {
		return Stamp{}, fmt.Errorf("parse provenance: %w", err)
	}
	if w.V != StampVersion {
		return Stamp{}, fmt.Errorf("parse provenance: unsupported version %d (want %d)", w.V, StampVersion)
	}
	s := Stamp{Version: w.V, At: w.At, Head: w.Head}
	s.Witnesses = make([]Witness, 0, len(w.Sources))
	for _, ws := range w.Sources {
		src, err := ParseSource(ws.Source)
		if err != nil {
			return Stamp{}, fmt.Errorf("parse provenance: %w", err)
		}
		s.Witnesses = append(s.Witnesses, Witness{Source: src, Value: ws.Witness})
	}
	return s, nil
}
