package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// An action entry with no live renderer previews its summary.
func TestPreviewFallsBackToSummary(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	m.spotlight.openSpotlight()
	walkTo(t, m, "Cycle theme")
	if got := strings.Join(m.spotlight.lines, " "); !strings.Contains(got, "next colour theme") {
		t.Errorf("action preview must show the summary, got %q", got)
	}
}

// A dialog entry previews the real overlay's content — asserted against the
// same previewBody the overlay itself renders, so the two cannot diverge.
func TestPreviewRendersLiveOverlayContent(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	seedChannels(t, m)
	m.spotlight.openSpotlight()
	walkTo(t, m, "Channels")

	got := strings.Join(m.spotlight.lines, "\n")
	want := m.channelsOv.previewBody(m.spotlight.menuBoxWidth() - 4)
	if want == "" {
		t.Fatal("channels previewBody produced nothing to compare")
	}
	for _, line := range strings.Split(strings.TrimRight(want, "\n"), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if !strings.Contains(got, strings.TrimSpace(line)) {
			t.Errorf("preview missing overlay line %q\n--- preview ---\n%s", line, got)
		}
	}
	if m.channelsOv.open {
		t.Error("previewing must not open the real overlay")
	}
}

// Carried into the fix round: the dispatch preview is the one path this task
// grew scope to make safe — dispatchModel needed a load/open split to avoid
// a panic on a fresh session (d.targets is nil until open() has run at least
// once). Hovering "Dispatch a session" before the real dialog has ever been
// opened must render real content without activating it.
func TestPreviewRendersDispatchOnColdSession(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	m.spotlight.openSpotlight()
	walkTo(t, m, "Dispatch a session")

	got := strings.Join(m.spotlight.lines, "\n")
	want := m.dispatchDlg.previewBody(m.spotlight.menuBoxWidth() - 4)
	if want == "" {
		t.Fatal("dispatch previewBody produced nothing to compare")
	}
	if !strings.Contains(got, "Agent:") {
		t.Errorf("dispatch preview missing the Agent field:\n%s", got)
	}
	if m.dispatchDlg.active {
		t.Error("previewing must not activate the dispatch dialog")
	}
}

// personasModel.entries is only populated once the overlay has actually been
// opened; on a fresh session hovering "Personas" must still show the
// built-in personas without opening the overlay.
func TestPreviewRendersPersonasOnColdSession(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	m.spotlight.openSpotlight()
	walkTo(t, m, "Personas")

	got := strings.Join(m.spotlight.lines, "\n")
	if !strings.Contains(got, "developer") {
		t.Errorf("personas preview missing the built-in developer persona:\n%s", got)
	}
	if m.personasOv.open {
		t.Error("previewing must not open the personas overlay")
	}
}

// capabilityModel is kept fresh by refreshAll rather than a load/open split
// (its entries are always current by the time the spotlight can open); this
// exercises that its entry still previews real content without opening the
// switcher.
func TestPreviewRendersCapabilitiesWithoutOpening(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	seedProject(t, m, "ATM", "Acme")
	m.projectScope = "ATM"
	m.refreshAll()
	m.spotlight.openSpotlight()
	walkTo(t, m, "Capabilities")

	got := strings.Join(m.spotlight.lines, "\n")
	if strings.TrimSpace(got) == "" {
		t.Fatal("capabilities preview produced nothing")
	}
	if m.capability.open {
		t.Error("previewing must not open the capabilities switcher")
	}
}

// A form entry previews a pristine form without touching model state.
func TestPreviewRendersPristineFormWithoutMutatingModel(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	m.spotlight.openSpotlight()
	walkTo(t, m, "Add project")

	got := strings.Join(m.spotlight.lines, "\n")
	for _, want := range []string{"code", "name"} {
		if !strings.Contains(got, want) {
			t.Errorf("project-create preview missing field %q:\n%s", want, got)
		}
	}
	if m.form != nil || m.formKind != formNone {
		t.Errorf("previewing must not install a form: form=%v kind=%v", m.form, m.formKind)
	}
}

// A reference entry previews its full content and scrolls once focused.
func TestPreviewReferenceScrollsWhenFocused(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	m.spotlight.openSpotlight()
	walkTo(t, m, "Conventions")
	if got := strings.Join(m.spotlight.lines, "\n"); !strings.Contains(got, "What ATM is") {
		t.Errorf("conventions preview missing content:\n%s", got)
	}

	m.spotlight.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if m.spotlight.focus != focusPreview {
		t.Fatal("Enter on a reference entry must focus the preview")
	}
	m.spotlight.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	if m.spotlight.offset != 1 {
		t.Errorf("j must scroll the focused preview, offset=%d", m.spotlight.offset)
	}
	// The preview footer advertises the arrows, so they must scroll too — and
	// a printable key must not type into the query here: the query belongs to
	// the list.
	m.spotlight.handleKey(tea.KeyMsg{Type: tea.KeyDown})
	if m.spotlight.offset != 2 {
		t.Errorf("down must scroll the focused preview, offset=%d", m.spotlight.offset)
	}
	m.spotlight.handleKey(tea.KeyMsg{Type: tea.KeyUp})
	if m.spotlight.offset != 1 {
		t.Errorf("up must scroll the focused preview back, offset=%d", m.spotlight.offset)
	}
	if m.spotlight.query != "" {
		t.Errorf("keys in a focused preview must not type into the query, query=%q", m.spotlight.query)
	}
	m.spotlight.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	if m.spotlight.focus != focusList || !m.spotlight.open {
		t.Error("Esc from a focused preview must return to the list, not close")
	}
}

// Carried into this task from Task 2's review: resizing the terminal while
// the spotlight is open must re-wrap the preview to the new width, not leave
// it wrapped to whatever width was current when the row was hovered.
func TestPreviewReflowsOnResize(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	m.spotlight.openSpotlight()
	walkTo(t, m, "Conventions")

	m.SetSize(60, 40)

	want := strings.Split(strings.TrimRight(renderConventionsText(m.styles, m.spotlight.menuBoxWidth()-4, conventionsTextTUI), "\n"), "\n")
	got := strings.Join(m.spotlight.lines, "\n")
	if got != strings.Join(want, "\n") {
		t.Errorf("preview did not re-wrap to the new width after resize\n--- got ---\n%s\n--- want ---\n%s", got, strings.Join(want, "\n"))
	}
}

// Carried into this task from Task 2's review: the scroll clamp must stop at
// the last screenful (len(lines)-previewHeight), not let the user scroll
// until a single line remains on screen. The clamp must hold both on the key
// path (j) and in the renderer.
func TestPreviewScrollClampsToLastScreenful(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	m.spotlight.openSpotlight()
	walkTo(t, m, "Keymap reference")
	m.spotlight.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if m.spotlight.focus != focusPreview {
		t.Fatal("Enter on a reference entry must focus the preview")
	}

	want := len(m.spotlight.lines) - m.spotlight.previewHeight()
	if want < 0 {
		want = 0
	}
	if want == 0 {
		t.Fatal("test needs a reference preview longer than one screenful")
	}

	for i := 0; i < len(m.spotlight.lines)+5; i++ {
		m.spotlight.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	}
	if m.spotlight.offset != want {
		t.Errorf("scroll offset = %d, want clamp at %d", m.spotlight.offset, want)
	}

	// The renderer's own clamp must agree (defense in depth: it must not
	// derive a different, looser bound from a stale sm.lines/height pairing).
	m.spotlight.renderOverlay()
	if m.spotlight.offset != want {
		t.Errorf("renderOverlay changed offset to %d, want %d", m.spotlight.offset, want)
	}
}
