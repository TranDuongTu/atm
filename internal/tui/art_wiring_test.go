package tui

import (
	"strings"
	"testing"
)

// TestArtTickAdvancesPhaseOnWorkspace proves artTickMsg advances m.artPhase
// while the plain workspace is showing, and freezes it once an overlay
// (help, in this case) covers the workspace — matching View()'s dispatch.
func TestArtTickAdvancesPhaseOnWorkspace(t *testing.T) {
	m := newTestModel(t)
	before := m.artPhase
	m.Update(artTickMsg{})
	if m.artPhase != before+1 {
		t.Fatalf("phase = %d, want %d", m.artPhase, before+1)
	}

	// Overlay open: phase freezes.
	m.helpOverlay = helpKeys
	frozen := m.artPhase
	m.Update(artTickMsg{})
	if m.artPhase != frozen {
		t.Fatal("phase must not advance while an overlay is open")
	}
}

// TestArtTickAlwaysReschedules proves the tick command keeps firing even
// while frozen (off-workspace), so art resumes the instant the overlay
// closes rather than needing another trigger to restart the loop.
func TestArtTickAlwaysReschedules(t *testing.T) {
	m := newTestModel(t)
	m.helpOverlay = helpKeys
	_, cmd := m.Update(artTickMsg{})
	if cmd == nil {
		t.Fatal("Update(artTickMsg) returned nil cmd even while frozen; want next-tick scheduling")
	}
}

// TestWorkspaceIdleMatchesViewDispatch proves workspaceIdle() flips false for
// every overlay condition View() checks before rendering something other
// than the plain workspace.
func TestWorkspaceIdleMatchesViewDispatch(t *testing.T) {
	fresh := func(t *testing.T) *Model { return newTestModel(t) }

	if !fresh(t).workspaceIdle() {
		t.Fatal("fresh model should be workspace-idle")
	}

	t.Run("help overlay", func(t *testing.T) {
		m := fresh(t)
		m.helpOverlay = helpKeys
		if m.workspaceIdle() {
			t.Fatal("workspaceIdle should be false under help overlay")
		}
	})
	t.Run("confirm", func(t *testing.T) {
		m := fresh(t)
		m.confirm = confirmRemoveProject
		if m.workspaceIdle() {
			t.Fatal("workspaceIdle should be false under confirm")
		}
	})
	t.Run("form active", func(t *testing.T) {
		m := fresh(t)
		m.form = NewForm("t", nil)
		if m.workspaceIdle() {
			t.Fatal("workspaceIdle should be false with an active form")
		}
	})
	t.Run("plugin overlay", func(t *testing.T) {
		m := fresh(t)
		m.pluginOverlay = 0
		if m.workspaceIdle() {
			t.Fatal("workspaceIdle should be false with a plugin overlay open")
		}
	})
	t.Run("capability overlay", func(t *testing.T) {
		m := fresh(t)
		m.capability.open = true
		if m.workspaceIdle() {
			t.Fatal("workspaceIdle should be false with the capability overlay open")
		}
	})
	t.Run("dispatch dialog", func(t *testing.T) {
		m := fresh(t)
		m.dispatchDlg.kind = dispatchManager
		if m.workspaceIdle() {
			t.Fatal("workspaceIdle should be false with the dispatch dialog open")
		}
	})
	t.Run("personas overlay", func(t *testing.T) {
		m := fresh(t)
		m.personasOv.open = true
		if m.workspaceIdle() {
			t.Fatal("workspaceIdle should be false with the personas overlay open")
		}
	})
}

// TestArtStylesExistInAllThemes proves ArtBase/ArtAccent are wired to
// distinct palette tones (Subtle vs Accent) in every theme.
func TestArtStylesExistInAllThemes(t *testing.T) {
	for _, name := range themeOrder {
		st := buildStyles(name)
		if st.ArtBase.GetForeground() == st.ArtAccent.GetForeground() {
			t.Fatalf("theme %v: art base and accent must differ", name)
		}
	}
}

// TestProjectsRenderListIncludesArtRegion proves the art region between the
// list and events/summary sections is populated with a non-blank glyph on a
// tall pane, not dead padding.
func TestProjectsRenderListIncludesArtRegion(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme Task Manager")
	m.projects.SetSize(60, 40)
	// Art is default-off and scoped: enable it for the scoped project before
	// asserting the art region is populated.
	m.projectScope = "ATM"
	if err := m.store.SetProjectArtOn("ATM", true, []string{"galaxy", "matrix"}, m.actor); err != nil {
		t.Fatalf("SetProjectArtOn: %v", err)
	}
	m.artOn["ATM"] = true
	m.artPair["ATM"] = []string{"galaxy", "matrix"}
	out := m.projects.renderList()
	lines := strings.Split(out, "\n")
	if len(lines) != 40 {
		t.Fatalf("renderList height = %d, want 40", len(lines))
	}
	_, artH, _, _ := projectPaneSplitHeights(40)
	if artH < 3 {
		t.Skip("pane too short for art in this configuration")
	}
	found := false
	for _, ln := range lines[9 : 9+artH] {
		if strings.TrimSpace(stripANSI(ln)) != "" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("art region is blank")
	}
}

// TestProjectsRenderArtEmptyListReturnsBlank proves renderArt degrades
// cleanly (no panic, blank output) when the project list is empty.
func TestProjectsRenderArtEmptyListReturnsBlank(t *testing.T) {
	m := newTestModel(t)
	m.projects.SetSize(60, 40)
	if got := m.projects.renderArt(6); got != "" {
		t.Fatalf("renderArt on empty list = %q, want empty", got)
	}
}
