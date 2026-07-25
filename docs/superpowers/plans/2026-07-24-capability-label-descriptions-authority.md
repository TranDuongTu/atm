# Capability Label Descriptions Authority Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make capability-owned vocabulary descriptions authoritative by allowing `LabelSeed` to refresh existing descriptions while keeping seed expressions create-only.

**Architecture:** Capabilities continue to call `EnsureVocabulary`, and `EnsureVocabulary` continues to call `Store.LabelSeed`. The store/event-log `SeedLabel` operation becomes the convergence point: absent labels are created, changed non-empty descriptions append a description-only upsert, unchanged labels remain clean no-ops, and existing expressions are never rewritten by seed.

**Tech Stack:** Go 1.22+, event-sourced store under `internal/store/eventlog`, capability vocabulary under `internal/capability/*`, repository verification through `make verify`.

## Global Constraints

- Work in the isolated worktree `.worktrees/atm-0116-capability-label-descriptions`.
- Keep `atm label seed` and `internal/seed/seed.go` removed.
- `LabelSeed` may update descriptions but must not update existing expressions.
- `LabelAdd` remains the explicit force-update path for expression changes.
- Use TDD: write failing tests and verify the red state before production code changes.
- Run `make verify` before declaring the task done.

---

## File Structure

- `internal/store/label_test.go`: direct store contract tests for `LabelSeed`.
- `internal/store/eventlog/changeset_dirty_test.go`: dirty-flag contract for changed and unchanged seeded descriptions.
- `internal/store/reproject_skip_test.go`: facade reprojection behavior for unchanged versus changed seeded descriptions.
- `internal/store/eventlog/changeset.go`: production implementation of authoritative seeded descriptions.
- `internal/store/label.go`: public comments for the changed `LabelSeed` contract.
- `internal/core/repository.go`: `ChangeSet.SeedLabel` interface comment.
- `internal/capability/capability.go`: capability interface comment.
- `internal/capability/workflow/vocabulary.go`: workflow `EnsureVocabulary` comment.
- `internal/capability/workflow/vocabulary_test.go`: workflow-facing overwrite behavior.
- `internal/capability/contextmap/vocabulary.go`: contextmap `EnsureVocabulary` comment.
- `internal/capability/contextmap/vocabulary_test.go`: contextmap-facing overwrite behavior.
- `internal/capability/workflowai/vocabulary.go`: workflow_ai `EnsureVocabulary` comment.
- `docs/superpowers/specs/2026-07-18-capability-namespace-manager-actions-v2-design.md`: short clarification that supersedes the old never-overwrite language.
- `docs/superpowers/specs/2026-07-24-capability-label-descriptions-authority-design.md`: already added spec.

---

## Task 1: Store Contract

**Files:**
- Modify: `internal/store/label_test.go`
- Modify: `internal/store/eventlog/changeset_dirty_test.go`
- Modify: `internal/store/reproject_skip_test.go`
- Modify: `internal/store/eventlog/changeset.go`
- Modify: `internal/store/label.go`
- Modify: `internal/core/repository.go`

**Interfaces:**
- Consumes: `(*Store).LabelSeed(name, description, expr, actor string) error`
- Consumes: `core.ChangeSet.SeedLabel(name, description, expr, actor string) error`
- Produces: `SeedLabel` appends when an existing live label has a different non-empty description, appends nothing when the live label already has the requested description, and never writes `expr` for existing live labels.

- [ ] **Step 1: Write failing store tests**

Replace `TestLabelSeedPreservesExistingDescription` in `internal/store/label_test.go` with overwrite-focused tests and add expression protection:

```go
func TestLabelSeedOverwritesExistingDescription(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", testActor)
	_ = s.LabelAdd("ATM:type:bug", "old wording", "", testActor)
	if err := s.LabelSeed("ATM:type:bug", "seed default", "", testActor); err != nil {
		t.Fatal(err)
	}
	l, _ := s.LabelShow("ATM:type:bug")
	if l.Description != "seed default" {
		t.Fatalf("description = %q want %q", l.Description, "seed default")
	}
}

func TestLabelSeedFillsExistingEmptyDescription(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", testActor)
	_ = s.LabelAdd("ATM:type:bug", "", "", testActor)
	if err := s.LabelSeed("ATM:type:bug", "seed default", "", testActor); err != nil {
		t.Fatal(err)
	}
	l, _ := s.LabelShow("ATM:type:bug")
	if l.Description != "seed default" {
		t.Fatalf("description = %q want %q", l.Description, "seed default")
	}
}

func TestLabelSeedDoesNotOverwriteExistingExpr(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", testActor)
	if err := s.LabelAdd("ATM:work-board", "old desc", "status:open", testActor); err != nil {
		t.Fatal(err)
	}
	if err := s.LabelSeed("ATM:work-board", "seed desc", "status:done", testActor); err != nil {
		t.Fatal(err)
	}
	l, _ := s.LabelShow("ATM:work-board")
	if l.Description != "seed desc" {
		t.Fatalf("description = %q want %q", l.Description, "seed desc")
	}
	if l.Expr != "status:open" {
		t.Fatalf("expr = %q want %q", l.Expr, "status:open")
	}
}
```

Update `internal/store/eventlog/changeset_dirty_test.go` so an unchanged description is the no-op case and a changed description is dirty:

```go
// Unchanged seed stays clean; changed description is a real convergence write.
if err := e.WithProjectWrite("ATM", func(cs core.ChangeSet) error {
	if err := cs.SeedLabel("ATM:open-tasks", "open work", "", "developer@claude:test"); err != nil {
		return err
	}
	if cs.Dirty() {
		t.Error("SeedLabel of an unchanged live label is a no-op but Dirty() is true")
	}
	if err := cs.SeedLabel("ATM:open-tasks", "updated open work", "", "developer@claude:test"); err != nil {
		return err
	}
	if !cs.Dirty() {
		t.Error("SeedLabel of a changed description appended but Dirty() is false")
	}
	return nil
}); err != nil {
	t.Fatalf("seed convergence txn: %v", err)
}
```

Update `internal/store/reproject_skip_test.go` so the canary survives an unchanged seed and is restored by a changed description:

```go
if err := s.LabelSeed("ATM:open-tasks", "open work", "", testActor); err != nil {
	t.Fatalf("unchanged LabelSeed: %v", err)
}
// assert CANARY survives

if err := s.LabelSeed("ATM:open-tasks", "updated open work", "", testActor); err != nil {
	t.Fatalf("changed-description LabelSeed: %v", err)
}
// assert real title is restored
```

- [ ] **Step 2: Run tests to verify red**

Run:

```bash
go test ./internal/store ./internal/store/eventlog -run 'TestLabelSeed|TestNoopLabelSeed|TestChangeSetDirty' -v
```

Expected: failures showing existing descriptions are still preserved/no-op, so the new overwrite and dirty/reprojection expectations fail.

- [ ] **Step 3: Implement minimal store change**

In `internal/store/eventlog/changeset.go`, change `SeedLabel`:

```go
func (cs *changeSet) SeedLabel(name, description, expr, actor string) error {
	ctx, err := cs.e.beginAuthorLocked(cs.code)
	if err != nil {
		return err
	}
	if l, ok := ctx.state.Labels[name]; ok && !l.Tombstoned {
		if description == "" || l.Description == description {
			return nil
		}
		fields := core.LabelFields{Description: &description}
		return cs.UpsertLabel(name, fields, actor)
	}
	payload := map[string]any{"description": description}
	if expr != "" {
		payload["expr"] = expr
	}
	_, err = cs.e.appendLocked(cs.code, draft{
		Actor:   actor,
		Action:  actionLabelUpserted,
		Subject: eventsource.Subject{Kind: "label", Name: name},
		Payload: payload,
	})
	if err == nil {
		cs.dirty = true
	}
	return err
}
```

Update comments in `internal/store/label.go` and `internal/core/repository.go` to describe description convergence and expression create-only behavior.

- [ ] **Step 4: Run tests to verify green**

Run:

```bash
go test ./internal/store ./internal/store/eventlog -run 'TestLabelSeed|TestNoopLabelSeed|TestChangeSetDirty' -v
```

Expected: all selected tests pass.

- [ ] **Step 5: Commit Task 1**

```bash
git add internal/store/label_test.go internal/store/eventlog/changeset_dirty_test.go internal/store/reproject_skip_test.go internal/store/eventlog/changeset.go internal/store/label.go internal/core/repository.go
git commit -m "fix(ATM-0116): let LabelSeed refresh descriptions"
```

---

## Task 2: Capability Contract And Documentation

**Files:**
- Modify: `internal/capability/workflow/vocabulary_test.go`
- Modify: `internal/capability/contextmap/vocabulary_test.go`
- Modify: `internal/capability/workflow/vocabulary.go`
- Modify: `internal/capability/contextmap/vocabulary.go`
- Modify: `internal/capability/workflowai/vocabulary.go`
- Modify: `internal/capability/capability.go`
- Modify: `docs/superpowers/specs/2026-07-18-capability-namespace-manager-actions-v2-design.md`

**Interfaces:**
- Consumes: `workflow.EnsureVocabulary(s core.LabelService, code, actor string) ([]core.Label, error)`
- Consumes: `contextmap.EnsureVocabulary(s core.LabelService, code, actor string) ([]core.Label, error)`
- Produces: capability-owned descriptions converge to the vocabulary literal on re-ensure.

- [ ] **Step 1: Write failing capability tests**

In `internal/capability/workflow/vocabulary_test.go`, rename and invert the old preserve-description tests:

```go
func TestEnsureVocabularyOverwritesOpenTasksDescription(t *testing.T) {
	s := newTestStore(t)
	if err := s.LabelAdd(BoardOpenTasks("ATM"), "old wording", "status:open", "admin@cli:unset"); err != nil {
		t.Fatalf("seed old label: %v", err)
	}
	if _, err := EnsureVocabulary(s, "ATM", "admin@cli:unset"); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	l, err := s.LabelShow(BoardOpenTasks("ATM"))
	if err != nil {
		t.Fatalf("label show: %v", err)
	}
	if l.Description != "every open task: the project's active work." {
		t.Errorf("description = %q, want workflow vocabulary", l.Description)
	}
	if l.Expr != "status:open" {
		t.Errorf("expr = %q, want existing expression preserved", l.Expr)
	}
}
```

Apply the same shape to backlog and all-tasks, using their literal vocabulary descriptions and asserting expressions are preserved.

In `internal/capability/contextmap/vocabulary_test.go`, replace `TestEnsureVocabularyPreservesHumanDescription`:

```go
func TestEnsureVocabularyOverwritesExistingDescription(t *testing.T) {
	s, actor := newTestStore(t)
	name := LabelSuperseded("TST")
	if err := s.LabelAdd(name, "old wording", "", actor); err != nil {
		t.Fatalf("LabelAdd: %v", err)
	}
	if _, err := EnsureVocabulary(s, "TST", actor); err != nil {
		t.Fatalf("EnsureVocabulary: %v", err)
	}
	l, err := s.LabelShow(name)
	if err != nil {
		t.Fatalf("LabelShow: %v", err)
	}
	if l.Description != "this context pointer is obsolete; its successor is named in the description. Kept for history -- it retains its kind, narrative, and provenance stamps. Applied by `atm capability contextmap supersede`." {
		t.Errorf("description = %q, want contextmap vocabulary", l.Description)
	}
}
```

- [ ] **Step 2: Run tests to verify red if Task 1 is not applied; otherwise verify capability behavior**

Run:

```bash
go test ./internal/capability/workflow ./internal/capability/contextmap -run 'TestEnsureVocabulary.*Description' -v
```

Expected after Task 1: pass. If run before Task 1, the new tests fail because descriptions are preserved.

- [ ] **Step 3: Update comments and spec clarification**

Replace old "human curation survives" comments with the new authority rule:

```go
// EnsureVocabulary seeds this capability's full vocabulary. Seeded
// descriptions are authoritative for labels this capability owns; re-running
// ensure refreshes descriptions while LabelSeed keeps existing expressions
// create-only.
```

In `docs/superpowers/specs/2026-07-18-capability-namespace-manager-actions-v2-design.md`, add a 2026-07-24 clarification under the existing clarifications:

```markdown
6. **Capability-owned descriptions are authoritative** (2026-07-24, ATM-0116) — supersedes the earlier "human curation survives" wording. `EnsureVocabulary` still owns additive label convergence, but `LabelSeed` now refreshes non-empty descriptions for existing labels emitted by the capability. Seed expressions remain create-only; expression migrations use explicit force-update paths.
```

- [ ] **Step 4: Run capability tests**

Run:

```bash
go test ./internal/capability/workflow ./internal/capability/contextmap ./internal/capability/workflowai -run TestEnsureVocabulary -v
```

Expected: all selected tests pass.

- [ ] **Step 5: Commit Task 2**

```bash
git add internal/capability/workflow/vocabulary_test.go internal/capability/contextmap/vocabulary_test.go internal/capability/workflow/vocabulary.go internal/capability/contextmap/vocabulary.go internal/capability/workflowai/vocabulary.go internal/capability/capability.go docs/superpowers/specs/2026-07-18-capability-namespace-manager-actions-v2-design.md
git commit -m "test(ATM-0116): assert capability descriptions converge"
```

---

## Task 3: Final Verification And Ledger

**Files:**
- Modify only if verification exposes a real issue from Tasks 1-2.

**Interfaces:**
- Consumes: all commits from Tasks 1-2.
- Produces: verified branch ready for review.

- [ ] **Step 1: Run targeted regression suite**

```bash
go test ./internal/store ./internal/store/eventlog ./internal/capability/workflow ./internal/capability/contextmap ./internal/capability/workflowai -run 'TestLabelSeed|TestNoopLabelSeed|TestChangeSetDirty|TestEnsureVocabulary' -v
```

Expected: pass.

- [ ] **Step 2: Run full verification**

```bash
make verify
```

Expected: pass.

- [ ] **Step 3: Record ATM progress**

```bash
atm task comment add --task ATM-0116 --actor developer@codex:gpt-5 --label ATM:comment:progress --body 'Implemented on branch atm-0116-capability-label-descriptions: LabelSeed now refreshes capability-owned descriptions while preserving existing expressions; capability tests assert vocabulary descriptions converge; make verify passed.'
```

- [ ] **Step 4: Final status**

Report the worktree path, branch, commits, and verification results.
