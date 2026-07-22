// Package skills hosts ATM's built-in prompt surface: persona and capability
// prompt files under skills/persona and skills/capability, embedded into the
// binary, plus the parser that enforces their format. Pure leaf — it imports
// nothing from this repository, so every layer (cli, store, capabilities) may
// depend on it.
package skills

// Mode is one operating mode a persona declares: the frontmatter summary and
// the matching `## Mode: <name>` body section.
type Mode struct {
	Name         string
	Summary      string // one-line, from frontmatter (CLI help / validation messages)
	Instructions string // full section body, rendered into the session prompt
}

// PersonaSpec is one parsed persona prompt file.
type PersonaSpec struct {
	Name        string
	Description string
	// Launch selects how the host agent receives the context file: "prompt"
	// (default — an initial message points at the rendered context file) or
	// "hook" (a session-start plugin hook loads it; the agent starts idle).
	Launch string
	// DefaultMode is used when --mode is not given. Empty means no mode block.
	DefaultMode string
	// ProjectOptional personas may launch without --project (concierge: the
	// project may not exist yet).
	ProjectOptional bool
	Modes           []Mode // declaration order
	Body            string // full markdown body (after frontmatter)
	CorePrompt      string // Body minus `## Mode:` and `## Personality` sections
	Personality     string // default `## Personality` section body, "" if none
}

// Mode returns the named mode.
func (p PersonaSpec) Mode(name string) (Mode, bool) {
	for _, m := range p.Modes {
		if m.Name == name {
			return m, true
		}
	}
	return Mode{}, false
}

// ModeNames lists declared mode names in declaration order.
func (p PersonaSpec) ModeNames() []string {
	out := make([]string, 0, len(p.Modes))
	for _, m := range p.Modes {
		out = append(out, m.Name)
	}
	return out
}

// CapabilitySpec is one parsed capability prompt file. Labels and Boards are
// the frontmatter declaration of the vocabulary the capability manages; the
// Go package remains the executable source of truth (a per-capability test
// pins the two in sync).
type CapabilitySpec struct {
	Name        string
	Description string
	Labels      []string
	Boards      []string
	Body        string // the full guide served by `atm capability <name> guide`
}
