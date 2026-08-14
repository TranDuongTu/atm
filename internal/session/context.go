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
	Capability    string
	PersonaPrompt string
}

// capabilityScopeSection is the Session scope block rendered when a session
// launches with --capability. It is capability-agnostic; capability-specific
// flow lives in that capability's guide. <CODE> is substituted (or left as
// the literal placeholder, like the rest of the template) by RenderContext's
// main replacer.
func capabilityScopeSection(capability string) string {
	return "## Session scope\n\nThis session is scoped to the `" + capability + "` capability. Skip any general onboarding or survey flow your persona defines — orient only as far as this capability needs. Read `atm capability " + capability + " guide` first, then work solely on this capability's setup and health for project <CODE>, and hand off when it is healthy."
}

// RenderContext substitutes ContextData into the session template. Empty
// Code/Name/Actor leave their placeholders literal so a generic template can
// be produced (`atm session-context` with no --project).
func RenderContext(d ContextData) string {
	tmpl := contextV1
	if d.Capability == "" {
		tmpl = strings.Replace(tmpl, "<CAPABILITY_SCOPE>\n\n", "", 1)
	} else {
		tmpl = strings.Replace(tmpl, "<CAPABILITY_SCOPE>", capabilityScopeSection(d.Capability), 1)
	}
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
	return strings.NewReplacer(final...).Replace(tmpl)
}
