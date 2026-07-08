# Persona Activity in Projects Pane + Overlay Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Move persona-grouped actor activity into the Projects pane (compact chart + `P` expand overlay), remove the `[4] Actors` maximized pane, add a `p` persona-create form, and fix bar-width alignment in the overlay list.

**Architecture:** Pure TUI change. `internal/tui/projects.go` replaces its raw-actor chart with a persona-grouped one built on `activity.Build`/`Aggregate` + `store.LoadAliases`. `internal/tui/actors.go` is refactored from a maximized pane into an overlay renderer sized to the centered modal box. `internal/tui/app.go` drops `paneActors`/`numPanes=4`/the `4` tab, adds `actorsOverlay` state, `P`/`p` key routing, and `formPersonaCreate`. The store layer (`internal/store/persona.go`, `internal/store/alias.go`), `internal/activity`, and the `atm persona`/`atm actor`/`atm activity` CLI are untouched.

**Tech Stack:** Go, cobra CLI, Bubble Tea/lipgloss TUI, existing `internal/store` + `internal/activity` + `internal/actor`.

## Global Constraints

- Go module path is `atm` (imports are `atm/internal/...`).
- Persona name slug: `^[a-z0-9]([a-z0-9-]*[a-z0-9])?$` (lowercase; never `@` or `:`). Validate via `store.ValidatePersonaName`.
- Actor-string convention: `<persona>@<agent>:<model>`; alias table first, then convention parse, then `(none)` — all via `actor.Resolve` (already implemented, unchanged).
- Personas/aliases are **global** (machine-wide store root), never per-project. Persona mutations are **not** written to `log.jsonl`. No new log action; no `Replay` change.
- "Remove old actor names everywhere" = **activity views only** (the Projects chart + the `P` overlay). HISTORY lines and FACTS `created by`/`updated by` keep the raw actor string (audit provenance stays literal). The store never rewrites `log.jsonl`.
- Bar-width formula (both Projects chart and overlay list): `meterW = availableWidth - nameW - fixedSuffixWidth`. Fixed suffix for the Projects chart is 10 (`%3d%%`(4) + space(1) + `%3d`(3) + 2 spaces around the meter = 10). Fixed suffix for the overlay list is 12 (cursor(1) + 2 spaces + `%3d%%`(4) + space(1) + `%4d`(4) = 12).
- Test helpers in `internal/tui`: `newTestModel(t)`, `newTestModelWithActor(t, actor)`, `keyMsg(s)`, `update(t, m, key)`, `mustContain(t, view, sub)`, `seedProject(t, m, code, name)`, `seedTask(t, m, code, title, labels...)`. Reuse these exactly — do not invent new harness names.
- Run `make build && make test` (a.k.a. `make verify`) green before each commit.
- No emojis in code or commits. No comments unless asked.

---

## File Structure

- `internal/tui/projects.go` (modify) — replace `actorActivityRows`/`renderActorActivityChart`/`longestActorNameWidth`/`actorActivityRow` with `renderPersonaActivityChart`/`longestPersonaKeyWidth`; update the `remaining == 1` caption; add `P`/`p` key dispatch in `handleKey`.
- `internal/tui/actors.go` (modify) — fix `renderList` bar width; update the empty-state line; keep `SetSize`/`refresh`/`handleKey`/`View`/`renderDetail`/`writeBreakdown`/`sortKV` (now driven by the overlay, not a pane).
- `internal/tui/app.go` (modify) — remove `paneActors`/`numPanes=4`/`case "4"`/maximized render/`paneActors` statusHint/Esc branch/per-pane dispatch; add `actorsOverlay bool`, `P`/`p` handling, overlay key routing, `renderActorsOverlay`, `formPersonaCreate`, `doPersonaCreate`, `actorsOverlayBoxSize`.
- `internal/tui/keymap.go` (modify) — add `P` and `p` rows.
- `internal/tui/actors_test.go` (modify) — replace pane tests with overlay tests.
- `internal/tui/app_test.go` (modify) — drop the `4`/`paneActors` assertion in `TestPaneFocusKeys`.
- `internal/tui/projects_test.go` (new or modify) — persona chart + `P`/`p` tests. (If `projects_test.go` exists, extend it; otherwise create it. Check first.)

---

## Task 1: Persona activity chart in Projects pane (replaces "activity by actor")

**Files:**
- Modify: `internal/tui/projects.go` (delete `actorActivityRow`, `actorActivityRows`, `renderActorActivityChart`, `longestActorNameWidth`; add `renderPersonaActivityChart`, `longestPersonaKeyWidth`; update `renderProjectSummary` call sites and the `remaining == 1` caption)
- Test: `internal/tui/projects_test.go` (extend if it exists, else create)

**Interfaces:**
- Consumes: `p.m.store.LoadAliases()`, `activity.Build`, `activity.Aggregate`, `meterBar`, `chartBoxInnerWidth`, `renderChartBox`, `dashboardLine`, `lipgloss.Width` (already imported in projects.go).
- Produces:
  - `func (p *projectsModel) renderPersonaActivityChart(entries []store.LogEntry, maxLines int) []string`
  - `func longestPersonaKeyWidth(groups []activity.Group) int`

- [ ] **Step 1: Locate the existing projects test file**

Run: `ls internal/tui/projects_test.go 2>/dev/null && echo EXISTS || echo MISSING`

If `projects_test.go` exists, read it to match its package/imports/helpers. If missing, the test file will be `internal/tui/projects_test.go` with `package tui` and the shared helpers from `app_test.go` (`newTestModel`, `seedProject`, `seedTask`, `update`, `mustContain`).

- [ ] **Step 2: Write the failing test**

Add to `internal/tui/projects_test.go` (create the file if missing; otherwise extend):

```go
package tui

import (
	"strings"
	"testing"

	"atm/internal/store"
)

// seedAlias installs an actor alias so legacy actor strings resolve to a
// persona during the chart render.
func seedAlias(t *testing.T, m *Model, raw, persona, agent string) {
	t.Helper()
	if err := m.store.SetAlias(raw, store.AliasEntry{Persona: persona, Agent: agent}); err != nil {
		t.Fatalf("SetAlias %s: %v", raw, err)
	}
}

func TestRenderPersonaActivityChart(t *testing.T) {
	m := newTestModelWithActor(t, "staff@claude:opus-4.8")
	seedProject(t, m, "ATM", "Acme Task Manager")
	seedTask(t, m, "ATM", "task one")
	seedTask(t, m, "ATM", "task two")
	// Legacy actor on a project mutation directly via store so the log has it.
	seedAlias(t, m, "claude", "developer", "claude")

	m.SetSize(80, 24)
	m.projectScope = "ATM"
	m.refreshAll()

	entries, err := m.store.ReadLog("ATM")
	if err != nil && !store.IsIntegrity(err) {
		t.Fatalf("ReadLog: %v", err)
	}
	lines := m.projects.renderPersonaActivityChart(entries, 8)
	view := strings.Join(lines, "\n")
	if !strings.Contains(view, "activity by persona") {
		t.Fatalf("chart title wrong:\n%s", view)
	}
	if !strings.Contains(view, "staff") {
		t.Fatalf("missing persona row 'staff':\n%s", view)
	}
	if strings.Contains(view, "claude-dev") {
		t.Fatalf("raw actor name leaked into persona chart:\n%s", view)
	}
}

func TestRenderPersonaActivityChartEmpty(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	m.SetSize(80, 24)
	m.projectScope = "ATM"
	m.refreshAll()
	entries, _ := m.store.ReadLog("ATM")
	// The project.created entry exists, so this is never truly empty, but
	// assert the title still renders for the degenerate maxLines<3 case.
	lines := m.projects.renderPersonaActivityChart(entries, 1)
	view := strings.Join(lines, "\n")
	if !strings.Contains(view, "activity by persona") {
		t.Fatalf("degenerate title wrong:\n%s", view)
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/tui/ -run TestRenderPersonaActivity`
Expected: FAIL (`renderPersonaActivityChart` undefined).

- [ ] **Step 4: Replace the chart implementation**

In `internal/tui/projects.go`, **delete** these four symbols (lines are approximate — match the names exactly):
- `type actorActivityRow struct { ... }` (around projects.go:54-58)
- `func actorActivityRows(entries []store.LogEntry, limit int) []actorActivityRow { ... }` (projects.go:92-131)
- `func (p *projectsModel) renderActorActivityChart(entries []store.LogEntry, maxLines int) []string { ... }` (projects.go:515-548)
- `func longestActorNameWidth(rows []actorActivityRow) int { ... }` (projects.go:550-558)

**Add** the new chart function and width helper in their place:

```go
func (p *projectsModel) renderPersonaActivityChart(entries []store.LogEntry, maxLines int) []string {
	if maxLines <= 0 {
		return nil
	}
	if maxLines < 3 {
		return []string{dashboardLine(p.width, "activity by persona")}
	}
	entryCap := maxLines - 2
	if entryCap <= 0 {
		return []string{dashboardLine(p.width, "activity by persona")}
	}
	aliases, _ := p.m.store.LoadAliases()
	groups := activity.Aggregate(activity.Build(entries, aliases), "persona")
	body := []string{}
	if len(groups) == 0 {
		body = append(body, p.m.styles.Muted.Render("no activity yet"))
		return strings.Split(p.renderChartBox("activity by persona", strings.Join(body, "\n"), 3), "\n")
	}
	if len(groups) > entryCap {
		groups = groups[:entryCap]
	}
	nameW := longestPersonaKeyWidth(groups)
	meterW := chartBoxInnerWidth(p.width) - nameW - 10
	if meterW < 10 {
		meterW = 10
	}
	total := 0
	for _, g := range groups {
		total += g.Count
	}
	for _, g := range groups {
		percent := 0
		if total > 0 {
			percent = (g.Count*100 + total/2) / total
		}
		line := fmt.Sprintf("%-*s %s %3d%% %3d", nameW, g.Key, meterBar(percent, meterW), percent, g.Count)
		body = append(body, line)
	}
	return strings.Split(p.renderChartBox("activity by persona", strings.Join(body, "\n"), maxLines), "\n")
}

func longestPersonaKeyWidth(groups []activity.Group) int {
	width := 0
	for _, g := range groups {
		if w := lipgloss.Width(g.Key); w > width {
			width = w
		}
	}
	return width
}
```

**Update imports** at the top of `internal/tui/projects.go`: add `"atm/internal/activity"` (the file already imports `atm/internal/store`, `lipgloss`, `fmt`, `strings`). Check the existing import block and add the one missing line.

- [ ] **Step 5: Update the `remaining == 1` caption in renderProjectSummary**

In `renderProjectSummary` (projects.go ~line 457), change:

```go
	if remaining == 1 {
		lines = append(lines, dashboardLine(p.width, "activity by actor"))
		return padToHeight(strings.Join(lines, "\n"), height)
	}
```

to:

```go
	if remaining == 1 {
		lines = append(lines, dashboardLine(p.width, "activity by persona"))
		return padToHeight(strings.Join(lines, "\n"), height)
	}
```

- [ ] **Step 6: Update the three call sites of renderActorActivityChart**

In `renderProjectSummary`, every `p.renderActorActivityChart(entries, ...)` call must become `p.renderPersonaActivityChart(entries, ...)`. There are three call sites (the `remaining == 2`, `remaining == 3`, `>= 9`, and the trailing `actorMax` block). Grep to confirm count:

Run: `grep -n renderActorActivityChart internal/tui/projects.go`
Expected: no matches (all replaced).

- [ ] **Step 7: Run tests**

Run: `go test ./internal/tui/ -run TestRenderPersonaActivity`
Expected: PASS. Then run the whole TUI package to catch call-site breakage:
Run: `go test ./internal/tui/`
Expected: any failures are only the `4`/`paneActors` tests (handled in Task 4); the projects/labels/tasks tests pass.

- [ ] **Step 8: Commit**

```bash
git add internal/tui/projects.go internal/tui/projects_test.go
git commit -m "Replace Projects pane 'activity by actor' with persona-grouped chart (ATM-0054)"
```

---

## Task 2: Fix overlay bar-width alignment in `actorsModel.renderList`

**Files:**
- Modify: `internal/tui/actors.go` (`renderList` meterW formula; empty-state line text)
- Test: `internal/tui/actors_test.go` (extend `TestActorsPaneRendersChart` with a width assertion; rename to overlay-appropriate name in Task 4 — for now keep the pane test alive since Task 4 flips it)

**Interfaces:** unchanged signatures; this is a pure rendering fix.

- [ ] **Step 1: Write the failing test**

In `internal/tui/actors_test.go`, add a width-alignment assertion to the existing `TestActorsPaneRendersChart` (keep using the pane path for now — Task 4 converts it to overlay):

```go
func TestActorsBarsAlignToWidth(t *testing.T) {
	m := mkActorsTestModel(t)
	m.SetSize(80, 24)
	m.projectScope = "ATM"
	m.focused = paneActors
	m.actors.refresh()
	m.actors.SetSize(80, 24)
	view := m.actors.renderList()
	// Each non-empty, non-border line must be exactly 80 cells wide (dashboardLine
	// pads to width). Assert the persona row line width equals the pane width.
	for _, line := range strings.Split(view, "\n") {
		if strings.Contains(line, "staff") {
			if w := lipgloss.Width(line); w != 80 {
				t.Fatalf("persona row width = %d, want 80:\n%q", w, line)
			}
		}
	}
}
```

Add `"github.com/charmbracelet/lipgloss"` to the actors_test.go imports if not already present.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tui/ -run TestActorsBarsAlign`
Expected: FAIL (the current `meterW = a.width - nameW - 16` leaves the line short by 4 cells, so `dashboardLine` pads with trailing spaces but the bar end is misaligned relative to the `%4d` column — the assertion catches the under-reservation because the meter doesn't reach the expected column).

- [ ] **Step 3: Fix the meter-width formula**

In `internal/tui/actors.go` `renderList`, replace:

```go
	meterW := a.width - nameW - 16
	if meterW < 10 {
		meterW = 10
	}
```

with:

```go
	// Fixed suffix: cursor(1) + 2 spaces + %3d%%(4) + space(1) + %4d(4) = 12.
	meterW := a.width - nameW - 12
	if meterW < 10 {
		meterW = 10
	}
```

- [ ] **Step 4: Update the empty-state line text**

In `internal/tui/actors.go` `View`, change:

```go
		return padToHeight(dashboardLine(a.width, a.m.styles.Muted.Render("select a project (pane 1) to see actor activity")), a.contentHeight)
```

to:

```go
		return padToHeight(dashboardLine(a.width, a.m.styles.Muted.Render("select a project to see actor activity")), a.contentHeight)
```

- [ ] **Step 5: Run tests**

Run: `go test ./internal/tui/ -run TestActorsBarsAlign`
Expected: PASS. Then:
Run: `go test ./internal/tui/`
Expected: existing actors tests still pass (Task 4 will convert them).

- [ ] **Step 6: Commit**

```bash
git add internal/tui/actors.go internal/tui/actors_test.go
git commit -m "Fix actorsModel.renderList bar-width alignment (ATM-0054)"
```

---

## Task 3: Remove the `[4] Actors` maximized pane from `app.go`

**Files:**
- Modify: `internal/tui/app.go` (enum, `numPanes`, `SetSize`, tab key, `renderWorkspace`, `statusHint`, Esc branch, per-pane dispatch)
- Modify: `internal/tui/app_test.go` (drop the `4` assertion in `TestPaneFocusKeys`)
- Test: no new tests; the regression is covered by `TestPaneFocusKeys` no longer asserting `paneActors`, and Task 4 adds the overlay tests.

**Interfaces:**
- Removes: `paneActors`, `numPanes=4` (back to 3), the `case "4"` handler, the maximized render branch, the `paneActors` statusHint/Esc/dispatch cases, the `m.actors.SetSize` call in `SetSize`.
- Keeps: `Model.actors actorsModel` (now driven by the overlay in Task 4).

- [ ] **Step 1: Update `TestPaneFocusKeys` to stop asserting `paneActors`**

In `internal/tui/app_test.go` `TestPaneFocusKeys`, delete the `4` block:

```go
	update(t, m, "4")
	if m.focused != paneActors {
		t.Fatalf("after 4: focus = %v want paneActors", m.focused)
	}
```

The test becomes `1 -> 2 -> 3 -> 1`. Pressing `4` is no longer asserted (and after this task `4` is a no-op).

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tui/ -run TestPaneFocusKeys`
Expected: FAIL (compile error: `paneActors` undefined once you remove it in step 4; but first the test change alone keeps it compiling because `paneActors` still exists until step 4). So run the test now — it should still PASS (we only removed an assertion). The real failure comes after step 4 removes the symbol. To drive TDD, we instead rely on the compile to be the gate after step 4.

- [ ] **Step 3: Remove the `paneActors` enum and `numPanes=4`**

In `internal/tui/app.go`, find the enum (around line 13-21):

```go
const (
	paneProjects workspacePane = iota
	paneTasks
	paneLabels
	paneActors
)

const numPanes = 4
```

Change to:

```go
const (
	paneProjects workspacePane = iota
	paneTasks
	paneLabels
)

const numPanes = 3
```

- [ ] **Step 4: Remove the `case "4"` tab handler**

In `handleKey` (app.go ~line 381-384), delete:

```go
	case "4":
		m.focused = paneActors
		m.actors.refresh()
		return nil
```

- [ ] **Step 5: Remove the maximized-`paneActors` render branch**

In `renderWorkspace` (app.go ~line 567-569), delete:

```go
	if m.focused == paneActors {
		return m.renderPane(paneActors, m.width, m.contentHeight, "[4] Actors", m.actors.View())
	}
```

- [ ] **Step 6: Remove the `paneActors` statusHint case**

In `statusHint` (app.go ~line 598-600), delete:

```go
	case paneActors:
		return m.actors.statusHint()
```

(The `switch` now has only `paneProjects`/`paneTasks`/`paneLabels` and falls through to the default return.)

- [ ] **Step 7: Remove the `paneActors` Esc branch**

In `handleKey` (app.go ~line 426-429), delete:

```go
		if m.focused == paneActors && m.actors.detail {
			m.actors.detail = false
			return nil
		}
```

- [ ] **Step 8: Remove the `paneActors` per-pane dispatch**

In `handleKey` (app.go ~line 441-442), delete:

```go
	case paneActors:
		return m.actors.handleKey(k)
```

- [ ] **Step 9: Remove `m.actors.SetSize` from `SetSize`**

In `SetSize` (app.go ~line 164), delete:

```go
	m.actors.SetSize(innerPaneWidth(m.width), innerPaneHeight(m.contentHeight))
```

(The overlay sizes `m.actors` itself when it opens, added in Task 4.)

- [ ] **Step 10: Compile and run tests**

Run: `go build ./internal/tui/`
Expected: builds clean (no remaining `paneActors` references).

Run: `rg -n "paneActors" internal/tui/`
Expected: no matches.

Run: `go test ./internal/tui/`
Expected: `TestPaneFocusKeys` PASS; the old `TestActorsPaneRendersChart`/`TestTabReachesActorsPane` (actors_test.go) FAIL because they reference `paneActors` — Task 4 rewrites them. Leave those failing for now (or temporarily skip them with `t.Skip` if they block the build of the test binary; but they should still compile since `paneActors` is gone — fix: in actors_test.go, change `m.focused = paneActors` to `m.focused = paneProjects` and the `paneActors` assertion to `paneProjects` as a placeholder; Task 4 rewrites the whole file).

Concretely, to keep the test binary compiling, in `internal/tui/actors_test.go` change:
- `m.focused = paneActors` → `m.focused = paneProjects`
- `if m.focused != paneActors` → `if m.focused != paneProjects`
- `want paneActors` → `want paneProjects`

These assertions become trivially wrong but compile; Task 4 replaces the file entirely.

- [ ] **Step 11: Commit**

```bash
git add internal/tui/app.go internal/tui/app_test.go internal/tui/actors_test.go
git commit -m "Remove [4] Actors maximized pane (numPanes back to 3) (ATM-0054)"
```

---

## Task 4: `P` expand overlay + `p` add-persona form

**Files:**
- Modify: `internal/tui/app.go` (overlay state, `actorsOverlayBoxSize`, `renderActorsOverlay`, key routing, `formPersonaCreate`, `doPersonaCreate`, `submitForm` case)
- Modify: `internal/tui/actors.go` (`statusHint` removed — it was for the pane; the overlay renders its own hint)
- Modify: `internal/tui/keymap.go` (add `P` and `p` rows)
- Modify: `internal/tui/actors_test.go` (rewrite as overlay tests)
- Test: `internal/tui/app_test.go` (overlay + form tests)

**Interfaces:**
- Consumes: `m.store.LoadAliases`, `m.store.CreatePersona`, `store.ValidatePersonaName`, `activity.Build`/`Aggregate`, `placeOverlay`, `titledBoxHeight`, `helpBoxSize` pattern, `NewForm`, `formField`.
- Produces:
  - `Model.actorsOverlay bool`
  - `func (m *Model) actorsOverlayBoxSize() (int, int)`
  - `func (m *Model) renderActorsOverlay() string`
  - `func (m *Model) doPersonaCreate(vals map[string]string) tea.Cmd`
  - `formPersonaCreate` in the `formAction` enum

- [ ] **Step 1: Write the failing test (overlay open/close + drilldown)**

Rewrite `internal/tui/actors_test.go` entirely:

```go
package tui

import (
	"strings"
	"testing"
)

func mkActorsOverlayTestModel(t *testing.T) *Model {
	t.Helper()
	m := newTestModelWithActor(t, "staff@claude:opus-4.8")
	seedProjectAsActor(t, m, "ATM", "Acme Task Manager", "staff@claude:opus-4.8")
	seedTaskAsActor(t, m, "ATM", "task one", "staff@claude:opus-4.8")
	return m
}

func seedProjectAsActor(t *testing.T, m *Model, code, name, actor string) {
	t.Helper()
	if _, err := m.store.CreateProject(code, name, actor); err != nil {
		t.Fatalf("CreateProject %s: %v", code, err)
	}
	m.refreshAll()
}

func seedTaskAsActor(t *testing.T, m *Model, projectCode, title, actor string) {
	t.Helper()
	if _, err := m.store.CreateTask(projectCode, title, "", nil, actor); err != nil {
		t.Fatalf("CreateTask %s: %v", title, err)
	}
	m.refreshAll()
}

func TestActorsOverlayOpensAndShowsPersona(t *testing.T) {
	m := mkActorsOverlayTestModel(t)
	m.SetSize(100, 30)
	m.projectScope = "ATM"
	m.focused = paneProjects
	update(t, m, "P")
	if !m.actorsOverlay {
		t.Fatal("P should open actors overlay")
	}
	view := m.View()
	if !strings.Contains(view, "Activity by persona") {
		t.Fatalf("overlay title missing:\n%s", view)
	}
	if !strings.Contains(view, "staff") {
		t.Fatalf("persona row missing:\n%s", view)
	}
}

func TestActorsOverlayDrilldownAndEsc(t *testing.T) {
	m := mkActorsOverlayTestModel(t)
	m.SetSize(100, 30)
	m.projectScope = "ATM"
	m.focused = paneProjects
	update(t, m, "P")
	update(t, m, "enter") // drill into detail
	view := m.View()
	if !strings.Contains(view, "persona: staff") && !strings.Contains(view, "agents") {
		t.Fatalf("detail not shown:\n%s", view)
	}
	update(t, m, "esc") // detail -> list
	if m.actors.detail {
		t.Fatal("Esc should leave detail")
	}
	update(t, m, "esc") // list -> close
	if m.actorsOverlay {
		t.Fatal("Esc should close overlay")
	}
}

func TestActorsOverlayNoProjectToasts(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(100, 30)
	m.focused = paneProjects
	m.projectScope = ""
	update(t, m, "P")
	if m.actorsOverlay {
		t.Fatal("overlay must not open without a project")
	}
	if m.toastMsg == "" || !strings.Contains(m.toastMsg, "select a project") {
		t.Fatalf("expected a 'select a project' toast, got %q", m.toastMsg)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tui/ -run TestActorsOverlay`
Expected: FAIL (`P` does nothing; `actorsOverlay` undefined).

- [ ] **Step 3: Add overlay state and box size to `Model`**

In `internal/tui/app.go`, add a field to `Model` (near `helpOverlay`):

```go
	// actorsOverlay, when true, renders the persona activity list/detail as a
	// centered modal over the workspace (opened by P in the Projects pane).
	actorsOverlay bool
```

Add a box-size helper near `helpBoxSize`:

```go
// actorsOverlayBoxSize returns the outer dimensions of the centered modal that
// hosts the P overlay. It mirrors helpBoxSize's ~80% sizing so the persona list
// and detail breakdowns stay readable.
func (m *Model) actorsOverlayBoxSize() (int, int) {
	bw, bh := m.helpBoxSize()
	return bw, bh
}
```

- [ ] **Step 4: Add the overlay renderer**

In `internal/tui/app.go`, add:

```go
// renderActorsOverlay renders the persona activity list/detail as a centered
// modal box (the P overlay) sized like the help overlay.
func (m *Model) renderActorsOverlay() string {
	bw, bh := m.actorsOverlayBoxSize()
	return titledBoxHeight(m.styles.DialogBody, bw, "Activity by persona", m.actors.View(), bh)
}
```

- [ ] **Step 5: Layer the overlay in `View()`**

In `View()` (app.go ~line 552-560), after the `m.confirm` overlay block, add:

```go
	if m.actorsOverlay {
		out = m.placeOverlay(out, m.renderActorsOverlay())
	}
```

- [ ] **Step 6: Size `m.actors` when the overlay opens**

Add a helper to size the actors model to the overlay's inner box. `titledBoxHeight` draws a 1-cell border + title row + bottom row, so inner = (bw-2) x (bh-2):

```go
func (m *Model) sizeActorsToOverlay() {
	bw, bh := m.actorsOverlayBoxSize()
	innerW := bw - 2
	if innerW < 1 {
		innerW = 1
	}
	innerH := bh - 2
	if innerH < 1 {
		innerH = 1
	}
	m.actors.SetSize(innerW, innerH)
}
```

Call it wherever the overlay opens (step 8 and after persona mutations).

- [ ] **Step 7: Add the `formPersonaCreate` enum value**

In `internal/tui/app.go`, add to the `formAction` const block:

```go
	formPersonaCreate
```

- [ ] **Step 8: Route keys while the overlay is open + add `P`/`p` to Projects pane**

In `handleKey`, add an overlay-routing block **after** the form/confirm/help blocks (which already return early) and **before** the `if m.focused == paneTasks && m.tasks.filterEditing` line (app.go ~line 359). Insert:

```go
	// Actors overlay (P) consumes navigation + p (add persona) + Esc until closed.
	if m.actorsOverlay {
		switch k.String() {
		case "esc":
			if m.actors.detail {
				m.actors.handleKey(k)
				return nil
			}
			m.actorsOverlay = false
			return nil
		case "p":
			return m.openPersonaCreateForm()
		case "?":
			m.openHelp(helpKeys)
			return nil
		case "C":
			m.openHelp(helpConventions)
			return nil
		case "T":
			m.cycleTheme()
			return nil
		}
		// j/k/up/down/enter route to the actors model.
		return m.actors.handleKey(k)
	}
```

Then, in the Projects-pane `handleKey` block, add `P` and `p`. Find `switch m.focused { case paneProjects: return m.projects.handleKey(k) }` — but `P`/`p` are global-ish to Projects, so add them in the tab-switch area. Actually the cleanest place: add `P` and `p` to the paneProjects case in the per-pane dispatch. But the per-pane dispatch is `return m.projects.handleKey(k)`. So add `P`/`p` handling in `projectsModel.handleKey`. Read `projectsModel.handleKey` first to find the right spot.

Run: `rg -n "func \(p \*projectsModel\) handleKey" internal/tui/projects.go`
Read that function. Add two cases alongside the existing `case "a":`, `case "x":` etc.:

```go
	case "P":
		return p.openActorsOverlay()
	case "p":
		return p.openPersonaCreateForm()
```

Add the two methods to `projects.go`:

```go
func (p *projectsModel) openActorsOverlay() tea.Cmd {
	if p.m.projectScope == "" {
		p.m.showToast("select a project first")
		return nil
	}
	p.m.actorsOverlay = true
	p.m.actors.refresh()
	p.m.sizeActorsToOverlay()
	return nil
}

func (p *projectsModel) openPersonaCreateForm() tea.Cmd {
	nameValidator := func(field, value string) error {
		if value == "" {
			return nil
		}
		if err := store.ValidatePersonaName(value); err != nil {
			return err
		}
		return nil
	}
	fields := []formField{
		{Label: "name", Required: true, Hint: "lowercase slug, e.g. staff-engineer", Validator: nameValidator},
		{Label: "description", Hint: "one-line summary (optional)"},
	}
	f := NewForm("New persona", fields)
	p.m.form = f
	p.m.formKind = formPersonaCreate
	return nil
}
```

Make sure `store` is imported in `projects.go` (it already is).

- [ ] **Step 9: Add `doPersonaCreate` and wire `submitForm`**

In `internal/tui/app.go`, add the case to `submitForm`:

```go
	case formPersonaCreate:
		return m.doPersonaCreate(vals)
```

And add the handler near `doCommentAdd`:

```go
func (m *Model) doPersonaCreate(vals map[string]string) tea.Cmd {
	name := vals["name"]
	desc := vals["description"]
	_, err := m.store.CreatePersona(name, "", desc, m.actor)
	if err != nil {
		if store.IsConflict(err) {
			m.showToast(fmt.Sprintf("persona %s already exists", name))
		} else {
			m.showToast("error: " + err.Error())
		}
		return nil
	}
	m.showToast(fmt.Sprintf("created persona %s", name))
	m.actors.refresh()
	m.refreshAll()
	return nil
}
```

- [ ] **Step 10: Remove `actorsModel.statusHint` (it was for the pane)**

In `internal/tui/actors.go`, delete the `statusHint` method (lines ~43-48):

```go
func (a *actorsModel) statusHint() string {
	if a.detail {
		return "[Esc]back"
	}
	return "[Enter]detail [↑/↓]move"
}
```

(Task 3 already removed its only caller.)

- [ ] **Step 11: Run tests**

Run: `go test ./internal/tui/ -run TestActorsOverlay`
Expected: PASS.

Run: `go test ./internal/tui/`
Expected: PASS (the old pane tests were rewritten in the actors_test.go rewrite; `TestPaneFocusKeys` passes since `4` is a no-op now).

- [ ] **Step 12: Commit**

```bash
git add internal/tui/app.go internal/tui/actors.go internal/tui/actors_test.go
git commit -m "Add P expand overlay + p persona-create form (ATM-0054)"
```

---

## Task 5: `p` form submit from overlay + refresh-after-create

**Files:**
- Modify: `internal/tui/app_test.go` (add form-submit-from-overlay test)
- Test: `internal/tui/app_test.go`

**Interfaces:** consumes `openPersonaCreateForm`, `doPersonaCreate`, `m.actors.refresh`.

- [ ] **Step 1: Write the failing test**

Add to `internal/tui/app_test.go`:

```go
func TestPersonaCreateFormFromOverlay(t *testing.T) {
	m := mkActorsOverlayTestModel(t)
	m.SetSize(100, 30)
	m.projectScope = "ATM"
	m.focused = paneProjects
	update(t, m, "P") // open overlay
	if !m.actorsOverlay {
		t.Fatal("overlay should be open")
	}
	update(t, m, "p") // open form on top of overlay
	if m.form == nil || m.formKind != formPersonaCreate {
		t.Fatalf("persona form not open: form=%v kind=%v", m.form, m.formKind)
	}
	// Type a name.
	for _, r := range "reviewer" {
		update(t, m, string(r))
	}
	update(t, m, "tab") // name -> description
	for _, r := range "holds a high bar" {
		update(t, m, string(r))
	}
	update(t, m, "enter") // submit (enter on last field submits)
	if m.form != nil {
		t.Fatalf("form should be closed after submit: %v", m.form)
	}
	// Persona exists now.
	p, err := m.store.GetPersona("reviewer")
	if err != nil || p.Description != "holds a high bar" {
		t.Fatalf("persona not created: %+v %v", p, err)
	}
	// Overlay is still open (form was on top; submit closes form, not overlay).
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
	update(t, m, "esc") // cancel form
	if m.form != nil {
		t.Fatal("form should be closed on Esc")
	}
	if !m.actorsOverlay {
		t.Fatal("overlay should still be open after form Esc (form was on top)")
	}
}
```

Note: `mkActorsOverlayTestModel`, `seedProjectAsActor`, `seedTaskAsActor` live in `actors_test.go` (Task 4). They're in the same `package tui` so they're visible to `app_test.go`.

- [ ] **Step 2: Run test to verify it fails or passes**

Run: `go test ./internal/tui/ -run TestPersonaCreateForm`
Expected: should PASS already if Task 4 wired everything (the form layers on top via the existing `handleFormKey` block in `handleKey` which runs before the overlay block). If it fails, the likely cause is that `m.form.Active` path closes the overlay — verify the form-submit path does not set `actorsOverlay=false`. `submitForm` calls `closeForm` (clears form only) and `doPersonaCreate` calls `m.actors.refresh()` but does not touch `actorsOverlay`. So the overlay stays open. If the test fails, debug via:

Run: `go test ./internal/tui/ -run TestPersonaCreateForm -v`
And inspect the failure. Most likely fix: ensure `handleFormKey`/`submitForm` path is reached before the overlay block (it is — the form block is earlier in `handleKey`).

- [ ] **Step 3: If green, commit; if not, fix and commit**

If the test already passes (Task 4's wiring is sufficient), commit the test:

```bash
git add internal/tui/app_test.go
git commit -m "Test persona-create form from overlay (ATM-0054)"
```

If it fails, fix the routing (likely the overlay block must skip when `m.form != nil && m.form.Active`, which the earlier form block already guarantees by returning). Add a guard at the top of the overlay block:

```go
	if m.actorsOverlay && m.form == nil {
```

i.e. change the overlay-routing block's condition to:

```go
	if m.actorsOverlay {
```
already only runs when no form/confirm/help is active (those return earlier). So the test should pass. Commit.

---

## Task 6: Keymap + docs + full verify

**Files:**
- Modify: `internal/tui/keymap.go` (add `P` and `p` rows)
- Modify: `README.md` and/or conventions source if it documents TUI keys
- Modify: `CHANGELOG.md`

- [ ] **Step 1: Update `keymapRows`**

In `internal/tui/keymap.go`, add two rows to `keymapRows` (insert near the `H`/`N`/`M` block of uppercase overlays):

```go
	{"P", "expand activity by persona", "-", "-", "-"},
	{"p", "add persona", "-", "-", "-"},
```

- [ ] **Step 2: Update README / conventions if they list TUI keys**

Run: `grep -rn "activity by actor\|\[4\] Actors\|Actors pane" README.md internal/cli/conventions* 2>/dev/null`
If the README or conventions text mentions the `[4] Actors` pane or "activity by actor", update it to reflect the `P` overlay + `p` add-persona. If it doesn't mention them, no change needed.

- [ ] **Step 3: Update CHANGELOG.md**

Add an entry under the current unreleased/next version summarizing: removed the `[4] Actors` maximized pane; Projects pane "activity by actor" chart now groups by persona (alias-resolved); `P` expands the chart into a persona activity overlay with drilldown; `p` opens a New persona form; bar-width alignment fix.

- [ ] **Step 4: Full verification**

Run: `make verify`
Expected: build + all tests PASS.

- [ ] **Step 5: Manual smoke (real binary)**

```bash
make build
./bin/atm tui
# In the TUI: select a project in [1], press P, verify persona bars + drilldown, Esc Esc to close, press p, create a persona, verify it appears.
```

- [ ] **Step 6: Commit**

```bash
git add internal/tui/keymap.go README.md CHANGELOG.md
git commit -m "Document P/p keybindings and remove [4] Actors from keymap (ATM-0054)"
```

---

## Self-Review

**Spec coverage:**
- D1 in-place chart swap → Task 1. ✓
- D2 remove `[4]` maximized pane → Task 3. ✓
- D3 `P` expand overlay → Task 4. ✓
- D4 `p` add persona form (name+description) → Task 4 (form) + Task 5 (submit-from-overlay test). ✓
- D5 "remove old actor names" = activity views only → Task 1 replaces the only activity chart; HISTORY/FACTS untouched (no task touches them, confirming scope). ✓
- D6 bar-width alignment fix → Task 2 (overlay list) + Task 1 (Projects chart uses the fixed formula). ✓
- Keymap/help → Task 6. ✓
- No store/CLI/log change → confirmed (no task touches `internal/store`, `internal/activity`, `internal/cli`). ✓

**Placeholder scan:** No TBD/TODO; each code step carries the full implementation. Test steps reference the real shared harness helpers (`newTestModel`, `newTestModelWithActor`, `seedProject`, `seedTask`, `update`, `keyMsg`, `mustContain`) by their actual names in `app_test.go`.

**Type consistency:** `formPersonaCreate` used in Task 4 (enum + `submitForm` case + `doPersonaCreate`) and referenced in Task 5's tests. `actorsOverlay bool` consistent across Tasks 4-6. `renderPersonaActivityChart`/`longestPersonaKeyWidth` consistent between Task 1's impl and tests. `activity.Group.Key`/`.Count` used consistently. `store.ValidatePersonaName` / `store.CreatePersona` / `store.IsConflict` are the real signatures from `internal/store/persona.go` and `internal/store/store.go`.

**Open implementer note:** Task 1's test file (`internal/tui/projects_test.go`) may not exist yet — the plan says to check and create if missing, reusing the shared helpers from `app_test.go` (same `package tui`, so all helpers are visible). Task 4's `mkActorsOverlayTestModel`/`seedProjectAsActor`/`seedTaskAsActor` replace the old `mkActorsTestModel`/etc. in `actors_test.go` — the rewrite is explicit in the step.