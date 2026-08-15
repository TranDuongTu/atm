package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// The defining property of the redesign: the list is identical from every
// context. The old menu filtered Actions through currentScopes(), so the
// Projects pane and a task detail showed disjoint sets.
func TestSpotlightListIsGlobalFromEveryContext(t *testing.T) {
	want := []string{"Add project", "Add task", "Edit title", "New board", "Channels", "Keymap reference"}

	var first string
	for _, focus := range []workspacePane{paneProjects, paneTasks} {
		m := newTestModel(t)
		m.SetSize(120, 40)
		seedProject(t, m, "ATM", "Acme")
		m.projectScope = "ATM"
		m.focused = focus

		m.spotlight.openSpotlight()
		var labels []string
		for _, r := range m.spotlight.rows {
			if r.entry != nil {
				labels = append(labels, r.entry.label)
			}
		}
		joined := strings.Join(labels, "\n")
		for _, w := range want {
			if !strings.Contains(joined, w) {
				t.Errorf("focus %v: spotlight missing %q\n%s", focus, w, joined)
			}
		}
		if first == "" {
			first = joined
		} else if joined != first {
			t.Errorf("spotlight content changed with pane focus:\n--- %v ---\n%s", focus, joined)
		}
	}
}

func TestSpotlightRowsAreKeyFirst(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	seedProject(t, m, "ATM", "Acme")
	m.projectScope = "ATM"
	m.focused = paneTasks
	m.spotlight.openSpotlight()
	walkTo(t, m, "Add task")

	view := m.spotlight.renderOverlay()
	for _, line := range strings.Split(view, "\n") {
		if !strings.Contains(line, "Add task") {
			continue
		}
		key := strings.Index(line, "[a]")
		label := strings.Index(line, "Add task")
		if key < 0 || key > label {
			t.Errorf("row must render its key before its label: %q", line)
		}
		return
	}
	t.Fatalf("no Add task row in:\n%s", view)
}

func TestSpotlightOmitsBorderHintedAndHiddenRows(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	m.spotlight.openSpotlight()
	view := m.spotlight.renderOverlay()
	for _, gone := range []string{"Projects pane", "Tasks pane", "Drill into persona activity", "Quit"} {
		if strings.Contains(view, gone) {
			t.Errorf("border-hinted/hidden entry %q must not be a spotlight row:\n%s", gone, view)
		}
	}
}

func TestSpotlightHidesProjectGatedViewsWithoutProject(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	m.spotlight.openSpotlight()
	if strings.Contains(m.spotlight.renderOverlay(), "Capabilities") {
		t.Error("needsProject entry shown without a project scope")
	}
}

// Cross-pane activation: from the Projects pane, activating Add task must
// replay the prelude (2) before the key (a) and land in the task-create form.
func TestSpotlightActivationReplaysPreludeThenKey(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	seedProject(t, m, "ATM", "Acme")
	scopeTasksPane(t, m, "ATM")
	m.focused = paneProjects

	m.spotlight.openSpotlight()
	walkTo(t, m, "Add task")
	m.spotlight.handleKey(tea.KeyMsg{Type: tea.KeyRight})

	if m.spotlight.open {
		t.Error("activation must close the spotlight")
	}
	if m.focused != paneTasks {
		t.Errorf("prelude must focus the Tasks pane, focused=%v", m.focused)
	}
	if m.form == nil || m.formKind != formTaskCreate {
		t.Errorf("activation must open the task-create form, formKind=%v", m.formKind)
	}
}

func TestSpotlightEnterAndLeftAreInertInTheList(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	seedChannels(t, m)
	m.spotlight.openSpotlight()
	walkTo(t, m, "Channels")

	m.spotlight.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if !m.spotlight.open || m.channelsOv.open {
		t.Error("Enter must be inert in the spotlight list")
	}
	m.spotlight.handleKey(tea.KeyMsg{Type: tea.KeyLeft})
	if !m.spotlight.open || m.channelsOv.open {
		t.Error("Left must be inert in the spotlight list")
	}
}

func TestSpotlightEscClosesFromTheList(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	m.spotlight.openSpotlight()
	m.spotlight.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	if m.spotlight.open {
		t.Error("Esc from the list must close the spotlight")
	}
}

// walkTo moves the spotlight cursor onto the row with the given label.
func walkTo(t *testing.T, m *Model, label string) {
	t.Helper()
	for i := 0; i < len(m.spotlight.rows)+2; i++ {
		if m.spotlight.selectedLabel() == label {
			return
		}
		m.spotlight.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	}
	t.Fatalf("never reached the %q row", label)
}
