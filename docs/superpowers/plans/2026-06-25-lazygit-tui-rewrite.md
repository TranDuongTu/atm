# Lazygit TUI Rewrite Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Rebuild `atm tui` from a five-tab UI into the approved lazygit-style workspace with persistent Projects, Tasks, Summary, and Help panes.

**Architecture:** Keep the TUI as a thin Bubble Tea client over `internal/store`. Replace tab state with workspace focus, project scope, scoped snapshots, and contextual right-column rendering while reusing existing forms, overlays, toast handling, keymap names, and store-backed mutations.

**Tech Stack:** Go 1.22+, Bubble Tea, Lip Gloss, existing `internal/store` APIs, `make verify`.

## Global Constraints

- Follow `docs/superpowers/specs/2026-06-25-lazygit-tui-rewrite-design.md`.
- Replace top-level tabs with one persistent workspace.
- Use a 30% left column for navigation and a 70% right column for contextual details and actions.
- Keep four left panes visible and stacked vertically: Projects, Tasks, Summary, Help.
- Let number keys `1`-`4` focus left panes.
- Let `j`/`k` and Up/Down move inside the currently focused pane only.
- Use `Space` on a project to set the active project scope; pressing `Space` on the scoped project clears scope back to all projects.
- Default Tasks and Summary to all projects when no project is scoped.
- Remove Actors from primary navigation and surface actor/claims context inside the Tasks right column.
- Keep all TUI mutations backed by existing `store.*` operations exposed through CLI parity.
- No new TUI-only store mutation.
- No auto-refresh.
- No emojis in code or commits.

---

## File Structure

- Modify `internal/tui/app.go`: replace tab fields/constants with workspace focus/scope fields, refresh and size propagation, root key routing, and workspace `View`.
- Modify `internal/tui/keymap.go`: remove tab-era labels from help text and keep existing action keys.
- Modify `internal/tui/projects.go`: keep project list/actions, add scope toggling helpers, expose compact left rendering and right stacked detail sections.
- Modify `internal/tui/tasks.go`: apply active project scope to list refresh, keep selected task available for right-column rendering, and show actor/claims context.
- Modify `internal/tui/dashboard.go`: convert dashboard behavior into Summary; aggregate all projects when no scope is active.
- Modify `internal/tui/help.go`: update help copy for workspace navigation and remove Actors as a primary pane.
- Modify `internal/tui/app_test.go`: replace tab tests with workspace navigation, scope, rendering, and actor-context tests.

## Task 1: Workspace Focus Model

**Files:**
- Modify: `internal/tui/app.go`
- Modify: `internal/tui/app_test.go`

**Interfaces:**
- Produces: `type workspacePane int`, constants `paneProjects`, `paneTasks`, `paneSummary`, `paneHelp`.
- Produces: `func (m *Model) focusedPaneName() string`.
- Produces: `func (m *Model) selectedProjectCode() string`.

- [ ] **Step 1: Write the failing tests**

Add tests to `internal/tui/app_test.go`:

```go
func TestWorkspace_DefaultFocusAndNoActorsTab(t *testing.T) {
	root := setupTempStore(t)
	seedProject(t, root, "human:alice")
	m, err := NewModel(NewModelOpts{StorePath: root, Actor: "human:alice"})
	if err != nil {
		t.Fatalf("NewModel: %v", err)
	}
	if m.focusedPaneName() != "Projects" {
		t.Fatalf("expected default focus Projects, got %s", m.focusedPaneName())
	}
	view := m.View()
	for _, label := range []string{"[1] Projects", "[2] Tasks", "[3] Summary", "[4] Help"} {
		if !strings.Contains(view, label) {
			t.Errorf("expected workspace label %q in view", label)
		}
	}
	if strings.Contains(view, "Actors") || strings.Contains(view, "5 Help") {
		t.Fatalf("actors/tab-era navigation should be absent:\n%s", view)
	}
}

func TestWorkspace_NumberKeysFocusPanes(t *testing.T) {
	root := setupTempStore(t)
	seedProject(t, root, "human:alice")
	m, err := NewModel(NewModelOpts{StorePath: root, Actor: "human:alice"})
	if err != nil {
		t.Fatalf("NewModel: %v", err)
	}
	cases := []struct {
		key  string
		want string
	}{
		{"2", "Tasks"},
		{"3", "Summary"},
		{"4", "Help"},
		{"1", "Projects"},
	}
	for _, c := range cases {
		mod, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(c.key)})
		m = mod.(*Model)
		if got := m.focusedPaneName(); got != c.want {
			t.Fatalf("after %q expected %s, got %s", c.key, c.want, got)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/tui -run 'TestWorkspace_DefaultFocusAndNoActorsTab|TestWorkspace_NumberKeysFocusPanes'`

Expected: FAIL because `focusedPaneName` and workspace pane labels do not exist.

- [ ] **Step 3: Implement minimal workspace focus**

In `internal/tui/app.go`, replace tab constants with workspace pane constants:

```go
type workspacePane int

const (
	paneProjects workspacePane = iota
	paneTasks
	paneSummary
	paneHelp
)

func (m *Model) focusedPaneName() string {
	switch m.focused {
	case paneProjects:
		return "Projects"
	case paneTasks:
		return "Tasks"
	case paneSummary:
		return "Summary"
	case paneHelp:
		return "Help"
	default:
		return "Projects"
	}
}

func (m *Model) selectedProjectCode() string {
	if m.projectScope == "" {
		return ""
	}
	return m.projectScope
}
```

Change `Model` fields:

```go
focused     workspacePane
projectScope string
summary     *dashboardModel
```

Remove `actors *actorsModel` from `Model`. Initialize `m.summary = newDashboardModel(m)` and keep `m.focused` at its zero value (`paneProjects`).

Route number keys in `handleKey`:

```go
case "1":
	m.focused = paneProjects
	return m, nil
case "2":
	m.focused = paneTasks
	return m, nil
case "3":
	m.focused = paneSummary
	return m, nil
case "4":
	m.focused = paneHelp
	return m, nil
```

Update references from `m.dash` to `m.summary` and remove `m.actors` setup calls.

- [ ] **Step 4: Render placeholder workspace view**

In `View`, render header, four left pane labels, the focused pane's current view on the right, and footer. Keep existing startup/form/overlay precedence unchanged.

Use labels exactly:

```text
[1] Projects
[2] Tasks
[3] Summary
[4] Help
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/tui -run 'TestWorkspace_DefaultFocusAndNoActorsTab|TestWorkspace_NumberKeysFocusPanes'`

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/tui/app.go internal/tui/app_test.go
git commit -m "feat: introduce tui workspace focus"
```

## Task 2: Workspace Layout and Left-Pane Movement

**Files:**
- Modify: `internal/tui/app.go`
- Modify: `internal/tui/app_test.go`

**Interfaces:**
- Consumes: `workspacePane`, `Model.focused`.
- Produces: root layout with left/right split.
- Produces: key routing where Up/Down only updates the focused left pane.

- [ ] **Step 1: Write the failing tests**

Add:

```go
func TestWorkspace_MovementOnlyAffectsFocusedPane(t *testing.T) {
	root := setupTempStore(t)
	seedProject(t, root, "human:alice")
	s, _ := store.Open(root)
	_, _ = s.CreateProject("DEMO", "Demo", "type", nil, nil, "human:alice")
	_, _ = s.CreateTask("ATM", "ATM task", "", nil, "human:alice")
	_, _ = s.CreateTask("DEMO", "Demo task", "", nil, "human:alice")
	m, err := NewModel(NewModelOpts{StorePath: root, Actor: "human:alice"})
	if err != nil {
		t.Fatalf("NewModel: %v", err)
	}
	projectCursor := m.projects.cursor
	mod, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("2")})
	m = mod.(*Model)
	mod, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	m = mod.(*Model)
	if m.projects.cursor != projectCursor {
		t.Fatalf("projects cursor changed while Tasks focused")
	}
	if m.tasks.cursor != 1 {
		t.Fatalf("expected tasks cursor 1, got %d", m.tasks.cursor)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tui -run TestWorkspace_MovementOnlyAffectsFocusedPane`

Expected: FAIL until root routing sends movement only to focused pane.

- [ ] **Step 3: Implement focused-pane routing**

In `handleKey`, after form/overlay/filter/global handling, switch on `m.focused`:

```go
switch m.focused {
case paneProjects:
	return m.projects.update(msg)
case paneTasks:
	return m.tasks.update(msg)
case paneSummary:
	return m.summary.update(msg)
case paneHelp:
	return m.help.update(msg)
}
return m, nil
```

Remove Tab/Shift+Tab tab cycling.

- [ ] **Step 4: Implement 30/70 sizing**

In `SetSize`, compute:

```go
leftW := w * 30 / 100
if leftW < 24 {
	leftW = 24
}
if leftW > 44 {
	leftW = 44
}
rightW := w - leftW
```

Set each left pane to `leftW` and each right view to `rightW` through existing model `setSize` calls. Preserve minimum height clamping.

- [ ] **Step 5: Run tests**

Run: `go test ./internal/tui -run 'TestWorkspace_DefaultFocusAndNoActorsTab|TestWorkspace_NumberKeysFocusPanes|TestWorkspace_MovementOnlyAffectsFocusedPane'`

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/tui/app.go internal/tui/app_test.go
git commit -m "feat: route tui workspace movement"
```

## Task 3: Project Scope Toggle and Scoped Tasks

**Files:**
- Modify: `internal/tui/app.go`
- Modify: `internal/tui/projects.go`
- Modify: `internal/tui/tasks.go`
- Modify: `internal/tui/app_test.go`

**Interfaces:**
- Consumes: `Model.projectScope`.
- Produces: `projectsModel.selectedCode() (string, bool)`.
- Produces: `tasksModel.refresh()` applies `store.QueryFilters{Project: m.projectScope}` when scope is set.

- [ ] **Step 1: Write failing tests**

Add:

```go
func TestWorkspace_ProjectScopeTogglesWithSpace(t *testing.T) {
	root := setupTempStore(t)
	seedProject(t, root, "human:alice")
	m, err := NewModel(NewModelOpts{StorePath: root, Actor: "human:alice"})
	if err != nil {
		t.Fatalf("NewModel: %v", err)
	}
	mod, _ := m.Update(tea.KeyMsg{Type: tea.KeySpace})
	m = mod.(*Model)
	if m.projectScope != "ATM" {
		t.Fatalf("expected scope ATM, got %q", m.projectScope)
	}
	mod, _ = m.Update(tea.KeyMsg{Type: tea.KeySpace})
	m = mod.(*Model)
	if m.projectScope != "" {
		t.Fatalf("expected cleared scope, got %q", m.projectScope)
	}
}

func TestWorkspace_TasksDefaultAllProjectsAndFilterWhenScoped(t *testing.T) {
	root := setupTempStore(t)
	seedProject(t, root, "human:alice")
	s, _ := store.Open(root)
	_, _ = s.CreateProject("DEMO", "Demo", "type", nil, nil, "human:alice")
	_, _ = s.CreateTask("ATM", "ATM task", "", nil, "human:alice")
	_, _ = s.CreateTask("DEMO", "Demo task", "", nil, "human:alice")
	m, err := NewModel(NewModelOpts{StorePath: root, Actor: "human:alice"})
	if err != nil {
		t.Fatalf("NewModel: %v", err)
	}
	if len(m.tasks.tasks) != 2 {
		t.Fatalf("expected all tasks by default, got %d", len(m.tasks.tasks))
	}
	mod, _ := m.Update(tea.KeyMsg{Type: tea.KeySpace})
	m = mod.(*Model)
	if len(m.tasks.tasks) != 1 || m.tasks.tasks[0].Project != "ATM" {
		t.Fatalf("expected scoped ATM task, got %#v", m.tasks.tasks)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/tui -run 'TestWorkspace_ProjectScopeTogglesWithSpace|TestWorkspace_TasksDefaultAllProjectsAndFilterWhenScoped'`

Expected: FAIL because Space does not toggle workspace scope.

- [ ] **Step 3: Implement project selection helper**

In `projects.go`:

```go
func (p *projectsModel) selectedCode() (string, bool) {
	list := p.filtered()
	if p.cursor < 0 || p.cursor >= len(list) {
		return "", false
	}
	return list[p.cursor].Code, true
}
```

- [ ] **Step 4: Implement scope toggle**

In root key handling, before pane routing:

```go
case " ":
	if m.focused == paneProjects {
		if code, ok := m.projects.selectedCode(); ok {
			if m.projectScope == code {
				m.projectScope = ""
			} else {
				m.projectScope = code
			}
			m.refreshAll()
		}
	}
	return m, nil
```

- [ ] **Step 5: Apply scope to tasks refresh**

In `tasksModel.refresh`, derive filters from `t.filters` but override project from scope:

```go
filters := t.filters
filters.Project = t.app.projectScope
t.tasks = t.app.store.ListTasks(filters)
```

- [ ] **Step 6: Run tests**

Run: `go test ./internal/tui -run 'TestWorkspace_ProjectScopeTogglesWithSpace|TestWorkspace_TasksDefaultAllProjectsAndFilterWhenScoped'`

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/tui/app.go internal/tui/projects.go internal/tui/tasks.go internal/tui/app_test.go
git commit -m "feat: add tui project scope"
```

## Task 4: Summary Scope and All-Project Aggregates

**Files:**
- Modify: `internal/tui/dashboard.go`
- Modify: `internal/tui/app.go`
- Modify: `internal/tui/app_test.go`

**Interfaces:**
- Consumes: `Model.projectScope`.
- Produces: Summary view that says `scope: all` or `scope: <PROJECT>`.
- Produces: all-project status counts from `store.ListTasks(store.QueryFilters{})`.

- [ ] **Step 1: Write failing test**

Add:

```go
func TestWorkspace_SummaryDefaultsAllAndScopesToProject(t *testing.T) {
	root := setupTempStore(t)
	seedProject(t, root, "human:alice")
	s, _ := store.Open(root)
	_, _ = s.CreateProject("DEMO", "Demo", "type", nil, nil, "human:alice")
	_, _ = s.CreateTask("ATM", "ATM task", "", nil, "human:alice")
	_, _ = s.CreateTask("DEMO", "Demo task", "", nil, "human:alice")
	m, err := NewModel(NewModelOpts{StorePath: root, Actor: "human:alice"})
	if err != nil {
		t.Fatalf("NewModel: %v", err)
	}
	mod, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("3")})
	m = mod.(*Model)
	view := m.View()
	if !strings.Contains(view, "scope: all") || !strings.Contains(view, "2 task(s)") {
		t.Fatalf("expected all-project summary:\n%s", view)
	}
	mod, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("1")})
	m = mod.(*Model)
	mod, _ = m.Update(tea.KeyMsg{Type: tea.KeySpace})
	m = mod.(*Model)
	mod, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("3")})
	m = mod.(*Model)
	view = m.View()
	if !strings.Contains(view, "scope: ATM") || !strings.Contains(view, "1 task(s)") {
		t.Fatalf("expected scoped summary:\n%s", view)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tui -run TestWorkspace_SummaryDefaultsAllAndScopesToProject`

Expected: FAIL because dashboard currently picks the first project only.

- [ ] **Step 3: Implement Summary rendering**

Update `dashboardModel.view` to use:

```go
filters := store.QueryFilters{Project: d.app.projectScope}
tasks := d.app.store.ListTasks(filters)
scope := "all"
if d.app.projectScope != "" {
	scope = d.app.projectScope
}
```

Render counts by status, review queue count, open followups count, and guide health using existing store reads. For project-specific `Dashboard`, call it only when `projectScope != ""`; otherwise aggregate status counts and omit project-only guide details with the line `Guide health: select a project for detailed guide freshness`.

- [ ] **Step 4: Run test**

Run: `go test ./internal/tui -run TestWorkspace_SummaryDefaultsAllAndScopesToProject`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/dashboard.go internal/tui/app.go internal/tui/app_test.go
git commit -m "feat: scope tui summary"
```

## Task 5: Contextual Right Columns

**Files:**
- Modify: `internal/tui/projects.go`
- Modify: `internal/tui/tasks.go`
- Modify: `internal/tui/help.go`
- Modify: `internal/tui/app_test.go`

**Interfaces:**
- Produces: Projects right column section labels `Facts`, `Labels`, `Repos`, `Guide`, `Advanced`.
- Produces: Left/Right navigation for `projectsModel.paneCursor`.
- Produces: Tasks right column includes `Actor / claims` text.

- [ ] **Step 1: Write failing tests**

Add:

```go
func TestWorkspace_ProjectsRightColumnSectionsNavigateWithLeftRight(t *testing.T) {
	root := setupTempStore(t)
	seedProject(t, root, "human:alice")
	m, err := NewModel(NewModelOpts{StorePath: root, Actor: "human:alice"})
	if err != nil {
		t.Fatalf("NewModel: %v", err)
	}
	view := m.View()
	for _, label := range []string{"Facts", "Labels", "Repos", "Guide", "Advanced"} {
		if !strings.Contains(view, label) {
			t.Fatalf("expected project section %q:\n%s", label, view)
		}
	}
	mod, _ := m.Update(tea.KeyMsg{Type: tea.KeyRight})
	m = mod.(*Model)
	if m.projects.paneCursor != 1 {
		t.Fatalf("expected paneCursor 1, got %d", m.projects.paneCursor)
	}
}

func TestWorkspace_TaskRightColumnIncludesActorClaimsContext(t *testing.T) {
	root := setupTempStore(t)
	seedProject(t, root, "human:alice")
	s, _ := store.Open(root)
	_, _ = s.CreateTask("ATM", "Sample task", "", nil, "human:alice")
	m, err := NewModel(NewModelOpts{StorePath: root, Actor: "human:alice"})
	if err != nil {
		t.Fatalf("NewModel: %v", err)
	}
	mod, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("2")})
	m = mod.(*Model)
	view := m.View()
	if !strings.Contains(view, "Actor / claims") || !strings.Contains(view, "current actor: human:alice") {
		t.Fatalf("task right column missing actor context:\n%s", view)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/tui -run 'TestWorkspace_ProjectsRightColumnSectionsNavigateWithLeftRight|TestWorkspace_TaskRightColumnIncludesActorClaimsContext'`

Expected: FAIL until contextual right-column rendering exists.

- [ ] **Step 3: Implement project right sections**

Add a `rightView()` method in `projects.go` that renders the selected project and all section headers:

```go
sections := []string{"Facts", "Labels", "Repos", "Guide", "Advanced"}
```

Use `paneCursor` to mark the active section. Handle `"right"` and `"left"` in `projectsModel.updateList` by clamping `paneCursor` within `0..len(sections)-1`.

- [ ] **Step 4: Implement task right actor/claims context**

Add `rightView()` in `tasks.go`. It should render the selected list task when no detail is open and render `detail.Task` when detail exists. Include:

```text
Actor / claims
current actor: <actor>
claimant: <task.Claimant or none>
assignee: <task.Assignee or none>
```

- [ ] **Step 5: Update help copy**

In `help.go`, replace tab-era text with workspace text:

```text
1-4 focus left panes
Space toggles project scope while Projects is focused
Left/Right moves project right-column sections
Actors are shown in task actor/claims context
```

- [ ] **Step 6: Run tests**

Run: `go test ./internal/tui -run 'TestWorkspace_ProjectsRightColumnSectionsNavigateWithLeftRight|TestWorkspace_TaskRightColumnIncludesActorClaimsContext|TestHelpModel_RendersParityTable'`

Expected: PASS after updating the help test expectations away from tab-era wording.

- [ ] **Step 7: Commit**

```bash
git add internal/tui/projects.go internal/tui/tasks.go internal/tui/help.go internal/tui/app_test.go
git commit -m "feat: render tui contextual panes"
```

## Task 6: Remove Tab-Era Actors Surface and Final Verification

**Files:**
- Modify: `internal/tui/app.go`
- Modify: `internal/tui/actors.go` only if compilation requires deleting references.
- Modify: `internal/tui/keymap.go`
- Modify: `internal/tui/app_test.go`

**Interfaces:**
- Consumes: workspace panes from earlier tasks.
- Produces: no top-level actor pane or tab-era key behavior.

- [ ] **Step 1: Write failing regression test**

Add:

```go
func TestWorkspace_TabEraKeysDoNotExposeActors(t *testing.T) {
	root := setupTempStore(t)
	seedProject(t, root, "human:alice")
	m, err := NewModel(NewModelOpts{StorePath: root, Actor: "human:alice"})
	if err != nil {
		t.Fatalf("NewModel: %v", err)
	}
	mod, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("5")})
	m = mod.(*Model)
	if got := m.focusedPaneName(); got != "Projects" {
		t.Fatalf("key 5 should not change focus, got %s", got)
	}
	mod, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = mod.(*Model)
	if got := m.focusedPaneName(); got != "Projects" {
		t.Fatalf("tab should not cycle panes, got %s", got)
	}
	if strings.Contains(m.View(), "Actors") {
		t.Fatalf("actors primary navigation should be absent:\n%s", m.View())
	}
}
```

- [ ] **Step 2: Run test to verify it fails if tab-era behavior remains**

Run: `go test ./internal/tui -run TestWorkspace_TabEraKeysDoNotExposeActors`

Expected: FAIL until all tab-era routing is removed.

- [ ] **Step 3: Remove remaining tab-era behavior**

Remove or stop using:

```go
tabDashboard
tabProjects
tabTasks
tabActors
tabHelp
Model.tab
Model.actors
```

Keep `actors.go` only if it is harmless unused code; delete it only if the package conventions prefer removing dead TUI surfaces. No root view or key should reference it.

- [ ] **Step 4: Run focused TUI tests**

Run: `go test ./internal/tui`

Expected: PASS.

- [ ] **Step 5: Run full verification**

Run: `make verify`

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/tui/app.go internal/tui/keymap.go internal/tui/app_test.go internal/tui/actors.go
git commit -m "feat: finish lazygit tui workspace"
```

## Self-Review

- Spec coverage: Tasks cover workspace navigation, 30/70 layout, four left panes, project scope toggling, scoped Tasks and Summary, actor removal from primary navigation, actor/claims context in Tasks, project stacked right sections, Help, and store-backed mutation preservation by reusing existing model actions.
- Placeholder scan: No placeholder markers are used as implementation instructions.
- Type consistency: `workspacePane`, `projectScope`, `focusedPaneName`, `selectedProjectCode`, and `projectsModel.selectedCode` are used consistently across tasks.
