package tui

import (
	"fmt"
	"strings"
	"testing"

	"atm/internal/core"
	"atm/internal/store"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
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
			if len(first) == 0 {
				t.Fatal("no root rows to compare")
			}
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

// left is inert in both halves of the launcher. In the list it is neither a
// case in handleKey's switch nor a printable rune, so it cannot activate,
// close, navigate, or type — the assertion the deleted
// TestSpotlightEnterAndLeftAreInertInTheList used to carry, re-homed here
// because only its Enter half went stale. In a focused preview it must NOT
// hand focus back either: Tab and Esc are the two advertised exits, and a
// third undocumented one is exactly what this pins against.
func TestSpotlightLeftIsInert(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	seedChannels(t, m)

	m.spotlight.openSpotlight()
	moveCursorToLabel(t, m, "Channels")
	m.spotlight.handleKey(tea.KeyMsg{Type: tea.KeyLeft})
	if !m.spotlight.open || m.channelsOv.open {
		t.Errorf("left must not activate or close from the list: open=%v channels=%v", m.spotlight.open, m.channelsOv.open)
	}
	if m.spotlight.query != "" {
		t.Errorf("left must not type into the query, query=%q", m.spotlight.query)
	}
	if m.spotlight.level != levelRoot || m.spotlight.selectedLabel() != "Channels" {
		t.Errorf("left must not move: level=%v selected=%q", m.spotlight.level, m.spotlight.selectedLabel())
	}

	m.spotlight.openSpotlight()
	walkTo(t, m, "Conventions")
	m.spotlight.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if m.spotlight.focus != focusPreview {
		t.Fatal("setup: Enter on a reference entry must focus the preview")
	}
	m.spotlight.handleKey(tea.KeyMsg{Type: tea.KeyLeft})
	if m.spotlight.focus != focusPreview {
		t.Errorf("left must not be a third way out of a focused preview, focus=%v", m.spotlight.focus)
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

// moveCursorToGroup drills into the named root group: the cursor onto that
// group's root row, then Enter. Every Task-group test starts this way.
func moveCursorToGroup(t *testing.T, m *Model, want string) {
	t.Helper()
	moveCursorToLabel(t, m, want)
	m.spotlight.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
}

// moveCursorToTask arrows the cursor onto the task row for id. Matched on the
// row's task rather than on its label: two tasks may share a title, and the ID
// is what the per-task actions target.
func moveCursorToTask(t *testing.T, m *Model, id string) {
	t.Helper()
	for i := 0; i <= len(m.spotlight.rows); i++ {
		if r := m.spotlight.selectedRow(); r != nil && r.kind == rowTask && r.task != nil && r.task.ID == id {
			return
		}
		m.spotlight.handleKey(tea.KeyMsg{Type: tea.KeyDown})
	}
	t.Fatalf("never reached the task row for %s in %v", id, rowLabels(m))
}

// selectProject scopes the model to code the way the Projects pane's [s]
// does, without depending on that pane's cursor.
func selectProject(t *testing.T, m *Model, code string) {
	t.Helper()
	m.projectScope = code
	m.refreshAll()
}

// typeQuery types q into the launcher one rune at a time — the real keystroke
// path, which is the only way a per-keystroke rebuild bug shows up.
func typeQuery(t *testing.T, m *Model, q string) {
	t.Helper()
	for _, r := range q {
		m.spotlight.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
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

// --- the horizontal layout ---

// The launcher is a split: a short action list on the left, a full-height
// preview on the right, a search input above both, a footer under both.
// Pinned as one test because the point is that all four regions render at
// once — the old vertical stack put the preview below the fold.
func TestSpotlightRenderHorizontal(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	m.spotlight.openSpotlight()

	view := stripANSI(m.spotlight.renderOverlay())
	for _, want := range []string{
		"Actions",            // left pane header
		"Preview",            // right pane header
		"▤ Project ›",        // a group row: icon, label, drill chevron, no key column
		"[D]",                // an entry row's key column
		"↯",                  // and its icon
		"Dispatch a session", // and its label
		"[Enter] open",       // the list footer
		"> ▏",                // the search input, with its caret
	} {
		mustContain(t, view, want)
	}
}

// Focus is unmistakable: the caret belongs to the list, and the footer
// advertises the keys of whichever half owns the keystrokes.
func TestSpotlightFocusSwapsCaretAndFooter(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	m.spotlight.openSpotlight()

	view := stripANSI(m.spotlight.renderOverlay())
	mustContain(t, view, "> ▏")
	mustContain(t, view, "[Tab] preview")
	mustNotContain(t, view, "back to list")

	m.spotlight.handleKey(tea.KeyMsg{Type: tea.KeyTab})
	if m.spotlight.focus != focusPreview {
		t.Fatal("setup: Tab must focus the preview")
	}
	view = stripANSI(m.spotlight.renderOverlay())
	mustNotContain(t, view, "▏")
	mustNotContain(t, view, "[Enter] open")
	mustContain(t, view, "back to list")
	mustContain(t, view, "[↑↓/PgUp/PgDn] scroll")
}

// Routed from Task 6's review: the footer used to promise "[Esc] back" at
// every level, but a bare root has nothing to go back to — Esc closes there.
// A typed query is a layer of its own, so it turns the promise back into
// "back" without any level change.
func TestSpotlightFooterEscLabelTracksWhatEscDoes(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	m.spotlight.openSpotlight()

	view := stripANSI(m.spotlight.renderOverlay())
	mustContain(t, view, "[Esc] close")

	m.spotlight.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	view = stripANSI(m.spotlight.renderOverlay())
	mustContain(t, view, "[Esc] back")

	m.spotlight.handleKey(tea.KeyMsg{Type: tea.KeyEsc}) // clears the query
	moveCursorToLabel(t, m, "Project")
	m.spotlight.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	view = stripANSI(m.spotlight.renderOverlay())
	mustContain(t, view, "[Esc] back")
}

// A focused preview that overflows says where in the content you are; an
// unfocused one does not — the marker is a scroll cue, and only a focused
// preview is being scrolled.
func TestSpotlightPreviewScrollMarker(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	m.spotlight.openSpotlight()
	walkTo(t, m, "Conventions")

	h, n := m.spotlight.previewHeight(), len(m.spotlight.lines)
	if n <= h {
		t.Fatalf("test needs a preview longer than one screenful: %d lines, height %d", n, h)
	}
	first := fmt.Sprintf("1–%d/%d", h, n)

	if view := stripANSI(m.spotlight.renderOverlay()); strings.Contains(view, first) {
		t.Errorf("an unfocused preview must not show the scroll marker %q:\n%s", first, view)
	}

	m.spotlight.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if m.spotlight.focus != focusPreview {
		t.Fatal("setup: Enter on a reference entry must focus the preview")
	}
	mustContain(t, stripANSI(m.spotlight.renderOverlay()), first)

	// A page down, clamped to the last screenful when the content is under
	// two screenfuls tall.
	m.spotlight.handleKey(tea.KeyMsg{Type: tea.KeyPgDown})
	off := h
	if top := n - h; off > top {
		off = top
	}
	mustContain(t, stripANSI(m.spotlight.renderOverlay()), fmt.Sprintf("%d–%d/%d", off+1, off+h, n))
}

// Routed from Task 6's review: the footer advertises PgUp/PgDn, so they must
// page the preview — from the preview's own focus and from the list, where
// the preview is visible but not focused.
func TestSpotlightPageKeysScrollPreviewFromEitherFocus(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	m.spotlight.openSpotlight()
	walkTo(t, m, "Keymap reference")
	// One page, or the last screenful when the content is under two screenfuls.
	h := m.spotlight.previewHeight()
	if top := m.spotlight.maxPreviewOffset(); h > top {
		h = top
	}
	if h == 0 {
		t.Fatal("test needs a preview longer than one screenful")
	}

	m.spotlight.handleKey(tea.KeyMsg{Type: tea.KeyPgDown})
	if m.spotlight.offset != h || m.spotlight.focus != focusList {
		t.Errorf("PgDn from the list must page the preview without stealing focus: offset=%d focus=%v", m.spotlight.offset, m.spotlight.focus)
	}
	m.spotlight.handleKey(tea.KeyMsg{Type: tea.KeyPgUp})
	if m.spotlight.offset != 0 {
		t.Errorf("PgUp from the list must page back, offset=%d", m.spotlight.offset)
	}

	m.spotlight.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if m.spotlight.focus != focusPreview {
		t.Fatal("setup: Enter on a reference entry must focus the preview")
	}
	m.spotlight.handleKey(tea.KeyMsg{Type: tea.KeyPgDown})
	if m.spotlight.offset != h {
		t.Errorf("PgDn in a focused preview must page down, offset=%d", m.spotlight.offset)
	}
	m.spotlight.handleKey(tea.KeyMsg{Type: tea.KeyPgUp})
	if m.spotlight.offset != 0 {
		t.Errorf("PgUp in a focused preview must page back up, offset=%d", m.spotlight.offset)
	}
}

// Routed from Task 6's review: Tab used to focus the preview unconditionally,
// stranding the user in a blank pane where most keys do nothing. An empty
// preview is not focusable, and it says so rather than rendering as a blank
// column the user cannot tell from a broken one.
func TestSpotlightTabIgnoresAnEmptyPreview(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	m.spotlight.openSpotlight()
	m.spotlight.lines = nil

	m.spotlight.handleKey(tea.KeyMsg{Type: tea.KeyTab})
	if m.spotlight.focus != focusList {
		t.Errorf("Tab must not focus an empty preview, focus=%v", m.spotlight.focus)
	}
	mustContain(t, stripANSI(m.spotlight.renderOverlay()), "(no preview)")
}

// The two panes and the divider column exactly fill the inner width, and the
// divider lands on the same column on the header row and on every body row —
// the alignment the split depends on to read as two panes rather than as
// ragged text.
func TestSpotlightLayoutColumnsAlign(t *testing.T) {
	for _, w := range []int{64, 100, 200} {
		m := newTestModel(t)
		m.SetSize(w, 40)
		m.spotlight.openSpotlight()

		bw, leftW := m.spotlight.menuBoxWidth(), m.spotlight.leftPaneWidth()
		if leftW+spotPaneGap+m.spotlight.previewPanelWidth() != bw-4 {
			t.Errorf("width %d: list %d + gap + panel %d must fill the inner width %d",
				w, leftW, m.spotlight.previewPanelWidth(), bw-4)
		}

		lines := strings.Split(stripANSI(m.spotlight.renderOverlay()), "\n")
		if got := len(lines); got != m.spotlight.spotlightHeight() {
			t.Fatalf("width %d: overlay is %d lines, want %d", w, got, m.spotlight.spotlightHeight())
		}
		for i, line := range lines {
			if got := lipgloss.Width(line); got != bw {
				t.Errorf("width %d: line %d is %d columns, want %d: %q", w, i, got, bw, line)
			}
		}
		// The preview panel's left border replaces the old free-floating
		// divider rule: every block line must show its edge at the same
		// column, so the panel reads as one box rather than a stack.
		col := m.spotlight.panelLeftColumn()
		mustContain(t, lines[m.spotlight.blockTop()], "Actions")
		for i := m.spotlight.blockTop(); i < m.spotlight.blockTop()+m.spotlight.blockHeight(); i++ {
			r := []rune(lines[i])
			switch r[col] {
			case '╭', '│', '╰', ' ':
			default:
				t.Errorf("width %d: line %d has no panel edge at column %d: %q", w, i, col, lines[i])
			}
		}
	}
}

// The overlay used to be 80% of the terminal by 34 rows tall regardless of
// content: eight root rows sat in a box with twenty-one empty ones. Height now
// follows the content it has, so a short list gets a short box.
func TestSpotlightBoxFitsContentHeight(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	m.spotlight.openSpotlight()

	lines := strings.Split(stripANSI(m.spotlight.renderOverlay()), "\n")
	rows := len(m.spotlight.rows)
	// header + rows, the search box (3), the footer (1), two outer borders.
	want := 1 + rows + 3 + 1 + 2
	if len(lines) > want+1 {
		t.Errorf("overlay is %d lines for %d rows; content needs about %d", len(lines), rows, want)
	}
	if len(lines) >= m.height-6 {
		t.Errorf("overlay height %d still tracks the terminal (%d), not its content", len(lines), m.height)
	}
}

// Width follows the list's widest row and a fixed prose measure, so the box is
// the same size on a laptop and on a 200-column monitor rather than growing to
// fill whatever it is given.
func TestSpotlightBoxWidthIsStableAcrossTerminals(t *testing.T) {
	width := func(cols int) int {
		m := newTestModel(t)
		m.SetSize(cols, 40)
		m.spotlight.openSpotlight()
		return m.spotlight.menuBoxWidth()
	}
	narrow, wide := width(120), width(200)
	if narrow != wide {
		t.Errorf("overlay is %d columns at 120 and %d at 200; it should not grow with the terminal", narrow, wide)
	}
	if wide >= 200*80/100 {
		t.Errorf("overlay is %d columns on a 200-column terminal — still proportional", wide)
	}
}

// A preview shorter than the list centres in its column. Pinned to the top of a
// tall block it reads as content that failed to finish loading.
func TestSpotlightShortPreviewPanelCentres(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	m.spotlight.openSpotlight()
	m.spotlight.lines = []string{"one", "two"} // 2 + 2 borders = 4 of a 9-row block

	panel := m.spotlight.previewPanel(9, 20)
	if len(panel) != 9 {
		t.Fatalf("panel is %d lines, want the block's 9", len(panel))
	}
	if strings.Contains(panel[0], "╭") {
		t.Errorf("panel starts on the block's first line; it should be centred: %q", panel[0])
	}
	if strings.Contains(panel[len(panel)-1], "╰") {
		t.Errorf("panel ends on the block's last line; it should be centred: %q", panel[len(panel)-1])
	}
	var top int
	for i, l := range panel {
		if strings.Contains(l, "╭") {
			top = i
		}
	}
	if top == 0 {
		t.Fatal("no panel top border found")
	}
}

// The search input read as a caption rather than a field, and the preview
// floated beside a rule that started below the search line and ran past the
// content. Both are boxes now.
func TestSpotlightSearchAndPreviewAreBordered(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	m.spotlight.openSpotlight()

	view := stripANSI(m.spotlight.renderOverlay())
	mustContain(t, view, "╭ Search")
	mustContain(t, view, "╭ Preview")
}

// Exactly one element is bright at a time: the focused pane's header, the
// divider running alongside it, and — only while the list owns focus — the ▸
// glyph. The test profile renders styles as plain text, so the choice itself
// is what gets asserted.
// The header accent is one of the four focus cues, and it is pinned on its own:
// the spec deliberately carries four so that no single one has to be
// sufficient, which is only true if each is independently correct.
func TestSpotlightPaneHeaderAccentFollowsFocus(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	tagFocusStyles(m)
	m.spotlight.openSpotlight()

	view := stripANSI(m.spotlight.renderOverlay())
	mustContain(t, view, "<accent>Actions</accent>")
	// The preview's label lives in its panel's border now, so the border test
	// below is what pins its half of this cue.

	m.spotlight.handleKey(tea.KeyMsg{Type: tea.KeyTab})
	if m.spotlight.focus != focusPreview {
		t.Fatal("setup: Tab must focus the preview")
	}
	view = stripANSI(m.spotlight.renderOverlay())
	mustContain(t, view, "<dim>Actions</dim>")
}

// Nothing in the launcher wears reverse video. RowCursor is a bare
// lipgloss.Reverse, which renders a filled block rather than emphasis; against
// a frame that already carries the focus cue it read as far too heavy. This is
// the guard on that — tagFocusStyles still tags RowCursor, so a return to it
// shows up here rather than in a screenshot.
func TestSpotlightUsesNoReverseVideo(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	tagFocusStyles(m)
	m.spotlight.openSpotlight()

	for _, focus := range []spotFocus{focusList, focusPreview} {
		m.spotlight.focus = focus
		mustNotContain(t, stripANSI(m.spotlight.renderOverlay()), "<bright>")
	}
}

// The ▸ glyph stays put when focus moves to the preview — the cursor row is
// still where Tab returns to — but it dims: a bright selection in a pane that
// is not taking keystrokes is the exact confusion the redesign removes.
func TestSpotlightCursorGlyphDimsWithoutListFocus(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	tagFocusStyles(m)
	m.spotlight.openSpotlight()

	view := stripANSI(m.spotlight.renderOverlay())
	mustContain(t, view, "<accent>▸ </accent>")
	mustNotContain(t, view, "<muted>▸ </muted>")

	m.spotlight.handleKey(tea.KeyMsg{Type: tea.KeyTab})
	if m.spotlight.focus != focusPreview {
		t.Fatal("setup: Tab must focus the preview")
	}
	view = stripANSI(m.spotlight.renderOverlay())
	mustContain(t, view, "<muted>▸ </muted>")
	mustNotContain(t, view, "<accent>▸ </accent>")
}

// The divider's accented run starts at the pane-header row and stops where the
// focused pane's content stops, so the length of the bright segment is itself
// the cue for which pane is live.
//
// Note the degenerate case: when the two panes happen to show the same number
// of rows, switching focus leaves the divider unchanged. That is tolerable only
// because the divider is not load-bearing alone — the header accent, the glyph,
// and the caret each carry the same information, and each is pinned by its own
// test above.
func TestSpotlightBorderMarksTheFocusedPane(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	tagFocusStyles(m)
	m.spotlight.openSpotlight()

	// titledBox styles a box's whole top line, title included, so the title
	// says which frame each tag belongs to — no counting, and no degenerate
	// case where two panes happen to be the same length.
	view := stripANSI(m.spotlight.renderOverlay())
	mustContain(t, view, "<accent>╭ Search")
	mustContain(t, view, "<dim>╭ Preview")

	m.spotlight.handleKey(tea.KeyMsg{Type: tea.KeyTab})
	if m.spotlight.focus != focusPreview {
		t.Fatal("setup: Tab must focus the preview")
	}
	view = stripANSI(m.spotlight.renderOverlay())
	mustContain(t, view, "<dim>╭ Search")
	mustContain(t, view, "<accent>╭ Preview")
}

// tagFocusStyles wraps the four styles the focus cues switch between in
// identifying markers, so a render test can assert which style each element was
// actually drawn with. The test color profile renders every style as plain text
// — RowCursor.Render("x") is just "x" — so without this a render assertion
// could not tell a bright header from a dim one. A lipgloss Transform survives
// the profile, which is what makes the cue visible to a test.
func tagFocusStyles(m *Model) {
	tag := func(s lipgloss.Style, name string) lipgloss.Style {
		return s.Transform(func(v string) string { return "<" + name + ">" + v + "</" + name + ">" })
	}
	m.styles.RowCursor = tag(m.styles.RowCursor, "bright")
	m.styles.KeyMenu = tag(m.styles.KeyMenu, "accent")
	m.styles.KeyMenuDim = tag(m.styles.KeyMenuDim, "dim")
	m.styles.Muted = tag(m.styles.Muted, "muted")
}

// The title says where in the tree you are, so a drilled launcher never looks
// like the root.
func TestSpotlightBreadcrumb(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	m.spotlight.openSpotlight()
	if got := m.spotlight.breadcrumb(); got != "Spotlight" {
		t.Errorf("root breadcrumb = %q", got)
	}

	moveCursorToLabel(t, m, "Project")
	m.spotlight.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if got := m.spotlight.breadcrumb(); got != "Spotlight · Project" {
		t.Errorf("group breadcrumb = %q", got)
	}

	m.spotlight.taskID = "ATM-7"
	m.spotlight.setLevel(levelTaskActions, groupTask)
	if got := m.spotlight.breadcrumb(); got != "Spotlight · Task · ATM-7" {
		t.Errorf("task-actions breadcrumb = %q", got)
	}
}

// A group's preview is the group's contents: its hint, a blank line, then one
// line per member entry. Hovering a group must answer "what is in here?"
// rather than repeating the one-line hint the row already carries.
func TestGroupPreviewLines(t *testing.T) {
	if got := groupPreviewLines(nil, 80); got != nil {
		t.Errorf("a nil group has no preview, got %v", got)
	}

	g := groupByID(groupBoard)
	got := groupPreviewLines(g, 200)
	if len(got) < 3 {
		t.Fatalf("group preview too short: %v", got)
	}
	if got[0] != g.hint {
		t.Errorf("first line = %q, want the group hint %q", got[0], g.hint)
	}
	if got[1] != "" {
		t.Errorf("second line = %q, want a blank separator", got[1])
	}
	var want []string
	for _, e := range menuEntries {
		if e.hidden || e.group != g.id {
			continue
		}
		want = append(want, e.icon+" "+e.label+" — "+e.summary)
	}
	if len(want) == 0 {
		t.Fatal("the Board group has no member entries to preview")
	}
	if !equalStrings(got[2:], want) {
		t.Errorf("member lines =\n%v\nwant\n%v", got[2:], want)
	}

	// Every line fits the pane it is rendered into, and a line too long for it
	// is cut with an ellipsis rather than stopping mid-word — these are prose
	// summaries, so at a realistic pane width most of them overflow, and a
	// whole pane of them cut silently reads as broken rather than as cropped.
	cut := 0
	for _, line := range groupPreviewLines(g, 30) {
		if w := lipgloss.Width(line); w > 30 {
			t.Errorf("line %q is %d columns, want at most 30", line, w)
		}
		if !strings.HasSuffix(line, "…") {
			continue
		}
		cut++
		if w := lipgloss.Width(line); w != 30 {
			t.Errorf("cut line %q is %d columns, want exactly 30 — the ellipsis must replace the last column, not extend past it", line, w)
		}
	}
	if cut == 0 {
		t.Error("no line was cut at width 30, so the ellipsis went unexercised")
	}
}

// The ellipsis has to hold in the real overlay, not only in the helper: it is
// the first content the launcher shows, because the cursor opens on a group
// row.
func TestSpotlightGroupPreviewCutsWithEllipsis(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	m.spotlight.openSpotlight()
	if got := m.spotlight.selectedLabel(); got != "Project" {
		t.Fatalf("setup: the launcher must open on a group row, got %q", got)
	}

	w := m.spotlight.previewWidth()
	cut := 0
	for _, line := range m.spotlight.lines {
		if !strings.HasSuffix(line, "…") {
			continue
		}
		cut++
		if got := lipgloss.Width(line); got != w {
			t.Errorf("cut preview line %q is %d columns, want previewWidth() = %d", line, got, w)
		}
	}
	if cut == 0 {
		t.Fatalf("no preview line was cut at width %d, so this asserts nothing:\n%s", w, strings.Join(m.spotlight.lines, "\n"))
	}
}

// --- type-to-filter (Task 8) ---

// matchRank's three tiers, pinned against the real registry entry rather than
// a paraphrase: "add project" is a label prefix of "add", a label substring
// (not prefix) of "proj", and only a summary substring of "code" (the word
// appears in "a 3-6 letter code", nowhere in the label).
func TestMatchRank(t *testing.T) {
	var addProject *menuEntry
	for i := range menuEntries {
		if menuEntries[i].label == "Add project" {
			addProject = &menuEntries[i]
			break
		}
	}
	if addProject == nil {
		t.Fatal("setup: no menu entry labelled \"Add project\"")
	}
	if got := addProject.summary; got != "Create a project from a 3-6 letter code and a display name." {
		t.Fatalf("setup: Add project's summary changed, got %q", got)
	}

	for _, tc := range []struct {
		q    string
		want int
	}{
		{"add", 0},  // label prefix
		{"proj", 1}, // label substring, not prefix
		{"code", 2}, // summary substring only
		{"zzz", -1}, // no match
	} {
		if got := matchRank(*addProject, tc.q); got != tc.want {
			t.Errorf("matchRank(Add project, %q) = %d, want %d", tc.q, got, tc.want)
		}
	}
}

// Typing at the root replaces the tree with a flat, ranked list: every row is
// a match, ranks never decrease down the list, and clearing the query brings
// the group-first tree straight back.
func TestSpotlightSearchFlattens(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	m.spotlight.openSpotlight()

	for _, r := range "board" {
		m.spotlight.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	if len(m.spotlight.rows) == 0 {
		t.Fatal("typing \"board\" produced no rows")
	}
	prevRank := -1
	for _, row := range m.spotlight.rows {
		if row.kind == rowGroup {
			t.Fatalf("a filtered list must not contain group rows: %v", rowLabels(m))
		}
		if row.kind != rowEntry || row.entry == nil {
			continue
		}
		rank := matchRank(*row.entry, "board")
		if rank < 0 {
			t.Errorf("row %q has matchRank %d, want >= 0", row.label(), rank)
		}
		if rank < prevRank {
			t.Errorf("ranks must be non-decreasing: row %q rank %d after rank %d", row.label(), rank, prevRank)
		}
		prevRank = rank
	}

	m.spotlight.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	if m.spotlight.query != "" {
		t.Fatalf("Esc must clear the query, query=%q", m.spotlight.query)
	}
	if len(m.spotlight.rows) == 0 || m.spotlight.rows[0].kind != rowGroup {
		t.Errorf("clearing the query must restore the group-first tree, got %v", rowLabels(m))
	}
}

// Stability: entries tied on rank keep their original table order, not just a
// non-decreasing rank sequence (which alone would also pass a shuffle within
// a tier). Compared by table index via pointer identity rather than by label,
// since two distinct entries share the label "Remove label" (Task and
// Board groups).
func TestSpotlightSearchRankTiesKeepTableOrder(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	m.spotlight.openSpotlight()
	m.spotlight.setQuery("e") // common enough to produce same-rank ties

	tableIndex := make(map[*menuEntry]int, len(menuEntries))
	for i := range menuEntries {
		tableIndex[&menuEntries[i]] = i
	}

	prevRank, prevIdx := -1, -1
	seen := 0
	for _, row := range m.spotlight.rows {
		if row.kind != rowEntry || row.entry == nil {
			continue
		}
		seen++
		rank := matchRank(*row.entry, "e")
		idx := tableIndex[row.entry]
		if rank == prevRank && idx < prevIdx {
			t.Errorf("rank %d tie out of table order: entry at table index %d rendered after table index %d", rank, idx, prevIdx)
		}
		prevRank, prevIdx = rank, idx
	}
	if seen == 0 {
		t.Fatal("setup: \"e\" must match at least one entry")
	}
}

// levelGroup's search is registry-wide, not scoped to the group the user
// drilled into: "theme" matches "Cycle theme", a root (groupNone) entry, even
// while browsing inside the Project group.
func TestSpotlightSearchAtGroupLevelIsRegistryWide(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	m.spotlight.openSpotlight()
	moveCursorToLabel(t, m, "Project")
	m.spotlight.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if m.spotlight.level != levelGroup || m.spotlight.group != groupProject {
		t.Fatalf("setup: must be inside the Project group, level=%v group=%v", m.spotlight.level, m.spotlight.group)
	}

	m.spotlight.setQuery("theme")
	found := false
	for _, row := range m.spotlight.rows {
		if row.kind == rowEntry && row.entry != nil && row.entry.label == "Cycle theme" {
			found = true
		}
	}
	if !found {
		t.Errorf("search inside a group must reach registry-wide entries, rows = %v", rowLabels(m))
	}
}

// The Task group is a search surface of its own (Task 9 owns it): typing
// there must not flatten into the registry search Task 8 owns.
func TestSpotlightSearchExcludesGroupTask(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	m.spotlight.openSpotlight()
	moveCursorToLabel(t, m, "Task")
	m.spotlight.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if m.spotlight.level != levelGroup || m.spotlight.group != groupTask {
		t.Fatalf("setup: must be inside the Task group, level=%v group=%v", m.spotlight.level, m.spotlight.group)
	}

	m.spotlight.setQuery("add")
	if got := rowLabels(m); !equalStrings(got, []string{"Add task", "select a project first"}) {
		t.Errorf("typing inside the Task group must not flatten the registry, rows = %v", got)
	}
}

// The Capabilities entry keeps its project gate under search: it is not a
// candidate without a project scope, and becomes one once a scope exists.
// "capabilities" also appears in other entries' summaries (Seed vocabulary,
// Conventions), so the assertion is specifically about the Capabilities row,
// not about the query matching nothing at all.
func TestSpotlightSearchCapabilitiesGateHolds(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	m.spotlight.openSpotlight()

	hasCapabilities := func() bool {
		for _, row := range m.spotlight.rows {
			if row.kind == rowEntry && row.entry != nil && row.entry.label == "Capabilities" {
				return true
			}
		}
		return false
	}

	m.spotlight.setQuery("capabilities")
	if hasCapabilities() {
		t.Errorf("without a project scope, Capabilities must not be a search candidate, rows = %v", rowLabels(m))
	}

	seedProject(t, m, "ATM", "Acme")
	m.projectScope = "ATM"
	m.spotlight.setQuery("capabilities")
	if !hasCapabilities() {
		t.Errorf("with a project scope, \"capabilities\" must match the Capabilities entry, rows = %v", rowLabels(m))
	}
}

// A filtered row activates exactly as a tree row does: Enter replays the
// entry's key and closes the launcher.
func TestSpotlightSearchEnterActivatesFilteredRow(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	before := m.themeName

	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("\\")})
	for _, r := range "cycle theme" {
		m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	if got := rowLabels(m); len(got) != 1 || got[0] != "Cycle theme" {
		t.Fatalf("setup: \"cycle theme\" must match exactly Cycle theme, rows = %v", got)
	}
	m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})

	if m.themeName == before {
		t.Errorf("activating a filtered row must replay its key, themeName still %q", m.themeName)
	}
	if m.spotlight.open {
		t.Error("activating a filtered entry must close the spotlight")
	}
}

// A search row displays "Group · Label", not the bare label, so a flattened
// list stays legible without its tree context.
func TestSearchLabelPrefixesTheGroup(t *testing.T) {
	e := menuEntries[0]
	for i := range menuEntries {
		if menuEntries[i].label == "Add project" {
			e = menuEntries[i]
		}
	}
	if got, want := searchLabel(e), "Project · Add project"; got != want {
		t.Errorf("searchLabel(Add project) = %q, want %q", got, want)
	}

	var view *menuEntry
	for i := range menuEntries {
		if menuEntries[i].label == "Cycle theme" {
			view = &menuEntries[i]
		}
	}
	if view == nil {
		t.Fatal("setup: no menu entry labelled \"Cycle theme\"")
	}
	if got, want := searchLabel(*view), "Cycle theme"; got != want {
		t.Errorf("searchLabel(root entry) = %q, want plain label %q", got, want)
	}
}

// The rendered list row itself must show the Group · Label form while
// filtering, not only searchLabel in isolation.
func TestSpotlightSearchRowsRenderGroupLabel(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	m.spotlight.openSpotlight()
	m.spotlight.setQuery("add project")

	view := stripANSI(m.spotlight.renderOverlay())
	mustContain(t, view, "Project · Add project")
}

// A query matching nothing collapses to a single "no matches" hint, which is
// not landable — Enter and the arrows must have nothing to do.
func TestSpotlightSearchZeroMatchesIsAHint(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	m.spotlight.openSpotlight()
	m.spotlight.setQuery("zzzznomatchzzzz")

	if got := rowLabels(m); len(got) != 1 || got[0] != "no matches" {
		t.Fatalf("zero matches must produce exactly one hint row, got %v", got)
	}
	if m.spotlight.rows[0].selectable() {
		t.Error("the \"no matches\" row must not be selectable")
	}
}

// Enter on the "no matches" hint is inert: selectedRow() is nil on a
// non-selectable row, so activate() has nothing to act on.
func TestSpotlightSearchEnterIsInertOnNoMatchesHint(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	m.spotlight.openSpotlight()
	m.spotlight.setQuery("zzzznomatchzzzz")
	if got := rowLabels(m); len(got) != 1 || got[0] != "no matches" {
		t.Fatalf("setup: expected exactly the no-matches hint, got %v", got)
	}

	if cmd := m.spotlight.handleKey(tea.KeyMsg{Type: tea.KeyEnter}); cmd != nil {
		t.Error("Enter on the no-matches hint must not return a command")
	}
	if !m.spotlight.open {
		t.Error("Enter on the no-matches hint must not close the spotlight")
	}
	if got := rowLabels(m); len(got) != 1 || got[0] != "no matches" {
		t.Errorf("Enter on the no-matches hint must not change the rows, got %v", got)
	}
}

// Hidden entries (keymap-reference-only rows) must never surface as a search
// result. "pane" is chosen because it matches the hidden "Projects pane" /
// "Tasks pane" rows by label — the sanity check below proves that, so the
// absence of a hit in the real results demonstrates the model's
// entryAvailable/hidden exclusion is doing the work, not that the query
// happened to be too narrow to find them.
func TestSpotlightSearchNeverMatchesHiddenEntries(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	m.spotlight.openSpotlight()

	hiddenHits := 0
	for _, e := range menuEntries {
		if e.hidden && matchRank(e, "pane") >= 0 {
			hiddenHits++
		}
	}
	if hiddenHits == 0 {
		t.Fatal("setup: query \"pane\" must matchRank at least one hidden entry, or this test proves nothing")
	}

	m.spotlight.setQuery("pane")
	if len(m.spotlight.rows) == 0 {
		t.Fatal("setup: query \"pane\" must also match at least one visible entry")
	}
	for _, row := range m.spotlight.rows {
		if row.kind == rowEntry && row.entry != nil && row.entry.hidden {
			t.Errorf("hidden entry %q must never appear in filtered results", row.entry.label)
		}
	}
}

// Routed from Task 6's review: firstSelectableRow used to fall back to row 0
// when nothing was selectable, which Task 8's zero-match hint makes reachable
// for real. Note that row 0 landing on the hint was already harmless by the
// time this test was written — renderListRow's `cursor && r.selectable()`
// guard (bd33346) already suppressed the glyph on a non-selectable row, and
// selectedRow() (also bd33346) already returned nil for it — so the glyph and
// selectedRow assertions below hold under the old `return 0` too. What they
// do NOT hold under is a lying cursor value: with `return 0`, sm.cursor is 0
// (a real row index) even though nothing is selected. The explicit sentinel
// check is what actually pins this fix; the rest of the test documents that
// the surrounding behavior stays correct.
func TestSpotlightNoMatchesDrawsNoCursorGlyph(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	m.spotlight.openSpotlight()
	m.spotlight.setQuery("zzzznomatchzzzz")

	if m.spotlight.cursor != -1 {
		t.Errorf("cursor must be the no-selection sentinel -1 on a zero-match hint, got %d", m.spotlight.cursor)
	}
	if m.spotlight.selectedRow() != nil {
		t.Errorf("selectedRow() must be nil on a zero-match hint, got %v", m.spotlight.selectedRow())
	}
	view := stripANSI(m.spotlight.renderOverlay())
	if strings.Contains(view, "▸") {
		t.Errorf("no row is selectable, so no cursor glyph should be drawn:\n%s", view)
	}

	// The arrows must not crash and must not manufacture a selection either.
	m.spotlight.handleKey(tea.KeyMsg{Type: tea.KeyDown})
	m.spotlight.handleKey(tea.KeyMsg{Type: tea.KeyUp})
	if m.spotlight.selectedRow() != nil {
		t.Error("arrow keys on an all-hint list must not manufacture a selection")
	}
}

// Clearing a query that had moved the cursor must land back on the tree's
// first selectable row, not wherever the filtered list's cursor happened to
// sit.
func TestSpotlightClearingQueryResetsCursorToFirstRow(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	m.spotlight.openSpotlight()

	m.spotlight.setQuery("e") // common enough to produce several rows
	m.spotlight.handleKey(tea.KeyMsg{Type: tea.KeyDown})
	m.spotlight.handleKey(tea.KeyMsg{Type: tea.KeyDown})
	if m.spotlight.cursor == 0 {
		t.Fatal("setup: the cursor must have moved off the first filtered row")
	}

	m.spotlight.setQuery("")
	if m.spotlight.cursor != 0 {
		t.Errorf("clearing the query must reset the cursor to row 0, got %d", m.spotlight.cursor)
	}
	if got := m.spotlight.selectedLabel(); got != "Project" {
		t.Errorf("cleared query must land on the tree's first selectable row, selected %q", got)
	}
}

// Routed from Task 7's review: the unfocused search line reserved three
// columns ("> " plus a caret it never draws) instead of two, truncating one
// column earlier than it needed to.
func TestSpotlightSearchLineUnfocusedOffByOneFixed(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	m.spotlight.openSpotlight()

	// the box's two borders, its padding column, and "> " — no caret, because
	// the unfocused branch draws none and must not reserve one
	w := m.spotlight.innerWidth()
	room := w - 5
	q := "HEAD" + strings.Repeat("y", room-8) + "TAIL"
	if lipgloss.Width(q) != room {
		t.Fatalf("setup: query is %d columns, want exactly room %d", lipgloss.Width(q), room)
	}
	// Set the query and focus directly rather than through setQuery/Tab: this
	// test is about searchLine's own truncation math, not about the query
	// matching anything, and a garbage query like this one is a "no matches"
	// hint with no preview lines, which Tab refuses to focus.
	m.spotlight.query = q
	m.spotlight.focus = focusPreview

	view := stripANSI(m.spotlight.renderOverlay())
	mustContain(t, view, "HEAD")
	mustContain(t, view, "TAIL")
}

// A query wider than the pane must scroll so its tail — what the user just
// typed — stays visible, exercising fitLineFrom's scroll-into-view path,
// which no earlier test typed a query long enough to reach.
func TestSpotlightSearchLineScrollsLongQueryIntoView(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	m.spotlight.openSpotlight()

	long := "HEADMARK" + strings.Repeat("y", 100) + "TAILMARK"
	m.spotlight.setQuery(long)

	view := stripANSI(m.spotlight.renderOverlay())
	mustContain(t, view, "TAILMARK")
	mustNotContain(t, view, "HEADMARK")
}

// --- the contextual Task group ---

// The Task group is the launcher's one contextual surface: its query searches
// the scoped project's live tasks instead of the registry. The result list is
// capped at 5 — it shares the pane with the static Add-task row, and a search
// that returns half a project is not a search.
func TestSpotlightTaskSearchTopFive(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	seedProject(t, m, "ATM", "Acme")
	selectProject(t, m, "ATM")
	for i := 1; i <= 7; i++ {
		seedTask(t, m, "ATM", fmt.Sprintf("alpha task %d", i))
	}
	seedTask(t, m, "ATM", "beta task")

	m.spotlight.openSpotlight()
	moveCursorToGroup(t, m, "Task")
	typeQuery(t, m, "alpha")

	var tasks int
	for _, r := range m.spotlight.rows {
		if r.kind == rowTask {
			tasks++
			if !strings.Contains(r.task.Title, "alpha") {
				t.Errorf("non-matching task row %q", r.task.Title)
			}
		}
	}
	if tasks != 5 {
		t.Errorf("task rows = %d, want the 5-match cap; rows = %v", tasks, rowLabels(m))
	}
	// The static Add-task row survives the search: it is how the user files
	// the task they just failed to find.
	if len(m.spotlight.rows) != 6 || m.spotlight.rows[0].kind != rowEntry || m.spotlight.rows[0].label() != "Add task" {
		t.Errorf("rows = %v, want Add task followed by the 5 matches", rowLabels(m))
	}
}

// The cap is applied AFTER the ranking, not before it: with more matches than
// the cap, the ID match must survive the cut even when the store hands it over
// last. Sibling to TestSpotlightTaskSearchTopFive (which pins the cap itself
// and the row shape) rather than a second phase inside it, because it needs a
// different query — every task there is a title match, so no ID can rank.
//
// Fixture built like TestSpotlightTaskSearchRanksIDOverTitle's: decoys first,
// retitled after the target exists, so creation order puts the ID match last.
// Capping before ranking would keep the first five decoys and drop the one
// task the user actually named.
func TestSpotlightTaskSearchRanksBeforeCapping(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	seedProject(t, m, "ATM", "Acme")
	selectProject(t, m, "ATM")
	var decoys []*store.Task
	for i := 1; i <= 7; i++ {
		decoys = append(decoys, seedTask(t, m, "ATM", fmt.Sprintf("placeholder %d", i)))
	}
	target := seedTask(t, m, "ATM", "no keyword here")
	for i, d := range decoys {
		retitle(t, m, d, fmt.Sprintf("decoy %d mentions %s", i+1, target.ID))
	}

	got := m.spotlight.taskMatches(target.ID)
	if len(got) != 5 {
		t.Fatalf("taskMatches over 8 matches returned %d, want the 5-cap", len(got))
	}
	if got[0].ID != target.ID {
		t.Fatalf("first match = %q (%q), want the ID match %q — the cap must be applied after the ranking",
			got[0].ID, got[0].Title, target.ID)
	}
	// And the same through the rows the user actually sees.
	m.spotlight.openSpotlight()
	moveCursorToGroup(t, m, "Task")
	typeQuery(t, m, target.ID)
	moveCursorToTask(t, m, target.ID) // Fatalf if the cut dropped it
}

// ID matches rank above title matches: a user who pastes an ID means that
// task, not every task whose title happens to contain the same run of
// characters.
//
// The fixture is built so the store order FIGHTS the assertion. ListTasks
// sorts by creation ordinal (internal/store/query.go), so seeding the ID match
// first would put it at got[0] whether or not taskMatches ranks anything —
// the assertion would hold with the rank sort deleted. The title decoys are
// therefore created first and only then rewritten to mention the target's ID
// (a title edit does not move a task's creation ordinal), which leaves the ID
// match LAST in store order: only the rank sort can bring it forward.
func TestSpotlightTaskSearchRanksIDOverTitle(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	seedProject(t, m, "ATM", "Acme")
	selectProject(t, m, "ATM")
	decoys := []*store.Task{
		seedTask(t, m, "ATM", "placeholder one"),
		seedTask(t, m, "ATM", "placeholder two"),
	}
	target := seedTask(t, m, "ATM", "no keyword here")
	retitle(t, m, decoys[0], "mentions "+target.ID+" in the title")
	retitle(t, m, decoys[1], "also mentions "+target.ID)

	// Setup check: the store really does hand taskMatches the ID match last.
	all := m.store.ListTasks(core.QueryFilters{Project: "ATM"})
	if all[len(all)-1].ID != target.ID {
		t.Fatalf("setup: want the ID match last in store order, got %q last", all[len(all)-1].ID)
	}

	got := m.spotlight.taskMatches(target.ID)
	if len(got) != 3 {
		t.Fatalf("taskMatches(%q) = %d tasks, want 3", target.ID, len(got))
	}
	if got[0].ID != target.ID {
		t.Errorf("first match = %q (%q), want the ID match %q", got[0].ID, got[0].Title, target.ID)
	}
}

// retitle rewrites a seeded task's title without disturbing its creation
// ordinal — the store's list order is by creation, so this is how a fixture
// puts a task's *content* out of step with its position.
func retitle(t *testing.T, m *Model, tk *store.Task, title string) {
	t.Helper()
	if err := m.store.SetTitle(tk.ID, title, testActor); err != nil {
		t.Fatalf("SetTitle %s: %v", tk.ID, err)
	}
	m.refreshAll()
}

// The search is case-insensitive and matches a substring anywhere in the
// title — not a prefix.
func TestSpotlightTaskSearchIsCaseInsensitiveSubstring(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	seedProject(t, m, "ATM", "Acme")
	selectProject(t, m, "ATM")
	seedTask(t, m, "ATM", "Wire The Indexer")

	if got := m.spotlight.taskMatches("the index"); len(got) != 1 {
		t.Errorf("taskMatches(\"the index\") = %d tasks, want 1", len(got))
	}
	if got := m.spotlight.taskMatches("nothing here"); len(got) != 0 {
		t.Errorf("taskMatches on a non-match = %d tasks, want 0", len(got))
	}
}

// Without a project scope the Task group has nothing to search: the hint says
// so and the query is inert — typing must not manufacture rows out of another
// project's tasks.
func TestSpotlightTaskQueryIsInertWithoutAScope(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	seedProject(t, m, "ATM", "Acme")
	seedTask(t, m, "ATM", "alpha task")
	m.projectScope = ""

	m.spotlight.openSpotlight()
	moveCursorToGroup(t, m, "Task")
	typeQuery(t, m, "alpha")

	if got := rowLabels(m); !equalStrings(got, []string{"Add task", "select a project first"}) {
		t.Errorf("unscoped Task group after typing = %v, want the inert two rows", got)
	}
	if got := m.spotlight.taskMatches("alpha"); len(got) != 0 {
		t.Errorf("taskMatches without a scope = %d tasks, want none", len(got))
	}
}

// A query that matches no task says so rather than leaving the group looking
// like it lost its rows; the hint is not landable, so the cursor stays on the
// Add-task row.
func TestSpotlightTaskSearchNoMatchesIsAHint(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	seedProject(t, m, "ATM", "Acme")
	selectProject(t, m, "ATM")
	seedTask(t, m, "ATM", "alpha task")

	m.spotlight.openSpotlight()
	moveCursorToGroup(t, m, "Task")
	typeQuery(t, m, "zzzz")

	if got := rowLabels(m); !equalStrings(got, []string{"Add task", "no tasks match"}) {
		t.Errorf("rows for a query matching nothing = %v", got)
	}
	if got := m.spotlight.selectedLabel(); got != "Add task" {
		t.Errorf("selected = %q, want the Add task row", got)
	}
}

// A query of nothing but whitespace is an empty query, in the row builder as
// well as in the matcher. Space is a real keystroke inside the launcher
// (printableRune maps tea.KeySpace to ' '), so the two disagreeing meant one
// space turned the invitation to type into "no tasks match".
func TestSpotlightTaskWhitespaceQueryIsEmpty(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	seedProject(t, m, "ATM", "Acme")
	selectProject(t, m, "ATM")
	seedTask(t, m, "ATM", "alpha task")

	m.spotlight.openSpotlight()
	moveCursorToGroup(t, m, "Task")
	m.spotlight.handleKey(tea.KeyMsg{Type: tea.KeySpace})

	if m.spotlight.query != " " {
		t.Fatalf("setup: space must type into the query, query=%q", m.spotlight.query)
	}
	if got := rowLabels(m); !equalStrings(got, []string{"Add task", "type to find a task…"}) {
		t.Errorf("rows for a whitespace query = %v, want the empty-query hint", got)
	}
}

// Regression pin for setQuery's cursor reset. The Task group's row 0 is the
// static Add-task entry, which no query ever matches: homing the cursor there
// on every keystroke meant the selection (and the preview beside it) sat on
// something unrelated to what the user was typing, and every character undid
// the arrow keys. A typed query homes onto the top *result* instead.
func TestSpotlightTaskSearchSelectsTheTopResultWhileTyping(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	seedProject(t, m, "ATM", "Acme")
	selectProject(t, m, "ATM")
	seedTask(t, m, "ATM", "alpha one")
	seedTask(t, m, "ATM", "alpha two")
	seedTask(t, m, "ATM", "alpha three")

	m.spotlight.openSpotlight()
	moveCursorToGroup(t, m, "Task")

	// Every keystroke of the query, not just the last: the reset ran per
	// rebuild, so a bug here is a per-keystroke bug.
	for _, r := range "alpha" {
		typeQuery(t, m, string(r))
		sel := m.spotlight.selectedRow()
		if sel == nil || sel.kind != rowTask {
			t.Fatalf("after typing %q the selection is %q, want a task row; rows = %v",
				m.spotlight.query, m.spotlight.selectedLabel(), rowLabels(m))
		}
		if want := m.spotlight.rows[1]; sel.task != want.task {
			t.Errorf("after typing %q the selection is %q, want the top result %q",
				m.spotlight.query, sel.label(), want.label())
		}
	}

	// The arrows still move within the results, and backspacing back to an
	// empty query returns to the tree's first selectable row.
	m.spotlight.handleKey(tea.KeyMsg{Type: tea.KeyDown})
	if got := m.spotlight.cursor; got != 2 {
		t.Errorf("down from the top result left the cursor at %d, want 2", got)
	}
	for range "alpha" {
		m.spotlight.handleKey(tea.KeyMsg{Type: tea.KeyBackspace})
	}
	if got := m.spotlight.selectedLabel(); got != "Add task" {
		t.Errorf("clearing the query selected %q, want Add task", got)
	}
}

// The whole drill-in: search a task, hover it (its preview is the task, not a
// registry summary), Enter into the six per-task actions, and run one — which
// must act on the task that was chosen, not on whatever the Tasks pane cursor
// happened to be on.
func TestSpotlightTaskDrillAndTargetedAction(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	seedProject(t, m, "ATM", "Acme")
	selectProject(t, m, "ATM")
	target := seedTask(t, m, "ATM", "wire the indexer")
	seedTask(t, m, "ATM", "decoy task")

	m.spotlight.openSpotlight()
	moveCursorToGroup(t, m, "Task")
	typeQuery(t, m, "indexer")
	moveCursorToTask(t, m, target.ID)

	preview := stripANSI(strings.Join(m.spotlight.lines, "\n"))
	if !strings.Contains(preview, target.ID) || !strings.Contains(preview, "History") {
		t.Errorf("hovering a task must preview that task with its history, got:\n%s", preview)
	}

	m.spotlight.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if m.spotlight.level != levelTaskActions || m.spotlight.taskID != target.ID {
		t.Fatalf("Enter on a task row must drill into its actions: level=%v taskID=%q",
			m.spotlight.level, m.spotlight.taskID)
	}
	if m.spotlight.taskTitle != target.Title {
		t.Errorf("taskTitle = %q, want %q", m.spotlight.taskTitle, target.Title)
	}
	if m.spotlight.query != "" {
		t.Errorf("drilling into a task must clear the query, query=%q", m.spotlight.query)
	}
	want := []string{"Edit title", "Edit description", "Add label", "Remove label", "Add comment", "Remove task"}
	if got := rowLabels(m); !equalStrings(got, want) {
		t.Errorf("task action rows =\n%v\nwant\n%v", got, want)
	}

	moveCursorToLabel(t, m, "Edit title")
	m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})

	if m.spotlight.open {
		t.Error("activating a task action must close the launcher")
	}
	if m.tasks.detail.id != target.ID {
		t.Errorf("open task detail = %q, want the chosen task %q", m.tasks.detail.id, target.ID)
	}
	if m.form == nil || m.formKind != formTaskSetTitle {
		t.Errorf("activation must open the title form, formKind=%v", m.formKind)
	}
	if m.focused != paneTasks {
		t.Errorf("activation must focus the Tasks pane, focused=%v", m.focused)
	}
}

// Esc peels the task-action level back to the Task group, exactly as it peels
// a group back to the root — and drops the task it was targeting.
func TestSpotlightEscLeavesTaskActions(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	seedProject(t, m, "ATM", "Acme")
	selectProject(t, m, "ATM")
	target := seedTask(t, m, "ATM", "wire the indexer")

	m.spotlight.openSpotlight()
	moveCursorToGroup(t, m, "Task")
	typeQuery(t, m, "indexer")
	moveCursorToTask(t, m, target.ID)
	m.spotlight.handleKey(tea.KeyMsg{Type: tea.KeyEnter})

	m.spotlight.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	if m.spotlight.level != levelGroup || m.spotlight.group != groupTask {
		t.Fatalf("Esc from the task actions must return to the Task group: level=%v group=%v",
			m.spotlight.level, m.spotlight.group)
	}
	if m.spotlight.taskID != "" || m.spotlight.taskTitle != "" {
		t.Errorf("leaving the task-action level must drop its target: id=%q title=%q",
			m.spotlight.taskID, m.spotlight.taskTitle)
	}
	// A second Esc — the query is back on screen, so it is the layer that
	// peels next — leaves the group's empty-query tree.
	m.spotlight.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	if got := rowLabels(m); !equalStrings(got, []string{"Add task", "type to find a task…"}) {
		t.Errorf("rows after clearing the restored query = %v, want the Task group's empty-query tree", got)
	}
}

// Esc out of a task's actions returns to the RESULTS, query intact — not to
// an empty Task group the user has to retype their search into. Both halves
// are asserted: restoring the query string without rebuilding the rows it
// selected would read as working while showing nothing.
func TestSpotlightEscRestoresTheTaskQuery(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	seedProject(t, m, "ATM", "Acme")
	selectProject(t, m, "ATM")
	target := seedTask(t, m, "ATM", "alpha one")
	seedTask(t, m, "ATM", "alpha two")
	seedTask(t, m, "ATM", "beta three")

	m.spotlight.openSpotlight()
	moveCursorToGroup(t, m, "Task")
	typeQuery(t, m, "alpha")
	before := rowLabels(m)
	moveCursorToTask(t, m, target.ID)
	m.spotlight.handleKey(tea.KeyMsg{Type: tea.KeyEnter})

	m.spotlight.handleKey(tea.KeyMsg{Type: tea.KeyEsc})

	if m.spotlight.query != "alpha" {
		t.Errorf("query after Esc = %q, want the search intact", m.spotlight.query)
	}
	if got := rowLabels(m); !equalStrings(got, before) {
		t.Errorf("rows after Esc =\n%v\nwant the results the user drilled from\n%v", got, before)
	}
	// The restored list is live, not a redrawn string: the cursor is on a
	// result and its preview is that task.
	sel := m.spotlight.selectedRow()
	if sel == nil || sel.kind != rowTask {
		t.Fatalf("selection after Esc = %q, want a task row", m.spotlight.selectedLabel())
	}
	if !strings.Contains(stripANSI(strings.Join(m.spotlight.lines, "\n")), sel.task.ID) {
		t.Errorf("the restored selection must preview its task, lines:\n%s", strings.Join(m.spotlight.lines, "\n"))
	}
	// Drilling in again still works off the restored list.
	moveCursorToTask(t, m, target.ID)
	m.spotlight.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if m.spotlight.level != levelTaskActions || m.spotlight.taskID != target.ID {
		t.Errorf("re-drilling from the restored results: level=%v taskID=%q", m.spotlight.level, m.spotlight.taskID)
	}
}

// The targeting requirement, stated as the states it must survive: a per-task
// action selects its target by ID rather than replaying the scopeTasksDetail
// prelude ({"2", "enter"}), which opens whatever the Tasks list cursor is on.
// Each state below is one the prelude would get wrong.
func TestSpotlightTaskActionTargetsFromAnyState(t *testing.T) {
	states := []struct {
		name  string
		setup func(t *testing.T, m *Model, target, decoy *store.Task)
	}{
		{"projects pane focused", func(t *testing.T, m *Model, target, decoy *store.Task) {
			m.focused = paneProjects
		}},
		{"projects detail open", func(t *testing.T, m *Model, target, decoy *store.Task) {
			m.focused = paneProjects
			m.projects.openDetail("ATM")
		}},
		{"tasks list, cursor on the decoy", func(t *testing.T, m *Model, target, decoy *store.Task) {
			m.focused = paneTasks
			for i, r := range m.tasks.rows {
				if r.id == decoy.ID {
					m.tasks.cursor = i
				}
			}
		}},
		{"another task's detail already open", func(t *testing.T, m *Model, target, decoy *store.Task) {
			m.focused = paneTasks
			m.tasks.openDetail(decoy.ID)
		}},
		{"a board filter that hides the target", func(t *testing.T, m *Model, target, decoy *store.Task) {
			m.focused = paneTasks
			m.tasks.setFocus(taskFocus{mode: focusUnlabeled}, "")
			for _, r := range m.tasks.rows {
				if r.id == target.ID {
					t.Fatalf("setup: the filter must hide the target, rows = %v", m.tasks.rows)
				}
			}
		}},
	}

	for _, st := range states {
		t.Run(st.name, func(t *testing.T) {
			m := newTestModel(t)
			m.SetSize(120, 40)
			seedProject(t, m, "ATM", "Acme")
			selectProject(t, m, "ATM")
			target := seedTask(t, m, "ATM", "wire the indexer", "ATM:status:open")
			decoy := seedTask(t, m, "ATM", "decoy task")
			st.setup(t, m, target, decoy)

			m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("\\")})
			moveCursorToGroup(t, m, "Task")
			typeQuery(t, m, "indexer")
			moveCursorToTask(t, m, target.ID)
			m.spotlight.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
			moveCursorToLabel(t, m, "Add comment")
			m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})

			if m.tasks.detail.id != target.ID {
				t.Fatalf("open task detail = %q, want the chosen task %q", m.tasks.detail.id, target.ID)
			}
			if m.form == nil || m.formKind != formCommentAdd {
				t.Fatalf("activation must open the comment form, formKind=%v form=%v", m.formKind, m.form)
			}
			if m.focused != paneTasks {
				t.Errorf("activation must focus the Tasks pane, focused=%v", m.focused)
			}
		})
	}
}

// Every one of the six actions targets the chosen task, not just the one the
// drill-in test happens to run. Remove task is the one that never opens a
// form, so it is checked through the confirm it raises.
func TestSpotlightEveryTaskActionTargetsTheChosenTask(t *testing.T) {
	cases := []struct {
		label string
		check func(t *testing.T, m *Model, id string)
	}{
		{"Edit title", func(t *testing.T, m *Model, id string) { wantFormKind(t, m, formTaskSetTitle) }},
		{"Edit description", func(t *testing.T, m *Model, id string) { wantFormKind(t, m, formTaskSetDescription) }},
		{"Add label", func(t *testing.T, m *Model, id string) { wantFormKind(t, m, formTaskLabelAdd) }},
		{"Remove label", func(t *testing.T, m *Model, id string) { wantFormKind(t, m, formTaskLabelRemove) }},
		{"Add comment", func(t *testing.T, m *Model, id string) { wantFormKind(t, m, formCommentAdd) }},
		{"Remove task", func(t *testing.T, m *Model, id string) {
			if m.confirm != confirmRemoveTask {
				t.Fatalf("confirm = %v, want confirmRemoveTask", m.confirm)
			}
			if !strings.Contains(m.confirmMsg, id) {
				t.Errorf("confirm message %q must name the chosen task %s", m.confirmMsg, id)
			}
		}},
	}

	for _, tc := range cases {
		t.Run(tc.label, func(t *testing.T) {
			m := newTestModel(t)
			m.SetSize(120, 40)
			seedProject(t, m, "ATM", "Acme")
			selectProject(t, m, "ATM")
			target := seedTask(t, m, "ATM", "wire the indexer", "ATM:status:open")
			seedTask(t, m, "ATM", "decoy task")
			m.focused = paneProjects

			m.spotlight.openSpotlight()
			moveCursorToGroup(t, m, "Task")
			typeQuery(t, m, "indexer")
			moveCursorToTask(t, m, target.ID)
			m.spotlight.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
			moveCursorToLabel(t, m, tc.label)
			m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})

			if m.tasks.detail.id != target.ID {
				t.Fatalf("open task detail = %q, want the chosen task %q", m.tasks.detail.id, target.ID)
			}
			tc.check(t, m, target.ID)
		})
	}
}

// Type-to-filter is live at the task-action level too (searchCandidates keeps
// it to the six actions there), and a row reached that way must still target
// the chosen task: the filtered list is rowEntry rows like any other, so the
// targeting has to hang off the level, not off how the row was reached.
func TestSpotlightFilteredTaskActionStaysTargeted(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	seedProject(t, m, "ATM", "Acme")
	selectProject(t, m, "ATM")
	target := seedTask(t, m, "ATM", "wire the indexer")
	seedTask(t, m, "ATM", "decoy task")

	m.spotlight.openSpotlight()
	moveCursorToGroup(t, m, "Task")
	typeQuery(t, m, "indexer")
	moveCursorToTask(t, m, target.ID)
	m.spotlight.handleKey(tea.KeyMsg{Type: tea.KeyEnter})

	typeQuery(t, m, "comment")
	if got := rowLabels(m); !equalStrings(got, []string{"Add comment"}) {
		t.Fatalf("filtered task actions = %v, want just Add comment", got)
	}
	m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})

	if m.tasks.detail.id != target.ID {
		t.Errorf("open task detail = %q, want the chosen task %q", m.tasks.detail.id, target.ID)
	}
	wantFormKind(t, m, formCommentAdd)
}

func wantFormKind(t *testing.T, m *Model, want formAction) {
	t.Helper()
	if m.form == nil || m.formKind != want {
		t.Fatalf("formKind = %v (form=%v), want %v", m.formKind, m.form, want)
	}
}

// The task rows are a snapshot of a store another process can change: a task
// removed between the search and the Enter must never be replayed against.
// The launcher says so and stays open on the action list.
func TestSpotlightTaskActionOnAGoneTask(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	seedProject(t, m, "ATM", "Acme")
	selectProject(t, m, "ATM")
	target := seedTask(t, m, "ATM", "wire the indexer")

	m.spotlight.openSpotlight()
	moveCursorToGroup(t, m, "Task")
	typeQuery(t, m, "indexer")
	moveCursorToTask(t, m, target.ID)
	m.spotlight.handleKey(tea.KeyMsg{Type: tea.KeyEnter})

	if err := m.store.RemoveTask(target.ID, testActor); err != nil {
		t.Fatalf("RemoveTask: %v", err)
	}
	moveCursorToLabel(t, m, "Edit title")
	m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})

	if want := "task " + target.ID + " is gone"; m.toastMsg != want {
		t.Errorf("toast = %q, want %q", m.toastMsg, want)
	}
	if !m.spotlight.open || m.spotlight.level != levelTaskActions {
		t.Errorf("a gone task must leave the launcher open on the action list: open=%v level=%v",
			m.spotlight.open, m.spotlight.level)
	}
	if m.form != nil || m.tasks.detail.id != "" {
		t.Errorf("nothing may be replayed against a gone task: form=%v detail=%q", m.form, m.tasks.detail.id)
	}
}

// The task preview is what replaced the deleted task-detail history overlay:
// identity, labels, the description's first lines, then the task's audit
// history rendered by taskHistoryLines itself.
func TestSpotlightTaskPreviewShowsHistory(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	seedProject(t, m, "ATM", "Acme")
	selectProject(t, m, "ATM")
	tk, err := m.store.CreateTask("ATM", "wire the indexer", "first line of the description\nsecond line", []string{"ATM:status:open"}, testActor)
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	m.refreshAll()

	joined := stripANSI(strings.Join(taskPreviewLines(m, tk, 60), "\n"))
	for _, want := range []string{tk.ID, "wire the indexer", "ATM:status:open", "first line of the description", "History", "task.created"} {
		if !strings.Contains(joined, want) {
			t.Errorf("task preview missing %q:\n%s", want, joined)
		}
	}
	if strings.Contains(joined, "(no history)") {
		t.Errorf("a task with real history must not render the empty fallback:\n%s", joined)
	}
}

// A task with neither description nor labels still previews: the header and
// the history are unconditional, so the pane never comes out blank (which the
// renderer would show as "(no preview)").
func TestSpotlightTaskPreviewWithoutDescriptionOrLabels(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	seedProject(t, m, "ATM", "Acme")
	selectProject(t, m, "ATM")
	tk := seedTask(t, m, "ATM", "bare task")

	lines := taskPreviewLines(m, tk, 60)
	if len(lines) == 0 {
		t.Fatal("taskPreviewLines returned nothing for a bare task")
	}
	joined := stripANSI(strings.Join(lines, "\n"))
	if !strings.Contains(joined, tk.ID) || !strings.Contains(joined, "bare task") {
		t.Errorf("preview header missing the id or title:\n%s", joined)
	}
	if !strings.Contains(joined, "History") {
		t.Errorf("preview missing the history section:\n%s", joined)
	}
}

// A task row renders its ID, not a bare title: the ID is what the search
// matches on and what the actions target, and two tasks may share a title. It
// also carries the same › drill marker a group row does, because Enter on it
// drills exactly the same way.
func TestSpotlightTaskRowRendersIDAndDrillMarker(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(160, 40)
	seedProject(t, m, "ATM", "Acme")
	selectProject(t, m, "ATM")
	target := seedTask(t, m, "ATM", "wire the indexer")

	m.spotlight.openSpotlight()
	moveCursorToGroup(t, m, "Task")
	typeQuery(t, m, "indexer")

	view := stripANSI(m.spotlight.renderOverlay())
	for _, want := range []string{target.ID, "wire the indexer ›"} {
		if !strings.Contains(view, want) {
			t.Errorf("task row missing %q in:\n%s", want, view)
		}
	}
}
