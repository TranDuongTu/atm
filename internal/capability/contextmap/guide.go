package contextmap

import (
	_ "embed"
)

//go:embed guide.md
var guideText string

// Summary is the capability's one-line description, used wherever
// capabilities are enumerated (conventions, manager prompt).
func (Cap) Summary() string {
	return "Context pointers with provenance — record what knowledge derives from, detect drift."
}

// Guide is the capability's full agent-facing semantics; `atm context guide`
// prints it. The manager prompt points here for the mapping procedure.
func (Cap) Guide() string { return guideText }
