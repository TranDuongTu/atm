package workflow

import _ "embed"

//go:embed guide.md
var guideText string

// Summary is the capability's one-line description, used wherever
// capabilities are enumerated (conventions, manager prompt).
func (Cap) Summary() string {
	return "Status-transition verbs and boards — the paved road for task status."
}

// Guide is the capability's full agent-facing semantics; `atm workflow guide`
// prints it. The capability explains itself: this text is the single source,
// composed surfaces (conventions, manager prompt) only point here.
func (Cap) Guide() string { return guideText }
