package components

import "strings"

type FilterInput struct {
	Value  string
	Active bool
	Width  int
	Prompt string
}

func NewFilterInput() *FilterInput {
	return &FilterInput{Width: 40, Prompt: "filter"}
}

func (f *FilterInput) Focus()        { f.Active = true }
func (f *FilterInput) Blur()         { f.Active = false; f.Value = "" }
func (f *FilterInput) SetSize(w int) { f.Width = w }

func (f *FilterInput) Update(s string) {
	f.Value = s
}

func (f *FilterInput) Append(r rune) {
	f.Value += string(r)
}

func (f *FilterInput) Backspace() {
	if len(f.Value) > 0 {
		f.Value = f.Value[:len(f.Value)-1]
	}
}

func (f *FilterInput) Clear() { f.Value = "" }

func (f *FilterInput) Render() string {
	pad := f.Width - len(f.Prompt) - len(f.Value) - 2
	if pad < 0 {
		pad = 0
	}
	cur := " "
	if f.Active {
		cur = "_"
	}
	return "/" + f.Prompt + ": " + f.Value + strings.Repeat(" ", pad) + cur
}
