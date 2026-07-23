package tui

import (
	"fmt"
	"strings"

	"atm/internal/core"

	tea "github.com/charmbracelet/bubbletea"
)

// personasModel is the read-only personas overlay: list built-ins and
// customs, enter to view the effective prompt. No mutation paths.
type personasModel struct {
	m       *Model
	open    bool
	cursor  int
	entries []*core.Persona
	detail  bool
	lines   []string
	offset  int
}

func (p *personasModel) openOverlay() {
	p.entries = p.m.store.ListPersonas()
	p.open, p.detail, p.offset = true, false, 0
	if p.cursor >= len(p.entries) {
		p.cursor = 0
	}
}

func (p *personasModel) handleKey(k tea.KeyMsg) tea.Cmd {
	switch k.String() {
	case "esc", "V":
		if p.detail {
			p.detail = false
			return nil
		}
		p.open = false
	case "j", "down":
		if p.detail {
			p.offset++
			return nil
		}
		if p.cursor < len(p.entries)-1 {
			p.cursor++
		}
	case "k", "up":
		if p.detail {
			if p.offset > 0 {
				p.offset--
			}
			return nil
		}
		if p.cursor > 0 {
			p.cursor--
		}
	case "g":
		p.offset, p.cursor = 0, 0
	case "enter":
		if !p.detail && len(p.entries) > 0 {
			p.lines = strings.Split(p.entries[p.cursor].Prompt, "\n")
			p.offset = 0
			p.detail = true
		}
	}
	return nil
}

// renderOverlay draws the persona list, or the scrolled prompt in detail
// mode. Box shape and cursor styling mirror capabilityModel.renderOverlay.
func (p *personasModel) renderOverlay() string {
	styles := p.m.styles
	bw := p.m.width * 60 / 100
	if bw < 64 {
		bw = 64
	}
	if bw > p.m.width-4 {
		bw = p.m.width - 4
	}

	if p.detail {
		height := p.m.height - 8
		if height < 8 {
			height = 8
		}
		if p.offset > len(p.lines)-1 {
			p.offset = len(p.lines) - 1
		}
		if p.offset < 0 {
			p.offset = 0
		}
		end := p.offset + height - 3
		if end > len(p.lines) {
			end = len(p.lines)
		}
		var body strings.Builder
		for _, ln := range p.lines[p.offset:end] {
			body.WriteString(fitLine(ln, bw-4) + "\n")
		}
		body.WriteString("\n" + styles.KeyMenuDim.Render("[j/k]scroll  [Esc]back"))
		title := "Persona: " + p.entries[p.cursor].Name
		return titledBoxHeight(styles.DialogBody, bw, title, body.String(), height)
	}

	nameW := 10
	for _, e := range p.entries {
		if len(e.Name) > nameW {
			nameW = len(e.Name)
		}
	}
	var body strings.Builder
	for i, e := range p.entries {
		line := fmt.Sprintf("%-*s  %s", nameW, e.Name, e.Description)
		line = fitLine(line, bw-4)
		if i == p.cursor {
			line = styles.RowCursor.Render(line)
		} else {
			line = styles.Body.Render(line)
		}
		body.WriteString(line + "\n")
	}
	body.WriteString("\n" + styles.KeyMenuDim.Render("[↑/↓]move  [Enter]view prompt  [Esc]close"))
	return titledBoxHeight(styles.DialogBody, bw, "Personas", body.String(), len(p.entries)+5)
}
