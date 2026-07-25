package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type formField struct {
	Label     string
	Value     string
	Required  bool
	Hint      string
	Validator func(field, value string) error
	// Note returns an optional non-error message rendered below the field
	// (in a success style) when the field's value is valid and non-empty.
	// Used by the board editor to show the live match count.
	Note func(field, value string) string
	pos int
}

type formFocus int

const (
	focusFields formFocus = iota
	focusButtons
)

type Form struct {
	Title  string
	Fields []formField
	cursor int // active field index
	btnIdx int // 0 = Submit, 1 = Cancel
	zone   formFocus
	Active bool
	Done   bool
	Cancel bool
	Err    string
	width  int
}

func NewForm(title string, fields []formField) *Form {
	for i := range fields {
		fields[i].pos = len(fields[i].Value)
	}
	return &Form{Title: title, Fields: fields, Active: true, width: 48, zone: focusFields}
}

func (f *Form) SetWidth(w int) {
	if w < 48 {
		w = 48
	}
	if w > 120 {
		w = 120
	}
	f.width = w
}

func FormWidth(termWidth int) int {
	w := termWidth - 8
	if w < 48 {
		w = 48
	}
	if w > 90 {
		w = 90
	}
	return w
}

func (f *Form) Values() map[string]string {
	out := map[string]string{}
	for _, fld := range f.Fields {
		out[fld.Label] = fld.Value
	}
	return out
}

func (f *Form) field() *formField { return &f.Fields[f.cursor] }

func (f *Form) norm() {
	if f.zone != focusFields {
		return
	}
	fld := f.field()
	if fld.pos > len(fld.Value) {
		fld.pos = len(fld.Value)
	}
}

func (f *Form) Update(msg tea.Msg) (*Form, tea.Cmd) {
	if !f.Active {
		return f, nil
	}
	f.norm()
	m, ok := msg.(tea.KeyMsg)
	if !ok {
		return f, nil
	}
	switch m.String() {
	case "esc":
		f.Cancel = true
		f.Active = false
		return f, nil
	case "tab":
		f.next()
		return f, nil
	case "shift+tab":
		f.prev()
		return f, nil
	case "up":
		f.prev()
		return f, nil
	case "down":
		f.next()
		return f, nil
	case "left":
		if f.zone == focusButtons {
			if f.btnIdx == 1 {
				f.btnIdx = 0
			}
			return f, nil
		}
		if f.zone == focusFields {
			fld := &f.Fields[f.cursor]
			if fld.pos > 0 {
				fld.pos--
			}
		}
		return f, nil
	case "right":
		if f.zone == focusButtons {
			if f.btnIdx == 0 {
				f.btnIdx = 1
			}
			return f, nil
		}
		if f.zone == focusFields {
			fld := &f.Fields[f.cursor]
			if fld.pos < len(fld.Value) {
				fld.pos++
			}
		}
		return f, nil
	case "home", "ctrl+a":
		if f.zone == focusFields {
			f.Fields[f.cursor].pos = 0
		}
		return f, nil
	case "end", "ctrl+e":
		if f.zone == focusFields {
			f.Fields[f.cursor].pos = len(f.Fields[f.cursor].Value)
		}
		return f, nil
	case "enter":
		if f.zone == focusButtons {
			if f.btnIdx == 0 {
				if err := f.validate(); err != "" {
					f.Err = err
					return f, nil
				}
				f.Done = true
				f.Active = false
			} else {
				f.Cancel = true
				f.Active = false
			}
			return f, nil
		}
		if f.cursor == len(f.Fields)-1 {
			if err := f.validate(); err != "" {
				f.Err = err
				return f, nil
			}
			f.Done = true
			f.Active = false
			return f, nil
		}
		f.next()
		return f, nil
	case "backspace":
		if f.zone == focusFields {
			fld := &f.Fields[f.cursor]
			if fld.pos > 0 {
				fld.Value = fld.Value[:fld.pos-1] + fld.Value[fld.pos:]
				fld.pos--
			}
			f.Err = ""
		}
		return f, nil
	case "delete", "ctrl+d":
		if f.zone == focusFields {
			fld := &f.Fields[f.cursor]
			if fld.pos < len(fld.Value) {
				fld.Value = fld.Value[:fld.pos] + fld.Value[fld.pos+1:]
			}
			f.Err = ""
		}
		return f, nil
	case " ": // tea delivers space as KeySpace, not KeyRunes
		if f.zone == focusFields {
			fld := &f.Fields[f.cursor]
			fld.Value = fld.Value[:fld.pos] + " " + fld.Value[fld.pos:]
			fld.pos++
			f.Err = ""
		}
		return f, nil
	default:
		if f.zone == focusFields && m.Type == tea.KeyRunes {
			fld := &f.Fields[f.cursor]
			fld.Value = fld.Value[:fld.pos] + string(m.Runes) + fld.Value[fld.pos:]
			fld.pos += len(m.Runes)
			f.Err = ""
		}
		return f, nil
	}
}

func (f *Form) next() {
	if f.zone == focusFields {
		// Clamp pos before leaving this field so it's valid.
		fld := &f.Fields[f.cursor]
		if fld.pos > len(fld.Value) {
			fld.pos = len(fld.Value)
		}
		if f.cursor < len(f.Fields)-1 {
			f.cursor++
		} else {
			f.zone = focusButtons
			f.btnIdx = 0
		}
		return
	}
	f.zone = focusFields
	f.cursor = 0
}

func (f *Form) prev() {
	if f.zone == focusButtons {
		f.zone = focusFields
		f.cursor = len(f.Fields) - 1
		return
	}
	// Clamp pos before leaving this field so it's valid.
	fld := &f.Fields[f.cursor]
	if fld.pos > len(fld.Value) {
		fld.pos = len(fld.Value)
	}
	if f.cursor > 0 {
		f.cursor--
	} else {
		f.zone = focusButtons
		f.btnIdx = 1
	}
}

func (f *Form) validate() string {
	for _, fld := range f.Fields {
		if fld.Required && fld.Value == "" {
			return fmt.Sprintf("%s is required", fld.Label)
		}
		if fld.Validator != nil {
			if err := fld.Validator(fld.Label, fld.Value); err != nil {
				return err.Error()
			}
		}
	}
	return ""
}

// valid reports whether the form's current values pass all required and
// per-field validator checks. Submit is disabled (button inactive) while
// invalid.
func (f *Form) valid() bool { return f.validate() == "" }

// fieldError returns the live error message for the field at idx, or "" if
// the field's current value is valid (or has no validator). Required-empty
// is suppressed here so an untouched field does not flash red; only a
// non-empty value that fails its validator is shown live.
func (f *Form) fieldError(idx int) string {
	fld := f.Fields[idx]
	if fld.Validator == nil {
		return ""
	}
	if fld.Value == "" {
		// Required-empty is handled at submit, not live.
		return ""
	}
	if err := fld.Validator(fld.Label, fld.Value); err != nil {
		return err.Error()
	}
	return ""
}

func (f *Form) View(styles Styles) string {
	var b strings.Builder
	innerW := f.width

	// Title bar.
	b.WriteString(styles.DialogTitle.Render(f.Title))
	b.WriteString("\n")
	b.WriteString(strings.Repeat("─", innerW))
	b.WriteString("\n\n")

	// Fields.
	for i, fld := range f.Fields {
		active := f.zone == focusFields && i == f.cursor
		label := styles.FieldLabel.Render(fld.Label + ":")
		labelW := lipgloss.Width(label)
		cursorW := 0
		if active {
			cursorW = 1
		}
		valueW := innerW - labelW - 1 /*sep*/ - cursorW
		var shown string
		var cursorCol int
		if active {
			start := 0
			if fld.pos > valueW {
				start = fld.pos - valueW
			}
			if start > 0 {
				shown = fitLineFrom(fld.Value, start, valueW)
			} else {
				shown = fitLine(fld.Value, valueW)
			}
			cursorCol = fld.pos - start
		} else {
			shown = fitLine(fld.Value, valueW)
		}
		val := styles.FieldValue.Render(shown)
		if active {
			runes := []rune(shown)
			if cursorCol < len(runes) {
				val = styles.FieldValue.Render(string(runes[:cursorCol]))
				val += styles.FieldValue.Underline(true).Render(string(runes[cursorCol]))
				if cursorCol+1 < len(runes) {
					val += styles.FieldValue.Render(string(runes[cursorCol+1:]))
				}
			} else {
				val += styles.FieldValue.Underline(true).Render(" ")
			}
		}
		row := fmt.Sprintf("%s %s", label, val)
		b.WriteString(row)
		b.WriteString("\n")
		if fld.Hint != "" {
			b.WriteString(styles.FieldHint.Render("  " + fld.Hint))
			b.WriteString("\n")
		}
		if errMsg := f.fieldError(i); errMsg != "" {
			b.WriteString(styles.Error.Render("  x " + errMsg))
			b.WriteString("\n")
		} else if fld.Note != nil {
			if note := fld.Note(fld.Label, fld.Value); note != "" {
				b.WriteString(styles.Success.Render("  v " + note))
				b.WriteString("\n")
			}
		}
		b.WriteString("\n")
	}

	if f.Err != "" {
		b.WriteString(styles.Error.Render("x " + f.Err))
		b.WriteString("\n\n")
	}

	// Action buttons row.
	submitActive := f.zone == focusButtons && f.btnIdx == 0
	cancelActive := f.zone == focusButtons && f.btnIdx == 1
	submit := styles.ButtonInactive.Render("[ Submit ]")
	cancel := styles.ButtonInactive.Render("[ Cancel ]")
	if submitActive && f.valid() {
		submit = styles.ButtonActive.Render("[ Submit ]")
	}
	if cancelActive {
		cancel = styles.ButtonActive.Render("[ Cancel ]")
	}
	buttons := lipgloss.JoinHorizontal(lipgloss.Center, submit, "  ", cancel)
	hint := styles.KeyMenuDim.Render("Tab/arrows to navigate  Enter to confirm  Esc to cancel")
	b.WriteString(buttons)
	b.WriteString("\n")
	b.WriteString(hint)

	return styles.Dialog.Render(b.String())
}
