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
	// Launch selects how the session starts: "prompt" (default — an initial
	// message points at the rendered context file), "hook" (a session-start
	// plugin hook loads it; the agent starts idle), or "tui" (routes to the
	// interactive TUI; no host argv or context file is built).
	Launch string
	// Kickoff is the initial-message template for eager (launch: prompt)
	// sessions; "" means the generic PromptMessage. Placeholders:
	// <CONTEXT_FILE>, <CODE>, <TASK_ID>.
	Kickoff string
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

// SeedStep is one node of a seed checklist's recursive step tree; markdown
// nested lists are the textual form.
type SeedStep struct {
	Text     string
	Children []SeedStep
}

// SeedRequires declares what a seed checklist needs to be runnable.
type SeedRequires struct {
	Capabilities []string
	Channels     []string
}

// ChecklistSeed is one shipped starter checklist a project's `seed` verb
// creates on demand: name-keyed, with purpose, recursive steps, suits
// (default-bind persona hints), requires, and origin (reset provenance).
// <CODE> placeholders are substituted with the project code by the seeder.
// Origin is "" when the file names none; the embed loader defaults it to
// "shipped:atm".
type ChecklistSeed struct {
	Name     string
	Purpose  string
	Suits    []string
	Requires SeedRequires
	Origin   string
	Steps    []SeedStep
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
