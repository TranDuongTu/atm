package session

import (
	_ "embed"
	"fmt"
	"strings"
)

//go:embed context_v2.md
var contextV2 string

// ChecklistSection is one dispatch-selected checklist rendered into the
// context file. StepsRendered is the pre-rendered numbered nested list
// (core.RenderChecklistSteps) — this package stays store- and registry-free.
type ChecklistSection struct {
	Name          string
	Purpose       string
	StepsRendered string
}

type ContextData struct {
	Code          string
	Name          string
	Actor         string
	TaskID        string
	Capability    string
	Capabilities  string // pre-rendered ## Capabilities block; "" removes the section (launcher-composed — this package stays registry-free)
	PersonaPrompt string
	Checklists    []ChecklistSection // in selection order; empty removes the sections
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
// Code/Name/Actor/TaskID/PersonaPrompt leave their placeholders literal so a
// generic template can be produced (`atm session-context` with no --project).
// Capability differs: an empty Capability REMOVES the Session scope section
// entirely (see capabilityScopeSection), because a scope section naming no
// capability would be meaningless — the other fields' empty case leaves a
// literal placeholder, the capability's does not.
func RenderContext(d ContextData) string {
	tmpl := contextV2
	if len(d.Checklists) == 0 {
		tmpl = strings.Replace(tmpl, "<CHECKLISTS_SECTIONS>\n\n", "", 1)
	} else {
		var cb strings.Builder
		for _, c := range d.Checklists {
			fmt.Fprintf(&cb, "## Checklist: %s\n\n", c.Name)
			if c.Purpose != "" {
				cb.WriteString(c.Purpose + "\n\n")
			}
			if s := strings.TrimRight(c.StepsRendered, "\n"); s != "" {
				cb.WriteString(s + "\n\n")
			}
			fmt.Fprintf(&cb, "(If told this checklist changed mid-session, re-read it: `atm checklist show --project %s --name %s`.)\n\n", d.Code, c.Name)
		}
		tmpl = strings.Replace(tmpl, "<CHECKLISTS_SECTIONS>\n\n", cb.String(), 1)
	}
	if d.Capability == "" {
		tmpl = strings.Replace(tmpl, "<CAPABILITY_SCOPE>\n\n", "", 1)
	} else {
		tmpl = strings.Replace(tmpl, "<CAPABILITY_SCOPE>", capabilityScopeSection(d.Capability), 1)
	}
	if d.Capabilities == "" {
		tmpl = strings.Replace(tmpl, "<CAPABILITIES>\n\n", "", 1)
	} else {
		tmpl = strings.Replace(tmpl, "<CAPABILITIES>", d.Capabilities, 1)
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
