package session

import (
	_ "embed"
	"fmt"
	"strings"
)

//go:embed context_v2.md
var contextV2 string

// noChecklistFallback is what "# What you do" says when a session was
// dispatched without one. Saying so is the point: an empty procedure section
// is indistinguishable from a rendering failure, and a session that cannot
// tell the two apart guesses.
const noChecklistFallback = "No checklists were selected for this session. Fall back to your persona's judgment and the project's conventions."

// ChecklistSection is one dispatch-selected checklist rendered into the
// context file. StepsRendered is the pre-rendered numbered nested list
// (core.RenderChecklistSteps) — this package stays store- and registry-free.
type ChecklistSection struct {
	Name          string
	Purpose       string
	StepsRendered string
}

type ContextData struct {
	Code            string
	Name            string
	Actor           string
	TaskID          string
	Capability      string
	CapabilityNames string // comma-separated enabled set; "" leaves the placeholder literal
	PersonaPrompt   string
	Checklists      []ChecklistSection // in selection order; empty renders the fallback
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
// Code/Name/Actor/TaskID/PersonaPrompt/CapabilityNames leave their
// placeholders literal so a generic template can be produced (`atm
// session-context` with no --project).
//
// Capability differs: an empty Capability REMOVES the Session scope section
// entirely (see capabilityScopeSection), because a scope section naming no
// capability would be meaningless — the other fields' empty case leaves a
// literal placeholder, the capability's does not.
func RenderContext(d ContextData) string {
	tmpl := contextV2
	tmpl = strings.Replace(tmpl, "<CHECKLISTS_SECTIONS>", renderChecklistSections(d.Checklists), 1)
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
		"<CAPABILITY_NAMES>", d.CapabilityNames,
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

// renderChecklistSections renders the dispatch-selected checklists as
// "## Checklist: <name>", italic purpose, numbered step tree. The re-read
// instruction is NOT repeated per section — the template states it once in
// the "What you do" prose, so it costs one line instead of one per checklist.
func renderChecklistSections(cls []ChecklistSection) string {
	if len(cls) == 0 {
		return noChecklistFallback
	}
	var b strings.Builder
	for i, c := range cls {
		if i > 0 {
			b.WriteString("\n")
		}
		fmt.Fprintf(&b, "## Checklist: %s\n\n", c.Name)
		if c.Purpose != "" {
			fmt.Fprintf(&b, "*%s*\n\n", c.Purpose)
		}
		if s := strings.TrimRight(c.StepsRendered, "\n"); s != "" {
			b.WriteString(s + "\n")
		}
	}
	return strings.TrimRight(b.String(), "\n")
}
