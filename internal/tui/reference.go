package tui

import (
	"fmt"
	"strings"

	"github.com/muesli/reflow/wordwrap"
)

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

// parityTable is the verbatim CLI/TUI parity table from mockup Screen 10.
var parityTable = `CLI                                   TUI
─────────────────────────────────────────────────────────────────────────────
atm init                              (auto on first atm tui)
atm store path                        status bar (STORE:)
atm conventions                       menu, conventions section
atm project create --code --name      Projects pane  [a]dd
atm project list                      Projects pane  (list)
atm project show --code               Projects pane  [Enter] detail
atm project set-name --code --name    Projects detail  [n]
atm project remove --code             Projects pane  [x]

atm label add --name --desc           Tasks pane [a]dd / [d]esc
atm label remove --name               Tasks pane [l]
atm capability workflow seed --project Tasks pane [S] (re-ensure capability vocabulary)
atm label list [--project] [--ns]     Tasks pane (boards strip)
atm label show --name                 — (CLI only)

atm task create --project --title [--label]   Tasks pane  [a]dd (labels field)
atm task list [--project] [--label]   Tasks pane  (board strip filters the list)
atm task list --facets                CLI wildcard faceting; TUI board strip (Tasks mirror)
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

// keymapReferenceText renders the menu entry table flat: one row per keyed
// entry, columns Key | Where | Action. Global/views entries come first, then
// each scope's actions in table order, then the hidden navigation pairs.
func keymapReferenceText() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%-18s %-18s %s\n", "Key", "Where", "Action")
	b.WriteString(strings.Repeat("-", 18+1+18+1+40))
	b.WriteString("\n")
	for _, e := range menuEntries {
		if e.key == "" {
			continue
		}
		fmt.Fprintf(&b, "%-18s %-18s %s\n", e.key, keymapScopeName(e), e.label)
	}
	return b.String()
}

// keymapScopeName is the Where-column display name for an entry: "global"
// for views, the focused scope for actions, "—" for hidden navigation rows.
func keymapScopeName(e menuEntry) string {
	if e.hidden {
		return "—"
	}
	if e.section == sectionViews {
		return "global"
	}
	for _, s := range e.scopes {
		switch s {
		case scopeProjectsList:
			return "projects"
		case scopeProjectsDetail:
			return "projects detail"
		case scopeProjectsDrill:
			return "persona drill"
		case scopeTasksList:
			return "tasks"
		case scopeTasksDetail:
			return "task detail"
		case scopeBoards:
			return "boards"
		}
	}
	return "global"
}

// conventionsTextTUI mirrors the CLI conventions text (the minimal substrate
// primer, spec §1 of the capability-namespace v2 design). Duplicated here
// (rather than imported) to avoid a cli->tui dependency cycle; the spec
// calls the TUI a "second render of the same reference" and this keeps the
// content authoritative in one place per surface.
//
// Paragraphs are single unwrapped lines (no hard column wrapping). The TUI
// renderer truncates overlong lines to the pane width rather than wrapping,
// and the user scrolls vertically to read the whole guide.
var conventionsTextTUI = "## What ATM is\n" +
	"\n" +
	"ATM (Agent Tasks Management) is a label-substrate task store. A project holds tasks; each task has free-form text (title, description) and a set of labels. No status field, no claims, no review queue, no state machine — status, type, priority, ownership, relationships are all labels, interpreted by the agent reading them. The store keeps the substrate legible; capabilities own the semantics.\n" +
	"\n" +
	"## Substrate\n" +
	"Substrate commands live under these namespaces; run `-h` on each for verbs and flags:\n" +
	"- `atm task` — tasks (ID, title, description, labels).\n" +
	"- `atm task comment` — per-task append-mostly thread, classified by a label.\n" +
	"- `atm label` — labels (`<CODE>:<ns>:<value>` or `<CODE>:<tag>`); a label's description records its intention. Three kinds: stored (asserted), namespace (prefix, emergent), board (computed from an expression).\n" +
	"- `atm project`, `atm persona`, `atm activity`, `atm store`, `atm search` — project lifecycle, actor identity, audit log, semantic search.\n" +
	"\n" +
	"## Capabilities\n" +
	"Semantics beyond the substrate live in capabilities. Each owns a slice of the label substrate, contributes verbs, and explains itself. A project enables a per-project subset; commands for disabled capabilities are not mounted.\n" +
	"- `atm capability list` — enumerate registered capabilities (enabled + disabled).\n" +
	"- `atm capability <name> -h` — the verb tree a capability mounts.\n" +
	"- `atm capability <name> guide` — the capability's full agent-facing semantics, actions, and converged state (Semantics / Actions / Converge sections).\n" +
	"\n" +
	"## Actor identity\n" +
	"Every mutation stamps `persona@agent:model` (e.g. `developer@claude:opus-4.8`). `atm persona -h`; built-ins `developer`, `manager`, `admin`, `concierge`. `atm --persona <name> --project <CODE> -h`.\n" +
	"\n" +
	"Conventions are advisory only."
