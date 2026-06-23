package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbletea"
)

type formField struct {
	Label    string
	Value    string
	Required bool
	Hint     string
}

type Form struct {
	Title  string
	Fields []formField
	Cursor int
	Active bool
	Done   bool
	Cancel bool
	Err    string
	width  int
}

func NewForm(title string, fields []formField) *Form {
	return &Form{Title: title, Fields: fields, Active: true, width: 60}
}

func (f *Form) SetWidth(w int) {
	if w > 20 {
		f.width = w - 4
	}
}

func (f *Form) Values() map[string]string {
	out := map[string]string{}
	for _, fld := range f.Fields {
		out[fld.Label] = fld.Value
	}
	return out
}

func (f *Form) Update(msg tea.Msg) (*Form, tea.Cmd) {
	if !f.Active {
		return f, nil
	}
	switch m := msg.(type) {
	case tea.KeyMsg:
		switch m.String() {
		case "esc":
			f.Cancel = true
			f.Active = false
			return f, nil
		case "enter":
			if f.Cursor == len(f.Fields)-1 {
				if err := f.validate(); err != "" {
					f.Err = err
					return f, nil
				}
				f.Done = true
				f.Active = false
				return f, nil
			}
			f.Cursor++
			return f, nil
		case "tab":
			if f.Cursor < len(f.Fields)-1 {
				f.Cursor++
			} else {
				f.Cursor = 0
			}
			return f, nil
		case "shift+tab":
			if f.Cursor > 0 {
				f.Cursor--
			} else {
				f.Cursor = len(f.Fields) - 1
			}
			return f, nil
		case "backspace":
			fld := &f.Fields[f.Cursor]
			if len(fld.Value) > 0 {
				fld.Value = fld.Value[:len(fld.Value)-1]
			}
			f.Err = ""
			return f, nil
		default:
			if m.Type == tea.KeyRunes {
				f.Fields[f.Cursor].Value += string(m.Runes)
				f.Err = ""
			}
			return f, nil
		}
	}
	return f, nil
}

func (f *Form) validate() string {
	for _, fld := range f.Fields {
		if fld.Required && fld.Value == "" {
			return fmt.Sprintf("2 usage: %s is required", fld.Label)
		}
	}
	return ""
}

func (f *Form) View() string {
	var b strings.Builder
	border := strings.Repeat("-", f.width)
	b.WriteString("+" + border + "+\n")
	title := fmt.Sprintf(" %s ", f.Title)
	pad := f.width - len(title)
	if pad < 0 {
		pad = 0
	}
	b.WriteString("|" + title + strings.Repeat(" ", pad) + "|\n")
	b.WriteString("+" + border + "+\n")
	for i, fld := range f.Fields {
		marker := " "
		cursor := " "
		if i == f.Cursor {
			cursor = "_"
			marker = ">"
		}
		label := fld.Label + ":"
		val := fld.Value
		avail := f.width - len(label) - 4
		if len(val) > avail {
			val = val[:avail]
		}
		pad = f.width - len(label) - len(val) - 3
		if pad < 0 {
			pad = 0
		}
		b.WriteString(fmt.Sprintf("|%s %s %s%s%s|\n", marker, label, val, strings.Repeat(" ", pad), cursor))
		if fld.Hint != "" {
			h := "  " + fld.Hint
			if len(h) > f.width {
				h = h[:f.width]
			}
			pad := f.width - len(h)
			if pad < 0 {
				pad = 0
			}
			b.WriteString("|" + h + strings.Repeat(" ", pad) + "|\n")
		}
	}
	if f.Err != "" {
		e := " err: " + f.Err
		if len(e) > f.width {
			e = e[:f.width]
		}
		pad := f.width - len(e)
		if pad < 0 {
			pad = 0
		}
		b.WriteString("|" + e + strings.Repeat(" ", pad) + "|\n")
	}
	b.WriteString("+" + border + "+")
	return b.String()
}
