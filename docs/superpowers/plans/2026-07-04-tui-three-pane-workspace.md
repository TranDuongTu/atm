# TUI Three-Pane Workspace Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the tabbed ATM TUI with a persistent three-pane workspace: Projects on the left, Tasks and Labels stacked on the right, with Help as a single overlay.

**Architecture:** Keep the existing `projectsModel`, `tasksModel`, `labelsModel`, and `helpModel` as pane-local state owners. Move layout ownership into the root `Model`: it calculates pane sizes, renders bordered panes, routes `1/2/3` as focus changes, and layers forms/confirms/help/toasts over the workspace. Entity details remain inside their pane; overlays are only for transient forms, confirms, help, and toasts.

**Tech Stack:** Go 1.22+, Bubble Tea, Lip Gloss, existing `internal/tui` model tests, `make verify`.

## Global Constraints

- Follow `docs/superpowers/specs/2026-07-04-tui-three-pane-workspace-design.md`.
- No store changes.
- No CLI changes.
- No new entity types or label semantics.
- No mouse-driven redesign.
- No overlay-based entity detail views.
- No full-screen ANSI snapshot tests.
- Keep API surface stable and versioned; the TUI consumes it.
- No emojis in code or commits.
- Follow existing style in neighboring files.
- Run `make verify` before declaring implementation complete.

---

## File Structure

- Modify `internal/tui/app.go`: root pane constants, size calculation, focus dispatch, workspace rendering, help overlay layering.
- Modify `internal/tui/theme.go`: add pane-border styles for focused and unfocused pane frames.
- Modify `internal/tui/styles.go` only if the existing `titledBoxHeight` helper needs a small clamp/fit adjustment.
- Modify `internal/tui/help.go`: make help content width/height explicit enough for overlay use and remove Help-tab language.
- Modify `internal/tui/keymap.go`: change tab language to pane focus language and remove the old Help column semantics.
- Modify `internal/tui/app_test.go`: replace tab tests with workspace/focus/help-overlay tests and update old Help tab expectations.
- Modify `internal/tui/labels_test.go`: update any comments or key sequences that assume a Labels tab rather than a Labels pane.
- Do not modify `internal/store` or `internal/cli`.

---

### Task 1: Root Focus Model And Help Overlay Dispatch

**Files:**
- Modify: `internal/tui/app.go`
- Modify: `internal/tui/app_test.go`

**Interfaces:**
- Consumes: existing `Model.Update`, `Model.handleKey`, `Model.statusHint`, `helpModel.handleKey`.
- Produces:
  - `paneProjects`, `paneTasks`, `paneLabels` as the only persistent panes.
  - `const numPanes = 3`.
  - `Model.helpOverlayOn bool`.
  - `1/2/3` focus keys that keep all panes visible.
  - `?`/`Esc` close help overlay, and `?` opens it from any focused pane.

- [ ] **Step 1: Replace tab-switching tests with failing focus/overlay tests**

In `internal/tui/app_test.go`, replace `TestTabSwitching`, `TestTabBarShowsNumbers`, `TestThemeCyclesInsideKeymapOverlay`, and `TestThemeChangesActiveTabStyle` with:

```go
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
	if !m.helpOverlayOn {
		t.Fatalf("help overlay should be open")
	}
	if m.focused != paneTasks {
		t.Fatalf("opening help changed focus = %v want paneTasks", m.focused)
	}
	v := m.View()
	mustContain(t, v, "CLI / TUI Parity")
	mustContain(t, v, "Global Keymap")
	before := m.help.offset
	update(t, m, "j")
	if m.help.offset <= before {
		t.Fatalf("help j did not scroll: before=%d after=%d", before, m.help.offset)
	}
	update(t, m, "esc")
	if m.helpOverlayOn {
		t.Fatalf("esc should close help overlay")
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

func TestThemeCyclesInsideHelpOverlay(t *testing.T) {
	m := newTestModel(t)
	update(t, m, "?")
	if !m.helpOverlayOn {
		t.Fatalf("setup: help overlay should be open")
	}
	update(t, m, "T")
	if m.themeName != themeLight {
		t.Fatalf("themeName = %q want %q", m.themeName, themeLight)
	}
	if !m.helpOverlayOn {
		t.Fatalf("theme cycling should not close help overlay")
	}
}
```

- [ ] **Step 2: Run the focused tests to verify they fail**

Run:

```sh
go test ./internal/tui -run 'TestPaneFocusKeys|TestHelpOverlayOpensClosesAndScrolls|TestHelpOverlayReadOnly|TestThemeCyclesInsideHelpOverlay'
```

Expected: fail because `paneHelp`/`keymapOverlayOn` and old `4` tab switching still exist.

- [ ] **Step 3: Update root pane state and key dispatch**

In `internal/tui/app.go`, change the pane constants and root field:

```go
type workspacePane int

const (
	paneProjects workspacePane = iota
	paneTasks
	paneLabels
)

const numPanes = 3
```

Replace `keymapOverlayOn bool` with:

```go
	helpOverlayOn bool
```

Update `handleKey` so the help overlay owns input before confirm/form handling:

```go
	if m.helpOverlayOn {
		switch k.String() {
		case "T":
			m.cycleTheme()
			return nil
		case "?", "esc":
			m.helpOverlayOn = false
			return nil
		default:
			return m.help.handleKey(k)
		}
	}
```

Update the global focus key block:

```go
	switch k.String() {
	case "1":
		m.focused = paneProjects
		return nil
	case "2":
		m.focused = paneTasks
		return nil
	case "3":
		m.focused = paneLabels
		return nil
	case "?":
		m.helpOverlayOn = true
		return nil
	case "T":
		m.cycleTheme()
		return nil
	}
```

Remove the `"4"` case and remove `case paneHelp` from pane-local dispatch and `statusHint`.

- [ ] **Step 4: Update overlay rendering references**

In `Model.View`, replace:

```go
	if m.keymapOverlayOn {
		out = m.placeOverlay(out, m.renderKeymapOverlay())
	}
```

with:

```go
	if m.helpOverlayOn {
		out = m.placeOverlay(out, m.renderHelpOverlay())
	}
```

Add this method near `renderBody`/overlay helpers:

```go
func (m *Model) renderHelpOverlay() string {
	overlayW := m.width - 8
	if overlayW < 40 {
		overlayW = m.width
	}
	overlayH := m.height - 4
	if overlayH < 8 {
		overlayH = m.height
	}
	return titledBoxHeight(m.styles.Dialog, overlayW, "Help", m.help.View(), overlayH)
}
```

Keep `renderKeymapOverlay` temporarily if other tests still reference it; remove it in Task 3 after keymap/help cleanup.

- [ ] **Step 5: Run tests and commit**

Run:

```sh
go test ./internal/tui -run 'TestPaneFocusKeys|TestHelpOverlayOpensClosesAndScrolls|TestHelpOverlayReadOnly|TestThemeCyclesInsideHelpOverlay'
```

Expected: PASS.

Commit:

```sh
git add internal/tui/app.go internal/tui/app_test.go
git commit -m "tui: route focus through three panes"
```

---

### Task 2: Three-Pane Workspace Rendering And Sizing

**Files:**
- Modify: `internal/tui/app.go`
- Modify: `internal/tui/theme.go`
- Modify if needed: `internal/tui/styles.go`
- Modify: `internal/tui/app_test.go`

**Interfaces:**
- Consumes: Task 1's three-pane focus model.
- Produces:
  - `Model.renderWorkspace() string`.
  - `Model.renderPane(pane workspacePane, width int, height int, title string, body string) string`.
  - `Styles.PaneActive lipgloss.Style`.
  - `Styles.PaneInactive lipgloss.Style`.
  - `SetSize` passes pane inner dimensions to `projects`, `tasks`, and `labels`.

- [ ] **Step 1: Add failing workspace rendering tests**

In `internal/tui/app_test.go`, add:

```go
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
	if m.styles.PaneActive.GetBorderForeground() == m.styles.PaneInactive.GetBorderForeground() &&
		m.styles.PaneActive.GetForeground() == m.styles.PaneInactive.GetForeground() {
		t.Fatalf("active and inactive pane styles should differ")
	}
}

func TestStatusLineHintsFollowFocusedPane(t *testing.T) {
	m := newTestModel(t)
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
	mustContain(t, labels, "[a]add")
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run:

```sh
go test ./internal/tui -run 'TestWorkspaceRendersAllPaneTitlesAtOnce|TestPaneFocusKeepsAllPanesVisible|TestFocusedPaneStyleChanges|TestStatusLineHintsFollowFocusedPane'
```

Expected: fail because `View` still renders tab chrome and no pane border styles exist.

- [ ] **Step 3: Add pane styles**

In `internal/tui/theme.go`, add fields to `Styles`:

```go
	PaneActive      lipgloss.Style
	PaneInactive    lipgloss.Style
```

In `buildStyles`, set them:

```go
		PaneActive:      lipgloss.NewStyle().Foreground(t.Text).BorderForeground(t.Accent),
		PaneInactive:    lipgloss.NewStyle().Foreground(t.Muted).BorderForeground(t.Border),
```

In the `themeMono` block, keep active/inactive visibly different:

```go
		s.PaneActive = lipgloss.NewStyle().Bold(true).BorderForeground(t.Accent)
		s.PaneInactive = lipgloss.NewStyle().BorderForeground(t.Border)
```

- [ ] **Step 4: Change size calculation to pane inner sizes**

In `internal/tui/app.go`, update `SetSize`:

```go
	m.contentHeight = h - 1 // workspace + status line; no tab bar/separators
	if m.contentHeight < 1 {
		m.contentHeight = 1
	}

	leftW, rightW := splitWorkspaceWidths(w)
	tasksH, labelsH := splitRightColumnHeights(m.contentHeight)

	m.projects.SetSize(innerPaneWidth(leftW), innerPaneHeight(m.contentHeight))
	m.tasks.SetSize(innerPaneWidth(rightW), innerPaneHeight(tasksH))
	m.labels.SetSize(innerPaneWidth(rightW), innerPaneHeight(labelsH))
	m.help.SetSize(w, m.contentHeight)
	m.help.refresh()
```

Add helpers near `SetSize`:

```go
func splitWorkspaceWidths(width int) (int, int) {
	if width < 2 {
		return width, 0
	}
	left := width * 40 / 100
	if left < 24 && width >= 48 {
		left = 24
	}
	if left > width-20 && width >= 40 {
		left = width - 20
	}
	right := width - left
	return left, right
}

func splitRightColumnHeights(height int) (int, int) {
	if height < 2 {
		return height, 0
	}
	top := height / 2
	bottom := height - top
	return top, bottom
}

func innerPaneWidth(width int) int {
	if width <= 2 {
		return 1
	}
	return width - 2
}

func innerPaneHeight(height int) int {
	if height <= 2 {
		return 1
	}
	return height - 2
}
```

- [ ] **Step 5: Replace tab body rendering with workspace rendering**

In `Model.View`, replace the builder that writes `renderTabBar`, separators, and `renderBody` with:

```go
	var b strings.Builder
	b.WriteString(m.renderWorkspace())
	b.WriteString("\n")
	b.WriteString(m.renderStatusLine())
```

Add:

```go
func (m *Model) renderWorkspace() string {
	leftW, rightW := splitWorkspaceWidths(m.width)
	tasksH, labelsH := splitRightColumnHeights(m.contentHeight)

	projects := m.renderPane(paneProjects, leftW, m.contentHeight, "[1] Projects", m.projects.View())
	tasks := m.renderPane(paneTasks, rightW, tasksH, "[2] Tasks", m.tasks.View())
	labels := m.renderPane(paneLabels, rightW, labelsH, "[3] Labels", m.labels.View())

	right := lipgloss.JoinVertical(lipgloss.Left, tasks, labels)
	return lipgloss.JoinHorizontal(lipgloss.Top, projects, right)
}

func (m *Model) renderPane(pane workspacePane, width int, height int, title string, body string) string {
	style := m.styles.PaneInactive
	if m.focused == pane {
		style = m.styles.PaneActive
	}
	return titledBoxHeight(style, width, title, body, height)
}
```

Remove `renderTabBar` and `renderBody` if no tests or methods call them after Task 3 cleanup.

- [ ] **Step 6: Run tests and commit**

Run:

```sh
go test ./internal/tui -run 'TestWorkspaceRendersAllPaneTitlesAtOnce|TestPaneFocusKeepsAllPanesVisible|TestFocusedPaneStyleChanges|TestStatusLineHintsFollowFocusedPane|TestProjectsListPopulated|TestTasksFlatListEmptyFilter'
```

Expected: PASS.

Commit:

```sh
git add internal/tui/app.go internal/tui/theme.go internal/tui/styles.go internal/tui/app_test.go
git commit -m "tui: render persistent three-pane workspace"
```

---

### Task 3: Help Content And Keymap Cleanup

**Files:**
- Modify: `internal/tui/help.go`
- Modify: `internal/tui/keymap.go`
- Modify: `internal/tui/app.go`
- Modify: `internal/tui/app_test.go`
- Modify: `internal/tui/labels_test.go`

**Interfaces:**
- Consumes: Task 1's `helpOverlayOn`, Task 2's workspace render.
- Produces:
  - help overlay content that describes panes instead of tabs.
  - keymap table without old Help-tab semantics.
  - no reachable `paneHelp`.
  - no remaining `renderTabBar`, `renderBody`, `renderKeymapOverlay`, or `keymapOverlayOn` references.

- [ ] **Step 1: Add failing help/keymap tests**

In `internal/tui/app_test.go`, replace the old Help tab tests with:

```go
func TestHelpOverlayParityTable(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 35)
	update(t, m, "?")
	v := m.View()
	mustContain(t, v, "─ CLI / TUI Parity ─")
	mustContain(t, v, "atm project create")
	mustContain(t, v, "atm task create")
	mustContain(t, v, "atm conventions")
	mustNotContain(t, v, "Help tab")
}

func TestHelpOverlayConventions(t *testing.T) {
	m := newTestModel(t)
	update(t, m, "?")
	content := strings.Join(m.help.lines, "\n")
	mustContain(t, content, "─ Conventions ─")
	mustContain(t, content, "advisory")
	mustContain(t, content, "Suggested seed namespaces")
	mustNotContain(t, content, "## Suggested seed namespaces")
	mustNotContain(t, content, "## Agent code-of-conduct")
}

func TestHelpOverlayKeymapUsesPaneLanguage(t *testing.T) {
	m := newTestModel(t)
	update(t, m, "?")
	content := strings.Join(m.help.lines, "\n")
	mustContain(t, content, "─ Global Keymap ─")
	mustContain(t, content, "focus pane")
	mustContain(t, content, "open help")
	mustContain(t, content, "cycle theme")
	mustNotContain(t, content, "switch tab")
	mustNotContain(t, content, "Help")
}
```

In `internal/tui/labels_test.go`, update comments and key steps that say `"Labels tab"` to `"Labels pane"`; do not change behavior except old `update(t, m, "3")` remains valid as focus.

- [ ] **Step 2: Run tests to verify they fail**

Run:

```sh
go test ./internal/tui -run 'TestHelpOverlayParityTable|TestHelpOverlayConventions|TestHelpOverlayKeymapUsesPaneLanguage'
```

Expected: fail because help strings and keymap rows still say tab/Help tab.

- [ ] **Step 3: Update keymap rows**

In `internal/tui/keymap.go`, change `keyEntry` to remove `Help`:

```go
// Columns: Key | Projects | Tasks | Labels | Detail.
type keyEntry struct {
	Key      string
	Projects string
	Tasks    string
	Labels   string
	Detail   string
}
```

Replace `keymapRows` with:

```go
var keymapRows = []keyEntry{
	{"1/2/3", "focus pane", "focus pane", "focus pane", "focus pane"},
	{"j/k", "move cursor", "move cursor", "move cursor", "scroll"},
	{"g", "top of list", "top of list", "top of list", "top"},
	{"Enter", "open detail", "open detail / toggle group", "open label detail", "confirm overlay"},
	{"Esc", "back", "back / cancel filter", "back", "back / cancel overlay"},
	{"/", "-", "edit filter", "-", "-"},
	{"s", "select project", "cycle sort", "-", "-"},
	{"S", "-", "-", "seed default labels", "-"},
	{"a", "add project", "add task", "add label", "-"},
	{"d", "-", "-", "describe label", "edit description (task)"},
	{"l", "-", "-", "remove label", "-"},
	{"x", "remove project (confirm)", "-", "-", "remove task (confirm)"},
	{"e", "-", "-", "-", "edit title (task)"},
	{"b/B", "-", "-", "-", "add/remove label (task)"},
	{"N", "set name (project detail)", "-", "-", "-"},
	{"H", "toggle history (project detail)", "-", "-", "-"},
	{"T", "cycle theme", "cycle theme", "cycle theme", "cycle theme"},
	{"?", "open help", "open help", "open help", "close help overlay"},
	{"q / ctrl+c", "quit", "quit", "quit", "quit"},
	{"PgDn/Space", "-", "next page", "next page", "scroll down"},
	{"PgUp", "-", "prev page", "prev page", "-"},
}
```

- [ ] **Step 4: Update help table and parity copy**

In `internal/tui/help.go`, update `parityTable` strings from `Projects tab`, `Tasks tab`, and `Labels tab` to `Projects pane`, `Tasks pane`, and `Labels pane`. Change:

```text
atm conventions                       this tab, bottom section
```

to:

```text
atm conventions                       help overlay, conventions section
```

Update `keymapTable` to five columns:

```go
func keymapTable() string {
	var b strings.Builder
	widths := []int{10, 18, 21, 19, 21}
	fmt.Fprintf(&b, "%-*s %-*s %-*s %-*s %-*s\n",
		widths[0], "Key",
		widths[1], "Projects",
		widths[2], "Tasks",
		widths[3], "Labels",
		widths[4], "Detail/Overlay",
	)
	b.WriteString(strings.Repeat("-", widths[0]+1+widths[1]+1+widths[2]+1+widths[3]+1+widths[4]))
	b.WriteString("\n")
	for _, r := range keymapRows {
		fmt.Fprintf(&b, "%-*s %-*s %-*s %-*s %-*s\n",
			widths[0], truncateRunes(r.Key, widths[0]),
			widths[1], truncateRunes(r.Projects, widths[1]),
			widths[2], truncateRunes(r.Tasks, widths[2]),
			widths[3], truncateRunes(r.Labels, widths[3]),
			widths[4], truncateRunes(r.Detail, widths[4]),
		)
	}
	return b.String()
}
```

- [ ] **Step 5: Remove old tab/keymap overlay code**

In `internal/tui/app.go`, remove dead code:

```go
func (m *Model) renderTabBar() string { ... }
func (m *Model) renderBody() string { ... }
func (m *Model) renderKeymapOverlay() string { ... }
```

Run:

```sh
rg -n 'paneHelp|keymapOverlayOn|renderTabBar|renderBody|renderKeymapOverlay|switch tab|Help tab' internal/tui
```

Expected: no code references. Test comments may be updated rather than left stale.

- [ ] **Step 6: Run tests and commit**

Run:

```sh
go test ./internal/tui -run 'TestHelpOverlayParityTable|TestHelpOverlayConventions|TestHelpOverlayKeymapUsesPaneLanguage|TestHelpOverlayReadOnly'
```

Expected: PASS.

Commit:

```sh
git add internal/tui/help.go internal/tui/keymap.go internal/tui/app.go internal/tui/app_test.go internal/tui/labels_test.go
git commit -m "tui: move help into workspace overlay"
```

---

### Task 4: Pane-Local Detail, Overlay Layering, And Cramped Terminal Guardrails

**Files:**
- Modify: `internal/tui/app.go`
- Modify: `internal/tui/app_test.go`

**Interfaces:**
- Consumes: Task 2's workspace rendering and Task 3's help overlay.
- Produces:
  - detail stays inside the focused pane.
  - `Esc` only backs the focused pane out of detail.
  - forms/confirms layer above the workspace.
  - task filter editing keeps priority over focus dispatch.
  - narrow/short sizes render without panic.

- [ ] **Step 1: Add failing/guard tests**

In `internal/tui/app_test.go`, add:

```go
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
	if m.form != nil || m.confirm != confirmNone || m.helpOverlayOn {
		t.Fatalf("detail should not open an overlay: form=%v confirm=%v help=%v", m.form != nil, m.confirm, m.helpOverlayOn)
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
	update(t, m, "enter")
	if m.projects.view != pViewDetail {
		t.Fatalf("setup: project detail not open")
	}
	update(t, m, "s")
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

func TestFormOverlayRendersAboveWorkspace(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 36)
	seedProject(t, m, "ATM", "Acme Task Manager")
	base := m.View()
	mustContain(t, base, "[1] Projects")
	mustContain(t, base, "[2] Tasks")
	update(t, m, "a")
	withOverlay := m.View()
	mustContain(t, withOverlay, "New project")
	mustContain(t, withOverlay, "[1] Projects")
	mustContain(t, withOverlay, "[2] Tasks")
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
	if m.tasks.filterDraft != "1" {
		t.Fatalf("filterDraft = %q want 1", m.tasks.filterDraft)
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
```

Add `fmt` to the `internal/tui/app_test.go` imports for `TestWorkspaceRendersAtCrampedSizes`.

- [ ] **Step 2: Run tests**

Run:

```sh
go test ./internal/tui -run 'TestDetailOpensInsideFocusedPaneNotOverlay|TestEscBacksOnlyFocusedPaneOutOfDetail|TestFormOverlayRendersAboveWorkspace|TestTaskFilterEditingKeepsPriorityOverFocusKeys|TestWorkspaceRendersAtCrampedSizes'
```

Expected: PASS or reveal small layout/dispatch bugs from Tasks 1-3.

- [ ] **Step 3: Fix any dispatch or cramped-size issues**

If `TestTaskFilterEditingKeepsPriorityOverFocusKeys` fails, ensure this block remains before global focus dispatch in `handleKey`:

```go
	if m.focused == paneTasks && m.tasks.filterEditing {
		return m.tasks.handleKey(k)
	}
```

If cramped rendering panics or produces negative dimensions, clamp all dimensions passed to `titledBoxHeight`:

```go
func clampPaneDimension(n int) int {
	if n < 1 {
		return 1
	}
	return n
}
```

Use it in `renderPane`:

```go
func (m *Model) renderPane(pane workspacePane, width int, height int, title string, body string) string {
	style := m.styles.PaneInactive
	if m.focused == pane {
		style = m.styles.PaneActive
	}
	return titledBoxHeight(style, clampPaneDimension(width), title, body, clampPaneDimension(height))
}
```

If `TestEscBacksOnlyFocusedPaneOutOfDetail` fails, keep the existing Esc logic scoped to `m.focused` only; do not globally close all detail views.

- [ ] **Step 4: Run all TUI tests**

Run:

```sh
go test ./internal/tui
```

Expected: PASS.

- [ ] **Step 5: Commit**

```sh
git add internal/tui/app.go internal/tui/app_test.go
git commit -m "tui: preserve pane-local detail in workspace"
```

---

### Task 5: Full Verification And Spec Alignment

**Files:**
- Modify only if verification exposes misses:
  - `internal/tui/app.go`
  - `internal/tui/help.go`
  - `internal/tui/keymap.go`
  - `internal/tui/app_test.go`
  - `internal/tui/labels_test.go`

**Interfaces:**
- Consumes: Tasks 1-4.
- Produces: repository verification passing with the three-pane workspace behavior.

- [ ] **Step 1: Search for stale tab/help references**

Run:

```sh
rg -n 'tab bar|switch tab|Help tab|paneHelp|keymapOverlayOn|renderTabBar|renderKeymapOverlay|1/2/3/4' internal/tui
```

Expected: no stale `internal/tui` code references. The spec may mention removed old behavior in the driver; do not edit the spec unless a real requirement changed.

- [ ] **Step 2: Run full verification**

Run:

```sh
make verify
```

Expected: PASS.

- [ ] **Step 3: Commit final fixes if any**

If Step 1 or Step 2 required fixes:

```sh
git add internal/tui
git commit -m "test: verify three-pane tui workspace"
```

If no fixes were needed, do not create an empty commit.

- [ ] **Step 4: Final implementation summary**

Record the final result for the user:

```text
Implemented the three-pane TUI workspace:
- Projects, Tasks, and Labels render together.
- 1/2/3 move focus instead of switching tabs.
- Help is a ? overlay.
- Entity detail stays pane-local.
- Forms/confirms/toasts still layer above the workspace.

Verification: make verify passed.
```
