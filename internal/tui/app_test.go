package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"atm/internal/store"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// --- test helpers ---

// newTestModel builds a Model against a fresh temp-dir store. The store is
// opened and auto-initialized; the model's actor is set to "claude" so mutating
// keys are active in the tests.
func newTestModel(t *testing.T) *Model {
	t.Helper()
	return newTestModelWithActor(t, "claude")
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
	m, err := NewModel(NewModelOpts{StorePath: s.StorePath(), Actor: actor})
	if err != nil {
		t.Fatalf("NewModel: %v", err)
	}
	return m
}

// keyMsg constructs a tea.KeyMsg from a key string. Rune keys use KeyRunes;
// the special-key strings bubbletea expects (enter, esc, backspace, space,
// down, up, pgdown, pgup, tab) map to their KeyType. The returned KeyMsg's
// String() matches what the TUI handlers switch on.
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
	if _, err := m.store.CreateProject(code, name, "claude"); err != nil {
		t.Fatalf("CreateProject %s: %v", code, err)
	}
	m.refreshAll()
}

// seedTask creates a task under the given project with the given labels.
func seedTask(t *testing.T, m *Model, projectCode, title string, labels ...string) *store.Task {
	t.Helper()
	tk, err := m.store.CreateTask(projectCode, title, "", labels, "claude")
	if err != nil {
		t.Fatalf("CreateTask %s: %v", title, err)
	}
	m.refreshAll()
	return tk
}

// seedLabel adds a label to the registry (with optional description).
func seedLabel(t *testing.T, m *Model, name, desc string) {
	t.Helper()
	if err := m.store.LabelAdd(name, desc, "claude"); err != nil {
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

func TestPaneModelsRenderWithinAssignedPaneWidth(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 36)
	seedProject(t, m, "ATM", "Acme Task Manager")
	update(t, m, "s")

	leftW, rightW := splitWorkspaceWidths(m.width)
	tasksH, labelsH := splitRightColumnHeights(m.contentHeight)
	if got, want := lipgloss.Width(strings.Split(m.projects.View(), "\n")[0]), innerPaneWidth(leftW); got != want {
		t.Fatalf("projects divider width = %d want pane inner width %d", got, want)
	}
	if got, want := lipgloss.Width(strings.Split(m.tasks.View(), "\n")[0]), innerPaneWidth(rightW); got != want {
		t.Fatalf("tasks divider width = %d want pane inner width %d", got, want)
	}
	if got, want := lipgloss.Width(strings.Split(m.labels.View(), "\n")[0]), innerPaneWidth(rightW); got != want {
		t.Fatalf("labels divider width = %d want pane inner width %d", got, want)
	}
	wantPageSize := (innerPaneHeight(tasksH) - 6) / 2
	if wantPageSize < 1 {
		wantPageSize = 1
	}
	if got, want := m.tasks.pageSize, wantPageSize; got != want {
		t.Fatalf("tasks pageSize = %d want %d", got, want)
	}
	_ = labelsH
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
	update(t, m, "3")
	if m.focused != paneLabels {
		t.Fatalf("after 3: focus = %v want paneLabels", m.focused)
	}
	update(t, m, "4")
	if m.focused != paneLabels {
		t.Fatalf("4 should not focus Help; focus = %v want paneLabels", m.focused)
	}
	update(t, m, "1")
	if m.focused != paneProjects {
		t.Fatalf("after 1: focus = %v want paneProjects", m.focused)
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
	mustContain(t, v, "[3] Labels")
	mustNotContain(t, v, "1  Projects")
	mustNotContain(t, v, "4  Help")
}

func TestPaneFocusKeepsAllPanesVisible(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 36)
	for _, key := range []string{"1", "2", "3"} {
		update(t, m, key)
		v := m.View()
		mustContain(t, v, "[1] Projects")
		mustContain(t, v, "[2] Tasks")
		mustContain(t, v, "[3] Labels")
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
	update(t, m, "3")
	labels := m.renderStatusLine()
	if projects == tasks || tasks == labels || projects == labels {
		t.Fatalf("status hints should differ by focused pane:\nprojects=%q\ntasks=%q\nlabels=%q", projects, tasks, labels)
	}
	mustContain(t, tasks, "[/]filter")
	mustContain(t, labels, "[a]dd")
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
	update(t, m, "/")
	for _, r := range "ATM:status:open" {
		update(t, m, string(r))
	}
	update(t, m, "enter")
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
// without --actor defaults the actor to "default", so [a] opens the create
// form with no actor field (the form only collects code + name).
func TestProjectCreateFormNoActor(t *testing.T) {
	m := newTestModelWithActor(t, "")
	if m.actor != "default" {
		t.Fatalf("actor = %q want %q", m.actor, "default")
	}
	if !m.canMutate() {
		t.Fatalf("canMutate = false want true (actor defaults to default)")
	}
	update(t, m, "a")
	if m.form == nil || !m.form.Active {
		t.Fatalf("pressing [a] did not open the create form")
	}
	for _, f := range m.form.Fields {
		if f.Label == "actor" {
			t.Errorf("create form should not collect an actor (actor defaults to default); got actor field")
		}
	}
}

// TestOverlayRendersDimmedBackdropWithModal verifies the create-project form
// renders as a centered modal over a dimmed `░` backdrop: the modal content is
// present, the backdrop shade is present, and the underlying workspace text
// is replaced by the dim shade (not visible through the modal).
func TestOverlayRendersDimmedBackdropWithModal(t *testing.T) {
	m := newTestModel(t)
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
	mustContain(t, body, "─ Overview ─")
	lines := strings.Split(body, "\n")
	if len(lines) < 2 {
		t.Fatalf("projects body too short\n--- body ---\n%s", body)
	}
	if leadingSpaces(lines[1]) != leadingSpaces(lines[0]) {
		t.Fatalf("summary should align with divider: divider=%q summary=%q", lines[0], lines[1])
	}
	mustContain(t, body, "total projects: 2")
	mustContain(t, body, "selected: none")
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
	mustContain(t, body, "─ Overview ─")
	mustContain(t, body, "─ Project Summary ─")
	mustContain(t, body, "select a project to see summaries")
	overviewIdx := strings.Index(body, "─ Overview ─")
	summaryIdx := strings.Index(body, "─ Project Summary ─")
	if overviewIdx < 0 || summaryIdx < 0 || summaryIdx <= overviewIdx {
		t.Fatalf("summary should render below overview\n--- body ---\n%s", body)
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
	mustContain(t, body, "more projects")
	lines := strings.Split(body, "\n")
	if len(lines) > 40 {
		t.Fatalf("projects view has %d lines, want <= 40\n--- body ---\n%s", len(lines), body)
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
	mustContain(t, v, "─ Facts ─")
	mustContain(t, v, "code")
	mustContain(t, v, "tasks")
	mustContain(t, v, "─ Actions ─")
	mustContain(t, v, "[N] set name")
	mustContain(t, v, "[H] history")
	mustContain(t, v, "[x] remove")
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

func TestActorActivityRowsSortAndPercent(t *testing.T) {
	entries := []store.LogEntry{
		{Actor: "codex"},
		{Actor: "claude"},
		{Actor: "codex"},
		{Actor: "ttran"},
		{Actor: "codex"},
		{Actor: "claude"},
	}
	got := actorActivityRows(entries, 10)
	want := []actorActivityRow{
		{actor: "codex", count: 3, percent: 50},
		{actor: "claude", count: 2, percent: 33},
		{actor: "ttran", count: 1, percent: 17},
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("actorActivityRows() = %#v, want %#v", got, want)
	}
}

func TestActorActivityRowsFoldsOthersAtLimit(t *testing.T) {
	entries := []store.LogEntry{
		{Actor: "a"}, {Actor: "a"}, {Actor: "a"}, {Actor: "a"}, {Actor: "a"},
		{Actor: "b"}, {Actor: "b"}, {Actor: "b"}, {Actor: "b"},
		{Actor: "c"}, {Actor: "c"}, {Actor: "c"},
		{Actor: "d"}, {Actor: "d"},
		{Actor: "e"},
	}
	got := actorActivityRows(entries, 4)
	want := []actorActivityRow{
		{actor: "a", count: 5, percent: 33},
		{actor: "b", count: 4, percent: 27},
		{actor: "c", count: 3, percent: 20},
		{actor: "others", count: 3, percent: 20},
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("actorActivityRows() = %#v, want %#v", got, want)
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
	mustContain(t, body, "activity by actor")
	mustContain(t, body, "claude")
	mustContain(t, body, "%")
	mustContain(t, body, "activity stripe")
	mustContain(t, body, "bubbles")
	mustContain(t, body, "events")
	mustContain(t, body, "agents")
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
	mustContain(t, body, "activity by actor")
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
	mustContain(t, body, "activity by actor")
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
	mustNotContain(t, body, "activity by actor")
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
	mustContain(t, body, "Overview")
	mustContain(t, body, "Project Summary")
	_ = m.View()
}

func TestKeywordSummaryDoesNotOpenFormOrConfirm(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	seedProject(t, m, "ATM", "Acme Task Manager")
	update(t, m, "s")
	body := m.projects.View()
	mustContain(t, body, "bubbles")
	mustContain(t, body, "events")
	mustContain(t, body, "agents")
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
		{Actor: "a"}, {Actor: "a"}, {Actor: "a"}, {Actor: "a"}, {Actor: "a"},
		{Actor: "b"}, {Actor: "b"}, {Actor: "b"}, {Actor: "b"},
		{Actor: "c"}, {Actor: "c"}, {Actor: "c"},
		{Actor: "d"}, {Actor: "d"},
		{Actor: "e"},
	}
	lines := p.renderActorActivityChart(entries, 5)
	got := strings.Join(lines, "\n")
	mustContain(t, got, "activity by actor")
	mustContain(t, got, "a")
	mustContain(t, got, "b")
	mustContain(t, got, "c")
	mustContain(t, got, "others")
}

func TestRenderActorActivityChartUsesMeterStyle(t *testing.T) {
	m := newTestModel(t)
	p := newProjectsModel(m)
	p.SetSize(80, 20)
	entries := []store.LogEntry{
		{Actor: "claude"}, {Actor: "claude"},
		{Actor: "codex"},
	}
	got := strings.Join(p.renderActorActivityChart(entries, 4), "\n")
	mustContain(t, got, "activity by actor")
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
		{Actor: "very-long-agent-name-with-role"},
	}
	got := strings.Join(p.renderActorActivityChart(entries, 4), "\n")
	mustContain(t, got, "very-long-agent-name-with-role")
	mustNotContain(t, got, "very-lo...")
}

func TestProjectSummaryChartBoxesAreCentered(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(100, 40)
	seedProject(t, m, "ATM", "Acme Task Manager")
	update(t, m, "s")
	body := m.projects.View()
	lines := strings.Split(body, "\n")
	for _, line := range lines {
		if strings.Contains(line, "activity by actor") && strings.Contains(line, "╭") {
			if strings.HasPrefix(line, "╭") {
				t.Fatalf("chart box should be centered with left padding, got %q\n--- body ---\n%s", line, body)
			}
			return
		}
	}
	t.Fatalf("missing centered activity by actor box\n--- body ---\n%s", body)
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
	if len(strings.Split(strings.TrimRight(got, "\n"), "\n")) < 2 {
		t.Fatalf("renderActivityStripeCanvas() should render a multi-line canvas, got %q", got)
	}
	mustContain(t, got, "█")
	mustContain(t, got, "▅")
	mustContain(t, got, "7d ago")
	mustContain(t, got, "Yesterday")
	mustContain(t, got, "Today")
	if activityCanvasStyle(10).GetForeground() == nil {
		t.Fatalf("activityCanvasStyle should configure foreground color")
	}
	barLine := strings.Split(got, "\n")[0]
	if got := strings.Count(barLine, " "); got != 6 {
		t.Fatalf("activity stripe should separate exactly 7 bars with 6 spaces, got %d spaces in %q", got, barLine)
	}
}

func TestRenderSampleBubbleCanvasShowsPlaceholders(t *testing.T) {
	got := renderSampleBubbleCanvas(28)
	mustContain(t, got, "events")
	mustContain(t, got, "agents")
	mustContain(t, got, "tasks")
	mustNotContain(t, got, "pending")
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
	mustContain(t, v, "History")
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
	seedTask(t, m, "ATM", "task one", "ATM:status:open")
	update(t, m, "s") // select ATM
	update(t, m, "2") // focus Tasks pane
	v := m.View()
	body := m.tasks.View()
	if strings.HasPrefix(body, "Tasks\n") {
		t.Fatalf("tasks body repeats tab title\n--- body ---\n%s", body)
	}
	mustContain(t, body, "─ Overview ─")
	mustContain(t, v, "PROJECT: ATM")
	mustContain(t, v, "FILTER: (none)")
	mustContain(t, v, "SORT: updated-desc")
	mustContain(t, v, "task one")
	mustContain(t, v, "id ATM-0001")
	mustContain(t, v, "labels ATM:status:open")
}

// TestTasksFilterInlineEditing verifies / enters edit mode, typing and Enter
// applies the filter and the list restricts.
func TestTasksFilterInlineEditing(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme Task Manager")
	seedTask(t, m, "ATM", "open one", "ATM:status:open")
	seedTask(t, m, "ATM", "done one", "ATM:status:done")
	update(t, m, "s")
	update(t, m, "2")
	update(t, m, "/")
	if !m.tasks.filterEditing {
		t.Fatalf("after /: filterEditing = false want true")
	}
	for _, r := range "ATM:status:open" {
		update(t, m, string(r))
	}
	update(t, m, "enter")
	if m.tasks.filterEditing {
		t.Errorf("after enter: filterEditing = true want false")
	}
	if m.tasks.filter != "ATM:status:open" {
		t.Errorf("filter = %q want ATM:status:open", m.tasks.filter)
	}
	v := m.View()
	mustContain(t, v, "open one")
	mustNotContain(t, v, "done one")
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
	update(t, m, "/")
	for _, r := range "ATM:status:open ATM:type:bug" {
		update(t, m, string(r))
	}
	update(t, m, "enter")
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
	update(t, m, "2")
	v := m.View()
	mustContain(t, v, "showing ")
	mustContain(t, v, "of 25")
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
	update(t, m, "/")
	for _, r := range "ATM:status:*" {
		update(t, m, string(r))
	}
	update(t, m, "enter")
	v := m.View()
	mustContain(t, v, "Groups")
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
	update(t, m, "/")
	for _, r := range "ATM:status:* ATM:type:*" {
		update(t, m, string(r))
	}
	update(t, m, "enter")
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
	update(t, m, "/")
	for _, r := range "ATM:status:*" {
		update(t, m, string(r))
	}
	update(t, m, "enter")
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
// facts, label chips, and HISTORY behind the [H] toggle (opened in-test).
func TestTaskDetailFactsLabelsHistory(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(160, 80)
	seedProject(t, m, "ATM", "Acme Task Manager")
	tk := seedTask(t, m, "ATM", "Fix label reconciliation", "ATM:status:in-progress", "ATM:type:bug")
	// Add a label after creation to get a second history entry.
	if err := m.store.TaskLabelAdd(tk.ID, "ATM:priority:high", "claude"); err != nil {
		t.Fatalf("TaskLabelAdd: %v", err)
	}
	m.refreshAll()
	update(t, m, "s")
	update(t, m, "2")
	// Cursor on the task (row 0); open detail.
	update(t, m, "enter")
	// History is hidden behind [H] by default; open it.
	update(t, m, "H")
	v := m.tasks.View()
	mustContain(t, v, "Task ATM-0001")
	mustContain(t, v, "─ Facts ─")
	mustContain(t, v, "id      ATM-0001")
	mustContain(t, v, "project ATM")
	mustContain(t, v, "title   Fix label reconciliation")
	mustContain(t, v, "─ Labels ─")
	mustContain(t, v, "ATM:status:in-progress")
	mustContain(t, v, "ATM:type:bug")
	mustContain(t, v, "ATM:priority:high")
	mustContain(t, v, "─ History ─")
	mustContain(t, v, "─ Actions ─")
	mustContain(t, v, "[e] edit title")
	mustContain(t, v, "[b] add label")
	mustContain(t, v, "task.created")
	mustContain(t, v, "task.label-added")
	// History rows are decorated with [seq] and ordered chronologically
	// (the log is append-only); task.created is logged before task.label-added.
	createdIdx := strings.Index(v, "task.created")
	addedIdx := strings.Index(v, "task.label-added")
	if createdIdx < 0 || addedIdx < 0 {
		t.Fatalf("missing task.created / task.label-added in history")
	}
	if addedIdx < createdIdx {
		t.Errorf("history not chronological: task.label-added (%d) before task.created (%d)", addedIdx, createdIdx)
	}
	// The [seq] decoration must precede each row.
	if !strings.Contains(v, "] ") {
		t.Errorf("history rows missing [seq] decoration")
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
	seedTask(t, m, "ATM", "inside pane task", "ATM:status:open")
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
	mustContain(t, v, "[3] Labels")
	mustContain(t, v, "Task ATM-0001")
}

func TestEscBacksOnlyFocusedPaneOutOfDetail(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme Task Manager")
	seedTask(t, m, "ATM", "pane task")
	update(t, m, "s")
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

func TestTaskFilterEditingKeepsPriorityOverFocusKeys(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme Task Manager")
	update(t, m, "s")
	update(t, m, "2")
	update(t, m, "/")
	update(t, m, "1")
	if m.focused != paneTasks {
		t.Fatalf("typing 1 in task filter should not focus Projects")
	}
	if m.tasks.filterEdit != "1" {
		t.Fatalf("filterEdit = %q want 1", m.tasks.filterEdit)
	}
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

// TestTasksEmptyStateFilterNoMatch verifies the filter-no-match empty state
// (mockup Screen 9, state 2) echoes the offending filter in plain English.
func TestTasksEmptyStateFilterNoMatch(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme Task Manager")
	seedTask(t, m, "ATM", "open one", "ATM:status:open")
	update(t, m, "s")
	update(t, m, "2")
	update(t, m, "/")
	for _, r := range "ATM:status:done" {
		update(t, m, string(r))
	}
	update(t, m, "enter")
	v := m.tasks.View()
	mustContain(t, v, "no tasks match this filter")
	mustContain(t, v, "ATM:status:done")
	mustContain(t, v, "[/] to edit filter")
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
	update(t, m, "/")
	for _, r := range "ATM:context:*" {
		update(t, m, string(r))
	}
	update(t, m, "enter")
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
	tk, err := m.store.CreateTask("ATM", "Wire [d] description edit", "initial desc", nil, "claude")
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	m.refreshAll()
	// Select project, switch to Tasks, open detail at cursor row 0.
	update(t, m, "s")
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
