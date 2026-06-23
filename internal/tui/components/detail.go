package components

import (
	"strings"
)

type Detail struct {
	Lines  []string
	Width  int
	Height int
	Offset int
}

func NewDetail() *Detail {
	return &Detail{Width: 80, Height: 20}
}

func (d *Detail) SetSize(w, h int) {
	d.Width = w
	if h > 0 {
		d.Height = h
	}
	d.clampOffset()
}

func (d *Detail) SetContent(lines []string) {
	d.Lines = lines
	d.clampOffset()
}

func (d *Detail) ScrollUp() {
	if d.Offset > 0 {
		d.Offset--
	}
}

func (d *Detail) ScrollDown() {
	maxOff := len(d.Lines) - d.Height
	if d.Offset < maxOff {
		d.Offset++
	}
}

func (d *Detail) clampOffset() {
	maxOff := len(d.Lines) - d.Height
	if maxOff < 0 {
		maxOff = 0
	}
	if d.Offset > maxOff {
		d.Offset = maxOff
	}
	if d.Offset < 0 {
		d.Offset = 0
	}
}

func (d *Detail) Render() string {
	var b strings.Builder
	end := d.Offset + d.Height
	if end > len(d.Lines) {
		end = len(d.Lines)
	}
	for i := d.Offset; i < end; i++ {
		b.WriteString(d.Lines[i])
		b.WriteString("\n")
	}
	return b.String()
}
