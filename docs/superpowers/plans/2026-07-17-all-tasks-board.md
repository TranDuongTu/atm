# all-tasks board Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add an `all-tasks` board (expr `*`) owned by `internal/workflow`, make it the TUI's default-selected board, and add a `*` tautology atom to the board-expression language that matches every task including unlabeled ones.

**Architecture:** A single new atom `*` short-circuits the expression evaluator (`internal/store/resolve.go` `evalAtom`) to true before qualification, so it never gets misread as the `<CODE>:*` namespace wildcard. `internal/workflow/vocabulary.go` seeds a fourth board `<CODE>:all-tasks` with expr `*` and updates the `open-tasks` description string (fresh seeds only — `LabelSeed` is create-only, a tested contract). `internal/tui/labels.go` `selectDefault` swaps its `want` from `BoardOpenTasks` to `BoardAllTasks`. The default sort is already `updated-desc`, so no sort work is needed.

**Tech Stack:** Go 1.22+; the `atm` single binary (CLI + Bubble Tea TUI). Tests are plain `go test`. No external libraries.

## Global Constraints

- Go 1.22+ (per AGENTS.md §4).
- No emojis in code or commits (AGENTS.md §5).
- Follow existing style in neighboring files (AGENTS.md §5).
- Keep the API surface stable and versioned; the TUI consumes it (AGENTS.md §5).
- `make verify` (or `make build && make test`) is the verification gate (AGENTS.md §4, superpowers context).
- The board-expression grammar is defined in `internal/core/expr.go`; operators are case-sensitive (`AND`/`OR`/`NOT`); atoms are bare label names with the project prefix omitted. A board is a `core.Label` with a non-empty `Expr`. Evaluation lives in `internal/store/resolve.go`.
- `EnsureVocabulary` MUST keep using `LabelSeed` only (never `LabelAdd`) — overwriting an existing label's description is forbidden by `TestEnsureVocabularyDoesNotOverwriteHumanDescription` (`internal/workflow/vocabulary_test.go:54-70`). The updated `open-tasks` description lands only on fresh projects.

## File Structure

- **Modify** `internal/core/expr.go` — no change required (the lexer/parser already accept `*`); only tests extend the contract. Listed for completeness.
- **Modify** `internal/store/resolve.go` — add the `*` short-circuit at the top of `evalAtom`.
- **Modify** `internal/workflow/vocabulary.go` — add `BoardAllTasks`, `allTasksExpr`, the `all-tasks` board entry in `EnsureVocabulary`, and the updated `open-tasks` description string.
- **Modify** `internal/tui/labels.go` — `selectDefault` targets `BoardAllTasks` instead of `BoardOpenTasks`; update the doc comment.
- **Modify (tests)** `internal/core/expr_test.go` — add `*` parsing cases.
- **Modify (tests)** `internal/store/resolve_test.go` — add `*` tautology evaluation cases (matches unlabeled and labeled tasks; composes with `NOT`).
- **Modify (tests)** `internal/workflow/vocabulary_test.go` — assert `all-tasks` seeded with expr `*`; assert fresh-seed `open-tasks` description; assert re-seed preserves a human-curated `all-tasks` description.
- **Modify (tests)** `internal/tui/labels_test.go` — rename `TestSelectDefaultPicksOpenTasksBoard` → `...PicksAllTasksBoard`; add `TestSelectDefaultOpenTasksRemainsSelectableInRing`.
- **Modify (tests)** `internal/cli` golden files (if a golden covers `task list` board output) — extend with an `all-tasks` / `*` case including an unlabeled task.

---

### Task 1: `*` tautology atom in the expression evaluator

This is the foundation. No board or TUI change depends on a *named* symbol from this task except the string `"*"`, which the later tasks hardcode in their own data. The contract this task produces is behavioral: a bare `*` atom (in any board's `Expr`, or as a standalone restricting filter token) evaluates to true for every task, including unlabeled ones.

**Files:**
- Modify: `internal/store/resolve.go:69-108` (`evalAtom`)
- Test: `internal/store/resolve_test.go` (extend), `internal/core/expr_test.go` (extend)

**Interfaces:**
- Consumes: `core.ParseExpr` (unchanged), `core.IsNamespaceName` (unchanged), the `resolver` struct.
- Produces: the behavioral contract that `evalAtom("*", ...)` returns `(true, nil)` without consulting labels, and that `"*"` composes with `AND`/`OR`/`NOT` like any other atom. Later tasks rely on this: the `all-tasks` board's expr `*` (Task 3) and the CLI `--label '*'` idiom both reach this code path.

- [ ] **Step 1: Write the failing parser test**

Add to `internal/core/expr_test.go`:

```go
func TestParseExprStarAtom(t *testing.T) {
	// The bare '*' tautology atom: lexes as a single token and parses as an
	// ExprAtom. It is the membership predicate of the all-tasks board and a
	// reusable standalone filter token.
	n, err := ParseExpr("*")
	if err != nil {
		t.Fatalf("ParseExpr(%q): %v", "*", err)
	}
	a, ok := n.(*ExprAtom)
	if !ok || a.Name != "*" {
		t.Fatalf("ParseExpr(%q) = %#v, want ExprAtom{%q}", "*", n, "*")
	}
}

func TestParseExprStarComposes(t *testing.T) {
	// '*' is a normal atom: it composes with AND/OR/NOT like any other.
	n, err := ParseExpr("* AND NOT status:done")
	if err != nil {
		t.Fatalf("ParseExpr: %v", err)
	}
	if _, ok := n.(*ExprAnd); !ok {
		t.Fatalf("root = %T, want *ExprAnd", n)
	}
	got := Atoms(n)
	want := []string{"*", "status:done"}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("Atoms = %v, want %v", got, want)
	}
}
```

- [ ] **Step 2: Run the parser test to verify it passes already**

Run: `go test ./internal/core/ -run TestParseExprStar -v`
Expected: PASS (the lexer already accepts `*` as a default character, and `parseAtom` already accepts it since it is none of `(`, `)`, `AND`, `OR`, `NOT`).

- [ ] **Step 3: Write the failing evaluator test**

Add to `internal/store/resolve_test.go`:

```go
// TestResolverStarTautologyMatchesEveryTask covers the all-tasks board's
// membership predicate: a bare '*' atom evaluates to true for every task,
// including unlabeled naked jottings, and composes with NOT/AND like any
// other atom. The short-circuit must fire before qualify, or '*' would be
// read as the <CODE>:* namespace wildcard ("has any label") and miss
// unlabeled tasks.
func TestResolverStarTautologyMatchesEveryTask(t *testing.T) {
	r := resolverFor() // no labels needed: '*' never consults them
	labeled := &Task{ID: "ATM-0001", Labels: []string{"ATM:status:open"}}
	unlabeled := &Task{ID: "ATM-0002", Labels: nil}

	cases := map[string]bool{
		"*":                  true, // tautology: matches labeled ...
		"* AND NOT *":        false,
		"* AND NOT status:done": true,  // labeled task: * true, NOT status:done true
		"* AND NOT status:open": false, // labeled task: NOT status:open false
	}
	for src, want := range cases {
		n, err := ParseExpr(src)
		if err != nil {
			t.Fatalf("ParseExpr(%q): %v", src, err)
		}
		got, err := r.Matches(labeled, n)
		if err != nil {
			t.Fatalf("Matches(%q) labeled: %v", src, err)
		}
		if got != want {
			t.Errorf("Matches(%q) labeled = %v, want %v", src, got, want)
		}
	}

	// The load-bearing case: an unlabeled task matches '*'. Without the
	// short-circuit, qualify('*') yields "ATM:*", IsNamespaceName reads it
	// as "has any label", and a task with no labels returns false.
	n, _ := ParseExpr("*")
	got, err := r.Matches(unlabeled, n)
	if err != nil {
		t.Fatalf("Matches(%q) unlabeled: %v", "*", err)
	}
	if !got {
		t.Errorf("Matches(%q) unlabeled = false, want true (the whole point of the all-tasks board)", "*")
	}
}

func TestResolverStarStandaloneRestrictingToken(t *testing.T) {
	// A bare '*' as a restricting filter token (CLI --label '*') reaches
	// evalAtom with Name="*" (TrimPrefix("*", "ATM:") is still "*"). The
	// short-circuit must fire before qualify turns it into "ATM:*".
	r := resolverFor()
	task := &Task{ID: "ATM-0001", Labels: nil}
	// Simulate the ListTasksErr path: AtomNode{Name: TrimPrefix("*", "ATM:")} .
	n, _ := ParseExpr("*")
	got, err := r.Matches(task, n)
	if err != nil || !got {
		t.Fatalf("standalone '*' on unlabeled task: got %v (err %v), want true, nil", got, err)
	}
}
```

- [ ] **Step 4: Run the evaluator test to verify it fails**

Run: `go test ./internal/store/ -run TestResolverStar -v`
Expected: FAIL — `Matches("*") unlabeled = false, want true`. Without the short-circuit, `qualify("*")` yields `"ATM:*"`, `IsNamespaceName` is true, and the namespace-predicate branch returns false for a task with no labels.

- [ ] **Step 5: Write the minimal implementation**

Edit `internal/store/resolve.go`, in `evalAtom`, add the short-circuit as the first statement, before `full := r.qualify(atom)`:

```go
func (r *resolver) evalAtom(t *Task, atom string, visiting map[string]bool) (bool, error) {
	// The bare '*' tautology atom: matches every task, including unlabeled
	// ones. MUST short-circuit before qualify — qualify("*") yields
	// "<CODE>:*", which IsNamespaceName reads as the namespace predicate
	// "has any label" and so misses naked unlabeled jottings. See
	// docs/superpowers/specs/2026-07-17-all-tasks-board-design.md.
	if atom == "*" {
		return true, nil
	}
	full := r.qualify(atom)
	// ... rest unchanged ...
```

- [ ] **Step 6: Run the evaluator test to verify it passes**

Run: `go test ./internal/store/ -run TestResolverStar -v`
Expected: PASS.

- [ ] **Step 7: Run the full store + core test suites to confirm no regression**

Run: `go test ./internal/store/ ./internal/core/ ./...`
Expected: PASS (all packages).

- [ ] **Step 8: Commit**

```bash
git add internal/store/resolve.go internal/store/resolve_test.go internal/core/expr_test.go
git commit -m "feat(ATM-18111b): bare '*' tautology atom in board expression evaluator

Short-circuit evalAtom before qualify so '*' matches every task
including unlabeled naked jottings; qualify('*') would yield <CODE>:*
and be misread as the namespace 'has any label' predicate. The lexer
and parser already accept '*'; only evalAtom changes. Composes with
AND/OR/NOT like any other atom."
```

---

### Task 2: `all-tasks` board vocabulary + `open-tasks` description update

Seeds the fourth workflow board and updates the `open-tasks` description string for fresh seeds. Existing projects keep their current `open-tasks` description (the never-overwrite contract).

**Files:**
- Modify: `internal/workflow/vocabulary.go` (whole file, 44 lines)
- Test: `internal/workflow/vocabulary_test.go` (extend)

**Interfaces:**
- Consumes: `core.LabelService.LabelSeed(name, description, expr, actor) error` (unchanged); `core.LabelService.LabelAdd` is NOT used. From Task 1: the behavioral contract that expr `*` evaluates to true for every task.
- Produces: `workflow.BoardAllTasks(code string) string` — returns `<code>:all-tasks`. Consumed by Task 3 (`internal/tui/labels.go` `selectDefault`). Also produces the `allTasksExpr()` helper (unexported, used only inside this file).

- [ ] **Step 1: Write the failing vocabulary test**

Add to `internal/workflow/vocabulary_test.go`:

```go
func TestEnsureVocabularySeedsAllTasksBoard(t *testing.T) {
	s := newTestStore(t)
	if err := EnsureVocabulary(s, "ATM", "admin@cli:unset"); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	l, err := s.LabelShow(BoardAllTasks("ATM"))
	if err != nil {
		t.Fatalf("label show: %v", err)
	}
	if l.Expr != "*" {
		t.Errorf("all-tasks expr = %q, want %q", l.Expr, "*")
	}
	if l.Description == "" {
		t.Error("all-tasks board has no description")
	}
}

func TestEnsureVocabularyFreshOpenTasksDescriptionDropsDefaultClause(t *testing.T) {
	// On a fresh project (open-tasks does not yet exist), LabelSeed writes
	// the new description that drops the "Default board in the TUI." clause
	// (all-tasks now holds that role). Existing projects keep their current
	// description (the never-overwrite contract); that path is covered by
	// TestEnsureVocabularyDoesNotOverwriteHumanDescription.
	s := newTestStore(t)
	if err := EnsureVocabulary(s, "ATM", "admin@cli:unset"); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	l, err := s.LabelShow(BoardOpenTasks("ATM"))
	if err != nil {
		t.Fatalf("label show: %v", err)
	}
	if l.Description == "" {
		t.Fatal("open-tasks board has no description")
	}
	if strings.Contains(l.Description, "Default board in the TUI") {
		t.Errorf("open-tasks description = %q, still references 'Default board'; all-tasks is now the default", l.Description)
	}
}

func TestEnsureVocabularyPreservesHumanAllTasksDescription(t *testing.T) {
	// Extends the never-overwrite contract to all-tasks: a human-curated
	// all-tasks description survives a re-ensure, exactly as open-tasks and
	// backlog do (TestEnsureVocabularyDoesNotOverwriteHumanDescription /
	// TestEnsureVocabularyPreservesHumanBacklogDescription).
	s := newTestStore(t)
	humanDesc := "curated by a human"
	if err := s.LabelAdd(BoardAllTasks("ATM"), humanDesc, "*", "admin@cli:unset"); err != nil {
		t.Fatalf("seed human label: %v", err)
	}
	if err := EnsureVocabulary(s, "ATM", "admin@cli:unset"); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	l, err := s.LabelShow(BoardAllTasks("ATM"))
	if err != nil {
		t.Fatalf("label show: %v", err)
	}
	if l.Description != humanDesc {
		t.Errorf("all-tasks description = %q, want %q (human curation must survive ensure)", l.Description, humanDesc)
	}
}
```

Also add `"strings"` to the import block at the top of `internal/workflow/vocabulary_test.go` if not already present (it is not — current imports are `path/filepath` and `testing`).

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/workflow/ -run TestEnsureVocabularySeedsAllTasksBoard -v`
Expected: FAIL — `BoardAllTasks` is undefined and `s.LabelShow(BoardAllTasks("ATM"))` errors (label absent).

- [ ] **Step 3: Write the minimal implementation**

Edit `internal/workflow/vocabulary.go`. Add the `BoardAllTasks` helper after `BoardInProgressTasks` (line 22):

```go
// BoardAllTasks returns the full name of the All Tasks board: every task in
// the project, including unlabeled naked jottings. Its membership predicate
// is the '*' tautology atom (internal/store/resolve.go). Surfaced as the
// TUI's default-selected board so the human's "browse recent activity"
// consult mode sees the whole project, not just status:open.
func BoardAllTasks(code string) string { return code + ":all-tasks" }
```

Add the expr helper next to the other expr helpers (after line 26):

```go
func allTasksExpr() string { return "*" }
```

Update the `boards` slice in `EnsureVocabulary` (line 33-37) to add the `all-tasks` entry and drop the "Default board in the TUI." clause from `open-tasks`:

```go
	boards := []struct{ name, desc, expr string }{
		{BoardBacklog(code), "tasks with no status label: incoming jottings awaiting triage. Review queue alongside open-tasks.", backlogExpr()},
		{BoardOpenTasks(code), "every open task: the project's active work.", openTasksExpr()},
		{BoardInProgressTasks(code), "tasks someone is actively working on (status:in-progress).", inProgressTasksExpr()},
		{BoardAllTasks(code), "every task in the project, ordered by recent activity. Default board in the TUI.", allTasksExpr()},
	}
```

Update the `EnsureVocabulary` doc comment (line 28-31) to mention four boards:

```go
// EnsureVocabulary creates the four workflow boards with descriptions, if
// absent. Idempotent: LabelSeed upserts only when the label is absent, so a
// human's curated description is never overwritten. Self-bootstrapping: it
// does not assume `atm label seed` ran.
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/workflow/ -v`
Expected: PASS (all vocabulary tests, including the three new ones and the existing never-overwrite tests).

- [ ] **Step 5: Run the full test suite to confirm no regression**

Run: `go test ./...`
Expected: PASS. Note: existing tests that reference `workflow.BoardOpenTasks` still pass (the symbol is unchanged). `TestSelectDefaultPicksOpenTasksBoard` in `internal/tui/labels_test.go` will now FAIL because `selectDefault` still selects `open-tasks` — that is expected and is fixed in Task 3. If you run `./...` here, expect one failure in `internal/tui`; that is the green test for Task 3's setup, not a regression.

- [ ] **Step 6: Commit**

```bash
git add internal/workflow/vocabulary.go internal/workflow/vocabulary_test.go
git commit -m "feat(ATM-18111b): seed all-tasks board (expr '*') and update open-tasks description

BoardAllTasks(code) -> '<code>:all-tasks' with expr '*', the tautology
from the previous commit. open-tasks description drops 'Default board
in the TUI.' on fresh seeds; existing projects keep their current
description (LabelSeed is create-only, per the never-overwrite
contract). EnsureVocabulary now seeds four boards."
```

---

### Task 3: default-select `all-tasks` in the TUI

One-symbol swap in `selectDefault`. Existing `open-tasks` becomes a normal selectable ring member.

**Files:**
- Modify: `internal/tui/labels.go:249-272` (`selectDefault` body + doc comment)
- Test: `internal/tui/labels_test.go:16-29` (rename + rewrite), extend with a ring-membership test

**Interfaces:**
- Consumes: `workflow.BoardAllTasks(code)` (produced by Task 2). `EnsureVocabulary` is already called on project select before `selectDefault` (wired by the workflow-capability spec, `2026-07-16-workflow-capability-design.md` line 96), so `all-tasks` exists in the ring when `selectDefault` runs.
- Produces: the TUI behavior that `m.boards.selected == workflow.BoardAllTasks(scope)` after `selectDefault()`, which the rest of `boardsModel` consumes unchanged (it only reads `b.selected`).

- [ ] **Step 1: Write the failing test (rewrite the existing default-selection test)**

In `internal/tui/labels_test.go`, replace `TestSelectDefaultPicksOpenTasksBoard` (lines 16-29) with:

```go
func TestSelectDefaultPicksAllTasksBoard(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	m.projectScope = "ATM"
	if err := workflow.EnsureVocabulary(m.store, "ATM", m.actor); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	seedTask(t, m, "ATM", "open one", "ATM:status:open")
	m.boards.refresh()
	m.boards.selectDefault()
	if m.boards.selected != workflow.BoardAllTasks("ATM") {
		t.Errorf("selected = %q, want ATM:all-tasks", m.boards.selected)
	}
}
```

Then add a new test asserting `open-tasks` remains selectable in the ring after the default swap:

```go
// TestSelectDefaultOpenTasksRemainsSelectableInRing guards the demote-
// not-remove decision: all-tasks becomes the default, but open-tasks stays
// in the board ring as a normal selectable member. A single [ press from
// the all-tasks default must be able to reach it.
func TestSelectDefaultOpenTasksRemainsSelectableInRing(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	m.projectScope = "ATM"
	if err := workflow.EnsureVocabulary(m.store, "ATM", m.actor); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	seedTask(t, m, "ATM", "open one", "ATM:status:open")
	m.boards.refresh()
	m.boards.selectDefault()
	if m.boards.selected != workflow.BoardAllTasks("ATM") {
		t.Fatalf("precondition: selected = %q, want ATM:all-tasks", m.boards.selected)
	}
	// Cycle the ring until open-tasks is selected; it MUST be present.
	found := false
	for i := 0; i < len(m.boards.rows); i++ {
		m.boards.cycleBoard(1)
		if m.boards.selected == workflow.BoardOpenTasks("ATM") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("open-tasks not reachable by cycling from all-tasks; ring = %v", m.boards.rowNames())
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/tui/ -run TestSelectDefaultPicksAllTasksBoard -v`
Expected: FAIL — `selected = "ATM:open-tasks", want ATM:all-tasks` (the `selectDefault` body still references `BoardOpenTasks`).

Run: `go test ./internal/tui/ -run TestSelectDefaultOpenTasksRemainsSelectableInRing -v`
Expected: PASS already (the ring already contains both boards once `EnsureVocabulary` is called; this test just pins the contract).

- [ ] **Step 3: Write the minimal implementation**

Edit `internal/tui/labels.go`, `selectDefault` (lines 255-272). Change the `want` line and the doc comment:

```go
// selectDefault selects the All Tasks board if present, else the first ring
// board. Called on project select after EnsureVocabulary, and from refresh()
// when the previously selected board vanished mid-session — that fallback can
// fire while a chart/detail is drilled into the now-vanished board, so this
// always resets the drill state for the same leak-prevention invariant as
// cycleBoard/jumpPin.
func (b *boardsModel) selectDefault() {
	b.resetDrill()
	b.pinFocus = -1 // the ring board becomes the active-filter highlight
	want := workflow.BoardAllTasks(b.m.projectScope)
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
```

Only the comment's "Open Tasks" → "All Tasks", the `want :=` symbol, and the doc comment header change. The fallback logic is byte-identical.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/tui/ -run TestSelectDefault -v`
Expected: PASS (both `PicksAllTasksBoard` and `OpenTasksRemainsSelectableInRing`, plus the other `TestSelectDefault*` pinFocus tests which only assert `pinFocus == -1`, not which board).

- [ ] **Step 5: Run the full TUI suite + the whole project to confirm no regression**

Run: `go test ./...`
Expected: PASS. Pay attention to any test that previously asserted `selectDefault` lands on `open-tasks` — search for `BoardOpenTasks` in `internal/tui/*_test.go` and confirm each either still passes (most assert `pinFocus`, not the board name) or was the one rewritten in Step 1. The project-switch test at `internal/tui/app_test.go:2162-2227` asserts `workflow.BoardOpenTasks("SCY")` — **read it before running**; if it asserts the default-selected board on project switch, it must be updated to `BoardAllTasks("SCY")`. The implementation in Step 3 does not touch `app_test.go`; if this test fails, update the asserted board name in that test as part of this step (it is the same one-symbol change).

- [ ] **Step 6: Commit**

```bash
git add internal/tui/labels.go internal/tui/labels_test.go
# add internal/tui/app_test.go too if Step 5 required it
git commit -m "feat(ATM-18111b): default-select the all-tasks board in the TUI

selectDefault now targets BoardAllTasks instead of BoardOpenTasks.
open-tasks stays in the ring as a normal selectable board (demoted,
not removed). The default sort is already updated-desc, so the
all-tasks board inherits 'browse recent agent activity' ordering
with no sort change."
```

---

### Task 4: CLI golden coverage for `all-tasks` and `*`

The board and the `*` atom already work end-to-end via `atm task list --label <CODE>:all-tasks` and `atm task list --label '*'` with no CLI code change (the store's `ListTasksErr` resolves the board's expr / the `*` atom through the same path). This task adds golden test coverage so the contract is pinned.

**Files:**
- Test: `internal/cli/task_test.go` (extend) or the relevant golden-harness file for `task list`

**Interfaces:**
- Consumes: the CLI `atm task list --project <CODE> --label <TOKEN>` flag (unchanged). From Tasks 1-2: `all-tasks` board exists with expr `*`; `*` evaluates to true for every task.
- Produces: pinned golden output proving `--label <CODE>:all-tasks` and `--label '*'` return every task including unlabeled ones.

- [ ] **Step 1: Locate the existing `task list` golden harness**

Run: `grep -n "func TestTaskList" internal/cli/task_test.go` (use ripgrep via the search tool, not bash grep). Identify the test that drives `task list` through the golden harness (the pattern is `newGoldenHarness` + `h.run("task", "list", ...)`, per the workflow-capability tests noted in the spec). Read that test to learn the exact harness setup (project create, seed tasks, golden file path convention).

- [ ] **Step 2: Write the failing test (or extend an existing one)**

Following the harness pattern found in Step 1, add a test that seeds a project with at least four tasks — one `status:open`, one `status:done`, one `status:in-progress`, and one **unlabeled naked jotting** (no labels) — then asserts:

```go
// TestTaskListAllTasksBoardReturnsEveryTask pins the all-tasks board
// (expr '*') and the standalone '*' filter token: both return every task,
// including unlabeled naked jottings, which the <CODE>:* namespace
// wildcard would miss.
func TestTaskListAllTasksBoardReturnsEveryTask(t *testing.T) {
	// Use the harness setup found in Step 1. Seed:
	//   task "open"     labels [ATM:status:open]
	//   task "done"     labels [ATM:status:done]
	//   task "wip"      labels [ATM:status:in-progress]
	//   task "naked"    labels []   (unlabeled jotting)
	// workflow.EnsureVocabulary(store, "ATM", actor) so all-tasks exists.

	// Assert h.run("task","list","--project","ATM","--label","ATM:all-tasks")
	// returns all four task titles in its stdout (text or JSON per the
	// harness's default).
	// Assert h.run("task","list","--project","ATM","--label","*") returns
	// the same four titles. The '*' must be passed as a literal arg (shell
	// quoting is the caller's concern; the harness calls the Go function
	// directly so no shell glob interferes).
}
```

Write out the full test body using the exact harness helpers found in Step 1 — do not leave the comment-only skeleton. Assert every title appears; assert the unlabeled task appears in both outputs (this is the load-bearing assertion that distinguishes `*` from `<CODE>:*`).

- [ ] **Step 3: Run the test to verify it passes (no CLI code change expected)**

Run: `go test ./internal/cli/ -run TestTaskListAllTasksBoardReturnsEveryTask -v`
Expected: PASS. If it FAILS, the cause is one of:
  - `all-tasks` board not seeded in the harness (call `workflow.EnsureVocabulary` after `CreateProject`).
  - The `*` arg is being glob-expanded by the test runner (it is not — the harness calls the cobra command directly; verify the arg is `"*"`).
  - A genuine bug in the `*` short-circuit (revisit Task 1).
Do NOT add CLI code to make this pass — the feature is complete at the store layer. Fix the test setup.

- [ ] **Step 4: Run the full CLI suite + whole project**

Run: `go test ./...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/cli/task_test.go
# add any new golden fixture files too
git commit -m "test(ATM-18111b): pin all-tasks board and '*' filter in CLI goldens

Seeds an unlabeled naked jotting alongside status:open/done/in-progress
tasks and asserts both 'atm task list --label ATM:all-tasks' and
'atm task list --label *' return every task. The unlabeled task is
the load-bearing assertion: it distinguishes the '*' tautology from
the <CODE>:* namespace wildcard, which misses unlabeled tasks."
```

---

### Task 5: verify (`make verify`) and ledger

Final gate. Run the repo's verification command, confirm a clean build, and record completion in the ATM ledger.

**Files:**
- No code changes. Optionally updates the ATM task ATM-18111b with a completion comment.

**Interfaces:**
- Consumes: the completed Tasks 1-4.

- [ ] **Step 1: Run the full verification gate**

Run: `make verify`
Expected: PASS (build + tests + any lint/format the Makefile wires). If `make verify` is not defined, fall back to `make build && make test` (AGENTS.md §4).

- [ ] **Step 2: Confirm a clean binary build**

Run: `make build` (or `go build ./...`)
Expected: PASS, exit 0.

- [ ] **Step 3: Record a completion comment on ATM-18111b**

Run:
```bash
atm task comment add --task ATM-18111b --label "ATM:comment:progress" --actor "<your actor>" --body "Implementation complete per plan docs/superpowers/plans/2026-07-17-all-tasks-board.md. Commits: Task 1 (evaluator short-circuit), Task 2 (vocabulary), Task 3 (TUI default), Task 4 (CLI goldens). make verify green. No store API change; EnsureVocabulary keeps using LabelSeed only; existing projects keep their current open-tasks description."
```
Replace `<your actor>` with the session actor (e.g. `developer@ollama:glm-5.2:cloud`).

- [ ] **Step 4: Do NOT mark the task done or merge**

Leave the task `status:open` (or let a human/manager move it to `status:done` after review). Do NOT commit, push, or open a PR unless the user explicitly asks. The finishing-a-development-branch skill is the user's to invoke.

---

## Self-Review Notes

- **Spec coverage:** Every spec section maps to a task. The `*` tautology (spec §"The tautology atom") → Task 1. The board + `open-tasks` description (spec §"The board") → Task 2. Default selection (spec §"Default selection") → Task 3. CLI (spec §"CLI") → Task 4. No sort change (spec §"No sort change") → intentionally no task (already done). Tests (spec §"Tests") → embedded in Tasks 1-4.
- **No placeholders:** every code step shows the actual code; every command shows the expected output.
- **Type consistency:** `BoardAllTasks` is the only new symbol, used identically in Task 2 (definition) and Task 3 (`selectDefault`). `allTasksExpr` is unexported and file-local; no cross-task reference.
- **Known cross-task test interaction:** Task 2 leaves `TestSelectDefaultPicksOpenTasksBoard` failing (it is rewritten in Task 3 Step 1). This is called out in Task 2 Step 5 and Task 3 Step 2. A worker executing tasks strictly in order will see it fixed at Task 3.