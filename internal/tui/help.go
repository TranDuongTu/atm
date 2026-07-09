package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbletea"
	"github.com/muesli/reflow/wordwrap"
)

type helpModel struct {
	m      *Model
	mode   helpOverlayKind
	lines  []string
	offset int
	// width and height are the outer dims of the box the help content is
	// being rendered for (either the full workspace when no overlay is open,
	// or the smaller centered modal when ?/C is active). refresh() wraps
	// content to width-2 (the manual titledBox border consumes the 2),
	// and View() exposes height-2 lines of scrolling content.
	width, height int
}

func newHelpModel(m *Model) helpModel {
	return helpModel{m: m, mode: helpKeys, width: m.width, height: m.contentHeight}
}

func (h *helpModel) SetSize(w, hh int) {
	if w < 1 {
		w = 1
	}
	if hh < 1 {
		hh = 1
	}
	h.width = w
	h.height = hh
}

func (h *helpModel) refresh() {
	switch h.mode {
	case helpConventions:
		h.lines = strings.Split(renderConventionsText(h.m.styles, h.width, conventionsTextTUI), "\n")
	default:
		h.mode = helpKeys
		var b strings.Builder
		b.WriteString(sectionDivider(h.m.styles, h.width, "CLI / TUI Parity"))
		b.WriteString("\n")
		b.WriteString(dashboardBlock(h.width, parityTable))
		b.WriteString("\n\n")
		b.WriteString(sectionDivider(h.m.styles, h.width, "Global Keymap"))
		b.WriteString("\n")
		b.WriteString(dashboardBlock(h.width, keymapTable()))
		b.WriteString("\n")
		b.WriteString(dashboardBlock(h.width, "g <n> opens the nth plugin overlay (g 1 = indexer)."))
		h.lines = strings.Split(b.String(), "\n")
	}
	h.clampOffset()
}

func renderConventionsText(styles Styles, width int, text string) string {
	// Wrap to the box's inner content width (width minus the titled box's
	// left+right borders). titledBoxHeight would otherwise truncate lines
	// wider than innerW, hiding the tail.
	contentW := width - 2
	if contentW < 1 {
		contentW = 1
	}
	lines := strings.Split(text, "\n")
	var b strings.Builder
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "## ") {
			if i > 0 {
				b.WriteString("\n")
			}
			b.WriteString(sectionDivider(styles, contentW, strings.TrimPrefix(trimmed, "## ")))
			b.WriteString("\n")
			continue
		}
		// Soft-wrap long lines to the content width so the full text is
		// readable by scrolling vertically (truncating would hide the
		// overflow past the pane edge). Numbered items and bullets get
		// their marker styled; the wrapped continuation lines inherit the
		// same plain style.
		switch {
		case strings.HasPrefix(trimmed, "- "):
			wrapped := wordwrap.String(strings.TrimPrefix(trimmed, "- "), contentW-4)
			b.WriteString(styles.Muted.Render("  • "))
			b.WriteString(wrapped)
		case isNumberedItem(trimmed):
			wrapped := wordwrap.String(trimmed, contentW-2)
			b.WriteString(styles.KeyMenu.Render("  "))
			b.WriteString(wrapped)
		default:
			b.WriteString(wordwrap.String(line, contentW))
		}
		if i < len(lines)-1 {
			b.WriteString("\n")
		}
	}
	return b.String()
}

// isNumberedItem reports whether line begins with a "N." ordered-list marker.
func isNumberedItem(line string) bool {
	if len(line) < 2 || line[0] < '1' || line[0] > '9' {
		return false
	}
	return line[1] == '.'
}

func (h *helpModel) clampOffset() {
	innerH := h.height - 2
	if innerH < 1 {
		innerH = 1
	}
	maxOff := len(h.lines) - innerH
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
	innerH := h.height - 2
	if innerH < 1 {
		innerH = 1
	}
	end := h.offset + innerH
	if end > len(h.lines) {
		end = len(h.lines)
	}
	var b strings.Builder
	for i := h.offset; i < end; i++ {
		b.WriteString(h.lines[i])
		b.WriteString("\n")
	}
	return padToHeight(b.String(), innerH)
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

atm task comment add --task --body [--label] [--reply-to]   Task detail  [M]
atm task comment list --task            Task detail  Comments section
atm task comment show --id              Task detail  [Enter] (read-only overlay)
atm task comment set-body --id --body   — (CLI only; TUI overlay is read-only)
atm task comment label add --id --label     — (CLI only)
atm task comment label remove --id --label  — (CLI only)
atm task comment remove --id            — (CLI only)

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
//
// Paragraphs are single unwrapped lines (no hard column wrapping). The TUI
// renderer truncates overlong lines to the pane width rather than wrapping,
// and the user scrolls vertically to read the whole guide.
var conventionsTextTUI = `## What ATM is

ATM (Agent Tasks Management) is a label-substrate task store for AI agents and the humans steering them. A project holds tasks; each task carries free-form text (title, description) and a set of labels. There is no status field, no claim entity, no review queue, no links table, no state machine — status, type, priority, ownership, and relationships are all expressed as labels and interpreted by the agent reading them. Workflow lives outside the store, in agent prompts and human habits; the store only keeps the substrate legible.

## Where labels live

Labels are global and per-project. The seeded defaults (status, type, priority, context) are written into every new project by ` + "`atm project create`" + ` and re-applied idempotently by ` + "`atm label seed --project <CODE>`" + ` / the Labels pane [S] key. The full, authoritative list with descriptions is in the store — do not memorize or duplicate it here. To see the labels that exist in this project, open the Labels pane or run ` + "`atm label list --project <CODE>`" + `. Each label carries a description; that description is the label's intention record.

## Where tasks live

Tasks live in the store, one JSON file per task, scoped to a project. The Tasks pane lists them; ` + "`atm task list --project <CODE>`" + ` lists them on the CLI. Each task has an ID (` + "`<CODE>-<NNNN>`" + `), a title, a description, and a label set. The description is free-form text — agents write what they are doing, what they found, what they decided, so the next agent or human can pick up where they left off.

## How to read a task and its labels

A task is read as: its title (one line of intent), its description (the running narrative), and its labels (the faceted classification). The labels are the query surface; the description is the working memory. A ` + "`context:agent`" + ` task's description holds agent directions for this project. A ` + "`context:repository`" + ` task points at a repo to read. A ` + "`context:documentation`" + ` task points at a doc to read. ` + "`status:open`" + ` means not done; ` + "`status:in-progress`" + ` means someone is on it; ` + "`status:done`" + ` means stop. Labels are advisory — nothing in the store enforces them, but reading them is how you understand a task without re-reading its whole history.

## How to search

Use labels as the filter. ` + "`atm task list --project <CODE> --label <CODE>:<ns>:<value>`" + ` returns tasks carrying that exact label. Wildcard labels (e.g. ` + "`<CODE>:status:*`" + `) drive faceted grouping in the Tasks pane and ` + "`atm task list --facets`" + ` on the CLI. In the TUI, press ` + "`/`" + ` in the Tasks pane to edit the filter. Combine labels to narrow: ` + "`--label <CODE>:status:open --label <CODE>:type:bug`" + ` is open bugs.

## Agent first-contact sequence

1. Read this guide, including the code-of-conduct below.
2. ` + "`atm label list --project <CODE>`" + ` — read every label's description first to learn the project's vocabulary before touching tasks. Labels are the project's language; knowing them makes every task query meaningful. Do not assume the seeded defaults are all the labels this project uses.
3. ` + "`atm task list --project <CODE> --label <CODE>:context:agent`" + ` — get agent directions for working in this project.
4. ` + "`atm task list --project <CODE> --label <CODE>:context:repository`" + ` / ` + "`:context:documentation`" + ` — discover repository pointers and documentation.
5. ` + "`atm task list --project <CODE> --label <CODE>:status:open`" + ` — get open work.

A fresh agent that does not yet know the project's namespaces runs the label-list step first and follows the descriptions.

## Agent code-of-conduct (label hygiene)

Agents working in an ATM project follow these rules to keep the label substrate legible for humans and other agents:

1. Read before you write. Run ` + "`atm label list --project <CODE>`" + ` and read every label's description before introducing any new label. The existing labels are the project's vocabulary; reuse them whenever one fits your intent.
2. Default setup is the baseline. The seeded labels (status, type, priority, context) cover the common cases. Prefer them. Do not reinvent ` + "`status:open`" + ` as ` + "`state:open`" + ` or ` + "`wf:open`" + `.
3. Invent only when nothing fits. If no existing label captures your intent, you may create a new one — agents are free to self-organize. But before you do, ask yourself: would a human reviewing the Labels pane understand why this label exists?
4. State the intention in the label description. When you create a new label, also call ` + "`atm label add --name <CODE>:<ns>:<value> --description \"<one sentence: why this label exists>\"`" + `. The description is the intention record. A label with no description is a flag for human review: "agent introduced this but didn't explain why."
5. One label, one meaning. Don't use the same label string to mean different things across tasks. If your intent diverges from an existing label's description, create a new label with a distinct name and a description that distinguishes it.
6. Humans reconcile. The Labels pane is the human's review surface. If you see labels that overlap, contradict, or lack descriptions, edit or remove them there. Agents follow the rules above; humans curate.

## First-time human sequence

1. ` + "`atm tui`" + ` (auto-inits the store).
2. Create the project (Add in the Projects pane). Project create auto-seeds the default labels with descriptions, so the Labels pane is populated from the start.
3. Create seed index tasks (` + "`context:agent`" + `, ` + "`context:repository`" + `, ` + "`context:documentation`" + `) and initial work tasks, labeling as you go. The human curates labels in the Labels pane.

## Notes

- Plugins/skills: ATM ships only the doc + the conventions command. Plugins or agent skills may wrap the first-contact sequence; ATM itself has no plugin mechanism.
- Re-seeding defaults: ` + "`atm label seed --project <CODE>`" + ` or the Labels pane [S] key re-applies the default set idempotently — existing descriptions are preserved, and any new defaults introduced in a release are added.

Conventions are advisory only — nothing in the store validates or special-cases the documented namespaces.`
