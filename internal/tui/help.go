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
		{"atm project create", "Tab 2 -> [a]"},
		{"atm project list", "Tab 2 (list)"},
		{"atm project show", "Tab 2 -> Enter"},
		{"atm project set-type-axis", "Tab 2 -> [T]"},
		{"atm project set-name", "Tab 2 -> [N]"},
		{"atm project label add/remove/list", "Tab 2 -> [L]/[l]"},
		{"atm project repo add/remove", "Tab 2 -> [R]/[r]"},
		{"atm project guide show", "Tab 2 -> Guide pane"},
		{"atm project guide section add/rename/remove/move", "Tab 2 -> [S]/[s]/[X]/[M]"},
		{"atm project guide ref add/remove/move", "Tab 2 -> [g]/[d]/[m]"},
		{"atm project guide set-freshness", "Tab 2 -> [F]"},
		{"atm project guide status", "Tab 1 GUIDE STATUS (or Tab 2 -> [D])"},
		{"atm task create", "Tab 3 -> [a]"},
		{"atm task show [--with-context]", "Tab 3 -> Enter"},
		{"atm task list", "Tab 3 (list + filters)"},
		{"atm task set-status", "Tab 3 detail -> [s]"},
		{"atm task set-title / set-description", "Tab 3 detail -> [e]"},
		{"atm task label add/remove", "Tab 3 detail -> [b]"},
		{"atm task next [--claim]", "Tab 3 -> [n] / [c]"},
		{"atm task claim / unclaim", "Tab 3 -> [c] / [u]"},
		{"atm task link add/remove/list", "Tab 3 detail -> [L]"},
		{"atm task todo add / toggle", "Tab 3 detail -> [t] / Space"},
		{"atm task followup add / resolve", "Tab 3 detail -> [o] / [O]"},
		{"atm task discussion add", "Tab 3 detail -> [d]"},
		{"atm task timeline", "Tab 3 detail -> TIMELINE section"},
		{"atm review request / approve / reject", "Tab 1 -> [v] / [a] / [r] (or Tab 3)"},
		{"atm review queue / followups", "Tab 1"},
		{"atm review dashboard", "Tab 1"},
		{"atm actor list / show", "Tab 4 -> Enter"},
	}
	for _, r := range rows {
		h.lines = append(h.lines, fmt.Sprintf("%-36s %s", r[0], r[1]))
	}
	h.lines = append(h.lines, "")
	h.lines = append(h.lines, "Global keymap:")
	h.lines = append(h.lines, "  1-5 / Tab/Shift+Tab  switch tabs")
	h.lines = append(h.lines, "  r                    refresh current view")
	h.lines = append(h.lines, "  /                    inline filter")
	h.lines = append(h.lines, "  :                    command palette")
	h.lines = append(h.lines, "  ?                    help (this tab)")
	h.lines = append(h.lines, "  q                    quit")
	h.lines = append(h.lines, "  Esc                  cancel input / close overlay")
	h.lines = append(h.lines, "  j/k or Up/Down       move list cursor")
	h.lines = append(h.lines, "  g/G                  top/bottom of list")
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
