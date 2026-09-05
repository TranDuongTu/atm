package tui

import (
	"strings"
	"testing"

	"atm/internal/core"

	tea "github.com/charmbracelet/bubbletea"
)

// seedProfilesProject gives the readiness table something to grade: a
// profile-origin roster of actions over the matrix project's channels.
func seedProfilesProject(t *testing.T, m *Model) {
	t.Helper()
	seedMatrixProject(t, m)
	for _, cl := range []core.ChecklistRecord{
		{Name: "planning", Purpose: "the weekly pass", Steps: []core.ChecklistStep{{Text: "sweep"}},
			Suits: []string{"manager"}, Origin: "scrumban@1.0.0"},
		{Name: "scrum-coding", Purpose: "implement one increment", Steps: []core.ChecklistStep{{Text: "build"}},
			Suits: []string{"developer"}, Requires: core.ChecklistRequires{Channels: []string{"code"}},
			Target: core.ChecklistTargetTask, Origin: "scrumban@1.0.0"},
		{Name: "attest", Purpose: "verify the channels on this agent", Steps: []core.ChecklistStep{{Text: "reach"}},
			Suits: []string{"manager"}, Origin: "scrumban@1.0.0"},
	} {
		if _, err := m.store.CreateChecklist("ATM", cl, testActor); err != nil {
			t.Fatal(err)
		}
	}
	m.refreshAll()
}

// TestProfilesOverlayRendersAppliedProfilesAndTheReadinessTable: the overlay
// is the TUI twin of `atm profile status` — what is applied, and how far each
// action gets PER AGENT, since that is the question a dispatch asks.
func TestProfilesOverlayRendersAppliedProfilesAndTheReadinessTable(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	seedProfilesProject(t, m)

	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("P")})
	if !m.profilesOv.open {
		t.Fatal("P must open the profiles overlay")
	}
	view := m.profilesOv.renderOverlay()
	for _, want := range []string{"scrumban@1.0.0", "in sync", "action", "persona", "claude", "codex", "planning", "scrum-coding"} {
		if !strings.Contains(view, want) {
			t.Errorf("overlay missing %q:\n%s", want, view)
		}
	}
}

// TestProfilesOverlayReasonChainNamesTheCommand: a rung says WHERE an action
// stopped; the chain says what to type. Without the command the overlay would
// diagnose without helping, which is the failure mode a read-only surface has
// to avoid.
func TestProfilesOverlayReasonChainNamesTheCommand(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	seedProfilesProject(t, m)

	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("P")})
	m.profilesOv.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if !m.profilesOv.expanded {
		t.Fatal("enter must expand the reason chain")
	}
	view := m.profilesOv.renderOverlay()
	if !strings.Contains(view, "atm ") {
		t.Fatalf("the chain must name a command to run:\n%s", view)
	}
	// It is agent-relative: each configured agent gets its own verdict.
	for _, want := range []string{"claude:", "codex:"} {
		if !strings.Contains(view, want) {
			t.Errorf("the chain must answer per agent, missing %q:\n%s", want, view)
		}
	}
	m.profilesOv.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	if m.profilesOv.expanded || !m.profilesOv.open {
		t.Fatal("esc must return to the table, not close the overlay")
	}
}

// TestProfilesOverlayDispatchKeyOpensTheSelectedAction: the overlay fixes
// nothing itself — [d] hands the dispatch to the dialog, the one place a
// session is bound.
func TestProfilesOverlayDispatchKeyOpensTheSelectedAction(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	seedProfilesProject(t, m)
	m.agentOptionsFn = testAgents
	m.dispatcher = &fakeDispatcher{preview: "window"}

	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("P")})
	want := m.profilesOv.selected().Name
	m.profilesOv.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")})
	if m.profilesOv.open {
		t.Fatal("d must close the overlay")
	}
	if !m.dispatchDlg.active {
		t.Fatal("d must open the dispatch dialog")
	}
	if got := m.dispatchDlg.action(); got == nil || got.Name != want {
		t.Fatalf("dialog action = %v, want the selected %q", got, want)
	}
}

// [v] prefills attest, the same fix-it the channels overlay offers, so one
// verification action is reachable from wherever the user noticed the gap.
func TestProfilesOverlayAttestKeyPrefillsAttest(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	seedProfilesProject(t, m)
	m.agentOptionsFn = testAgents
	m.dispatcher = &fakeDispatcher{preview: "window"}

	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("P")})
	m.profilesOv.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("v")})
	if got := m.dispatchDlg.action(); got == nil || got.Name != "attest" {
		t.Fatalf("dialog action = %v, want attest prefilled", got)
	}
}

// TestProfilesOverlayIsReadOnly: every fix is a named command or a dispatch.
// The overlay must not write, so no key may reach the store.
func TestProfilesOverlayIsReadOnly(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	seedProfilesProject(t, m)
	before, _ := m.store.StoreStats("ATM")

	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("P")})
	for _, k := range []string{"j", "k", "g", "x", "a", "s", "r", "enter"} {
		m.profilesOv.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)})
	}
	if after, _ := m.store.StoreStats("ATM"); after.EventCount != before.EventCount {
		t.Fatalf("event count %d -> %d; the overlay must write nothing", before.EventCount, after.EventCount)
	}
}

// TestProfilesOverlayWithNoProject explains itself rather than rendering an
// empty table, which is indistinguishable from a broken one.
func TestProfilesOverlayWithNoProject(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	m.profilesOv.m = m
	m.profilesOv.openOverlay("")
	if !strings.Contains(m.profilesOv.renderOverlay(), "no project selected") {
		t.Fatalf("overlay must say why it is empty:\n%s", m.profilesOv.renderOverlay())
	}
}

// TestStatusGlyphCountsDegradedActionsForTheDefaultAgent: §3.11 asks for ONE
// aggregate passive signal, with the drill-in behind [P]. It reads the
// snapshot refreshAll took — a status line runs every frame and cannot ask.
func TestStatusGlyphCountsDegradedActionsForTheDefaultAgent(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	seedProfilesProject(t, m)
	if err := m.store.SetSelectedAgent("claude", testActor); err != nil {
		t.Fatal(err)
	}
	m.refreshAll()

	if m.profileAgent != "claude" {
		t.Fatalf("glyph agent = %q, want the selected claude", m.profileAgent)
	}
	if m.profileDegraded != 3 {
		t.Fatalf("degraded = %d, want all 3 actions (nothing is attested here)", m.profileDegraded)
	}
	if !strings.Contains(m.renderStatusLine(), "⚠ profile: 3 degraded [P]") {
		t.Fatalf("the status line must carry the aggregate glyph:\n%s", m.renderStatusLine())
	}
}

// With no project scoped there is nothing to grade, so the glyph is absent —
// a warning that is always on teaches nothing.
func TestStatusGlyphAbsentWithoutAProject(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	m.refreshAll()
	if m.profileDegraded != 0 {
		t.Fatalf("degraded = %d, want 0 with no project", m.profileDegraded)
	}
	if strings.Contains(m.renderStatusLine(), "profile:") {
		t.Fatalf("no project means no readiness glyph:\n%s", m.renderStatusLine())
	}
}
