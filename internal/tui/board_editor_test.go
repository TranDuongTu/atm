package tui

import (
	"strings"
	"testing"
)

// TestBoardEditorLiveValidation drives the board editor directly: a valid
// expression reports a live match count and is saveable; a malformed one is
// neither valid nor saveable. This is what makes the expression language
// usable by someone who does not know its syntax.
func TestBoardEditorLiveValidation(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", testActor)
	_, _ = s.CreateTask("ATM", "a", "", []string{"ATM:status:open"}, testActor)
	_, _ = s.CreateTask("ATM", "b", "", []string{"ATM:status:done"}, testActor)

	ed := newBoardEditor(s, "ATM")
	ed.SetExpr("status:open")
	if !ed.Valid() {
		t.Fatalf("valid expression reported invalid: %v", ed.Err())
	}
	if ed.MatchCount() != 1 {
		t.Errorf("MatchCount = %d, want 1", ed.MatchCount())
	}

	ed.SetExpr("status:open AND")
	if ed.Valid() {
		t.Error("malformed expression must be invalid")
	}
	if ed.CanSave() {
		t.Error("an invalid expression must not be saveable")
	}
}

// TestBoardEditorCanSaveGatesOnName ensures Save is refused when the name is
// empty even if the expression is valid: a board with no name cannot exist.
func TestBoardEditorCanSaveGatesOnName(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", testActor)
	_, _ = s.CreateTask("ATM", "a", "", []string{"ATM:status:open"}, testActor)

	ed := newBoardEditor(s, "ATM")
	ed.SetExpr("status:open")
	if !ed.Valid() {
		t.Fatalf("valid expression reported invalid: %v", ed.Err())
	}
	if ed.CanSave() {
		t.Error("an empty-named board must not be saveable even with a valid expr")
	}
	ed.Name = "next-sprint"
	if !ed.CanSave() {
		t.Error("a named board with a valid expr must be saveable")
	}
}

// TestBoardEditorSaveCreatesBoard confirms Save round-trips through LabelAdd
// so the new board appears in the registry with its expression.
func TestBoardEditorSaveCreatesBoard(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", testActor)
	_, _ = s.CreateTask("ATM", "a", "", []string{"ATM:status:open"}, testActor)

	ed := newBoardEditor(s, "ATM")
	ed.Name = "next-sprint"
	ed.Description = "the sprint board"
	ed.SetExpr("status:open")
	if err := ed.Save(testActor); err != nil {
		t.Fatalf("Save: %v", err)
	}
	l, err := s.LabelShow("ATM:next-sprint")
	if err != nil {
		t.Fatalf("LabelShow: %v", err)
	}
	if l.Expr != "status:open" {
		t.Errorf("saved Expr = %q want status:open", l.Expr)
	}
	if l.Description != "the sprint board" {
		t.Errorf("saved Description = %q want %q", l.Description, "the sprint board")
	}
}

// TestBoardsTabNewBoardEditor drives the [n]ew key through the full TUI
// harness: typing a name + expression, watching the live match count, and
// submitting creates the board in the registry.
func TestBoardsTabNewBoardEditor(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	seedTask(t, m, "ATM", "open", "ATM:status:open")
	update(t, m, "s")
	m.boards.handleKey(keyMsg("n")) // new board editor form
	if m.form == nil || m.formKind != formBoardEditor {
		t.Fatalf("board editor form not open: form=%v kind=%v", m.form, m.formKind)
	}
	if m.boardEd == nil {
		t.Fatalf("boardEditor not attached to model")
	}
	// Field 0 is name; type "next-sprint".
	for _, r := range "next-sprint" {
		update(t, m, string(r))
	}
	// Tab to field 2 (expression) — tab past description.
	update(t, m, "tab")
	update(t, m, "tab")
	for _, r := range "status:open" {
		update(t, m, string(r))
	}
	// Rendering the form drives the expression field's live validator
	// (and Note), which is how the real TUI updates the editor state on
	// every keystroke — the form recomputes per-field errors/notes in View.
	_ = m.form.View(m.styles)
	// The live editor must now report a valid expression with 1 match.
	if !m.boardEd.Valid() {
		t.Fatalf("expression should be valid: %v", m.boardEd.Err())
	}
	if got := m.boardEd.MatchCount(); got != 1 {
		t.Errorf("MatchCount = %d want 1", got)
	}
	// The form view must render the live success line for the count.
	v := m.form.View(m.styles)
	if !strings.Contains(v, "1 task") {
		t.Errorf("form view missing live count: %s", v)
	}
	// Submit (Enter on the last field submits directly).
	update(t, m, "enter")
	if m.form != nil {
		t.Fatalf("form should be closed after submit: %v", m.form)
	}
	l, err := m.store.LabelShow("ATM:next-sprint")
	if err != nil {
		t.Fatalf("board not saved: %v", err)
	}
	if l.Expr != "status:open" {
		t.Errorf("saved Expr = %q want status:open", l.Expr)
	}
}

// TestBoardsTabNewBoardEditorRefusesInvalidExpr confirms a malformed
// expression cannot be saved: pressing Enter on the last field does not
// submit, and the error is shown live.
func TestBoardsTabNewBoardEditorRefusesInvalidExpr(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	seedTask(t, m, "ATM", "open", "ATM:status:open")
	update(t, m, "s")
	m.boards.handleKey(keyMsg("n"))
	for _, r := range "next-sprint" {
		update(t, m, string(r))
	}
	update(t, m, "tab")
	update(t, m, "tab")
	for _, r := range "status:open AND" {
		update(t, m, string(r))
	}
	if m.boardEd.Valid() {
		t.Fatal("malformed expression must be invalid")
	}
	v := m.form.View(m.styles)
	if !strings.Contains(v, "x ") {
		t.Errorf("form view missing error marker for invalid expr: %s", v)
	}
	// Enter on the last field would submit directly; the form must refuse.
	update(t, m, "enter")
	if m.form == nil {
		t.Fatalf("form must not close on invalid submit")
	}
	if _, err := m.store.LabelShow("ATM:next-sprint"); err == nil {
		t.Errorf("invalid-expr board must not be saved")
	}
}

// TestBoardsTabEditBoardEditorPrefills confirms [e]dit opens the editor
// pre-filled with the board under the cursor, and the live count shows.
func TestBoardsTabEditBoardEditorPrefills(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	if err := m.store.LabelAdd("ATM:next-sprint", "the sprint board", "status:open", m.actor); err != nil {
		t.Fatal(err)
	}
	seedTask(t, m, "ATM", "open", "ATM:status:open")
	update(t, m, "s")
	cursorToBoardRow(t, m, "next-sprint")
	m.boards.handleKey(keyMsg("e"))
	if m.form == nil || m.formKind != formBoardEditor {
		t.Fatalf("board editor form not open: form=%v kind=%v", m.form, m.formKind)
	}
	if got := m.form.Fields[0].Value; got != "next-sprint" {
		t.Errorf("name field = %q want next-sprint", got)
	}
	if got := m.form.Fields[1].Value; got != "the sprint board" {
		t.Errorf("description field = %q want %q", got, "the sprint board")
	}
	if got := m.form.Fields[2].Value; got != "status:open" {
		t.Errorf("expression field = %q want status:open", got)
	}
	if !m.boardEd.Valid() {
		t.Fatalf("prefilled expression should be valid: %v", m.boardEd.Err())
	}
	if got := m.boardEd.MatchCount(); got != 1 {
		t.Errorf("prefilled MatchCount = %d want 1", got)
	}
	v := m.form.View(m.styles)
	if !strings.Contains(v, "1 task") {
		t.Errorf("form view missing live count for prefilled expr: %s", v)
	}
}
