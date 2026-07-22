package session

import (
	_ "embed"
	"fmt"
	"strings"

	"atm/skills"
)

//go:embed context_v1.md
var contextV1 string

type ContextData struct {
	Code  string
	Name  string
	Actor string
	// Spec is the resolved persona (built-in from skills, or a custom persona
	// parsed into the same shape by the CLI).
	Spec skills.PersonaSpec
	// Personality overrides the spec's default personality section ("" keeps
	// the default).
	Personality string
	// Mode selects one declared mode; "" renders no mode block.
	Mode string
	// Capability scopes the session to one enabled capability ("" = all).
	Capability string
}

// RenderContext substitutes ContextData into the session template. Empty
// Code/Name/Actor leave their placeholders literal so a generic template can
// be produced (`atm session-context` with no --project).
func RenderContext(d ContextData) string {
	personality := d.Spec.Personality
	if d.Personality != "" {
		personality = d.Personality
	}
	var pb strings.Builder
	fmt.Fprintf(&pb, "## Persona: %s\n\n%s\n\n%s\n", d.Spec.Name, d.Spec.Description, d.Spec.CorePrompt)
	if personality != "" {
		fmt.Fprintf(&pb, "\n### Personality\n\n%s\n", personality)
	}
	pb.WriteString("\nYou are operating as this persona. Hold to its principles throughout the session, alongside repo instructions and the working routine below.\n")

	modeBlock := ""
	if d.Mode != "" {
		if m, ok := d.Spec.Mode(d.Mode); ok {
			var mb strings.Builder
			fmt.Fprintf(&mb, "## Mode: %s\n\n%s\n", m.Name, m.Instructions)
			if d.Capability != "" {
				fmt.Fprintf(&mb, "\nScope: limit this session to the `%s` capability.\n", d.Capability)
			}
			modeBlock = mb.String()
		}
	}

	pairs := []string{
		"<CODE>", d.Code,
		"<PROJECT_NAME>", d.Name,
		"<ACTOR>", d.Actor,
		"<PERSONA_BLOCK>", pb.String(),
		"<MODE_BLOCK>", modeBlock,
	}
	final := make([]string, 0, len(pairs))
	for i := 0; i < len(pairs); i += 2 {
		key, val := pairs[i], pairs[i+1]
		if val == "" && key != "<PERSONA_BLOCK>" && key != "<MODE_BLOCK>" {
			final = append(final, key, key) // keep placeholder literal
		} else {
			final = append(final, key, val)
		}
	}
	return strings.NewReplacer(final...).Replace(contextV1)
}
