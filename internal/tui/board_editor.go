package tui

import (
	"fmt"

	"atm/internal/core"
	tea "github.com/charmbracelet/bubbletea"
)

// boardEditor backs the [n]ew/[e]dit form. It re-validates on every
// keystroke so the user sees a match count as they type, and cannot save an
// expression that does not parse. This is what makes the expression language
// usable by someone who does not know its syntax.
type boardEditor struct {
	store core.Service
	code  string

	Name, Description, expr string

	parsed core.Expr
	err    error
	count  int
}

func newBoardEditor(s core.Service, code string) *boardEditor {
	return &boardEditor{store: s, code: code}
}

// SetExpr parses src and evaluates it against the live task set so a cyclic
// or otherwise unresolvable expression surfaces here, not at save time. The
// match count is what the form renders below the expression field.
func (e *boardEditor) SetExpr(src string) {
	e.expr = src
	e.parsed, e.err = core.ParseExpr(src)
	e.count = 0
	if e.err != nil {
		return
	}
	// A cyclic or otherwise unresolvable expression surfaces here, because
	// ListTasksErr evaluates it. ListTasks swallows the error and would
	// conflate a broken board with an empty one.
	tasks, err := e.store.ListTasksErr(core.QueryFilters{Project: e.code, Expr: src})
	if err != nil {
		e.err = err
		return
	}
	e.count = len(tasks)
}

func (e *boardEditor) Valid() bool     { return e.err == nil && e.expr != "" }
func (e *boardEditor) Err() error      { return e.err }
func (e *boardEditor) MatchCount() int { return e.count }
func (e *boardEditor) CanSave() bool   { return e.Valid() && e.Name != "" }

// Save upserts the board through LabelAdd, which re-checks the invariants
// (bad/cyclic/colliding expr) on the write path. The live validation in
// SetExpr makes a save-time failure rare but not impossible — another actor
// could have created a colliding label between the last keystroke and submit.
func (e *boardEditor) Save(actor string) error {
	return e.store.LabelAdd(e.code+":"+e.Name, e.Description, e.expr, actor)
}

// boardExprNote returns the live success message ("N tasks") for the
// expression field, or "" when the expression is empty/invalid (the error
// line covers the invalid case). Used as the formField.Note callback.
func (e *boardEditor) boardExprNote(_, value string) string {
	if value == "" || !e.Valid() {
		return ""
	}
	return fmt.Sprintf("%d %s", e.count, pluralTasks(e.count))
}

// boardExprValidator is the live per-field validator for the expression. It
// re-runs SetExpr so the editor's parsed/err/count state tracks every
// keystroke, then surfaces the error (if any) for the form's error line.
func (e *boardEditor) boardExprValidator(_, value string) error {
	e.SetExpr(value)
	if !e.Valid() && value != "" {
		return e.Err()
	}
	return nil
}

// nameValidator mirrors labelSuffixRe so the board name field live-validates
// the same way the add-label form does.
func (e *boardEditor) nameValidator(_, value string) error {
	if value == "" {
		return nil
	}
	if !labelSuffixRe.MatchString(value) {
		return fmt.Errorf("use <namespace>:<value> or <tag>, e.g. status:open")
	}
	return nil
}

// --- Form wiring ---

// openBoardEditorForm opens the live-validated board editor form. When
// existing is a non-empty board name (the [e]dit case), the form is pre-filled
// with that board's current Name/Description/Expr; otherwise ([n]ew) the
// fields start blank.
func (m *Model) openBoardEditorForm(code, existing string) {
	ed := newBoardEditor(m.store, code)
	exprValidator := ed.boardExprValidator
	exprNote := ed.boardExprNote
	nameValidator := ed.nameValidator
	desc, expr := "", ""
	if existing != "" {
		if l, err := m.store.LabelShow(code + ":" + existing); err == nil {
			ed.Name = existing
			ed.Description = l.Description
			ed.expr = l.Expr
			desc = l.Description
			expr = l.Expr
			// Seed the live count/error state so the form opens showing
			// the current match count for the existing expression.
			ed.SetExpr(l.Expr)
		}
	}
	fields := []formField{
		{Label: "name", Required: true, Value: ed.Name, Hint: "<namespace>:<value> or <tag>, e.g. next-sprint", Validator: nameValidator},
		{Label: "description", Required: false, Value: desc, Hint: "one-line summary (optional)"},
		{Label: "expression", Required: true, Value: expr, Hint: "AND/OR/NOT over labels, e.g. status:open AND NOT type:chore", Validator: exprValidator, Note: exprNote},
	}
	f := NewForm(fmt.Sprintf("Board  %s:", code), fields)
	f.Title = fmt.Sprintf("Board  %s:", code)
	m.form = f
	m.formKind = formBoardEditor
	m.formPayload = code
	m.boardEd = ed
}

// doBoardEdit handles submit of the board editor form. It refuses to save
// when the editor's live validation says the expression is invalid or the
// name is empty — the form's own validation covers the latter, but the
// cycle/broken-expression state lives in the editor and must be re-checked
// here at submit time.
func (m *Model) doBoardEdit(vals map[string]string) tea.Cmd {
	ed := m.boardEd
	if ed == nil {
		return nil
	}
	ed.Name = vals["name"]
	ed.Description = vals["description"]
	// Re-run SetExpr with the final field value in case the last keystroke
	// was not yet reflected (it is, but this is defensive and cheap).
	ed.SetExpr(vals["expression"])
	if !ed.CanSave() {
		if err := ed.Err(); err != nil {
			m.showToast("error: " + err.Error())
		} else {
			m.showToast("error: name and expression are required")
		}
		return nil
	}
	if err := ed.Save(m.actor); err != nil {
		m.showToast("error: " + err.Error())
		return nil
	}
	m.showToast(fmt.Sprintf("saved board %s:%s", ed.code, ed.Name))
	m.refreshAll()
	return nil
}
