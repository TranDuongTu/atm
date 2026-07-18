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

// keymapTable renders the global keymap summary as a fixed-width table.
func keymapTable() string {
	var b strings.Builder
	widths := []int{10, 18, 21, 19, 21}
	fmt.Fprintf(&b, "%-*s %-*s %-*s %-*s %-*s\n",
		widths[0], "Key",
		widths[1], "Projects",
		widths[2], "Tasks",
		widths[3], "Boards",
		widths[4], "Detail/Overlay",
	)
	b.WriteString(strings.Repeat("-", widths[0]+1+widths[1]+1+widths[2]+1+widths[3]+1+widths[4]))
	b.WriteString("\n")
	for _, r := range keymapRows {
		fmt.Fprintf(&b, "%-*s %-*s %-*s %-*s %-*s\n",
			widths[0], truncateRunes(r.Key, widths[0]),
			widths[1], truncateRunes(r.Projects, widths[1]),
			widths[2], truncateRunes(r.Tasks, widths[2]),
			widths[3], truncateRunes(r.Boards, widths[3]),
			widths[4], truncateRunes(r.Detail, widths[4]),
		)
	}
	return b.String()
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
	"- `atm capability <name> guide` — the capability's full agent-facing semantics, vocabulary, and operating mode (Brief + Autopilot sections).\n" +
	"\n" +
	"## Actor identity\n" +
	"Every mutation stamps `persona@agent:model` (e.g. `developer@claude:opus-4.8`). `atm persona -h`; built-ins `developer`, `manager`, `admin`. `atm dev -h`.\n" +
	"\n" +
	"Conventions are advisory only."
