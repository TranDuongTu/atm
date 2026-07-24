package session

import (
	_ "embed"
	"strings"
)

//go:embed context_v1.md
var contextV1 string

type ContextData struct {
	Code          string
	Name          string
	Actor         string
	TaskID        string
	PersonaPrompt string
}

// RenderContext substitutes ContextData into the session template. Empty
// Code/Name/Actor leave their placeholders literal so a generic template can
// be produced (`atm session-context` with no --project).
func RenderContext(d ContextData) string {
	pairs := []string{
		"<CODE>", d.Code,
		"<PROJECT_NAME>", d.Name,
		"<ACTOR>", d.Actor,
		"<TASK_ID>", d.TaskID,
		"<PERSONA_PROMPT>", d.PersonaPrompt,
	}
	final := make([]string, 0, len(pairs))
	for i := 0; i < len(pairs); i += 2 {
		key, val := pairs[i], pairs[i+1]
		if val == "" {
			final = append(final, key, key)
		} else {
			final = append(final, key, val)
		}
	}
	return strings.NewReplacer(final...).Replace(contextV1)
}
