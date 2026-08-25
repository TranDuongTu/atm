// Package skills hosts ATM's built-in prompt surface: persona and capability
// prompt files under skills/persona and skills/capability, embedded into the
// binary, plus the parser that enforces their format. Pure leaf — it imports
// nothing from this repository, so every layer (cli, store, capabilities) may
// depend on it.
package skills

// PersonaSpec is one parsed persona prompt file.
type PersonaSpec struct {
	Name        string
	Description string
	// Launch selects how the host agent receives the context file: "prompt"
	// (default — an initial message points at the rendered context file) or
	// "hook" (a session-start plugin hook loads it; the agent starts idle).
	Launch string
	// ProjectOptional personas may launch without --project (concierge: the
	// project may not exist yet).
	ProjectOptional bool
	// Expects lists the required context params this persona expects the
	// session context to provide (CODE, PROJECT_NAME, ACTOR, TASK_ID).
	Expects []string // declaration order
	// Optional lists context params that may or may not be present.
	Optional    []string // declaration order
	Body        string   // full markdown body (after frontmatter)
	CorePrompt  string   // Body minus `## Personality` section
	Personality string   // default `## Personality` section body, "" if none
}

// ChecklistSeed is one shipped starter checklist a project's `seed` verb
// creates on demand: persona-scoped, purpose and ordered steps, with <CODE>
// placeholders the seeder substitutes with the project code.
type ChecklistSeed struct {
	Persona string
	Name    string
	Purpose string
	Steps   []string
}

// CapabilitySpec is one parsed capability prompt file. Labels and Boards are
// the frontmatter declaration of the vocabulary the capability manages; the
// Go package remains the executable source of truth (a per-capability test
// pins the two in sync).
type CapabilitySpec struct {
	Name        string
	Description string
	Brief       string // optional one-line session-injection imperative ("" = fall back to Description)
	Labels      []string
	Boards      []string
	// Duty is the persona targeted by the guide's `## Duty: <persona>`
	// section — the persona that runs this capability's lanes. "" for
	// registry capabilities, whose guides carry no duty section.
	Duty string
	Body string // the full guide served by `atm capability <name> guide`
}
