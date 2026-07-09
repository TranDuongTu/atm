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
}

// RenderContext substitutes the ContextData placeholders into the manager
// prompt template. Empty fields leave their placeholder in place so a generic,
// unrendered template can still be produced (e.g. `atm manager render-context`
// with no --project). The installed atm-manager subagent is a thin pointer that
// calls `atm manager render-context` at dispatch; it is NOT produced from this
// render.
func RenderContext(data ContextData) string {
	personaBlock := ""
	if data.Persona != "" {
		personaBlock = fmt.Sprintf("## Persona: %s\n\n%s\n\n%s\n\nYou are operating as this persona. Hold to its principles throughout the session, alongside the responsibilities below.\n",
			data.Persona, data.PersonaDescription, data.PersonaPrompt)
	}
	// Build a replacer that substitutes non-empty values. Empty values are
	// replaced with the placeholder itself so it survives (a generic, unrendered
	// template can still be produced by `atm manager render-context` with no
	// --project). <PERSONA_BLOCK> is the exception: when there is no persona,
	// the block is genuinely absent, so it substitutes with "" (no placeholder
	// survives).
	pairs := []string{
		"<CODE>", data.Code,
		"<PROJECT_NAME>", data.Name,
		"<ATM_BIN>", data.ATMBin,
		"<ACTOR>", data.Actor,
		"<RUN_ID>", data.RunID,
		"<TIMESTAMP>", data.Timestamp,
		"<PERSONA_BLOCK>", personaBlock,
	}
	final := make([]string, 0, len(pairs))
	for i := 0; i < len(pairs); i += 2 {
		key, val := pairs[i], pairs[i+1]
		if val == "" && key != "<PERSONA_BLOCK>" {
			final = append(final, key, key)
		} else {
			final = append(final, key, val)
		}
	}
	return strings.NewReplacer(final...).Replace(contextV1)
}
