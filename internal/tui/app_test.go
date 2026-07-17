package tui

import (
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	"atm/internal/store"
	"atm/internal/workflow"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// --- test helpers ---

// testActor is the conforming actor (persona@agent:model with a registered
// persona) used by TUI tests when stamping mutations. "developer" is a
// built-in persona, lazily seeded by the store's validateActor.
const testActor = "developer@claude:test"

// newTestModel builds a Model against a fresh temp-dir store. The store is
// opened and auto-initialized; the model's actor is set to testActor so
// mutating keys are active in the tests.
func newTestModel(t *testing.T) *Model {
	t.Helper()
	return newTestModelWithActor(t, testActor)
}

// newTestModelWithActor builds a Model with the given actor (empty string
// simulates a first-run launch with no --actor / ATM_ACTOR).
func newTestModelWithActor(t *testing.T, actor string) *Model {
	t.Helper()
	s, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	if err := s.Init(""); err != nil {
		t.Fatalf("Init: %v", err)
	}
	m, err := NewModel(NewModelOpts{Service: s, Actor: actor})
	if err != nil {
		t.Fatalf("NewModel: %v", err)
	}
	return m
}

// keyMsg constructs a tea.KeyMsg from a key string. Rune keys use KeyRunes;
// the special-key strings bubbletea expects (enter, esc, backspace, space,
// down, up, shift+up, shift+down, shift+left, shift+right, pgdown, pgup, tab)
// map to their KeyType. The returned KeyMsg's String() matches what the TUI
// handlers switch on.
func keyMsg(s string) tea.KeyMsg {
	switch s {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "backspace":
		return tea.KeyMsg{Type: tea.KeyBackspace}
	case " ":
		return tea.KeyMsg{Type: tea.KeySpace}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "shift+up":
		return tea.KeyMsg{Type: tea.KeyShiftUp}
	case "shift+down":
		return tea.KeyMsg{Type: tea.KeyShiftDown}
	case "shift+left":
		return tea.KeyMsg{Type: tea.KeyShiftLeft}
	case "shift+right":
		return tea.KeyMsg{Type: tea.KeyShiftRight}
	case "pgdown":
		return tea.KeyMsg{Type: tea.KeyPgDown}
	case "pgup":
		return tea.KeyMsg{Type: tea.KeyPgUp}
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

// seedProject creates a project and returns its code. Uses the store API
// directly (no actor gating — tests bypass canMutate by seeding via store).
func seedProject(t *testing.T, m *Model, code, name string) {
	t.Helper()
	if _, err := m.store.CreateProject(code, name, testActor); err != nil {
		t.Fatalf("CreateProject %s: %v", code, err)
	}
	m.refreshAll()
}

// seedTask creates a task under the given project with the given labels.
func seedTask(t *testing.T, m *Model, projectCode, title string, labels ...string) *store.Task {
	t.Helper()
	tk, err := m.store.CreateTask(projectCode, title, "", labels, testActor)
	if err != nil {
		t.Fatalf("CreateTask %s: %v", title, err)
	}
	m.refreshAll()
	return tk
}

// seedLabel adds a label to the registry (with optional description).
func seedLabel(t *testing.T, m *Model, name, desc string) {
	t.Helper()
	if err := m.store.LabelAdd(name, desc, "", testActor); err != nil {
		t.Fatalf("LabelAdd %s: %v", name, err)
	}
	m.refreshAll()
}

// update sends a key into the model (the Update method returns a model; cast
// back to *Model). All TUI tests drive the model this way.
func update(t *testing.T, m *Model, key string) *Model {
	t.Helper()
	mm, _ := m.Update(keyMsg(key))
	out, ok := mm.(*Model)
	if !ok {
		t.Fatalf("Update did not return *Model")
	}
	return out
}

// mustContain fails the test if sub is not in view.
func mustContain(t *testing.T, view, sub string) {
	t.Helper()
	if !strings.Contains(view, sub) {
		t.Errorf("view missing %q\n--- view ---\n%s", sub, view)
	}
}

// mustNotContain fails the test if sub is in view.
func mustNotContain(t *testing.T, view, sub string) {
	t.Helper()
	if strings.Contains(view, sub) {
		t.Errorf("view unexpectedly contains %q\n--- view ---\n%s", sub, view)
	}
}

func leadingSpaces(s string) int {
	return len(s) - len(strings.TrimLeft(s, " "))
}

func TestDashboardContentUsesPaneWidthWithoutCentering(t *testing.T) {
	m := newTestModel(t)
	width := 38
	if got := dashboardContentWidth(width); got != width {
		t.Fatalf("dashboardContentWidth(%d) = %d want %d", width, got, width)
	}
	line := sectionDivider(m.styles, width, "Overview")
	if got := leadingSpaces(line); got != 0 {
		t.Fatalf("divider left padding = %d want 0\nline: %q", got, line)
	}
	if got := lipgloss.Width(line); got != width {
		t.Fatalf("divider width = %d want pane width %d\nline: %q", got, width, line)
	}
	text := dashboardLine(width, "PROJECT: ATM")
	if got := leadingSpaces(text); got != 0 {
		t.Fatalf("dashboard line left padding = %d want 0\nline: %q", got, text)
	}
	if got := lipgloss.Width(text); got != len("PROJECT: ATM") {
		t.Fatalf("dashboard line should not pad text, width = %d", got)
	}
	block := centerLinesBoth([]string{"no projects"}, width, 1)
	if got := leadingSpaces(block); got != 0 {
		t.Fatalf("centerLinesBoth left padding = %d want 0\nblock: %q", got, block)
	}
}

func TestSectionCaptionRuleScopedToTitleWidth(t *testing.T) {
	m := newTestModel(t)
	out := sectionCaption(m.styles, 40, "FACTS")
	lines := strings.Split(out, "\n")
	if len(lines) != 2 {
		t.Fatalf("sectionCaption produced %d lines, want 2\n%q", len(lines), out)
	}
	if !strings.Contains(lines[0], "FACTS") {
		t.Fatalf("line 0 = %q, want it to contain FACTS", lines[0])
	}
	// The rule must be exactly as wide as the title ("FACTS" = 5 dashes), not
	// the full 40-column pane width.
	dashCount := strings.Count(lines[1], "─")
	if dashCount != len("FACTS") {
		t.Fatalf("rule has %d dashes, want %d\n%q", dashCount, len("FACTS"), lines[1])
	}
	if lipgloss.Width(lines[1]) >= 40 {
		t.Fatalf("rule line width = %d, want it short (title-scoped), not pane-width\n%q", lipgloss.Width(lines[1]), lines[1])
	}
}

func TestPaneModelsRenderWithinAssignedPaneWidth(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 36)
	seedProject(t, m, "ATM", "Acme Task Manager")
	update(t, m, "s")

	leftW, rightW := splitWorkspaceWidths(m.width)
	assertLinesWithinWidth := func(name, body string, maxW int) {
		for i, line := range strings.Split(body, "\n") {
			if got := lipgloss.Width(line); got > maxW {
				t.Fatalf("%s line %d width = %d want <= pane inner width %d\nline: %q", name, i, got, maxW, line)
			}
		}
	}
	assertLinesWithinWidth("projects", m.projects.View(), innerPaneWidth(leftW))
	assertLinesWithinWidth("tasks", m.tasks.View(), innerPaneWidth(rightW))
	wantPageSize := innerPaneHeight(m.contentHeight) - stripHeight - 6
	if wantPageSize < 1 {
		wantPageSize = 1
	}
	if got, want := m.tasks.pageSize, wantPageSize; got != want {
		t.Fatalf("tasks pageSize = %d want %d", got, want)
	}
}

// --- Step 1: pane focus ---

func TestPaneFocusKeys(t *testing.T) {
	m := newTestModel(t)
	if m.focused != paneProjects {
		t.Fatalf("default focus = %v want paneProjects", m.focused)
	}
	update(t, m, "2")
	if m.focused != paneTasks {
		t.Fatalf("after 2: focus = %v want paneTasks", m.focused)
	}
	update(t, m, "1")
	if m.focused != paneProjects {
		t.Fatalf("after 1: focus = %v want paneProjects", m.focused)
	}
}

func TestWorkspaceRendersTwoPanesNotThree(t *testing.T) {
	m := newTestModel(t)
	v := m.View()
	mustContain(t, v, "[1] Projects")
	mustContain(t, v, "[2] Tasks")
	mustNotContain(t, v, "[3] Boards")
}

func TestKey3IsNoOp(t *testing.T) {
	m := newTestModel(t)
	m.focused = paneProjects
	m.handleKey(keyMsg("3"))
	if m.focused != paneProjects {
		t.Errorf("focused = %v, want paneProjects (3 must not switch panes)", m.focused)
	}
}

func TestHelpOverlayOpensClosesAndScrolls(t *testing.T) {
	m := newTestModel(t)
	update(t, m, "2")
	update(t, m, "?")
	if m.helpOverlay != helpKeys {
		t.Fatalf("help overlay should be open (keys), got %v", m.helpOverlay)
	}
	if m.focused != paneTasks {
		t.Fatalf("opening help changed focus = %v want paneTasks", m.focused)
	}
	v := m.View()
	mustContain(t, v, "Help - Keys")
	mustContain(t, v, "CLI / TUI Parity")
	// The keys overlay holds parity + keymap; scroll to reveal keymap.
	for i := 0; i < 30; i++ {
		update(t, m, "j")
	}
	mustContain(t, m.View(), "Global Keymap")
	// Scrolling is clamped at the bottom; verify j still advances when not
	// at the limit by resetting to top and stepping once.
	update(t, m, "g")
	before := m.help.offset
	update(t, m, "j")
	if m.help.offset <= before {
		t.Fatalf("help j did not scroll: before=%d after=%d", before, m.help.offset)
	}
	update(t, m, "esc")
	if m.helpOverlay != helpNone {
		t.Fatalf("esc should close help overlay, got %v", m.helpOverlay)
	}
}

func TestHelpOverlayReadOnly(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme Task Manager")
	update(t, m, "?")
	for _, k := range []string{"a", "x", "L", "l", "N", "H", "s", "S", "d"} {
		update(t, m, k)
	}
	if m.form != nil {
		t.Errorf("mutating key opened a form from help overlay")
	}
	if m.confirm != confirmNone {
		t.Errorf("mutating key opened a confirm from help overlay")
	}
	if m.toastMsg != "" {
		t.Errorf("mutating key produced toast %q from help overlay", m.toastMsg)
	}
	ps := m.store.ListProjects()
	if len(ps) != 1 || ps[0].Code != "ATM" {
		t.Errorf("store changed from help overlay: projects = %+v", ps)
	}
}

func TestWorkspaceRendersAllPaneTitlesAtOnce(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 36)
	v := m.View()
	mustContain(t, v, "[1] Projects")
	mustContain(t, v, "[2] Tasks")
	mustNotContain(t, v, "1  Projects")
	mustNotContain(t, v, "4  Help")
}

func TestPaneFocusKeepsAllPanesVisible(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 36)
	for _, key := range []string{"1", "2"} {
		update(t, m, key)
		v := m.View()
		mustContain(t, v, "[1] Projects")
		mustContain(t, v, "[2] Tasks")
	}
}

func TestFocusedPaneStyleChanges(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 36)
	update(t, m, "1")
	projectsFocused := m.View()
	update(t, m, "2")
	tasksFocused := m.View()
	if projectsFocused == tasksFocused {
		t.Fatalf("focus change should change rendered pane styling")
	}
	if m.styles.PaneActive.GetForeground() == m.styles.PaneInactive.GetForeground() {
		t.Fatalf("active and inactive pane foregrounds should differ")
	}
}

func TestStatusLineHintsFollowFocusedPane(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme Task Manager")
	update(t, m, "s")
	update(t, m, "1")
	projects := m.renderStatusLine()
	update(t, m, "2")
	tasks := m.renderStatusLine()
	if projects == tasks {
		t.Fatalf("status hints should differ by focused pane:\nprojects=%q\ntasks=%q", projects, tasks)
	}
	mustContain(t, tasks, "[s]ort")
}

func TestDefaultTheme(t *testing.T) {
	m := newTestModel(t)
	if m.themeName != themeGraphite {
		t.Fatalf("themeName = %q want %q", m.themeName, themeGraphite)
	}
	if string(m.themeName) != "graphite" {
		t.Fatalf("themeName string = %q want graphite", m.themeName)
	}
}

func TestNextThemeNameWraps(t *testing.T) {
	order := []ThemeName{themeGraphite, themeLight, themeMono, themeGraphite}
	for i := 0; i < len(order)-1; i++ {
		if got := nextThemeName(order[i]); got != order[i+1] {
			t.Fatalf("nextThemeName(%q) = %q want %q", order[i], got, order[i+1])
		}
	}
	if got := nextThemeName(ThemeName("unknown")); got != themeGraphite {
		t.Fatalf("nextThemeName(unknown) = %q want %q", got, themeGraphite)
	}
}

func TestThemeCycleKeyUpdatesThemeAndStatus(t *testing.T) {
	m := newTestModel(t)
	mustContain(t, m.renderStatusLine(), "theme: graphite")
	update(t, m, "T")
	if m.themeName != themeLight {
		t.Fatalf("after T: themeName = %q want %q", m.themeName, themeLight)
	}
	mustContain(t, m.renderStatusLine(), "theme: light")
}

func TestThemeCyclePreservesNavigationState(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme Task Manager")
	seedTask(t, m, "ATM", "open task", "ATM:status:open")
	update(t, m, "s")
	update(t, m, "2")
	m.tasks.setFocus(taskFocus{mode: focusOff}, "ATM:status:open")
	update(t, m, "enter")
	if m.tasks.view != tViewDetail {
		t.Fatalf("setup: expected task detail")
	}
	update(t, m, "T")
	if m.focused != paneTasks {
		t.Errorf("focused = %v want paneTasks", m.focused)
	}
	if m.projectScope != "ATM" {
		t.Errorf("projectScope = %q want ATM", m.projectScope)
	}
	if m.tasks.filter != "ATM:status:open" {
		t.Errorf("filter = %q want ATM:status:open", m.tasks.filter)
	}
	if m.tasks.view != tViewDetail {
		t.Errorf("tasks.view = %v want tViewDetail", m.tasks.view)
	}
}

func TestThemeKeyDoesNotHijackTextInput(t *testing.T) {
	m := newTestModel(t)
	update(t, m, "a")
	update(t, m, "T")
	if m.themeName != themeGraphite {
		t.Fatalf("themeName changed in form input: %q", m.themeName)
	}
	if got := m.form.Fields[0].Value; got != "T" {
		t.Fatalf("form field value = %q want T", got)
	}
}

func TestThemeCyclesInsideHelpOverlay(t *testing.T) {
	m := newTestModel(t)
	update(t, m, "?")
	if m.helpOverlay != helpKeys {
		t.Fatalf("setup: help overlay should be open, got %v", m.helpOverlay)
	}
	update(t, m, "T")
	if m.themeName != themeLight {
		t.Fatalf("themeName = %q want %q", m.themeName, themeLight)
	}
	if m.helpOverlay != helpKeys {
		t.Fatalf("theme cycling should not close help overlay, got %v", m.helpOverlay)
	}
}

// TestQuitBinding verifies `q` quits the app when no overlay/form/confirm is
// active (ctrl+c also quits; both set quitting=true and return tea.Quit).
func TestQuitBinding(t *testing.T) {
	m := newTestModel(t)
	mm, cmd := m.Update(keyMsg("q"))
	out, ok := mm.(*Model)
	if !ok {
		t.Fatalf("Update did not return *Model")
	}
	if !out.quitting {
		t.Errorf("after q: quitting = false want true")
	}
	if cmd == nil {
		t.Errorf("after q: cmd = nil want tea.Quit")
	}
}

// --- Step 2: project create form ---

// TestProjectCreateFormEmptySubmitDisabled verifies that an empty code field
// fails validation (form.valid() == false), so submit is disabled.
func TestProjectCreateFormEmptySubmitDisabled(t *testing.T) {
	m := newTestModel(t)
	update(t, m, "a") // open create form
	if m.form == nil || !m.form.Active {
		t.Fatalf("create form not open")
	}
	// Empty code + empty name -> invalid.
	if m.form.valid() {
		t.Errorf("empty form should be invalid (submit disabled)")
	}
}

// TestProjectCreateFormInvalidCode verifies lowercase input fails the live
// per-field validator, then uppercase ATM passes.
func TestProjectCreateFormInvalidCode(t *testing.T) {
	m := newTestModel(t)
	update(t, m, "a")
	if m.form == nil {
		t.Fatalf("form not open")
	}
	// Type lowercase "atm" into the code field.
	for _, r := range "atm" {
		update(t, m, string(r))
	}
	errMsg := m.form.fieldError(0)
	if errMsg == "" {
		t.Errorf("lowercase code should fail validation")
	}
	if !strings.Contains(errMsg, "uppercase") {
		t.Errorf("lowercase error = %q, want mention of 'uppercase'", errMsg)
	}
	// Backspace the lowercase input, type valid "ATM".
	for range "atm" {
		update(t, m, "backspace")
	}
	for _, r := range "ATM" {
		update(t, m, string(r))
	}
	if m.form.fieldError(0) != "" {
		t.Errorf("valid ATM code still has error: %q", m.form.fieldError(0))
	}
}

// TestProjectCreateFormValidCreates verifies that filling code+name and
// submitting creates the project and the list shows it.
func TestProjectCreateFormValidCreates(t *testing.T) {
	m := newTestModel(t)
	// Use a width where the Projects pane can show the full project name in
	// its NAME column (the column now truncates to fit the pane rather than
	// overflowing and clipping the UPDATED column — see ATM-46f820).
	m.SetSize(160, 30)
	update(t, m, "a")
	for _, r := range "ATM" {
		update(t, m, string(r))
	}
	update(t, m, "tab") // move to name field
	for _, r := range "Acme Task Manager" {
		update(t, m, string(r))
	}
	// Enter on last field submits.
	update(t, m, "enter")
	if m.form != nil {
		t.Errorf("form should be closed after submit")
	}
	mustContain(t, m.View(), "ATM")
	mustContain(t, m.View(), "Acme Task Manager")
}

// TestProjectCreateFormConflict verifies creating ATM twice yields a conflict
// toast "4 conflict: code ATM exists".
func TestProjectCreateFormConflict(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme Task Manager")
	// Open create form and submit ATM again.
	update(t, m, "a")
	for _, r := range "ATM" {
		update(t, m, string(r))
	}
	update(t, m, "tab")
	for _, r := range "Acme Task Manager" {
		update(t, m, string(r))
	}
	update(t, m, "enter")
	if m.toastMsg == "" || !strings.Contains(m.toastMsg, "4 conflict: code ATM exists") {
		t.Errorf("toast = %q, want 4 conflict: code ATM exists", m.toastMsg)
	}
}

// TestProjectCreateFormNoActor verifies the first-run flow: launching the TUI
// without --actor defaults the actor to "admin@tui:unset", so [a] opens the
// create form with no actor field (the form only collects code + name).
func TestProjectCreateFormNoActor(t *testing.T) {
	m := newTestModelWithActor(t, "")
	if m.actor != "admin@tui:unset" {
		t.Fatalf("actor = %q want %q", m.actor, "admin@tui:unset")
	}
	if !m.canMutate() {
		t.Fatalf("canMutate = false want true (actor defaults to admin@tui:unset)")
	}
	update(t, m, "a")
	if m.form == nil || !m.form.Active {
		t.Fatalf("pressing [a] did not open the create form")
	}
	for _, f := range m.form.Fields {
		if f.Label == "actor" {
			t.Errorf("create form should not collect an actor (actor defaults to admin@tui:unset); got actor field")
		}
	}
}

// TestOverlayRendersDimmedBackdropWithModal verifies the create-project form
// renders as a centered modal over a dimmed `░` backdrop: the modal content is
// present, the backdrop shade is present, and the underlying workspace text
// is replaced by the dim shade (not visible through the modal).
func TestOverlayRendersDimmedBackdropWithModal(t *testing.T) {
	m := newTestModel(t)
	// Use a width where the Projects pane can show the full project name in
	// its NAME column (the column now truncates to fit the pane rather than
	// overflowing and clipping the UPDATED column — see ATM-46f820).
	m.SetSize(160, 30)
	seedProject(t, m, "ATM", "Acme Task Manager")
	base := m.View()
	mustContain(t, base, "Acme Task Manager")
	update(t, m, "a")
	withOverlay := m.View()
	mustContain(t, withOverlay, "New project")
	mustContain(t, withOverlay, "░")
	mustNotContain(t, withOverlay, "Acme Task Manager")
}

// --- Step 3: projects list + detail ---

// TestProjectsListEmpty verifies the empty-store landing (mockup Screen 1).
func TestProjectsListEmpty(t *testing.T) {
	m := newTestModel(t)
	v := m.projects.View()
	mustContain(t, v, "no projects")
	mustContain(t, v, "press [a] to add a project")
}

// TestProjectsListPopulated verifies columns CODE/NAME/TASKS/LABELS/UPDATED
// and the selection gutter marker (mockup Screen 3).
func TestProjectsListPopulated(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(200, 35)
	seedProject(t, m, "ATM", "Acme Task Manager")
	seedProject(t, m, "SCY", "Scylla")
	v := m.View()
	body := m.projects.View()
	if strings.HasPrefix(body, "Projects\n") {
		t.Fatalf("projects body repeats tab title\n--- body ---\n%s", body)
	}
	mustNotContain(t, body, "─ Overview ─")
	mustContain(t, body, "total projects: 2")
	mustContain(t, body, "selected: none")
	mustContain(t, body, "showing 1-2 of 2")
	for _, col := range []string{"CODE", "NAME"} {
		mustContain(t, v, col)
	}
	mustContain(t, v, "ATM")
	mustContain(t, v, "Acme Task Manager")
	mustContain(t, v, "SCY")
	// Select ATM via [s]; the gutter marker appears on the ATM row.
	// cursor starts at 0 (ATM, since code-asc sort).
	if m.projects.cursor != 0 {
		t.Errorf("cursor = %d want 0", m.projects.cursor)
	}
	update(t, m, "s")
	if m.projectScope != "ATM" {
		t.Errorf("after s: projectScope = %q want ATM", m.projectScope)
	}
	selected := m.View()
	mustContain(t, m.projects.View(), "selected: ATM")
	mustContain(t, selected, "▸")
}

func TestProjectsListRendersSummaryRegionBelowList(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	seedProject(t, m, "ATM", "Acme Task Manager")
	body := m.projects.View()
	mustNotContain(t, body, "─ Overview ─")
	mustContain(t, body, "total projects: 1")
	mustContain(t, body, "Project Summary")
	mustContain(t, body, "select a project to see summaries")
	captionIdx := strings.Index(body, "total projects: 1")
	summaryIdx := strings.Index(body, "Project Summary")
	if captionIdx < 0 || summaryIdx < 0 || summaryIdx <= captionIdx {
		t.Fatalf("summary should render below the list caption\n--- body ---\n%s", body)
	}
}

func TestProjectsListSummaryUsesSelectedProjectNotCursor(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	seedProject(t, m, "ATM", "Acme Task Manager")
	seedProject(t, m, "SCY", "Scylla")
	update(t, m, "s")
	update(t, m, "j")
	body := m.projects.View()
	mustContain(t, body, "project: ATM")
	if m.projectScope != "ATM" {
		t.Fatalf("projectScope = %q want ATM", m.projectScope)
	}
}

func TestProjectsListOverflowSentinelRendersWithinHeight(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	codes := []string{"AAA", "AAB", "AAC", "AAD", "AAE", "AAF", "AAG", "AAH", "AAI", "AAJ"}
	for i, code := range codes {
		seedProject(t, m, code, fmt.Sprintf("Project %02d", i))
	}
	body := m.projects.View()
	mustContain(t, body, "showing ")
	lines := strings.Split(body, "\n")
	if len(lines) > 40 {
		t.Fatalf("projects view has %d lines, want <= 40\n--- body ---\n%s", len(lines), body)
	}
}

// TestProjectsListScrollsWithCursor verifies the list window follows the
// cursor: a project seeded past the first page is not rendered until the
// cursor reaches it (regression guard for the "cursor runs off-screen while
// the list stays still" bug).
func TestProjectsListScrollsWithCursor(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 30)
	codes := []string{"AAA", "AAB", "AAC", "AAD", "AAE", "AAF", "AAG", "AAH", "AAI", "AAJ"}
	for i, code := range codes {
		seedProject(t, m, code, fmt.Sprintf("Project %02d", i))
	}
	last := codes[len(codes)-1]
	if strings.Contains(m.projects.View(), last) {
		t.Fatalf("expected %s to be scrolled out of view initially:\n%s", last, m.projects.View())
	}
	m.projects.cursor = len(codes) - 1
	view := m.projects.View()
	if !strings.Contains(view, last) {
		t.Fatalf("cursor on %s but it is not visible:\n%s", last, view)
	}
}

// TestProjectsBracketKeysPageThroughList verifies "]"/"[" jump the cursor a
// full page forward/backward.
func TestProjectsBracketKeysPageThroughList(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 30)
	codes := []string{"AAA", "AAB", "AAC", "AAD", "AAE", "AAF", "AAG", "AAH", "AAI", "AAJ"}
	for i, code := range codes {
		seedProject(t, m, code, fmt.Sprintf("Project %02d", i))
	}
	start := m.projects.cursor
	update(t, m, "]")
	if m.projects.cursor <= start {
		t.Fatalf("] should move cursor forward, got %d (was %d)", m.projects.cursor, start)
	}
	after := m.projects.cursor
	update(t, m, "[")
	if m.projects.cursor >= after {
		t.Fatalf("[ should move cursor backward, got %d (was %d)", m.projects.cursor, after)
	}
}

func TestProjectsViewUsesThirtySeventySplit(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 30)
	seedProject(t, m, "ATM", "Acme Task Manager")
	body := m.projects.View()
	lines := strings.Split(body, "\n")
	summaryLine := -1
	for i, line := range lines {
		if strings.Contains(line, "Project Summary") {
			summaryLine = i
			break
		}
	}
	if summaryLine != 8 {
		t.Fatalf("summary divider is on line %d, want 8\n--- body ---\n%s", summaryLine, body)
	}
}

func TestProjectDetailDoesNotRenderSummaryCharts(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	seedProject(t, m, "ATM", "Acme Task Manager")
	update(t, m, "s")
	update(t, m, "enter")
	body := m.projects.View()
	mustContain(t, body, "Project ATM")
	mustNotContain(t, body, "Project Summary")
	mustNotContain(t, body, "Activities by actor")
}

func TestProjectDetailDashboardSections(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(200, 50)
	seedProject(t, m, "ATM", "Acme Task Manager")
	update(t, m, "enter")
	v := m.projects.View()
	mustContain(t, v, "Project ATM")
	mustContain(t, v, "FACTS")
	mustContain(t, v, "code")
	mustContain(t, v, "tasks")
	mustNotContain(t, v, "Actions")
	hint := m.projects.statusHint()
	mustContain(t, hint, "[N]ame")
	mustContain(t, hint, "[H]istory")
	mustContain(t, hint, "[x]remove")
}

func TestProjectPaneSplitHeights(t *testing.T) {
	listH, summaryH := projectPaneSplitHeights(30)
	if listH != 9 || summaryH != 21 {
		t.Fatalf("projectPaneSplitHeights(30) = (%d,%d), want (9,21)", listH, summaryH)
	}
	listH, summaryH = projectPaneSplitHeights(3)
	if listH < 1 || summaryH < 1 || listH+summaryH != 3 {
		t.Fatalf("projectPaneSplitHeights(3) = (%d,%d), want positive heights summing to 3", listH, summaryH)
	}
	listH, summaryH = projectPaneSplitHeights(1)
	if listH != 1 || summaryH != 0 {
		t.Fatalf("projectPaneSplitHeights(1) = (%d,%d), want (1,0)", listH, summaryH)
	}
}

func TestActivityStripeDayCountsUsesOneWeekEndingToday(t *testing.T) {
	mustTime := func(s string) time.Time {
		t.Helper()
		ts, err := time.Parse(time.RFC3339, s)
		if err != nil {
			t.Fatalf("time.Parse(%q): %v", s, err)
		}
		return ts
	}
	entries := []store.LogEntry{
		{At: mustTime("2026-07-01T10:00:00Z")},
		{At: mustTime("2026-07-03T10:00:00Z")},
		{At: mustTime("2026-07-03T11:00:00Z")},
		{At: mustTime("2026-07-05T10:00:00Z")},
	}
	today := mustTime("2026-07-08T22:00:00Z")
	got := activityStripeDayCountsEnding(entries, 7, today)
	want := []activityStripeDay{
		{day: "2026-07-02", count: 0},
		{day: "2026-07-03", count: 2},
		{day: "2026-07-04", count: 0},
		{day: "2026-07-05", count: 1},
		{day: "2026-07-06", count: 0},
		{day: "2026-07-07", count: 0},
		{day: "2026-07-08", count: 0},
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("activityStripeDayCountsEnding() = %#v, want %#v", got, want)
	}
	if activityDensityGlyph(0) != "·" || activityDensityGlyph(1) != "░" || activityDensityGlyph(3) != "▒" || activityDensityGlyph(6) != "▓" || activityDensityGlyph(10) != "█" {
		t.Fatalf("activityDensityGlyph returned unexpected density marks")
	}
}

func TestSelectedProjectSummaryRendersCharts(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(140, 48)
	seedProject(t, m, "ATM", "Acme Task Manager")
	seedTask(t, m, "ATM", "bug one", "ATM:status:open", "ATM:type:bug", "ATM:urgent")
	seedTask(t, m, "ATM", "bug two", "ATM:status:open", "ATM:type:bug")
	update(t, m, "s")
	body := m.projects.View()
	mustContain(t, body, "activity by persona")
	mustContain(t, body, "developer")
	mustContain(t, body, "%")
	mustContain(t, body, "activity stripe")
	mustContain(t, body, "Ubiquitous Language")
	mustContain(t, body, "no vocabulary yet")
	mustNotContain(t, body, "Activities by actor")
	mustNotContain(t, body, "Activity stripe")
}

func TestSelectedProjectSummaryRendersActivityInCompactPane(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(100, 14)
	seedProject(t, m, "ATM", "Acme Task Manager")
	seedTask(t, m, "ATM", "bug one", "ATM:status:open", "ATM:type:bug")
	update(t, m, "s")
	body := m.projects.View()
	mustContain(t, body, "activity by persona")
	mustContain(t, body, "activity stripe")
	mustContain(t, body, "█")
}

func TestProjectSummaryTinyHeightStillRendersActivity(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(80, 20)
	seedProject(t, m, "ATM", "Acme Task Manager")
	seedTask(t, m, "ATM", "bug one", "ATM:status:open", "ATM:type:bug")
	update(t, m, "s")
	body := m.projects.renderSummary(5)
	mustContain(t, body, "Project Summary")
	mustContain(t, body, "activity by persona")
	mustContain(t, body, "activity stripe")
}

func TestProjectSummaryClearsWhenSelectedProjectRemoved(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	seedProject(t, m, "ATM", "Acme Task Manager")
	seedProject(t, m, "SCY", "Scylla")
	update(t, m, "s")
	if m.projectScope != "ATM" {
		t.Fatalf("projectScope = %q want ATM", m.projectScope)
	}
	update(t, m, "x")
	update(t, m, "enter")
	if m.projectScope != "" {
		t.Fatalf("projectScope after removal = %q want empty", m.projectScope)
	}
	body := m.projects.View()
	mustContain(t, body, "select a project to see summaries")
	mustNotContain(t, body, "activity by persona")
	mustNotContain(t, body, "activity stripe")
}

func TestProjectSummaryRendersOnShortTerminalWithoutPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("View panicked on short terminal: %v", r)
		}
	}()
	m := newTestModel(t)
	m.SetSize(50, 8)
	seedProject(t, m, "ATM", "Acme Task Manager")
	update(t, m, "s")
	body := m.projects.View()
	mustContain(t, body, "Project Summary")
	_ = m.View()
}

func TestKeywordSummaryDoesNotOpenFormOrConfirm(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	seedProject(t, m, "ATM", "Acme Task Manager")
	update(t, m, "s")
	body := m.projects.View()
	mustContain(t, body, "Ubiquitous Language")
	if m.form != nil {
		t.Fatalf("bubble placeholder opened form")
	}
	if m.confirm != confirmNone {
		t.Fatalf("bubble placeholder opened confirm = %v", m.confirm)
	}
}

func TestRenderActivityStripeDeterministic(t *testing.T) {
	days := []activityStripeDay{
		{day: "2026-07-01", count: 1},
		{day: "2026-07-02", count: 3},
		{day: "2026-07-03", count: 10},
	}
	got := renderActivityStripe(days)
	want := "░▒█"
	if got != want {
		t.Fatalf("renderActivityStripe() = %q, want %q", got, want)
	}
}

func TestRenderActorActivityChartShowsOverflowSummary(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	p := newProjectsModel(m)
	p.SetSize(120, 40)
	entries := []store.LogEntry{
		{Actor: "a@x:1"}, {Actor: "a@x:1"}, {Actor: "a@x:1"}, {Actor: "a@x:1"}, {Actor: "a@x:1"},
		{Actor: "b@x:1"}, {Actor: "b@x:1"}, {Actor: "b@x:1"}, {Actor: "b@x:1"},
		{Actor: "c@x:1"}, {Actor: "c@x:1"}, {Actor: "c@x:1"},
		{Actor: "d@x:1"}, {Actor: "d@x:1"},
		{Actor: "e@x:1"},
	}
	lines := p.renderPersonaActivityChart(entries, 5)
	got := strings.Join(lines, "\n")
	mustContain(t, got, "activity by persona")
	// entryCap = 3; the chart caps at 3 persona groups (no "others" fold).
	mustContain(t, got, "a")
	mustContain(t, got, "b")
	mustContain(t, got, "c")
	mustNotContain(t, got, "others")
}

func TestRenderActorActivityChartUsesMeterStyle(t *testing.T) {
	m := newTestModel(t)
	p := newProjectsModel(m)
	p.SetSize(80, 20)
	entries := []store.LogEntry{
		{Actor: "claude@x:1"}, {Actor: "claude@x:1"},
		{Actor: "codex@x:1"},
	}
	got := strings.Join(p.renderPersonaActivityChart(entries, 4), "\n")
	mustContain(t, got, "activity by persona")
	mustContain(t, got, "claude")
	mustContain(t, got, "67%")
	mustContain(t, got, "codex")
	mustContain(t, got, "33%")
	mustContain(t, got, "█")
}

func TestRenderActorActivityChartShowsFullActorName(t *testing.T) {
	m := newTestModel(t)
	p := newProjectsModel(m)
	p.SetSize(120, 20)
	entries := []store.LogEntry{
		{Actor: "very-long-agent-name-with-role@x:1"},
	}
	got := strings.Join(p.renderPersonaActivityChart(entries, 4), "\n")
	mustContain(t, got, "very-long-agent-name-with-role")
	mustNotContain(t, got, "very-lo...")
}

// TestRenderActorActivityChartBarsAlignAcrossRows asserts that every actor
// row's meter bar starts at the same display column, regardless of the count's
// digit width. The chart box center-aligns each body line independently; if
// rows have different widths (e.g. counts "200", "50", "5"), narrower rows get
// shifted right and the bars no longer share a common left baseline.
func TestRenderActorActivityChartBarsAlignAcrossRows(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	p := newProjectsModel(m)
	p.SetSize(120, 40)
	// Counts with different digit widths: 200 (3d), 50 (2d), 5 (1d).
	entries := make([]store.LogEntry, 0, 255)
	for i := 0; i < 200; i++ {
		entries = append(entries, store.LogEntry{Actor: "aaa@x:1"})
	}
	for i := 0; i < 50; i++ {
		entries = append(entries, store.LogEntry{Actor: "bb@x:1"})
	}
	for i := 0; i < 5; i++ {
		entries = append(entries, store.LogEntry{Actor: "c@x:1"})
	}

	lines := p.renderPersonaActivityChart(entries, 6)

	ansiRe := regexp.MustCompile("\x1b\\[[0-9;]*m")
	var barCols []int
	for _, line := range lines {
		s := ansiRe.ReplaceAllString(line, "")
		// Body rows are bounded by box borders '│'. Skip border/title/blank rows.
		if !strings.HasPrefix(s, "  │") {
			continue
		}
		idx := strings.IndexAny(s, "█░")
		if idx < 0 {
			continue
		}
		barCols = append(barCols, idx)
	}
	if len(barCols) < 2 {
		t.Fatalf("expected at least 2 bar rows, got %d (%v)\n--- body ---\n%s", len(barCols), barCols, strings.Join(lines, "\n"))
	}
	first := barCols[0]
	for i, c := range barCols {
		if c != first {
			t.Fatalf("bar start column differs across rows: row 0 at col %d, row %d at col %d (all=%v)\n--- body ---\n%s", first, i, c, barCols, strings.Join(lines, "\n"))
		}
	}
}

func TestProjectSummaryChartBoxesAreCentered(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(100, 40)
	seedProject(t, m, "ATM", "Acme Task Manager")
	update(t, m, "s")
	body := m.projects.View()
	lines := strings.Split(body, "\n")
	for _, line := range lines {
		if strings.Contains(line, "activity by persona") && strings.Contains(line, "╭") {
			if strings.HasPrefix(line, "╭") {
				t.Fatalf("chart box should be centered with left padding, got %q\n--- body ---\n%s", line, body)
			}
			return
		}
	}
	t.Fatalf("missing centered activity by persona box\n--- body ---\n%s", body)
}

func TestProjectSummaryChartBoxesUseNinetyFivePercentWidth(t *testing.T) {
	if got := chartBoxWidth(100); got < 95 {
		t.Fatalf("chartBoxWidth(100) = %d, want at least 95", got)
	}
}

func TestProjectSummaryChartBoxesFillRemainingSummarySpace(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	seedProject(t, m, "ATM", "Acme Task Manager")
	update(t, m, "s")
	body := m.projects.renderSummary(24)
	lines := strings.Split(body, "\n")
	if len(lines) != 24 {
		t.Fatalf("renderSummary(24) lines = %d, want 24\n--- body ---\n%s", len(lines), body)
	}
	if strings.TrimSpace(lines[len(lines)-1]) == "" {
		t.Fatalf("summary should fill the last allocated line with chart content\n--- body ---\n%s", body)
	}
}

func TestRenderChartBoxDimsBorderAndCentersContent(t *testing.T) {
	m := newTestModel(t)
	p := newProjectsModel(m)
	p.SetSize(80, 20)
	got := p.renderChartBox("activity stripe", "█", 7)
	lines := strings.Split(got, "\n")
	if len(lines) != 7 {
		t.Fatalf("renderChartBox lines = %d, want 7\n%s", len(lines), got)
	}
	mustContain(t, got, "╭")
	mustContain(t, got, "╰")
	centerLine := lines[len(lines)/2]
	if !strings.Contains(centerLine, "█") {
		t.Fatalf("chart content should be vertically centered, got middle line %q\n%s", centerLine, got)
	}
}

func TestRenderActivityStripeCanvasUsesMultiLineChart(t *testing.T) {
	days := []activityStripeDay{
		{day: "2026-07-01", count: 1},
		{day: "2026-07-02", count: 3},
		{day: "2026-07-03", count: 10},
		{day: "2026-07-04", count: 0},
		{day: "2026-07-05", count: 0},
		{day: "2026-07-06", count: 0},
		{day: "2026-07-07", count: 0},
	}
	got := renderActivityStripeCanvas(days, 70)
	lines := strings.Split(strings.TrimRight(got, "\n"), "\n")
	if len(lines) < 2 {
		t.Fatalf("renderActivityStripeCanvas() should render a multi-line canvas, got %q", got)
	}
	mustContain(t, got, "█")
	mustContain(t, got, "7d ago")
	mustContain(t, got, "Today")
	if activityCanvasStyle(10).GetForeground() == nil {
		t.Fatalf("activityCanvasStyle should configure foreground color")
	}
	// Density fill: count 10 → █, count 3 → ▅, count 1 → ▂, count 0 → ·
	mustContain(t, got, "█")
	mustContain(t, got, "▅")
	mustContain(t, got, "▂")
	mustContain(t, got, "·")
	mustContain(t, got, "7d ago")
	mustContain(t, got, "Yesterday")
	mustContain(t, got, "Today")
	// Proportional height: render at height=5 (bodyH=4). count 10 fills 4 rows,
	// count 1 fills 1 row (clamped). Top row (index 0) should only show █ for
	// count 10 bar, and · for lower-count bars.
	got2 := renderActivityStripeCanvas(days, 70, 5)
	if got2 == "" {
		t.Fatal("renderActivityStripeCanvas(height=5) returned empty")
	}
	lines2 := strings.Split(strings.TrimRight(got2, "\n"), "\n")
	if len(lines2) < 5 {
		t.Fatalf("expected 5 lines (4 bar + axis), got %d:\n%s", len(lines2), got2)
	}
	if !strings.Contains(lines2[0], "█") {
		t.Fatal("top row should contain █ for count 10 bar")
	}
}

func TestComputeStripDaysRange(t *testing.T) {
	tests := []struct {
		width    int
		wantDays int
	}{
		{width: 10, wantDays: 7},
		{width: 69, wantDays: 7},   // (69+1)/10 = 7
		{width: 79, wantDays: 8},   // (79+1)/10 = 8
		{width: 139, wantDays: 14}, // (139+1)/10 = 14
		{width: 200, wantDays: 14},
	}
	for _, tc := range tests {
		got := computeStripDays(tc.width)
		if got != tc.wantDays {
			t.Errorf("computeStripDays(%d) = %d, want %d", tc.width, got, tc.wantDays)
		}
	}
}

func TestActivityStripeAxisLabels(t *testing.T) {
	days := make([]activityStripeDay, 7)
	for i := range days {
		days[i] = activityStripeDay{day: fmt.Sprintf("2026-07-%02d", i+1), count: i}
	}
	got := activityStripeAxis(days, 70, 9, 1)
	mustContain(t, got, "7d ago")
	mustContain(t, got, "Yesterday")
	mustContain(t, got, "Today")
}
func TestRenderActivityStripeChartAdaptiveDays(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(200, 48)
	seedProject(t, m, "ATM", "Acme Task Manager")
	seedTask(t, m, "ATM", "t1", "ATM:status:open")
	update(t, m, "s")
	body := m.projects.View()
	mustContain(t, body, "activity stripe")
}

func TestActivityStripeDayCountsReturnsEmptyForNoEvents(t *testing.T) {
	got := activityStripeDayCounts(nil, 7)
	if len(got) != 7 {
		t.Fatalf("activityStripeDayCounts(nil) len = %d, want 7", len(got))
	}
	for _, day := range got {
		if day.count != 0 {
			t.Fatalf("activityStripeDayCounts(nil) = %#v, want all zero counts", got)
		}
	}
}

func TestRenderActivityStripeIncludesQuietDaysWithinWindow(t *testing.T) {
	days := []activityStripeDay{
		{day: "2026-07-01", count: 1},
		{day: "2026-07-02", count: 0},
		{day: "2026-07-03", count: 3},
	}
	got := renderActivityStripe(days)
	want := "░·▒"
	if got != want {
		t.Fatalf("renderActivityStripe() = %q, want %q", got, want)
	}
}

// TestProjectsListCursorVsSelectionIndependent verifies the cursor is
// independent of the selection (mockup "Selection model"): cursoring down to
// SCY does not move the ATM selection.
func TestProjectsListCursorVsSelectionIndependent(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme Task Manager")
	seedProject(t, m, "SCY", "Scylla")
	// cursor on ATM (row 0); select it.
	update(t, m, "s")
	if m.projectScope != "ATM" {
		t.Fatalf("projectScope = %q want ATM", m.projectScope)
	}
	// Move cursor down to SCY; selection stays ATM.
	update(t, m, "j")
	if m.projects.cursor != 1 {
		t.Fatalf("cursor = %d want 1", m.projects.cursor)
	}
	if m.projectScope != "ATM" {
		t.Errorf("cursor moved but projectScope changed to %q; should stay ATM", m.projectScope)
	}
	// Status line should still report SELECTED: ATM.
	mustContain(t, m.View(), "SELECTED: ATM")
}

// TestProjectDetailNoLabelsSection verifies the project detail no longer
// renders a LABELS section (label management moved to the Labels pane).
func TestProjectDetailNoLabelsSection(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme Task Manager")
	seedLabel(t, m, "ATM:custom:x", "custom")
	update(t, m, "enter") // open ATM detail (cursor on ATM)
	v := m.projects.View()
	mustContain(t, v, "Project ATM")
	mustNotContain(t, v, "LABELS")
	// The labels count fact line stays.
	mustContain(t, v, "labels")
}

// TestProjectDetailLabelKeysNoOp verifies L and l do nothing in project detail.
func TestProjectDetailLabelKeysNoOp(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme Task Manager")
	update(t, m, "enter")
	update(t, m, "L")
	update(t, m, "l")
	if m.form != nil {
		t.Errorf("L/l opened a form in project detail (should be a no-op)")
	}
}

// TestProjectDetailHistoryToggle verifies the UPPERCASE [H] binding toggles
// the HISTORY section in the project detail (mockup Screen 4; Task 5 fix).
func TestProjectDetailHistoryToggle(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(200, 70)
	seedProject(t, m, "ATM", "Acme Task Manager")
	update(t, m, "enter")
	v := m.View()
	// HISTORY off by default.
	mustNotContain(t, v, "HISTORY")
	// Press uppercase H to toggle on.
	update(t, m, "H")
	if !m.projects.detail.historyOn {
		t.Errorf("after H: detail.historyOn = false want true")
	}
	v = m.View()
	mustContain(t, v, "HISTORY")
	// Press H again to toggle off.
	update(t, m, "H")
	if m.projects.detail.historyOn {
		t.Errorf("after second H: detail.historyOn = true want false")
	}
}

// --- Step 4: label add/remove forms (moved to Labels pane) ---

// --- Step 5: tasks flat + grouped ---

// TestTasksFlatListEmptyFilter verifies the persistent header line for the
// flat list with no filter (mockup Screen 6).
func TestTasksFlatListEmptyFilter(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme Task Manager")
	tk := seedTask(t, m, "ATM", "task one", "ATM:status:open")
	update(t, m, "s") // select ATM
	// Task 4: project-select now defaults the Tasks pane to the Open Tasks
	// board; clear it here so this test can verify the unfiltered flat list.
	m.tasks.setFocus(taskFocus{mode: focusOff}, "")
	update(t, m, "2") // focus Tasks pane
	v := m.View()
	body := m.tasks.View()
	if strings.HasPrefix(body, "Tasks\n") {
		t.Fatalf("tasks body repeats tab title\n--- body ---\n%s", body)
	}
	mustNotContain(t, body, "─ Overview ─")
	mustContain(t, body, "ID")
	mustContain(t, body, "TITLE")
	mustContain(t, body, "LABELS")
	mustContain(t, body, "UPDATED")
	mustContain(t, v, "PROJECT: ATM")
	mustContain(t, v, "FOCUS: (all)")
	mustContain(t, v, "SORT: updated-desc")
	mustContain(t, v, "task one")
	mustContain(t, v, tk.ID)
	mustContain(t, v, "ATM:status:open")
	mustContain(t, v, "showing 1-1 of 1")
}

// TestTasksFlatListExactFilterRestricts verifies an exact filter token narrows
// the list (AND-intersect across multiple tokens).
func TestTasksFlatListExactFilterRestricts(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme Task Manager")
	seedTask(t, m, "ATM", "open bug", "ATM:status:open", "ATM:type:bug")
	seedTask(t, m, "ATM", "open feature", "ATM:status:open", "ATM:type:feature")
	update(t, m, "s")
	update(t, m, "2")
	m.tasks.setFocus(taskFocus{mode: focusOff}, "ATM:status:open ATM:type:bug")
	v := m.View()
	mustContain(t, v, "open bug")
	mustNotContain(t, v, "open feature")
}

// TestTasksPagingFooter verifies the "showing M-N of T" footer (mockup Screen
// 6). We seed more tasks than one page holds and check the footer text.
func TestTasksPagingFooter(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme Task Manager")
	// Seed enough tasks to exceed one page (pageSize = contentHeight - 4).
	// Default contentHeight for 30-row terminal = 26, so pageSize ~ 22.
	// Seed 25 tasks.
	for i := 0; i < 25; i++ {
		seedTask(t, m, "ATM", "task "+string(rune('A'+i)))
	}
	update(t, m, "s")
	// Task 4: clear the Open Tasks board default so all 25 (unlabeled) tasks
	// are visible for this paging test.
	m.tasks.setFocus(taskFocus{mode: focusOff}, "")
	update(t, m, "2")
	v := m.View()
	mustContain(t, v, "showing ")
	mustContain(t, v, "of 25")
}

// TestTasksFlatListScrollsWithCursor verifies the flat list window follows
// the cursor: a task seeded past the first page is not rendered until the
// cursor reaches it (regression guard for lists whose window never moves).
func TestTasksFlatListScrollsWithCursor(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme Task Manager")
	for i := 0; i < 25; i++ {
		seedTask(t, m, "ATM", "task "+string(rune('A'+i)))
	}
	update(t, m, "s")
	// Task 4: clear the Open Tasks board default so all 25 (unlabeled) tasks
	// are visible for this scrolling test.
	m.tasks.setFocus(taskFocus{mode: focusOff}, "")
	update(t, m, "2")

	rows := m.tasks.rows
	if len(rows) != 25 {
		t.Fatalf("expected 25 rows, got %d", len(rows))
	}
	last := rows[len(rows)-1]
	if strings.Contains(m.tasks.View(), last.id) {
		t.Fatalf("expected %s to be scrolled out of view initially:\n%s", last.id, m.tasks.View())
	}
	m.tasks.cursor = len(rows) - 1
	view := m.tasks.View()
	if !strings.Contains(view, last.id) {
		t.Fatalf("cursor on %s but it is not visible:\n%s", last.id, view)
	}
}

// TestTasksFlatListPageKeysPageThroughList verifies pgdown/pgup jump the
// cursor a full page forward/backward in the flat list. (Relocated from
// "]"/"[", which now cycle the board ring — Task 7.)
func TestTasksFlatListPageKeysPageThroughList(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme Task Manager")
	for i := 0; i < 25; i++ {
		seedTask(t, m, "ATM", "task "+string(rune('A'+i)))
	}
	update(t, m, "s")
	// Task 4: clear the Open Tasks board default so all 25 (unlabeled) tasks
	// are visible for this paging test.
	m.tasks.setFocus(taskFocus{mode: focusOff}, "")
	update(t, m, "2")
	start := m.tasks.cursor
	update(t, m, "pgdown")
	if m.tasks.cursor <= start {
		t.Fatalf("pgdown should move cursor forward, got %d (was %d)", m.tasks.cursor, start)
	}
	after := m.tasks.cursor
	update(t, m, "pgup")
	if m.tasks.cursor >= after {
		t.Fatalf("pgup should move cursor backward, got %d (was %d)", m.tasks.cursor, after)
	}
}

// TestTasksGroupedListScrollsWithCursor verifies the grouped/tree list window
// also follows the cursor (the grouped view previously never windowed at
// all, relying on padToHeight to silently truncate the bottom).
func TestTasksGroupedListScrollsWithCursor(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme Task Manager")
	for i := 0; i < 30; i++ {
		seedTask(t, m, "ATM", "task "+string(rune('A'+i)), "ATM:status:open")
	}
	update(t, m, "s")
	update(t, m, "2")
	m.tasks.setFocus(taskFocus{mode: focusOff}, "ATM:status:*")

	total := m.tasks.flatLineCount()
	lastLeaf := -1
	for i := total - 1; i >= 0; i-- {
		m.tasks.cursor = i
		if _, ok := m.tasks.rowAtCursor(); ok {
			lastLeaf = i
			break
		}
	}
	if lastLeaf < 0 {
		t.Fatalf("expected at least one leaf row in the grouped view")
	}
	m.tasks.cursor = lastLeaf
	r, _ := m.tasks.rowAtCursor()

	m.tasks.cursor = 0
	if strings.Contains(m.tasks.View(), r.id) {
		t.Fatalf("expected last leaf row %s to be scrolled out of view when cursor is at top", r.id)
	}
	m.tasks.cursor = lastLeaf
	view := m.tasks.View()
	if !strings.Contains(view, r.id) {
		t.Fatalf("cursor on last leaf row %s but it is not visible:\n%s", r.id, view)
	}
}

// TestTasksGroupedListPageKeysPageThroughList verifies pgdown/pgup jump the
// cursor a full page forward/backward in the grouped/tree list. (Relocated
// from "]"/"[", which now cycle the board ring — Task 7.)
func TestTasksGroupedListPageKeysPageThroughList(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme Task Manager")
	for i := 0; i < 12; i++ {
		seedTask(t, m, "ATM", "task "+string(rune('A'+i)), "ATM:status:open")
	}
	update(t, m, "s")
	update(t, m, "2")
	m.tasks.setFocus(taskFocus{mode: focusOff}, "ATM:status:*")
	start := m.tasks.cursor
	update(t, m, "pgdown")
	if m.tasks.cursor <= start {
		t.Fatalf("pgdown should move cursor forward, got %d (was %d)", m.tasks.cursor, start)
	}
	after := m.tasks.cursor
	update(t, m, "pgup")
	if m.tasks.cursor >= after {
		t.Fatalf("pgup should move cursor backward, got %d (was %d)", m.tasks.cursor, after)
	}
}

// TestTasksGroupedSingleWildcard verifies the grouped view for a single
// wildcard (mockup Screen 7): group headers "▾ LABEL (N)" and multi-membership.
func TestTasksGroupedSingleWildcard(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme Task Manager")
	// A task carrying both open and done (multi-membership: appears in both groups).
	seedTask(t, m, "ATM", "multi", "ATM:status:open", "ATM:status:done")
	seedTask(t, m, "ATM", "open only", "ATM:status:open")
	seedTask(t, m, "ATM", "untagged")
	update(t, m, "s")
	update(t, m, "2")
	m.tasks.setFocus(taskFocus{mode: focusOff}, "ATM:status:*")
	v := m.View()
	mustContain(t, v, "▾ ATM:status:done")
	mustContain(t, v, "▾ ATM:status:open")
	// (no matching labels) bucket last.
	mustContain(t, v, "▾ (no matching labels)")
	// The untagged task lands in the bucket.
	mustContain(t, v, "untagged")
	// Multi-membership: "multi" appears in the view twice (once per group).
	multiCount := strings.Count(v, "multi")
	if multiCount < 2 {
		t.Errorf("multi-membership: 'multi' appears %d time(s), want >=2", multiCount)
	}
}

// TestTasksGroupedNestedWildcards verifies nested facets for two wildcards
// (mockup Screen 7, two-wildcard case).
func TestTasksGroupedNestedWildcards(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(160, 70)
	seedProject(t, m, "ATM", "Acme Task Manager")
	seedTask(t, m, "ATM", "a", "ATM:status:open", "ATM:type:bug")
	seedTask(t, m, "ATM", "b", "ATM:status:open", "ATM:type:task")
	seedTask(t, m, "ATM", "c", "ATM:status:done", "ATM:type:bug")
	seedTask(t, m, "ATM", "untagged")
	update(t, m, "s")
	update(t, m, "2")
	m.tasks.setFocus(taskFocus{mode: focusOff}, "ATM:status:* ATM:type:*")
	v := m.tasks.View()
	mustContain(t, v, "▾ ATM:status:open")
	mustContain(t, v, "▾ ATM:status:done")
	// Nested sub-groups (indented by two spaces).
	mustContain(t, v, "▾ ATM:type:bug")
	mustContain(t, v, "▾ ATM:type:task")
	// (no matching labels) bucket last.
	mustContain(t, v, "▾ (no matching labels)")
}

// TestTasksGroupedNoMatchingLabelsBucket verifies the (no matching labels)
// bucket is always rendered, last, even when empty.
func TestTasksGroupedNoMatchingLabelsBucket(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme Task Manager")
	seedTask(t, m, "ATM", "open", "ATM:status:open")
	seedTask(t, m, "ATM", "done", "ATM:status:done")
	update(t, m, "s")
	update(t, m, "2")
	m.tasks.setFocus(taskFocus{mode: focusOff}, "ATM:status:*")
	v := m.View()
	// Bucket present (count 0 here since every task matches the wildcard).
	mustContain(t, v, "(no matching labels)")
	// Ensure bucket appears after the group headers: find the group header
	// index and the bucket index.
	openIdx := strings.Index(v, "▾ ATM:status:open")
	bucketIdx := strings.Index(v, "▾ (no matching labels)")
	if openIdx < 0 || bucketIdx < 0 {
		t.Fatalf("missing group header or bucket in view")
	}
	if bucketIdx < openIdx {
		t.Errorf("bucket should appear after group headers; bucketIdx=%d openIdx=%d", bucketIdx, openIdx)
	}
}

// --- Step 6: task detail + empty states ---

// TestTaskDetailFactsLabelsHistory verifies the task detail (mockup Screen 8):
// facts, label chips, and HISTORY behind the [H] overlay (opened in-test).
func TestTaskDetailFactsLabelsHistory(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(160, 80)
	seedProject(t, m, "ATM", "Acme Task Manager")
	tk := seedTask(t, m, "ATM", "Fix label reconciliation", "ATM:status:in-progress", "ATM:type:bug")
	// Add a label after creation to get a second history entry.
	if err := m.store.TaskLabelAdd(tk.ID, "ATM:priority:high", testActor); err != nil {
		t.Fatalf("TaskLabelAdd: %v", err)
	}
	m.refreshAll()
	update(t, m, "s")
	// Task 4: the task carries status:in-progress, not status:open, so it is
	// excluded from the default Open Tasks board; clear focus to see it.
	m.tasks.setFocus(taskFocus{mode: focusOff}, "")
	update(t, m, "2")
	// Cursor on the task (row 0); open detail.
	update(t, m, "enter")
	// Detail-mode facts + labels render by default; History is hidden
	// behind the [H] overlay.
	v := m.tasks.View()
	mustContain(t, v, "Task "+tk.ID)
	mustContain(t, v, "FACTS")
	mustContain(t, v, "id      "+tk.ID)
	mustContain(t, v, "project ATM")
	mustContain(t, v, "title   Fix label reconciliation")
	mustContain(t, v, "LABELS")
	mustContain(t, v, "ATM:status:in-progress")
	mustContain(t, v, "ATM:type:bug")
	mustContain(t, v, "ATM:priority:high")
	mustNotContain(t, v, "Actions")
	hint := m.tasks.statusHint()
	mustContain(t, hint, "[e]title")
	mustContain(t, hint, "[b]add label")
	if strings.Contains(v, "task.created") {
		t.Fatalf("history must be hidden behind [H] overlay by default, found task.created:\n%s", v)
	}
	// Open the history overlay: it replaces the detail view while active.
	update(t, m, "H")
	if !m.tasks.historyOverlay.active {
		t.Fatal("expected history overlay active after [H]")
	}
	v = m.tasks.View()
	mustContain(t, v, "History")
	mustContain(t, v, "task.created")
	mustContain(t, v, "task.label-added")
	// History rows are decorated with [seq] and ordered chronologically
	// (the log is append-only); task.created is logged before task.label-added.
	createdIdx := strings.Index(v, "task.created")
	addedIdx := strings.Index(v, "task.label-added")
	if createdIdx < 0 || addedIdx < 0 {
		t.Fatalf("missing task.created / task.label-added in history overlay")
	}
	if addedIdx < createdIdx {
		t.Errorf("history not chronological: task.label-added (%d) before task.created (%d)", addedIdx, createdIdx)
	}
	// The [seq] decoration must precede each row.
	if !strings.Contains(v, "] ") {
		t.Errorf("history rows missing [seq] decoration")
	}
	// Closing the overlay returns to the detail view.
	update(t, m, "esc")
	if m.tasks.historyOverlay.active {
		t.Fatal("Esc should have closed the history overlay")
	}
	v = m.tasks.View()
	mustContain(t, v, "Task "+tk.ID)
	mustContain(t, v, "FACTS")
}

// TestTaskDetailScrollDoesNotBreakPaneBorders pins ATM-0100: scrolling the
// task detail view must only move the Tasks pane content. The Projects pane
// (its border and its content) must stay fixed, and every workspace line
// must stay exactly m.width columns wide. The bug report described the
// borders breaking and scrolling bleeding into the other panes; this test
// pins the correct behavior so a regression is caught.
func TestTaskDetailScrollDoesNotBreakPaneBorders(t *testing.T) {
	cases := []struct{ w, h int }{
		{120, 36},
		{80, 24},
		{60, 20},
	}
	for _, c := range cases {
		t.Run(fmt.Sprintf("%dx%d", c.w, c.h), func(t *testing.T) {
			m := newTestModel(t)
			m.SetSize(c.w, c.h)
			seedProject(t, m, "ATM", "Acme Task Manager")
			desc := strings.Repeat("description line that is reasonably long\n", 30)
			tk, err := m.store.CreateTask("ATM", "Bug task with a long title to stress the pane width", desc, nil, testActor)
			if err != nil {
				t.Fatalf("CreateTask: %v", err)
			}
			m.refreshAll()
			update(t, m, "s")
			// Task 4: the task has no labels, so it is excluded from the
			// default Open Tasks board; clear focus to see it.
			m.tasks.setFocus(taskFocus{mode: focusOff}, "")
			update(t, m, "2")
			update(t, m, "enter")
			if m.tasks.view != tViewDetail {
				t.Fatalf("expected tViewDetail, got %v", m.tasks.view)
			}

			// Snapshot the Projects pane content before scrolling.
			projBefore := m.projects.View()

			// Scroll the task detail to the bottom and back.
			for i := 0; i < 40; i++ {
				update(t, m, "j")
			}
			for i := 0; i < 40; i++ {
				update(t, m, "k")
			}

			// The Projects pane must be byte-for-byte unchanged.
			if got := m.projects.View(); got != projBefore {
				t.Errorf("Projects pane changed while scrolling task detail:\nbefore:\n%s\nafter:\n%s", projBefore, got)
			}

			// Every workspace line must be exactly m.width columns wide so
			// the pane borders stay aligned vertically.
			ws := m.renderWorkspace()
			for i, line := range strings.Split(ws, "\n") {
				if w := lipgloss.Width(line); w != m.width {
					t.Errorf("workspace line %d width=%d want %d (panes misaligned):\n%s", i, w, m.width, ws)
				}
			}

			// Switching panes (1 -> 2 -> 3 -> 2) must not change the workspace
			// geometry either; it only changes which pane is focused. "3" is a
			// no-op since Task 3 removed the Boards pane; it is kept in the
			// sequence to confirm a stale/removed key press does not disturb
			// the geometry.
			update(t, m, "1")
			update(t, m, "2")
			update(t, m, "3")
			update(t, m, "2")
			for i, line := range strings.Split(m.renderWorkspace(), "\n") {
				if w := lipgloss.Width(line); w != m.width {
					t.Errorf("workspace line %d width=%d want %d after pane switches:\n%s", i, w, m.width, line)
				}
			}
			if m.tasks.view != tViewDetail {
				t.Errorf("pane switches dropped out of task detail: view=%v", m.tasks.view)
			}
			_ = tk
		})
	}
}

func TestTaskDetailLabelsRenderAsChips(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(160, 50)
	seedProject(t, m, "ATM", "Acme Task Manager")
	seedTask(t, m, "ATM", "chip task", "ATM:status:open", "ATM:type:bug")
	update(t, m, "s")
	update(t, m, "2")
	update(t, m, "enter")
	v := m.tasks.View()
	mustContain(t, v, " ATM:status:open ")
	mustContain(t, v, " ATM:type:bug ")
}

func TestDetailOpensInsideFocusedPaneNotOverlay(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 36)
	seedProject(t, m, "ATM", "Acme Task Manager")
	tk := seedTask(t, m, "ATM", "inside pane task", "ATM:status:open")
	update(t, m, "s")
	update(t, m, "2")
	update(t, m, "enter")
	if m.tasks.view != tViewDetail {
		t.Fatalf("tasks.view = %v want tViewDetail", m.tasks.view)
	}
	if m.form != nil || m.confirm != confirmNone || m.helpOverlay != helpNone {
		t.Fatalf("detail should not open an overlay: form=%v confirm=%v help=%v", m.form != nil, m.confirm, m.helpOverlay)
	}
	v := m.View()
	mustContain(t, v, "[1] Projects")
	mustContain(t, v, "[2] Tasks")
	mustContain(t, v, "Task "+tk.ID)
}

func TestEscBacksOnlyFocusedPaneOutOfDetail(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme Task Manager")
	seedTask(t, m, "ATM", "pane task")
	update(t, m, "s")
	// Task 4: the task has no labels, so it is excluded from the default
	// Open Tasks board; clear focus to see it.
	m.tasks.setFocus(taskFocus{mode: focusOff}, "")
	update(t, m, "enter")
	if m.projects.view != pViewDetail {
		t.Fatalf("setup: project detail not open")
	}
	update(t, m, "2")
	update(t, m, "enter")
	if m.tasks.view != tViewDetail {
		t.Fatalf("setup: task detail not open")
	}
	update(t, m, "esc")
	if m.tasks.view != tViewList {
		t.Fatalf("tasks should return to list")
	}
	if m.projects.view != pViewDetail {
		t.Fatalf("projects detail should stay open when Tasks is focused")
	}
}

// TestFormOverlayRendersAsModalOverDimmedBackdrop verifies the form overlay
// covers the workspace with a dim `░` backdrop on every row the modal does
// not occupy, and the modal content stays readable on top.
func TestFormOverlayRendersAsModalOverDimmedBackdrop(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 36)
	seedProject(t, m, "ATM", "Acme Task Manager")
	base := m.View()
	mustContain(t, base, "[1] Projects")
	mustContain(t, base, "[2] Tasks")
	update(t, m, "a")
	withOverlay := m.View()
	mustContain(t, withOverlay, "New project")
	mustContain(t, withOverlay, "░")
	// Underlying pane titles are replaced by the dim backdrop, not visible.
	mustNotContain(t, withOverlay, "[1] Projects")
	mustNotContain(t, withOverlay, "[2] Tasks")
}

func TestWorkspaceRendersAtCrampedSizes(t *testing.T) {
	for _, size := range []struct {
		w int
		h int
	}{
		{20, 8},
		{10, 5},
		{1, 1},
	} {
		t.Run(fmt.Sprintf("%dx%d", size.w, size.h), func(t *testing.T) {
			m := newTestModel(t)
			m.SetSize(size.w, size.h)
			_ = m.View()
		})
	}
}

// TestTasksEmptyStateNoProject verifies the no-project-selected prompt
// (mockup Screen 9, state 1).
func TestTasksEmptyStateNoProject(t *testing.T) {
	m := newTestModel(t)
	// No project selected (projectScope empty).
	update(t, m, "2")
	v := m.tasks.View()
	mustContain(t, v, "PROJECT: (none)")
	mustContain(t, v, "no project selected")
	mustContain(t, v, "press [s] in the Projects pane")
}

// TestTasksEmptyStateFocusNoMatch verifies the focus-no-match empty state.
func TestTasksEmptyStateFilterNoMatch(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme Task Manager")
	seedTask(t, m, "ATM", "open one", "ATM:status:open")
	update(t, m, "s")
	update(t, m, "2")
	m.tasks.setFocus(taskFocus{mode: focusOff}, "ATM:status:done")
	v := m.tasks.View()
	mustContain(t, v, "no tasks match this focus")
	mustContain(t, v, "switch boards with [ / ] to change focus")
}

// TestTasksEmptyStateWildcardNoLabels verifies the wildcard-yields-no-labels
// empty state (mockup Screen 9, state 3) and that the (no matching labels)
// bucket renders with all in-scope tasks.
func TestTasksEmptyStateWildcardNoLabels(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(160, 60)
	seedProject(t, m, "ATM", "Acme Task Manager")
	seedTask(t, m, "ATM", "task one", "ATM:type:bug")
	seedTask(t, m, "ATM", "task two", "ATM:status:open")
	update(t, m, "s")
	update(t, m, "2")
	m.tasks.setFocus(taskFocus{mode: focusOff}, "ATM:context:*")
	v := m.tasks.View()
	mustContain(t, v, "no labels match wildcard — add labels to tasks")
	mustContain(t, v, "▾ (no matching labels)")
	mustContain(t, v, "task one")
	mustContain(t, v, "task two")
}

// --- Step 7: help overlay ---

func TestHelpOverlayParityTable(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 35)
	update(t, m, "?")
	v := m.View()
	mustContain(t, v, "─ CLI / TUI Parity ─")
	mustNotContain(t, v, "Help tab")
	content := strings.Join(m.help.lines, "\n")
	mustContain(t, content, "atm project create")
	mustContain(t, content, "atm task create")
	mustContain(t, content, "atm conventions")
	lines := strings.Split(m.help.View(), "\n")
	if len(lines) < 3 {
		t.Fatalf("help view too short\n--- help ---\n%s", m.help.View())
	}
	if leadingSpaces(lines[1]) != leadingSpaces(lines[0]) {
		t.Fatalf("help content should align with divider: divider=%q content=%q", lines[0], lines[1])
	}
}

func TestHelpOverlayConventions(t *testing.T) {
	m := newTestModel(t)
	update(t, m, "C")
	if m.helpOverlay != helpConventions {
		t.Fatalf("help overlay should be open (conventions), got %v", m.helpOverlay)
	}
	content := strings.Join(m.help.lines, "\n")
	mustContain(t, content, "What ATM is")
	mustContain(t, content, "Where labels live")
	mustContain(t, content, "Where tasks live")
	mustContain(t, content, "How to read a task and its labels")
	mustContain(t, content, "How to search")
	mustContain(t, content, "Agent code-of-conduct")
	mustContain(t, content, "advisory")
	// The conventions text references where to find labels/tasks but does NOT
	// duplicate the seeded label list itself.
	mustNotContain(t, content, "## Suggested seed namespaces")
	mustNotContain(t, content, "## Agent code-of-conduct")
	mustNotContain(t, content, "status:open, todo, in-progress")
	// Keys-only content is NOT present in the conventions overlay.
	mustNotContain(t, content, "CLI / TUI Parity")
	mustNotContain(t, content, "Global Keymap")
}

func TestHelpOverlayKeymapUsesPaneLanguage(t *testing.T) {
	m := newTestModel(t)
	update(t, m, "?")
	content := strings.Join(m.help.lines, "\n")
	mustContain(t, content, "Global Keymap")
	mustContain(t, content, "focus pane")
	mustContain(t, content, "open keys help")
	mustContain(t, content, "open conventions")
	mustContain(t, content, "cycle theme")
	mustNotContain(t, content, "switch tab")
	// Conventions-only content is NOT present in the keys overlay.
	mustNotContain(t, content, "Suggested seed namespaces")
	mustNotContain(t, content, "advisory")
}

// --- Step 8: task detail [d] description edit + [x] remove confirm ---

// TestTaskDetailDescriptionEdit verifies the [d] key on a task detail opens
// the description-edit form, a typed edit persists to the store, and the
// updated description renders in the detail view (mockup Screen 8, [d]).
func TestTaskDetailDescriptionEdit(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme Task Manager")
	// Seed a task with a known description via the store directly.
	tk, err := m.store.CreateTask("ATM", "Wire [d] description edit", "initial desc", nil, testActor)
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	m.refreshAll()
	// Select project, switch to Tasks, open detail at cursor row 0.
	update(t, m, "s")
	// Task 4: the task has no labels, so it is excluded from the default
	// Open Tasks board; clear focus to see it.
	m.tasks.setFocus(taskFocus{mode: focusOff}, "")
	update(t, m, "2")
	update(t, m, "enter")
	if m.tasks.view != tViewDetail {
		t.Fatalf("expected task detail view, got %v", m.tasks.view)
	}
	// The initial description renders.
	mustContain(t, m.View(), "initial desc")

	// Press [d] to open the description-edit form.
	update(t, m, "d")
	if m.form == nil || !m.form.Active {
		t.Fatalf("[d] did not open an active form")
	}
	mustContain(t, m.form.Title, "Edit description")
	// The form pre-fills the existing description.
	if got := m.form.Fields[0].Value; got != "initial desc" {
		t.Errorf("description form prefill = %q, want %q", got, "initial desc")
	}

	// Clear the field and type a new description, then submit (Enter on the
	// last field submits directly per form.Update).
	for range "initial desc" {
		update(t, m, "backspace")
	}
	for _, r := range "edited description persists" {
		update(t, m, string(r))
	}
	update(t, m, "enter")
	if m.form != nil {
		t.Fatalf("form should be closed after submit")
	}

	// Detail view should now render the new description.
	v := m.View()
	mustContain(t, v, "edited description persists")
	// And it should persist in the store.
	stored, err := m.store.GetTask(tk.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if stored.Description != "edited description persists" {
		t.Errorf("stored description = %q, want %q", stored.Description, "edited description persists")
	}
}

// TestTaskDetailRemoveConfirm verifies the [x] key on a task detail opens a
// confirm overlay, confirming removes the task from the store and returns
// the view to the list (mockup Screen 8, [x]).
func TestTaskDetailRemoveConfirm(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme Task Manager")
	tk := seedTask(t, m, "ATM", "Doomed task")
	update(t, m, "s")
	// Task 4: the task has no labels, so it is excluded from the default
	// Open Tasks board; clear focus to see it.
	m.tasks.setFocus(taskFocus{mode: focusOff}, "")
	update(t, m, "2")
	update(t, m, "enter")
	if m.tasks.view != tViewDetail {
		t.Fatalf("expected task detail view, got %v", m.tasks.view)
	}

	// Press [x] — a confirm overlay should appear.
	update(t, m, "x")
	if m.confirm != confirmRemoveTask {
		t.Fatalf("[x] did not open remove confirm (confirm=%v)", m.confirm)
	}
	v := m.View()
	mustContain(t, v, "Remove task")
	mustContain(t, v, "confirm")

	// Confirm with Enter (handleConfirmKey accepts enter/y).
	update(t, m, "enter")
	if m.confirm != confirmNone {
		t.Errorf("confirm should be cleared after yes, got %v", m.confirm)
	}
	// View returns to the list.
	if m.tasks.view != tViewList {
		t.Errorf("expected list view after remove, got %v", m.tasks.view)
	}
	// The detail overlay should no longer render the task.
	mustNotContain(t, m.View(), "Doomed task")
	// And the task is gone from the store.
	if _, err := m.store.GetTask(tk.ID); !store.IsNotFound(err) {
		t.Errorf("GetTask after remove: err=%v, want ErrNotFound", err)
	}
}

func TestPersonaCreateFormFromOverlay(t *testing.T) {
	m := mkActorsOverlayTestModel(t)
	m.SetSize(100, 30)
	m.projectScope = "ATM"
	m.focused = paneProjects
	update(t, m, "P")
	if !m.actorsOverlay {
		t.Fatal("overlay should be open")
	}
	update(t, m, "p")
	if m.form == nil || m.formKind != formPersonaCreate {
		t.Fatalf("persona form not open: form=%v kind=%v", m.form, m.formKind)
	}
	for _, r := range "reviewer" {
		update(t, m, string(r))
	}
	update(t, m, "tab")
	for _, r := range "holds a high bar" {
		update(t, m, string(r))
	}
	update(t, m, "enter")
	if m.form != nil {
		t.Fatalf("form should be closed after submit: %v", m.form)
	}
	p, err := m.store.GetPersona("reviewer")
	if err != nil || p.Description != "holds a high bar" {
		t.Fatalf("persona not created: %+v %v", p, err)
	}
	if !m.actorsOverlay {
		t.Fatal("overlay should still be open after form submit")
	}
}

func TestPersonaCreateFormEscReturnsToOverlay(t *testing.T) {
	m := mkActorsOverlayTestModel(t)
	m.SetSize(100, 30)
	m.projectScope = "ATM"
	m.focused = paneProjects
	update(t, m, "P")
	update(t, m, "p")
	if m.form == nil {
		t.Fatal("form should be open")
	}
	update(t, m, "esc")
	if m.form != nil {
		t.Fatal("form should be closed on Esc")
	}
	if !m.actorsOverlay {
		t.Fatal("overlay should still be open after form Esc (form was on top)")
	}
}

func TestStatusBarHasNoActorSegment(t *testing.T) {
	m := newTestModelWithActor(t, testActor)
	m.SetSize(100, 30)
	view := m.View()
	if strings.Contains(view, "actor:") {
		t.Errorf("status bar still renders actor: segment\n--- view ---\n%s", view)
	}
}

func TestStatusBarPluginDockEmptyWhenNoPlugins(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(100, 30)
	m.plugins = nil
	line := m.renderStatusLine()
	if strings.Contains(line, "idx:") || strings.Contains(line, "IDX:") {
		t.Errorf("empty dock should render no plugin segment, got %q", line)
	}
}

func TestGPrefixSetsFlagAndG1OpensOverlay(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(100, 30)
	seedProject(t, m, "TST", "Test")
	m.projectScope = "TST"
	m.plugins = []plugin{&fakePlugin{}}
	update(t, m, "g")
	if !m.pluginPrefixActive {
		t.Fatal("g should set pluginPrefixActive")
	}
	update(t, m, "1")
	if m.pluginOverlay != 0 {
		t.Fatalf("g 1 should open plugin overlay 0, got %d", m.pluginOverlay)
	}
	if m.pluginPrefixActive {
		t.Fatal("prefix flag should clear after opening")
	}
}

func TestGPrefixNonMatchingKeyClearsFlag(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(100, 30)
	m.plugins = []plugin{&fakePlugin{}}
	update(t, m, "g")
	if !m.pluginPrefixActive {
		t.Fatal("g should set pluginPrefixActive")
	}
	update(t, m, "x")
	if m.pluginPrefixActive {
		t.Fatal("non-matching key should clear prefix flag")
	}
	if m.pluginOverlay != -1 {
		t.Fatalf("non-matching key should not open overlay, got %d", m.pluginOverlay)
	}
}

func TestEscClosesPluginOverlay(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(100, 30)
	m.plugins = []plugin{&fakePlugin{}}
	m.pluginOverlay = 0
	update(t, m, "esc")
	if m.pluginOverlay != -1 {
		t.Fatalf("Esc should close plugin overlay, got %d", m.pluginOverlay)
	}
}

func TestKeymapHasPluginPrefixRows(t *testing.T) {
	foundG := false
	foundG1 := false
	for _, r := range keymapRows {
		if r.Key == "g" {
			foundG = true
		}
		if r.Key == "g 1" {
			foundG1 = true
		}
	}
	if !foundG {
		t.Error("keymapRows missing 'g' (plugin prefix)")
	}
	if !foundG1 {
		t.Error("keymapRows missing 'g 1' (indexer overlay)")
	}
}

func TestHelpMentionsPluginOverlays(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	m.openHelp(helpKeys)
	content := strings.Join(m.help.lines, "\n")
	if !strings.Contains(content, "g ") {
		t.Errorf("keys help should mention 'g <n>' plugin overlays\n--- content ---\n%s", content)
	}
}

// TestSwitchProjectClearsTasksAndLabelsState (ATM-0082) verifies that pressing
// [s] on a project row resets the Tasks pane's view + filter + cursor and the
// Labels pane's drill level, so no stale detail/filter from the previously
// selected project survives into the newly-selected one. The previous handler
// cleared tasks.filter and called labels.reset() but left tasks.view stuck in
// tViewDetail when the user had opened a task detail before switching, and it
// bypassed tasks.setFocus so cursor/offset were not reset either.
func TestSwitchProjectClearsTasksAndLabelsState(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	seedProject(t, m, "ATM", "Acme Task Manager")
	seedProject(t, m, "SCY", "Scylla")
	seedLabel(t, m, "ATM:status:open", "open")
	seedLabel(t, m, "ATM:status:done", "done")
	tk := seedTask(t, m, "ATM", "ATM task one", "ATM:status:open")
	seedTask(t, m, "ATM", "ATM task two", "ATM:status:done")

	// Select ATM, drill into a label detail (which sets a tasks filter), and
	// open the task detail view so both panes carry non-trivial state.
	update(t, m, "s")
	if m.projectScope != "ATM" {
		t.Fatalf("projectScope = %q want ATM", m.projectScope)
	}
	// paneLabels no longer exists as a focusable pane (Task 3 removed the
	// [3] Boards pane); drive boardsModel directly since Task 7 has not yet
	// re-wired its key handling into the Tasks pane.
	//
	// Locate the "status" namespace row explicitly rather than assuming
	// cursor 0 lands on it: the workflow capability added "backlog" and
	// "in-progress-tasks" as normal L0 board members (sorted by display
	// name, buildBoardRows), and "backlog" now sorts alphabetically before
	// "status" (and before the template's other emergent namespaces), so
	// cursor 0 lands on a non-expandable board row instead of a namespace.
	// This test doesn't care which namespace it drills into -- it only
	// needs non-trivial chart/detail state to exercise the project-switch
	// reset -- so pick "status" (seeded above) by name instead of by
	// position.
	statusIdx := -1
	for i, r := range m.boards.rows {
		if r.Name == "status" {
			statusIdx = i
			break
		}
	}
	if statusIdx < 0 {
		t.Fatal("status namespace row not found in boards ring")
	}
	m.boards.cursor = statusIdx
	m.boards.handleKey(keyMsg("enter")) // enter ATM namespace chart
	if m.boards.level != lLevelChart {
		t.Fatalf("boards.level = %v want lLevelChart", m.boards.level)
	}
	// Pick the first chart row (a concrete label) and drill into detail.
	m.boards.handleKey(keyMsg("enter"))
	if m.boards.level != lLevelDetail {
		t.Fatalf("boards.level = %v want lLevelDetail", m.boards.level)
	}
	if m.tasks.filter == "" {
		t.Fatal("tasks.filter should be set after label detail drill")
	}
	// Open the task detail in the Tasks pane.
	m.focused = paneTasks
	m.tasks.openDetail(tk.ID)
	if m.tasks.view != tViewDetail {
		t.Fatalf("tasks.view = %v want tViewDetail", m.tasks.view)
	}
	// Cursors non-zero so we can detect they were reset.
	m.tasks.cursor = 5
	m.tasks.offset = 3

	// Switch to SCY via [s] from the Projects pane.
	m.focused = paneProjects
	m.projects.cursor = 1 // SCY row
	update(t, m, "s")

	if m.projectScope != "SCY" {
		t.Fatalf("projectScope = %q want SCY", m.projectScope)
	}
	// Tasks pane must return to the list view, with no leftover filter/focus.
	if m.tasks.view != tViewList {
		t.Errorf("tasks.view = %v want tViewList (detail leaked across project switch)", m.tasks.view)
	}
	if m.tasks.detail.id != "" {
		t.Errorf("tasks.detail.id = %q want empty (stale detail survived switch)", m.tasks.detail.id)
	}
	// Project-select now defaults the Tasks pane to the new project's All
	// Tasks board (not an empty filter), so the invariant under test is that
	// the OLD project's (ATM) filter does not survive — not that the filter
	// is literally empty.
	wantFilter := workflow.BoardAllTasks("SCY")
	if m.tasks.filter != wantFilter {
		t.Errorf("tasks.filter = %q want %q (new project's default board, not the stale ATM filter)", m.tasks.filter, wantFilter)
	}
	if m.tasks.focus.mode != focusOff {
		t.Errorf("tasks.focus.mode = %v want focusOff", m.tasks.focus.mode)
	}
	if m.tasks.cursor != 0 {
		t.Errorf("tasks.cursor = %d want 0", m.tasks.cursor)
	}
	if m.tasks.offset != 0 {
		t.Errorf("tasks.offset = %d want 0", m.tasks.offset)
	}
	// Boards pane must return to L0 with no namespace selected.
	if m.boards.level != lLevelTable {
		t.Errorf("boards.level = %v want lLevelTable", m.boards.level)
	}
	if m.boards.ns != "" {
		t.Errorf("boards.ns = %q want empty", m.boards.ns)
	}
	// The Tasks pane body must not reference the old project's task.
	body := m.tasks.View()
	if strings.Contains(body, tk.ID) {
		t.Errorf("Tasks view still references old project task %q after switch\n--- body ---\n%s", tk.ID, body)
	}
	if strings.Contains(body, "ATM") {
		t.Errorf("Tasks view still references old project ATM after switch\n--- body ---\n%s", body)
	}
}
