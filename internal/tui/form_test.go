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

// TestFormActiveFieldShowsCursorForLongValue reproduces the regression
// introduced by the ATM-0091 fix's head-truncation: the form's input model
// appends runes to fld.Value and renders the cursor as a trailing underline
// cell after the value, so the cursor is always at the END of the buffer. A
// head-truncation (fitLine) shows the start of a long value and hides the
// cursor (and any newly typed runes) off the right edge — the user can no
// longer see what they type. The active field must instead show a tail
// window of the value so the trailing cursor cell and the most recently
// typed runes stay visible within the field width.
func TestFormActiveFieldShowsCursorForLongValue(t *testing.T) {
	styles := buildStyles(themeGraphite)
	// Use a distinguishable value: "AAAA...Z" so the head (A's) and tail (Z)
	// do not overlap. Head-truncation would show A's and hide the Z.
	long := strings.Repeat("A", 320) + "Z" // 321 chars; tail is "...Z"
	fields := []formField{
		{Label: "title", Required: true, Value: long, Hint: "new task title"},
	}
	f := NewForm("Edit title", fields)
	f.zone = focusFields
	f.cursor = 0

	view := f.View(styles)
	// The tail of the value (the last rune the user would see next to the
	// cursor) must be visible, not just the head of A's.
	if !strings.Contains(view, "Z") {
		t.Fatalf("tail of long value not visible in active field (cursor off-screen); expected to see 'Z' near the cursor\n--- view ---\n%s", view)
	}
	// And the cursor cell (underline-styled space) must be rendered.
	cursorCell := styles.FieldValue.Underline(true).Render(" ")
	if !strings.Contains(view, cursorCell) {
		t.Fatalf("active field's trailing cursor cell not visible in view\n--- view ---\n%s", view)
	}
}

// TestFormInactiveFieldShowsValueHeadForLongValue confirms that a long value
// on a NON-active field still shows the head (start) of the value, since
// there is no cursor to keep visible there. This preserves the original
// ATM-0091 fix's intent (no overflow) for non-focused rows while only the
// active row switches to a tail window.
func TestFormInactiveFieldShowsValueHeadForLongValue(t *testing.T) {
	styles := buildStyles(themeGraphite)
	// Distinguishable value: head is "H" + A's, tail is A's + "Z".
	long := "H" + strings.Repeat("A", 319) + "Z" // 321 chars
	fields := []formField{
		{Label: "title", Required: true, Value: long, Hint: "new task title"},
		{Label: "description", Required: false, Value: "", Hint: "new description"},
	}
	f := NewForm("Edit title", fields)
	// Focus the second field so the first (title) is inactive.
	f.zone = focusFields
	f.cursor = 1

	view := f.View(styles)
	if !strings.Contains(view, "H") {
		t.Fatalf("head of long value not visible on inactive field; expected to see 'H'\n--- view ---\n%s", view)
	}
}
