package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

// modalBorderedWidth returns the outer (border-inclusive) width of a rendered
// form view. The Dialog style has a rounded border (1 column each side) and
// Padding(0, 1) (1 column each side), so the bordered width is the inner
// content width plus 4.
func modalBorderedWidth(view string) int {
	maxW := 0
	for _, line := range strings.Split(view, "\n") {
		if w := lipgloss.Width(line); w > maxW {
			maxW = w
		}
	}
	return maxW
}

// assertNoFormLineExceeds fails the test if the rendered form's modal width
// exceeds the given ceiling. Catches the ATM-0091 regression where a long
// prefilled field value forces the modal to grow past its inherent chrome
// width and overflow the screen via overlayLineAt's fitLine truncation.
func assertNoFormLineExceeds(t *testing.T, f *Form, styles Styles, ceiling int) {
	t.Helper()
	view := f.View(styles)
	if got := modalBorderedWidth(view); got > ceiling {
		t.Errorf("form modal width %d exceeds ceiling %d (f.width=%d)\n--- view ---\n%s",
			got, ceiling, f.width, view)
	}
}

// baselineModalWidth returns the natural bordered width of a form with the
// given title and hint strings but only short field values — i.e. the width
// the modal would take from its chrome (title, divider, buttons, keymenu
// hint) alone, before any long prefilled value pushes it wider.
func baselineModalWidth(t *testing.T, styles Styles, title string, fields []formField) int {
	t.Helper()
	shortFields := make([]formField, len(fields))
	for i, fld := range fields {
		shortFields[i] = fld
		shortFields[i].Value = ""
	}
	f := NewForm(title, shortFields)
	return modalBorderedWidth(f.View(styles))
}

// TestFormLongPrefilledDescriptionDoesNotOverflowModal reproduces ATM-0091:
// opening the edit-description form on a task with a long prefilled value
// must not let the rendered modal exceed its natural chrome width. Before
// the fix the field row rendered the full prefilled value with no width
// constraint, so the modal grew to the value's full width (~330 chars) and
// overflowed the screen via overlayLineAt's fitLine truncation.
func TestFormLongPrefilledDescriptionDoesNotOverflowModal(t *testing.T) {
	styles := buildStyles(themeGraphite)
	long := strings.Repeat("abcdefghij", 33) // 330 chars, well over f.width=48
	fields := []formField{
		{Label: "description", Required: false, Value: long, Hint: "new description (empty clears)"},
	}
	ceiling := baselineModalWidth(t, styles, "Edit description", fields)
	f := NewForm("Edit description", fields)
	if f.width != 48 {
		t.Fatalf("f.width = %d, want 48 (form fixed inner width)", f.width)
	}
	assertNoFormLineExceeds(t, f, styles, ceiling)
}

// TestFormLongPrefilledTitleDoesNotOverflowModal covers the edit-title form,
// a single-line required field that also overflowed before the fix when the
// prefilled title was longer than the form inner width.
func TestFormLongPrefilledTitleDoesNotOverflowModal(t *testing.T) {
	styles := buildStyles(themeGraphite)
	long := strings.Repeat("abcdefghij", 33) // 330 chars
	fields := []formField{
		{Label: "title", Required: true, Value: long, Hint: "new task title"},
	}
	ceiling := baselineModalWidth(t, styles, "Edit title", fields)
	f := NewForm("Edit title", fields)
	assertNoFormLineExceeds(t, f, styles, ceiling)
}

// TestFormShortValueFitsModal confirms a short prefilled value appears
// untruncated and the modal stays within its chrome width — a regression
// guard against an over-aggressive truncation that would clip short values.
func TestFormShortValueFitsModal(t *testing.T) {
	styles := buildStyles(themeGraphite)
	fields := []formField{
		{Label: "title", Required: true, Value: "short title", Hint: "new task title"},
	}
	ceiling := baselineModalWidth(t, styles, "Edit title", fields)
	f := NewForm("Edit title", fields)
	view := f.View(styles)
	if !strings.Contains(view, "short title") {
		t.Fatalf("short prefilled value should appear untruncated in form view\n--- view ---\n%s", view)
	}
	assertNoFormLineExceeds(t, f, styles, ceiling)
}

// TestFormActiveCursorCellFitsModal confirms the trailing underline cursor
// cell on the active field does not itself push the modal past its chrome
// width.
func TestFormActiveCursorCellFitsModal(t *testing.T) {
	styles := buildStyles(themeGraphite)
	fields := []formField{
		{Label: "title", Required: true, Value: strings.Repeat("abcdefghij", 4), Hint: "new task title"},
	}
	ceiling := baselineModalWidth(t, styles, "Edit title", fields)
	// f is freshly created; the first field is active by default, so the
	// trailing cursor cell is rendered.
	f := NewForm("Edit title", fields)
	assertNoFormLineExceeds(t, f, styles, ceiling)
}