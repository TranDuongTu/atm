# ATM Tasks Management v2 — TUI Mockups Design Spec

**Status:** Approved (companion to the v2 design spec at
`docs/superpowers/specs/2026-07-02-tasks-management-v2-design.md`).
**Scope:** Terminal TUI (Bubble Tea) screen-level design — layout, keybindings,
view states, empty states, and form overlays for the three v2 tabs (Projects,
Tasks, Help). The CLI surface is covered by the parent spec; this document
covers only what the TUI renders and how a human interacts with it.

## Driver

The v2 parent spec pins the data model, the store API, the CLI command surface,
and the behavioral rules (filter-driven faceting, no intrinsic workflow
knowledge, labels as the single substrate). What the parent spec leaves to this
document is the **screen-level rendering**: how the three tabs are laid out,
which keys do what, how the filter syntax surfaces as a view mode, how forms and
overlays behave, and how empty states read. This spec makes those choices
concrete so the implementation layer has a single reference for the TUI.

## Shared chrome

Every screen sits inside the same frame:

```
┌─ ATM ───────────────────────────────────────────────────────────────────────┐
│ [ Projects ]  [  Tasks  ]  [  Help  ]                                        │  <- tab bar
├──────────────────────────────────────────────────────────────────────────────┤
│ ... screen body ...                                                          │
├──────────────────────────────────────────────────────────────────────────────┤
│ STORE: <path>  SELECTED: <CODE>  <context keymap hint>          actor: <id> │  <- status line
└──────────────────────────────────────────────────────────────────────────────┘
```

**Tab bar.** Three tabs in fixed order: **Projects → Tasks → Help**. Projects is
the default (matches the spec §7 first-time-human sequence: create the project
first). Active tab is inverse-video highlighted; inactive tabs are dimmed. No
icons (no-emoji rule). Switch via `1`/`2`/`3` or mouse click.

**Status line.** Single bottom line, always visible across all tabs:
- `STORE: <path>` — the resolved store path (`--store` / `ATM_HOME` /
  `~/.config/atm`), so a human pointing `--store` elsewhere always sees where
  they are.
- `SELECTED: <CODE>` — the project that scopes the Tasks tab (see Selection
  model). Omitted entirely when no project is selected.
- Context keymap hint — tab-specific keys (e.g. `[a]dd [/]filter [Enter]detail`
  on Tasks). Shrinks to `[?]keys` when nothing else is actionable (e.g. empty
  states, Help tab).
- `actor: <id>` — the free-form actor string from `--actor`, shown right-aligned.
  If no actor was set, reads `set --actor to mutate` and mutating keys are
  inert.

## Selection model

Selection is **explicit and persistent**, distinct from the navigational
cursor:

- **Cursor** (`j/k`, inverse-video highlight) — the row `Enter`/`[e]` opens and
  `[x]` removes. Ephemeral, navigational.
- **Selection** (`[s]`, gutter marker `▸` + status bar `SELECTED: <CODE>`) — the
  project that scopes the Tasks tab. Persists across tab switches until changed
  or cleared.
- `[s]` acts on the cursor row. Pressing `[s]` on an already-selected row is a
  no-op (no toggle — avoids accidental loss of scope).
- If the selected project is removed (`[x]`), the selection clears and the
  Tasks tab reverts to its no-project empty state.

**Tasks tab with no selection** shows an empty-state prompt (see Screen 9); no
cross-project browsing. Selection is required to view tasks.

## Screen 1 — Projects tab, empty store

The landing a first-time human sees after `atm tui` auto-inits the store
(creates `projects/` + touches `labels.json`) and launches. Projects is the
default tab.

```
┌─ ATM ───────────────────────────────────────────────────────────────────────┐
│ [ Projects ]  [  Tasks  ]  [  Help  ]                                        │
├──────────────────────────────────────────────────────────────────────────────┤
│                                                                              │
│                          no projects                                         │
│                                                                              │
│             press [a] to add a project, then seed                            │
│             index tasks (start-here, repo:, doc:)                            │
│             and label as you go                                              │
│                                                                              │
│                                                                              │
├──────────────────────────────────────────────────────────────────────────────┤
│ STORE: ~/.config/atm        [a]add [?]keys  set --actor to mutate            │
└──────────────────────────────────────────────────────────────────────────────┘
```

Empty-state copy mirrors the spec §7 first-time-human sequence in spirit — no
hidden bootstrap, just `[a]dd` then seed.

## Screen 2 — Project create (form overlay)

`[a]dd` from the Projects tab opens an inline form overlay. Per spec §1,
project create is minimal: only `--code` (`^[A-Z]{3,6}$`) and `--name`. No
labels, no type-axis, no repo-path at create time.

```
   ┌─ New project ───────────────────────────────── actor: claude ──┐
   │                                                                │
   │   code  [          ]   3-6 uppercase letters, e.g. ATM          │
   │                                                                │
   │   name  [                                                  ]   │
   │                                                                │
   │              [Enter] create   [Esc] cancel                     │
   └────────────────────────────────────────────────────────────────┘
```

- **Overlay, not full screen.** Dimmed backdrop (░); underlying tab visible but
  inert. Consistent overlay pattern for all TUI forms.
- **Live per-field validation.** Code field validates `^[A-Z]{3,6}$` on every
  keystroke; error text appears below the field in red, submit disabled while
  invalid. Catches lowercase, digits, hyphens, >6 chars — all v1-allowed shapes
  v2 rejects (spec §1).
- **Duplicate-code check on submit** (requires a store read). On conflict, the
  form stays open with a toast: `4 conflict: code ATM exists`.
- **Actor in the form header** is read-only when `--actor` was passed at launch.
  If no actor was set, the form gains a third field (actor, free-form string)
  and submit stays disabled until non-empty — mirroring the CLI's `--actor`
  requirement on mutating commands.
- **No label wizard.** Form closes on success; the new project appears selected
  in the list. Labels get added from the project detail view, not as part of
  creation.

## Screen 3 — Projects tab, populated list

After creating one or more projects. Per spec §6: columns are
`CODE  NAME  TASKS  LABELS  UPDATED` (drops v1's GUIDE column).

```
    CODE   NAME                TASKS   LABELS   UPDATED
   ─────────────────────────────────────────────────────────────
▸  ATM    Acme Task Manager       42       17    2h ago
   SCY    Scylla                   8        6    1d ago
```

- **Columns.** `TASKS` is the count of task files under
  `projects/<CODE>/tasks/`; `LABELS` is the count of distinct `<CODE>:`-prefixed
  labels in the global registry; `UPDATED` is a relative timestamp from the
  project's `updated_at`. No GUIDE column (guide subsystem deleted, spec §4).
- **Gutter marker `▸`** indicates the selected project (see Selection model),
  distinct from the cursor highlight (inverse video). The two are independent:
  you can cursor over SCY and open its detail without changing the ATM
  selection.
- **Fixed `code-asc` sort.** Projects are few and rarely reordered; no sort
  cycle on this tab (YAGNI).
- **No filter line.** Filtering is a Tasks-tab concept; projects are few and
  flat.
- **Remove guard.** `[x]` on a project with zero tasks deletes after a confirm
  overlay. On a project with tasks, refuses: `3 conflict: project has N tasks
  — remove tasks first` (spec §Store API `RemoveProject` zero-task guard).

Keys: `[a]dd`, `[s]elect`, `[Enter]`/`[e]` detail, `[x]` remove, `[?]keys`.

## Screen 4 — Project detail (single pane)

From the Projects list, `Enter`/`[e]` opens the detail. Per spec §6: single
pane (no multi-pane right column — repos/guide/advanced are gone). Renders
project facts + labels grouped by namespace, each label shown with its current
task usage count (`name (N tasks)`).

```
PROJECT
──────────────────────────────────────────────────────────────────────────────
code      ATM
name      Acme Task Manager                                  [N] set name
tasks     42
labels    17
created   2026-06-12 09:14 UTC   by claude
updated   2026-07-02 14:33 UTC   by claude

LABELS
──────────────────────────────────────────────────────────────────────────────
status:
   ATM:status:blocked                            (3 tasks)
   ATM:status:done                               (18 tasks)
   ATM:status:in-progress                        (4 tasks)
   ATM:status:open                               (14 tasks)
   ATM:status:review                             (3 tasks)

type:
   ATM:type:bug                                  (9 tasks)
   ATM:type:chore                                (3 tasks)
   ...

tags:
   ATM:hot                                       (4 tasks)
```

- **Single-pane, scrollable.** Project facts at top, then the labels section
  grouped by namespace. No split panes, no right column.
- **Namespace headings** are presentational only (the store has no namespace
  entity; `Namespaces(code)` derives them from the registry). Sorted
  alphabetically; unnamespaced tags (e.g. `ATM:hot`) grouped under a `tags:`
  heading, last.
- **Usage counts are the headline feature.** The `(N tasks)` suffix makes this
  a reconciliation surface — a human scanning the status namespace sees 18 done
  vs 14 open vs 3 review and can spot typos (e.g. `ATM:status:dnoe` with 2
  tasks would jump out). Singular/plural ("1 task" vs "N tasks") for
  readability.
- **Project history** exists on the Project entity (spec §5) but is **not
  rendered by default**. A `[H]` toggle appends a HISTORY section below labels
  (on-demand, off by default). Rationale: project history is rarely consulted
  compared to task history; always-visible would crowd the reconciliation
  surface.
- **No "matching conventions" / "TIMELINE" / "LINKS" sections** — all deleted
  per spec §4/§6.

Keys: `[N]` set name, `[L]` add label, `[l]` remove label, `[H]` toggle
history, `[x]` remove project, `[Esc]` back.

## Screen 5 — Label add/remove forms (project detail)

From the project detail, `[L]` add label and `[l]` remove label open inline
form overlays. Same overlay pattern as project create (dimmed backdrop, inline
validation, `Enter` submit / `Esc` cancel).

```
   ┌─ Add label ────────────────────────────── actor: claude ──┐
   │   name  [ ATM:priority:urgent ]                            │
   │         <namespace>:<value> or <tag>, e.g. status:open    │
   │   desc  [ use for tasks blocking the release             ]  │
   │         optional; preserved if already set                 │
   │              [Enter] upsert   [Esc] cancel                 │
   └────────────────────────────────────────────────────────────┘
```

- **`ATM:` prefix is fixed and non-editable** in both add and remove forms —
  you're inside ATM's detail, so labels are scoped to ATM. Prevents
  cross-project typos and shortens input. To manage another project's labels,
  open that project's detail.
- **Validation** on the name field: regex
  `^[A-Z]{3,6}(:[a-z0-9][a-z0-9-]*){1,2}$` after the fixed prefix is applied.
  Live, per-keystroke.
- **Description is optional.** Spec §3: description defaults to empty on
  auto-registration, preserved if already set. On upsert of an existing label,
  a non-empty description updates; an empty one preserves the existing.
- **Remove form is name-only + warning.** Shows the current description and a
  live `retained_usage` preview ("3 tasks carry this label") in amber. After
  confirm, the toast reports the actual `retained_usage` from the store (spec
  §3: "the removal response reports `retained_usage`").
- **Removed label leaves a grayed comment** in the project listing (e.g.
  `// ATM:status:blocked removed from registry; 3 tasks retain it`) rather
  than silently disappearing — the human sees the removal took effect and which
  tasks still carry the string. Tasks carrying a removed label still render the
  label string in their own detail view.

## Screen 6 — Tasks tab: flat list (no wildcard)

Switching to the Tasks tab with a project selected. Per spec §6: persistent
one-line header `PROJECT: <code>  FILTER: <tokens>  SORT: <mode>`. **No
wildcard in filter → flat paged list.** Columns: `ID  TITLE  LABELS  UPDATED`
(drops v1's STATUS and CLAIMANT).

```
┌──────────────────────────────────────────────────────────────────────────────┐
│ PROJECT: ATM    FILTER: (none)    SORT: updated-desc                        │  <- persistent header
├──────────────────────────────────────────────────────────────────────────────┤
│ ID          TITLE                           LABELS                         UPDATED
│ ────────────────────────────────────────────────────────────────────────────
│ ATM-0007    Fix label reconciliation       ATM:status:in-progress ...  14m ago
│ ATM-0012    Seed index tasks               ATM:context:start-here ...   1h ago
│ ...
├──────────────────────────────────────────────────────────────────────────────┤
│ showing 1-10 of 42                                                           │  <- paging footer
└──────────────────────────────────────────────────────────────────────────────┘
```

- **Persistent header line** directly under the tab bar, always visible — the
  view-state at a glance. `PROJECT` reflects the selection from the Projects
  tab; `FILTER` is the current filter string (empty = no restriction); `SORT`
  is the current sort mode.
- **Filter is editable inline via `/`.** The FILTER segment of the header
  becomes an input cursor; `Enter` applies, `Esc` reverts. The header *is* the
  input — no separate input bar. This matches "filter syntax doubles as
  view-mode selector" (§6).
- **Exact filter tokens restrict (AND-intersect).** `ATM:status:open` narrows
  to tasks carrying that exact label. Multiple exact tokens AND-intersect
  (e.g. `ATM:status:open ATM:type:bug` = both required).
- **LABELS column shows full label names**, space-separated, truncated with
  ellipsis if overflow. Column is wide (spec says full names); TITLE gets the
  remainder.
- **UPDATED is relative** ("2h ago") — same treatment as Projects tab.
- **No STATUS column, no CLAIMANT column** — both deleted (spec §4/§6). Status
  and ownership live in the LABELS column as `ATM:status:open`,
  `ATM:claimed-by:claude`.
- **Paging.** Long lists paginate with a `showing 1-10 of 42` footer;
  `PageDown`/`Space` and `PageUp`/`b` to navigate pages.

SORT cycles via `s`: `updated-desc` (default) → `updated-asc` → `id-asc`.

## Screen 7 — Tasks tab: grouped/faceted view (wildcard in filter)

Per spec §6: **≥1 wildcard in filter → grouped view.** Wildcard tokens
(suffix-only at a namespace boundary: `ATM:*`, `ATM:status:*`) do **not**
restrict — they declare facet dimensions. Groups are the concrete labels
matched by each wildcard that appear on ≥1 in-scope task. A task lands in
**every** group whose key it carries (multi-membership). A single shared
`(no matching labels)` bucket holds in-scope tasks matching no wildcard.

**Single wildcard** (`ATM:status:*`):
```
▾ ATM:status:open (14)
  ATM-0008  Remove claim/unclaim   ATM:status:open ATM:type:refactor    5h ago
  ATM-0011  Write label_test.go    ATM:status:open ATM:type:task        6h ago
  ... (11 more)
▾ ATM:status:in-progress (4)
  ATM-0007  Fix label reconciliation  ATM:status:in-progress ATM:type:bug  14m ago
  ... (3 more)
...
▾ (no matching labels) (2)
  ATM-0001  Bootstrap store init  ATM:type:task                      3d ago
  ATM-0013  Untagged idea         (no labels)                        4d ago
```

**Two wildcards** (`ATM:status:* ATM:type:*`) → nested facets:
```
▾ ATM:status:open (14)
    ▾ ATM:type:refactor (1)
      ATM-0008  Remove claim/unclaim   5h ago
    ▾ ATM:type:task (1)
      ATM-0011  Write label_test.go    6h ago
    ...
▾ (no matching labels) (2)
  ATM-0001  Bootstrap store init  ATM:type:task  3d ago
  ATM-0013  Untagged idea  (no labels)          4d ago
```

- **Group header format:** `▾ <label> (N)` — collapse marker, concrete label
  name, count. Collapsed state (`▸`) hides rows. `Enter` on a header toggles
  collapse; `Enter` on a row opens detail. Same key, context-sensitive.
- **Multi-membership by row repetition.** A task carrying both
  `ATM:status:open` and `ATM:status:done` renders in both groups — no de-dup.
  The duplication is the signal (spec §6: "surfacing inconsistencies for
  cleanup").
- **Multiple wildcards → nested facets.** Tasks appear once per matched label
  per wildcard. Nesting depth = number of wildcard tokens, typically 1-2. Nested
  keeps the row count manageable vs a flat cross-product.
- **`(no matching labels)` bucket is last**, always rendered even when empty
  (count 0 or more) — the spec's "view for finding and correcting under-labeled
  tasks" (§6). Unlabeled tasks and tasks missing every faceted namespace land
  here.
- **LABELS column omitted on grouped rows.** The group header *is* the label
  axis; showing it again per row is redundant. Rows show ID/TITLE/UPDATED only;
  the full label set is visible in detail. (Differs from the flat view, Screen
  6, where LABELS is the only label surface.)

## Screen 8 — Task detail view

From any task list, `Enter` opens the detail. Per spec §6: simplified to task
facts + HISTORY section (machine-generated, immutable log). Single-pane,
scrollable.

```
TASK
──────────────────────────────────────────────────────────────────────────────
id           ATM-0007
project      ATM
title        Fix label reconciliation                            [e] edit
description  When removing a label from the registry, the project detail
             view still shows a stale count. Re-derive on refresh.    [d] edit
created      2026-06-28 10:22 UTC   by claude
updated      2026-07-02 14:19 UTC   by claude

LABELS
──────────────────────────────────────────────────────────────────────────────
 ATM:status:in-progress   ATM:type:bug   ATM:priority:high
                                      [b] add label   [B] remove label

HISTORY
──────────────────────────────────────────────────────────────────────────────
 h1   2026-06-28 10:22 UTC   claude     created
      meta: {"title":"Fix label reconciliation"}
 h2   2026-06-28 10:25 UTC   claude     label-added
      meta: {"label":"ATM:type:bug"}
 h3   2026-07-02 14:19 UTC   claude     title-changed
      meta: {"from":"Fix label counts","to":"Fix label reconciliation"}
```

- **Single pane: facts → labels → history.** No split panes, no
  "MATCHING CONVENTIONS"/"TIMELINE"/"LINKS" sections (all deleted, §4/§6).
- **Labels as horizontal chips** (not grouped by namespace) — this is a single
  task; grouping 3 labels by namespace would be overkill. `[b]` add and `[B]`
  remove open the same overlay form pattern as project label forms, with the
  `ATM:` prefix fixed.
- **HISTORY is always-visible** here (unlike project detail, where it's a
  toggle). Task history is the spec's "only narrative record" (§5) — the thing
  a human consults to understand what's happened. Hiding it would bury the
  most-consulted section.
- **HISTORY format:** one block per entry — `h<N>` id, timestamp, actor,
  action, then indented `meta:` JSON line. Chronological (oldest first) so the
  story reads top-to-bottom. `actor` is the free-form string from the mutating
  command.
- **History is immutable and system-generated** (§5/§6) — no edit, no delete.
  The section is read-only. This is the spec's "immutable system invariant."
- **Description editing:** `[d]` edits description (separate key from `[e]`
  title). The spec's TUI key list (§6) names `[e]`, `[b]`, `[B]`, `[x]` but not
  a description-edit key; the CLI has `task set-description`, so the capability
  exists and `[d]` exposes it in the TUI. Simplest, most discoverable.
- **Remove confirm overlay** mirrors project remove: amber warning,
  destructive language ("History is lost"), note that registry labels are
  unaffected. `Enter` confirms, `Esc` cancels.

Keys: `[e]` title, `[d]` description, `[b]` add label, `[B]` remove label,
`[x]` remove (confirm), `[Esc]` back.

## Screen 9 — Tasks tab empty states

Three empty states the Tasks tab must handle. The header (view-state contract)
is preserved in all of them.

**State 1 — no project selected:**
```
PROJECT: (none)   FILTER: (none)   SORT: updated-desc

                     no project selected

           press [s] in the Projects tab to scope this view
```
The Tasks tab is a prompt, not a list. `PROJECT` reads `(none)`; the status bar
drops `SELECTED:` entirely; keymap hint shrinks to `[?]keys` — nothing else is
actionable until you pick a project.

**State 2 — filter matches no tasks:**
```
PROJECT: ATM   FILTER: ATM:status:done ATM:priority:urgent   SORT: updated-desc

                      no tasks match this filter

            no task carries both ATM:status:done
            and ATM:priority:urgent

            [/] to edit filter, or clear it to see all 42 tasks
```
Echoes the offending filter in plain English ("no task carries both X and Y")
so the human understands *why* it's empty without re-reading the filter line.
The hint offers `[/]` to edit or clear.

**State 3 — wildcard filter yields no concrete labels to group:**
```
PROJECT: ATM   FILTER: ATM:context:*   SORT: updated-desc

no labels match wildcard — add labels to tasks

▾ (no matching labels) (8)
  ATM-0001  Bootstrap store init  ATM:type:task                    3d ago
  ATM-0002  Labels grouped         ATM:status:done ATM:type:task    2d ago
  ... (6 more)
```
Uses the spec's verbatim phrasing (§6). The `(no matching labels)` bucket
renders with all in-scope tasks (since none match the wildcard, all land in the
bucket). This is the corrective surface: a human sees tasks lacking
`ATM:context:*` labels and can go add them.

## Screen 10 — Help tab + conventions

Per spec §6/§7: rewritten to CLI/TUI parity table + global keymap, shrunk to
the v2 surface. Also renders the `atm conventions` content (spec §7 onboarding
guide + suggested label namespaces) so the TUI is a first-class management
surface for the conventions reference.

**Section 1 — CLI/TUI parity table:**
```
CLI                                   TUI
─────────────────────────────────────────────────────────────────────────────
atm init                              (auto on first `atm tui`)
atm store path                        status bar (STORE:)
atm conventions                       this tab, bottom section

atm project create --code --name      Projects tab  [a]dd
atm project list                      Projects tab  (list)
atm project show --code               Projects tab  [Enter] detail
atm project set-name --code --name    Projects detail  [N]
atm project remove --code             Projects tab  [x]

atm label add --name --desc           Projects detail  [L]
atm label remove --name               Projects detail  [l]
atm label list [--project] [--ns]     Projects detail  (labels section)
atm label show --name                 — (CLI only)

atm task create --project --title     Tasks tab  [a]dd
atm task list [--project] [--label]   Tasks tab  (list; / for filter)
atm task list --facets                Tasks tab  (wildcard filter → grouped)
atm task show --id                    Tasks tab  [Enter] detail
atm task set-title --id --title       Task detail  [e]
atm task set-description --id --desc  Task detail  [d]
atm task label add --id --label       Task detail  [b]
atm task label remove --id --label    Task detail  [B]
atm task remove --id                  Task detail  [x]

atm tui                                (you are here)
```

**Section 2 — Global keymap:** the complete key reference across all tabs.
`?` toggles a compact overlay too (so you don't have to tab to Help mid-action),
but the Help tab has the full reference for browsing.

**Section 3 — Conventions** (verbatim from spec §7): the suggested seed-namespace
table, first-time human sequence, and agent first-contact sequence. Labeled
"(advisory — the system treats all namespaces identically)" so a reader
understands nothing here is enforced. This is the same content `atm
conventions` prints; the TUI is a second render of the same reference.

- **Parity table is the navigation bridge** — makes the spec's "both surfaces
  carry full management capability" claim (§6) literal and scannable.
- **`s` is dual-purpose** (cycle sort on Tasks, select project on Projects) —
  noted in the keymap. The action depends on the active tab; no ambiguity.
- **Read-only.** No mutating keys here. Status bar keymap hint is minimal:
  `[1/2/3]tabs [?]keys`.

## Global keymap summary

| Key | Projects tab | Tasks tab | Help tab | Detail views |
|-----|--------------|-----------|----------|--------------|
| `1`/`2`/`3` | switch tab | switch tab | switch tab | switch tab |
| `j`/`k` | move cursor | move cursor | scroll | scroll |
| `g` | top of list | top of list | top | top |
| `Enter` | open detail | open detail / toggle group | — | confirm overlay |
| `Esc` | back | back / cancel filter | — | back / cancel overlay |
| `/` | — | edit filter | — | — |
| `s` | select project | cycle sort | — | — |
| `a` | add project | add task | — | — |
| `x` | remove project (confirm) | — | — | remove task (confirm) |
| `e` | — | — | — | edit title (task) |
| `d` | — | — | — | edit description (task) |
| `b`/`B` | — | — | — | add/remove label (task) |
| `L`/`l` | add/remove label (project detail) | — | — | — |
| `N` | set name (project detail) | — | — | — |
| `H` | toggle history (project detail) | — | — | — |
| `?` | toggle keymap overlay | toggle keymap overlay | — | toggle keymap overlay |
| `PgDn`/`Space` | — | next page | next page | scroll down |
| `PgUp`/`b` | — | prev page | prev page | scroll up |

## Testing approach

TUI tests mirror v1's `app_test.go` (model updates + view snapshots) per the
parent spec §Testing. The screens above are the view-snapshot targets:

- Tab switching (1/2/3).
- Project create form (empty, invalid-code, valid, conflict).
- Projects list (empty, populated, selection marker, cursor-vs-selection
  independence).
- Project detail (facts, labels grouped by namespace with counts, `[H]`
  history toggle).
- Label add/remove forms (validation, upsert, retained-usage warning + toast).
- Tasks flat list (empty filter, inline `/` editing, exact filter applied,
  paging footer).
- Tasks grouped view (single wildcard, nested wildcards, multi-membership
  repetition, `(no matching labels)` bucket).
- Task detail (facts, label chips, history rendering, `[d]` description edit,
  `[x]` remove confirm).
- Empty states (no project, filter no match, wildcard no labels).
- Help tab (parity table, conventions, global keymap).

Verification gate: `make verify` (runs `make build && make test`) per AGENTS.md.
No new make targets.

## Out of scope

- Mouse support beyond tab switching (keyboard is the primary surface).
- Theming/color customization (the chrome uses Bubble Tea defaults).
- A compact keymap overlay's exact rendering (referenced as `?` toggle; detail
  deferred to implementation).
- CLI rendering (covered by the parent spec).