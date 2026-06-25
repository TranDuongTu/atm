package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbletea"
)

type helpModel struct {
	app    *Model
	offset int
	height int
	width  int
	lines  []string
}

func newHelpModel(app *Model) *helpModel {
	h := &helpModel{app: app}
	h.buildLines()
	return h
}

func (h *helpModel) setSize(w, he int) {
	h.width = w
	h.height = he
	h.clampOffset()
}

func (h *helpModel) setFilter(v string) {}

func (h *helpModel) refresh() {
	h.buildLines()
}

func (h *helpModel) buildLines() {
	h.lines = nil
	h.lines = append(h.lines, "Help - CLI / TUI parity")
	h.lines = append(h.lines, strings.Repeat("-", 40))
	h.lines = append(h.lines, fmt.Sprintf("%-36s %s", "CLI command", "TUI path"))
	rows := [][2]string{
		{"atm init", "Startup flow (no-store prompt) -> [I]"},
		{"atm store path", "Header -> store indicator"},
		{"atm project create", "Projects -> [a]"},
		{"atm project list", "Projects left pane"},
		{"atm project show", "Projects right column"},
		{"atm project set-type-axis", "Projects -> [T]"},
		{"atm project set-name", "Projects -> [N]"},
		{"atm project label add/remove/list", "Projects -> [L]/[l]"},
		{"atm project repo add/remove", "Projects -> [R]/[r]"},
		{"atm project guide show", "Projects right column -> Guide"},
		{"atm project guide section add/rename/remove/move", "Projects guide actions -> [S]/[s]/[X]/[M]"},
		{"atm project guide ref add/remove/move", "Projects guide actions -> [g]/[d]/[m]"},
		{"atm project guide set-freshness", "Projects guide actions -> [F]"},
		{"atm project guide status", "Summary when scoped to project"},
		{"atm task create", "Tasks -> [a]"},
		{"atm task show [--with-context]", "Tasks -> Enter"},
		{"atm task list", "Tasks left pane + filters"},
		{"atm task set-status", "Tasks detail -> [s]"},
		{"atm task set-title / set-description", "Tasks detail -> [e]"},
		{"atm task label add/remove", "Tasks detail -> [b]"},
		{"atm task next [--claim]", "Tasks -> [n] / [c]"},
		{"atm task claim / unclaim", "Tasks -> [c] / [u]"},
		{"atm task link add/remove/list", "Tasks detail -> [L]"},
		{"atm task todo add / toggle", "Tasks detail -> [t] / Space"},
		{"atm task followup add / resolve", "Tasks detail -> [o] / [O]"},
		{"atm task discussion add", "Tasks detail -> [d]"},
		{"atm task timeline", "Tasks detail -> TIMELINE section"},
		{"atm review request / approve / reject", "Summary -> [a] / [r] or Tasks -> [v]"},
		{"atm review queue / followups", "Summary"},
		{"atm review dashboard", "Summary"},
		{"atm actor list / show", "Tasks -> Actor / claims context"},
	}
	for _, r := range rows {
		h.lines = append(h.lines, fmt.Sprintf("%-36s %s", r[0], r[1]))
	}
	h.lines = append(h.lines, "")
	h.lines = append(h.lines, "Global keymap:")
	h.lines = append(h.lines, "  1-4                  focus left panes")
	h.lines = append(h.lines, "  r                    refresh current view")
	h.lines = append(h.lines, "  /                    inline filter")
	h.lines = append(h.lines, "  :                    command palette")
	h.lines = append(h.lines, "  ?                    focus Help")
	h.lines = append(h.lines, "  q                    quit")
	h.lines = append(h.lines, "  Esc                  cancel input / close overlay")
	h.lines = append(h.lines, "  j/k or Up/Down       move list cursor")
	h.lines = append(h.lines, "  g/G                  top/bottom of list")
	h.lines = append(h.lines, "  Space                toggle project scope in Projects")
	h.lines = append(h.lines, "  Left/Right           move project right-column sections")
	h.lines = append(h.lines, "  Actors               shown in task Actor / claims context")
	h.clampOffset()
}

func (h *helpModel) clampOffset() {
	maxOff := len(h.lines) - h.height
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

func (h *helpModel) update(msg tea.Msg) (tea.Model, tea.Cmd) {
	key := msg.(tea.KeyMsg).String()
	switch key {
	case "j", "down":
		h.offset++
		h.clampOffset()
	case "k", "up":
		if h.offset > 0 {
			h.offset--
		}
	case "g":
		h.offset = 0
	case "G":
		h.offset = len(h.lines)
		h.clampOffset()
	}
	return h.app, nil
}

func (h *helpModel) view() string {
	var b strings.Builder
	end := h.offset + h.height
	if end > len(h.lines) {
		end = len(h.lines)
	}
	for i := h.offset; i < end; i++ {
		b.WriteString(h.lines[i])
		b.WriteString("\n")
	}
	if end < len(h.lines) {
		b.WriteString(fmt.Sprintf("... %d more (j/k to scroll)\n", len(h.lines)-end))
	}
	return b.String()
}
