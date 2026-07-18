package manager

import (
	_ "embed"
	"fmt"
	"strings"
)

//go:embed context_v1.md
var contextV1 string

// CapabilityAction is a manager action contributed by an enabled capability
// (e.g. contextmap's "mapping"). internal/manager stays decoupled from
// internal/capability: callers pass plain data.
type CapabilityAction struct {
	Name    string // action name, e.g. "mapping"
	Summary string
	Command string // capability command for the consult pointer, e.g. "context"
}

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

	// CapabilityActions are the manager actions the project's enabled
	// capabilities contribute; rendered as additional Roles bullets that
	// point at each capability's guide. The procedures live in the guides.
	CapabilityActions []CapabilityAction
	// ActionConsult is the capability command to consult when Action is a
	// capability-contributed action ("" for the core curate/recall).
	ActionConsult string
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
		actionBlock = fmt.Sprintf("## Current manager action\n\nFocus this session on **%s**. Use the matching responsibility below as the primary goal, while still preserving ledger correctness.\n",
			data.Action)
		if data.ActionConsult != "" {
			actionBlock += fmt.Sprintf("This action is capability-contributed: run `%s %s guide` and follow its \"Manager duty\" section.\n",
				binOr(data.ATMBin), data.ActionConsult)
		}
	}
	rolesBlock := ""
	for _, a := range data.CapabilityActions {
		rolesBlock += fmt.Sprintf("- **%s** — %s. Capability-contributed action: run `%s %s guide` and follow its \"Manager duty\" section for the operating procedure.\n",
			titleWord(a.Name), a.Summary, binOr(data.ATMBin), a.Command)
	}
	// Build a replacer that substitutes non-empty values. Empty values are
	// replaced with the placeholder itself so it survives (a generic, unrendered
	// template can still be produced by `atm manage-context` with no --project).
	// <PERSONA_BLOCK>, <ACTION_BLOCK>, and <CAPABILITY_ROLES> are exceptions:
	// when absent, the blocks are genuinely omitted, so they substitute with ""
	// (no placeholders survive).
	pairs := []string{
		"<CODE>", data.Code,
		"<PROJECT_NAME>", data.Name,
		"<ATM_BIN>", data.ATMBin,
		"<ACTOR>", data.Actor,
		"<RUN_ID>", data.RunID,
		"<TIMESTAMP>", data.Timestamp,
		"<PERSONA_BLOCK>", personaBlock,
		"<ACTION_BLOCK>", actionBlock,
		"<CAPABILITY_ROLES>", rolesBlock,
	}
	final := make([]string, 0, len(pairs))
	for i := 0; i < len(pairs); i += 2 {
		key, val := pairs[i], pairs[i+1]
		if val == "" && key != "<PERSONA_BLOCK>" && key != "<ACTION_BLOCK>" && key != "<CAPABILITY_ROLES>" {
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

// titleWord upper-cases the first byte of an ASCII action name for the role
// bullet ("mapping" -> "Mapping"). Not strings.Title (deprecated).
func titleWord(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
