package manager

import (
	_ "embed"
	"fmt"
	"strings"
)

//go:embed context_v1.md
var contextV1 string

type ContextData struct {
	Code      string
	Name      string
	ATMBin    string
	Actor     string
	RunID     string
	Timestamp string

	// Persona, PersonaPrompt, PersonaDescription describe the persona the
	// manager is operating as. Rendered into a persona block by RenderContext
	// when Persona is non-empty.
	Persona            string
	PersonaPrompt      string
	PersonaDescription string
	Action             string
	// Capability scopes the action to one capability. Empty means "all enabled
	// capabilities"; the action block then reads "each enabled capability".
	Capability string
}

// RenderContext substitutes the ContextData placeholders into the manager
// prompt template. Empty fields leave their placeholder in place so a generic,
// unrendered template can still be produced (e.g. `atm manage-context`
// with no --project). The installed atm-manager subagent is a thin pointer that
// calls `atm manage-context` at dispatch; it is NOT produced from this
// render.
func RenderContext(data ContextData) string {
	personaBlock := ""
	if data.Persona != "" {
		personaBlock = fmt.Sprintf("## Persona: %s\n\n%s\n\n%s\n\nYou are operating as this persona. Hold to its principles throughout the session, alongside the responsibilities below.\n",
			data.Persona, data.PersonaDescription, data.PersonaPrompt)
	}
	actionBlock := ""
	if data.Action != "" {
		bin := binOr(data.ATMBin)
		code := data.Code
		if code == "" {
			code = "<CODE>"
		}
		scope := fmt.Sprintf("each enabled capability (`%s capability list --project %s` enumerates them)", bin, code)
		if data.Capability != "" {
			scope = fmt.Sprintf("the `%s` capability", data.Capability)
		}
		switch data.Action {
		case "brief":
			actionBlock = fmt.Sprintf("## Current manager action\n\nFocus this session on **brief**. For %s, run `%s capability <name> guide` and follow its \"Brief\" section — interview the human to set up that capability's territory.\n", scope, bin)
		case "autopilot":
			actionBlock = fmt.Sprintf("## Current manager action\n\nFocus this session on **autopilot**. For %s, run `%s capability <name> guide` and follow its \"Autopilot\" section — autonomously keep that capability's territory following its guide.\n", scope, bin)
		case "ask":
			actionBlock = fmt.Sprintf("## Current manager action\n\nFocus this session on **ask**. Standby for the human to ask questions; do not act proactively and do not mutate the ledger. Read the guide of %s (`%s capability <name> guide`) to be ready to answer.\n", scope, bin)
		default:
			actionBlock = fmt.Sprintf("## Current manager action\n\nFocus this session on **%s**.\n", data.Action)
		}
	}
	// Build a replacer that substitutes non-empty values. Empty values are
	// replaced with the placeholder itself so it survives (a generic, unrendered
	// template can still be produced by `atm manage-context` with no --project).
	// <PERSONA_BLOCK> and <ACTION_BLOCK> are exceptions: when absent, the blocks
	// are genuinely omitted, so they substitute with "" (no placeholders
	// survive). The action block already embeds concrete bin/code, so it never
	// carries a placeholder the replacer would skip.
	pairs := []string{
		"<CODE>", data.Code,
		"<PROJECT_NAME>", data.Name,
		"<ATM_BIN>", data.ATMBin,
		"<ACTOR>", data.Actor,
		"<RUN_ID>", data.RunID,
		"<TIMESTAMP>", data.Timestamp,
		"<PERSONA_BLOCK>", personaBlock,
		"<ACTION_BLOCK>", actionBlock,
	}
	final := make([]string, 0, len(pairs))
	for i := 0; i < len(pairs); i += 2 {
		key, val := pairs[i], pairs[i+1]
		if val == "" && key != "<PERSONA_BLOCK>" && key != "<ACTION_BLOCK>" {
			final = append(final, key, key)
		} else {
			final = append(final, key, val)
		}
	}
	return strings.NewReplacer(final...).Replace(contextV1)
}

// binOr keeps the <ATM_BIN> placeholder alive in a generic render (no
// project), matching the replacer's convention for empty fields.
func binOr(bin string) string {
	if bin == "" {
		return "<ATM_BIN>"
	}
	return bin
}
