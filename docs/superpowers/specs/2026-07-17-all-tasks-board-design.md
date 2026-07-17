# all-tasks board — Design Spec

**Status:** Draft 2026-07-17.
**Date:** 2026-07-17
**Task:** ATM-18111b — *all-tasks board: default-selected recency-ordered board owned by workflow capability*
**Builds on:** `2026-07-16-workflow-capability-design.md` (the `internal/workflow` capability), `2026-07-15-tui-tasks-boards-merge-design.md` (the merged board ring), `2026-07-13-computed-labels-boards-design.md` (the board expression algebra).

## Driver

After the Boards/Tasks merge (ATM-2412f2), the TUI always has a SELECTED board, defaulting to `ATM:open-tasks` (`status:open`). Every task that is not `status:open` — `status:done`, `status:todo`, `status:blocked`, `status:in-progress`, and naked unlabeled jottings — is invisible under the default view. A human consulting the TUI to "browse recent agent activity" (the v2 spec's consult mode, `2026-07-02-tasks-management-v2-design.md` line 543) sees only one slice of the project and must consciously switch boards to recover the rest.

The reframe recorded on ATM-18111b (`ATM-18111b-c4562`, 2026-07-17) collapses the three open options from the 2026-07-16 split decision into one deliverable: **a single `all-tasks` board owned by `internal/workflow`, selected by default in the TUI, ordered by recent `updated_at`.** A board EXPRESSION selects a set; it does not order it, so "recency" was never an expression-syntax problem — it is a sort problem, and the TUI's default sort already is `updated-desc`. The membership predicate — "every task, including unlabeled naked jottings" — is the one genuinely new piece, because the board-expression grammar has no tautology.

This is now viable because ATM-fe669c is fixed: `updated_at` is derived from live-comment activity in the event-source fold (Pass 4, `libs/eventsource/fold.go:382-420`, merged as `596e78b`), so it is a reliable recency field. The earlier `ATM-18111b-c2db3` progress note flagged fe669c as a blocker; that blocker is gone.

## Scope

- One new workflow-ensured board: `<CODE>:all-tasks`, expr `*`, description "every task in the project, ordered by recent activity. Default board in the TUI."
- One new atom in the board-expression language: a bare `*` that evaluates to true unconditionally (a tautology), reusable in any board expression and as a standalone CLI/TUI filter token.
- One change to the TUI default: `boardsModel.selectDefault` targets `all-tasks` instead of `open-tasks`.
- One description update to the existing `open-tasks` board (drop the "Default board in the TUI" clause; it becomes a regular selectable board in the ring).

## Non-Goals

- No new sort mode and no change to the default sort. The TUI default is already `sortUpdatedDesc` (`internal/tui/tasks.go:90`); `all-tasks` inherits it like every other board.
- No user-configurable default board. The default is a capability decision, hardwired in `selectDefault`, as `open-tasks` was before.
- No per-board sort persistence. The `s` key cycles `updated-desc → updated-asc → id-asc` globally, unchanged.
- No removal or redefinition of `open-tasks` beyond its description. Its expr (`status:open`) and name stay; it remains a normal ring member.
- No changes to `backlog`, `in-progress-tasks`, `context-current`, or any other capability's boards.
- No new CLI command. Board browsing stays in the TUI; `atm task list --label <CODE>:all-tasks` already resolves through the board's expression.

## The tautology atom `*`

The board-expression grammar (`internal/core/expr.go`) is:

```
or   := and ("OR" and)*
and  := not ("AND" not)*
not  := "NOT" not | atom
atom := NAME | "(" or ")"
```

`NAME` is any run of non-space, non-paren characters that is not `AND`/`OR`/`NOT`. The lexer (`lexExpr`) already accepts `*` as a default character and `parseAtom` already accepts it as a NAME (it is none of `(`, `)`, `AND`, `OR`, `NOT`), so `ParseExpr("*")` succeeds today and yields `ExprAtom{Name:"*"}`. The grammar needs no change.

The evaluator (`internal/store/resolve.go`, `evalAtom`) resolves an atom BY LOOKUP: a namespace name (`<CODE>:<ns>:*`) matches "task carries any label in the namespace"; a bare atom is qualified to `<CODE>:<atom>` and matched as a stored label or recursed into as a board. There is no atom that evaluates to true unconditionally. The fix is a single short-circuit at the top of `evalAtom`, **before** `qualify`:

```go
func (r *resolver) evalAtom(t *Task, atom string, visiting map[string]bool) (bool, error) {
	if atom == "*" {
		return true, nil
	}
	full := r.qualify(atom)
	// ... unchanged ...
}
```

Placing the short-circuit before `qualify` is load-bearing. Without it, `qualify("*")` would produce `<CODE>:*`, which `IsNamespaceName` (`internal/core/label.go:69`) reads as a namespace predicate — "task carries any label sharing the `<CODE>:` prefix" — and that **misses unlabeled tasks**, which is exactly the bug `all-tasks` exists to fix. The bare `*` (no project prefix) must remain unqualified so it can never be mistaken for the namespace wildcard.

### Where `*` becomes a tautology

The single edit makes `*` a tautology in three contexts, uniformly:

1. **Inside a board's Expr.** `ParseExpr("*")` → `ExprAtom{Name:"*"}` → `evalAtom` returns true. The `all-tasks` board uses this. So does any user-authored board, e.g. `* AND NOT status:done` for "everything not done."
2. **As a standalone CLI filter.** `atm task list --label '*'` → `RestrictingTokens` keeps `*` (it is not a `:*` wildcard, `internal/core/label.go:15`) → `AtomNode{Name: TrimPrefix("*", "<CODE>:") = "*"}` (`internal/store/query.go:78`) → `evalAtom` returns true.
3. **As a TUI filter token.** The same `ListTasksErr` path as (2), reached via `tasksModel.setFocus`/`refresh`.

### No collision with the namespace wildcard

The namespace wildcard is `<CODE>:*` (e.g. `ATM:status:*`, `ATM:*`) — a token *ending* in `:*` (`IsWildcard`, `internal/core/label.go:15`). It is a **facet-declaring** token (`WildcardTokens`), never a **restricting** one (`RestrictingTokens` partitions the two, `internal/core/label.go:26-46`). The bare `*` has no `:` at all, so `IsWildcard("*")` is false; it is a restricting token that reaches `evalAtom` as `Name:"*"`, where the short-circuit fires before `qualify` could turn it into `<CODE>:*`. The two readings of `*` never share a code path.

### `Atoms()` and cycle detection

`core.Atoms(n)` (`internal/core/expr.go:53`) returns the atom names in a tree; for the `all-tasks` board it returns `["*"]`. `Atoms` is used for cycle detection and display, not evaluation. `"*"` names no live label (it short-circuits before lookup), so it can form no cycle. No change to `Atoms` or cycle detection is needed.

## The board

In `internal/workflow/vocabulary.go`:

- Add `func BoardAllTasks(code string) string { return code + ":all-tasks" }`, paralleling `BoardOpenTasks`/`BoardBacklog`/`BoardInProgressTasks`.
- Add `func allTasksExpr() string { return "*" }`, paralleling `openTasksExpr`/`backlogExpr`/`inProgressTasksExpr`.
- Add an entry to the `boards` slice in `EnsureVocabulary`:
  `{BoardAllTasks(code), "every task in the project, ordered by recent activity. Default board in the TUI.", allTasksExpr()}`.
- Update the `open-tasks` board's description string in the `boards` slice from `"every open task: the project's active work. Default board in the TUI."` to `"every open task: the project's active work."` (drop the "Default board" clause; `all-tasks` now holds that role). Its name and expr are unchanged.

`EnsureVocabulary` keeps using `LabelSeed` for every board (including the new `all-tasks`). `LabelSeed` (`internal/store/label.go:117`, `labelSeedV2:180-203`) is **create-only**: if the label exists and is not tombstoned, `labelSeedV2` returns nil immediately and writes nothing (line 186-187). This is a deliberate, tested contract — `TestEnsureVocabularyDoesNotOverwriteHumanDescription` (`internal/workflow/vocabulary_test.go:54-70`) and the `EnsureVocabulary` doc comment (vocabulary.go:29-30) establish that **the capability never overwrites an existing label's description**, so a human's curation survives re-seeding.

The consequence: the updated `open-tasks` description lands **only on fresh projects** (where `open-tasks` does not yet exist, so `LabelSeed` writes the new string). **Existing projects keep their current `open-tasks` description** ("…Default board in the TUI."), which becomes slightly stale but harmless — `open-tasks` remains a normal, selectable board in the ring, and the staleness is cosmetic. Forcing the update on existing projects (via `LabelAdd` force-upsert) would break the never-overwrite contract that the tests guard, so this spec does not do that. `all-tasks` is new, so `LabelSeed` creates it on every project (new and existing) with the correct description. `backlog` and `in-progress-tasks` are byte-identical to before.

### `validateExpr` and `*`

`LabelAdd`/`LabelSeed` with a non-empty `expr` call `validateExpr` (`internal/store/label.go:65-113`), which walks `Atoms` of the parsed expr to reject cycles. For the `all-tasks` board, `Atoms` returns `["*"]`; `validateExpr` qualifies to `<CODE>:*`, looks it up in `live` (the live-label map), finds no stored label named `<CODE>:*` (it is a namespace name, not a stored label), and treats it as a leaf (`!ok → continue`, line 93-94). `*` names no board, so it can form no cycle. **No change to `validateExpr` is needed** — `*` passes cycle validation as-is.

`backlog` and `in-progress-tasks` are byte-identical to before.

## Default selection

In `internal/tui/labels.go:255-272` (`boardsModel.selectDefault`):

```go
func (b *boardsModel) selectDefault() {
	b.resetDrill()
	b.pinFocus = -1
	want := workflow.BoardAllTasks(b.m.projectScope)   // was BoardOpenTasks
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

One symbol swap. `EnsureVocabulary` is already called on project select before `selectDefault` (the workflow-capability spec wired this, `2026-07-16-workflow-capability-design.md` line 96), so `all-tasks` exists in the ring when selection runs. The fallback (first ring board when `want` is absent) is unchanged and remains a safe degenerate path.

## No sort change

The default `tasksModel.sortMode` is `sortUpdatedDesc` (`internal/tui/tasks.go:90`), set in `newTasksModel`. The `all-tasks` board inherits it like every other board; no per-board sort wiring exists or is needed. The v2 spec's "browse recent agent activity" consult mode (`2026-07-02-tasks-management-v2-design.md` line 543) is already satisfied. This spec does not touch sort.

## CLI

No new command. `atm task list --label <CODE>:all-tasks` already works (a board label resolves through its Expr in `ListTasksErr`, `internal/store/query.go:71-101`). `atm task list --label '*'` becomes a new idiom for "all tasks" that the board is the canonical instance of; it is supported by the same `evalAtom` short-circuit, with no CLI code change. Shell quoting for `*` is the caller's concern (glob-escape as needed).

## Tests

- **`internal/core/expr_test.go`** (add or extend): `ParseExpr("*")` succeeds and yields `ExprAtom{Name:"*"}`; `ParseExpr("* AND NOT status:done")` parses to the expected tree; `Atoms` on the `all-tasks` board's expr returns `["*"]`.
- **`internal/store/resolve_test.go`** (or `query_test.go`): `*` matches a task with no labels; `*` matches a task with labels; `* AND NOT status:done` excludes only done tasks; a bare `*` restricting token returns every task including unlabeled ones.
- **`internal/workflow/vocabulary_test.go`**: `EnsureVocabulary` seeds `all-tasks` with Expr `*` and the new description; the `open-tasks` description is the new text on a fresh seed (no "Default board in the TUI."); `backlog`/`in-progress-tasks` are byte-identical to before; idempotency holds across a second `EnsureVocabulary` call (re-seed does not overwrite a human-curated `all-tasks` description — extend the existing `TestEnsureVocabularyDoesNotOverwriteHumanDescription` pattern to cover `all-tasks`).
- **`internal/tui/labels_test.go`**: rename `TestSelectDefaultPicksOpenTasksBoard` → `TestSelectDefaultPicksAllTasksBoard`, asserting `selectDefault` lands on `workflow.BoardAllTasks`; add `TestOpenTasksRemainsSelectableInRing` asserting `open-tasks` is still a ring member reachable by `[`/`]`.
- **`internal/cli` golden**: `atm task list --project <CODE> --label <CODE>:all-tasks` returns every task (open, in-progress, done, blocked, unlabeled); `atm task list --project <CODE> --label '*'` returns the same set. Both golden cases include at least one unlabeled task to prove the tautology beats the namespace-wildcard reading.

## Out of scope

- A user-configurable default board.
- A TUI sort-mode indicator or per-board sort persistence.
- The `context-current` board or any other capability's boards.
- Any store API change. `EnsureVocabulary` keeps using `LabelSeed` exclusively; the `open-tasks` description update lands only on fresh seeds.

## Risks and mitigations

- **`*` misread as namespace wildcard.** Mitigated by the load-bearing short-circuit placement in `evalAtom` (before `qualify`) and the partition between restricting and facet tokens (`internal/core/label.go`). Tests assert an unlabeled task is returned.
- **`*` rejected by write-time cycle validation.** `validateExpr` already treats `*` as a leaf (it qualifies to `<CODE>:*`, which names no stored label). No change needed; tests assert `all-tasks` seeds without error.
- **`open-tasks` description stays stale on existing projects.** `LabelSeed` is create-only (a tested contract); forcing an update via `LabelAdd` would break `TestEnsureVocabularyDoesNotOverwriteHumanDescription`. The new description lands only on fresh projects. Existing projects keep the cosmetic "Default board in the TUI." clause; `open-tasks` still works. Acceptable per the capability's never-overwrite stance.
- **Default change surprises existing users.** The board ring still contains `open-tasks` one `[`/`]` away; the change is which board is selected on project open, not what boards exist. Low surprise, and the new default is strictly broader (everything the old default showed, plus the rest).