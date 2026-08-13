# Universal Dispatch Dialog Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the TUI dispatch dialog universal — persona becomes a selectable field over all store personas, context only preselects defaults, and `D` opens the dialog from any pane.

**Architecture:** The `dispatchKind` enum in `internal/tui/dispatch.go` is deleted. `dispatchModel` holds a persona list (`store.ListPersonas()`) + cursor, with project-requirement derived per-persona from a new `core.Persona.ProjectOptional` field (populated from the persona spec's `project_optional` frontmatter). The universal `D` handler resolves context into (default persona, project, task) and calls the same `open()`. `open()` signature changes from `open(kind, project, taskID, taskTitle)` to `open(defaultPersona, project, taskID, taskTitle)`.

**Tech Stack:** Go 1.22+, Bubble Tea TUI, cobra CLI (unchanged), existing `internal/store` + `internal/core` + `skills` packages.

## Global Constraints

- Go 1.22+; module `atm`.
- Run `make verify` (build + test + scripts-test) before declaring done; per-task targeted tests use `go test ./internal/store/...` / `go test ./internal/tui/...`.
- Every commit message must name the task: `ATM-29f8b0`.
- No emojis in code or commits. Follow existing style in neighboring files (`internal/tui/dispatch.go`, `internal/tui/capabilityModel` patterns).
- Keep the API surface stable and versioned; the TUI consumes it. `atm --persona <name> --project <CODE> --agent <name>` argv format is unchanged except as specified.
- `admin` is treated as project-optional in the dialog even though its frontmatter lacks `project_optional` (its launch routes to a fresh TUI that ignores `--project`).
- Spec: `docs/superpowers/specs/2026-08-13-universal-dispatch-dialog-design.md`.

---

### Task 1: `core.Persona.ProjectOptional` + store population

**Files:**
- Modify: `internal/core/types.go:66-74`
- Modify: `internal/store/persona.go:28-36`, `internal/store/persona.go:71`
- Test: `internal/store/persona_test.go`

**Interfaces:**
- Consumes: `skills.PersonaSpec.ProjectOptional` (already exists).
- Produces: `core.Persona.ProjectOptional bool`; `Store.ListPersonas()` / `Store.GetPersona()` now populate it from the persona spec.

- [ ] **Step 1: Write the failing store tests**

Add to `internal/store/persona_test.go`:

```go
func TestListPersonasCarriesProjectOptional(t *testing.T) {
	s := newTestStore(t)
	byName := map[string]bool{}
	for _, p := range s.ListPersonas() {
		byName[p.Name] = p.ProjectOptional
	}
	if !byName["concierge"] {
		t.Error("concierge should be project-optional")
	}
	if byName["manager"] || byName["developer"] {
		t.Error("manager/developer should be project-required")
	}
}

func TestCustomPersonaProjectOptionalParsed(t *testing.T) {
	s := newTestStore(t)
	doc := "---\nname: rover\ndescription: Rover guide\nproject_optional: true\n---\nRover body\n"
	if err := os.MkdirAll(filepath.Join(s.Root, "personas"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.Root, "personas", "rover.md"), []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
	p, err := s.GetPersona("rover")
	if err != nil {
		t.Fatal(err)
	}
	if !p.ProjectOptional {
		t.Error("custom persona with project_optional: true should parse as optional")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/store/ -run 'TestListPersonasCarriesProjectOptional|TestCustomPersonaProjectOptionalParsed' -v`
Expected: FAIL — `core.Persona` has no field `ProjectOptional` (compile error).

- [ ] **Step 3: Add the field to `core.Persona`**

In `internal/core/types.go`, add `ProjectOptional` after `Description`:

```go
type Persona struct {
	Name            string    `json:"name"`
	Prompt          string    `json:"prompt"`
	Description     string    `json:"description"`
	ProjectOptional bool      `json:"project_optional,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
	CreatedBy       string    `json:"created_by"`
	UpdatedBy       string    `json:"updated_by"`
}
```

- [ ] **Step 4: Populate it from the persona spec in the store**

In `internal/store/persona.go`, `builtinPersona` becomes:

```go
func builtinPersona(spec skills.PersonaSpec) *core.Persona {
	return &core.Persona{
		Name:            spec.Name,
		Prompt:          spec.Body,
		Description:     spec.Description,
		ProjectOptional: spec.ProjectOptional,
		CreatedBy:       "builtin",
		UpdatedBy:       "builtin",
	}
}
```

In `internal/store/persona.go`, `parsePersonaDoc`'s struct literal becomes:

```go
	p := &core.Persona{Name: spec.Name, Prompt: spec.Body, Description: spec.Description, ProjectOptional: spec.ProjectOptional}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/store/ -run 'TestListPersonasCarriesProjectOptional|TestCustomPersonaProjectOptionalParsed' -v`
Expected: PASS.

Run the full store suite to confirm nothing regressed:
`go test ./internal/store/...`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/core/types.go internal/store/persona.go internal/store/persona_test.go
git commit -m "feat(ATM-29f8b0): expose persona project_optional on core.Persona"
```

---

### Task 2: `dispatchModel` rework — persona field, active state, render

**Files:**
- Modify: `internal/tui/dispatch.go` (full rework)
- Modify: `internal/tui/app.go:399`, `:560`, `:649-676`, `:933`
- Modify: `internal/tui/projects.go:333-350`
- Modify: `internal/tui/dispatch_test.go` (rewrite)
- Modify: `internal/tui/actors_test.go:119-138`, `:180-206`
- Modify: `internal/tui/art_wiring_test.go:85-91`

**Interfaces:**
- Consumes: `core.Persona.ProjectOptional` (Task 1), `Store.ListPersonas() []*core.Persona`, `Store.ProjectRepos(project) ([]core.RepoConfig, error)`.
- Produces: `dispatchModel.active bool` (replaces `kind != dispatchNone`); `dispatchModel.open(defaultPersona string, project, taskID, taskTitle string)`; `dispatchModel.persona() string`; `dispatchModel.projectRequired() bool`. Callers in `app.go` and `projects.go` are updated to the new signature; the D handler in this task keeps its existing conditional behavior (manager/developer/drilled) but passes persona names.

> Note: Go forbids a struct field and method with the same name, so the open/closed flag is `active` (the dialog method stays `open(...)`).

- [ ] **Step 1: Write the failing tests first (rewrite `dispatch_test.go`)**

Replace the body of `internal/tui/dispatch_test.go` with:

```go
package tui

import (
	"errors"
	"os"
	"strings"
	"testing"

	"atm/internal/dispatch"
	tea "github.com/charmbracelet/bubbletea"
)

type fakeDispatcher struct {
	preview       string
	previewErr    error
	spawned       []dispatch.Spec
	spawnErr      error
	previewTarget func(string) (string, error)
}

func (f *fakeDispatcher) Preview() (string, error) { return f.preview, f.previewErr }
func (f *fakeDispatcher) PreviewTarget(target string) (string, error) {
	if f.previewTarget != nil {
		return f.previewTarget(target)
	}
	return f.preview, f.previewErr
}
func (f *fakeDispatcher) Spawn(s dispatch.Spec) error {
	f.spawned = append(f.spawned, s)
	return f.spawnErr
}

func testAgents() []agentOption {
	return []agentOption{
		{name: "claude", ready: true},
		{name: "codex", ready: false, hint: "missing bin: codex (https://developers.openai.com/codex)"},
	}
}

// dispatchKey delivers one key press to the model, mirroring the
// tea.KeyMsg construction used elsewhere in this package (see keyMsg).
func dispatchKey(m *Model, s string) {
	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)})
}

// sizeDispatchModel gives the model a real size the way other overlay tests
// do (see capabilities_test.go / tasks_test.go): the renderOverlay box-width
// math assumes a nonzero m.width.
func sizeDispatchModel(m *Model) {
	m.SetSize(120, 40)
}

func TestDispatchManagerFromProjectsPane(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	m.projectScope = "ATM"
	m.focused = paneProjects
	sizeDispatchModel(m)

	fd := &fakeDispatcher{preview: "tmux · new window"}
	m.dispatcher = fd
	m.agentOptionsFn = testAgents

	dispatchKey(m, "D")
	if !m.dispatchDlg.active {
		t.Fatal("D on projects pane must open the dialog")
	}
	if m.dispatchDlg.persona() != "manager" {
		t.Fatalf("persona = %q, want manager", m.dispatchDlg.persona())
	}
	view := m.dispatchDlg.renderOverlay()
	for _, want := range []string{"Persona:", "manager", "claude", "tmux · new window"} {
		if !strings.Contains(view, want) {
			t.Errorf("overlay missing %q:\n%s", want, view)
		}
	}

	m.dispatchDlg.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if len(fd.spawned) != 1 {
		t.Fatal("enter on ready agent must spawn")
	}
	got := fd.spawned[0]
	wantArgv := []string{"atm", "--persona", "manager", "--project", "ATM", "--agent", "claude"}
	if strings.Join(got.Argv, " ") != strings.Join(wantArgv, " ") {
		t.Errorf("argv = %v, want %v", got.Argv, wantArgv)
	}
	if got.Title != "ATM · manager" {
		t.Errorf("title = %q, want ATM · manager", got.Title)
	}
	if m.dispatchDlg.active {
		t.Error("dialog must close after dispatch")
	}
}

func TestDispatchDeveloperFromTaskRow(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	m.projectScope = "ATM"
	task, err := m.store.CreateTask("ATM", "dispatch work", "", nil, testActor)
	if err != nil {
		t.Fatal(err)
	}
	m.refreshAll()
	m.focused = paneTasks
	sizeDispatchModel(m)

	fd := &fakeDispatcher{preview: "herdr · new pane"}
	m.dispatcher = fd
	m.agentOptionsFn = testAgents

	dispatchKey(m, "D")
	if !m.dispatchDlg.active {
		t.Fatal("D on tasks pane must open the dialog")
	}
	if m.dispatchDlg.persona() != "developer" {
		t.Fatalf("persona = %q, want developer", m.dispatchDlg.persona())
	}
	if m.dispatchDlg.taskID != task.ID {
		t.Fatalf("task = %q, want %q", m.dispatchDlg.taskID, task.ID)
	}
	m.dispatchDlg.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if len(fd.spawned) != 1 {
		t.Fatal("must spawn")
	}
	argv := strings.Join(fd.spawned[0].Argv, " ")
	if !strings.Contains(argv, "--persona developer") || !strings.Contains(argv, "--task "+task.ID) {
		t.Errorf("argv = %s", argv)
	}
	if want := task.ID; fd.spawned[0].Title != want {
		t.Errorf("title = %q, want %q", fd.spawned[0].Title, want)
	}
}

func TestDispatchUnreadyAgentRefused(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	m.projectScope = "ATM"
	m.focused = paneProjects
	sizeDispatchModel(m)

	fd := &fakeDispatcher{preview: "tmux · new window"}
	m.dispatcher = fd
	m.agentOptionsFn = testAgents

	dispatchKey(m, "D")
	m.dispatchDlg.handleKey(tea.KeyMsg{Type: tea.KeyRight}) // move to codex (unready)
	m.dispatchDlg.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if len(fd.spawned) != 0 {
		t.Fatal("unready agent must not spawn")
	}
	if !strings.Contains(m.toastMsg, "not ready") {
		t.Errorf("toast = %q, want not-ready error", m.toastMsg)
	}
	if !m.dispatchDlg.active {
		t.Error("dialog must stay open after refusal")
	}
}

func TestDispatchNoTargetDisables(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	m.projectScope = "ATM"
	m.focused = paneProjects
	sizeDispatchModel(m)

	m.dispatcher = &fakeDispatcher{previewErr: errors.New(`no dispatch target: not inside herdr or tmux and no known terminal detected — set "terminal_cmd" in dispatch.json at the store root`)}
	m.agentOptionsFn = testAgents

	dispatchKey(m, "D")
	view := m.dispatchDlg.renderOverlay()
	if !strings.Contains(view, "no dispatch target") {
		t.Errorf("overlay must show detection error:\n%s", view)
	}
	m.dispatchDlg.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if len(m.dispatcher.(*fakeDispatcher).spawned) != 0 {
		t.Fatal("enter with no target must not spawn")
	}
}

func TestDispatchDeveloperWithRepoSpawnsIntoRepoPath(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	m.projectScope = "ATM"
	repoDir := t.TempDir()
	if err := m.store.SetProjectRepo("ATM", "main", repoDir, "https://example.com/atm.git", testActor); err != nil {
		t.Fatal(err)
	}
	task, err := m.store.CreateTask("ATM", "dispatch work", "", nil, testActor)
	if err != nil {
		t.Fatal(err)
	}
	m.refreshAll()
	m.focused = paneTasks
	sizeDispatchModel(m)

	fd := &fakeDispatcher{preview: "herdr · new pane"}
	m.dispatcher = fd
	m.agentOptionsFn = testAgents

	dispatchKey(m, "D")
	if m.dispatchDlg.persona() != "developer" {
		t.Fatal("D on tasks pane must default to developer")
	}
	if len(m.dispatchDlg.repos) != 1 || m.dispatchDlg.repos[0].Path != repoDir {
		t.Fatalf("repos = %+v, want one main -> %s", m.dispatchDlg.repos, repoDir)
	}
	view := m.dispatchDlg.renderOverlay()
	if !strings.Contains(view, "Repo:") || !strings.Contains(view, "main") {
		t.Errorf("overlay must show Repo: line with the repo name:\n%s", view)
	}

	m.dispatchDlg.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if len(fd.spawned) != 1 {
		t.Fatal("must spawn")
	}
	if fd.spawned[0].Dir != repoDir {
		t.Errorf("Spec.Dir = %q, want repo path %q", fd.spawned[0].Dir, repoDir)
	}
	_ = task
}

func TestDispatchDeveloperNoRepoFallsBackToCwd(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	m.projectScope = "ATM"
	task, err := m.store.CreateTask("ATM", "dispatch work", "", nil, testActor)
	if err != nil {
		t.Fatal(err)
	}
	m.refreshAll()
	m.focused = paneTasks
	sizeDispatchModel(m)

	fd := &fakeDispatcher{preview: "herdr · new pane"}
	m.dispatcher = fd
	m.agentOptionsFn = testAgents

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}

	dispatchKey(m, "D")
	if m.dispatchDlg.persona() != "developer" {
		t.Fatal("D on tasks pane must default to developer")
	}
	if len(m.dispatchDlg.repos) != 0 {
		t.Fatalf("repos = %+v, want empty", m.dispatchDlg.repos)
	}
	view := m.dispatchDlg.renderOverlay()
	if !strings.Contains(view, "Repo:") || !strings.Contains(view, "(cwd)") {
		t.Errorf("overlay must show Repo: (cwd) when no repos recorded:\n%s", view)
	}
	m.dispatchDlg.handleKey(tea.KeyMsg{Type: tea.KeyDown})
	m.dispatchDlg.handleKey(tea.KeyMsg{Type: tea.KeyUp})
	if m.dispatchDlg.repoCursor != 0 {
		t.Errorf("repoCursor = %d, want 0 (no-op with empty repos)", m.dispatchDlg.repoCursor)
	}

	m.dispatchDlg.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if len(fd.spawned) != 1 {
		t.Fatal("must spawn")
	}
	if fd.spawned[0].Dir != cwd {
		t.Errorf("Spec.Dir = %q, want cwd %q", fd.spawned[0].Dir, cwd)
	}
	_ = task
}

func TestDispatchDeveloperRepoCyclePicker(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	m.projectScope = "ATM"
	d1, d2 := t.TempDir(), t.TempDir()
	if err := m.store.SetProjectRepo("ATM", "main", d1, "", testActor); err != nil {
		t.Fatal(err)
	}
	if err := m.store.SetProjectRepo("ATM", "docs", d2, "", testActor); err != nil {
		t.Fatal(err)
	}
	task, err := m.store.CreateTask("ATM", "dispatch work", "", nil, testActor)
	if err != nil {
		t.Fatal(err)
	}
	m.refreshAll()
	m.focused = paneTasks
	sizeDispatchModel(m)

	fd := &fakeDispatcher{preview: "herdr · new pane"}
	m.dispatcher = fd
	m.agentOptionsFn = testAgents

	dispatchKey(m, "D")
	if len(m.dispatchDlg.repos) != 2 || m.dispatchDlg.repoCursor != 0 {
		t.Fatalf("repos = %+v cursor = %d, want 2 repos cursor 0", m.dispatchDlg.repos, m.dispatchDlg.repoCursor)
	}
	m.dispatchDlg.handleKey(tea.KeyMsg{Type: tea.KeyDown})
	if m.dispatchDlg.repoCursor != 1 {
		t.Fatalf("repoCursor = %d, want 1 after down", m.dispatchDlg.repoCursor)
	}
	view := m.dispatchDlg.renderOverlay()
	if !strings.Contains(view, "docs") {
		t.Errorf("overlay must show second repo name after down:\n%s", view)
	}
	m.dispatchDlg.handleKey(tea.KeyMsg{Type: tea.KeyUp})
	if m.dispatchDlg.repoCursor != 0 {
		t.Fatalf("repoCursor = %d, want 0 after up", m.dispatchDlg.repoCursor)
	}

	m.dispatchDlg.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if len(fd.spawned) != 1 {
		t.Fatal("must spawn")
	}
	if fd.spawned[0].Dir != d1 {
		t.Errorf("Spec.Dir = %q, want first repo %q", fd.spawned[0].Dir, d1)
	}
	_ = task
}

// TestDispatchManagerShowsRepoWhenProjectPresent replaces the old
// "manager must not show a Repo line" guard: with the universal dialog the
// Repo picker appears for any persona whenever a project is present.
func TestDispatchManagerShowsRepoWhenProjectPresent(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	m.projectScope = "ATM"
	repoDir := t.TempDir()
	if err := m.store.SetProjectRepo("ATM", "main", repoDir, "", testActor); err != nil {
		t.Fatal(err)
	}
	m.focused = paneProjects
	sizeDispatchModel(m)

	fd := &fakeDispatcher{preview: "tmux · new window"}
	m.dispatcher = fd
	m.agentOptionsFn = testAgents

	dispatchKey(m, "D")
	if m.dispatchDlg.persona() != "manager" {
		t.Fatalf("persona = %q want manager", m.dispatchDlg.persona())
	}
	view := m.dispatchDlg.renderOverlay()
	if !strings.Contains(view, "Repo:") || !strings.Contains(view, "main") {
		t.Errorf("manager dialog must show Repo line when a project is present:\n%s", view)
	}
	m.dispatchDlg.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if len(fd.spawned) != 1 {
		t.Fatal("must spawn")
	}
	if fd.spawned[0].Dir != repoDir {
		t.Errorf("Spec.Dir = %q, want repo %q", fd.spawned[0].Dir, repoDir)
	}
}

func TestDispatchPersonaCycle(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	m.projectScope = "ATM"
	m.focused = paneProjects
	sizeDispatchModel(m)
	m.dispatcher = &fakeDispatcher{preview: "tmux"}
	m.agentOptionsFn = testAgents

	dispatchKey(m, "D")
	first := m.dispatchDlg.persona()
	m.dispatchDlg.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("p")})
	if m.dispatchDlg.persona() == first {
		t.Fatal("p must change the persona")
	}
}

func TestDispatchProjectRequiredNoScopeRefuses(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	m.SetSize(100, 30)
	fd := &fakeDispatcher{preview: "tmux"}
	m.dispatcher = fd
	m.agentOptionsFn = testAgents
	m.dispatchDlg.m = m

	m.dispatchDlg.open("manager", "", "", "")
	if m.dispatchDlg.persona() != "manager" {
		t.Fatalf("persona = %q want manager", m.dispatchDlg.persona())
	}
	view := m.dispatchDlg.renderOverlay()
	if !strings.Contains(view, "requires a project scope") {
		t.Errorf("overlay must show the no-scope warning:\n%s", view)
	}
	m.dispatchDlg.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if len(fd.spawned) != 0 {
		t.Fatal("must not spawn without a project")
	}
	if !strings.Contains(m.toastMsg, "requires a project scope") {
		t.Errorf("toast = %q, want project-scope error", m.toastMsg)
	}
	if !m.dispatchDlg.active {
		t.Error("dialog must stay open")
	}
}

func TestDispatchUnknownDefaultFallsBackToConcierge(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(100, 30)
	m.dispatcher = &fakeDispatcher{preview: "tmux"}
	m.agentOptionsFn = testAgents
	m.dispatchDlg.m = m

	m.dispatchDlg.open("ghost", "", "", "")
	if m.dispatchDlg.persona() != "concierge" {
		t.Fatalf("persona = %q, want concierge fallback", m.dispatchDlg.persona())
	}
}

func TestDispatchTaskPersistsAcrossPersonaSwitch(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	m.projectScope = "ATM"
	task, err := m.store.CreateTask("ATM", "dispatch work", "", nil, testActor)
	if err != nil {
		t.Fatal(err)
	}
	m.refreshAll()
	m.focused = paneTasks
	sizeDispatchModel(m)
	fd := &fakeDispatcher{preview: "herdr"}
	m.dispatcher = fd
	m.agentOptionsFn = testAgents

	dispatchKey(m, "D")
	if m.dispatchDlg.persona() != "developer" {
		t.Fatalf("persona = %q want developer", m.dispatchDlg.persona())
	}
	for i := 0; i < len(m.dispatchDlg.personas); i++ {
		m.dispatchDlg.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("p")})
	}
	if m.dispatchDlg.persona() != "developer" {
		t.Fatalf("persona = %q, want developer after full cycle", m.dispatchDlg.persona())
	}
	if m.dispatchDlg.taskID != task.ID {
		t.Fatalf("task = %q, want %q after persona cycle", m.dispatchDlg.taskID, task.ID)
	}
	m.dispatchDlg.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if len(fd.spawned) != 1 {
		t.Fatal("must spawn")
	}
	argv := strings.Join(fd.spawned[0].Argv, " ")
	if !strings.Contains(argv, "--persona developer") || !strings.Contains(argv, "--task "+task.ID) {
		t.Errorf("argv = %s", argv)
	}
}

func TestDispatchConciergeOmitsProject(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	m.SetSize(100, 30)
	fd := &fakeDispatcher{preview: "tmux · new window"}
	m.dispatcher = fd
	m.agentOptionsFn = testAgents
	m.dispatchDlg.m = m

	m.dispatchDlg.open("concierge", "", "", "")
	if m.dispatchDlg.persona() != "concierge" {
		t.Fatalf("persona = %q want concierge", m.dispatchDlg.persona())
	}
	m.dispatchDlg.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if len(fd.spawned) != 1 {
		t.Fatal("concierge should spawn")
	}
	argv := strings.Join(fd.spawned[0].Argv, " ")
	if strings.Contains(argv, "--project") {
		t.Errorf("concierge argv must omit --project: %s", argv)
	}
	if !strings.Contains(argv, "--persona concierge") {
		t.Errorf("concierge argv must set --persona concierge: %s", argv)
	}
}

func TestDispatchAdminOpensTUI(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(100, 30)
	fd := &fakeDispatcher{preview: "tmux · new window"}
	m.dispatcher = fd
	m.agentOptionsFn = testAgents
	m.dispatchDlg.m = m

	m.dispatchDlg.open("admin", "ATM", "", "")
	if m.dispatchDlg.persona() != "admin" {
		t.Fatalf("persona = %q want admin", m.dispatchDlg.persona())
	}
	// admin is not gated on agent readiness: move to the unready codex and
	// dispatch anyway.
	m.dispatchDlg.handleKey(tea.KeyMsg{Type: tea.KeyRight})
	m.dispatchDlg.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if len(fd.spawned) != 1 {
		t.Fatal("admin should spawn even with an unready agent")
	}
	argv := strings.Join(fd.spawned[0].Argv, " ")
	if !strings.Contains(argv, "--persona admin") {
		t.Errorf("admin argv must set --persona admin: %s", argv)
	}
	if strings.Contains(argv, "--project") || strings.Contains(argv, "--task") {
		t.Errorf("admin argv must omit --project/--task: %s", argv)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/tui/ -run TestDispatch -v`
Expected: FAIL — compile errors referencing `dispatchKind`, `dispatchManager`, `dispatchNone`, `.kind`, and the old `open(kind, ...)` signature.

- [ ] **Step 3: Rewrite `internal/tui/dispatch.go`**

Replace the entire file with:

```go
package tui

import (
	"os"
	"os/exec"
	"strings"

	"atm/internal/agent"
	"atm/internal/core"
	"atm/internal/dispatch"

	tea "github.com/charmbracelet/bubbletea"
)

// Dispatcher is the TUI-facing dispatch port; *dispatch.Service implements
// it. nil disables dispatch with a clear error in the dialog.
type Dispatcher interface {
	Preview() (string, error)
	PreviewTarget(string) (string, error)
	Spawn(dispatch.Spec) error
}

type agentOption struct {
	name  string
	ready bool
	hint  string
}

// agentOptions snapshots the catalog with readiness; swapped in tests via
// Model.agentOptionsFn.
func agentOptions() []agentOption {
	home, _ := os.UserHomeDir()
	var out []agentOption
	for _, e := range agent.Catalog() {
		r := agent.Status(e, home, exec.LookPath)
		out = append(out, agentOption{name: e.Name, ready: r.Ready(), hint: r.String()})
	}
	return out
}

// dispatchModel is the universal dispatch dialog overlay (pattern:
// capabilityModel). Persona is a selectable field cycling over every store
// persona; context only preselects persona/project/task defaults at open.
type dispatchModel struct {
	m             *Model
	active        bool
	personas      []*core.Persona
	personaCursor int
	project       string
	taskID        string
	taskTitle     string
	agents        []agentOption
	cursor        int
	targets       []string
	targetCursor  int
	preview       string
	previewErr    string
	repos         []core.RepoConfig
	repoCursor    int
}

// selectedPersona returns the persona under the cursor, or nil.
func (d *dispatchModel) selectedPersona() *core.Persona {
	if d.personaCursor < 0 || d.personaCursor >= len(d.personas) {
		return nil
	}
	return d.personas[d.personaCursor]
}

func (d *dispatchModel) persona() string {
	if p := d.selectedPersona(); p != nil {
		return p.Name
	}
	return ""
}

// projectRequired reports whether the selected persona needs --project in its
// argv. Derived from the persona's project_optional spec; admin is always
// project-optional because --persona admin routes to a fresh TUI that ignores
// --project.
func (d *dispatchModel) projectRequired() bool {
	p := d.selectedPersona()
	if p == nil {
		return false
	}
	if p.Name == "admin" {
		return false
	}
	return !p.ProjectOptional
}

func (d *dispatchModel) target() string {
	if d.targetCursor == 0 {
		return ""
	}
	return d.targets[d.targetCursor]
}

func (d *dispatchModel) title() string {
	if d.taskID != "" {
		return d.taskID
	}
	if d.project != "" && d.persona() != "" {
		return d.project + " · " + d.persona()
	}
	return d.persona()
}

// repoLabel renders the Repo: line's value: the selected repo's path, or
// "(cwd)" when no repos are recorded. Paths are truncated to the box's inner
// width with fitLine so a long path cannot widen the dialog.
func (d *dispatchModel) repoLabel() string {
	if len(d.repos) == 0 {
		return "‹ (cwd) ›"
	}
	r := d.repos[d.repoCursor]
	label := r.Path
	if r.Name != "" {
		label = r.Name + " · " + r.Path
	}
	return "‹ " + fitLine(label, bwInner(d.m.width)) + " ›"
}

// bwInner returns the inner text width of the dispatch dialog box for the
// given terminal width, mirroring renderOverlay's box-width math so a long
// repo path truncates consistently with the task title.
func bwInner(width int) int {
	bw := width * 60 / 100
	if bw < 64 {
		bw = 64
	}
	if bw > width-4 {
		bw = width - 4
	}
	return bw - 4
}

// open preselects the given default persona (falling back to concierge when
// it is not in the store list), sets the context defaults, and refreshes the
// target preview. Dispatch logic never branches on how it was opened.
func (d *dispatchModel) open(defaultPersona, project, taskID, taskTitle string) {
	d.project, d.taskID, d.taskTitle = project, taskID, taskTitle
	d.personas = d.m.store.ListPersonas()
	d.personaCursor = 0
	for i, p := range d.personas {
		if p.Name == defaultPersona {
			d.personaCursor = i
			break
		}
	}
	if d.persona() != defaultPersona {
		// default not found: preselect concierge (project-optional, always
		// dispatchable) so the dialog always opens with a usable persona.
		for i, p := range d.personas {
			if p.Name == "concierge" {
				d.personaCursor = i
				break
			}
		}
	}
	d.agents = d.m.agentOptionsFn()
	d.cursor = 0
	for i, a := range d.agents { // preselect the first ready agent
		if a.ready {
			d.cursor = i
			break
		}
	}
	d.targets = []string{"auto", "herdr", "tmux", "terminal"}
	d.targetCursor = 0
	d.preview, d.previewErr = "", ""
	d.repos, d.repoCursor = nil, 0
	if project != "" {
		if repos, err := d.m.store.ProjectRepos(project); err == nil {
			d.repos = repos
		}
	}
	d.active = true
	if d.m.dispatcher == nil {
		d.previewErr = "dispatch unavailable in this build"
		return
	}
	d.refreshPreview()
}

func (d *dispatchModel) refreshPreview() {
	d.preview, d.previewErr = "", ""
	target := ""
	if d.targetCursor > 0 {
		target = d.targets[d.targetCursor]
	}
	p, err := d.m.dispatcher.PreviewTarget(target)
	if err != nil {
		d.previewErr = err.Error()
	} else {
		d.preview = p
	}
}

func (d *dispatchModel) handleKey(k tea.KeyMsg) tea.Cmd {
	switch k.String() {
	case "esc":
		d.active = false
	case "p":
		if len(d.personas) > 0 {
			d.personaCursor = (d.personaCursor + 1) % len(d.personas)
		}
	case "left", "h":
		if d.cursor > 0 {
			d.cursor--
		}
	case "right", "l":
		if d.cursor < len(d.agents)-1 {
			d.cursor++
		}
	case "down", "j":
		if len(d.repos) > 0 {
			d.repoCursor = (d.repoCursor + 1) % len(d.repos)
		}
	case "up", "k":
		if len(d.repos) > 0 {
			d.repoCursor = (d.repoCursor - 1 + len(d.repos)) % len(d.repos)
		}
	case "t":
		d.targetCursor = (d.targetCursor + 1) % len(d.targets)
		d.refreshPreview()
	case "enter":
		d.submit()
	}
	return nil
}

func (d *dispatchModel) submit() {
	if d.previewErr != "" {
		d.m.showToast("error: " + d.previewErr)
		return
	}
	p := d.selectedPersona()
	if p == nil {
		d.m.showToast("error: no personas available")
		return
	}
	if d.projectRequired() && d.project == "" {
		d.m.showToast("error: persona " + p.Name + " requires a project scope")
		return
	}
	if len(d.agents) == 0 {
		d.m.showToast("error: agent catalog is empty")
		return
	}
	a := d.agents[d.cursor]
	if p.Name != "admin" && !a.ready {
		d.m.showToast("error: agent " + a.name + " not ready: " + a.hint)
		return
	}
	argv := []string{"atm", "--persona", p.Name}
	if d.projectRequired() {
		argv = append(argv, "--project", d.project)
	}
	if p.Name != "admin" {
		argv = append(argv, "--agent", a.name)
	}
	// --task rides only with --project: the CLI launcher rejects
	// "--task requires --project", so a task is only passed when the selected
	// persona will receive --project in the same argv.
	if d.taskID != "" && d.projectRequired() {
		argv = append(argv, "--task", d.taskID)
	}
	dir, err := os.Getwd()
	if err != nil {
		d.m.showToast("error: " + err.Error())
		return
	}
	if len(d.repos) > 0 {
		dir = d.repos[d.repoCursor].Path
	}
	if err := d.m.dispatcher.Spawn(dispatch.Spec{Title: d.title(), Argv: argv, Dir: dir, Target: d.target()}); err != nil {
		d.m.showToast("error: " + err.Error())
		return
	}
	d.m.showToast("dispatched " + p.Name + " → " + d.preview)
	d.active = false
}

// renderOverlay draws the dialog. Box construction mirrors
// capabilityModel.renderOverlay (titledBoxHeight + styles.DialogBody) — reuse
// the same helpers and width conventions found there. The persona description,
// taskTitle, and repo path hints are truncated to the box's inner width with
// fitLine so a long value cannot widen the dialog.
func (d *dispatchModel) renderOverlay() string {
	styles := d.m.styles

	// Box width mirrors capabilityModel.renderOverlay's computation; it is
	// computed before the content lines so the truncations below can use the
	// inner width.
	bw := d.m.width * 60 / 100
	if bw < 64 {
		bw = 64
	}
	if bw > d.m.width-4 {
		bw = d.m.width - 4
	}

	var b strings.Builder
	if p := d.selectedPersona(); p != nil {
		b.WriteString("Persona: ‹ " + p.Name + " ›\n")
		b.WriteString(styles.FieldHint.Render("        "+fitLine(p.Description, bw-10)) + "\n\n")
	}
	if d.taskID != "" {
		b.WriteString("Task:   " + d.taskID + "\n")
		b.WriteString(styles.FieldHint.Render("        "+fitLine(d.taskTitle, bw-10)) + "\n\n")
	}
	if d.project != "" {
		b.WriteString("Repo:   " + d.repoLabel() + "\n\n")
	}
	a := agentOption{name: "—"}
	if len(d.agents) > 0 {
		a = d.agents[d.cursor]
	}
	b.WriteString("Agent:  ‹ " + a.name + " ›\n")
	if a.ready || d.persona() == "admin" {
		b.WriteString(styles.Success.Render("        ready") + "\n\n")
	} else {
		b.WriteString(styles.Error.Render("        x "+a.hint) + "\n\n")
	}
	if d.previewErr != "" {
		b.WriteString(styles.Error.Render("Target: x "+d.previewErr) + "\n")
	} else {
		b.WriteString("Target: " + d.targets[d.targetCursor] + " · " + d.preview + " \"" + d.title() + "\"\n")
	}
	if d.projectRequired() && d.project == "" {
		b.WriteString(styles.Error.Render("⚠ " + d.persona() + " requires a project scope") + "\n")
	}
	help := "[p]persona  [←/→]agent  [t]target  [Enter]dispatch  [Esc]close"
	if d.project != "" {
		help = "[p]persona  [←/→]agent  [↑/↓]repo  [t]target  [Enter]dispatch  [Esc]close"
	}
	b.WriteString("\n" + styles.KeyMenuDim.Render(help))

	bh := strings.Count(b.String(), "\n") + 3
	return titledBoxHeight(styles.DialogBody, bw, "Dispatch", b.String(), bh)
}
```

- [ ] **Step 4: Update `internal/tui/app.go` callers**

Change line 399 (`workspaceIdle`):

```go
		m.dispatchDlg.active &&
```

Change line 560:

```go
	if m.dispatchDlg.active {
		return m.dispatchDlg.handleKey(k)
	}
```

Change line 933:

```go
	if m.dispatchDlg.active {
		out = m.placeOverlay(out, m.dispatchDlg.renderOverlay())
	}
```

Replace the `case "D":` block (lines 649-676) — this task keeps the existing
conditional defaults but passes persona names:

```go
	case "D":
		// Drilled persona chart takes precedence; otherwise a project row
		// dispatches manager and a task row dispatches developer-on-task.
		if m.focused == paneProjects {
			if m.projects.personaDrilled && m.projects.personaCursor < len(m.projects.personaGroups) {
				return m.projects.openDispatchForPersona(m.projects.personaGroups[m.projects.personaCursor].Key)
			}
			if row, ok := m.projects.selected(); ok {
				m.dispatchDlg.open("manager", row.code, "", "")
			}
			return nil
		}
		if m.focused == paneTasks {
			if r, ok := m.tasks.selectedRow(); ok {
				project := m.projectScope
				if r.task != nil && r.task.ProjectCode != "" {
					project = r.task.ProjectCode
				}
				if project == "" {
					m.showToast("error: no project scope for dispatch")
					return nil
				}
				m.dispatchDlg.open("developer", project, r.id, r.title)
			}
			return nil
		}
```

- [ ] **Step 5: Update `internal/tui/projects.go`**

Replace `openDispatchForPersona` (lines 333-350) with a name lookup — no kind
mapping, no manager fallback (the dialog falls back to concierge itself):

```go
// openDispatchForPersona opens the dispatch dialog with the given persona
// preselected over the current project scope. Unknown personas fall back to
// concierge inside the dialog.
func (p *projectsModel) openDispatchForPersona(persona string) tea.Cmd {
	p.m.dispatchDlg.open(persona, p.m.projectScope, "", "")
	return nil
}
```

- [ ] **Step 6: Update `internal/tui/actors_test.go`**

Replace `TestPersonaChartDDispatchesWhenDrilled` (lines 119-138):

```go
func TestPersonaChartDDispatchesWhenDrilled(t *testing.T) {
	m := mkActorsOverlayTestModel(t)
	m.SetSize(100, 40)
	m.projectScope = "ATM"
	m.focused = paneProjects
	fd := &fakeDispatcher{preview: "tmux · new window"}
	m.dispatcher = fd
	m.agentOptionsFn = testAgents

	update(t, m, "ctrl+right") // drill into persona 0
	if !m.projects.personaDrilled {
		t.Fatal("ctrl+right should drill in")
	}
	update(t, m, "d")
	if !m.dispatchDlg.active {
		t.Fatal("D should open the dispatch dialog")
	}
	if got := m.dispatchDlg.persona(); got != "staff" {
		t.Fatalf("D should preselect the drilled persona (staff), got %q", got)
	}
}
```

Delete the old `TestDispatchConciergeOmitsProject` from this file (lines
180-206) — it moved to `dispatch_test.go`.

- [ ] **Step 7: Update `internal/tui/art_wiring_test.go`**

In the `"dispatch dialog"` subtest (line 85-91):

```go
	t.Run("dispatch dialog", func(t *testing.T) {
		m := fresh(t)
		m.dispatchDlg.active = true
		if m.workspaceIdle() {
			t.Fatal("workspaceIdle should be false with the dispatch dialog open")
		}
	})
```

- [ ] **Step 8: Run the TUI tests to verify they pass**

Run: `go test ./internal/tui/ -run TestDispatch -v`
Expected: PASS.

Then the full TUI suite:
`go test ./internal/tui/...`
Expected: PASS.

- [ ] **Step 9: Commit**

```bash
git add internal/tui/dispatch.go internal/tui/dispatch_test.go internal/tui/app.go internal/tui/projects.go internal/tui/actors_test.go internal/tui/art_wiring_test.go
git commit -m "feat(ATM-29f8b0): universal dispatch dialog with persona field"
```

---

### Task 3: Universal `D` handler — always opens, concierge fallback

**Files:**
- Modify: `internal/tui/app.go:649-676` (`case "D":` block)
- Modify: `internal/tui/keymap.go:57`
- Test: `internal/tui/dispatch_test.go` (append) and `internal/tui/app_test.go` (append)

**Interfaces:**
- Consumes: `dispatchModel.open(defaultPersona, project, taskID, taskTitle)` (Task 2), `projectsModel.personaDrilled/personaCursor/personaGroups`, `tasksModel.selectedRow()`, `projectsModel.selected()`.
- Produces: `Model.openDispatch()` — resolves the current pane/selection into (default persona, project, task) and calls `open()`. The "no project scope for dispatch" toast is removed.

- [ ] **Step 1: Write the failing tests first**

Append to `internal/tui/dispatch_test.go`:

```go
func TestDispatchDOpensFromTasksPaneWithoutTask(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	m.projectScope = "ATM"
	m.focused = paneTasks // no tasks seeded, no row selected
	sizeDispatchModel(m)
	m.dispatcher = &fakeDispatcher{preview: "tmux"}
	m.agentOptionsFn = testAgents

	dispatchKey(m, "D")
	if !m.dispatchDlg.active {
		t.Fatal("D must open the dialog on the tasks pane with no task")
	}
	if m.dispatchDlg.persona() != "concierge" {
		t.Fatalf("persona = %q, want concierge fallback", m.dispatchDlg.persona())
	}
}

func TestDispatchDOpensFromEmptyWorkspace(t *testing.T) {
	m := newTestModel(t)
	sizeDispatchModel(m)
	m.dispatcher = &fakeDispatcher{preview: "tmux"}
	m.agentOptionsFn = testAgents

	dispatchKey(m, "D")
	if !m.dispatchDlg.active {
		t.Fatal("D must open the dialog from an empty workspace")
	}
	if m.dispatchDlg.persona() != "concierge" {
		t.Fatalf("persona = %q, want concierge fallback", m.dispatchDlg.persona())
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/tui/ -run 'TestDispatchDOpensFrom' -v`
Expected: FAIL — the tasks-pane test opens a developer dialog with no project
(then refuses); the empty-workspace test does not open at all.

- [ ] **Step 3: Add `Model.openDispatch()` and make the D handler universal**

Replace the `case "D":` block from Task 2 in `internal/tui/app.go` with:

```go
	case "D":
		m.openDispatch()
		return nil
```

Add the method near the other pane key handlers (e.g. after `handleKey`):

```go
// openDispatch opens the universal dispatch dialog, resolving the current
// pane/selection into persona, project, and task defaults. Context never
// changes dispatch logic — it only preselects. With no selection the dialog
// still opens, defaulting to concierge (the one built-in usable without a
// project).
func (m *Model) openDispatch() {
	persona, project, taskID, taskTitle := "concierge", m.projectScope, "", ""
	switch {
	case m.focused == paneProjects && m.projects.personaDrilled && m.projects.personaCursor < len(m.projects.personaGroups):
		persona = m.projects.personaGroups[m.projects.personaCursor].Key
	case m.focused == paneProjects:
		if row, ok := m.projects.selected(); ok {
			persona, project = "manager", row.code
		}
	case m.focused == paneTasks:
		if r, ok := m.tasks.selectedRow(); ok {
			persona, taskID, taskTitle = "developer", r.id, r.title
			if r.task != nil && r.task.ProjectCode != "" {
				project = r.task.ProjectCode
			}
		}
	}
	m.dispatchDlg.open(persona, project, taskID, taskTitle)
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/tui/ -run 'TestDispatch|TestPersonaChartDDispatchesWhenDrilled' -v`
Expected: PASS.

Full TUI suite:
`go test ./internal/tui/...`
Expected: PASS.

- [ ] **Step 5: Update the keymap row**

In `internal/tui/keymap.go`, change line 57 to:

```go
	{"D", "dispatch (persona picker)", "dispatch (persona picker)", "-", "-"},
```

- [ ] **Step 6: Verify help/status hints need no edits**

The help overlay's Global Keymap table renders `keymapRows` (help.go:52), so
updating `keymap.go:57` in Step 5 updates the help overlay automatically — no
`help.go` change needed. The `conventionsTextTUI` and `parityTable` do not
mention dispatch keys. The persona-chart status hints
(`projects.go:965` `[D]dispatch`, `projects.go:1352`
`[Ctrl+Shift+→]dispatch`) remain accurate because D still dispatches (now via
the universal dialog). Run the existing hint assertions to confirm they still
pass:

Run: `go test ./internal/tui/ -run 'TestProjectsStatusHintMentionsPersonaKeys|TestPersonaChart' -v`
Expected: PASS. No edits required.

- [ ] **Step 7: Commit**

```bash
git add internal/tui/app.go internal/tui/keymap.go internal/tui/dispatch_test.go
git commit -m "feat(ATM-29f8b0): D opens the dispatch dialog from any pane"
```

---

### Task 4: Docs, CHANGELOG, spec amendment, ledger

**Files:**
- Modify: `README.md:44-49` and `:57-67`
- Modify: `CHANGELOG.md` (Unreleased section)
- Modify: `docs/superpowers/specs/2026-08-13-universal-dispatch-dialog-design.md` (submit bullet refinement)
- Modify: `docs/superpowers/specs/2026-07-23-tui-agent-dispatch-design.md` (superseded note)

- [ ] **Step 1: Update the README dispatch section**

Replace the dispatch bullets (lines 46-49) with:

```markdown
- **Onboard**: press `D` anywhere to open the dispatch dialog, press `p` to cycle to **concierge**, and dispatch a plain-language onboarding session that creates your project, enables the right capabilities, and seeds their vocabulary.
- **Autopilot**: select a project, press `D` (it preselects **manager**), and dispatch a session that grooms the backlog, converges the enabled capabilities, and briefs you on what's next.
- **Work a task**: select a task and press `D` to dispatch a **developer** session bound to it — no re-explaining the context. Cycle the persona with `p`, the host agent with `←/→`, the repo to spawn into with `↑/↓`, the spawn target with `t` (herdr pane, tmux window, or terminal tab), then `Enter` launches it.
- **Explore**: `V` browses personas, `?` lists every keybinding.
```

Update the dispatch screenshot captions (lines 57-67):

```markdown
![Dispatch dialog with persona, agent, repo, and spawn target](docs/assets/screenshots/atm-dispatch-developer.png)

The universal dispatch dialog: pick a persona with `p`, an agent with `←/→`, a repo with `↑/↓` (when a project is in scope), a spawn target with `t`, then `Enter` launches it.
```

(Keep the existing `atm-dispatch-manager.png` reference and its caption; remove
the "developer/manager dialog" framing if it no longer matches.)

- [ ] **Step 2: Add a CHANGELOG entry**

At the top of the `## Unreleased` section add:

```markdown
- ATM-29f8b0: the TUI dispatch dialog is now universal. `D` opens it from any
  pane (even an empty workspace); the persona is a selectable field (`p`) over
  every store persona instead of being fixed by the opening context. Context
  only preselects the persona, project, and task. A persona that requires a
  project shows an inline warning and refuses to dispatch when no project is in
  scope. Concierge (and other project-optional personas) are now reachable from
  the TUI on a fresh project with no activity.
```

- [ ] **Step 3: Refine the spec's submit bullet**

In `docs/superpowers/specs/2026-08-13-universal-dispatch-dialog-design.md`,
replace the `submit()` bullet's `--task` clause:

```markdown
    `--project` only when `projectRequired()`, `--task` only when a task is
    bound AND the dispatch includes `--project` (the CLI launcher rejects
    `--task` without `--project`);
```

- [ ] **Step 4: Note supersession in the old dispatch spec**

Append to the "Decisions of record" list in
`docs/superpowers/specs/2026-07-23-tui-agent-dispatch-design.md`:

```markdown
- **Superseded (2026-08-13):** the "Persona is fixed per trigger" decision and
  the rejected "one generic dispatch form with a persona picker" are replaced
  by the universal dispatch dialog — see
  `2026-08-13-universal-dispatch-dialog-design.md`.
```

- [ ] **Step 5: Verify**

Run: `make verify`
Expected: build succeeds, all tests pass, scripts-test passes.

- [ ] **Step 6: Commit**

```bash
git add README.md CHANGELOG.md docs/superpowers/specs/2026-08-13-universal-dispatch-dialog-design.md docs/superpowers/specs/2026-07-23-tui-agent-dispatch-design.md
git commit -m "docs(ATM-29f8b0): universal dispatch dialog docs and changelog"
```

- [ ] **Step 7: Update the ATM ledger**

Record completion on the task and journal the outcome:

```bash
atm task comment add --task ATM-29f8b0 --body "Implementation complete (developer@opencode:unset). Universal dispatch dialog shipped: persona field via 'p' over all store personas, dispatchKind removed, project requirement from persona project_optional, D opens from any pane with concierge fallback. Verified with make verify."
```
