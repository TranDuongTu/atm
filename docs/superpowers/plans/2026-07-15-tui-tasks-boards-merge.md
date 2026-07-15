# TUI Tasks/Boards Merge Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Merge the `[3] Boards` pane into `[2] Tasks` as a single full-height browsing pane with a board thumbnail carousel, capability-owned Open Tasks default board, and persisted pinning.

**Architecture:** A new `internal/workflow` capability owns the Open Tasks board vocabulary (mirroring `internal/contextmap`). The TUI drops `paneLabels`/`splitRightColumnHeights`; `tasksModel` gains a top board-thumbnail strip + bottom pinned row, reusing `boardsModel`'s level renderers for the SELECTED thumbnail. A new `internal/store/pins.go` adds per-project pin persistence. Navigation is modeless: arrows browse tasks, `[]` switch boards, `Enter` opens task detail, `>`/`<` drill the thumbnail.

**Tech Stack:** Go 1.22+, Bubble Tea, lipgloss, cobra. Store = machine-global text files under `$ATM_HOME`.

**Spec:** `docs/superpowers/specs/2026-07-15-tui-tasks-boards-merge-design.md`

**ATM task:** ATM-2412f2

## Global Constraints

- Go 1.22+; module path `atm`.
- No emojis in code or commits.
- No privileged label: `ATM:open-tasks` is a normal board ensured via `LabelSeed` (idempotent, never overwrites a human's description). The TUI render path must never reference `status:open`.
- Reuse existing renderers: `boardsModel.renderChart` / `renderDetail` for the SELECTED thumbnail. No new compact preview renderer.
- Per-project files under `<store>/projects/<CODE>/`, written via `WithLock` + `WriteFileAtomic`. Missing file = empty state (return `nil, nil`), never an error.
- `make verify` is the completion gate.
- Actor for all store mutations: `developer@<agent>:<model>` — use this session's actual actor (see the session context file); never copy a prior session's stamp.
- TUI tests drive keys via the existing helpers `keyMsg(s string) tea.KeyMsg` (`app_test.go:52`) and `update(t, m, key)` (`app_test.go:108`). There is no `key()` helper. `mustNotContain` already exists (`app_test.go:127`) — do not re-add it.
- CLI tests use `newGoldenHarness(t)` + `h.run(args...)` (`harness_test.go`); golden fixtures regenerate with `go test ./internal/cli/ -run <Test> -update` (`harness_test.go:48`).

---

## File Structure

**New files:**
- `internal/workflow/vocabulary.go` — `EnsureVocabulary(s, code, actor)` ensures `ATM:open-tasks`. Pure vocabulary-ensure capability.
- `internal/workflow/vocabulary_test.go` — idempotency / no-overwrite / fresh-project tests.
- `internal/store/pins.go` — `Pins` struct, `GetPins`, `WritePins`, `pinsPath`.
- `internal/store/pins_test.go` — round-trip / missing-file / prune tests.
- `internal/tui/thumbnails.go` — the thumbnail strip + pinned row renderer and width-split helper.

**Modified files:**
- `internal/tui/app.go` — remove `paneLabels`/`numPanes=3`→`2`, `splitRightColumnHeights`, the `[3]` pane render; `SetSize` grows `[2]` to full right-column height; project-select calls `workflow.EnsureVocabulary`; key dispatch routes `3` to no-op, `[]/>/<` to the merged pane.
- `internal/tui/labels.go` — `boardsModel` gains `selected` (FullName), `pins` slice, ring navigation (`cycleBoard`), `selectDefault`, `pin`/`unpin`/`jumpPin`; `handleKey` rewires `[]`/`>`/`<`/`p`/`Shift-N`; `statusHint` updates.
- `internal/tui/tasks.go` — `tasksModel` gains a `stripHeight` reservation and renders the strip + pinned row above/below the list; `handleKey` handles `[]` (board ring), `>`/`<` (thumbnail drill), `p`, `Shift-1..9`; `Enter` keeps opening task detail.
- `internal/tui/keymap.go` — drop `3`, add `[]` = prev/next board, `>`/`<` = thumbnail drill, `p` = pin, `Shift-N` = jump to pin.
- `internal/tui/help.go` — parity table updates (`[3] Boards` → merged into Tasks; new keys).
- `internal/tui/projects.go` — on project select, call `workflow.EnsureVocabulary` then `boards.selectDefault()`.
- `internal/cli/project.go` — `atm project create` ensures the open-tasks board after `CreateProject`.
- `internal/cli/label.go` — `atm label seed` ensures the open-tasks board after `SeedLabels`.
- `internal/cli/conventions.go` — first-contact sequence points at `ATM:open-tasks` (replace the `--label <CODE>:status:open` line, keeping a `status:open` fallback note).
- `internal/cli/conventions_test.go` — assert `open-tasks` appears in text + JSON.
- `internal/cli/testdata/golden/conventions-*.json` — regenerated golden.
- `internal/tui/app_test.go`, `internal/tui/labels_test.go`, `internal/tui/tasks_test.go` — update tests that reference `[3]`, `paneLabels`, `splitRightColumnHeights`; add merged-pane tests.

---

## Task 1: `internal/workflow` capability — EnsureVocabulary

**Files:**
- Create: `internal/workflow/vocabulary.go`
- Test: `internal/workflow/vocabulary_test.go`

**Interfaces:**
- Produces: `workflow.BoardOpenTasks(code string) string`, `workflow.EnsureVocabulary(s *store.Store, code, actor string) error`.

- [ ] **Step 1: Write the failing test**

```go
package workflow

import (
	"path/filepath"
	"testing"

	"atm/internal/store"
)

func newTestStore(t *testing.T) *store.Store {
	t.Helper()
	dir := t.TempDir()
	root := filepath.Join(dir, "atm")
	s, err := store.Open(root)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := s.Init(""); err != nil {
		t.Fatalf("init: %v", err)
	}
	if _, err := s.CreateProject("ATM", "Atm", "admin@cli:unset"); err != nil {
		t.Fatalf("create project: %v", err)
	}
	return s
}

func TestEnsureVocabularyCreatesOpenTasksBoard(t *testing.T) {
	s := newTestStore(t)
	if err := EnsureVocabulary(s, "ATM", "admin@cli:unset"); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	l, err := s.LabelShow(BoardOpenTasks("ATM"))
	if err != nil {
		t.Fatalf("label show: %v", err)
	}
	if l.Expr == "" {
		t.Error("open-tasks board has no expression")
	}
	if l.Description == "" {
		t.Error("open-tasks board has no description")
	}
}

func TestEnsureVocabularyIdempotent(t *testing.T) {
	s := newTestStore(t)
	if err := EnsureVocabulary(s, "ATM", "admin@cli:unset"); err != nil {
		t.Fatalf("first ensure: %v", err)
	}
	if err := EnsureVocabulary(s, "ATM", "admin@cli:unset"); err != nil {
		t.Fatalf("second ensure: %v", err)
	}
}

func TestEnsureVocabularyDoesNotOverwriteHumanDescription(t *testing.T) {
	s := newTestStore(t)
	humanDesc := "curated by a human"
	if err := s.LabelAdd(BoardOpenTasks("ATM"), humanDesc, "status:open", "admin@cli:unset"); err != nil {
		t.Fatalf("seed human label: %v", err)
	}
	if err := EnsureVocabulary(s, "ATM", "admin@cli:unset"); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	l, err := s.LabelShow(BoardOpenTasks("ATM"))
	if err != nil {
		t.Fatalf("label show: %v", err)
	}
	if l.Description != humanDesc {
		t.Errorf("description = %q, want %q (human curation must survive ensure)", l.Description, humanDesc)
	}
}

func TestEnsureVocabularyWorksWithoutLabelSeed(t *testing.T) {
	s := newTestStore(t)
	// Intentionally do NOT call SeedLabels.
	if err := EnsureVocabulary(s, "ATM", "admin@cli:unset"); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if _, err := s.LabelShow(BoardOpenTasks("ATM")); err != nil {
		t.Errorf("open-tasks missing after ensure without seed: %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/workflow/...`
Expected: FAIL — package does not exist / `EnsureVocabulary` undefined.

- [ ] **Step 3: Write minimal implementation**

```go
// Package workflow owns the vocabulary for the TUI's default board surface.
// It is a minimal capability: it ensures the Open Tasks board exists
// idempotently (mirroring internal/contextmap), exposes no verbs, and owns no
// private data format. The board is a normal label with an expression; a human
// may edit or delete it (capability = paved road, not a fence). The next
// project-select re-ensures it.
package workflow

import "atm/internal/store"

// BoardOpenTasks returns the full name of the Open Tasks board for a project.
// Callers select this board by name; they never reference the expression.
func BoardOpenTasks(code string) string { return code + ":open-tasks" }

// openTasksExpr is the membership expression for the Open Tasks board. It lives
// here, not in the TUI render path, so the TUI never hardcodes a namespace.
func openTasksExpr() string { return "status:open" }

// EnsureVocabulary creates the Open Tasks board with a description, if absent.
// Idempotent: LabelSeed upserts only when the label is absent, so a human's
// curated description is never overwritten. Self-bootstrapping: it does not
// assume `atm label seed` ran.
func EnsureVocabulary(s *store.Store, code, actor string) error {
	return s.LabelSeed(
		BoardOpenTasks(code),
		"every open task: the project's active work. Default board in the TUI.",
		openTasksExpr(),
		actor,
	)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/workflow/...`
Expected: PASS (4 tests).

- [ ] **Step 5: Commit**

```bash
git add internal/workflow/vocabulary.go internal/workflow/vocabulary_test.go
git commit -m "feat(workflow): add EnsureVocabulary for the Open Tasks board"
```

---

## Task 2: `internal/store/pins.go` — per-project pin persistence

**Files:**
- Create: `internal/store/pins.go`
- Test: `internal/store/pins_test.go`

**Interfaces:**
- Produces: `store.Pins` struct, `(*Store).GetPins(code string) (*Pins, error)`, `(*Store).WritePins(code string, p *Pins) error`.
- Consumes: `ReadJSON`, `WriteFileAtomic`, `WithLock`, `validateActor` (all existing).

- [ ] **Step 1: Write the failing test**

```go
package store

import (
	"path/filepath"
	"testing"
)

func setupPinsStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	root := filepath.Join(dir, "atm")
	s, err := Open(root)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := s.Init(""); err != nil {
		t.Fatalf("init: %v", err)
	}
	if _, err := s.CreateProject("ATM", "Atm", "admin@cli:unset"); err != nil {
		t.Fatalf("create project: %v", err)
	}
	return s
}

func TestGetPinsMissingFileReturnsNil(t *testing.T) {
	s := setupPinsStore(t)
	p, err := s.GetPins("ATM")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if p != nil {
		t.Errorf("got %+v, want nil for missing file", p)
	}
}

func TestWritePinsRoundTrip(t *testing.T) {
	s := setupPinsStore(t)
	in := &Pins{
		Actor:  "admin@cli:unset",
		Boards: []string{"ATM:open-tasks", "ATM:status:*"},
	}
	if err := s.WritePins("ATM", in); err != nil {
		t.Fatalf("write: %v", err)
	}
	out, err := s.GetPins("ATM")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if out == nil {
		t.Fatal("got nil pins after write")
	}
	if len(out.Boards) != 2 || out.Boards[0] != "ATM:open-tasks" || out.Boards[1] != "ATM:status:*" {
		t.Errorf("boards = %v, want [ATM:open-tasks, ATM:status:*]", out.Boards)
	}
	if out.UpdatedAt.IsZero() {
		t.Error("UpdatedAt not stamped")
	}
}

func TestWritePinsValidatesActor(t *testing.T) {
	s := setupPinsStore(t)
	err := s.WritePins("ATM", &Pins{Actor: "bogus"})
	if err == nil {
		t.Fatal("expected actor validation error, got nil")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/store/ -run TestPins -count=1` (adjust pattern as needed: `-run 'Pins'`)
Expected: FAIL — `Pins`, `GetPins`, `WritePins` undefined.

- [ ] **Step 3: Write minimal implementation**

```go
package store

import (
	"path/filepath"
	"time"
)

// Pins is the per-project ordered list of pinned board full names, persisted to
// <store>/projects/<CODE>/pins.json. Missing file == empty state (GetPins
// returns nil, nil), mirroring Vocabulary.
type Pins struct {
	UpdatedAt time.Time `json:"updated_at"`
	Actor     string    `json:"actor"`
	Boards    []string  `json:"boards"`
}

func (s *Store) pinsPath(code string) string {
	return filepath.Join(s.projectDir(code), "pins.json")
}

// GetPins reads a project's pins. A missing file returns (nil, nil).
func (s *Store) GetPins(code string) (*Pins, error) {
	var p Pins
	if err := ReadJSON(s.pinsPath(code), &p); err != nil {
		if isNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	return &p, nil
}

// WritePins stamps UpdatedAt and writes pins.json under the project's
// per-project lock. Actor is required (validated).
func (s *Store) WritePins(code string, p *Pins) error {
	if err := s.validateActor(p.Actor); err != nil {
		return err
	}
	p.UpdatedAt = Now()
	return s.WithLock(code, func() error {
		if err := os.MkdirAll(s.projectDir(code), 0o755); err != nil {
			return err
		}
		return WriteFileAtomic(s.pinsPath(code), p)
	})
}
```

Note: add `"os"` to the import block if not already present in the file (it is not, in the snippet above). The final import block is:

```go
import (
	"os"
	"path/filepath"
	"time"
)
```

Also confirm `isNotExist` exists in the package — check `internal/store/vocabulary.go` uses `os.IsNotExist(err)`. If `isNotExist` is not defined, use `os.IsNotExist(err)` directly:

```go
	if err := ReadJSON(s.pinsPath(code), &p); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
```

Use the `os.IsNotExist` form to match `vocabulary.go` exactly.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/store/ -run 'Pins' -count=1`
Expected: PASS (3 tests).

- [ ] **Step 5: Commit**

```bash
git add internal/store/pins.go internal/store/pins_test.go
git commit -m "feat(store): add per-project pins.json persistence"
```

---

## Task 3: Remove the `[3]` pane from the workspace layout

**Files:**
- Modify: `internal/tui/app.go` (`paneLabels`, `numPanes`, `splitRightColumnHeights`, `SetSize`, `renderWorkspace`, `statusHint`, `handleKey` `3` case)
- Modify: `internal/tui/app_test.go` (tests referencing `[3]`, `paneLabels`, `splitRightColumnHeights`)
- Modify: `internal/tui/keymap.go` (drop `3`)
- Modify: `internal/tui/help.go` (parity table `[3] Boards` row)

**Interfaces:**
- Produces: `numPanes = 2`, `paneLabels` removed, `[2] Tasks` sized to full right-column height.

- [ ] **Step 1: Write the failing test**

Add to `internal/tui/app_test.go`:

```go
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
```

(`mustNotContain` already exists at `app_test.go:127`; `keyMsg` at `app_test.go:52`.)

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tui/ -run 'TestWorkspaceRendersTwoPanesNotThree|TestKey3IsNoOp' -count=1`
Expected: FAIL — `[3] Boards` still present.

- [ ] **Step 3: Implement the removal**

In `internal/tui/app.go`:

1. Remove `paneLabels` from the `workspacePane` iota and set `const numPanes = 2`.
2. Delete `splitRightColumnHeights` entirely.
3. In `SetSize`, replace the right-column vertical split:
```go
	leftW, rightW := splitWorkspaceWidths(w)
	m.projects.SetSize(innerPaneWidth(leftW), innerPaneHeight(m.contentHeight))
	m.tasks.SetSize(innerPaneWidth(rightW), innerPaneHeight(m.contentHeight))
```
Remove the `m.boards.SetSize(...)` line. (Keep `m.boards` field for now — Task 4 reuses it; Task 6 rewrites its key handling.)

4. In `renderWorkspace`, replace:
```go
func (m *Model) renderWorkspace() string {
	leftW, rightW := splitWorkspaceWidths(m.width)
	projects := m.renderPane(paneProjects, leftW, m.contentHeight, "[1] Projects", m.projects.View())
	tasks := m.renderPane(paneTasks, rightW, m.contentHeight, "[2] Tasks", m.tasks.View())
	return lipgloss.JoinHorizontal(lipgloss.Top, projects, tasks)
}
```

5. In `handleKey`, remove the `case "3":` block. In `statusHint`, remove the `paneLabels` case.

6. Remove the `paneLabels` branch in the `switch m.focused` at the end of `handleKey` (the `m.boards.handleKey(k)` call). Leave `m.boards` field + `newBoardsModel` init in place for Task 4.

In `internal/tui/keymap.go`, remove the `3` from `{"1/2/3", ...}` — change to `{"1/2", "focus pane", "focus pane", "", "focus pane"}` and update the `Boards` column values to `"-"` or remove the column if the table narrows (keep the column for now, set `Boards` values to `"-"`; a later task rewrites the table).

In `internal/tui/help.go`, change the parity-table row `Boards pane` references to `Tasks pane` (the boards actions now live in `[2]`). Specifically lines referencing `[a]dd / [d]esc` etc. as "Boards pane" → "Tasks pane (boards)".

- [ ] **Step 4: Run test to verify it passes + fix fallout**

Run: `go test ./internal/tui/ -count=1`
Expected: failures in tests that still reference `paneLabels`/`splitRightColumnHeights`/`[3]`. Update those tests:
- Remove `splitRightColumnHeights` tests.
- Change `m.focused = paneLabels` test setups to `paneTasks` or delete the now-obsolete cases.
- Replace `mustContain(t, v, "[3] Boards")` assertions with `mustNotContain` or delete.

Re-run until PASS.

- [ ] **Step 5: Run full verify gate**

Run: `make verify`
Expected: PASS (lint + typecheck + tests). Fix any remaining references.

- [ ] **Step 6: Commit**

```bash
git add internal/tui/app.go internal/tui/app_test.go internal/tui/keymap.go internal/tui/help.go internal/tui/labels_test.go internal/tui/tasks_test.go
git commit -m "refactor(tui): remove the [3] Boards pane; [2] Tasks takes full right-column height"
```

---

## Task 4: Board ring + default selection in `boardsModel`

**Files:**
- Modify: `internal/tui/labels.go` (`boardsModel` gains `selected string`, `selectDefault`, `cycleBoard`; `refresh` rebuilds ring)
- Modify: `internal/tui/projects.go` (project-select calls `workflow.EnsureVocabulary` then `boards.selectDefault`)
- Modify: `internal/tui/app.go` (`NewModel`/`refreshAll` ensure vocab on project select)
- Test: `internal/tui/labels_test.go`

**Interfaces:**
- Produces: `(*boardsModel).selectDefault()`, `(*boardsModel).cycleBoard(dir int)`, `boardsModel.selected string`, `(*boardsModel).ringIndex() int`.
- Consumes: `workflow.EnsureVocabulary`, `workflow.BoardOpenTasks`.

- [ ] **Step 1: Write the failing test**

Add to `internal/tui/labels_test.go`:

```go
func TestSelectDefaultPicksOpenTasksBoard(t *testing.T) {
	m := newTestModel(t)
	if err := workflow.EnsureVocabulary(m.store, "ATM", m.actor); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	seedTask(t, m, "ATM", "open one", "ATM:status:open")
	m.boards.refresh()
	m.boards.selectDefault()
	if m.boards.selected != workflow.BoardOpenTasks("ATM") {
		t.Errorf("selected = %q, want ATM:open-tasks", m.boards.selected)
	}
}

func TestSelectDefaultFallsBackToFirstWhenOpenTasksAbsent(t *testing.T) {
	m := newTestModel(t)
	seedTask(t, m, "ATM", "open one", "ATM:status:open")
	// Do NOT ensure open-tasks; it is absent.
	m.boards.refresh()
	m.boards.selectDefault()
	if m.boards.selected == "" && len(m.boards.rows) > 0 {
		t.Errorf("selected empty but ring has %d boards", len(m.boards.rows))
	}
	if len(m.boards.rows) > 0 && m.boards.selected != m.boards.rows[0].FullName {
		t.Errorf("selected = %q, want first ring board %q", m.boards.selected, m.boards.rows[0].FullName)
	}
}

func TestCycleBoardMovesRing(t *testing.T) {
	m := newTestModel(t)
	seedTask(t, m, "ATM", "open one", "ATM:status:open")
	m.boards.refresh()
	m.boards.selectDefault()
	first := m.boards.selected
	m.boards.cycleBoard(1) // next
	if m.boards.selected == first {
		t.Error("cycleBoard(1) did not move selection")
	}
	m.boards.cycleBoard(-1) // back
	if m.boards.selected != first {
		t.Errorf("after cycle back, selected = %q, want %q", m.boards.selected, first)
	}
}
```

(If `newTestModel` is not the helper name in `labels_test.go`, use the existing helper — check the file's top.)

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tui/ -run 'TestSelectDefault|TestCycleBoard' -count=1`
Expected: FAIL — `selectDefault`/`cycleBoard` undefined.

- [ ] **Step 3: Implement**

In `internal/tui/labels.go`, add to `boardsModel`:

```go
type boardsModel struct {
	// ... existing fields ...
	selected string // FullName of the SELECTED board (ring cursor); "" when no project
}
```

Add methods:

```go
// selectDefault selects the Open Tasks board if present, else the first ring
// board. Called on project select after EnsureVocabulary.
func (b *boardsModel) selectDefault() {
	want := workflow.BoardOpenTasks(b.m.projectScope)
	for _, r := range b.rows {
		if r.FullName == want {
			b.selected = want
			b.applyFocus()
			return
		}
	}
	if len(b.rows) > 0 {
		b.selected = b.rows[0].FullName
		b.applyFocus()
		return
	}
	b.selected = ""
}

// cycleBoard moves the ring selection by dir (+1 next, -1 prev) with wraparound
// and applies the new board's focus to the Tasks list.
func (b *boardsModel) cycleBoard(dir int) {
	if len(b.rows) == 0 {
		return
	}
	idx := b.ringIndex()
	if idx < 0 {
		idx = 0
	}
	idx = (idx + dir) % len(b.rows)
	if idx < 0 {
		idx += len(b.rows)
	}
	b.selected = b.rows[idx].FullName
	b.applyFocus()
}

// ringIndex returns the current ring index of b.selected, or -1 if absent.
func (b *boardsModel) ringIndex() int {
	for i, r := range b.rows {
		if r.FullName == b.selected {
			return i
		}
	}
	return -1
}

// applyFocus pushes the selected board's focus to the Tasks pane, reusing the
// existing setFocus channel. A namespace board (Expandable) uses focusPresent;
// a leaf board uses focusOff + the board's FullName as the filter token.
func (b *boardsModel) applyFocus() {
	if b.selected == "" || b.m.projectScope == "" {
		b.m.tasks.setFocus(taskFocus{mode: focusOff}, "")
		return
	}
	idx := b.ringIndex()
	if idx < 0 {
		return
	}
	r := b.rows[idx]
	if r.Expandable {
		b.m.tasks.setFocus(taskFocus{mode: focusPresent, ns: r.Name}, facetToken(b.m.projectScope, r.Name))
	} else {
		b.m.tasks.setFocus(taskFocus{mode: focusOff}, r.FullName)
	}
}
```

Add the import `"atm/internal/workflow"` to `labels.go`.

At the end of `boardsModel.refresh`, after `b.clampCursor()`:

- If `b.selected == ""`, the caller is responsible for calling `selectDefault`. Do NOT auto-call inside refresh for the empty case (refresh runs on every tick; the initial selection must run on project select).
- If `b.selected != "" && b.ringIndex() < 0`, the previously selected board vanished from the rebuilt ring (deleted mid-session) — call `b.selectDefault()` so a stale selection never keeps driving the task list. This is safe on ticks because it only fires when the selection is already invalid:

```go
	if b.selected != "" && b.ringIndex() < 0 {
		b.selectDefault()
	}
```

In `internal/tui/projects.go`, in the `"s"` project-select handler, after `p.m.boards.reset()`:

```go
			p.m.boards.reset()
			p.m.tasks.backToList()
			p.m.tasks.setFocus(taskFocus{mode: focusOff}, "")
			if err := workflow.EnsureVocabulary(p.m.store, r.code, p.m.actor); err != nil {
				p.m.showToast("ensure open-tasks: " + err.Error())
			}
```

Then, after the existing `p.m.tasks.refresh()` / `p.m.boards.refresh()` calls near the end of that case, add:

```go
			p.m.boards.selectDefault()
```

Add `"atm/internal/workflow"` to `projects.go` imports.

In `internal/tui/app.go` `NewModel`, after `m.refreshAll()` (or right before returning), if `m.projectScope != ""`, call `workflow.EnsureVocabulary(m.store, m.projectScope, m.actor)` then `m.boards.selectDefault()`. (NewModel has no project selected at launch, so this is a no-op until the user selects one — but keep it defensive.)

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/tui/ -run 'TestSelectDefault|TestCycleBoard' -count=1`
Expected: PASS.

- [ ] **Step 5: Run full test suite for fallout**

Run: `go test ./internal/tui/ -count=1`
Expected: PASS (existing boards tests that called the old `enterTable`/`enterChart` may need updating to use `selectDefault`/`cycleBoard` — fix as they surface, but do not rewrite the drill-down handlers yet; Task 5 rewires them).

- [ ] **Step 6: Commit**

```bash
git add internal/tui/labels.go internal/tui/labels_test.go internal/tui/projects.go internal/tui/app.go
git commit -m "feat(tui): board ring with Open Tasks default selection"
```

---

## Task 5: Thumbnail strip renderer (`internal/tui/thumbnails.go`)

**Files:**
- Create: `internal/tui/thumbnails.go`
- Test: `internal/tui/thumbnails_test.go`

**Interfaces:**
- Produces: `(*boardsModel) renderStrip(paneW, stripH int) string`, `(*boardsModel) renderPinnedRow(paneW int) string`, `splitStripWidths(paneW int) (prev, sel, next int)`.
- Consumes: `boardsModel.renderChart`, `boardsModel.renderDetail`, `boardsModel.rows`, `boardsModel.selected`, `titledBoxHeight`, `PaneActive`/`PaneInactive` styles.

- [ ] **Step 1: Write the failing test**

```go
package tui

import (
	"strings"
	"testing"

	"atm/internal/workflow"
)

func TestSplitStripWidths(t *testing.T) {
	prev, sel, next := splitStripWidths(80)
	if prev != 20 || sel != 40 || next != 20 {
		t.Errorf("splitStripWidths(80) = %d/%d/%d, want 20/40/20", prev, sel, next)
	}
}

func TestSplitStripWidthsClampsSmall(t *testing.T) {
	prev, sel, next := splitStripWidths(20)
	if prev < 6 || sel < 8 || next < 6 {
		t.Errorf("splitStripWidths(20) = %d/%d/%d, each must be >= minimum", prev, sel, next)
	}
	if prev+sel+next > 20 {
		t.Errorf("sum %d exceeds pane width 20", prev+sel+next)
	}
}

func TestRenderStripShowsSelectedOpenTasks(t *testing.T) {
	m := newTestModel(t)
	if err := workflow.EnsureVocabulary(m.store, "ATM", m.actor); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	seedTask(t, m, "ATM", "open one", "ATM:status:open")
	m.boards.refresh()
	m.boards.selectDefault()
	strip := m.boards.renderStrip(80, 8)
	if !strings.Contains(strip, "open-tasks") {
		t.Errorf("strip missing open-tasks:\n%s", strip)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tui/ -run 'TestSplitStripWidths|TestRenderStrip' -count=1`
Expected: FAIL — `splitStripWidths`/`renderStrip` undefined.

- [ ] **Step 3: Implement**

```go
package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// splitStripWidths divides pane [2] inner width into prev (25%) / SELECTED (50%)
// / next (25%) with minimum clamps so narrow terminals still render a board name.
func splitStripWidths(paneW int) (prev, sel, next int) {
	prev = paneW * 25 / 100
	sel = paneW * 50 / 100
	next = paneW - prev - sel
	const minSide = 6
	const minSel = 8
	if prev < minSide {
		prev = minSide
	}
	if next < minSide {
		next = minSide
	}
	if sel < minSel {
		sel = minSel
	}
	// Re-fit if the minimums overflow: shrink sides first, then selected.
	for prev+sel+next > paneW && prev > minSide {
		prev--
	}
	for prev+sel+next > paneW && next > minSide {
		next--
	}
	for prev+sel+next > paneW && sel > minSel {
		sel--
	}
	if prev+sel+next > paneW {
		// Last resort: hard truncate to pane width keeping selected priority.
		next = paneW - prev - sel
		if next < 0 {
			next = 0
			sel = paneW - prev
			if sel < 0 {
				sel = 0
				prev = paneW
			}
		}
	}
	return
}

// renderStrip renders the horizontal board thumbnail strip: prev (25%) /
// SELECTED (50%) / next (25%). The SELECTED cell reuses boardsModel's level
// render (chart for a namespace board, detail for a leaf board) sized to its
// width. stripH is the fixed row height.
func (b *boardsModel) renderStrip(paneW, stripH int) string {
	if b.m.projectScope == "" || len(b.rows) == 0 {
		placeholder := titledBoxHeight(b.m.styles.PaneInactive, paneW, "Boards", "no project selected", stripH)
		return placeholder
	}
	prevW, selW, nextW := splitStripWidths(paneW)
	idx := b.ringIndex()
	if idx < 0 {
		idx = 0
	}
	selRow := b.rows[idx]

	// Small rings never duplicate a board across cells: one board -> both
	// sides blank; two boards -> the other board once, on the next side.
	blank := func(w int) string {
		return titledBoxHeight(b.m.styles.PaneInactive, w, "", "", stripH)
	}
	prevCell, nextCell := blank(prevW), blank(nextW)
	switch {
	case len(b.rows) >= 3:
		prevCell = b.renderSideCell(prevW, stripH, b.rows[(idx-1+len(b.rows))%len(b.rows)], "◂")
		nextCell = b.renderSideCell(nextW, stripH, b.rows[(idx+1)%len(b.rows)], "▸")
	case len(b.rows) == 2:
		nextCell = b.renderSideCell(nextW, stripH, b.rows[(idx+1)%len(b.rows)], "▸")
	}
	selCell := b.renderSelectedCell(selW, stripH, selRow)

	return lipgloss.JoinHorizontal(lipgloss.Top, prevCell, selCell, nextCell)
}

// renderSideCell renders a quiet prev/next thumbnail: board name + task count.
func (b *boardsModel) renderSideCell(w, h int, r boardRow, marker string) string {
	body := fmt.Sprintf("%s %s\n%d tasks", marker, r.Name, r.Count)
	return titledBoxHeight(b.m.styles.PaneInactive, w, r.Name, body, h)
}

// renderSelectedCell renders the SELECTED thumbnail, reusing the existing level
// renderer for the board's current level. A namespace board shows its chart; a
// leaf board shows its detail.
func (b *boardsModel) renderSelectedCell(w, h int, r boardRow) string {
	// Temporarily size boardsModel to the selected cell so the reused renderers
	// window correctly, then restore.
	savedW, savedH := b.width, b.contentHeight
	b.SetSize(w, h)
	defer func() { b.width, b.contentHeight = savedW, savedH; b.pageSize = savedH - 2; if b.pageSize < 1 { b.pageSize = 1 } }()

	var inner string
	switch {
	case r.Expandable:
		// Ensure the chart is built for this namespace regardless of b.level:
		// render at the namespace's chart level directly.
		savedLevel, savedNS, savedCursor := b.level, b.ns, b.cursor
		b.level = lLevelChart
		b.ns = r.Name
		b.cursor = 0
		defer func() { b.level, b.ns, b.cursor = savedLevel, savedNS, savedCursor }()
		inner = b.renderChart()
	default:
		// Leaf board (board or stored label): show its detail.
		savedLevel, savedDetail := b.level, b.detail
		b.level = lLevelDetail
		b.detail = labelDetailState{row: labelRow{
			suffix:      strings.TrimPrefix(r.FullName, b.m.projectScope+":"),
			full:        r.FullName,
			description: r.Description,
			usage:       r.Count,
		}}
		defer func() { b.level, b.detail = savedLevel, savedDetail }()
		inner = b.renderDetail()
	}
	return titledBoxHeight(b.m.styles.PaneActive, w, r.Name, inner, h)
}

// renderPinnedRow renders the single compact pinned-boards line. Empty when no
// pins exist.
func (b *boardsModel) renderPinnedRow(paneW int) string {
	if len(b.pins) == 0 {
		return ""
	}
	var parts []string
	for i, full := range b.pins {
		name := strings.TrimPrefix(full, b.m.projectScope+":")
		parts = append(parts, fmt.Sprintf("[%d] %s", i+1, name))
	}
	line := " pinned: " + strings.Join(parts, "  ")
	return dashboardLine(paneW, b.m.styles.Muted.Render(line))
}
```

Note: `b.pins` is added in Task 6; for this task, the renderer compiles against the field that Task 6 introduces. To keep Task 5 independently testable, add the `pins []string` field to `boardsModel` now (Task 6 fills it from the store):

In `internal/tui/labels.go`, add to `boardsModel`:
```go
	pins []string // ordered pinned board FullNames; loaded from store.GetPins
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/tui/ -run 'TestSplitStripWidths|TestRenderStrip' -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/thumbnails.go internal/tui/thumbnails_test.go internal/tui/labels.go
git commit -m "feat(tui): board thumbnail strip + pinned row renderer"
```

---

## Task 6: Pinning — load/persist + `p` / `Shift-N` keys

**Files:**
- Modify: `internal/tui/labels.go` (`boardsModel.pins`, `loadPins`, `togglePin`, `jumpPin`, refresh loads pins)
- Modify: `internal/tui/projects.go` (project-select loads pins)
- Test: `internal/tui/labels_test.go`

**Interfaces:**
- Produces: `(*boardsModel).loadPins()`, `(*boardsModel).togglePin()`, `(*boardsModel).jumpPin(n int) bool`.
- Consumes: `store.GetPins`, `store.WritePins`.

- [ ] **Step 1: Write the failing test**

```go
func TestTogglePinPersists(t *testing.T) {
	m := newTestModel(t)
	seedTask(t, m, "ATM", "open one", "ATM:status:open")
	m.boards.refresh()
	m.boards.selectDefault()
	m.boards.togglePin()
	p, err := m.store.GetPins("ATM")
	if err != nil {
		t.Fatalf("get pins: %v", err)
	}
	if p == nil || len(p.Boards) != 1 || p.Boards[0] != m.boards.selected {
		t.Errorf("pins after toggle = %+v, want [%s]", p, m.boards.selected)
	}
	// Toggle again unpins.
	m.boards.togglePin()
	p, _ = m.store.GetPins("ATM")
	if p != nil && len(p.Boards) != 0 {
		t.Errorf("pins after second toggle = %v, want empty", p.Boards)
	}
}

func TestJumpPinSelectsNth(t *testing.T) {
	m := newTestModel(t)
	seedTask(t, m, "ATM", "open one", "ATM:status:open")
	m.boards.refresh()
	m.boards.selectDefault()
	first := m.boards.selected
	// Pin a second board if one exists; else pin first twice is a no-op. For a
	// deterministic test, pin first and verify jumpPin(1) selects it.
	m.boards.togglePin()
	if !m.boards.jumpPin(1) {
		t.Fatal("jumpPin(1) returned false with 1 pin")
	}
	if m.boards.selected != first {
		t.Errorf("after jumpPin(1), selected = %q, want %q", m.boards.selected, first)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tui/ -run 'TestTogglePin|TestJumpPin' -count=1`
Expected: FAIL — `togglePin`/`jumpPin` undefined.

- [ ] **Step 3: Implement**

In `internal/tui/labels.go`:

```go
// loadPins reads the project's pins from the store and prunes any whose board
// no longer exists. Called on project select and on refresh (cheap read).
func (b *boardsModel) loadPins() {
	b.pins = nil
	if b.m.projectScope == "" {
		return
	}
	p, err := b.m.store.GetPins(b.m.projectScope)
	if err != nil || p == nil {
		return
	}
	live := map[string]bool{}
	for _, r := range b.rows {
		live[r.FullName] = true
	}
	for _, full := range p.Boards {
		if live[full] {
			b.pins = append(b.pins, full)
		}
	}
}

// togglePin adds the selected board to the pin list (at the end) if absent, or
// removes it if present, then persists.
func (b *boardsModel) togglePin() {
	if b.selected == "" || b.m.projectScope == "" {
		return
	}
	out := b.pins[:0:0]
	pinned := false
	for _, full := range b.pins {
		if full == b.selected {
			pinned = true
			continue
		}
		out = append(out, full)
	}
	if !pinned {
		out = append(out, b.selected)
	}
	b.pins = out
	b.persistPins()
}

// jumpPin moves the ring selection to the nth pinned board (1-based). Returns
// false if n is out of range.
func (b *boardsModel) jumpPin(n int) bool {
	if n < 1 || n > len(b.pins) {
		return false
	}
	b.selected = b.pins[n-1]
	b.applyFocus()
	return true
}

func (b *boardsModel) persistPins() {
	if b.m.projectScope == "" {
		return
	}
	_ = b.m.store.WritePins(b.m.projectScope, &store.Pins{
		Actor:  b.m.actor,
		Boards: b.pins,
	})
}
```

Add `"atm/internal/store"` import if not present (it is already imported in `labels.go`).

In `boardsModel.refresh`, after `b.clampCursor()`, call `b.loadPins()`.

In `internal/tui/projects.go`, after `p.m.boards.selectDefault()` (added in Task 4), add `p.m.boards.loadPins()`.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/tui/ -run 'TestTogglePin|TestJumpPin' -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/labels.go internal/tui/labels_test.go internal/tui/projects.go
git commit -m "feat(tui): board pinning with per-project persistence"
```

---

## Task 7: Merge into `tasksModel` — strip + list + pinned row layout

**Files:**
- Modify: `internal/tui/tasks.go` (`tasksModel.SetSize` reserves strip height; `View` renders strip + list + pinned row; `handleListKey` adds `[]`/`>`/`<`/`p`/`Shift-N`)
- Test: `internal/tui/tasks_test.go`

**Interfaces:**
- Produces: `tasksModel` renders the strip above the list and the pinned row below; `[]`/`>`/`<`/`p`/`Shift-N` dispatched here.

- [ ] **Step 1: Write the failing test**

```go
func TestTasksPaneRendersStripAndPinnedRow(t *testing.T) {
	m := newTestModel(t)
	if err := workflow.EnsureVocabulary(m.store, "ATM", m.actor); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	seedTask(t, m, "ATM", "open one", "ATM:status:open")
	m.boards.refresh()
	m.boards.selectDefault()
	v := m.tasks.View()
	if !strings.Contains(v, "open-tasks") {
		t.Errorf("tasks view missing strip board name:\n%s", v)
	}
}

func TestBracketKeysSwitchBoard(t *testing.T) {
	m := newTestModel(t)
	seedTask(t, m, "ATM", "open one", "ATM:status:open")
	m.boards.refresh()
	m.boards.selectDefault()
	first := m.boards.selected
	m.tasks.handleKey(keyMsg("]"))
	if m.boards.selected == first {
		t.Error("] did not advance the board ring")
	}
	m.tasks.handleKey(keyMsg("["))
	if m.boards.selected != first {
		t.Errorf("[ did not return to first board: got %q want %q", m.boards.selected, first)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tui/ -run 'TestTasksPaneRendersStripAndPinnedRow|TestBracketKeysSwitchBoard' -count=1`
Expected: FAIL.

- [ ] **Step 3: Implement**

In `internal/tui/tasks.go`:

Add a strip-height constant and reserve it in `SetSize`:

```go
const stripHeight = 8 // board thumbnail strip; clamps down on short terminals
```

In `tasksModel.SetSize`, after setting `t.contentHeight = h`, subtract the strip unconditionally. Do NOT read `t.m.boards.pins` here — the pinned row's extra line is handled in the render path only, so a pin toggle can never leave a stale `pageSize` (SetSize is not re-run on pin changes):

```go
func (t *tasksModel) SetSize(w, h int) {
	if w < 1 { w = 1 }
	if h < 1 { h = 1 }
	t.width = w
	t.contentHeight = h
	t.pageSize = h - stripHeight - 6
	if t.pageSize < 1 { t.pageSize = 1 }
}
```

Rewrite `tasksModel.View` to render the strip + list + pinned row (only in list view; detail view keeps the full pane since the strip is contextual to browsing):

```go
func (t *tasksModel) View() string {
	switch t.view {
	case tViewList:
		return t.renderListWithStrip()
	case tViewDetail:
		return t.renderDetailView()
	}
	return ""
}
```

Implement `renderListWithStrip` by calling the existing `renderList()` with a temporarily reduced `contentHeight` — no refactor of `renderList` itself:

```go
func (t *tasksModel) renderListWithStrip() string {
	strip := t.m.boards.renderStrip(t.width, stripHeight)
	pinned := t.m.boards.renderPinnedRow(t.width)
	listH := t.contentHeight - stripHeight
	if pinned != "" {
		listH--
	}
	if listH < 4 { listH = 4 }
	savedH := t.contentHeight
	savedPageSize := t.pageSize
	t.contentHeight = listH
	t.pageSize = listH - 6
	if t.pageSize < 1 { t.pageSize = 1 }
	listOut := t.renderList()
	t.contentHeight = savedH
	t.pageSize = savedPageSize
	var b strings.Builder
	b.WriteString(strip)
	b.WriteString("\n")
	b.WriteString(listOut)
	if pinned != "" {
		b.WriteString("\n")
		b.WriteString(pinned)
	}
	return padToHeight(b.String(), t.contentHeight)
}
```

(`renderList` already pads to `t.contentHeight`, so the joined output height is strip + listH + optional pinned ≈ contentHeight. The trailing `padToHeight` clamps any rounding.)

Add key handling in `tasksModel.handleListKey`. The existing `case "]":` / `case "[":` paging bodies (`tasks.go:479-484`) are REPLACED by the board-ring dispatch; paging relocates to `pgdown` / `pgup`:

```go
	case "[", "]":
		dir := -1
		if k.String() == "]" { dir = 1 }
		t.m.boards.cycleBoard(dir)
	case "pgdown":
		t.cursor += t.listPageSize()
		t.clampCursor()
	case "pgup":
		t.cursor -= t.listPageSize()
		t.clampCursor()
	case ">", "<":
		// Drill the SELECTED thumbnail in / out via boardsModel's level navigation.
		if k.String() == ">" {
			t.m.boards.drillIn()
		} else {
			t.m.boards.drillOut()
		}
	case "{", "}":
		// Move the SELECTED thumbnail's chart cursor (the member that >, d, l target).
		dir := -1
		if k.String() == "}" { dir = 1 }
		t.m.boards.chartCursorMove(dir)
	case "p":
		t.m.boards.togglePin()
	case "!", "@", "#", "$", "%", "^", "&", "*", "(":
		n := shiftDigitToInt(k.String())
		t.m.boards.jumpPin(n)
```

Add the helper:

```go
func shiftDigitToInt(k string) int {
	switch k {
	case "!": return 1
	case "@": return 2
	case "#": return 3
	case "$": return 4
	case "%": return 5
	case "^": return 6
	case "&": return 7
	case "*": return 8
	case "(": return 9
	}
	return 0
}
```

`drillIn`/`drillOut`/`chartCursorMove` are added to `boardsModel` in Task 8.

- [ ] **Step 4: Run test to verify it passes (with Task 8 stubs)**

Since `drillIn`/`drillOut`/`chartCursorMove` are defined in Task 8, add minimal stubs now in `labels.go` so this compiles:

```go
func (b *boardsModel) drillIn()               {} // Task 8 fills in
func (b *boardsModel) drillOut()              {} // Task 8 fills in
func (b *boardsModel) chartCursorMove(int)    {} // Task 8 fills in
```

Run: `go test ./internal/tui/ -run 'TestTasksPaneRendersStripAndPinnedRow|TestBracketKeysSwitchBoard' -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/tasks.go internal/tui/tasks_test.go internal/tui/labels.go
git commit -m "feat(tui): merge board strip + pinned row into Tasks pane layout"
```

---

## Task 8: Thumbnail drill-down `>` / `<` via `boardsModel`

**Files:**
- Modify: `internal/tui/labels.go` (replace `drillIn`/`drillOut` stubs with real level navigation; `renderStrip`'s SELECTED cell honors the drill level)
- Modify: `internal/tui/thumbnails.go` (SELECTED cell renders the *current drill level* of the selected board, not always L0)
- Test: `internal/tui/labels_test.go`

**Interfaces:**
- Produces: `(*boardsModel).drillIn()`, `(*boardsModel).drillOut()`.

- [ ] **Step 1: Write the failing test**

```go
func TestDrillIntoNamespaceChart(t *testing.T) {
	m := newTestModel(t)
	seedTask(t, m, "ATM", "open one", "ATM:status:open")
	m.boards.refresh()
	// Select the status:* namespace board.
	for i, r := range m.boards.rows {
		if r.Expandable && r.Name == "status" {
			m.boards.selected = r.FullName
			_ = i
			break
		}
	}
	m.boards.applyFocus()
	m.boards.drillIn()
	if m.boards.level != lLevelChart {
		t.Errorf("level = %v, want lLevelChart after drillIn on namespace", m.boards.level)
	}
	m.boards.drillOut()
	if m.boards.level != lLevelTable {
		t.Errorf("level = %v, want lLevelTable after drillOut", m.boards.level)
	}
}

func TestChartCursorMoveTargetsMember(t *testing.T) {
	m := newTestModel(t)
	seedTask(t, m, "ATM", "open one", "ATM:status:open")
	seedTask(t, m, "ATM", "done one", "ATM:status:done")
	m.boards.refresh()
	for _, r := range m.boards.rows {
		if r.Expandable && r.Name == "status" {
			m.boards.selected = r.FullName
			break
		}
	}
	m.boards.applyFocus()
	m.boards.drillIn() // -> chart
	if m.boards.chartCursorMove(0); m.boards.cursor != 0 {
		t.Fatalf("cursor = %d, want 0 at chart entry", m.boards.cursor)
	}
	m.boards.chartCursorMove(1)
	if m.boards.cursor != 1 {
		t.Errorf("cursor = %d after move, want 1", m.boards.cursor)
	}
	m.boards.chartCursorMove(-1)
	if m.boards.cursor != 0 {
		t.Errorf("cursor = %d after move back, want 0", m.boards.cursor)
	}
}

func TestDrillOutOfLeafBoardKeepsBoardFocus(t *testing.T) {
	m := newTestModel(t)
	if err := workflow.EnsureVocabulary(m.store, "ATM", m.actor); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	seedTask(t, m, "ATM", "open one", "ATM:status:open")
	m.boards.refresh()
	m.boards.selectDefault() // SELECTED = ATM:open-tasks (leaf board)
	m.boards.drillIn()       // leaf board -> its detail
	if m.boards.level != lLevelDetail {
		t.Fatalf("level = %v, want lLevelDetail", m.boards.level)
	}
	m.boards.drillOut() // back to L0 — board focus must be re-applied, not cleared
	if m.boards.level != lLevelTable {
		t.Fatalf("level = %v, want lLevelTable", m.boards.level)
	}
	// The task list must still be filtered by the SELECTED board (assert via
	// the tasks pane's focus caption / filter — check the exact accessor in
	// tasks.go; the invariant is: NOT the unfiltered focusOff+"" state).
	if got := m.tasks.focusCaption(); !strings.Contains(got, "open-tasks") {
		t.Errorf("focus caption = %q, want it to reference open-tasks after drillOut", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tui/ -run 'TestDrillIntoNamespaceChart|TestChartCursorMove|TestDrillOutOfLeafBoard' -count=1`
Expected: FAIL (stubs are no-ops).

- [ ] **Step 3: Implement**

In `internal/tui/labels.go`, replace the stubs:

```go
// drillIn advances the SELECTED thumbnail one level deeper. For a namespace
// board: L0 -> chart. For a leaf board: no-op (detail is the only level). For
// a chart row under the cursor: chart -> that label's detail (or unset leaf).
func (b *boardsModel) drillIn() {
	if b.selected == "" || b.m.projectScope == "" {
		return
	}
	idx := b.ringIndex()
	if idx < 0 { return }
	r := b.rows[idx]
	switch b.level {
	case lLevelTable:
		if r.Expandable {
			b.enterChart(r.Name)
		} else {
			// Leaf board: show its detail.
			b.level = lLevelDetail
			b.detail = labelDetailState{row: labelRow{
				suffix:      strings.TrimPrefix(r.FullName, b.m.projectScope+":"),
				full:        r.FullName,
				description: r.Description,
				usage:       r.Count,
			}}
		}
	case lLevelChart:
		rows := b.chartRows()
		if b.cursor >= 0 && b.cursor < len(rows) {
			if rows[b.cursor].unset {
				b.enterUnsetLeaf()
				return
			}
			if rr, ok := b.chartLabelRow(); ok {
				b.enterDetail(rr)
			}
		}
	case lLevelDetail:
		// already at the deepest level; no-op
	}
}

// drillOut climbs the SELECTED thumbnail one level out: detail -> chart ->
// L0. At L0 it is a no-op. It must NOT route through enterTable(), whose
// setFocus(focusOff, "") would clear the task filter while a board is still
// SELECTED — climbing out re-applies the selected board's own focus instead.
func (b *boardsModel) drillOut() {
	switch b.level {
	case lLevelDetail:
		if b.ns != "" {
			// Came from a namespace chart (member detail or unset leaf):
			// climb back to the chart. reenterChart restores the facet focus.
			b.reenterChart()
			return
		}
		// Leaf board's detail -> L0; the SELECTED board keeps driving the list.
		b.level = lLevelTable
		b.detail = labelDetailState{}
		b.applyFocus()
	case lLevelChart:
		b.level = lLevelTable
		b.ns = ""
		b.cursor = 0
		b.applyFocus()
	}
}

// chartCursorMove moves the SELECTED thumbnail's chart cursor (the member row
// that >, d, l target). Only meaningful at the chart level; no-op elsewhere.
func (b *boardsModel) chartCursorMove(dir int) {
	if b.level != lLevelChart {
		return
	}
	rows := b.chartRows()
	if len(rows) == 0 {
		return
	}
	b.cursor += dir
	if b.cursor < 0 {
		b.cursor = 0
	}
	if b.cursor >= len(rows) {
		b.cursor = len(rows) - 1
	}
}
```

Note the discriminator in `drillOut`: `b.ns != ""` means the detail was reached through a namespace chart (`enterChart` set `ns`; `cycleBoard` clears it), while a leaf board's detail (set directly by `drillIn` at L0) leaves `ns` empty. Do not discriminate on `b.detail.leaf` — that field is only non-empty for the synthetic unset leaf, not for real chart members.

Update `renderSelectedCell` in `internal/tui/thumbnails.go` to honor `b.level` for the selected board instead of forcing chart/detail:

```go
func (b *boardsModel) renderSelectedCell(w, h int, r boardRow) string {
	savedW, savedH := b.width, b.contentHeight
	b.SetSize(w, h)
	defer func() {
		b.width, b.contentHeight = savedW, savedH
		b.pageSize = savedH - 2
		if b.pageSize < 1 { b.pageSize = 1 }
	}()
	var inner string
	switch b.level {
	case lLevelChart:
		inner = b.renderChart()
	case lLevelDetail:
		inner = b.renderDetail()
	default: // lLevelTable
		if r.Expandable {
			// Default view for a namespace at L0 is its chart.
			savedLevel, savedNS, savedCursor := b.level, b.ns, b.cursor
			b.level = lLevelChart
			b.ns = r.Name
			b.cursor = 0
			defer func() { b.level, b.ns, b.cursor = savedLevel, savedNS, savedCursor }()
			inner = b.renderChart()
		} else {
			savedLevel, savedDetail := b.level, b.detail
			b.level = lLevelDetail
			b.detail = labelDetailState{row: labelRow{
				suffix:      strings.TrimPrefix(r.FullName, b.m.projectScope+":"),
				full:        r.FullName,
				description: r.Description,
				usage:       r.Count,
			}}
			defer func() { b.level, b.detail = savedLevel, savedDetail }()
			inner = b.renderDetail()
		}
	}
	return titledBoxHeight(b.m.styles.PaneActive, w, r.Name, inner, h)
}
```

Note: when `b.level` is already `lLevelChart`/`lLevelDetail` from a prior drill, `b.ns`/`b.detail` must match the selected board. `drillIn` sets these via `enterChart`/`enterDetail`, and `cycleBoard` should reset the drill level on ring move — add to `cycleBoard` (after setting `b.selected`):

```go
	b.level = lLevelTable
	b.ns = ""
	b.cursor = 0
	b.detail = labelDetailState{}
```

so switching boards does not leak a stale chart/detail (or chart cursor) into the new selection.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/tui/ -run 'TestDrillIntoNamespaceChart|TestChartCursorMove|TestDrillOutOfLeafBoard' -count=1`
Expected: PASS.

- [ ] **Step 5: Run full suite**

Run: `go test ./internal/tui/ -count=1`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/tui/labels.go internal/tui/thumbnails.go internal/tui/labels_test.go
git commit -m "feat(tui): >/< drill the SELECTED board thumbnail levels"
```

---

## Task 9: CLI ensure wiring + conventions + help/keymap text updates

**Files:**
- Modify: `internal/cli/project.go` (`newProjectCreateCmd` ensures the open-tasks board after `CreateProject`)
- Modify: `internal/cli/label.go` (`newLabelSeedCmd` ensures the open-tasks board after `SeedLabels`)
- Modify: `internal/cli/project_test.go`, `internal/cli/label_test.go` (ensure assertions)
- Modify: `internal/cli/conventions.go` (first-contact sequence: replace `--label <CODE>:status:open` line with `--label <CODE>:open-tasks` + fallback note; structured JSON likewise)
- Modify: `internal/cli/conventions_test.go` (assert `open-tasks` present)
- Modify: `internal/cli/testdata/golden/` (regenerate affected goldens: conventions + project-create if its output changes)
- Modify: `internal/tui/keymap.go` (keymap table: `1/2`, `[]` = prev/next board in Tasks / page in Projects, `{}`/`><` = thumbnail cursor/drill, `PgDn/PgUp` = page task list, `p` = pin, `Shift-N` = jump)
- Modify: `internal/tui/help.go` (parity table Boards → Tasks)

- [ ] **Step 1: Write the failing tests**

In `internal/cli/conventions_test.go`, add (note: `conventionsText` is a package-level **const** at `conventions.go:11`, not a function):

```go
func TestConventionsMentionsOpenTasksBoard(t *testing.T) {
	if !strings.Contains(conventionsText, "open-tasks") {
		t.Error("conventions text must reference the open-tasks board in the first-contact sequence")
	}
	j := conventionsStructured()
	seq, _ := j["agent_first_contact_sequence"].([]string)
	joined := strings.Join(seq, " ")
	if !strings.Contains(joined, "open-tasks") {
		t.Error("agent_first_contact_sequence JSON must reference open-tasks")
	}
}
```

In `internal/cli/project_test.go`, add (harness pattern per `TestGoldenProjectCreate`):

```go
func TestProjectCreateEnsuresOpenTasksBoard(t *testing.T) {
	h := newGoldenHarness(t)
	sp := h.store.StorePath()
	h.run("init", "--store", sp, "--actor", "admin@cli:unset")
	_, _, code := h.run("project", "create", "--store", sp, "--code", "FOO", "--name", "Foo", "--actor", "admin@cli:unset")
	if code != 0 {
		t.Fatalf("exit = %d stderr=%s", code, h.stderr.String())
	}
	l, err := h.store.LabelShow("FOO:open-tasks")
	if err != nil {
		t.Fatalf("open-tasks board missing after project create: %v", err)
	}
	if l.Expr == "" {
		t.Error("open-tasks board has no expression")
	}
}
```

In `internal/cli/label_test.go`, add the analogous test running `label seed --project FOO` on a project whose open-tasks board was deleted (or on a store-created project that never had it), then asserting `LabelShow("FOO:open-tasks")` succeeds.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli/ -run 'TestConventionsMentionsOpenTasksBoard|TestProjectCreateEnsuresOpenTasksBoard|OpenTasks' -count=1`
Expected: FAIL.

- [ ] **Step 3: Wire the CLI ensures + update conventions**

In `internal/cli/project.go` `newProjectCreateCmd`, after the `s.CreateProject(...)` call succeeds and before `st.emit`:

```go
			if err := workflow.EnsureVocabulary(s, p.Code, actor); err != nil {
				return err
			}
```

In `internal/cli/label.go` `newLabelSeedCmd`, after `s.SeedLabels(project, actor)` succeeds:

```go
			if err := workflow.EnsureVocabulary(s, project, actor); err != nil {
				return err
			}
```

Add `"atm/internal/workflow"` to both files' imports.

In `internal/cli/conventions.go`, change line 76 from:

```
4. ` + "`atm task list --project <CODE> --label <CODE>:status:open`" + ` — get open work.
```

to:

```
4. ` + "`atm task list --project <CODE> --label <CODE>:open-tasks`" + ` — get open work. The open-tasks board (expression status:open) is ensured on project create / label seed / TUI use; in an older project where it is absent, ` + "`--label <CODE>:status:open`" + ` is equivalent.
```

In `conventionsStructured()` `agent_first_contact_sequence` (the `[]string` at line 146), change the matching entry (line ~150) to the same wording in plain text:

```go
"atm task list --project <CODE> --label <CODE>:open-tasks — get open work. The open-tasks board (expression status:open) is ensured on project create / label seed / TUI use; in an older project where it is absent, --label <CODE>:status:open is equivalent.",
```

Regenerate the goldens (the `-update` flag exists at `harness_test.go:48`):

```bash
go test ./internal/cli/ -run 'TestGolden' -count=1 -update
```

Then re-run WITHOUT `-update` and diff the golden changes — only the conventions fixtures (and project-create, if the ensure changed its output, which it should not) may differ.

- [ ] **Step 4: Update keymap + help**

In `internal/tui/keymap.go`, rewrite `keymapRows`:

```go
var keymapRows = []keyEntry{
	{"1/2", "focus pane", "focus pane", "-", "focus pane"},
	{"j/k", "move cursor", "move cursor", "-", "scroll"},
	{"g", "top of list", "top of list", "-", "top"},
	{"Enter", "open detail", "open detail", "-", "confirm overlay"},
	{"Esc", "back", "back", "-", "back / cancel overlay"},
	{"[ / ]", "prev/next page", "prev/next board", "-", "-"},
	{"> / <", "-", "drill board thumbnail in/out", "-", "-"},
	{"{ / }", "-", "move thumbnail chart cursor", "-", "-"},
	{"PgDn/PgUp", "-", "page task list", "-", "scroll (detail)"},
	{"s", "select project", "cycle sort", "-", "-"},
	{"S", "-", "-", "-", "-"},
	{"a", "add project", "add task", "-", "-"},
	{"n", "-", "new board", "-", "-"},
	{"e", "-", "edit board", "-", "edit title (task)"},
	{"d", "-", "describe label", "-", "edit description (task)"},
	{"l", "-", "remove label", "-", "-"},
	{"x", "remove project (confirm)", "-", "-", "remove task (confirm)"},
	{"b/B", "-", "-", "-", "add/remove label (task)"},
	{"p", "-", "pin board", "-", "-"},
	{"Shift-1..9", "-", "jump to pinned board", "-", "-"},
	{"N", "set name (project detail)", "-", "-", "-"},
	{"M", "-", "-", "-", "add comment (task)"},
	{"H", "toggle history (project detail)", "-", "-", "history overlay (task detail)"},
	{"P", "expand activity by persona", "-", "-", "-"},
	{"T", "cycle theme", "cycle theme", "cycle theme", "cycle theme"},
	{"?", "open keys help", "open keys help", "open keys help", "close help overlay"},
	{"C", "open conventions", "open conventions", "open conventions", "close help overlay"},
	{"g", "plugin prefix", "plugin prefix", "plugin prefix", "plugin prefix"},
	{"g 1", "open indexer overlay", "open indexer overlay", "open indexer overlay", "open indexer overlay"},
	{"q / ctrl+c", "quit", "quit", "quit", "quit"},
	{"Space", "-", "-", "-", "scroll down"},
}
```

(If the `keyEntry` struct's `Boards` column is now unused, leave it in the struct but set all values to `"-"`; removing the column is a larger change — keep it for stability.)

In `internal/tui/help.go`, change parity-table rows that say "Boards pane" to "Tasks pane (boards)":
- `atm label add --name --desc` → "Tasks pane [a]dd / [d]esc"
- `atm label remove --name` → "Tasks pane [l]"
- `atm label seed --project` → "Tasks pane [S]"
- `atm label list [--project] [--ns]` → "Tasks pane (boards strip)"
- `atm task list --facets` → "CLI wildcard faceting; TUI board strip (Tasks mirror)"

- [ ] **Step 5: Run tests + verify**

Run: `go test ./internal/cli/ -count=1 && go test ./internal/tui/ -run 'Help|Keymap' -count=1`
Expected: PASS.

Run: `make verify`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/cli/project.go internal/cli/project_test.go internal/cli/label.go internal/cli/label_test.go internal/cli/conventions.go internal/cli/conventions_test.go internal/cli/testdata/golden/ internal/tui/keymap.go internal/tui/help.go
git commit -m "feat(cli): ensure open-tasks board on project create/label seed; conventions + keymap/help updates"
```

---

## Task 10: Wiring, final test pass, and `make verify`

**Files:**
- Modify: `internal/tui/app.go` (ensure `handleKey` routes `Esc` correctly for the merged pane; remove any dead `paneLabels` references)
- Modify: any remaining tests referencing the old `[3]` behavior

- [ ] **Step 1: Audit dead references**

Run: `grep -rn "paneLabels\|splitRightColumnHeights\|\"\[3\] Boards\"" internal/`
Expected: no matches (all removed by Tasks 3-9). Fix any remaining.

- [ ] **Step 2: Esc routing**

In `internal/tui/app.go` `handleKey`, the `case "esc":` block references `m.focused == paneLabels` for the boards Esc. Remove that branch (the boards drill now uses `<`, not Esc). Keep the `paneProjects` and `paneTasks` Esc branches. The `paneTasks` branch should handle Esc from task detail / filter edit only.

- [ ] **Step 3: Run the full TUI suite**

Run: `go test ./internal/tui/ -count=1`
Expected: PASS. Fix any failures (likely stale tests referencing the old boards pane keyset — update them to the merged-pane keys).

- [ ] **Step 4: Run `make verify`**

Run: `make verify`
Expected: PASS (build + lint + typecheck + tests).

- [ ] **Step 5: Update the ATM task**

Run:
```bash
atm task comment add --task ATM-2412f2 --body "Implementation complete per plan docs/superpowers/plans/2026-07-15-tui-tasks-boards-merge.md. All 10 tasks done; make verify green. Spec: docs/superpowers/specs/2026-07-15-tui-tasks-boards-merge-design.md." --actor "<this session's actor, developer@<agent>:<model>>"
```

- [ ] **Step 6: Final commit (if any cleanup)**

```bash
git add -A
git commit -m "chore(tui): final wiring + test cleanup for merged Tasks pane"
```

---

## Self-Review

**Spec coverage:**
- Workspace layout (two panes, 40/60, [2] full height, strip/list/pinned stack) — Task 3 + Task 7.
- Board ring + Open Tasks default + stale-selection fallback — Task 4.
- Open Tasks capability ownership (`internal/workflow`, EnsureVocabulary, no privileged label, re-ensure on select) — Task 1 + Task 4.
- CLI ensure call sites (`project create`, `label seed` — no project-select path exists in the CLI) — Task 9.
- Thumbnail strip (25/50/25, reuse level render, small-ring dedupe) — Task 5 + Task 8.
- Navigation (arrows=tasks, []=boards, PgDn/PgUp=page tasks, Enter=task detail, ><=drill, {}=chart cursor, p=pin, Shift-N=jump, one focus modeless) — Task 7 + Task 8 + Task 6.
- Drill-out preserves the SELECTED board's task focus (never `enterTable`'s focus-clear) — Task 8.
- Pinning + persistence (`pins.json`, GetPins/WritePins) — Task 2 + Task 6.
- Conventions (with `status:open` fallback wording) + keymap/help — Task 9.
- Testing matrix (workflow, store pins, cli ensures, tui merged pane, narrow terminals) — embedded in each task; Task 10 runs the full gate.

**Placeholder scan:** No TBD/TODO/"add appropriate". Each code step contains real code.

**Type consistency:** `selected string`, `cycleBoard(int)`, `ringIndex() int`, `selectDefault()`, `togglePin()`, `jumpPin(int) bool`, `drillIn()`/`drillOut()`/`chartCursorMove(int)`, `renderStrip(int,int) string`, `renderPinnedRow(int) string`, `splitStripWidths(int) (int,int,int)` — used consistently across tasks. `store.Pins{Actor, Boards, UpdatedAt}` matches `GetPins`/`WritePins`. `workflow.BoardOpenTasks(string) string` + `workflow.EnsureVocabulary(*store.Store, string, string) error` match usages.

**Amendments (2026-07-15 code-verification review, ATM-2412f2-c4c8f):** CLI ensure moved from the nonexistent "project-select path" to `project create` + `label seed`; `{`/`}` chart-cursor keys added (arrows are task-scoped, so the chart cursor was otherwise unreachable); `drillOut` re-applies the selected board's focus instead of `enterTable`'s focus-clear (discriminating chart-origin by `b.ns`, not `b.detail.leaf`); task-list paging moved to `PgDn`/`PgUp` and the Projects pane's `[`/`]` paging kept in the keymap; `tasksModel.SetSize` decoupled from `boards.pins`; test snippets corrected to the real helpers (`keyMsg`/`update`, `conventionsText` const, existing `mustNotContain`); small-ring strip dedupe; stale-selection fallback in `refresh()`.