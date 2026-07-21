package workflowai

import (
	_ "embed"
)

//go:embed guide.md
var guideText string

// Summary is the capability's one-line description for enumeration surfaces.
func (Cap) Summary() string {
	return "AI-native task cycle (brainstorm→clarify→plan→ready) with links, plan tracking, and stage boards."
}

// Guide is the capability's full agent-facing semantics; `atm capability
// workflow_ai guide` prints it verbatim. Single source: composed surfaces
// only point here.
func (Cap) Guide() string { return guideText }
