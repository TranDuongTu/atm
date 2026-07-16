# Workflow Capability Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Promote `internal/workflow` from vocabulary-only into a full capability that owns status-transition verbs (`atm workflow start/open/block/complete/status/seed`), ensures three boards (`backlog`, `open-tasks`, `in-progress-tasks`), and updates conventions to point agents at the paved road.

**Architecture:** `internal/workflow` mirrors `internal/contextmap`'s recorder/reporter split. Recorder mutates via existing store calls (`TaskLabelAdd`/`TaskLabelRemove`/`GetTask`/`LabelSeed`) only; the swap adds the target then removes every other `status:*` label (add-before-remove, so a failure never strips a task's status). Reporter is pure (store byte-identical before/after). A new `atm workflow` CLI command tree parallels `atm context`. The store gains nothing — the capability is a paved road, not a fence.

**Tech Stack:** Go 1.22+, cobra (CLI), the existing `internal/store` API.

**Spec:** `docs/superpowers/specs/2026-07-16-workflow-capability-design.md`

**Task:** ATM-e23fe5 (scope split off from ATM-18111b, which retains the recently-updated-tasks-board half)

## Global Constraints

- Go 1.22+; no new external dependencies.
- No store API additions. Use only `TaskLabelAdd`, `TaskLabelRemove`, `GetTask`, `LabelSeed`, `ParseTaskID`.
- The store enforces nothing; the capability's verbs are a paved road. Raw `atm task label add/remove --label <CODE>:status:<value>` keeps working.
- Label names and expressions live ONLY in `internal/workflow`. The CLI verbs and the TUI never reference `status:*`, `status:open`, etc. as string literals — they use `workflow.StatusInProgress` etc.
- No emojis in code or commits. Follow existing style in neighboring files.
- Every mutation stamps a valid actor (`persona@agent:model`); mutating CLI verbs require `--actor` via `requireMutatingActor`.
- `make verify` is the completion gate.

---

## File Structure

**Create:**
- `internal/workflow/status.go` — status value constants + namespace name (the only place the `"status"` literal lives).
- `internal/workflow/recorder.go` — `Recorder` with `SetStatus` + four scrum-verb wrappers.
- `internal/workflow/reporter.go` — `Reporter.Status` (read-only).
- `internal/workflow/recorder_test.go` — recorder behavior.
- `internal/workflow/reporter_test.go` — reporter behavior + purity.
- `internal/cli/workflow.go` — `atm workflow` command tree.
- `internal/cli/workflow_test.go` — CLI verb tests.

**Modify:**
- `internal/workflow/vocabulary.go` — add `BoardBacklog`, `BoardInProgressTasks`, their expressions; extend `EnsureVocabulary` to seed all three.
- `internal/workflow/vocabulary_test.go` — add assertions for `BoardBacklog` and `BoardInProgressTasks` (the existing file already has `newTestStore` + the open-tasks tests; extend it).
- `internal/cli/root.go` — register `newWorkflowCmd`.
- `internal/cli/conventions.go` — workflow paragraph + sequence step + JSON keys; soften "Workflow lives outside the store".
- `internal/cli/conventions_test.go` — assertions for workflow verbs + backlog board.
- `internal/cli/testdata/golden/conventions-text.json` — regenerated.
- `internal/cli/testdata/golden/conventions-json.json` — regenerated.
- `internal/cli/testdata/golden/determinism-conventions.json` — regenerated (if it lists boards).

No changes to `internal/store`, `internal/tui` (the ensure call sites already call `workflow.EnsureVocabulary`, which now seeds all three — the new boards enter the ring as normal members), or `internal/seed`.

---

## Task 1: Status constants

**Files:**
- Create: `internal/workflow/status.go`

**Interfaces:**
- Produces: `workflow.StatusOpen`, `workflow.StatusInProgress`, `workflow.StatusBlocked`, `workflow.StatusDone` (string constants); `workflow.StatusNamespace` (`"status"`).

- [ ] **Step 1: Create the constants file**

```go
// internal/workflow/status.go
package workflow

// StatusNamespace is the label suffix prefix this capability owns. It is the
// only place the string "status" appears in the capability; CLI verbs and the
// TUI reference these constants, never the literal.
const StatusNamespace = "status"

// Status values are the seeded lifecycle states the workflow capability
// transitions between. They match internal/seed's status:* labels.
// Note: status:todo is deliberately absent from the seed (see
// internal/seed/seed_test.go TestDroppedNamespacesAbsent), so there is no
// StatusTodo and no queue verb.
const (
	StatusOpen       = "open"
	StatusInProgress = "in-progress"
	StatusBlocked    = "blocked"
	StatusDone       = "done"
)
```

- [ ] **Step 2: Verify it compiles**

Run: `go build ./internal/workflow/...`
Expected: PASS (no errors).

- [ ] **Step 3: Commit**

```bash
git add internal/workflow/status.go
git commit -m "feat(workflow): add status value constants"
```

---

## Task 2: Extend vocabulary to three boards (TDD)

**Files:**
- Modify: `internal/workflow/vocabulary.go`
- Modify: `internal/workflow/vocabulary_test.go` (extend the existing file; it already defines `newTestStore` and the open-tasks tests)

**Interfaces:**
- Produces: `workflow.BoardBacklog(code) string`, `workflow.BoardInProgressTasks(code) string`; `workflow.EnsureVocabulary(s, code, actor) error` now seeds all three boards.
- Consumes: `store.LabelSeed`, `store.LabelShow`, `store.Store`.

- [ ] **Step 1: Write the failing test (append to the existing `vocabulary_test.go`)**

The existing file already defines `newTestStore(t)` and four `TestEnsureVocabulary*` tests that only check `BoardOpenTasks`. Append these new tests, which exercise the two new boards:

```go
func TestEnsureVocabularySeedsBacklogBoard(t *testing.T) {
	s := newTestStore(t)
	if err := EnsureVocabulary(s, "ATM", "admin@cli:unset"); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	l, err := s.LabelShow(BoardBacklog("ATM"))
	if err != nil {
		t.Fatalf("label show: %v", err)
	}
	if l.Expr != "NOT status:*" {
		t.Errorf("backlog expr = %q, want %q", l.Expr, "NOT status:*")
	}
	if l.Description == "" {
		t.Error("backlog board has no description")
	}
}

func TestEnsureVocabularySeedsInProgressTasksBoard(t *testing.T) {
	s := newTestStore(t)
	if err := EnsureVocabulary(s, "ATM", "admin@cli:unset"); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	l, err := s.LabelShow(BoardInProgressTasks("ATM"))
	if err != nil {
		t.Fatalf("label show: %v", err)
	}
	if l.Expr != "status:in-progress" {
		t.Errorf("in-progress-tasks expr = %q, want %q", l.Expr, "status:in-progress")
	}
	if l.Description == "" {
		t.Error("in-progress-tasks board has no description")
	}
}

func TestEnsureVocabularyPreservesHumanBacklogDescription(t *testing.T) {
	s := newTestStore(t)
	humanDesc := "curated by a human"
	if err := s.LabelAdd(BoardBacklog("ATM"), humanDesc, "NOT status:*", "admin@cli:unset"); err != nil {
		t.Fatalf("seed human label: %v", err)
	}
	if err := EnsureVocabulary(s, "ATM", "admin@cli:unset"); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	l, err := s.LabelShow(BoardBacklog("ATM"))
	if err != nil {
		t.Fatalf("label show: %v", err)
	}
	if l.Description != humanDesc {
		t.Errorf("backlog description = %q, want %q (human curation must survive ensure)", l.Description, humanDesc)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/workflow/ -run TestEnsureVocabulary -v`
Expected: FAIL — `BoardBacklog` / `BoardInProgressTasks` undefined (compile error), and the existing `EnsureVocabulary` only seeds `open-tasks`.

- [ ] **Step 3: Extend the vocabulary**

Replace the body of `internal/workflow/vocabulary.go` with:

```go
// Package workflow owns the vocabulary for the TUI's default board surface
// and the status-transition paved road. It is a capability mirroring
// internal/contextmap: it ensures its own vocabulary idempotently, exposes
// intent-level verbs (see recorder.go / reporter.go), and owns the status
// label namespace. The store enforces nothing; this capability is a paved
// road, not a fence. A human may edit or delete any board or status label;
// the next project-select / label-seed re-ensures the vocabulary.
package workflow

import "atm/internal/store"

// BoardOpenTasks returns the full name of the Open Tasks board for a project.
// Callers select this board by name; they never reference the expression.
func BoardOpenTasks(code string) string { return code + ":open-tasks" }

// BoardBacklog returns the full name of the Backlog board: untriaged tasks
// carrying no status label. Surfaced so quick jottings (created with no
// labels) do not vanish from the default board ring.
func BoardBacklog(code string) string { return code + ":backlog" }

// BoardInProgressTasks returns the full name of the In-Progress board.
func BoardInProgressTasks(code string) string { return code + ":in-progress-tasks" }

func openTasksExpr() string       { return "status:open" }
func backlogExpr() string         { return "NOT status:*" }
func inProgressTasksExpr() string { return "status:in-progress" }

// EnsureVocabulary creates the three workflow boards with descriptions, if
// absent. Idempotent: LabelSeed upserts only when the label is absent, so a
// human's curated description is never overwritten. Self-bootstrapping: it
// does not assume `atm label seed` ran.
func EnsureVocabulary(s *store.Store, code, actor string) error {
	boards := []struct{ name, desc, expr string }{
		{BoardBacklog(code), "tasks with no status label: incoming jottings awaiting triage. Review queue alongside open-tasks.", backlogExpr()},
		{BoardOpenTasks(code), "every open task: the project's active work. Default board in the TUI.", openTasksExpr()},
		{BoardInProgressTasks(code), "tasks someone is actively working on (status:in-progress).", inProgressTasksExpr()},
	}
	for _, b := range boards {
		if err := s.LabelSeed(b.name, b.desc, b.expr, actor); err != nil {
			return err
		}
	}
	return nil
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/workflow/ -run TestEnsureVocabulary -v`
Expected: PASS — all seven vocabulary tests pass (the four existing open-tasks tests + the three new ones).

- [ ] **Step 5: Run the full workflow + store packages to catch regressions**

Run: `go test ./internal/workflow/ ./internal/store/ ./internal/cli/`
Expected: PASS (the existing `EnsureVocabulary` callers now seed all three; existing tests that only check `open-tasks` still pass because it remains).

- [ ] **Step 6: Commit**

```bash
git add internal/workflow/vocabulary.go internal/workflow/vocabulary_test.go
git commit -m "feat(workflow): ensure backlog + in-progress-tasks boards"
```

---

## Task 3: Reporter — read-only status (TDD)

**Files:**
- Create: `internal/workflow/reporter.go`
- Create: `internal/workflow/reporter_test.go`

**Interfaces:**
- Produces: `workflow.Reporter{Store *store.Store}`, method `Status(taskID string) (value string, err error)` — returns the bare status value (`"open"`, …) or `""` when untriaged.
- Consumes: `store.GetTask`, `store.ParseTaskID`.

- [ ] **Step 1: Write the failing test**

```go
// internal/workflow/reporter_test.go
package workflow

import (
	"testing"

	"atm/internal/store"
)

func TestReporterStatusReturnsValue(t *testing.T) {
	s := newTestStore(t)
	tk, err := s.CreateTask("ATM", "t", "", []string{"ATM:status:in-progress"}, "admin@cli:unset")
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	r := &Reporter{Store: s}
	got, err := r.Status(tk.ID)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if got != StatusInProgress {
		t.Errorf("Status = %q, want %q", got, StatusInProgress)
	}
}

func TestReporterStatusUntriagedReturnsEmpty(t *testing.T) {
	s := newTestStore(t)
	tk, _ := s.CreateTask("ATM", "t", "", nil, "admin@cli:unset")
	r := &Reporter{Store: s}
	got, err := r.Status(tk.ID)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if got != "" {
		t.Errorf("Status = %q, want \"\" (untriaged)", got)
	}
}

func TestReporterStatusOnlyNonStatusReturnsEmpty(t *testing.T) {
	s := newTestStore(t)
	tk, _ := s.CreateTask("ATM", "t", "", []string{"ATM:priority:high"}, "admin@cli:unset")
	r := &Reporter{Store: s}
	got, _ := r.Status(tk.ID)
	if got != "" {
		t.Errorf("Status = %q, want \"\" (only non-status label)", got)
	}
}

func TestReporterStatusUnknownTask(t *testing.T) {
	s := newTestStore(t)
	r := &Reporter{Store: s}
	if _, err := r.Status("ATM-deadbeef"); err == nil {
		t.Error("expected error for unknown task id")
	}
}

func TestReporterStatusIsPure(t *testing.T) {
	// Purity: the project log's event count must not advance when the
	// reporter runs. LastLogSeq is the store's staleness probe (see
	// internal/store/log.go) and is the cleanest byte-stable check.
	s := newTestStore(t)
	tk, _ := s.CreateTask("ATM", "t", "", []string{"ATM:status:open"}, "admin@cli:unset")
	code, _, _ := store.ParseTaskID(tk.ID)
	before, err := s.LastLogSeq(code)
	if err != nil {
		t.Fatalf("LastLogSeq before: %v", err)
	}
	r := &Reporter{Store: s}
	if _, err := r.Status(tk.ID); err != nil {
		t.Fatalf("Status: %v", err)
	}
	after, err := s.LastLogSeq(code)
	if err != nil {
		t.Fatalf("LastLogSeq after: %v", err)
	}
	if before != after {
		t.Fatalf("reporter advanced log seq %d -> %d — reporter must be pure", before, after)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/workflow/ -run TestReporter -v`
Expected: FAIL — `Reporter` undefined.

- [ ] **Step 3: Implement the reporter**

```go
// internal/workflow/reporter.go
package workflow

import (
	"fmt"
	"strings"

	"atm/internal/store"
)

// Reporter is the read-only side of the workflow capability. It never
// mutates the store; the project log is byte-identical before and after
// any Reporter call (testable, like contextmap's reporter contract).
type Reporter struct {
	Store *store.Store
}

// Status returns the task's status value (e.g. "open", "in-progress") or
// "" when the task carries no status:* label (untriaged).
func (r *Reporter) Status(taskID string) (string, error) {
	tk, err := r.Store.GetTask(taskID)
	if err != nil {
		return "", err
	}
	code, _, ok := store.ParseTaskID(taskID)
	if !ok {
		return "", fmt.Errorf("invalid task id %q", taskID)
	}
	prefix := code + ":" + StatusNamespace + ":"
	for _, l := range tk.Labels {
		if strings.HasPrefix(l, prefix) {
			return strings.TrimPrefix(l, prefix), nil
		}
	}
	return "", nil
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/workflow/ -run TestReporter -v`
Expected: PASS — all five reporter tests pass, including purity.

- [ ] **Step 5: Commit**

```bash
git add internal/workflow/reporter.go internal/workflow/reporter_test.go
git commit -m "feat(workflow): add read-only status reporter"
```

---

## Task 4: Recorder — SetStatus swap (TDD)

**Files:**
- Create: `internal/workflow/recorder.go`
- Create: `internal/workflow/recorder_test.go`

**Interfaces:**
- Produces: `workflow.Recorder{Store *store.Store, Actor string}`, method `SetStatus(taskID, target string) (prior string, err error)`; wrappers `Start`, `Open`, `Block`, `Complete`.
- Consumes: `store.GetTask`, `store.TaskLabelAdd`, `store.TaskLabelRemove`, `store.ParseTaskID`, `workflow.StatusNamespace`.

- [ ] **Step 1: Write the failing test**

```go
// internal/workflow/recorder_test.go
package workflow

import (
	"strings"
	"testing"

	"atm/internal/store"
)

func TestRecorderSetStatusFromUntriaged(t *testing.T) {
	s := newTestStore(t)
	tk, _ := s.CreateTask("ATM", "t", "", nil, "admin@cli:unset")
	r := &Recorder{Store: s, Actor: "admin@cli:unset"}
	prior, err := r.SetStatus(tk.ID, StatusOpen)
	if err != nil {
		t.Fatalf("SetStatus: %v", err)
	}
	if prior != "" {
		t.Errorf("prior = %q, want \"\" (was untriaged)", prior)
	}
	got, _ := (&Reporter{Store: s}).Status(tk.ID)
	if got != StatusOpen {
		t.Errorf("after SetStatus, status = %q, want %q", got, StatusOpen)
	}
}

func TestRecorderSetStatusSwapsExisting(t *testing.T) {
	s := newTestStore(t)
	tk, _ := s.CreateTask("ATM", "t", "", []string{"ATM:status:open"}, "admin@cli:unset")
	r := &Recorder{Store: s, Actor: "admin@cli:unset"}
	prior, err := r.SetStatus(tk.ID, StatusInProgress)
	if err != nil {
		t.Fatalf("SetStatus: %v", err)
	}
	if prior != StatusOpen {
		t.Errorf("prior = %q, want %q", prior, StatusOpen)
	}
	if got, _ := (&Reporter{Store: s}).Status(tk.ID); got != StatusInProgress {
		t.Errorf("status = %q, want %q", got, StatusInProgress)
	}
	if n := countStatusLabels(s.GetTaskOrFatal(t, tk.ID), "ATM"); n != 1 {
		t.Errorf("status label count = %d, want 1", n)
	}
}

func TestRecorderSetStatusNoOpWhenAlreadyAtTarget(t *testing.T) {
	s := newTestStore(t)
	tk, _ := s.CreateTask("ATM", "t", "", []string{"ATM:status:done"}, "admin@cli:unset")
	code, _, _ := store.ParseTaskID(tk.ID)
	before, _ := s.LastLogSeq(code)
	r := &Recorder{Store: s, Actor: "admin@cli:unset"}
	prior, err := r.SetStatus(tk.ID, StatusDone)
	if err != nil {
		t.Fatalf("SetStatus: %v", err)
	}
	if prior != StatusDone {
		t.Errorf("prior = %q, want %q (already done)", prior, StatusDone)
	}
	after, _ := s.LastLogSeq(code)
	if before != after {
		t.Fatalf("no-op SetStatus advanced log seq %d -> %d", before, after)
	}
}

func TestRecorderSetStatusPreservesNonStatusLabels(t *testing.T) {
	s := newTestStore(t)
	tk, _ := s.CreateTask("ATM", "t", "", []string{"ATM:priority:high", "ATM:status:open"}, "admin@cli:unset")
	r := &Recorder{Store: s, Actor: "admin@cli:unset"}
	if _, err := r.SetStatus(tk.ID, StatusDone); err != nil {
		t.Fatalf("SetStatus: %v", err)
	}
	hasPrio := false
	for _, l := range s.GetTaskOrFatal(t, tk.ID).Labels {
		if l == "ATM:priority:high" {
			hasPrio = true
		}
	}
	if !hasPrio {
		t.Error("priority:high label was dropped by the status swap")
	}
}

func TestRecorderSetStatusRemovesMultipleStatusLabels(t *testing.T) {
	// A hand-edited task may carry several status:* labels (the store permits
	// it). SetStatus must remove ALL of them and add the target, restoring
	// exactly-one-status as a capability-maintained invariant.
	s := newTestStore(t)
	tk, _ := s.CreateTask("ATM", "t", "", nil, "admin@cli:unset")
	_ = s.TaskLabelAdd(tk.ID, "ATM:status:open", "admin@cli:unset")
	_ = s.TaskLabelAdd(tk.ID, "ATM:status:done", "admin@cli:unset")
	r := &Recorder{Store: s, Actor: "admin@cli:unset"}
	prior, err := r.SetStatus(tk.ID, StatusInProgress)
	if err != nil {
		t.Fatalf("SetStatus: %v", err)
	}
	// prior is the lexicographically first non-target status. The store returns
	// labels sorted, so "done" precedes "open".
	if prior != StatusDone {
		t.Errorf("prior = %q, want %q (lexicographically first non-target)", prior, StatusDone)
	}
	if n := countStatusLabels(s.GetTaskOrFatal(t, tk.ID), "ATM"); n != 1 {
		t.Errorf("status label count = %d, want 1 (after collapsing hand-edit)", n)
	}
}

func TestRecorderSetStatusCollapsesWhenTargetAlreadyPresent(t *testing.T) {
	// The highest-risk branch: the target is ALREADY one of several status
	// labels. The recorder must keep the target and drop the others -- never
	// remove the target and fail to re-add it, which would leave the task with
	// no status at all. Seeded [open, done] with target=done so that the
	// alreadyHasTarget path is exercised; TestRecorderSetStatusRemovesMultiple
	// only covers a target that is absent from the existing set.
	s := newTestStore(t)
	tk, err := s.CreateTask("ATM", "t", "", nil, "admin@cli:unset")
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if err := s.TaskLabelAdd(tk.ID, "ATM:status:open", "admin@cli:unset"); err != nil {
		t.Fatalf("seed open: %v", err)
	}
	if err := s.TaskLabelAdd(tk.ID, "ATM:status:done", "admin@cli:unset"); err != nil {
		t.Fatalf("seed done: %v", err)
	}
	r := &Recorder{Store: s, Actor: "admin@cli:unset"}
	prior, err := r.SetStatus(tk.ID, StatusDone)
	if err != nil {
		t.Fatalf("SetStatus: %v", err)
	}
	if prior != StatusOpen {
		t.Errorf("prior = %q, want %q (lexicographically first non-target)", prior, StatusOpen)
	}
	got := getTaskOrFatal(t, s, tk.ID)
	if n := countStatusLabels(got, "ATM"); n != 1 {
		t.Errorf("status label count = %d, want 1", n)
	}
	if v, _ := (&Reporter{Store: s}).Status(tk.ID); v != StatusDone {
		t.Errorf("status = %q, want %q (target must survive)", v, StatusDone)
	}
}

func TestRecorderScrumVerbsMapToCorrectStatus(t *testing.T) {
	s := newTestStore(t)
	tk, _ := s.CreateTask("ATM", "t", "", nil, "admin@cli:unset")
	r := &Recorder{Store: s, Actor: "admin@cli:unset"}
	cases := []struct {
		fn   func(string) (string, error)
		want string
	}{
		{r.Start, StatusInProgress},
		{r.Open, StatusOpen},
		{r.Block, StatusBlocked},
		{r.Complete, StatusDone},
	}
	for i, c := range cases {
		if _, err := c.fn(tk.ID); err != nil {
			t.Fatalf("verb %d: %v", i, err)
		}
		got, _ := (&Reporter{Store: s}).Status(tk.ID)
		if got != c.want {
			t.Errorf("verb %d: status = %q, want %q", i, got, c.want)
		}
	}
}

func TestRecorderSetStatusFailedAddDoesNotStripStatus(t *testing.T) {
	// Pins add-before-remove -- the ordering this recorder deliberately uses.
	// Under remove-then-add, a failed add leaves the task with NO status label,
	// silently dropping it off every board. Every other recorder test exercises
	// only the happy path, where the two orderings are observationally
	// identical; without this test the ordering could be reverted with the
	// whole suite still green.
	//
	// store.ValidateLabelName rejects an uppercase value segment, and
	// TaskLabelAdd calls it as its first statement (internal/store/task.go),
	// so the add fails deterministically with zero mutation.
	s := newTestStore(t)
	tk, err := s.CreateTask("ATM", "t", "", []string{"ATM:status:open"}, "admin@cli:unset")
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	r := &Recorder{Store: s, Actor: "admin@cli:unset"}
	prior, err := r.SetStatus(tk.ID, "BOGUS")
	if err == nil {
		t.Fatal("expected an error for an invalid status target")
	}
	if prior != StatusOpen {
		t.Errorf("prior = %q, want %q -- callers must be able to report what the task was", prior, StatusOpen)
	}
	if v, _ := (&Reporter{Store: s}).Status(tk.ID); v != StatusOpen {
		t.Fatalf("status = %q, want %q -- a failed add must not strip the task's status", v, StatusOpen)
	}
}

func TestRecorderSetStatusUnknownTask(t *testing.T) {
	s := newTestStore(t)
	r := &Recorder{Store: s, Actor: "admin@cli:unset"}
	if _, err := r.SetStatus("ATM-deadbeef", StatusOpen); err == nil {
		t.Error("expected error for unknown task id")
	}
}

// countStatusLabels counts labels with the <code>:status:* prefix.
func countStatusLabels(tk *store.Task, code string) int {
	prefix := code + ":" + StatusNamespace + ":"
	n := 0
	for _, l := range tk.Labels {
		if strings.HasPrefix(l, prefix) {
			n++
		}
	}
	return n
}
```

> **Note for the implementer:** `s.GetTaskOrFatal(t, tk.ID)` is shown as a hypothetical helper. The real store API is `s.GetTask(id) (*store.Task, error)`. Replace each `s.GetTaskOrFatal(t, id)` call with:

```go
tk, err := s.GetTask(id)
if err != nil {
	t.Fatalf("GetTask: %v", err)
}
```

inline at the call site (or define a local `getTaskOrFatal` helper in the test file). The plan shows the helper form for brevity; the implementer MUST use the real `GetTask` API.

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/workflow/ -run TestRecorder -v`
Expected: FAIL — `Recorder` undefined.

- [ ] **Step 3: Implement the recorder**

```go
// internal/workflow/recorder.go
package workflow

import (
	"fmt"
	"strings"

	"atm/internal/store"
)

// Recorder is the mutating side of the workflow capability. It swaps a
// task's status:* label via existing store calls; the store itself
// enforces nothing. The "exactly one status" invariant is maintained by
// this recorder, not by the store.
type Recorder struct {
	Store *store.Store
	Actor string
}

// SetStatus swaps the task's status label to target. It adds the target,
// then removes every other <code>:status:* label on the task (a hand-edited
// task may carry several). When the task already carries target as its sole
// status, it is a no-op and no store call is made.
//
// Add-before-remove is deliberate: the store has no transactions, so
// remove-first would leave the task with no status at all if the add failed.
// This ordering bounds the worst case to a recoverable extra label.
//
// Returns the prior status value (e.g. "open") or "" if the task was
// untriaged. When the task had multiple status labels, prior is the
// lexicographically first non-target one (the store returns labels sorted;
// see internal/store/cache.go ORDER BY label) - NOT necessarily the most
// recently set. On error, prior is still returned so callers can report what
// the task was.
func (r *Recorder) SetStatus(taskID, target string) (prior string, err error) {
	tk, err := r.Store.GetTask(taskID)
	if err != nil {
		return "", err
	}
	code, _, ok := store.ParseTaskID(taskID)
	if !ok {
		return "", fmt.Errorf("invalid task id %q", taskID)
	}
	prefix := code + ":" + StatusNamespace + ":"
	targetLabel := prefix + target

	// Collect all existing status:* labels, note whether the target is among
	// them, and pick the prior value in one pass.
	var existing []string
	alreadyHasTarget := false
	for _, l := range tk.Labels {
		if !strings.HasPrefix(l, prefix) {
			continue
		}
		existing = append(existing, l)
		if l == targetLabel {
			alreadyHasTarget = true
		} else if prior == "" {
			prior = strings.TrimPrefix(l, prefix)
		}
	}

	// No-op when the target is already the sole status: zero store calls, so
	// the event log cannot advance.
	if len(existing) == 1 && alreadyHasTarget {
		return target, nil
	}

	// Add the target BEFORE removing anything. The store has no transactions,
	// and TaskLabelAdd validates only once called — so remove-then-add would
	// leave a task with NO status label if the add failed, silently dropping it
	// off every board. Add-first bounds the worst case to a leftover label.
	if !alreadyHasTarget {
		if err := r.Store.TaskLabelAdd(taskID, targetLabel, r.Actor); err != nil {
			return prior, fmt.Errorf("add %s: %w", targetLabel, err)
		}
	}

	// Then remove every other status label. If one of these fails the task
	// carries the target plus a leftover: the exactly-one invariant is
	// violated, but no status is lost and re-running the verb converges.
	for _, l := range existing {
		if l == targetLabel {
			continue
		}
		if err := r.Store.TaskLabelRemove(taskID, l, r.Actor); err != nil {
			return prior, fmt.Errorf("remove %s: %w", l, err)
		}
	}
	return prior, nil
}

// Start transitions the task to in-progress (someone is now on this).
func (r *Recorder) Start(taskID string) (string, error) { return r.SetStatus(taskID, StatusInProgress) }

// Open transitions the task to open ((re)open for consideration).
func (r *Recorder) Open(taskID string) (string, error) { return r.SetStatus(taskID, StatusOpen) }

// Block transitions the task to blocked (cannot proceed pending something else).
func (r *Recorder) Block(taskID string) (string, error) { return r.SetStatus(taskID, StatusBlocked) }

// Complete transitions the task to done (finished).
func (r *Recorder) Complete(taskID string) (string, error) { return r.SetStatus(taskID, StatusDone) }
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/workflow/ -v`
Expected: PASS — all recorder + reporter + vocabulary tests pass.

- [ ] **Step 5: Run the full repo test to catch regressions**

Run: `go test ./...`
Expected: PASS (the new `EnsureVocabulary` seeds two more boards; existing tests that assert only `open-tasks` are unaffected).

- [ ] **Step 6: Commit**

```bash
git add internal/workflow/recorder.go internal/workflow/recorder_test.go
git commit -m "feat(workflow): add status-transition recorder with swap semantics"
```

---

## Task 5: CLI `atm workflow` command tree (TDD)

**Files:**
- Create: `internal/cli/workflow.go`
- Create: `internal/cli/workflow_test.go`
- Modify: `internal/cli/root.go` (register the command)

**Interfaces:**
- Produces: `newWorkflowCmd(st *cliState) *cobra.Command` with subcommands: `start`, `open`, `block`, `complete`, `status`, `seed`.
- Consumes: `cliState.openStore`, `resolveTaskID`, `requireMutatingActor`, `resolveActor`, `emit`, `taskToJSON`; `workflow.Recorder`, `workflow.Reporter`, `workflow.EnsureVocabulary`.

- [ ] **Step 1: Write the failing test**

```go
// internal/cli/workflow_test.go
package cli

import (
	"encoding/json"
	"strings"
	"testing"
)

// seedWorkflowProject boots a store with project ATM, returns its store path.
// The golden harness defaults to JSON output, so `task create` returns JSON
// and task ids are extracted with taskIDFromCreateJSON (already defined in
// harness_test.go).
func seedWorkflowProject(t *testing.T, h *goldenHarness) string {
	t.Helper()
	sp := h.store.StorePath()
	h.run("init", "--store", sp, "--actor", "admin@cli:unset")
	h.run("project", "create", "--store", sp, "--code", "ATM", "--name", "Agent Tasks Management", "--actor", "admin@cli:unset")
	return sp
}

func createTaskWithLabels(t *testing.T, h *goldenHarness, sp, title string, labels ...string) string {
	t.Helper()
	args := []string{"task", "create", "--store", sp, "--project", "ATM", "--title", title, "--actor", "admin@cli:unset"}
	for _, l := range labels {
		args = append(args, "--label", l)
	}
	out, _, code := h.run(args...)
	if code != 0 {
		t.Fatalf("task create exit=%d stderr=%s", code, h.stderr.String())
	}
	return taskIDFromCreateJSON(t, out)
}

func TestWorkflowStartSwapsStatus(t *testing.T) {
	h := newGoldenHarness(t)
	sp := seedWorkflowProject(t, h)
	id := createTaskWithLabels(t, h, sp, "jotting", "ATM:status:open")

	out, stderr, code := h.run("workflow", "start", "--store", sp, "--task", id, "--actor", "admin@cli:unset")
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, stderr)
	}
	// Swap semantics: the target must be present AND the prior status gone.
	// Asserting only the target would pass even if BOTH labels survived,
	// which is exactly the bug the swap exists to prevent.
	if !strings.Contains(out, "ATM:status:in-progress") {
		t.Fatalf("output missing ATM:status:in-progress: %s", out)
	}
	if strings.Contains(out, "ATM:status:open") {
		t.Fatalf("prior status ATM:status:open survived the swap: %s", out)
	}
}

func TestWorkflowStartRequiresActor(t *testing.T) {
	h := newGoldenHarness(t)
	sp := seedWorkflowProject(t, h)
	id := createTaskWithLabels(t, h, sp, "t")

	_, _, code := h.run("workflow", "start", "--store", sp, "--task", id)
	if code == 0 {
		t.Fatal("expected non-zero exit when --actor missing on mutating verb")
	}
}

func TestWorkflowStatusReporter(t *testing.T) {
	h := newGoldenHarness(t)
	sp := seedWorkflowProject(t, h)
	id := createTaskWithLabels(t, h, sp, "t", "ATM:status:done")

	out, _, code := h.run("workflow", "status", "--store", sp, "--task", id)
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, h.stderr.String())
	}
	// Parse the envelope rather than substring-matching: a bare
	// strings.Contains(out, "done") would also pass on a task id containing
	// "done".
	var env struct {
		Task   string `json:"task"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("unmarshal %q: %v", out, err)
	}
	if env.Status != "done" {
		t.Fatalf("status = %q, want \"done\"", env.Status)
	}
}

func TestWorkflowStatusUntriaged(t *testing.T) {
	h := newGoldenHarness(t)
	sp := seedWorkflowProject(t, h)
	id := createTaskWithLabels(t, h, sp, "t")

	out, _, code := h.run("workflow", "status", "--store", sp, "--task", id)
	if code != 0 {
		t.Fatalf("exit=%d", code)
	}
	// The CLI pretty-prints JSON (store.MarshalSorted uses SetIndent("", "  ")),
	// so a substring match on `"status":""` would NOT match the real output.
	// Parse the envelope and assert the field instead.
	var env struct {
		Task   string `json:"task"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("unmarshal %q: %v", out, err)
	}
	if env.Status != "" {
		t.Fatalf("expected status \"\" for untriaged, got %q", env.Status)
	}
}

func TestWorkflowStatusReporterIsReadOnly(t *testing.T) {
	h := newGoldenHarness(t)
	sp := seedWorkflowProject(t, h)
	id := createTaskWithLabels(t, h, sp, "t", "ATM:status:open")

	before, err := h.store.LastLogSeq("ATM")
	if err != nil {
		t.Fatalf("LastLogSeq before: %v", err)
	}
	if _, _, code := h.run("workflow", "status", "--store", sp, "--task", id); code != 0 {
		t.Fatalf("status exit=%d stderr=%s", code, h.stderr.String())
	}
	after, err := h.store.LastLogSeq("ATM")
	if err != nil {
		t.Fatalf("LastLogSeq after: %v", err)
	}
	if before != after {
		t.Fatalf("workflow status advanced log seq %d -> %d — reporter must be read-only", before, after)
	}
}

func TestWorkflowSeedEnsuresAllThreeBoards(t *testing.T) {
	h := newGoldenHarness(t)
	sp := seedWorkflowProject(t, h)
	_, _, code := h.run("workflow", "seed", "--store", sp, "--project", "ATM", "--actor", "admin@cli:unset")
	if code != 0 {
		t.Fatalf("seed exit=%d stderr=%s", code, h.stderr.String())
	}
	for _, name := range []string{"ATM:backlog", "ATM:open-tasks", "ATM:in-progress-tasks"} {
		if _, err := h.store.LabelShow(name); err != nil {
			t.Errorf("%s not ensured by workflow seed: %v", name, err)
		}
	}
}

func TestWorkflowSeedIdempotent(t *testing.T) {
	h := newGoldenHarness(t)
	sp := seedWorkflowProject(t, h)
	if _, _, code := h.run("workflow", "seed", "--store", sp, "--project", "ATM", "--actor", "admin@cli:unset"); code != 0 {
		t.Fatalf("first seed exit=%d stderr=%s", code, h.stderr.String())
	}
	_, _, code := h.run("workflow", "seed", "--store", sp, "--project", "ATM", "--actor", "admin@cli:unset")
	if code != 0 {
		t.Fatal("second seed exited non-zero")
	}
}

func TestWorkflowSeedRequiresActor(t *testing.T) {
	h := newGoldenHarness(t)
	sp := seedWorkflowProject(t, h)
	_, _, code := h.run("workflow", "seed", "--store", sp, "--project", "ATM")
	if code == 0 {
		t.Fatal("expected non-zero exit when --actor missing on seed")
	}
}

func TestWorkflowCompleteSwapsToDone(t *testing.T) {
	h := newGoldenHarness(t)
	sp := seedWorkflowProject(t, h)
	id := createTaskWithLabels(t, h, sp, "t", "ATM:status:in-progress")

	out, _, code := h.run("workflow", "complete", "--store", sp, "--task", id, "--actor", "admin@cli:unset")
	if code != 0 {
		t.Fatalf("exit=%d", code)
	}
	if !strings.Contains(out, "ATM:status:done") {
		t.Fatalf("output missing ATM:status:done: %s", out)
	}
	if strings.Contains(out, "ATM:status:in-progress") {
		t.Fatalf("prior status ATM:status:in-progress survived the swap: %s", out)
	}
}
```

> **Note for the implementer:** The golden harness defaults to JSON output (`h.output = outputJSON`). The assertions above check JSON envelope contents with `strings.Contains`, which is robust to the exact envelope shape. If the text-mode output line (`<id>: status <prior> -> <value>`) needs explicit testing, set `h.output = outputText` for that one test and assert on the text line. The `taskIDFromCreateJSON` helper already exists in `internal/cli/harness_test.go:176` — do not redefine it.

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/cli/ -run TestWorkflow -v`
Expected: FAIL — `newWorkflowCmd` undefined / `workflow` subcommand not registered.

- [ ] **Step 3: Implement the CLI command tree**

Create `internal/cli/workflow.go`:

```go
package cli

import (
	"fmt"

	"atm/internal/workflow"

	"github.com/spf13/cobra"
)

func newWorkflowCmd(st *cliState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "workflow",
		Short: "Status-transition verbs (the paved road for task status)",
		Long: "Status transitions live in the internal/workflow capability. " +
			"Each verb swaps the task's status:* label (removes any existing one, " +
			"adds the target), so exactly-one-status is an invariant the capability " +
			"maintains. The store still enforces nothing; raw `atm task label " +
			"add/remove --label <CODE>:status:<value>` works. This is a paved road, " +
			"not a fence.",
	}
	bindActorFlag(cmd, st)
	cmd.AddCommand(newWorkflowStartCmd(st))
	cmd.AddCommand(newWorkflowOpenCmd(st))
	cmd.AddCommand(newWorkflowBlockCmd(st))
	cmd.AddCommand(newWorkflowCompleteCmd(st))
	cmd.AddCommand(newWorkflowStatusCmd(st))
	cmd.AddCommand(newWorkflowSeedCmd(st))
	return cmd
}

// runStatusVerb is the shared body for the five scrum verbs. It resolves the
// task, requires an explicit actor, runs the swap, then prints the
// transition line and emits the updated task JSON.
func runStatusVerb(st *cliState, id, legacy string, fn func(*workflow.Recorder, string) (string, error)) error {
	taskID, err := resolveTaskID(st, id, legacy)
	if err != nil {
		return err
	}
	actor, err := st.requireMutatingActor()
	if err != nil {
		return err
	}
	s, err := st.openStore()
	if err != nil {
		return err
	}
	rec := &workflow.Recorder{Store: s, Actor: actor}
	prior, err := fn(rec, taskID)
	if err != nil {
		return err
	}
	t, err := s.GetTask(taskID)
	if err != nil {
		return err
	}
	now, err := (&workflow.Reporter{Store: s}).Status(taskID)
	if err != nil {
		return err
	}
	return st.emit(st.stdout(), map[string]any{"task": taskToJSON(t, nil)}, func() {
		if prior == "" {
			fmt.Fprintf(st.stdout(), "%s: status -> %s\n", t.ID, now)
		} else {
			fmt.Fprintf(st.stdout(), "%s: status %s -> %s\n", t.ID, prior, now)
		}
	})
}

func newWorkflowStartCmd(st *cliState) *cobra.Command {
	var id, legacy string
	cmd := &cobra.Command{
		Use:   "start",
		Short: "Transition a task to in-progress",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runStatusVerb(st, id, legacy, func(r *workflow.Recorder, tid string) (string, error) {
				return r.Start(tid)
			})
		},
	}
	bindTaskIDFlags(cmd, &id, &legacy)
	return cmd
}

func newWorkflowOpenCmd(st *cliState) *cobra.Command {
	var id, legacy string
	cmd := &cobra.Command{
		Use:   "open",
		Short: "Transition a task to open",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runStatusVerb(st, id, legacy, func(r *workflow.Recorder, tid string) (string, error) {
				return r.Open(tid)
			})
		},
	}
	bindTaskIDFlags(cmd, &id, &legacy)
	return cmd
}

func newWorkflowBlockCmd(st *cliState) *cobra.Command {
	var id, legacy string
	cmd := &cobra.Command{
		Use:   "block",
		Short: "Transition a task to blocked",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runStatusVerb(st, id, legacy, func(r *workflow.Recorder, tid string) (string, error) {
				return r.Block(tid)
			})
		},
	}
	bindTaskIDFlags(cmd, &id, &legacy)
	return cmd
}

func newWorkflowCompleteCmd(st *cliState) *cobra.Command {
	var id, legacy string
	cmd := &cobra.Command{
		Use:   "complete",
		Short: "Transition a task to done",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runStatusVerb(st, id, legacy, func(r *workflow.Recorder, tid string) (string, error) {
				return r.Complete(tid)
			})
		},
	}
	bindTaskIDFlags(cmd, &id, &legacy)
	return cmd
}

func newWorkflowStatusCmd(st *cliState) *cobra.Command {
	var id, legacy string
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Print the task's current status (read-only)",
		RunE: func(cmd *cobra.Command, args []string) error {
			taskID, err := resolveTaskID(st, id, legacy)
			if err != nil {
				return err
			}
			s, err := st.openStore()
			if err != nil {
				return err
			}
			rep := &workflow.Reporter{Store: s}
			value, err := rep.Status(taskID)
			if err != nil {
				return err
			}
			return st.emit(st.stdout(), map[string]any{"task": taskID, "status": value}, func() {
				if value == "" {
					fmt.Fprintf(st.stdout(), "untriaged\n")
					return
				}
				fmt.Fprintf(st.stdout(), "%s\n", value)
			})
		},
	}
	bindTaskIDFlags(cmd, &id, &legacy)
	return cmd
}

func newWorkflowSeedCmd(st *cliState) *cobra.Command {
	var project string
	cmd := &cobra.Command{
		Use:   "seed",
		Short: "Ensure the workflow boards (backlog, open-tasks, in-progress-tasks) exist",
		RunE: func(cmd *cobra.Command, args []string) error {
			actor, err := st.requireMutatingActor()
			if err != nil {
				return err
			}
			s, err := st.openStore()
			if err != nil {
				return err
			}
			if err := workflow.EnsureVocabulary(s, project, actor); err != nil {
				return err
			}
			return st.emit(st.stdout(), map[string]any{
				"project": project,
				// Board names come from the capability's helpers, never rebuilt
				// here: internal/workflow owns these names exclusively, and a
				// hand-built string would silently drift from what
				// EnsureVocabulary actually seeds if a board is ever renamed.
				"boards": []string{
					workflow.BoardBacklog(project),
					workflow.BoardOpenTasks(project),
					workflow.BoardInProgressTasks(project),
				},
			}, func() {
				fmt.Fprintf(st.stdout(), "ensured workflow boards for %s\n", project)
			})
		},
	}
	cmd.Flags().StringVar(&project, "project", "", "project code")
	_ = cmd.MarkFlagRequired("project")
	return cmd
}
```

- [ ] **Step 4: Register the command on the root**

In `internal/cli/root.go`, add the import (if not already present) and register the command next to the other top-level commands. Insert after line 109 (`root.AddCommand(newContextCmd(st))`):

```go
	root.AddCommand(newWorkflowCmd(st))
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/cli/ -run TestWorkflow -v`
Expected: PASS — all workflow CLI tests pass.

- [ ] **Step 6: Run the full CLI test suite to catch regressions**

Run: `go test ./internal/cli/`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/cli/workflow.go internal/cli/workflow_test.go internal/cli/root.go
git commit -m "feat(cli): add atm workflow command tree"
```

---

## Task 6: Wire EnsureVocabulary call sites to the three-board version

**Files:**
- Verify (no code change expected): `internal/cli/project.go:46`, `internal/cli/label.go:164`, `internal/tui/projects.go:243`, `internal/tui/app.go:186`, `internal/tui/labels.go:259`.

**Interfaces:**
- Consumes: `workflow.EnsureVocabulary` (now seeds all three), `workflow.BoardOpenTasks`.

The existing call sites already invoke `workflow.EnsureVocabulary`, which Task 2 extended to seed all three boards. No source change is required here. This task verifies that the broader test suite still passes and that the TUI ring picks up the new boards as normal members.

> **Amendment 2026-07-16 (found during Task 2):** this plan originally assumed `go test ./...` would simply PASS here. It does not. `atm project create` (`internal/cli/project.go:46`) calls `EnsureVocabulary`, which now emits **two extra label-seed events** per project creation. ATM derives task ids and event `seq` from mutation history, so those two events shift every downstream id and seq in a freshly-created store — invalidating ~18 `internal/cli` golden/determinism fixtures that hardcode them. This is intended behavior, not a bug: the fixtures were stale. They were regenerated in a dedicated commit right after Task 2, with the regenerated diff reviewed to confirm it contained ONLY the two new boards and mechanical id/seq shifts. If you still see golden failures here, do not blind-regenerate — read the diff first.

- [ ] **Step 1: Run the TUI tests**

Run: `go test ./internal/tui/`
Expected: PASS. If any test asserts the exact board-ring membership count or order, it will need updating to account for `backlog` and `in-progress-tasks` — update those assertions to match the new ring (they are normal members, sorted by display name by `buildBoardRows`).

- [ ] **Step 2: Run the full test suite**

Run: `go test ./...`
Expected: PASS.

- [ ] **Step 3: If any TUI test needed an assertion update, commit it**

```bash
git add internal/tui/
git commit -m "test(tui): account for backlog + in-progress-tasks in board ring"
```

If no changes were needed, skip the commit.

---

## Task 7: Conventions update (TDD)

**Files:**
- Modify: `internal/cli/conventions.go` (text + structured map)
- Modify: `internal/cli/conventions_test.go`
- Regenerate: `internal/cli/testdata/golden/conventions-text.json`, `internal/cli/testdata/golden/conventions-json.json`, `internal/cli/testdata/golden/determinism-conventions.json`

**Interfaces:**
- Produces: conventions text + JSON mention the workflow verbs, the three boards, and the softened "workflow lives in capabilities" wording.

- [ ] **Step 1: Write the failing test assertions**

Append to `internal/cli/conventions_test.go`:

```go
func TestConventionsMentionWorkflowVerbs(t *testing.T) {
	for _, verb := range []string{
		"atm workflow start", "atm workflow open",
		"atm workflow block", "atm workflow complete", "atm workflow status",
		"atm workflow seed",
	} {
		if !strings.Contains(conventionsText, verb) {
			t.Errorf("conventions text missing %q", verb)
		}
	}
	js := conventionsStructured()
	wv, _ := js["workflow_verbs"].(string)
	for _, verb := range []string{"atm workflow start", "atm workflow complete", "atm workflow status"} {
		if !strings.Contains(wv, verb) {
			t.Errorf("workflow_verbs JSON missing %q", verb)
		}
	}
}

func TestConventionsMentionBacklogBoard(t *testing.T) {
	if !strings.Contains(conventionsText, "backlog") {
		t.Error("conventions text must reference the backlog board")
	}
	js := conventionsStructured()
	seq, _ := js["agent_first_contact_sequence"].([]string)
	joined := strings.Join(seq, " ")
	if !strings.Contains(joined, "backlog") {
		t.Error("agent_first_contact_sequence must reference the backlog board")
	}
}

func TestConventionsSoftenedWorkflowWording(t *testing.T) {
	// The store stays neutral; the paved road lives in a capability.
	if !strings.Contains(conventionsText, "capability") {
		t.Error("conventions text must mention that workflow lives in a capability")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/cli/ -run 'TestConversations|TestConventions' -v`
Expected: FAIL — the new assertions miss `atm workflow ...`, `backlog`, and `capability`.

- [ ] **Step 3: Update the conventions text**

In `internal/cli/conventions.go`, edit the `## What ATM is` paragraph (line 15). Replace the sentence:

> `Workflow lives outside the store, in agent prompts and human habits; the store only keeps the substrate legible.`

with:

> `Workflow lives in capabilities (internal/workflow), not in the store; the store only keeps the substrate legible. A capability is a paved road, not a fence — a project can replace it.`

Then, after the `## The context map` section (which ends before `## Actor identity`), insert a new section:

```text
## Workflow verbs (status transitions)

Status transitions live in the `internal/workflow` capability, exposed as `atm workflow` verbs — `start` (in-progress), `open`, `block` (blocked), `complete` (done) — plus a read-only `status` reporter and `seed` to ensure the boards. Each mutating verb swaps the task's `status:*` label (removes any existing one, adds the target), so exactly-one-status is an invariant the capability maintains. The store still enforces nothing: raw `atm task label add/remove --label <CODE>:status:<value>` works and a human may hand-assign, rename, or delete any status label. `internal/workflow` is a paved road, not a fence — a project can replace it with a different transition model.

Three boards are ensured on project create / label seed / TUI use: `ATM:backlog` (`NOT status:*` — untriaged jottings), `ATM:open-tasks` (`status:open` — active work, the TUI default), `ATM:in-progress-tasks` (`status:in-progress`). In an older project where a board is absent, the expression fallback applies (`--label <CODE>:status:open` etc.).
```

(Use the same backtick-escaping style as the surrounding `const conventionsText` raw string — concatenate with `+ "`..."` +` where needed, exactly like the existing sections.)

In the `## Agent first-contact sequence`, after step 4 (the `open-tasks` step), insert a new step:

```text
5. `atm workflow status <task-id>` / `atm workflow start <task-id>` — the paved road for status transitions; prefer these over raw `task label add/remove --label status:*`. Use `atm task list --project <CODE> --label <CODE>:backlog` to review untriaged jottings (in an older project, `--expr 'NOT status:*'` is equivalent).
```

Renumber the subsequent steps (5 -> 6, 6 -> 7, ... 9 -> 10) accordingly.

- [ ] **Step 4: Update the structured conventions map**

In `conventionsStructured()` in `internal/cli/conventions.go`:

1. Update the `"what_atm_is"` value string the same way as the text (replace the "Workflow lives outside the store..." sentence with the capability wording).
2. Add a new key:

```go
"workflow_verbs": "Status transitions live in the internal/workflow capability, exposed as atm workflow verbs: start (in-progress), open, block (blocked), complete (done), plus a read-only status reporter and seed. Each mutating verb swaps the task's status:* label (removes any existing one, adds the target), so exactly-one-status is an invariant the capability maintains. The store enforces nothing; raw atm task label add/remove --label <CODE>:status:<value> still works. internal/workflow is a paved road, not a fence — a project can replace it.",
```

3. In the `"agent_first_contact_sequence"` slice, insert the new step (the workflow status / backlog step) right after the `open-tasks` step, and keep the subsequent steps in order.

- [ ] **Step 5: Run the conventions tests to verify they pass**

Run: `go test ./internal/cli/ -run TestConventions -v`
Expected: PASS — including the three new tests. The golden tests will now FAIL because the output changed; that's expected and is fixed in the next step.

- [ ] **Step 6: Regenerate the golden files**

The repo's golden harness uses a `-update` flag (see `internal/cli/harness_test.go:48`: `var updateGolden = flag.Bool("update", false, ...)`). Run:

```bash
go test ./internal/cli/ -run 'TestConventionsText|TestConversationsJSON|TestDeterminism' -update
```

This rewrites the golden files with the new expected content. The files affected:
- `internal/cli/testdata/golden/conventions-text.json`
- `internal/cli/testdata/golden/conventions-json.json`
- `internal/cli/testdata/golden/determinism-conventions.json` — only if it references conventions content. Verify by grepping it first:

```bash
grep -l "conventions\|open-tasks\|status:open" internal/cli/testdata/golden/determinism-conventions.json 2>/dev/null || echo "no determinism-conventions golden to update"
```

If the determinism golden does not reference conventions content, skip it.

- [ ] **Step 7: Run the full conventions + golden tests**

Run: `go test ./internal/cli/ -run 'TestConventions|TestDeterminism' -v`
Expected: PASS — golden files match.

- [ ] **Step 8: Commit**

```bash
git add internal/cli/conventions.go internal/cli/conventions_test.go internal/cli/testdata/golden/
git commit -m "docs(conventions): document the workflow capability verbs and boards"
```

---

## Task 8: Full verification

- [ ] **Step 1: Run the repository verification gate**

Run: `make verify`
Expected: PASS — builds, lints, tests, golden files all green.

- [ ] **Step 2: Manual smoke test (optional but recommended)**

```bash
SP=$(mktemp -d)
atm init --store "$SP" --actor admin@cli:unset
atm project create --store "$SP" --code TST --name "smoke" --actor admin@cli:unset
atm workflow seed --store "$SP" --project TST --actor admin@cli:unset
# Create a naked jotting (no labels):
T=$(atm task create --store "$SP" --project TST --title "quick jotting" --actor admin@cli:unset)
# It should be untriaged:
atm workflow status --store "$SP" --task "$T"
# It should appear under the backlog board:
atm task list --store "$SP" --project TST --label TST:backlog
# Start it:
atm workflow start --store "$SP" --task "$T" --actor admin@cli:unset
atm workflow status --store "$SP" --task "$T"
# It should now appear under in-progress-tasks, not backlog:
atm task list --store "$SP" --project TST --label TST:in-progress-tasks
```

Expected: the naked task is `untriaged` and appears under `TST:backlog`; after `start` it is `in-progress` and appears under `TST:in-progress-tasks`.

- [ ] **Step 3: Record completion in the ATM ledger**

Stamp the task with a progress comment per the atm-developing skill:

```bash
atm task comment add --task ATM-e23fe5 --body "Implementation complete per plan docs/superpowers/plans/2026-07-16-workflow-capability.md; make verify green." --label ATM:comment:progress --actor <your-actor>
```

Then transition the task to done via the new paved road (dogfooding):

```bash
atm workflow complete --task ATM-e23fe5 --actor <your-actor>
```

Do NOT stamp ATM-18111b — that is the separate recently-updated-tasks-board feature this work was split off from, and it stays open.

---

## Self-Review Notes

- **Spec coverage:** backlog board (Task 2), in-progress-tasks board (Task 2), open-tasks retained (Task 2), five scrum verbs with swap (Tasks 4 + 5), status reporter + purity (Tasks 3 + 5), `atm workflow seed` (Task 5), ensure call sites wire to three-board version (Task 6), conventions text + JSON + first-contact step + softened wording (Task 7), golden regeneration (Task 7), TUI ring picks up new boards as normal members (Task 6), naked-task-under-backlog assertion (Task 8 smoke + Task 5 CLI test). Covered.
- **Placeholder scan:** Task 4 has one marked helper (`GetTaskOrFatal`) with an explicit fix instruction in the same task (use real `GetTask` API) — not a hidden placeholder. Task 5's implementation is fully concrete (no `currentStatusValue` placeholder remains). All other steps show complete code.
- **Type consistency:** `Recorder.SetStatus(taskID, target string) (prior string, err error)` is used identically in Task 4 (test + impl) and Task 5 (CLI). `Reporter.Status(taskID string) (string, error)` is used identically in Tasks 3, 4, 5. `EnsureVocabulary(s *store.Store, code, actor string) error` is used identically in Tasks 2, 5, 6. `BoardBacklog` / `BoardOpenTasks` / `BoardInProgressTasks` signatures match across tasks. Purity is checked via `store.LastLogSeq(code)` consistently in Tasks 3, 4, 5 (the store's actual staleness probe, not a fabricated `ProjectLog`).