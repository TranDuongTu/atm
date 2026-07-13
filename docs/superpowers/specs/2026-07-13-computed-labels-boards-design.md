# Computed Labels — Boards as Saved Queries Over the Label Substrate

**Status:** Proposed (design)
**Date:** 2026-07-13
**Supersedes the interaction model of:** ATM-0111 (Labels pane redesign) — the namespace drill-down survives as a component; the pane it lives in changes.

## Problem

Task filtering today is AND-only over exact labels. `QueryFilters.Labels` intersects exact label names, and `:*` wildcards declare facets for `GroupTasks` but explicitly do **not** restrict (`internal/store/query.go:8-12`, `:130-144`). There is no OR, no NOT, and no way to name a filter.

Real work wants named, reusable groupings that cut across namespaces: "next sprint", "everything still needed to ship v1.0.0", "high-priority open work", "untriaged". Each is a predicate over labels. Today each must be re-typed as a flag soup at every call site, and cannot be shared, described, or discovered.

Separately, a **namespace has no meaning record**. `ATM:status:open` is a `Label` with a description. `status` — the namespace — is nowhere: not an entity, no description, derived by string-splitting at read time, and explicitly disclaimed by the store (`atm conventions`: "nothing in the store validates or special-cases the documented namespaces"). You can say what `status:open` means. You cannot say what `status` means.

## Solution

A **board is a computed label**: a label whose membership is derived from an expression over other labels, rather than asserted by tasks.

This is a database **view** — a named relation defined by a query over base relations. Prior art: TaskWarrior's *virtual tags* (`+READY`, `+OVERDUE`), mail clients' saved-search folders, Jira filters, Linear views.

`Label` gains one optional field:

```go
type Label struct {
    Name        string `json:"name"`
    Description string `json:"description,omitempty"`
    Expr        string `json:"expr,omitempty"`  // non-empty ⇒ computed
    LogSeq      int    `json:"log_seq,omitempty"`
}
```

### Three kinds of label

Discriminated mechanically, with no namespace special-cased (which `atm conventions` forbids):

| Example | Name ends `:*`? | `Expr` set? | Kind | Membership |
|---|---|---|---|---|
| `ATM:status:open` | no | no | **stored** | tasks assert it |
| `ATM:status:*` | yes | — | **namespace** | any label with that prefix; expression implicit in the name; emergent; expandable into members |
| `ATM:next-sprint` | no | yes | **board** | evaluate the boolean; authored; no members |

Boards are **top-level, two-segment names** (`ATM:next-sprint`), not namespaced under `board:`. A `board:` prefix would make `board:*` a namespace whose members are the boards — rows already shown at top level — creating visible redundancy or requiring a view-level special case. Dropping the prefix removes the problem rather than papering over it.

### Why this shape

The decisive property: **a board is a label, so the entire existing query surface gets boards for free.** `atm task list --label ATM:next-sprint` needs no `--board` flag and no parallel query path — the engine expands computed labels before matching. `--label ATM:*` faceting still works. The TUI queries typed, indexed label rows. The conventions doc's central claim, *"labels are the query surface"*, gets stronger rather than being forked.

Alternatives rejected:

- **A `Board` entity beside `Label`.** Structurally it is `Label` plus one string, with identical CRDT semantics. It would fork the query surface (`--board` alongside `--label`), duplicate the pane, and add a concept the interop spec must carry forever.
- **A task carrying `context:board`, expression in a fenced block in the description.** Adds zero event actions, but moves the cost to the read path: the pane's primary query becomes a regex over free-form prose; the expression is a convention rather than a schema, so any agent rewriting the description silently breaks it; and board-tasks match their own boards (a board defined as `NOT status:done` includes the not-done board tasks themselves), requiring a permanent exclusion special case in the query engine.

## Expression language

Atoms are label names with the project prefix implied. Operators: `AND`, `OR`, `NOT`, parentheses.

```
next-sprint      = status:open AND sprint:next AND NOT type:chore
release-v1.0.0   = release:v1.0.0 AND NOT status:done
untriaged        = NOT status:*
release-blockers = release-v1.0.0 AND priority:high
```

Three atom forms:

- **stored label** — `status:open` — task carries this exact label.
- **namespace predicate** — `status:*` — task carries *any* label with prefix `status:`. This makes `NOT status:*` ("untriaged") and `NOT priority:*` ("unprioritized") expressible for the first time.
- **board reference** — `release-v1.0.0` — recursively evaluate that board's expression. Boards compose.

**Atom resolution is by lookup, not by syntax.** The parser reads a bare name, finds its `Label` record, expands it if it has an `Expr`, and treats it as stored if it does not. This is what lets bare-tag labels (`stale`, `fixit`) and boards share one name space without ambiguity.

### The wildcard's two readings

`status:*` means **"has any status label"** inside an expression — it restricts. It means **"group by status"** in `--facets` — it does not restrict (`query.go:10-11`). The current code overloads this token silently; the two readings are context-determined and both are needed. Implementations must not unify them.

## Invariants

**I1 — Computed labels are never stored on tasks.** `task label add` rejects a label with an `Expr` (and rejects any `:*` name). Otherwise the label means two things at once, violating conventions rule 5 ("one label, one meaning"). On merge, a concurrent "make label computed" and "assign label to task" resolve as: **computed-ness wins**; the stored assignment becomes inert and is ignored by the resolver.

**I2 — Cycles are rejected at write AND depth-guarded at read.** Write-time rejection alone is insufficient. Under the event-source model (`docs/eventsource/00-architecture.md`, D4), replica A edits board `a` to reference `b` while replica B edits `b` to reference `a`. Both writes are individually valid. The order-independent fold merges them into a **cycle neither replica wrote**. Therefore: reject at write time (fast feedback), *and* carry a visited-set guard in the resolver, *and* render a cyclic board in the pane as broken (`⚠ cyclic`) rather than hanging or returning empty.

**I3 — A board name may not collide with a namespace name.** `ATM:status` (board) and `ATM:status:*` (namespace) are distinct strings the store would happily hold, but both display as `status`. Reject at write.

**I5 — A board cannot be a facet.** `GroupTasks` partitions tasks by the members of a wildcard (`query.go:57`). A board has no members, so faceting by one is meaningless. A board is valid as a *restriction* (`--label ATM:next-sprint`) and invalid as a *facet*; reject the latter rather than returning an empty grouping.

**I4 — Namespaces stay emergent.** Writing `ATM:sprint:next` brings the `sprint` namespace into being implicitly, exactly as today. A namespace descriptor is *optional metadata that may be attached*, never a gate that must be passed first — otherwise agents lose the self-organizing freedom conventions rule 3 grants them.

## Storage and event source

One new log action: `label_set_expr`.

Its CRDT rule is the **last-writer-wins register keyed on the HLC stamp that D4 already specifies for label descriptions** (`docs/eventsource/00-architecture.md:58`). No new resolution semantics. The event vocabulary and the future interop spec grow by one clause in an existing rule, not by one concept. Sync, merge, and tombstoning are inherited from `Label` unchanged.

`cache.db`'s labels table gains an `expr` column. The log remains the sole source of truth; the cache stays derived and rebuildable.

## Query engine

`taskMatchesLabels` (`query.go:130`) generalizes from AND-over-exacts to `eval(task, ast)`:

- **stored atom** — task has the exact label.
- **namespace atom** `ns:*` — task has any label with prefix `ns:`.
- **board atom** — recursively evaluate its expression, carrying a visited set (I2).

Memoize board resolution per query: a board referenced by three other boards is evaluated once.

`--label a --label b` remains sugar for `a AND b`. Nothing regresses; existing callers are unaffected.

New: `atm task list --expr '<expression>'` for ad-hoc expressions that are not worth naming.

## Seed

Namespace descriptors join `internal/seed/seed.go`, which is already "the single source of truth for the seeded label names and their descriptions." Because `atm label seed` re-applies idempotently and adds new defaults introduced in a release (`atm conventions`), existing projects are back-filled with **no migration step**:

```go
{"status:*",   "lifecycle state of a task; exactly one status label should be present"},
{"priority:*", "optional urgency ranking; absent means default priority"},
{"context:*",  "index tasks whose description is the payload: agent directions, repos, docs, questions"},
{"comment:*",  "the kinds of narrative an agent writes on a task"},
```

`type:*` is deliberately **not** seeded — `type` is invented on demand (`seed.go:33-35`), so it correctly renders as undescribed until a human or agent explains it. The warning fires exactly where it should.

## TUI

The Labels pane becomes the **Boards pane**: a flat list of computed labels — boards and namespaces intermixed and indistinguishable by design, each rendering as `{name, description, count}`.

```
BOARDS                                    [n]ew  [e]dit  [d]elete

  next-sprint         12   Work committed for the sprint starting 2026-07-20
  release-v1.0.0       5   Everything still needed to ship v1.0.0
  untriaged            3   Tasks with no status label yet
  status              48 ▸ Lifecycle state; exactly one should be present
  priority            31 ▸ Optional urgency ranking; absent means unranked
  context              6 ▸ Index tasks: agent directions, repos, docs
  sprint         ⚠     7 ▸ (no description)
```

- **Boards** select straight to their matching tasks.
- **Namespaces** expand (`▸`) into their member labels, preserving ATM-0111's three-level drill-down as a component.
- **Undescribed rows carry `⚠`.** This is how the human curation surface survives the revamp with no second pane and no toggle: when an agent invents `ATM:sprint:next`, a `sprint` row appears undescribed, which is precisely the "agent introduced this but didn't explain why" signal conventions rule 6 asks a human to reconcile.

The label substrate is thereby de-emphasized for users without being hidden from them, and agents remain the primary authors of label logic.

### Authoring

`[n]ew` / `[e]dit` open an editor with name, description, and expression fields. The expression is **parsed as the user types**, showing a live match count and refusing to save when invalid, cyclic, or name-colliding. The live count is what makes an expression language usable by someone who does not know the syntax.

`atm label add --name <name> --expr '<expression>' --description '<meaning>'` is the agent-facing path, validated identically. Users may also simply ask `atm-manager` to author a board for them.

## Testing

- **Parser**: precedence, associativity, parens, `NOT` binding, malformed input, empty expression.
- **Resolver**: each atom form; composition depth > 1; memoization; the visited-set guard on a hand-constructed cyclic label set (simulating a merge-induced cycle, which cannot be produced through the write path).
- **Invariants**: I1 rejection at write and inert-on-merge; I2 rejection at write; I3 collision rejection.
- **Query**: `--label` sugar still equals AND; `--expr`; a board used as a `--label` value; faceting a board's results.
- **Seed**: idempotent re-application; back-fill of an existing project adds namespace descriptors without touching existing descriptions.
- **Store round-trip**: `expr` survives log → replay → cache rebuild.

## Out of scope

- Columns, ordering, or per-board task state. A board is a predicate; the Tasks pane is the renderer. Per-board position would put task state outside the label substrate — a departure from the design this whole feature is built to honor.
- Non-label predicates (title text match, date ranges, actor). Every motivating use case is a pure label predicate. Adding fields would make this a JQL clone.
- A display-name/title field distinct from the label name. The label token *is* the name; make it kebab-case and memorable.
