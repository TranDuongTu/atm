package developing

import (
	_ "embed"
	"fmt"
	"strings"
)

//go:embed context_v1.md
var contextV1 string

type ContextData struct {
	Code          string
	Name          string
	Actor         string
	Persona       string
	PersonaPrompt string

	// PersonaDescription is the persona's human-readable description, rendered
	// into the persona block alongside the prompt so the agent sees both.
	PersonaDescription string
}

func RenderContext(data ContextData) string {
	personaBlock := ""
	if data.Persona != "" {
		personaBlock = fmt.Sprintf("## Persona: %s\n\n%s\n\n%s\n\nYou are operating as this persona. Hold to its principles throughout the session, alongside repo instructions and the working routine below.\n",
			data.Persona, data.PersonaDescription, data.PersonaPrompt)
	}
	replacer := strings.NewReplacer(
		"<CODE>", data.Code,
		"<PROJECT_NAME>", data.Name,
		"<ACTOR>", data.Actor,
		"<PERSONA_BLOCK>", personaBlock,
	)
	return replacer.Replace(contextV1)
}