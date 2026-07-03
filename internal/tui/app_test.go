package tui

import (
	"strings"
	"testing"

	"atm/internal/store"
	tea "github.com/charmbracelet/bubbletea"
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

// --- Step 1: tab switching ---

// TestTabSwitching verifies 1/2/3/4 switches the focused pane (mockup Screen
// shared chrome: four tabs Projects/Tasks/Labels/Help).
func TestTabSwitching(t *testing.T) {
	m := newTestModel(t)
	if m.focused != paneProjects {
		t.Fatalf("default focus = %v want paneProjects", m.focused)
	}
	m = update(t, m, "2")
	if m.focused != paneTasks {
		t.Fatalf("after 2: focus = %v want paneTasks", m.focused)
	}
	m = update(t, m, "3")
	if m.focused != paneLabels {
		t.Fatalf("after 3: focus = %v want paneLabels", m.focused)
	}
	m = update(t, m, "4")
	if m.focused != paneHelp {
		t.Fatalf("after 4: focus = %v want paneHelp", m.focused)
	}
	m = update(t, m, "1")
	if m.focused != paneProjects {
		t.Fatalf("after 1: focus = %v want paneProjects", m.focused)
	}
}

// TestTabBarShowsNumbers verifies the tab bar renders numeric prefixes (1/2/3/4)
// so the [1]/[2]/[3]/[4] switching keys are discoverable.
func TestTabBarShowsNumbers(t *testing.T) {
	m := newTestModel(t)
	bar := m.renderTabBar()
	for _, want := range []string{"1", "2", "3", "4", "Projects", "Tasks", "Labels", "Help"} {
		if !strings.Contains(bar, want) {
			t.Errorf("tab bar missing %q\nbar: %s", want, bar)
		}
	}
}

func TestDefaultTheme(t *testing.T) {
	m := newTestModel(t)
	if m.themeName != themeATMDark {
		t.Fatalf("themeName = %q want %q", m.themeName, themeATMDark)
	}
	if string(m.themeName) != "atm-dark" {
		t.Fatalf("themeName string = %q want atm-dark", m.themeName)
	}
}

func TestNextThemeNameWraps(t *testing.T) {
	order := []ThemeName{themeATMDark, themeGraphite, themeLight, themeMono, themeATMDark}
	for i := 0; i < len(order)-1; i++ {
		if got := nextThemeName(order[i]); got != order[i+1] {
			t.Fatalf("nextThemeName(%q) = %q want %q", order[i], got, order[i+1])
		}
	}
	if got := nextThemeName(ThemeName("unknown")); got != themeATMDark {
		t.Fatalf("nextThemeName(unknown) = %q want %q", got, themeATMDark)
	}
}

func TestThemeCycleKeyUpdatesThemeAndStatus(t *testing.T) {
	m := newTestModel(t)
	mustContain(t, m.renderStatusLine(), "theme: atm-dark")
	update(t, m, "T")
	if m.themeName != themeGraphite {
		t.Fatalf("after T: themeName = %q want %q", m.themeName, themeGraphite)
	}
	mustContain(t, m.renderStatusLine(), "theme: graphite")
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
	if m.themeName != themeATMDark {
		t.Fatalf("themeName changed in form input: %q", m.themeName)
	}
	if got := m.form.Fields[0].Value; got != "T" {
		t.Fatalf("form field value = %q want T", got)
	}
}

func TestThemeCyclesInsideKeymapOverlay(t *testing.T) {
	m := newTestModel(t)
	update(t, m, "?")
	if !m.keymapOverlayOn {
		t.Fatalf("setup: keymap overlay should be open")
	}
	update(t, m, "T")
	if m.themeName != themeGraphite {
		t.Fatalf("themeName = %q want %q", m.themeName, themeGraphite)
	}
	if !m.keymapOverlayOn {
		t.Fatalf("theme cycling should not close keymap overlay")
	}
}

func TestThemeChangesActiveTabStyle(t *testing.T) {
	m := newTestModel(t)
	before := m.styles.ActiveTab.GetBackground()
	update(t, m, "T")
	after := m.styles.ActiveTab.GetBackground()
	if before == after {
		t.Fatalf("active tab background did not change after theme cycle")
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

func TestOverlayPreservesUnderlyingScreen(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme Task Manager")
	base := m.View()
	mustContain(t, base, "Acme Task Manager")
	update(t, m, "a")
	withOverlay := m.View()
	mustContain(t, withOverlay, "New project")
	mustContain(t, withOverlay, "Acme Task Manager")
}

// --- Step 3: projects list + detail ---

// TestProjectsListEmpty verifies the empty-store landing (mockup Screen 1).
func TestProjectsListEmpty(t *testing.T) {
	m := newTestModel(t)
	v := m.View()
	mustContain(t, v, "no projects")
	mustContain(t, v, "press [a] to add a project")
}

// TestProjectsListPopulated verifies columns CODE/NAME/TASKS/LABELS/UPDATED
// and the selection gutter marker (mockup Screen 3).
func TestProjectsListPopulated(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme Task Manager")
	seedProject(t, m, "SCY", "Scylla")
	v := m.View()
	for _, col := range []string{"CODE", "NAME", "TASKS", "LABELS", "UPDATED"} {
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
	mustContain(t, m.View(), "▸")
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
// renders a LABELS section (label management moved to the Labels tab).
func TestProjectDetailNoLabelsSection(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme Task Manager")
	seedLabel(t, m, "ATM:custom:x", "custom")
	update(t, m, "enter") // open ATM detail (cursor on ATM)
	v := m.View()
	mustContain(t, v, "PROJECT")
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

// --- Step 4: label add/remove forms (moved to Labels tab in Task 8) ---

// --- Step 5: tasks flat + grouped ---

// TestTasksFlatListEmptyFilter verifies the persistent header line for the
// flat list with no filter (mockup Screen 6).
func TestTasksFlatListEmptyFilter(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme Task Manager")
	seedTask(t, m, "ATM", "task one", "ATM:status:open")
	update(t, m, "s") // select ATM
	update(t, m, "2") // switch to Tasks tab
	v := m.View()
	mustContain(t, v, "PROJECT: ATM")
	mustContain(t, v, "FILTER: (none)")
	mustContain(t, v, "SORT: updated-desc")
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
	v := m.View()
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
// facts, label chips, and always-visible HISTORY chronological.
func TestTaskDetailFactsLabelsHistory(t *testing.T) {
	m := newTestModel(t)
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
	v := m.View()
	mustContain(t, v, "TASK")
	mustContain(t, v, "id           ATM-0001")
	mustContain(t, v, "project      ATM")
	mustContain(t, v, "title        Fix label reconciliation")
	mustContain(t, v, "LABELS")
	mustContain(t, v, "ATM:status:in-progress")
	mustContain(t, v, "ATM:type:bug")
	mustContain(t, v, "ATM:priority:high")
	mustContain(t, v, "HISTORY")
	mustContain(t, v, "created")
	mustContain(t, v, "label-added")
	// Chronological: "created" (h1) before "label-added" (h2).
	createdIdx := strings.Index(v, "created")
	addedIdx := strings.Index(v, "label-added")
	if createdIdx < 0 || addedIdx < 0 {
		t.Fatalf("missing created/label-added in history")
	}
	if addedIdx < createdIdx {
		t.Errorf("history not chronological: label-added (%d) before created (%d)", addedIdx, createdIdx)
	}
}

func TestTaskDetailLabelsRenderAsChips(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme Task Manager")
	seedTask(t, m, "ATM", "chip task", "ATM:status:open", "ATM:type:bug")
	update(t, m, "s")
	update(t, m, "2")
	update(t, m, "enter")
	v := m.View()
	mustContain(t, v, " ATM:status:open ")
	mustContain(t, v, " ATM:type:bug ")
}

// TestTasksEmptyStateNoProject verifies the no-project-selected prompt
// (mockup Screen 9, state 1).
func TestTasksEmptyStateNoProject(t *testing.T) {
	m := newTestModel(t)
	// No project selected (projectScope empty).
	update(t, m, "2")
	v := m.View()
	mustContain(t, v, "PROJECT: (none)")
	mustContain(t, v, "no project selected")
	mustContain(t, v, "press [s] in the Projects tab")
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
	v := m.View()
	mustContain(t, v, "no tasks match this filter")
	mustContain(t, v, "ATM:status:done")
	mustContain(t, v, "[/] to edit filter")
}

// TestTasksEmptyStateWildcardNoLabels verifies the wildcard-yields-no-labels
// empty state (mockup Screen 9, state 3) and that the (no matching labels)
// bucket renders with all in-scope tasks.
func TestTasksEmptyStateWildcardNoLabels(t *testing.T) {
	m := newTestModel(t)
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
	v := m.View()
	mustContain(t, v, "no labels match wildcard — add labels to tasks")
	mustContain(t, v, "▾ (no matching labels)")
	mustContain(t, v, "task one")
	mustContain(t, v, "task two")
}

// --- Step 7: help tab ---

// TestHelpTabParityTable verifies the CLI/TUI parity table is present in the
// Help tab (mockup Screen 10, Section 1).
func TestHelpTabParityTable(t *testing.T) {
	m := newTestModel(t)
	update(t, m, "4")
	v := m.View()
	mustContain(t, v, "CLI / TUI parity")
	mustContain(t, v, "atm project create")
	mustContain(t, v, "atm task create")
	mustContain(t, v, "atm conventions")
}

// TestHelpTabConventions verifies the conventions (advisory) section is
// present (mockup Screen 10, Section 3). It lives below the parity table and
// keymap; the test checks the help model's rendered lines (the full content)
// rather than the scrolled viewport, since Section 3 is far down.
func TestHelpTabConventions(t *testing.T) {
	m := newTestModel(t)
	update(t, m, "4")
	content := strings.Join(m.help.lines, "\n")
	mustContain(t, content, "Conventions")
	mustContain(t, content, "advisory")
	mustContain(t, content, "Suggested seed namespaces")
}

// TestHelpTabReadOnly verifies no mutating keys route to the store from the
// Help tab. We press several mutating keys (a, x, L, l, N) and confirm the
// store state is unchanged (no projects created/removed).
func TestHelpTabReadOnly(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme Task Manager")
	update(t, m, "4")
	for _, k := range []string{"a", "x", "L", "l", "N", "H", "s", "S", "d"} {
		update(t, m, k)
	}
	// No form should have opened; no confirm; no toast.
	if m.form != nil {
		t.Errorf("mutating key opened a form from Help tab")
	}
	if m.confirm != confirmNone {
		t.Errorf("mutating key opened a confirm from Help tab")
	}
	if m.toastMsg != "" {
		t.Errorf("mutating key produced toast %q from Help tab", m.toastMsg)
	}
	// Store unchanged: ATM still present, no extra projects.
	ps := m.store.ListProjects()
	if len(ps) != 1 || ps[0].Code != "ATM" {
		t.Errorf("store changed from Help tab: projects = %+v", ps)
	}
}

// TestHelpTabKeymap verifies the Help tab's Section 2 (Global keymap) is
// rendered with its heading and the keymap summary table content (mockup
// Screen 10, Section 2). Covers the third spec target "global keymap" that
// TestHelpTabParityTable (§1) and TestHelpTabConventions (§3) do not assert.
func TestHelpTabKeymap(t *testing.T) {
	m := newTestModel(t)
	update(t, m, "4")
	content := strings.Join(m.help.lines, "\n")
	mustContain(t, content, "Global keymap")
	// A stable binding row from the keymap table — [a] add project/task.
	mustContain(t, content, "[a]")
	// The table header should be present too.
	mustContain(t, content, "Key")
	mustContain(t, content, "Detail")
	mustContain(t, content, "T")
	mustContain(t, content, "cycle theme")
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
