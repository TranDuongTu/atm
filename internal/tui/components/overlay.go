package components

import (
	"fmt"
	"strings"
)

type Overlay struct {
	Title   string
	Width   int
	Lines   []string
	Visible bool
}

func NewOverlay() *Overlay { return &Overlay{Width: 60} }

func (o *Overlay) Show(title string, lines []string) {
	o.Title = title
	o.Lines = lines
	o.Visible = true
}

func (o *Overlay) Hide() { o.Visible = false }

func (o *Overlay) Render(maxWidth, maxHeight int) string {
	if !o.Visible {
		return ""
	}
	w := o.Width
	if w > maxWidth-4 {
		w = maxWidth - 4
	}
	if w < 20 {
		w = 20
	}
	border := strings.Repeat("-", w-4)
	var b strings.Builder
	b.WriteString("+" + border + "+\n")
	titleLine := fmt.Sprintf(" %s ", o.Title)
	if len(titleLine) > w-4 {
		titleLine = titleLine[:w-4]
	}
	pad := w - 4 - len(titleLine)
	if pad < 0 {
		pad = 0
	}
	b.WriteString("|" + titleLine + strings.Repeat(" ", pad) + "|\n")
	b.WriteString("+" + border + "+\n")
	for _, l := range o.Lines {
		if len(l) > w-4 {
			l = l[:w-4]
		}
		pad := w - 4 - len(l)
		if pad < 0 {
			pad = 0
		}
		b.WriteString("|" + l + strings.Repeat(" ", pad) + "|\n")
		if b.Len() > maxHeight*2 {
			break
		}
	}
	b.WriteString("+" + border + "+")
	return b.String()
}
