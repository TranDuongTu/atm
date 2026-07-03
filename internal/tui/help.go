package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbletea"
)

type helpModel struct {
	m     *Model
	lines []string
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
	b.WriteString("ATM Help\n")
	b.WriteString(sepLine("─", 78, h.m.width, 2))
	b.WriteString("\n\n")

	b.WriteString("Section 1 — CLI / TUI parity\n")
	b.WriteString(sepLine("─", 78, h.m.width, 2))
	b.WriteString("\n")
	b.WriteString(parityTable)
	b.WriteString("\n\n")

	b.WriteString("Section 2 — Global keymap\n")
	b.WriteString(sepLine("─", 78, h.m.width, 2))
	b.WriteString("\n")
	b.WriteString(keymapTable())
	b.WriteString("\n\n")

	b.WriteString("Section 3 — Conventions (advisory)\n")
	b.WriteString(sepLine("─", 78, h.m.width, 2))
	b.WriteString("\n")
	b.WriteString(conventionsTextTUI)

	h.lines = strings.Split(b.String(), "\n")
	h.clampOffset()
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
atm conventions                       this tab, bottom section

atm project create --code --name      Projects tab  [a]dd
atm project list                      Projects tab  (list)
atm project show --code               Projects tab  [Enter] detail
atm project set-name --code --name    Projects detail  [N]
atm project remove --code             Projects tab  [x]

atm label add --name --desc           Projects detail  [L]
atm label remove --name               Projects detail  [l]
atm label list [--project] [--ns]     Projects detail  (labels section)
atm label show --name                 — (CLI only)

atm task create --project --title     Tasks tab  [a]dd
atm task list [--project] [--label]   Tasks tab  (list; / for filter)
atm task list --facets                Tasks tab  (wildcard filter -> grouped)
atm task show --id                    Tasks tab  [Enter] detail
atm task set-title --id --title       Task detail  [e]
atm task set-description --id --desc  Task detail  [d]
atm task label add --id --label       Task detail  [b]
atm task label remove --id --label    Task detail  [B]
atm task remove --id                  Task detail  [x]

atm tui                                (you are here)`

// keymapTable renders the global keymap summary as a fixed-width table.
func keymapTable() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%-12s %-30s %-22s %-10s %-26s\n", "Key", "Projects", "Tasks", "Help", "Detail")
	b.WriteString(strings.Repeat("-", min(100, 0+12+1+30+1+22+1+10+1+26)))
	b.WriteString("\n")
	for _, r := range keymapRows {
		fmt.Fprintf(&b, "%-12s %-30s %-22s %-10s %-26s\n", r.Key, r.Projects, r.Tasks, r.Help, r.Detail)
	}
	return b.String()
}

// conventionsTextTUI mirrors the CLI conventions text (spec §7). Duplicated
// here (rather than imported) to avoid a cli->tui dependency cycle; the spec
// calls the TUI a "second render of the same reference" and this keeps the
// content authoritative in one place per surface.
var conventionsTextTUI = `Version: v2.0

v2's vision is that workflow lives outside the system — in agent prompts and
human habits, not in the store. But a fresh agent landing in a project needs a
deterministic entry point, and a fresh project needs its labels seeded.
Onboarding is the act of seeding index tasks and seed labels; the conventions
below tell humans and agents how to do that using only the label substrate.
No bootstrap command, no reserved labels, no repo-side files, no store-side
repo paths. The conventions are documented, not code-reserved: the system
treats ATM:context:start-here identically to ATM:type:bug.

## Suggested seed namespaces

  status:            open, todo, in-progress, done, blocked, review
  type:              bug, feature, task, chore
  priority:          high, medium, low
  repo:<name>        ATM:repo:atm  (repo->project binding, label substrate)
  doc:<name>         ATM:doc:architecture  (index task -> doc/resource)
  context:always-read  pointer to always-read context markdown
  context:start-here   the single entry-point task a fresh agent queries first
  claimed-by:<agent>   who's working on what (replaces v1 Claim; last-writer-wins)
  blocks:<ID>, related:<ID>   task relationships via labels (replaces v1 Links)

## First-time human sequence

  1. atm tui (auto-inits the store)
  2. Create the project (Add in the Projects tab)
  3. Create a few seed index tasks (start-here, repo:<name>, doc:<name>,
     context:always-read) and initial work tasks, labeling as you go.

## Agent first-contact sequence

  1. atm conventions — read the guide.
  2. task list --project <CODE> --label <CODE>:context:start-here
  3. task list --project <CODE> --label <CODE>:repo:* / :doc:* / :context:*
  4. task list --project <CODE> --label <CODE>:status:open

(advisory — the system treats all namespaces identically)`