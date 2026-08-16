package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// The root is the launcher's whole surface: the four groups, then the inline
// global views. Pinned exactly (order included) because everything else in
// the tree hangs off it — and because the Capabilities gate is the one row
// whose presence depends on model state rather than the table.
func TestSpotlightRootRows(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	m.spotlight.openSpotlight()

	want := []string{"Project", "Task", "Board", "Reference", "Dispatch a session", "Channels", "Personas", "Cycle theme"}
	if got := rowLabels(m); !equalStrings(got, want) {
		t.Errorf("root rows without a project scope =\n%v\nwant\n%v", got, want)
	}

	// The capabilities switcher replays into a no-op without a project, so it
	// only becomes a row once a scope exists.
	seedProject(t, m, "ATM", "Acme")
	m.projectScope = "ATM"
	m.spotlight.openSpotlight()
	want = []string{"Project", "Task", "Board", "Reference", "Dispatch a session", "Channels", "Personas", "Capabilities", "Cycle theme"}
	if got := rowLabels(m); !equalStrings(got, want) {
		t.Errorf("root rows with a project scope =\n%v\nwant\n%v", got, want)
	}
}

// Enter drills into a group; Esc peels exactly one layer per press — group
// back to root, then root closes.
func TestSpotlightEnterDrillsAndEscPeels(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	m.spotlight.openSpotlight()

	moveCursorToLabel(t, m, "Project")
	m.spotlight.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if m.spotlight.level != levelGroup || m.spotlight.group != groupProject {
		t.Fatalf("Enter on Project must drill in: level=%v group=%v", m.spotlight.level, m.spotlight.group)
	}
	got := rowLabels(m)
	if !equalStrings(got, []string{"Add project", "Select project", "Remove project", "Set project name", "Dispatch this persona"}) {
		t.Errorf("Project group rows = %v", got)
	}

	m.spotlight.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	if m.spotlight.level != levelRoot || !m.spotlight.open {
		t.Fatalf("Esc from a group must return to the root: level=%v open=%v", m.spotlight.level, m.spotlight.open)
	}
	m.spotlight.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	if m.spotlight.open {
		t.Error("Esc from the bare root must close the launcher")
	}
}

// The Task group is a search surface: only the group-level Add task entry
// plus the hint that tells the user to type. The per-task actions belong to
// a chosen task (levelTaskActions), not to the group.
func TestSpotlightTaskGroupIsASearchSurface(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	m.spotlight.openSpotlight()
	moveCursorToLabel(t, m, "Task")
	m.spotlight.handleKey(tea.KeyMsg{Type: tea.KeyEnter})

	if got := rowLabels(m); !equalStrings(got, []string{"Add task", "select a project first"}) {
		t.Errorf("Task group without a project scope = %v", got)
	}

	seedProject(t, m, "ATM", "Acme")
	m.projectScope = "ATM"
	m.spotlight.buildRows()
	if got := rowLabels(m); !equalStrings(got, []string{"Add task", "type to find a task…"}) {
		t.Errorf("Task group with a project scope = %v", got)
	}

	// The hint is not landable: the cursor stays on Add task.
	m.spotlight.handleKey(tea.KeyMsg{Type: tea.KeyDown})
	if got := m.spotlight.selectedLabel(); got != "Add task" {
		t.Errorf("down must skip the hint row, selected %q", got)
	}
}

// Esc unwinds the typed query before it unwinds the level: a user who typed
// a wrong query must not be thrown out of the group they were browsing.
func TestSpotlightEscClearsQueryFirst(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	m.spotlight.openSpotlight()

	m.spotlight.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	m.spotlight.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("b")})
	if m.spotlight.query != "ab" {
		t.Fatalf("printable keys must type into the query, query=%q", m.spotlight.query)
	}

	m.spotlight.handleKey(tea.KeyMsg{Type: tea.KeyBackspace})
	if m.spotlight.query != "a" {
		t.Fatalf("backspace must trim the query, query=%q", m.spotlight.query)
	}
	m.spotlight.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("b")})

	m.spotlight.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	if m.spotlight.query != "" {
		t.Errorf("Esc must clear the query, query=%q", m.spotlight.query)
	}
	if !m.spotlight.open || m.spotlight.level != levelRoot {
		t.Errorf("Esc that clears a query must not also close or pop a level: open=%v level=%v", m.spotlight.open, m.spotlight.level)
	}
}

// A static entry activates through the replay path: the launcher closes and
// the entry's key runs through Model.handleKey, exactly as if the user had
// pressed it. Driven through the outer m.handleKey so the whole real path
// (dispatchKey -> spotlight -> replay -> return wrapper) is exercised.
func TestSpotlightStaticActivationReplays(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	before := m.themeName

	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("\\")})
	if !m.spotlight.open {
		t.Fatal("\\ must open the spotlight")
	}
	moveCursorToLabel(t, m, "Cycle theme")
	m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})

	if m.themeName == before {
		t.Errorf("activating Cycle theme must replay T, themeName still %q", m.themeName)
	}
	if m.spotlight.open {
		t.Error("activating a static entry must close the spotlight")
	}
}

// Tab moves focus to the preview and back; `\` closes from anywhere inside.
func TestSpotlightTabTogglesFocusAndBackslashCloses(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	m.spotlight.openSpotlight()

	m.spotlight.handleKey(tea.KeyMsg{Type: tea.KeyTab})
	if m.spotlight.focus != focusPreview {
		t.Fatal("Tab must focus the preview")
	}
	m.spotlight.handleKey(tea.KeyMsg{Type: tea.KeyTab})
	if m.spotlight.focus != focusList {
		t.Fatal("Tab must return focus to the list")
	}

	moveCursorToLabel(t, m, "Project")
	m.spotlight.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	m.spotlight.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("\\")})
	if m.spotlight.open {
		t.Error("\\ must close the launcher from inside a group")
	}
}

// The defining property of the redesign: the launcher is identical from every
// context. The old menu filtered Actions through currentScopes(), so the
// Projects pane, the Tasks pane, a task detail, and the persona drill showed
// disjoint sets. These are the four currentScopes() states the spec's
// Testing section names; the two most likely to reintroduce contextual
// filtering (a task detail's tasks.view, and the persona drill's
// personaDrilled) are exactly the two a two-state guard would miss.
func TestSpotlightListIsGlobalFromEveryContext(t *testing.T) {
	states := []struct {
		name  string
		setup func(t *testing.T) *Model
	}{
		{"Projects pane", func(t *testing.T) *Model {
			m := newTestModel(t)
			m.SetSize(120, 40)
			seedProject(t, m, "ATM", "Acme")
			m.projectScope = "ATM"
			m.focused = paneProjects
			return m
		}},
		{"Tasks pane", func(t *testing.T) *Model {
			m := newTestModel(t)
			m.SetSize(120, 40)
			seedProject(t, m, "ATM", "Acme")
			m.projectScope = "ATM"
			m.focused = paneTasks
			return m
		}},
		{"task detail", func(t *testing.T) *Model {
			m := newTestModel(t)
			m.SetSize(120, 40)
			seedProject(t, m, "ATM", "Acme")
			m.projectScope = "ATM"
			tk := seedTask(t, m, "ATM", "task one")
			m.focused = paneTasks
			m.tasks.openDetail(tk.ID)
			return m
		}},
		{"persona drill", func(t *testing.T) *Model {
			m := mkActorsOverlayTestModel(t)
			m.SetSize(120, 40)
			m.projectScope = "ATM"
			m.focused = paneProjects
			// The chart renders from the refresh-time snapshot, so a directly
			// assigned scope needs a refresh before ctrl+right can drill in.
			m.refreshAll()
			update(t, m, "ctrl+right")
			if !m.projects.personaDrilled {
				t.Fatalf("setup: ctrl+right must drill into persona detail")
			}
			return m
		}},
	}

	var first []string
	for _, st := range states {
		m := st.setup(t)
		m.spotlight.openSpotlight()
		got := rowLabels(m)
		if first == nil {
			first = got
			continue
		}
		if !equalStrings(got, first) {
			t.Errorf("root rows changed in %s:\n%v\nwant\n%v", st.name, got, first)
		}
	}
}

// Border-hinted and keymap-only entries are documented in the keymap
// reference and must never become rows — at the root or inside any group.
func TestSpotlightOmitsBorderHintedAndHiddenRows(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	seedProject(t, m, "ATM", "Acme")
	m.projectScope = "ATM"

	var labels []string
	m.spotlight.openSpotlight()
	labels = append(labels, rowLabels(m)...)
	for _, g := range menuGroups {
		m.spotlight.openSpotlight()
		moveCursorToLabel(t, m, g.label)
		m.spotlight.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
		labels = append(labels, rowLabels(m)...)
	}
	joined := strings.Join(labels, "\n")
	for _, gone := range []string{"Projects pane", "Tasks pane", "Drill into persona activity", "Quit"} {
		if strings.Contains(joined, gone) {
			t.Errorf("border-hinted/hidden entry %q must not be a spotlight row:\n%s", gone, joined)
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
	m.spotlight.handleKey(tea.KeyMsg{Type: tea.KeyEnter})

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

// Regression: the Capabilities row's handler (app.go's "C" case) requires
// the Tasks pane to be focused, but the entry used to carry no scopes, so
// activate() replayed a bare "C" into whatever pane was already focused — a
// silent no-op from the Projects pane. The entry now carries
// scopes: []menuScope{scopeTasksList}, so activation replays the {"2"}
// prelude first.
func TestActivateCapabilitiesFromProjectsPane(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	seedProject(t, m, "ATM", "Acme")
	m.projectScope = "ATM"
	m.focused = paneProjects

	m.spotlight.openSpotlight()
	walkTo(t, m, "Capabilities")
	m.spotlight.handleKey(tea.KeyMsg{Type: tea.KeyEnter})

	if !m.capability.open {
		t.Fatal("activating Capabilities from the Projects pane must open the capabilities switcher")
	}
	if m.focused != paneTasks {
		t.Errorf("activating Capabilities must focus the Tasks pane first, focused=%v", m.focused)
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

// rowLabels is the launcher's current level as plain text, in row order.
func rowLabels(m *Model) []string {
	out := make([]string, 0, len(m.spotlight.rows))
	for _, r := range m.spotlight.rows {
		out = append(out, r.label())
	}
	return out
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// moveCursorToLabel arrows the cursor down onto the row labelled want at the
// current level. Arrows, never j/k: inside the launcher every printable key
// types into the query.
func moveCursorToLabel(t *testing.T, m *Model, want string) {
	t.Helper()
	for i := 0; i <= len(m.spotlight.rows); i++ {
		if m.spotlight.selectedLabel() == want {
			return
		}
		m.spotlight.handleKey(tea.KeyMsg{Type: tea.KeyDown})
	}
	t.Fatalf("never reached the %q row in %v", want, rowLabels(m))
}

// walkTo moves the cursor onto the row for a menu entry's label, drilling
// into the group that owns it first — the launcher is a tree now, so only the
// four groups and the inline global views live at the root.
func walkTo(t *testing.T, m *Model, label string) {
	t.Helper()
	for i := range menuEntries {
		e := &menuEntries[i]
		if e.hidden || e.label != label {
			continue
		}
		if g := groupByID(e.group); g != nil {
			moveCursorToLabel(t, m, g.label)
			m.spotlight.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
		}
		moveCursorToLabel(t, m, label)
		return
	}
	t.Fatalf("no menu entry labelled %q", label)
}
