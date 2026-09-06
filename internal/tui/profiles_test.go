package tui

import (
	"strings"
	"testing"

	"atm/internal/core"
	"atm/internal/profile"
	"atm/profiles"

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
	if !m.profilesOv.detail {
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
	if m.profilesOv.detail || !m.profilesOv.open {
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

// seedScrumbanChecklist creates the named checklist exactly as the embedded
// scrumban@1.0.0 ships it for project ATM, so readiness reads it as in sync.
// seedProfilesProject's hand-written records are all "modified" against the
// real profile, which is fine for rung tests but useless for sync tests.
func seedScrumbanChecklist(t *testing.T, m *Model, name string) core.ChecklistRecord {
	t.Helper()
	fsys, ok := profiles.FS("scrumban")
	if !ok {
		t.Fatal("scrumban is not embedded")
	}
	p, err := profile.Load(fsys)
	if err != nil {
		t.Fatal(err)
	}
	doc, ok := p.ForProject("ATM").ProfileChecklist(name)
	if !ok {
		t.Fatalf("scrumban ships no checklist %s", name)
	}
	doc.Origin = "scrumban@1.0.0"
	if _, err := m.store.CreateChecklist("ATM", doc, testActor); err != nil {
		t.Fatal(err)
	}
	m.refreshAll()
	return doc
}

// TestProfilesOverlayListShowsOriginAndTheModifiedMark: the origin column and
// the ~ mark are read from RecordSync, and the summary line folds the same
// facts — one computation, three renderings.
func TestProfilesOverlayListShowsOriginAndTheModifiedMark(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	seedMatrixProject(t, m)
	seedScrumbanChecklist(t, m, "planning")
	edited := seedScrumbanChecklist(t, m, "standup")
	edited.Purpose = "edited locally"
	if err := m.store.SetChecklist("ATM", "standup", edited, testActor); err != nil {
		t.Fatal(err)
	}
	if _, err := m.store.CreateChecklist("ATM", core.ChecklistRecord{
		Name: "my-routine", Purpose: "mine", Steps: []core.ChecklistStep{{Text: "do"}}}, testActor); err != nil {
		t.Fatal(err)
	}
	m.refreshAll()

	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("P")})
	view := m.profilesOv.renderOverlay()
	for _, want := range []string{"origin", "scrumban@1.0.0 ~", "3 checklists", "2 from scrumban@1.0.0 (1 modified)", "1 user"} {
		if !strings.Contains(view, want) {
			t.Errorf("overlay missing %q:\n%s", want, view)
		}
	}
	for _, line := range strings.Split(view, "\n") {
		if strings.Contains(line, "planning") && strings.Contains(line, "~") {
			t.Errorf("planning is in sync and must carry no mark: %q", line)
		}
		if strings.Contains(line, "my-routine") && !strings.Contains(line, "user") {
			t.Errorf("my-routine must show origin user: %q", line)
		}
	}
	if m.profilesOv.syncs["standup"].State != "modified" || m.profilesOv.records["my-routine"].Origin != "user" {
		t.Fatalf("caches: syncs=%+v records=%+v", m.profilesOv.syncs, m.profilesOv.records)
	}
}

// TestProfilesOverlayEnterOpensTheChecklistRecord: one keystroke answers both
// what the checklist is and how far it gets — the record on top, the
// per-agent chain below. Esc returns to the list, not to the workspace.
func TestProfilesOverlayEnterOpensTheChecklistRecord(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	seedMatrixProject(t, m)
	seedScrumbanChecklist(t, m, "planning")

	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("P")})
	m.profilesOv.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if !m.profilesOv.detail {
		t.Fatal("Enter must open the record detail")
	}
	view := m.profilesOv.renderOverlay()
	for _, want := range []string{"Checklist: planning", "origin", "scrumban@1.0.0 · in sync", "suits", "manager", "target project", "purpose", "1. "} {
		if !strings.Contains(view, want) {
			t.Errorf("detail missing %q:\n%s", want, view)
		}
	}
	// The chain sits UNDER the record (spec §13.3), so a checklist with a
	// long step tree scrolls it past the window — assert it on the document
	// the window is cut from, not on one screenful of it.
	doc := strings.Join(m.profilesOv.detailLines(), "\n")
	for _, want := range []string{"claude:", "codex:"} {
		if !strings.Contains(doc, want) {
			t.Errorf("the chain must answer per agent, missing %q:\n%s", want, doc)
		}
	}
	m.profilesOv.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	if m.profilesOv.detail || !m.profilesOv.open {
		t.Fatal("Esc in the detail must return to the list and keep the overlay open")
	}
}

// TestProfilesOverlayDetailNamesTheDrift: a modified record says which
// fields drifted, from RecordSync.Diff — the same list the reset confirm
// will show.
func TestProfilesOverlayDetailNamesTheDrift(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	seedMatrixProject(t, m)
	edited := seedScrumbanChecklist(t, m, "planning")
	edited.Purpose = "edited locally"
	if err := m.store.SetChecklist("ATM", "planning", edited, testActor); err != nil {
		t.Fatal(err)
	}
	m.refreshAll()
	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("P")})
	m.profilesOv.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if view := m.profilesOv.renderOverlay(); !strings.Contains(view, "modified: purpose") {
		t.Fatalf("detail must name the drifted field:\n%s", view)
	}
}
