package contextmap

import _ "embed"

//go:embed guide.md
var guideText string

// Summary is the capability's one-line description, used wherever
// capabilities are enumerated (conventions, manager prompt).
func (Cap) Summary() string {
	return "Context pointers with provenance — record what knowledge derives from, detect drift."
}

// Guide is the capability's full agent-facing semantics; `atm context guide`
// prints it. Its Manager-duty section is the mapping procedure the manager
// prompt used to hardcode — the prompt now points here.
func (Cap) Guide() string { return guideText }
