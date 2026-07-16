# Design: `internal/core` and the unified faceting/wildcard algebra

Ledger task: `ATM-cca7b0` — *Refactor step 3: create internal/core; parity-test then unify faceting/wildcard algebra*
Umbrella: ATM-9eb7dc · Specification: [docs/architecture/logical-components.md](../../architecture/logical-components.md)
Date: 2026-07-16 · Status: approved, pending implementation plan

## Problem

`docs/architecture/logical-components.md` names `internal/core` as the domain leaf that owns "the label algebra (wildcards, faceting, grouping, board expressions)", and forbids `internal/tui` from reimplementing it. Today the package does not exist and the algebra lives in two places: `internal/store/query.go` and `internal/tui/tasks_grouping.go`.

Step 3 creates `internal/core`, moves the algebra there, and deletes both copies.

### What the investigation found

The task brief assumed two copies of one algorithm. That is not what is in the tree.

**The two implementations are composed, not duplicated.** `internal/tui/tasks.go:142` sources the *top* level of its group tree from `store.GroupTasks`, and hands only `wildcards[1:]` to the TUI's own `buildNestedGroups`. The two disagree about what multi-wildcard faceting means:

- `store.GroupTasks` is **flat**: it buckets a task under every concrete label it carries that matches *any* wildcard in the filter.
- `buildNestedGroups` is **nested**: at each level it buckets by *one* wildcard, in filter order, and recurses.

So a filter of `ATM:status:* ATM:type:*` renders a top level faceted by both namespaces side by side, with `type:*` subgroups hanging under every top-level group — including under the `type:` groups themselves. The TUI's own doc comment ("the TUI-side nesting pass that turns the store's flat per-concrete-label groups into the nested facet tree") describes a tree it does not actually produce.

The genuinely duplicated code is only the small string helpers — `isWildcard`/`isWildcardTUI`, `wildcardTokens`, `labelMatchesWildcard`/`labelMatchesWildcardTUI` — which are character-identical. The `TUI`-suffixed names exist only because both live in scope-separate packages; that naming is the seam this step removes.

**A latent bug in `store/query.go:174-185`.** The bucketing loop nests as `for w := range wildcards { for l := range t.Labels }`, so when two wildcards both match the same label the task is appended to that bucket twice:

```go
wildcards := []string{"ATM:*", "ATM:status:*"}   // task carries ATM:status:open
// w = "ATM:*"        -> prefix "ATM:"        matches -> buckets["ATM:status:open"] = [t]
// w = "ATM:status:*" -> prefix "ATM:status:" matches -> buckets["ATM:status:open"] = [t, t]
```

`ATM:*` is a documented facet — the `QueryFilters` doc comment names it explicitly — so this is reachable from documented input, and surfaces in both `atm task list --facets` and the TUI.

## Design

### `internal/core`, a pure leaf

Three source files. No internal imports; standard library only. The package never names `Task`.

**`internal/core/label.go`** — label-string algebra:

```go
func IsWildcard(label string) bool                       // suffix ":*"
func LabelMatchesWildcard(label, wildcard string) bool   // prefix match on wildcard minus "*"
func WildcardTokens(labels []string) []string
func RestrictingTokens(labels []string) []string
func FacetToken(scope, ns string) string                 // ("ATM","status") -> "ATM:status:*"
func HasBareTag(scope string, labels []string) bool      // takes labels, not a Task
```

**`internal/core/filter.go`** — the filter string as a label query:

```go
func ParseFilter(s string) []string
func FilterHasToken(filter, token string) bool
func FilterAddToken(filter, token string) string
func FilterRemoveToken(filter, token string) string
```

**`internal/core/facet.go`** — grouping, generic over a labels accessor:

```go
type Group[T any] struct {
    Label string
    Items []T
}

// GroupByWildcard buckets items under every concrete label they carry that
// matches any wildcard. Flat, one level. Items carrying no matching label are
// returned in others. A given item appears at most once per bucket.
func GroupByWildcard[T any](items []T, labelsOf func(T) []string, wildcards []string) (groups []Group[T], others []T)

type Node[T any] struct {
    Label    string   // "" is the (no matching labels) bucket
    Items    []T      // populated at leaf level only
    Children []Node[T]
}

// GroupNested buckets items by wildcards[0], recursing into each bucket with
// wildcards[1:]. Items are attached at the deepest level only.
func GroupNested[T any](items []T, labelsOf func(T) []string, wildcards []string) []Node[T]
```

The generic accessor is the load-bearing decision. `internal/core` must be a pure leaf, but the algebra operates on tasks, and moving `Task` into `core` is explicitly step 4's scope (ATM-b9d83a). Parameterising over `labelsOf func(T) []string` lets `core` group things it knows nothing about:

```go
// store/query.go
core.GroupByWildcard(inScope, func(t *Task) []string { return t.Labels }, wildcards)

// tui/tasks.go
core.GroupNested(tasks, func(t *store.Task) []string { return t.Labels }, wildcards)
```

When step 4 moves `Task` into `core`, it instantiates the same functions with `core.Task` and nothing else changes. Rejected alternatives: a `core.Labeled` interface (forces a method onto `store.Task` and needs casts to get the concrete type back), and pulling the type move forward (merges two ledger tasks and turns a medium-risk step into one touching every package).

### The move boundary

`tasks_grouping.go` mixes label algebra with viewport bookkeeping. Only the former crosses into `core`.

| Moves to `internal/core` | Stays in `internal/tui` |
|---|---|
| `IsWildcard`, `LabelMatchesWildcard`, `WildcardTokens`, `RestrictingTokens` | `taskGroup{label, rows, subgroups, collapsed}` |
| `FacetToken`, `HasBareTag` | `groupLineCount`, `rowInGroup`, `toggleInGroup`, `groupLeafCount` |
| `ParseFilter`, `FilterHasToken`, `FilterAddToken`, `FilterRemoveToken` | `(t *tasksModel) hasWildcard`, `grouped` — focus-mode policy |
| `GroupByWildcard`, `GroupNested` | the adapter from `[]core.Node[*store.Task]` to `[]taskGroup` |

The four walkers reason about flattened cursor indices and collapse state — Bubble Tea rendering concerns. The import rules forbid dragging them into the domain leaf, so "delete the file outright" is not the goal; "delete the label algebra from the file" is.

### Consumers after the move

- **`store/query.go`** deletes its four helpers. `GroupTasksErr` keeps its `*Store` receiver, its board-is-not-a-facet guard, and its `([]LabelGroup, []*Task, error)` signature; only the bucketing body becomes a `core.GroupByWildcard` call.
- **`internal/cli`** is untouched. It calls `GroupTasksErr` and emits `groups`/`others`; that shape does not change.
- **`tui/tasks_grouping.go`** loses `isWildcardTUI`, `labelMatchesWildcardTUI`, `wildcardTokens`, `buildNestedGroups`, `facetToken`, `filterHasToken/AddToken/RemoveToken`, `taskHasBareTag`, and `parseFilter`'s body.
- **`store.Task` does not move**, and no package outside `store` gains a dependency on a core type.

## Behavior policy

Current behavior is pinned first, bug-for-bug. The fixes then land inside step 3 as a separate, final commit with their test updates — so the move is provably neutral and the behavior change is isolated to a diff of its own.

Two fixes, both in commit 3:

1. **Dedup.** `GroupByWildcard` appends a given item at most once per bucket. Affects `--facets` and the TUI; strictly a bugfix, no intended output shape changes.
2. **The TUI's tree.** `tui` stops sourcing level 1 from store's flat grouping and calls `core.GroupNested` from `wildcards[0]`, producing the tree its doc comment and mockup Screen 7 always described.

`cli --facets` keeps calling the flat grouping, so its published `groups`/`others` JSON contract is untouched. The one user-visible change in the whole step is the TUI's group tree under a filter with two or more wildcards.

## Testing

### Characterization, not parity

The brief says "parity tests exercising BOTH existing implementations". That phrasing assumes the two agree; they do not, so an assertion that both produce the same output would fail by construction. These are characterization tests instead: one shared corpus, exercised through each path, with today's output — divergence, duplicate append, and all — captured in golden files.

- `internal/store/query_facet_test.go` drives `GroupTasksErr` over the corpus via the existing `newTestStore(t)` fixture, dumping `groups`/`others` to `testdata/facet_flat.golden`.
- `internal/tui/tasks_grouping_test.go` drives the **composed** render path (`store.GroupTasks` → `buildNestedGroups`, as `tasks.go:142` wires it), dumping the tree to `testdata/facet_tree.golden`. The divergence lives in the composition, so testing `buildNestedGroups` in isolation would miss it.

Golden files rather than inline assertions, because commit 3's diff then *shows the behavior change directly*. `internal/cli/testdata` is the existing precedent for the pattern.

The corpus is a ~15-line Go literal, written once per package with a comment cross-referencing its twin. Sharing it across two packages needs a new test-only package, which costs more than the duplication saves.

### Corpus coverage

Zero, one, two, and three wildcards; a task carrying two labels in one namespace (multi-membership); a task matching no wildcard (the `others` and `""` buckets); overlapping wildcards `ATM:*` + `ATM:status:*` (pins the duplicate append); a repeated token; bare tags; an empty result.

### Where tests live afterwards

Commit 2 adds direct table-driven unit tests for `core` — it is new API and deserves tests that do not route through an adapter. The store and TUI golden tests survive the step as adapter-level integration checks: they prove the adapters call `core` correctly, which the `core` unit tests cannot.

## Plan of commits

Work happens on a worktree branch off `main`, never on `main` directly.

Three phases. The implementation plan — `docs/superpowers/plans/2026-07-16-internal-core-faceting.md` — refines them into six commits, splitting phase 1 by package and phase 2 by algebra (label/filter, nested, flat) so each lands its own reviewable, independently testable diff. The phase contract below is what matters and is unchanged by that split.

| Phase | Commits | Golden files |
|---|---|---|
| 1 — pin | Characterization tests, one per package. No production changes. | created |
| 2 — move | Create `core`; move the algebra; point `store` and `tui` at it; delete both copies. Add `core` unit tests. | **untouched** — passing unchanged is the proof of neutrality |
| 3 — fix | The two fixes. | updated in the same diff |

Phase 2 necessarily edits `internal/tui/tasks_test.go`: four existing tests (`TestBuildNestedGroupsTwoWildcards`, `TestBuildNestedGroupsThreeWildcards`, `TestFilterTokenHelpers`, `TestTaskHasBareTag`) have subjects that move into `core`. Test churn there is expected and is not a neutrality violation — the neutrality claim rests on the goldens alone.

Commit 2 must **preserve the composition it inherits**, oddities included. `core.GroupNested` is a faithful port of `buildNestedGroups`, and the TUI keeps calling it with `wildcards[1:]` on top of store's flat level 1 — exactly as `tasks.go:142` wires it today. Neutrality means the seam moves, not the shape. Commit 3 is where the call site becomes `core.GroupNested(tasks, labelsOf, wildcards)` from `wildcards[0]` and the flat level-1 source is dropped.

## Acceptance criteria

Verified by running, not asserted:

- `make verify` green after each of the three commits.
- No wildcard *matching* remains in `internal/tui`: `grep -rn 'HasSuffix\|TrimSuffix\|HasPrefix' internal/tui/*.go` returns no line that manipulates a `:*` token. The surviving `hasWildcard`/`grouped` methods are focus-mode policy and must contain no string surgery — each delegates to `core`. A bare grep for `wildcard` is *not* the criterion; those two names are expected to stay.
- `go list -deps ./internal/core | grep atm/internal` is empty — the leaf rule holds mechanically.
- `atm task list --facets` JSON is byte-identical across commit 2.

## Risks

| Risk | Mitigation |
|---|---|
| Commit 3 changes the TUI group tree under 2+ wildcard filters | The only user-visible change in the step. Commits 1-2 stand alone; commit 3 can be dropped without touching them if it proves contentious. |
| Generic API is awkward at a call site | Both call sites are one-liners; the accessor closure is three tokens. |
| Golden churn hides a real regression in commit 2 | Commit 2 is required to leave goldens byte-identical. Any churn there is a defect, not a rebaseline. |

## Out of scope

Moving `Task` or any domain type into `core` (step 4, ATM-b9d83a). Board expression evaluation — `ParseExpr`, `AtomNode`, `newResolver` — stays in `store` for now, despite the architecture doc listing board expressions as core's eventual property; it has no second copy, so it is not this step's duplication problem. The capability registry (step 5) and the event-log carve (step 6).
