package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbletea"
)

type helpModel struct {
	m      *Model
	lines  []string
	offset int
}

func newHelpModel(m *Model) helpModel {
	return helpModel{m: m}
}

func (h *helpModel) SetSize(w, hh int) {
	_ = w
	_ = hh
}

func (h *helpModel) refresh() {
	var b strings.Builder
	b.WriteString(sectionDivider(h.m.styles, h.m.width, "CLI / TUI Parity"))
	b.WriteString("\n")
	b.WriteString(dashboardBlock(h.m.width, parityTable))
	b.WriteString("\n\n")

	b.WriteString(sectionDivider(h.m.styles, h.m.width, "Global Keymap"))
	b.WriteString("\n")
	b.WriteString(dashboardBlock(h.m.width, keymapTable()))
	b.WriteString("\n\n")

	b.WriteString(sectionDivider(h.m.styles, h.m.width, "Conventions"))
	b.WriteString("\n")
	b.WriteString(renderConventionsText(h.m.styles, h.m.width, conventionsTextTUI))

	h.lines = strings.Split(b.String(), "\n")
	h.clampOffset()
}

func renderConventionsText(styles Styles, width int, text string) string {
	lines := strings.Split(text, "\n")
	var b strings.Builder
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "## ") {
			if i > 0 {
				b.WriteString("\n")
			}
			b.WriteString(sectionDivider(styles, width, strings.TrimPrefix(trimmed, "## ")))
			b.WriteString("\n")
			continue
		}
		switch {
		case strings.HasPrefix(trimmed, "- "):
			b.WriteString(dashboardLine(width, styles.Muted.Render("  • "+strings.TrimPrefix(trimmed, "- "))))
		case strings.HasPrefix(trimmed, "1.") ||
			strings.HasPrefix(trimmed, "2.") ||
			strings.HasPrefix(trimmed, "3.") ||
			strings.HasPrefix(trimmed, "4.") ||
			strings.HasPrefix(trimmed, "5.") ||
			strings.HasPrefix(trimmed, "6."):
			b.WriteString(dashboardLine(width, styles.KeyMenu.Render("  "+trimmed)))
		default:
			b.WriteString(dashboardLine(width, line))
		}
		if i < len(lines)-1 {
			b.WriteString("\n")
		}
	}
	return b.String()
}

func (h *helpModel) clampOffset() {
	maxOff := len(h.lines) - h.m.contentHeight
	if maxOff < 0 {
		maxOff = 0
	}
	if h.offset > maxOff {
		h.offset = maxOff
	}
	if h.offset < 0 {
		h.offset = 0
	}
}

func (h *helpModel) handleKey(k tea.KeyMsg) tea.Cmd {
	switch k.String() {
	case "j", "down":
		h.offset++
		h.clampOffset()
	case "k", "up":
		if h.offset > 0 {
			h.offset--
		}
	case "g":
		h.offset = 0
	case "pgdown", " ":
		h.offset += h.m.contentHeight / 2
		h.clampOffset()
	case "pgup", "b":
		if h.offset > h.m.contentHeight/2 {
			h.offset -= h.m.contentHeight / 2
		} else {
			h.offset = 0
		}
	}
	return nil
}

func (h *helpModel) View() string {
	end := h.offset + h.m.contentHeight
	if end > len(h.lines) {
		end = len(h.lines)
	}
	var b strings.Builder
	for i := h.offset; i < end; i++ {
		b.WriteString(h.lines[i])
		b.WriteString("\n")
	}
	return padToHeight(b.String(), h.m.contentHeight)
}

// parityTable is the verbatim CLI/TUI parity table from mockup Screen 10.
var parityTable = `CLI                                   TUI
─────────────────────────────────────────────────────────────────────────────
atm init                              (auto on first atm tui)
atm store path                        status bar (STORE:)
atm conventions                       help overlay, conventions section
atm project create --code --name      Projects pane  [a]dd
atm project list                      Projects pane  (list)
atm project show --code               Projects pane  [Enter] detail
atm project set-name --code --name    Projects detail  [N]
atm project remove --code             Projects pane  [x]

atm label add --name --desc           Labels pane  [a]dd / [d]esc
atm label remove --name               Labels pane  [l]
atm label seed --project              Labels pane  [S]
atm label list [--project] [--ns]     Labels pane  (list)
atm label show --name                 — (CLI only)

atm task create --project --title [--label]   Tasks pane  [a]dd (labels field)
atm task list [--project] [--label]   Tasks pane  (list; / for filter)
atm task list --facets                Tasks pane  (wildcard filter -> grouped)
atm task show --id                    Tasks pane  [Enter] detail
atm task set-title --id --title       Task detail  [e]
atm task set-description --id --desc  Task detail  [d]
atm task label add --id --label       Task detail  [b]
atm task label remove --id --label    Task detail  [B]
atm task remove --id                  Task detail  [x]

atm tui                                (you are here)`

// keymapTable renders the global keymap summary as a fixed-width table.
func keymapTable() string {
	var b strings.Builder
	widths := []int{10, 18, 21, 19, 21}
	fmt.Fprintf(&b, "%-*s %-*s %-*s %-*s %-*s\n",
		widths[0], "Key",
		widths[1], "Projects",
		widths[2], "Tasks",
		widths[3], "Labels",
		widths[4], "Detail/Overlay",
	)
	b.WriteString(strings.Repeat("-", widths[0]+1+widths[1]+1+widths[2]+1+widths[3]+1+widths[4]))
	b.WriteString("\n")
	for _, r := range keymapRows {
		fmt.Fprintf(&b, "%-*s %-*s %-*s %-*s %-*s\n",
			widths[0], truncateRunes(r.Key, widths[0]),
			widths[1], truncateRunes(r.Projects, widths[1]),
			widths[2], truncateRunes(r.Tasks, widths[2]),
			widths[3], truncateRunes(r.Labels, widths[3]),
			widths[4], truncateRunes(r.Detail, widths[4]),
		)
	}
	return b.String()
}

// conventionsTextTUI mirrors the CLI conventions text (spec §7). Duplicated
// here (rather than imported) to avoid a cli->tui dependency cycle; the spec
// calls the TUI a "second render of the same reference" and this keeps the
// content authoritative in one place per surface.
var conventionsTextTUI = `Version: v2.1

v2's vision is that workflow lives outside the system — in agent prompts and
human habits, not in the store. But a fresh agent landing in a project needs a
deterministic entry point, and a fresh project needs its labels seeded.
Onboarding is the act of seeding index tasks and seed labels; the conventions
below tell humans and agents how to do that using only the label substrate.
No bootstrap command, no reserved labels, no repo-side files, no store-side
repo paths. The conventions are documented, not code-reserved: the system
treats ATM:context:agent identically to ATM:type:bug.

## Suggested seed namespaces

A fresh project is auto-seeded with the 17 default labels below on atm project
create (and re-applied idempotently by atm label seed --project <CODE> / the
Labels pane [S] key). Templated namespaces (repo:<name>, doc:<name>,
claimed-by:<agent>, blocks:<ID>, related:<ID>) are created on demand — they
depend on project-specific values and are NOT seeded as concrete labels.

  status:            open, todo, in-progress, done, blocked, review  (workflow states — labels only, no state machine)
  type:              bug, feature, task, chore                       (task categorization)
  priority:          high, medium, low                                (optional prioritization)
  context:documentation  the labeled task contains documentation about the project
  context:repository    the labeled task contains a pointer to a code repository
  context:agent         ATM:context:agent — agent direction when navigating the project
  context:fixit         ATM:context:fixit — something on this task should be reviewed, updated, or altered
  repo:<name>           ATM:repo:atm — index task pointing at a repo (created on demand, not seeded)
  doc:<name>            ATM:doc:architecture — index task pointing at a doc/resource (created on demand, not seeded)
  claimed-by:<agent>    ATM:claimed-by:claude — who's working on what (last-writer-wins, no conflict detection)
  blocks:<ID>, related:<ID>   ATM:blocks:ATM-0002 — task relationships via labels (created on demand, not seeded)

## Agent code-of-conduct (label hygiene)

Agents working in an ATM project follow these rules to keep the label substrate
legible for humans and other agents:

  1. Read before you write. Run atm label list --project <CODE> and read every
     label's description before introducing any new label. The existing labels
     are the project's vocabulary; reuse them whenever one fits your intent.
  2. Default setup is the baseline. The seeded labels (status, type, priority,
     context) cover the common cases. Prefer them. Do not reinvent status:open
     as state:open or wf:open.
  3. Invent only when nothing fits. If no existing label captures your intent,
     you may create a new one — agents are free to self-organize. But before
     you do, ask yourself: would a human reviewing the Labels pane understand
     why this label exists?
  4. State the intention in the label description. When you create a new label,
     also call atm label add --name <CODE>:<ns>:<value> --description "<one
     sentence: why this label exists>". The description is the intention record.
     A label with no description is a flag for human review: "agent introduced
     this but didn't explain why."
  5. One label, one meaning. Don't use the same label string to mean different
     things across tasks. If your intent diverges from an existing label's
     description, create a new label with a distinct name and a description
     that distinguishes it.
  6. Humans reconcile. The Labels pane is the human's review surface. If you see
     labels that overlap, contradict, or lack descriptions, edit or remove
     them there. Agents follow the rules above; humans curate.

## First-time human sequence

  1. atm tui (auto-inits the store)
  2. Create the project (Add in the Projects pane). Project create auto-seeds the
     17 default labels with descriptions, so the Labels pane is populated from
     the start.
  3. Create seed index tasks (context:agent, context:repository,
     context:documentation) and initial work tasks, labeling as you go. The
     human curates labels in the Labels pane.

## Agent first-contact sequence

  1. atm conventions — read this guide, including the code-of-conduct.
  2. atm label list --project <CODE> — read every label's description first to
     understand the project's vocabulary before exploring tasks. Labels are
     the project's language; knowing them makes every task query meaningful.
  3. task list --project <CODE> --label <CODE>:context:agent — get agent
     directions for working in this project.
  4. task list --project <CODE> --label <CODE>:context:repository /
     :context:documentation — discover repository pointers and documentation.
  5. task list --project <CODE> --label <CODE>:status:open — get open work.

  A fresh agent that does not yet know the project's namespaces runs the
  label-list step first and follows the descriptions.

## Notes

  - Plugins/skills: ATM ships only the doc + the conventions command. Plugins
    or agent skills may wrap the first-contact sequence; ATM itself has no
    plugin mechanism.
  - Re-seeding defaults: atm label seed --project <CODE> or the Labels pane [S]
    key re-applies the default set idempotently — existing descriptions are
    preserved, and any new defaults introduced in a release are added.

Conventions are advisory only — nothing in the store validates or
special-cases the documented namespaces.`
