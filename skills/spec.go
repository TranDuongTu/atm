// Package skills hosts ATM's built-in prompt surface: persona prompt files
// under skills/persona, embedded into the binary, plus the parser that
// enforces their format. Pure leaf — it imports nothing from this
// repository, so every layer (cli, store, capabilities) may depend on it.
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
	// ProjectOptional personas may launch without --project (admin: the
	// human operator may be setting up their first store).
	ProjectOptional bool
	Body            string // full markdown body (after frontmatter)
	CorePrompt      string // Body minus `## Personality` section
	Personality     string // default `## Personality` section body, "" if none
}