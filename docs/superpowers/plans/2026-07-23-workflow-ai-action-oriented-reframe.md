# workflow_ai action-oriented reframe — implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Reframe the workflow_ai capability so boards orient around the next action, the ladder is five explicit artifact-gated rungs (queued → brainstormed → clarified → planned → done), `queued` is a real stored label, `clarify` gains a `--ref` spec locator, `planned` absorbs `implementable`, and `demote` resets to `queued`.

**Architecture:** Single-package change to `internal/capability/workflowai` (payload, stage, vocabulary, recorder, reporter, annotate, command, guide) plus the skills file `skills/capability/workflow_ai.md`. The payload gains a `spec` locator mirroring `plan`. Vocabulary reseed force-updates board labels/exprs and removes the three old boards. No cross-capability changes; the manager persona is unchanged.

**Tech Stack:** Go 1.22+, cobra CLI, atm internal store/core/capability packages.

**Spec:** `docs/superpowers/specs/2026-07-23-workflow-ai-action-oriented-reframe-design.md`

## Global Constraints

- Go 1.22+; `make build && make test` is the verify gate.
- No emojis in code or commits. Commit style: `<type>(ATM-711a9d): <summary>`.
- Follow existing style in neighboring files; no comments unless asked.
- The store enforces nothing — every invariant is a paved road maintained by the verbs.
- `LabelSeed` is a no-op on existing labels (preserves description); `LabelAdd` force-sets non-empty fields via `UpsertLabel`. Use `LabelAdd` to force-update board descriptions/exprs during reseed; use `LabelRemove` to delete old board labels.
- Unknown payload fields must survive every read-modify-write (degrade-never-reject).

---

### Task 1: Payload — add SpecRecord

**Files:**
- Modify: `internal/capability/workflowai/payload.go`
- Test: `internal/capability/workflowai/payload_test.go`

**Interfaces:**
- Produces: `SpecRecord` struct, `Payload.Spec() *SpecRecord`, `Payload.SetSpec(SpecRecord)`, `Payload.ClearSpec()` — mirror the `PlanRecord`/`Plan()`/`SetPlan`/`ClearPlan` shape exactly.

- [ ] **Step 1: Write the failing tests**

Add to `internal/capability/workflowai/payload_test.go`:

```go
func TestPayloadSpecRoundTrip(t *testing.T) {
	pl, _ := DecodePayload("")
	pl.SetSpec(SpecRecord{Kind: PlanKindFile, Ref: "docs/superpowers/specs/x.md", RecordedAt: "2026-07-23T00:00:00Z", Actor: "a"})
	out, err := pl.Encode()
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	back, err := DecodePayload(out)
	if err != nil {
		t.Fatalf("DecodePayload(%q): %v", out, err)
	}
	s := back.Spec()
	if s == nil || s.Kind != PlanKindFile || s.Ref != "docs/superpowers/specs/x.md" || s.RecordedAt != "2026-07-23T00:00:00Z" || s.Actor != "a" {
		t.Errorf("Spec = %+v", s)
	}
}

func TestPayloadSpecAndPlanCoexist(t *testing.T) {
	pl, _ := DecodePayload("")
	pl.SetSpec(SpecRecord{Kind: PlanKindFile, Ref: "spec.md", RecordedAt: "t0", Actor: "a"})
	pl.SetPlan(PlanRecord{Kind: PlanKindFile, Ref: "plan.md", RecordedAt: "t1", Actor: "b"})
	out, _ := pl.Encode()
	back, _ := DecodePayload(out)
	if s := back.Spec(); s == nil || s.Ref != "spec.md" {
		t.Errorf("spec lost: %+v", s)
	}
	if p := back.Plan(); p == nil || p.Ref != "plan.md" {
		t.Errorf("plan lost: %+v", p)
	}
}

func TestPayloadClearSpec(t *testing.T) {
	pl, _ := DecodePayload("")
	pl.SetSpec(SpecRecord{Kind: PlanKindFile, Ref: "s.md", RecordedAt: "t", Actor: "a"})
	pl.ClearSpec()
	if out, _ := pl.Encode(); out != "" {
		t.Errorf("Encode = %q, want \"\" after clearing the only field", out)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/capability/workflowai/ -run TestPayloadSpec -v`
Expected: FAIL — `SpecRecord` undefined, `Spec`/`SetSpec`/`ClearSpec` undefined.

- [ ] **Step 3: Implement SpecRecord and accessors**

Add to `internal/capability/workflowai/payload.go`, after the `PlanRecord` type and the `Plan()`/`SetPlan`/`ClearPlan` accessors:

```go
// SpecRecord is the typed spec locator stored in the payload: where the
// task's design spec lives. Kind is one of the PlanKind constants (the
// same three kinds as plans).
type SpecRecord struct {
	Kind       string
	Ref        string
	RecordedAt string
	Actor      string
}

// Spec returns the recorded spec locator, or nil when none is recorded.
func (p *Payload) Spec() *SpecRecord {
	m, ok := p.raw["spec"].(map[string]any)
	if !ok {
		return nil
	}
	return &SpecRecord{Kind: str(m["kind"]), Ref: str(m["ref"]), RecordedAt: str(m["recorded_at"]), Actor: str(m["actor"])}
}

func (p *Payload) SetSpec(r SpecRecord) {
	p.raw["spec"] = map[string]any{"kind": r.Kind, "ref": r.Ref, "recorded_at": r.RecordedAt, "actor": r.Actor}
}

func (p *Payload) ClearSpec() { delete(p.raw, "spec") }
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/capability/workflowai/ -run TestPayloadSpec -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/capability/workflowai/payload.go internal/capability/workflowai/payload_test.go
git commit -m "feat(ATM-711a9d): add SpecRecord locator to workflow_ai payload"
```

---

### Task 2: Stage constants — add queued, remove implementable

**Files:**
- Modify: `internal/capability/workflowai/stage.go`
- Test: `internal/capability/workflowai/payload_test.go` (the `TestFirstStageValue` case already covers stage values; update it)

**Interfaces:**
- Produces: `StageQueued = "queued"` (new real stored label). `StageImplementable` removed. `StageNew` sentinel stays.
- Consumes: nothing new.

- [ ] **Step 1: Update the stage constants**

In `internal/capability/workflowai/stage.go`, replace the stage const block:

```go
// Stage values: the ladder queued → brainstormed → clarified → planned →
// done. "New" is the ABSENCE of any stage:* label, not a stored label;
// StageNew is the sentinel guards and reporters use for it. queued is a
// real stored label — the explicit entry stamp into the cycle.
const (
	StageNew          = ""
	StageQueued       = "queued"
	StageBrainstormed = "brainstormed"
	StageClarified    = "clarified"
	StagePlanned      = "planned"
	StageDone         = "done"
)
```

Remove the `StageImplementable` constant. Update the package doc comment at the top of `stage.go` from `brainstorm→clarify→plan→ready cycle` to `queue→brainstorm→clarify→plan→done cycle`.

- [ ] **Step 2: Run the package to find all compile sites referencing StageImplementable**

Run: `go build ./internal/capability/workflowai/ 2>&1`
Expected: compile errors at every `StageImplementable` / `Ready` reference — these are the sites Tasks 3-6 fix. Do not fix them yet; this step only enumerates them.

- [ ] **Step 3: Commit the constant change (package will not compile yet — that's expected; the next tasks fix it)**

Skip commit; the package is broken until Tasks 3-6 land. Proceed to Task 3.

---

### Task 3: Vocabulary — new stage labels and action-oriented boards

**Files:**
- Modify: `internal/capability/workflowai/vocabulary.go`
- Test: `internal/capability/workflowai/vocabulary_test.go`

**Interfaces:**
- Produces: `BoardToBrainstorm`, `BoardToClarify`, `BoardToPlan`, `BoardToImplement` name helpers (replacing `BoardNewTasks`, `BoardBrainstormedTasks`, `BoardPlannedTasks`). `BoardRevisions` and `BoardDoneTasks` unchanged. Vocabulary lists `stage:queued` and drops `stage:implementable`. Each board selects exactly one stage.

- [ ] **Step 1: Write the failing tests**

Replace `internal/capability/workflowai/vocabulary_test.go` contents with:

```go
package workflowai

import (
	"path/filepath"
	"testing"

	"atm/internal/core"
	"atm/internal/store"
)

func newTestStore(t *testing.T) *store.Store {
	t.Helper()
	dir := t.TempDir()
	s, err := store.Open(filepath.Join(dir, "atm"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := s.Init(""); err != nil {
		t.Fatalf("init: %v", err)
	}
	if _, err := s.CreateProject("ATM", "Atm", "admin@cli:unset"); err != nil {
		t.Fatalf("create project: %v", err)
	}
	if _, err := EnsureVocabulary(s, "ATM", "admin@cli:unset"); err != nil {
		t.Fatalf("ensure vocabulary: %v", err)
	}
	return s
}

func TestEnsureVocabularySeedsStageAndMarkerLabels(t *testing.T) {
	s := newTestStore(t)
	got := map[string]bool{}
	for _, l := range s.LabelList("ATM", "") {
		got[l.Name] = true
	}
	for _, want := range []string{
		"ATM:stage:*", "ATM:stage:queued", "ATM:stage:brainstormed",
		"ATM:stage:clarified", "ATM:stage:planned", "ATM:stage:done",
		"ATM:wfai:*", "ATM:wfai:revision", "ATM:wfai:framework",
		"ATM:to-brainstorm", "ATM:to-clarify", "ATM:to-plan",
		"ATM:to-implement", "ATM:revisions", "ATM:done-tasks",
	} {
		if !got[want] {
			t.Errorf("missing label %s", want)
		}
	}
	for _, gone := range []string{
		"ATM:stage:implementable", "ATM:new-tasks",
		"ATM:brainstormed-tasks", "ATM:planned-tasks",
	} {
		if got[gone] {
			t.Errorf("old label %s should not be seeded", gone)
		}
	}
}

func TestEnsureVocabularyReturnsTheSixBoards(t *testing.T) {
	s := newTestStore(t)
	boards, err := EnsureVocabulary(s, "ATM", "admin@cli:unset")
	if err != nil {
		t.Fatalf("EnsureVocabulary: %v", err)
	}
	if len(boards) != 6 {
		t.Fatalf("boards = %d, want 6", len(boards))
	}
	for _, b := range boards {
		if b.Expr == "" {
			t.Errorf("board %s has empty expr", b.Name)
		}
		if _, err := core.ParseExpr(b.Expr); err != nil {
			t.Errorf("board %s expr %q does not parse: %v", b.Name, b.Expr, err)
		}
	}
}

func TestExposedIsSubsetOfVocabulary(t *testing.T) {
	vocab := map[string]bool{}
	for _, l := range Vocabulary("ATM") {
		vocab[l.Name] = true
	}
	for _, l := range Exposed("ATM") {
		if !vocab[l.Name] {
			t.Errorf("Exposed label %s not in Vocabulary", l.Name)
		}
	}
	if len(Exposed("ATM")) != 8 { // 6 boards + 2 namespace descriptors
		t.Errorf("Exposed = %d entries, want 8", len(Exposed("ATM")))
	}
}

func TestBoardsSelectByStage(t *testing.T) {
	s := newTestStore(t)
	actor := "admin@cli:unset"
	queued, _ := s.CreateTask("ATM", "q", "", []string{"ATM:stage:queued"}, actor)
	br, _ := s.CreateTask("ATM", "br", "", []string{"ATM:stage:brainstormed"}, actor)
	cl, _ := s.CreateTask("ATM", "cl", "", []string{"ATM:stage:clarified"}, actor)
	pl, _ := s.CreateTask("ATM", "pl", "", []string{"ATM:stage:planned"}, actor)
	rev, _ := s.CreateTask("ATM", "rev", "", []string{"ATM:stage:clarified", "ATM:wfai:revision"}, actor)
	done, _ := s.CreateTask("ATM", "dn", "", []string{"ATM:stage:done"}, actor)
	noStage, _ := s.CreateTask("ATM", "naked", "", nil, actor)

	find := func(board string) map[string]bool {
		out := map[string]bool{}
		for _, tk := range s.ListTasks(store.QueryFilters{Project: "ATM", Labels: []string{board}}) {
			out[tk.ID] = true
		}
		return out
	}
	if got := find(BoardToBrainstorm("ATM")); !got[queued.ID] || got[br.ID] || got[noStage.ID] {
		t.Errorf("to-brainstorm = %v", got)
	}
	if got := find(BoardToClarify("ATM")); !got[br.ID] || got[cl.ID] {
		t.Errorf("to-clarify = %v (want brainstormed only)", got)
	}
	if got := find(BoardToPlan("ATM")); !got[cl.ID] || got[pl.ID] || got[rev.ID] {
		t.Errorf("to-plan = %v (want clarified only)", got)
	}
	if got := find(BoardToImplement("ATM")); !got[pl.ID] || got[done.ID] {
		t.Errorf("to-implement = %v", got)
	}
	if got := find(BoardRevisions("ATM")); !got[rev.ID] || got[br.ID] || got[done.ID] {
		t.Errorf("revisions = %v", got)
	}
	if got := find(BoardDoneTasks("ATM")); !got[done.ID] || got[pl.ID] {
		t.Errorf("done-tasks = %v", got)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/capability/workflowai/ -run TestEnsureVocabulary -v`
Expected: FAIL — `BoardToBrainstorm` etc. undefined, old labels still seeded.

- [ ] **Step 3: Implement the new vocabulary**

Replace the body of `internal/capability/workflowai/vocabulary.go`:

```go
package workflowai

import "atm/internal/core"

// Board name helpers: callers select boards by name, never by expression.

// BoardToBrainstorm: tasks queued for brainstorming (stage:queued).
func BoardToBrainstorm(code string) string { return code + ":to-brainstorm" }

// BoardToClarify: tasks brainstormed, ready to clarify (stage:brainstormed).
func BoardToClarify(code string) string { return code + ":to-clarify" }

// BoardToPlan: tasks clarified, ready to plan (stage:clarified).
func BoardToPlan(code string) string { return code + ":to-plan" }

// BoardToImplement: tasks planned, cleared for implementation (stage:planned).
func BoardToImplement(code string) string { return code + ":to-implement" }

// BoardRevisions: open revision follow-ups of a bigger planned task.
func BoardRevisions(code string) string { return code + ":revisions" }

// BoardDoneTasks: tasks that completed the cycle.
func BoardDoneTasks(code string) string { return code + ":done-tasks" }

func toBrainstormExpr() string  { return "stage:queued" }
func toClarifyExpr() string     { return "stage:brainstormed" }
func toPlanExpr() string        { return "stage:clarified" }
func toImplementExpr() string   { return "stage:planned" }
func revisionsExpr() string     { return "wfai:revision AND NOT stage:done" }
func doneTasksExpr() string     { return "stage:done" }

// vocabulary is the single literal list every contract method derives from:
// stored/namespace labels first (Expr == ""), then the six boards, in seed
// order. Ownership (Vocabulary), ring display (Exposed), and seeding
// (EnsureVocabulary) all read this list, so they cannot diverge.
func vocabulary(code string) []core.Label {
	return []core.Label{
		{Name: code + ":stage:*", Description: "workflow_ai cycle stage; at most one stage label per task, absence means not in the cycle (not yet queued)"},
		{Name: code + ":stage:queued", Description: "workflow_ai stage: entry — in the cycle, not yet brainstormed"},
		{Name: code + ":stage:brainstormed", Description: "workflow_ai stage: the idea has been explored; ready to clarify"},
		{Name: code + ":stage:clarified", Description: "workflow_ai stage: scope and success criteria settled; a spec locator is recorded in the capability metadata"},
		{Name: code + ":stage:planned", Description: "workflow_ai stage: a plan locator is recorded in the capability metadata; cleared for implementation"},
		{Name: code + ":stage:done", Description: "workflow_ai stage: completed the queue→brainstorm→clarify→plan→implement cycle"},
		{Name: code + ":wfai:*", Description: "workflow_ai markers; machine topology lives in the capability metadata, markers only make it board-visible"},
		{Name: code + ":wfai:revision", Description: "revision follow-up of a bigger planned task (revision_of link in workflow_ai metadata)"},
		{Name: code + ":wfai:framework", Description: "project framework conventions (written during a Semantics pass, read at session start); the description is the note"},
		{Name: BoardToBrainstorm(code), Description: "tasks queued for brainstorming.", Expr: toBrainstormExpr()},
		{Name: BoardToClarify(code), Description: "tasks brainstormed, ready to clarify (write a spec).", Expr: toClarifyExpr()},
		{Name: BoardToPlan(code), Description: "tasks clarified, ready to plan.", Expr: toPlanExpr()},
		{Name: BoardToImplement(code), Description: "tasks planned, cleared for implementation.", Expr: toImplementExpr()},
		{Name: BoardRevisions(code), Description: "open revision follow-ups of a bigger planned task, still needing refinement.", Expr: revisionsExpr()},
		{Name: BoardDoneTasks(code), Description: "tasks that completed the workflow_ai cycle.", Expr: doneTasksExpr()},
	}
}

func Vocabulary(code string) []core.Label { return vocabulary(code) }

func Exposed(code string) []core.Label {
	byName := map[string]core.Label{}
	for _, l := range vocabulary(code) {
		byName[l.Name] = l
	}
	names := []string{
		BoardToBrainstorm(code), BoardToClarify(code), BoardToPlan(code),
		BoardToImplement(code), BoardRevisions(code), BoardDoneTasks(code),
		code + ":stage:*", code + ":wfai:*",
	}
	out := make([]core.Label, 0, len(names))
	for _, n := range names {
		out = append(out, byName[n])
	}
	return out
}

// EnsureVocabulary seeds this capability's full vocabulary idempotently.
// New boards are seeded via LabelAdd (force-sets description/expr); old
// boards from the prior vocabulary are removed via LabelRemove so a reseed
// over an existing project converges to the new board set.
func EnsureVocabulary(s core.LabelService, code, actor string) ([]core.Label, error) {
	var boards []core.Label
	for _, l := range vocabulary(code) {
		if err := s.LabelSeed(l.Name, l.Description, l.Expr, actor); err != nil {
			return nil, err
		}
		if l.Expr != "" {
			boards = append(boards, l)
		}
	}
	for _, gone := range []string{
		code + ":new-tasks", code + ":brainstormed-tasks", code + ":planned-tasks",
	} {
		_, _ = s.LabelRemove(gone, actor)
	}
	return boards, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/capability/workflowai/ -run TestEnsureVocabulary -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/capability/workflowai/vocabulary.go internal/capability/workflowai/vocabulary_test.go
git commit -m "feat(ATM-711a9d): action-oriented boards + queued stage label"
```

---

### Task 4: Recorder — queue, clarify --ref, remove ready, demote to queued

**Files:**
- Modify: `internal/capability/workflowai/recorder.go`
- Test: `internal/capability/workflowai/recorder_test.go`

**Interfaces:**
- Produces: `Recorder.Queue(taskID) (string, error)` (new → queued). `Recorder.Clarify(taskID, kind, ref string) (string, error)` (now writes spec locator). `Recorder.Ready` removed. `Recorder.Done` transitions `planned → done`. `Recorder.Demote` resets to `queued` and clears both `spec` and `plan`.

- [ ] **Step 1: Write the failing tests**

Replace `internal/capability/workflowai/recorder_test.go` contents with:

```go
package workflowai

import (
	"strings"
	"testing"
	"time"

	"atm/internal/store"
)

const testActor = "admin@cli:unset"

func fixedNow() time.Time { return time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC) }

func newRecorder(s *store.Store) *Recorder {
	return &Recorder{Store: s, Actor: testActor, Now: fixedNow}
}

func stageLabelCount(t *testing.T, s *store.Store, id string) int {
	t.Helper()
	tk, err := s.GetTask(id)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	n := 0
	for _, l := range tk.Labels {
		if strings.HasPrefix(l, "ATM:stage:") {
			n++
		}
	}
	return n
}

func TestLadderHappyPath(t *testing.T) {
	s := newTestStore(t)
	tk, _ := s.CreateTask("ATM", "t", "", nil, testActor)
	r := newRecorder(s)

	steps := []struct {
		call               func() (string, error)
		wantPrior, wantNow string
	}{
		{func() (string, error) { return r.Queue(tk.ID) }, StageNew, StageQueued},
		{func() (string, error) { return r.Brainstorm(tk.ID) }, StageQueued, StageBrainstormed},
		{func() (string, error) { return r.Clarify(tk.ID, PlanKindFile, "docs/superpowers/specs/x.md") }, StageBrainstormed, StageClarified},
		{func() (string, error) { return r.Plan(tk.ID, PlanKindFile, "docs/superpowers/plans/x.md") }, StageClarified, StagePlanned},
		{func() (string, error) { return r.Done(tk.ID) }, StagePlanned, StageDone},
	}
	for i, st := range steps {
		prior, err := st.call()
		if err != nil {
			t.Fatalf("step %d: %v", i, err)
		}
		if prior != st.wantPrior {
			t.Errorf("step %d prior = %q, want %q", i, prior, st.wantPrior)
		}
		got, _ := (&Reporter{Store: s}).Stage(tk.ID)
		if got != st.wantNow {
			t.Errorf("step %d stage = %q, want %q", i, got, st.wantNow)
		}
		if n := stageLabelCount(t, s, tk.ID); n != 1 {
			t.Errorf("step %d stage label count = %d, want 1", i, n)
		}
	}
}

func TestLadderRejectsSkippedRungs(t *testing.T) {
	s := newTestStore(t)
	r := newRecorder(s)
	tk, _ := s.CreateTask("ATM", "t", "", nil, testActor)

	cases := []struct {
		name    string
		call    func() (string, error)
		wantMsg string
	}{
		{"brainstorm from new (must queue first)", func() (string, error) { return r.Brainstorm(tk.ID) }, "brainstorm requires queued"},
		{"clarify from new", func() (string, error) { return r.Clarify(tk.ID, PlanKindFile, "x") }, "clarify requires brainstormed"},
		{"plan from new", func() (string, error) { return r.Plan(tk.ID, PlanKindFile, "x") }, "plan requires clarified"},
		{"done from new", func() (string, error) { return r.Done(tk.ID) }, "done requires planned"},
	}
	for _, c := range cases {
		if _, err := c.call(); err == nil || !strings.Contains(err.Error(), c.wantMsg) {
			t.Errorf("%s: err = %v, want containing %q", c.name, err, c.wantMsg)
		}
	}
	// And a rung above: brainstorm on a staged task fails.
	tk2, _ := s.CreateTask("ATM", "t2", "", []string{"ATM:stage:clarified"}, testActor)
	if _, err := r.Brainstorm(tk2.ID); err == nil || !strings.Contains(err.Error(), "brainstorm requires queued") {
		t.Errorf("brainstorm on clarified: err = %v", err)
	}
}

func TestVerbIsIdempotentNoOp(t *testing.T) {
	s := newTestStore(t)
	r := newRecorder(s)
	tk, _ := s.CreateTask("ATM", "t", "", []string{"ATM:stage:brainstormed"}, testActor)
	prior, err := r.Brainstorm(tk.ID)
	if err != nil {
		t.Fatalf("Brainstorm: %v", err)
	}
	if prior != StageBrainstormed {
		t.Errorf("prior = %q, want %q (no-op signals prior == target)", prior, StageBrainstormed)
	}
}

func TestSwapSelfHealsHandEditedMultiStage(t *testing.T) {
	s := newTestStore(t)
	r := newRecorder(s)
	tk, _ := s.CreateTask("ATM", "t", "", []string{"ATM:stage:brainstormed", "ATM:stage:clarified"}, testActor)
	if _, err := r.Clarify(tk.ID, PlanKindFile, "docs/s.md"); err != nil {
		t.Fatalf("Clarify: %v", err)
	}
	if n := stageLabelCount(t, s, tk.ID); n != 1 {
		t.Errorf("stage label count = %d, want 1 (self-healed)", n)
	}
	if got, _ := (&Reporter{Store: s}).Stage(tk.ID); got != StageClarified {
		t.Errorf("stage = %q, want %q", got, StageClarified)
	}
}

func TestClarifyRecordsSpecLocator(t *testing.T) {
	s := newTestStore(t)
	r := newRecorder(s)
	tk, _ := s.CreateTask("ATM", "t", "", []string{"ATM:stage:brainstormed"}, testActor)
	if _, err := r.Clarify(tk.ID, PlanKindFile, "docs/superpowers/specs/x.md"); err != nil {
		t.Fatalf("Clarify: %v", err)
	}
	got, _ := s.GetTask(tk.ID)
	pl, _ := DecodePayload(got.Meta[CapabilityName])
	sp := pl.Spec()
	if sp == nil || sp.Kind != PlanKindFile || sp.Ref != "docs/superpowers/specs/x.md" || sp.Actor != testActor {
		t.Errorf("spec = %+v", sp)
	}
	if sp.RecordedAt != "2026-07-23T12:00:00Z" {
		t.Errorf("recorded_at = %q (injectable clock not used?)", sp.RecordedAt)
	}
}

func TestClarifyUpdatesInPlaceFromClarified(t *testing.T) {
	s := newTestStore(t)
	r := newRecorder(s)
	tk, _ := s.CreateTask("ATM", "t", "", []string{"ATM:stage:brainstormed"}, testActor)
	_, _ = r.Clarify(tk.ID, PlanKindEphemeral, "session 2026-07-20")
	prior, err := r.Clarify(tk.ID, PlanKindFile, "docs/s.md")
	if err != nil {
		t.Fatalf("re-clarify: %v", err)
	}
	if prior != StageClarified {
		t.Errorf("prior = %q, want %q (update-in-place signals current stage)", prior, StageClarified)
	}
	if got, _ := (&Reporter{Store: s}).Stage(tk.ID); got != StageClarified {
		t.Errorf("stage = %q, want still clarified", got)
	}
	got, _ := s.GetTask(tk.ID)
	pl, _ := DecodePayload(got.Meta[CapabilityName])
	if sp := pl.Spec(); sp == nil || sp.Kind != PlanKindFile {
		t.Errorf("spec not updated: %+v", sp)
	}
}

func TestClarifyRejectsBadKind(t *testing.T) {
	s := newTestStore(t)
	r := newRecorder(s)
	tk, _ := s.CreateTask("ATM", "t", "", []string{"ATM:stage:brainstormed"}, testActor)
	if _, err := r.Clarify(tk.ID, "url", "https://x"); err == nil || !strings.Contains(err.Error(), "invalid spec kind") {
		t.Errorf("err = %v", err)
	}
}

func TestClarifyRejectsEmptyRef(t *testing.T) {
	s := newTestStore(t)
	r := newRecorder(s)
	tk, _ := s.CreateTask("ATM", "t", "", []string{"ATM:stage:brainstormed"}, testActor)
	if _, err := r.Clarify(tk.ID, PlanKindFile, "  "); err == nil || !strings.Contains(err.Error(), "non-empty") {
		t.Errorf("err = %v", err)
	}
}

func TestPlanRecordsLocatorAndTransitions(t *testing.T) {
	s := newTestStore(t)
	r := newRecorder(s)
	tk, _ := s.CreateTask("ATM", "t", "", []string{"ATM:stage:clarified"}, testActor)
	if _, err := r.Plan(tk.ID, PlanKindFile, "docs/superpowers/plans/x.md"); err != nil {
		t.Fatalf("Plan: %v", err)
	}
	got, _ := s.GetTask(tk.ID)
	pl, _ := DecodePayload(got.Meta[CapabilityName])
	p := pl.Plan()
	if p == nil || p.Kind != PlanKindFile || p.Ref != "docs/superpowers/plans/x.md" || p.Actor != testActor {
		t.Errorf("plan = %+v", p)
	}
}

func TestPlanUpdatesInPlaceFromPlanned(t *testing.T) {
	s := newTestStore(t)
	r := newRecorder(s)
	tk, _ := s.CreateTask("ATM", "t", "", []string{"ATM:stage:clarified"}, testActor)
	_, _ = r.Plan(tk.ID, PlanKindEphemeral, "session 2026-07-20")
	prior, err := r.Plan(tk.ID, PlanKindFile, "docs/p.md")
	if err != nil {
		t.Fatalf("re-plan: %v", err)
	}
	if prior != StagePlanned {
		t.Errorf("prior = %q, want %q", prior, StagePlanned)
	}
	if got, _ := (&Reporter{Store: s}).Stage(tk.ID); got != StagePlanned {
		t.Errorf("stage = %q, want still planned", got)
	}
}

func TestPlanRejectsBadKind(t *testing.T) {
	s := newTestStore(t)
	r := newRecorder(s)
	tk, _ := s.CreateTask("ATM", "t", "", []string{"ATM:stage:clarified"}, testActor)
	if _, err := r.Plan(tk.ID, "url", "https://x"); err == nil || !strings.Contains(err.Error(), "invalid plan kind") {
		t.Errorf("err = %v", err)
	}
}

func TestDemoteClearsStageAndArtifactsKeepsLinks(t *testing.T) {
	s := newTestStore(t)
	r := newRecorder(s)
	parent, _ := s.CreateTask("ATM", "parent", "", []string{"ATM:stage:planned"}, testActor)
	tk, _ := s.CreateTask("ATM", "t", "", []string{"ATM:stage:brainstormed"}, testActor)
	_, _ = r.Clarify(tk.ID, PlanKindEphemeral, "spec session x")
	_, _ = r.Plan(tk.ID, PlanKindEphemeral, "plan session x")
	if err := r.LinkRevisionOf(tk.ID, parent.ID); err != nil {
		t.Fatalf("link: %v", err)
	}
	prior, err := r.Demote(tk.ID, "artifacts lost in session cleanup")
	if err != nil {
		t.Fatalf("Demote: %v", err)
	}
	if prior != StagePlanned {
		t.Errorf("prior = %q, want %q", prior, StagePlanned)
	}
	got, _ := s.GetTask(tk.ID)
	if got, _ := (&Reporter{Store: s}).Stage(tk.ID); got != StageQueued {
		t.Errorf("after demote stage = %q, want %q", got, StageQueued)
	}
	pl, _ := DecodePayload(got.Meta[CapabilityName])
	if pl.Plan() != nil {
		t.Error("plan record survived demote")
	}
	if pl.Spec() != nil {
		t.Error("spec record survived demote")
	}
	if pl.RevisionOf() != parent.ID {
		t.Error("revision_of link did not survive demote")
	}
	if !containsString(got.Labels, "ATM:wfai:revision") {
		t.Error("revision marker did not survive demote")
	}
	comments, err := s.ListComments(tk.ID)
	if err != nil || len(comments) == 0 || !strings.Contains(comments[len(comments)-1].Body, "artifacts lost in session cleanup") {
		t.Errorf("demote reason comment missing: %v, %v", comments, err)
	}
}

func TestDemoteRequiresReason(t *testing.T) {
	s := newTestStore(t)
	r := newRecorder(s)
	tk, _ := s.CreateTask("ATM", "t", "", []string{"ATM:stage:brainstormed"}, testActor)
	if _, err := r.Demote(tk.ID, "  "); err == nil || !strings.Contains(err.Error(), "requires --reason") {
		t.Errorf("err = %v", err)
	}
}

func TestDemoteOfNewTaskIsNoOp(t *testing.T) {
	s := newTestStore(t)
	r := newRecorder(s)
	tk, _ := s.CreateTask("ATM", "t", "", nil, testActor)
	prior, err := r.Demote(tk.ID, "why not")
	if err != nil {
		t.Fatalf("Demote: %v", err)
	}
	if prior != StageNew {
		t.Errorf("prior = %q, want StageNew", prior)
	}
	got, _ := s.GetTask(tk.ID)
	if got.Meta[CapabilityName] != "" {
		t.Errorf("no-op demote wrote a payload: %q", got.Meta[CapabilityName])
	}
	if comments, _ := s.ListComments(tk.ID); len(comments) != 0 {
		t.Errorf("no-op demote wrote a comment")
	}
}

func TestDemoteOfQueuedTaskIsNoOp(t *testing.T) {
	s := newTestStore(t)
	r := newRecorder(s)
	tk, _ := s.CreateTask("ATM", "t", "", []string{"ATM:stage:queued"}, testActor)
	prior, err := r.Demote(tk.ID, "why not")
	if err != nil {
		t.Fatalf("Demote: %v", err)
	}
	if prior != StageQueued {
		t.Errorf("prior = %q, want StageQueued", prior)
	}
	if comments, _ := s.ListComments(tk.ID); len(comments) != 0 {
		t.Errorf("demote of a bare queued task wrote a comment")
	}
}

func TestVerbFailsOnMalformedPayload(t *testing.T) {
	s := newTestStore(t)
	r := newRecorder(s)
	tk, _ := s.CreateTask("ATM", "t", "", []string{"ATM:stage:brainstormed"}, testActor)
	if err := s.SetTaskCapabilityMeta(tk.ID, CapabilityName, "not json", testActor); err != nil {
		t.Fatalf("seed malformed payload: %v", err)
	}
	if _, err := r.Clarify(tk.ID, PlanKindFile, "docs/s.md"); err == nil || !strings.Contains(err.Error(), "hand-repair") {
		t.Errorf("err = %v, want the hand-repair error", err)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/capability/workflowai/ -run TestLadder -v`
Expected: FAIL — `Queue` undefined, `Clarify` signature mismatch, `Ready` references, `Done`/`Demote` transitions wrong.

- [ ] **Step 3: Implement the recorder changes**

In `internal/capability/workflowai/recorder.go`:

Replace the `Brainstorm`, `Clarify`, `Ready`, `Done` methods and add `Queue`. The `Plan` method stays, but its in-place update check must drop `StageImplementable`:

```go
// Queue stamps the explicit entry label: new → queued.
func (r *Recorder) Queue(taskID string) (string, error) {
	return r.setStage(taskID, "queue", StageQueued, StageNew)
}

// Brainstorm marks the idea explored: queued → brainstormed.
func (r *Recorder) Brainstorm(taskID string) (string, error) {
	return r.setStage(taskID, "brainstorm", StageBrainstormed, StageQueued)
}

// Clarify records the spec locator and, from brainstormed, advances to
// clarified. From clarified/planned it UPDATES the locator in place (stage
// untouched) — a moved spec file or a re-clarification pass. The payload is
// written before the label swap: a clarified task must never lack a spec
// record. Returns the prior stage (== current stage on update).
func (r *Recorder) Clarify(taskID, kind, ref string) (string, error) {
	switch kind {
	case PlanKindFile, PlanKindCommit, PlanKindEphemeral:
	default:
		return "", fmt.Errorf("invalid spec kind %q (want %s, %s or %s)", kind, PlanKindFile, PlanKindCommit, PlanKindEphemeral)
	}
	if strings.TrimSpace(ref) == "" {
		return "", fmt.Errorf("clarify requires a non-empty --ref")
	}
	tk, pl, err := r.taskPayload(taskID)
	if err != nil {
		return "", err
	}
	code, _, ok := core.ParseTaskID(taskID)
	if !ok {
		return "", fmt.Errorf("invalid task id %q", taskID)
	}
	_, vals, _, _ := stageState(tk, code, StageClarified)
	update := containsString(vals, StageClarified) || containsString(vals, StagePlanned)
	if !update && !containsString(vals, StageBrainstormed) {
		current := "new"
		if len(vals) > 0 {
			current = strings.Join(vals, ", ")
		}
		return "", fmt.Errorf("cannot clarify %s: stage is %s (clarify requires brainstormed, or clarified/planned to update the locator)", taskID, current)
	}
	pl.SetSpec(SpecRecord{Kind: kind, Ref: ref, RecordedAt: r.now(), Actor: r.Actor})
	if err := r.writePayload(taskID, pl); err != nil {
		return "", err
	}
	if update {
		return firstStageValue(tk.Labels, code), nil
	}
	return r.setStage(taskID, "clarify", StageClarified, StageBrainstormed)
}

// Plan records the plan locator and, from clarified, advances to planned.
// From planned it UPDATES the locator in place (stage untouched). The
// payload is written before the label swap. Returns the prior stage (==
// current stage on update).
func (r *Recorder) Plan(taskID, kind, ref string) (string, error) {
	switch kind {
	case PlanKindFile, PlanKindCommit, PlanKindEphemeral:
	default:
		return "", fmt.Errorf("invalid plan kind %q (want %s, %s or %s)", kind, PlanKindFile, PlanKindCommit, PlanKindEphemeral)
	}
	if strings.TrimSpace(ref) == "" {
		return "", fmt.Errorf("plan requires a non-empty --ref")
	}
	tk, pl, err := r.taskPayload(taskID)
	if err != nil {
		return "", err
	}
	code, _, ok := core.ParseTaskID(taskID)
	if !ok {
		return "", fmt.Errorf("invalid task id %q", taskID)
	}
	_, vals, _, _ := stageState(tk, code, StagePlanned)
	update := containsString(vals, StagePlanned)
	if !update && !containsString(vals, StageClarified) {
		current := "new"
		if len(vals) > 0 {
			current = strings.Join(vals, ", ")
		}
		return "", fmt.Errorf("cannot plan %s: stage is %s (plan requires clarified, or planned to update the locator)", taskID, current)
	}
	pl.SetPlan(PlanRecord{Kind: kind, Ref: ref, RecordedAt: r.now(), Actor: r.Actor})
	if err := r.writePayload(taskID, pl); err != nil {
		return "", err
	}
	if update {
		return firstStageValue(tk.Labels, code), nil
	}
	return r.setStage(taskID, "plan", StagePlanned, StageClarified)
}

// Done closes the cycle: planned → done.
func (r *Recorder) Done(taskID string) (string, error) {
	return r.setStage(taskID, "done", StageDone, StagePlanned)
}
```

Remove the `Ready` method entirely.

Update `Demote` so that after clearing labels it stamps `StageQueued` (not nothing), and clears both `spec` and `plan`. Replace the body of `Demote`:

```go
// Demote resets the task to queued from any stage: clears the spec and plan
// records, writes the demoted breadcrumb, appends the reason as a task
// comment, and stamps stage:queued so the task stays in the cycle on the
// to-brainstorm board. Links and the revision marker survive — topology is
// true regardless of stage. A task already queued with no artifacts is a
// pure no-op. Payload is written first, labels second, comment last: every
// partial-failure state converges on re-run.
func (r *Recorder) Demote(taskID, reason string) (string, error) {
	if strings.TrimSpace(reason) == "" {
		return "", fmt.Errorf("demote requires --reason")
	}
	tk, pl, err := r.taskPayload(taskID)
	if err != nil {
		return "", err
	}
	code, _, ok := core.ParseTaskID(taskID)
	if !ok {
		return "", fmt.Errorf("invalid task id %q", taskID)
	}
	existing, _, _, _ := stageState(tk, code, "\x00none")
	prior := firstStageValue(tk.Labels, code)
	// Pure no-op: a queued (or new) task with no artifacts and no stage to clear.
	if (prior == StageQueued || prior == StageNew) && pl.Plan() == nil && pl.Spec() == nil && len(existing) <= (map[string]bool{})["x"] {
		// existing has the queued label if prior == queued; nothing to do.
		if prior == StageQueued && len(existing) == 1 {
			return StageQueued, nil
		}
		if prior == StageNew && len(existing) == 0 {
			return StageNew, nil
		}
	}
	pl.ClearPlan()
	pl.ClearSpec()
	pl.SetDemoted(Demotion{At: r.now(), By: r.Actor, Reason: reason})
	if err := r.writePayload(taskID, pl); err != nil {
		return prior, err
	}
	for _, l := range existing {
		if l == code+":"+StageNamespace+":"+StageQueued {
			continue
		}
		if err := r.Store.TaskLabelRemove(taskID, l, r.Actor); err != nil {
			return prior, fmt.Errorf("remove %s: %w", l, err)
		}
	}
	// Stamp queued (idempotent: setStage adds it if absent, removes nothing).
	if _, err := r.setStage(taskID, "demote", StageQueued, StageNew); err != nil {
		// setStage from StageNew allows when no stage label exists; if the
		// task had a non-queued stage we already removed it above, so this
		// is the new→queued path. If it was already queued we skipped the
		// remove loop and this is a no-op.
		return prior, err
	}
	if _, err := r.Store.CreateComment(taskID, "workflow_ai: demoted to queued — "+reason, nil, "", r.Actor); err != nil {
		return prior, fmt.Errorf("demoted, but recording the reason comment failed: %w", err)
	}
	return prior, nil
}
```

Note: the no-op guard above is subtle. Simplify it — the clean expression is: if the task is queued (or new) AND has no plan AND no spec AND no non-queued stage labels to clear, it's a no-op. Rewrite the guard more readably before committing:

```go
	// Pure no-op: nothing to clear and nothing to stamp.
	hasArtifacts := pl.Plan() != nil || pl.Spec() != nil
	hasOtherStage := false
	for _, l := range existing {
		if l != code+":"+StageNamespace+":"+StageQueued {
			hasOtherStage = true
		}
	}
	if !hasArtifacts && !hasOtherStage {
		if prior == StageQueued {
			return StageQueued, nil
		}
		if prior == StageNew {
			return StageNew, nil
		}
	}
```

Use this readable form in the final code.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/capability/workflowai/ -run "TestLadder|TestClarify|TestDemote|TestVerbFails" -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/capability/workflowai/recorder.go internal/capability/workflowai/recorder_test.go
git commit -m "feat(ATM-711a9d): queue verb, clarify --ref, demote to queued, drop ready"
```

---

### Task 5: Reporter — verify spec and plan locators

**Files:**
- Modify: `internal/capability/workflowai/reporter.go`
- Test: `internal/capability/workflowai/reporter_test.go`

**Interfaces:**
- Produces: `Reporter.PlanCheck` now walks `stage:clarified OR stage:planned`, verifies `spec` on both and `plan` on planned. `Finding.Detail` distinguishes spec-at-risk from plan-at-risk.

- [ ] **Step 1: Write the failing tests**

Replace `internal/capability/workflowai/reporter_test.go` (keep the `TestDefaultVerifierFile`, `TestDefaultVerifierCommit`, `TestLinksOutboundAndInbound` tests unchanged — only replace `TestPlanCheckFindings`):

```go
func TestPlanCheckFindings(t *testing.T) {
	s := newTestStore(t)
	r := newRecorder(s)

	mkClarified := func(title, kind, ref string) string {
		tk, _ := s.CreateTask("ATM", title, "", []string{"ATM:stage:brainstormed"}, testActor)
		if _, err := r.Clarify(tk.ID, kind, ref); err != nil {
			t.Fatalf("clarify %s: %v", title, err)
		}
		return tk.ID
	}
	mkPlanned := func(title, kind, ref string) string {
		id := mkClarified(title, PlanKindFile, "docs/spec.md")
		if _, err := r.Plan(id, kind, ref); err != nil {
			t.Fatalf("plan %s: %v", title, err)
		}
		return id
	}
	goodSpec := mkClarified("goodSpec", PlanKindFile, "docs/good-spec.md")
	missingSpec := mkClarified("missingSpec", PlanKindFile, "docs/gone-spec.md")
	ephSpec := mkClarified("ephSpec", PlanKindEphemeral, "session 2026-07-14")
	goodPlan := mkPlanned("goodPlan", PlanKindFile, "docs/good-plan.md")
	missingPlan := mkPlanned("missingPlan", PlanKindFile, "docs/gone-plan.md")
	// Hand-edited to planned with NO records at all.
	bare, _ := s.CreateTask("ATM", "bare", "", []string{"ATM:stage:planned"}, testActor)
	// Malformed payload on a planned task.
	bad, _ := s.CreateTask("ATM", "bad", "", []string{"ATM:stage:planned"}, testActor)
	_ = s.SetTaskCapabilityMeta(bad.ID, CapabilityName, "not json", testActor)
	// A new task with no stage is out of scope entirely.
	_, _ = s.CreateTask("ATM", "outofscope", "", nil, testActor)

	verify := func(kind, ref string) (bool, string) {
		if ref == "docs/good-spec.md" || ref == "docs/good-plan.md" {
			return true, ""
		}
		return false, "file missing: " + ref
	}
	findings, healthy, err := (&Reporter{Store: s}).PlanCheck("ATM", verify)
	if err != nil {
		t.Fatalf("PlanCheck: %v", err)
	}
	if healthy != 2 {
		t.Errorf("healthy = %d, want 2 (%s, %s)", healthy, goodSpec, goodPlan)
	}
	byTask := map[string][]string{}
	for _, f := range findings {
		byTask[f.TaskID] = append(byTask[f.TaskID], f.Detail)
	}
	// missingSpec: spec missing (1 finding)
	if ds := byTask[missingSpec]; len(ds) != 1 || !strings.Contains(ds[0], "spec file missing") {
		t.Errorf("missingSpec: %v", ds)
	}
	// ephSpec: spec ephemeral unverifiable (1 finding)
	if ds := byTask[ephSpec]; len(ds) != 1 || !strings.Contains(ds[0], "spec") || !strings.Contains(ds[0], "unverifiable") {
		t.Errorf("ephSpec: %v", ds)
	}
	// missingPlan: plan missing (1 finding; spec is good)
	if ds := byTask[missingPlan]; len(ds) != 1 || !strings.Contains(ds[0], "plan file missing") {
		t.Errorf("missingPlan: %v", ds)
	}
	// bare: no spec + no plan (2 findings)
	if ds := byTask[bare.ID]; len(ds) != 2 {
		t.Errorf("bare: %v, want 2 findings (no spec + no plan)", ds)
	}
	// bad: payload unparseable (1 finding)
	if ds := byTask[bad.ID]; len(ds) != 1 || !strings.Contains(ds[0], "unparseable") {
		t.Errorf("bad: %v", ds)
	}
}
```

Keep the existing `TestDefaultVerifierFile`, `TestDefaultVerifierCommit`, `TestLinksOutboundAndInbound` tests as-is.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/capability/workflowai/ -run TestPlanCheck -v`
Expected: FAIL — spec not verified, findings count/detail wrong.

- [ ] **Step 3: Implement the reporter changes**

In `internal/capability/workflowai/reporter.go`, replace `PlanCheck`:

```go
// PlanCheck walks the project's clarified and planned tasks and reports
// every artifact at risk: unparseable payloads, missing spec/plan records,
// ephemeral locators (unverifiable by construction), and locators the
// verifier rejects. A clarified task is checked for its spec only; a
// planned task is checked for both spec and plan. Healthy artifacts are
// counted, not listed. Read-only: the reporter reports, the DECIDER
// demotes — never this code.
func (r *Reporter) PlanCheck(code string, verify PlanVerifier) ([]Finding, int, error) {
	tasks, err := r.Store.ListTasksErr(core.QueryFilters{Project: code, Expr: "stage:clarified OR stage:planned"})
	if err != nil {
		return nil, 0, err
	}
	var findings []Finding
	healthy := 0
	for _, tk := range tasks {
		stage := firstStageValue(tk.Labels, code)
		pl, err := DecodePayload(tk.Meta[CapabilityName])
		if err != nil {
			findings = append(findings, Finding{tk.ID, stage, "payload unparseable (hand-repair needed)"})
			continue
		}
		// Spec: checked on both clarified and planned.
		if s := pl.Spec(); s == nil {
			findings = append(findings, Finding{tk.ID, stage, "no spec recorded"})
		} else if s.Kind == PlanKindEphemeral {
			findings = append(findings, Finding{tk.ID, stage, "ephemeral spec: " + s.Ref + " (unverifiable)"})
		} else {
			if ok, detail := verify(s.Kind, s.Ref); !ok {
				findings = append(findings, Finding{tk.ID, stage, "spec " + detail})
			} else {
				healthy++
			}
		}
		// Plan: checked on planned only.
		if stage == StagePlanned {
			if p := pl.Plan(); p == nil {
				findings = append(findings, Finding{tk.ID, stage, "no plan recorded"})
			} else if p.Kind == PlanKindEphemeral {
				findings = append(findings, Finding{tk.ID, stage, "ephemeral plan: " + p.Ref + " (unverifiable)"})
			} else {
				if ok, detail := verify(p.Kind, p.Ref); !ok {
					findings = append(findings, Finding{tk.ID, stage, "plan " + detail})
				} else {
					healthy++
				}
			}
		}
	}
	return findings, healthy, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/capability/workflowai/ -run TestPlanCheck -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/capability/workflowai/reporter.go internal/capability/workflowai/reporter_test.go
git commit -m "feat(ATM-711a9d): report verifies spec and plan locators"
```

---

### Task 6: Annotate — update for new stages

**Files:**
- Modify: `internal/capability/workflowai/annotate.go`
- Test: `internal/capability/workflowai/annotate_test.go`

**Interfaces:**
- Produces: `Annotate` renders `queued`/`brainstormed`/`clarified`/`planned`/`done`. `planned` shows the plan kind (file/commit/ephemeral/no-plan). No more `implementable` cell. `clarified` shows the spec kind.

- [ ] **Step 1: Write the failing tests**

Replace `internal/capability/workflowai/annotate_test.go`:

```go
package workflowai

import (
	"testing"

	"atm/internal/capability"
	"atm/internal/core"
)

func TestAnnotateTable(t *testing.T) {
	planPayload := func(kind string) string {
		pl, _ := DecodePayload("")
		pl.SetPlan(PlanRecord{Kind: kind, Ref: "r", RecordedAt: "t", Actor: "a"})
		out, _ := pl.Encode()
		return out
	}
	specPayload := func(kind string) string {
		pl, _ := DecodePayload("")
		pl.SetSpec(SpecRecord{Kind: kind, Ref: "r", RecordedAt: "t", Actor: "a"})
		out, _ := pl.Encode()
		return out
	}
	bothPayload := func(specKind, planKind string) string {
		pl, _ := DecodePayload("")
		pl.SetSpec(SpecRecord{Kind: specKind, Ref: "s", RecordedAt: "t", Actor: "a"})
		pl.SetPlan(PlanRecord{Kind: planKind, Ref: "p", RecordedAt: "t", Actor: "a"})
		out, _ := pl.Encode()
		return out
	}
	cases := []struct {
		name     string
		labels   []string
		payload  string
		wantNil  bool
		wantText string
		wantTone capability.Tone
	}{
		{"no stage -> nil even with links", nil, `{"v":1,"revision_of":"ATM-aaaaaa"}`, true, "", 0},
		{"queued neutral", []string{"ATM:stage:queued"}, "", false, "queued", capability.ToneNeutral},
		{"brainstormed neutral", []string{"ATM:stage:brainstormed"}, "", false, "brainstormed", capability.ToneNeutral},
		{"clarified file neutral", []string{"ATM:stage:clarified"}, specPayload(PlanKindFile), false, "clarified·file", capability.ToneNeutral},
		{"clarified no-spec attention", []string{"ATM:stage:clarified"}, "", false, "clarified·no-spec", capability.ToneAttention},
		{"clarified ephemeral attention", []string{"ATM:stage:clarified"}, specPayload(PlanKindEphemeral), false, "clarified·ephemeral", capability.ToneAttention},
		{"planned file neutral", []string{"ATM:stage:planned"}, bothPayload(PlanKindFile, PlanKindFile), false, "planned·file", capability.ToneNeutral},
		{"planned no-plan attention", []string{"ATM:stage:planned"}, specPayload(PlanKindFile), false, "planned·no-plan", capability.ToneAttention},
		{"planned ephemeral-plan attention", []string{"ATM:stage:planned"}, bothPayload(PlanKindFile, PlanKindEphemeral), false, "planned·ephemeral", capability.ToneAttention},
		{"done neutral", []string{"ATM:stage:done"}, "", false, "done", capability.ToneNeutral},
		{"malformed payload degrades to label-only", []string{"ATM:stage:planned"}, "not json", false, "planned", capability.ToneNeutral},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tk := core.Task{ID: "ATM-1234", Labels: c.labels}
			if c.payload != "" {
				tk.Meta = map[string]string{CapabilityName: c.payload}
			}
			cell := Cap{}.Annotate(tk)
			if c.wantNil {
				if cell != nil {
					t.Fatalf("cell = %+v, want nil", cell)
				}
				return
			}
			if cell == nil {
				t.Fatal("cell = nil")
			}
			if cell.Text != c.wantText || cell.Tone != c.wantTone {
				t.Errorf("cell = {%q, %v}, want {%q, %v}", cell.Text, cell.Tone, c.wantText, c.wantTone)
			}
		})
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/capability/workflowai/ -run TestAnnotateTable -v`
Expected: FAIL — `queued` cell not rendered, `clarified` spec not shown, `implementable` cases gone.

- [ ] **Step 3: Implement the annotate changes**

Replace the body of `Annotate` in `internal/capability/workflowai/annotate.go`:

```go
func (Cap) Annotate(t core.Task) *capability.Cell {
	code, _, ok := core.ParseTaskID(t.ID)
	if !ok {
		return nil
	}
	stage := firstStageValue(t.Labels, code)
	if stage == StageNew {
		return nil
	}
	cell := &capability.Cell{Text: stage, Tone: capability.ToneNeutral}
	if stage == StageQueued || stage == StageBrainstormed || stage == StageDone {
		return cell
	}
	pl, err := DecodePayload(t.Meta[CapabilityName])
	if err != nil {
		return cell
	}
	if stage == StageClarified {
		s := pl.Spec()
		switch {
		case s == nil:
			cell.Text += "·no-spec"
			cell.Tone = capability.ToneAttention
		case s.Kind == PlanKindEphemeral:
			cell.Text += "·ephemeral"
			cell.Tone = capability.ToneAttention
		default:
			cell.Text += "·" + s.Kind
		}
		return cell
	}
	// stage == StagePlanned: show plan kind (spec already verified at clarify).
	p := pl.Plan()
	switch {
	case p == nil:
		cell.Text += "·no-plan"
		cell.Tone = capability.ToneAttention
	case p.Kind == PlanKindEphemeral:
		cell.Text += "·ephemeral"
		cell.Tone = capability.ToneAttention
	default:
		cell.Text += "·" + p.Kind
	}
	return cell
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/capability/workflowai/ -run TestAnnotateTable -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/capability/workflowai/annotate.go internal/capability/workflowai/annotate_test.go
git commit -m "feat(ATM-711a9d): annotate queued/clarified-spec/planned cells"
```

---

### Task 7: Commands — queue, clarify --ref, remove ready

**Files:**
- Modify: `internal/capability/workflowai/command.go`

**Interfaces:**
- Produces: `queue` stage command (new). `clarify` gains `--kind`/`--ref` flags (mirror `plan`). `ready` command removed. `report` short text updated. The long help text updated to the new ladder.

- [ ] **Step 1: Update the command tree**

In `internal/capability/workflowai/command.go`:

Update the `Cap.Command` long text and subcommands. Replace the `cmd.Long` string and the `AddCommand` calls:

```go
func (Cap) Command(env capability.Env) *cobra.Command {
	cmd := &cobra.Command{
		Use:   CapabilityName,
		Short: "AI-native task cycle: stage ladder, links, plan tracking (the workflow_ai paved road)",
		Long: "The workflow_ai capability climbs tasks through queue → " +
			"brainstorm → clarify → plan → done over the stage:* namespace, " +
			"one rung at a time; exactly-one-stage is an invariant the verbs " +
			"maintain, and machine state (spec locator, plan locator, links, " +
			"demotion breadcrumb) lives in this capability's metadata key. " +
			"The store enforces nothing. This is a paved road, not a fence.",
	}
	env.BindActorFlag(cmd)
	cmd.AddCommand(newStageCmd(env, "queue", "Stamp the entry label (new → queued)", (*Recorder).Queue))
	cmd.AddCommand(newStageCmd(env, "brainstorm", "Mark the idea brainstormed (queued → brainstormed)", (*Recorder).Brainstorm))
	cmd.AddCommand(newClarifyCmd(env))
	cmd.AddCommand(newPlanCmd(env))
	cmd.AddCommand(newStageCmd(env, "done", "Close the cycle (planned → done)", (*Recorder).Done))
	cmd.AddCommand(newDemoteCmd(env))
	cmd.AddCommand(newLinkCmd(env, true))
	cmd.AddCommand(newLinkCmd(env, false))
	cmd.AddCommand(newStageReportCmd(env))
	cmd.AddCommand(newLinksCmd(env))
	cmd.AddCommand(newSeedCmd(env))
	return cmd
}
```

Add the `newClarifyCmd` (mirrors `newPlanCmd` but calls `Clarify` and writes the spec locator):

```go
func newClarifyCmd(env capability.Env) *cobra.Command {
	var id, legacy, kind, ref string
	cmd := &cobra.Command{
		Use:   "clarify",
		Short: "Record the spec locator (brainstormed → clarified; from clarified/planned updates it in place)",
		RunE: func(cmd *cobra.Command, args []string) error {
			taskID, err := env.ResolveTaskID(id, legacy)
			if err != nil {
				return err
			}
			actor, err := env.RequireMutatingActor()
			if err != nil {
				return err
			}
			svc, err := env.OpenService()
			if err != nil {
				return err
			}
			rec := &Recorder{Store: svc, Actor: actor}
			prior, err := rec.Clarify(taskID, kind, ref)
			if err != nil {
				return err
			}
			t, err := svc.GetTask(taskID)
			if err != nil {
				return err
			}
			now, err := (&Reporter{Store: svc}).Stage(taskID)
			if err != nil {
				return err
			}
			return env.Emit(map[string]any{"task": env.TaskJSON(t), "spec": map[string]string{"kind": kind, "ref": ref}}, func() {
				if prior == now {
					fmt.Fprintf(env.Stdout(), "%s: spec updated (%s %s); stage %s unchanged\n", t.ID, kind, ref, stageDisplay(now))
					return
				}
				fmt.Fprintf(env.Stdout(), "%s: stage %s -> %s (spec: %s %s)\n", t.ID, stageDisplay(prior), stageDisplay(now), kind, ref)
			})
		},
	}
	env.BindTaskIDFlags(cmd, &id, &legacy)
	cmd.Flags().StringVar(&kind, "kind", "", "spec locator kind: file|commit|ephemeral")
	cmd.Flags().StringVar(&ref, "ref", "", "spec locator: repo-relative path, git revision, or ephemeral note")
	_ = cmd.MarkFlagRequired("kind")
	_ = cmd.MarkFlagRequired("ref")
	return cmd
}
```

Update the `newStageReportCmd` short text:

```go
		Short: "Check every clarified/planned task's spec/plan locator; list what is at risk (read-only)",
```

Update the `newDemoteCmd` short text:

```go
		Short: "Reset the task to queued (any stage; clears spec+plan records, keeps links, logs the reason)",
```

Remove the `newStageCmd(env, "ready", ...)` line entirely.

- [ ] **Step 2: Build the package**

Run: `go build ./internal/capability/workflowai/`
Expected: compiles clean (no `Ready` references remain).

- [ ] **Step 3: Run the full package tests**

Run: `go test ./internal/capability/workflowai/ -v`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add internal/capability/workflowai/command.go
git commit -m "feat(ATM-711a9d): queue/clarify commands, drop ready"
```

---

### Task 8: Guide — rewrite the skills file

**Files:**
- Modify: `skills/capability/workflow_ai.md`
- Test: `internal/capability/workflowai/guide_skills_test.go` (unchanged — it only checks section headers exist and Summary matches frontmatter)

**Interfaces:**
- Produces: updated frontmatter (description, labels, boards) and guide body (Semantics, Boards, Actions, Converge) reflecting the new ladder.

- [ ] **Step 1: Rewrite the skills file**

Replace `skills/capability/workflow_ai.md` with:

```markdown
---
name: workflow_ai
description: AI-native task cycle (queue→brainstorm→clarify→plan→done) with spec/plan tracking, links, and action-oriented stage boards.
labels: [stage:*, wfai:*]
boards: [to-brainstorm, to-clarify, to-plan, to-implement, revisions, done-tasks]
---
# workflow_ai capability — agent guide

The AI-native task cycle: queue → brainstorm → clarify → plan → done, over the `stage:*` namespace, with spec/plan locators, task links, and demotion breadcrumbs in this capability's metadata key. Independent of the `workflow` capability (`status:*`): the label namespaces are disjoint and neither capability's verbs touch the other's labels — a task may carry both views. Disjointness is about labels, not evidence: another capability's state is admissible evidence for staging (a `status:done` task with completed work behind it is evidence for `stage:done`).

## Semantics

Stage verbs climb the ladder one rung at a time; each swaps the task's `stage:*` label (adds the target, removes any other), so exactly-one-stage is an invariant the capability maintains. `queued` is a real stored label — the explicit entry stamp into the cycle. A task with no `stage:*` label is simply not in the workflow_ai cycle (it may be a context pointer belonging to `contextmap`, a naked jotting, or a deliberate deferral). The store enforces nothing: raw `atm task label add/remove` works. This is a paved road, not a fence.

Adoption: when this capability is enabled over an existing backlog, legacy tasks are stamped directly to the stage their existing evidence supports — completed work is its own evidence, so a finished task is stamped `stage:done` outright. Raw `atm task label add` is the sanctioned path for backfill; the one-rung climb governs live refinement, not adoption. Until the backlog is backfilled, the boards misread history as intake.

There is no `implement` verb: implementation is a dev session. The gate is doctrine — **never implement a task whose stage is not `stage:planned`**; check the stage first and refuse otherwise.

Stages (exactly one per task; absence = not in the cycle):
- `stage:queued` — entry; in the cycle, not yet brainstormed.
- `stage:brainstormed` — the idea has been explored; ready to clarify.
- `stage:clarified` — scope and success criteria settled; a spec locator is recorded in this capability's metadata.
- `stage:planned` — a plan locator is recorded; cleared for implementation.
- `stage:done` — completed the cycle.

Markers:
- `wfai:revision` — this task is a revision follow-up of a bigger planned task; the link itself (`revision_of`) lives in the metadata payload, the marker only makes it board-visible.
- `wfai:framework` — a stored label (not stamped on tasks) carrying the project's framework conventions in its description; written during setup, read at session start. See Converge.

Boards (seeded by `atm capability workflow_ai seed` / project create):
- `<CODE>:to-brainstorm` (`stage:queued`) — what to brainstorm next.
- `<CODE>:to-clarify` (`stage:brainstormed`) — what to clarify (write a spec for) next.
- `<CODE>:to-plan` (`stage:clarified`) — what to plan next.
- `<CODE>:to-implement` (`stage:planned`) — what can be implemented now.
- `<CODE>:revisions` (revision marker, not done) — follow-ups still needing refinement.
- `<CODE>:done-tasks` (`stage:done`).

Sizing doctrine: a task is sized to one plan a framework like superpowers can execute in a single session. When a planned task is bigger than that, split it: create follow-up tasks linked with `--revision-of`, each entering the cycle at its own stage. The revisions board is the refinement queue. The manager walks `revision_of` links to stay aware of follow-ups sharing a parent's plan.

## Actions

- `atm capability workflow_ai queue --task <ID>` — stamp the entry label (new → queued).
- `atm capability workflow_ai brainstorm --task <ID>` — mark the idea brainstormed (queued → brainstormed).
- `atm capability workflow_ai clarify --task <ID> --kind file|commit|ephemeral --ref <ref>` — record WHERE the spec lives: `--kind file --ref docs/superpowers/specs/...` (repo-relative), `--kind commit --ref <rev>`, or `--kind ephemeral --ref "session ..."` for a spec that lives in a conversation. Record ephemeral specs honestly — they are unverifiable and always at-risk.
- `atm capability workflow_ai plan --task <ID> --kind file|commit|ephemeral --ref <ref>` — record WHERE the plan lives (same kind/ref shape as clarify; clarified → planned, or updates the locator from planned).
- `atm capability workflow_ai done --task <ID>` — close the cycle (planned → done).
- `atm capability workflow_ai demote --task <ID> --reason "..."` — reset any stage back to queued, clear the spec+plan records, log the reason as a comment.
- `atm capability workflow_ai link --task X --revision-of Y` — X is a refinement follow-up of Y (one parent max). `--relates-to Y` — generic association. `unlink` reverses either. `links --task X` shows both directions.
- `atm capability workflow_ai report --project <CODE>` — the staleness check: verifies every clarified/planned task's spec and plan (file existence, commit resolvability from the current directory) and lists what is at risk. The reporter never demotes; the operator decides, then `demote --reason`.
- `atm capability workflow_ai seed --project <CODE>` — idempotently ensure vocabulary and boards.

## Converge

A converged project under this capability looks like:

- **The framework conventions are recorded and current.** The `<CODE>:wfai:framework` label's description says which framework the project uses (superpowers, speckit, grillme, or none), where specs and plans normally live (committed docs vs ephemeral sessions), sizing expectations, and any customizations. Agents read it at session start (`atm label show <CODE>:wfai:framework`) and bend accordingly; when practice drifts from what it says, it gets updated — convention changes are confirmed with the decider before the label is rewritten. Where plans normally live is also recorded in the `stage:planned` label description. Specific one-off answers stay as task comments; only conventions live in `wfai:framework`.
- **Coverage is total.** Every workflow task carries a stage validated against its evidence; absence means the task is not in the workflow_ai cycle (a context pointer, or un-triaged intake) — never "this task predates the capability". On adoption, backfilling stages across the existing backlog (see Semantics) is the first convergence job. Context pointers (`context:*`) are `contextmap`'s domain — never queue them into workflow_ai.
- **Every stage is evidenced.** A `stage:brainstormed` task has exploration notes; `stage:clarified` has a locatable spec (`report` verifies); `stage:planned` has a locatable plan (`report` verifies). Tasks whose evidence has decayed are demoted with a reason — replanning is cheaper than implementing against a ghost.
- **Staging is recognition at bounded depth.** Whoever stages a task reads its title, labels, and latest comments; a full read only when promoting a rung. When in doubt, keep the lower rung — under-staging is recoverable, over-staging misleads. Staging recognizes evidence that exists; creating it (exploration notes, settled scope, specs, plans) is developing-session work — a curator stamps and demotes but does not invent evidence to enable a climb.
- **The intake queue is triaged.** Tasks on `<CODE>:to-brainstorm` worth pursuing are brainstormed; the rest are deferred deliberately, with the deferral recorded as a task comment so absence reads as a decision, not neglect. Ambiguity goes to the decider — or is decided, recorded as a task comment, and flagged for the next decider review. Never let ambiguity stall silently.
- **No skipped rungs, no premature implementation.** Tasks advance one rung at a time and only when the next rung's artifact exists; nothing below `stage:planned` is implemented.
- **Links hold.** Every `revision_of`/`relates_to` link points at a live task and a relationship that still holds; stale links are unlinked. Oversized planned tasks are split into `--revision-of` follow-ups.
- **The vocabulary is fixed.** Five stages, queued-as-entry, six boards, two link types. Extra stages are not part of the paved road.
```

- [ ] **Step 2: Run the guide test**

Run: `go test ./internal/capability/workflowai/ -run TestSkillsFile -v`
Expected: PASS (the test checks `## Semantics`, `## Actions`, `## Converge` exist and Summary matches frontmatter description).

- [ ] **Step 3: Commit**

```bash
git add skills/capability/workflow_ai.md
git commit -m "docs(ATM-711a9d): rewrite workflow_ai guide for action-oriented reframe"
```

---

### Task 9: Verify gate and integration

**Files:**
- No new files; this task runs the full verify gate.

- [ ] **Step 1: Build the whole binary**

Run: `make build`
Expected: compiles clean.

- [ ] **Step 2: Run the full test suite**

Run: `make test`
Expected: PASS. If any test outside `internal/capability/workflowai/` references the old board names or `stage:implementable`, fix it (search: `grep -rn "implementable\|new-tasks\|brainstormed-tasks\|planned-tasks" --include="*.go" | grep -v _test`).

- [ ] **Step 3: Run the CLI smoke**

Run: `./bin/atm capability workflow_ai guide | head -20`
Expected: the new guide text (queue → brainstorm → clarify → plan → done).

Run: `./bin/atm capability workflow_ai --help`
Expected: `queue`, `brainstorm`, `clarify`, `plan`, `done`, `demote` subcommands; no `ready`.

- [ ] **Step 4: Commit any stray fixes**

```bash
git add -A
git commit -m "fix(ATM-711a9d): integration fixes from verify gate"
```

---

### Task 10: Backfill ATM's existing tasks (manager pass)

**Files:**
- No code; this is a data migration via the CLI, run by the developer acting as the ledger owner.

- [ ] **Step 1: Reseed the vocabulary (force-updates boards, removes old ones)**

Run: `./bin/atm capability workflow_ai seed --project ATM --actor "developer@ollama:glm-5.2:cloud"`
Expected: `ensured workflow_ai boards for ATM`. The old `new-tasks`/`brainstormed-tasks`/`planned-tasks` labels are removed; `to-brainstorm`/`to-clarify`/`to-plan`/`to-implement` are created.

- [ ] **Step 2: Restamp implementable tasks to planned**

Run: `./bin/atm task list --project ATM --label "ATM:stage:implementable" --output json | python3 -c "import json,sys; print([t['id'] for t in json.load(sys.stdin)['tasks']])"`
Expected: `["ATM-0074", "ATM-0123"]` (or empty if already migrated).

For each: `./bin/atm task label remove --task <ID> --label "ATM:stage:implementable" --actor "developer@ollama:glm-5.2:cloud" && ./bin/atm task label add --task <ID> --label "ATM:stage:planned" --actor "developer@ollama:glm-5.2:cloud"`

- [ ] **Step 3: Confirm the boards read correctly**

Run: `./bin/atm task list --project ATM --label "ATM:to-implement"`
Expected: the two restamped tasks (ATM-0074, ATM-0123).

Run: `./bin/atm task list --project ATM --label "ATM:to-brainstorm"`
Expected: empty (no tasks have been queued yet — the brainstormed backlog stays on `to-clarify` until the manager promotes them).

Run: `./bin/atm task list --project ATM --label "ATM:to-clarify"`
Expected: the 32 tasks currently at `stage:brainstormed`.

- [ ] **Step 4: Record the migration as a task comment**

Run:
```bash
./bin/atm task comment add --task ATM-711a9d --body "Backfill pass: reseeded workflow_ai vocabulary (old boards removed, to-* boards created). Restamped stage:implementable -> stage:planned for ATM-0074, ATM-0123. Context pointers (24 tasks with context:* labels, no stage) left untouched — they belong to contextmap. The 32 brainstormed tasks remain on to-clarify pending manager promotion." --label "ATM:comment:progress" --actor "developer@ollama:glm-5.2:cloud"
```

- [ ] **Step 5: No commit (data migration, not code)**

---

## Self-Review

**Spec coverage:**
- §1 ladder (5 rungs, artifact-gated) → Tasks 2 (constants), 4 (recorder transitions)
- §2 boards (action-oriented, one stage per board) → Task 3 (vocabulary)
- §3 payload (spec locator) → Task 1 (SpecRecord)
- §4 verbs (queue, clarify --ref, drop ready, demote to queued, report verifies both) → Tasks 4, 5, 7
- §5 context-pointer exclusion → Task 8 (guide text records the rule; no code change needed — structural)
- §6 migration & backfill → Task 3 (EnsureVocabulary removes old boards), Task 10 (data backfill)
- §7 code change surface → all tasks
- §8 what does not change → no tasks (correct — nothing to do)

**Placeholder scan:** no TBD/TODO in the plan; the spec header's TBD was already replaced with ATM-711a9d.

**Type consistency:** `SpecRecord` fields (Kind, Ref, RecordedAt, Actor) match `PlanRecord` exactly. `Clarify(taskID, kind, ref string)` signature consistent across recorder, command, and tests. `Queue(taskID)` returns `(string, error)` like every other stage verb.