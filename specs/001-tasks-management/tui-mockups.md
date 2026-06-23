# TUI Mockups: Tasks Management System

**Feature**: 001-tasks-management | **Date**: 2026-06-23 | **Spec revision**: v1.1.0

This document specifies the Bubble Tea TUI as a **first-class management surface** that mirrors every CLI operation in `contracts/cli.md`. The TUI is a thin client over `internal/store` (the in-process API), exactly as the CLI is a thin client over the same store. FR-002 requires both surfaces; this artifact closes the gap left by the original plan, which treated the TUI as a coordinator-only afterthought.

Design goals:
1. **Parity**: every read and mutating CLI command has a TUI equivalent. If a human can do it on the command line, they can do it in the TUI, and vice versa.
2. **Coordinator-first default, author-capable everywhere**: the landing view is the coordinator dashboard (US5/FR-010/FR-018), but every view is writable for the signed-in actor — a human can create tasks, labels, and guide refs from the TUI, not just review.
3. **Determinism**: TUI reads come from the same `store` functions that back the CLI, so output is identical for a given store. The TUI never owns a separate data path.
4. **Keyboard-driven**: all primary actions have single-key bindings; forms are field-based (Bubble Tea text inputs), never modal labyrinths.
5. **No emojis** (constitution). Status/labels use text tokens (`open`, `type:bug`, `[STALE]`).

## Navigation model

A single full-screen app with a persistent **header**, a **tab bar** of top-level views, a **content pane** for the active view, and a **footer** showing the active actor, store path, and the context-sensitive keymap. Tabs switch with `1`-`5` (or `Tab`/`Shift+Tab`); `?` toggles a help overlay; `q` quits; `r` refreshes the current view (no auto-refresh in v1).

```
+--------------------------------------------------------------------------------+
| atm  ATM@~/.config/atm  actor: human:alice                              [r]efresh [q]uit|
| [1 Dashboard] [2 Projects] [3 Tasks] [4 Actors] [5 Help]                       |
+--------------------------------------------------------------------------------+
|                                                                                |
|  (content pane for the active tab)                                            |
|                                                                                |
+--------------------------------------------------------------------------------+
| actor: human:alice | store: ~/.config/atm | <keymap hints for the current view> |
+--------------------------------------------------------------------------------+
```

Tab inventory (every CLI group maps to a tab):

| Tab | CLI group mirrored | Primary screens |
|-----|--------------------|-----------------|
| 1 Dashboard | `review dashboard`, `review queue`, `review followups`, `project guide status` | Coordinator overview: review queue, open followups, guide coverage/freshness |
| 2 Projects | `project *`, `project label *`, `project guide *`, `project repo *` | Project list, project detail (labels, guide, repos) |
| 3 Tasks | `task *`, `task link *`, `task todo/followup/discussion *`, `task timeline`, `task next/claim/unclaim` | Task list, task detail (context, timeline, links, entries) |
| 4 Actors | `actor list`, `actor show` | Actor list, actor detail |
| 5 Help | (none) | Keymap, command parity table |

`atm init`/`atm store path` are setup utilities, not day-to-day management; they are exposed via a startup flow when no store is found (see Startup flow) rather than a tab.

## Startup flow

On launch, `atm tui` resolves the store with the same rule as the CLI (`--store` > `ATM_HOME` > `~/.config/atm`). If the store is missing or empty:

```
+--------------------------------------------------------------------------------+
| atm                                                                [q]uit       |
+--------------------------------------------------------------------------------+
|                                                                                |
|  No store found at ~/.config/atm                                                |
|                                                                                |
|  [I]nit here   [C]hoose directory...   [Q]uit                                   |
|                                                                                |
+--------------------------------------------------------------------------------+
| store resolution: default ~/.config/atm (ATM_HOME not set)                      |
+--------------------------------------------------------------------------------+
```

- `I` runs `store.Init(resolvedPath)` (the same `atm init` path) and proceeds to the Dashboard.
- `C` opens a path input; `Enter` inits at the given path.
- If a store exists, the actor is read from `--actor`/`ATM_ACTOR`; if unset, the TUI prompts for an actor id once and remembers it for the session (it does **not** persist the actor choice — that remains a CLI/env concern). Mutating actions are disabled (greyed out / rejected with a hint) until an actor is set.

## Global keymap (all tabs)

| Key | Action |
|-----|--------|
| `1`-`5`, `Tab`/`Shift+Tab` | switch tabs |
| `r` | refresh current view (re-read store; no auto-refresh in v1) |
| `/` | filter/search the current list (inline filter input) |
| `:` | open a command palette (type a CLI subcommand name to jump to the matching screen, e.g. `:project create`) |
| `?` | toggle help overlay (keymap + parity table for the current view) |
| `q` | quit |
| `Esc` | cancel current input / close overlay |

List widgets share keys: `j`/`k` or Up/Down move, `g`/`G` top/bottom, `Enter` open detail, `a` add, `e` edit selected, `x` delete/remove selected (with `y` confirm), `Space` toggle where applicable.

---

## Tab 1 - Dashboard (coordinator)

Mirrors: `review dashboard`, `review queue`, `review followups`, `project guide status`.

This is the default landing view (US5). It composes the same store reads as `atm review dashboard --project <CODE>`; the project is selected in the header (a project switcher, `P` from the header, defaults to the most recently updated project).

```
+--------------------------------------------------------------------------------+
| atm  project: [ATM v]  actor: human:alice                       [r]efresh [q]uit|
| [1 Dashboard] [2 Projects] [3 Tasks] [4 Actors] [5 Help]                       |
+--------------------------------------------------------------------------------+
| REVIEW QUEUE                                              3 tasks awaiting      |
|   agent:claude-1                                            |
|     ATM-0005  Impl: claim command        review  claimed 2h ago               |
|   agent:gemini-1                                                              |
|     ATM-0011  Add guide freshness        review  claimed 1h ago               |
|     ATM-0012  Fix link stale warning      review  claimed 30m ago             |
|   [a]pprove  [r]eject  [Enter] open task                                       |
|                                                                                |
| OPEN FOLLOWUPS                                              2 open              |
|   ATM-0002  f1  Decide storage format     assignee: human:alice  due 2026-06-30|
|   ATM-0007  f3  Review PR conventions     assignee: (none)        due (none)   |
|   [Enter] open task  [R] resolve selected                                      |
|                                                                                |
| GUIDE STATUS  threshold: 720h                              2 sections, 5 refs   |
|   conventions  2 refs   [STALE] ATM-0005 (updated 2026-05-01)                   |
|                       [OK]   ATM-0001 (updated 2026-06-20)                     |
|   testing      1 ref    [OK]   ATM-0006                                        |
|   work-conduct 0 refs   [EMPTY]                                                |
|   [E] edit guide (Projects tab)                                               |
+--------------------------------------------------------------------------------+
| actor: human:alice | store: ~/.config/atm | a:approve r:reject R:resolve E:edit|
+--------------------------------------------------------------------------------+
```

Actions:
- `a` on a review-queue row opens an inline **approve** form (optional comment field) and calls `store.Review.Approve(id, actor, comment)`. On success the row is removed and a toast confirms.
- `r` on a review-queue row opens a **reject** form (comment required) and calls `store.Review.Reject(id, actor, comment)`; the comment is recorded as a discussion entry by the actor (matching the CLI).
- `Enter` on any row jumps to Tab 3 (Tasks) with that task open in detail.
- `R` on a followup resolves it (`store.Followup.Resolve`).
- `E` jumps to Tab 2 > Project detail > Guide section.
- `P` (header) opens the project switcher (lists projects; `Enter` selects; the Dashboard reloads for the chosen project).

Every action records the actor from the header; no anonymous mutations (FR-012).

---

## Tab 2 - Projects

Mirrors: `project create/list/show/set-type-axis/set-name`, `project label add/remove/list`, `project repo add/remove`, `project guide show/section add-rename-remove-move/ref add-remove-move/set-freshness/status`.

### 2a - Project list

```
+--------------------------------------------------------------------------------+
| atm  project: [ATM v]  actor: human:alice                       [r]efresh [q]uit|
| [1 Dashboard] [2 Projects] [3 Tasks] [4 Actors] [5 Help]                       |
+--------------------------------------------------------------------------------+
| PROJECTS                                       filter: /___  sort: [code v]    |
|   CODE    NAME                    TASKS   LABELS   GUIDE   UPDATED             |
|   ATM     Agent Tasks Management  13      7        2/OK    2026-06-23 10:05   |
|   DEMO    Demo                    2       3        none    2026-06-22 14:11   |
|   SCYLLA  Scylla migration        47      5        1/STALE 2026-06-21 09:30   |
|                                                                                |
|   [a]dd project  [e]dit selected  [Enter] open  [x] remove (needs empty)       |
+--------------------------------------------------------------------------------+
| actor: human:alice | a:add e:edit Enter:open x:remove                          |
+--------------------------------------------------------------------------------+
```

Actions:
- `a` opens the **new project** form (mirrors `project create`):
  ```
  +--- New project --------------------------------------------------------+
  | code:    [ATM_______]   ^[A-Z][A-Z0-9-]{1,15}$; unique               |
  | name:    [Agent Tasks Management______________________________________]|
  | type-axis (optional): [type___]                                       |
  | repo paths (comma-separated, optional): [/Users/me/projects/scyllas_]|
  | labels (comma-separated, optional): [type:impl, area:cli, kind:conv_] |
  |                                                                       |
  | [Enter] create    [Esc] cancel                                        |
  +-----------------------------------------------------------------------+
  ```
  Calls `store.Project.Create(code, name, typeAxis, labels, repoPaths, actor)`. Duplicate code errors with `4 conflict` (shown inline).
- `e` / `Enter` opens **project detail** (2b).
- `x` removes a project only if it has zero tasks (else shows the blocking count); calls `store.Project.Remove(code, actor)`.

### 2b - Project detail

A three-pane screen: **left** = project facts + labels + repo paths; **center** = the guide editor; **right** = label/ref action targets. Tab/`Shift+Tab` cycles panes.

```
+--------------------------------------------------------------------------------+
| atm  project: ATM  actor: human:alice                            [r]efresh [q]uit|
| [1 Dashboard] [2 Projects] [3 Tasks] [4 Actors] [5 Help]                       |
+--------------------------------------------------------------------------------+
| FACTS & LABELS        | GUIDE                       | ACTIONS                    |
| code:    ATM          | sections (ordered):         | [L] add label              |
| name:    Agent Tasks  |  > conventions   2 refs     | [l] remove label           |
| type-axis: type       |    testing       1 ref      | [T] set type-axis          |
| next_task_n: 14       |    work-conduct  0 refs [EMPTY]| [N] set name              |
| created: 2026-06-23   |  freshness threshold: 720h  | [R] add repo path          |
| updated: 2026-06-23   |  guide updated: 2026-06-23  | [r] remove repo path       |
| repo paths:           |          by human:alice     | [S] add guide section       |
|   /Users/me/proj/atm  |                             | [s] rename guide section   |
|   /Users/me/proj/x    | refs in selected section:  | [X] remove guide section   |
|                       |  [task] ATM-0001  [OK]      | [M] move section            |
| LABELS (allowed)      |  [file] /abs/CONV  [MISS]   | [g] add guide ref           |
|   type:epic          |                             | [m] move guide ref          |
|   type:user-story    |                             | [d] remove guide ref        |
|   type:impl          |                             | [F] set freshness threshold |
|   type:bug           |                             | [D] guide status (dashboard)|
|   area:cli  (soft-removed: 1 task retains) |        | [Enter] open ref task       |
|   area:tui           |                             |                             |
|   kind:convention    |                             |                             |
+--------------------------------------------------------------------------------+
| L/l:labels T:type-axis R/r:repos S/s/X/M:guide-section g/m/d:guide-ref F:freshness|
+--------------------------------------------------------------------------------+
```

Actions (each calls the exact `store` function behind the matching CLI command):
- `L` add label (`store.Project.LabelAdd`) — inline form: `name`, optional `description`. Rejects names already in the set or invalid format.
- `l` remove label (`store.Project.LabelRemove`) — confirms; reports `retained_usage` count in a toast if existing tasks keep it (soft removal, matches CLI JSON `retained_usage`).
- `T` set type-axis (`store.Project.SetTypeAxis`) — form: `namespace`; validates the namespace has >=1 label.
- `N` set name (`store.Project.SetName`).
- `R`/`r` add/remove repo path (`store.Project.RepoAdd`/`RepoRemove`).
- `S` add guide section (`store.Guide.SectionAdd`) — form: `name`; rejects duplicate section names.
- `s` rename section (`store.Guide.SectionRename`) — `name` -> `new-name`.
- `X` remove section (`store.Guide.SectionRemove`) — confirms; drops the section and its refs (a `guide-updated` history entry is appended to the project record).
- `M` move section (`store.Guide.SectionMove`) — pick `before <other>` or move to end.
- `g` add guide ref (`store.Guide.RefAdd`) — form: `section` (pick), `kind` (`task`|`file`), `target`. `kind:task` validates the id exists in the project; `kind:file` accepts an absolute path (no existence check at add time).
- `m` move guide ref (`store.Guide.RefMove`) — reorder within the section.
- `d` remove guide ref (`store.Guide.RefRemove`) — by `(kind, target)`.
- `F` set freshness threshold (`store.Guide.SetFreshness`) — form: duration (`720h`) or `unset`; validates a positive Go `time.Duration`.
- `D` jumps to Tab 1 (Dashboard) scoped to this project.
- `Enter` on a `kind:task` ref jumps to Tab 3 with that task open.

All guide edits set `guide.updated_at`/`updated_by` and append a `guide-updated` project-history entry, exactly as the CLI does.

---

## Tab 3 - Tasks

Mirrors: `task create/show/show --with-context/list/set-status/set-title/set-description/label add/remove`, `task link add/remove/list`, `task todo add/toggle`, `task followup add/resolve`, `task discussion add`, `task timeline`, `task next/claim/unclaim`.

### 3a - Task list

```
+--------------------------------------------------------------------------------+
| atm  project: [ATM v]  actor: human:alice                       [r]efresh [q]uit|
| [1 Dashboard] [2 Projects] [3 Tasks] [4 Actors] [5 Help]                       |
+--------------------------------------------------------------------------------+
| TASKS  filter: /___   [project: ATM v] [status: * v] [label: type:impl v]      |
|   ID        TITLE                      STATUS    CLAIMANT        LABELS          |
|   ATM-0001  PR conventions for bugs    open      (none)          convention,bug |
|   ATM-0002  Fix claim race             in-prog   agent:claude-1  bug,cli        |
|   ATM-0003  Blocked subtask            blocked   (none)          impl           |
|   ATM-0004  Epic: agent workflow        open      (none)          epic           |
|   ATM-0005  Impl: claim command        done      agent:claude-1  impl          |
|                                                                                |
|   [a]dd  [Enter] open  [n]ext  [c]laim selected  [u]nclaim  [f]ilter             |
+--------------------------------------------------------------------------------+
| a:add Enter:open n:next c:claim u:unclaim f:filter                             |
+--------------------------------------------------------------------------------+
```

The filter bar is the TUI equivalent of `task list` flags: project, status, label (multi-select), assignee, claimant — all wired to `store.Query` with the same AND-intersection semantics and stable sort as the CLI (so the on-screen order matches `--output json` byte-for-byte given the same filters).

Actions:
- `a` opens the **new task** form (mirrors `task create`):
  ```
  +--- New task -----------------------------------------------------------+
  | project: [ATM v]                                                       |
  | title:    [___________________________________________________________]|
  | description (markdown, optional):                                      |
  |   [_________________________________________________________________] |
  |   [_________________________________________________________________] |
  | labels (comma-separated, optional): [type:impl, area:cli_____________] |
  |                                                                        |
  | [Enter] create    [Esc] cancel                                         |
  +------------------------------------------------------------------------+
  ```
  Calls `store.Task.Create(project, title, desc, labels, actor)`; assigns `<CODE>-<N>` and appends a `created` history entry.
- `n` runs `store.Claim.Next(project, claim=false)` and shows the result inline (the agent/human can preview before claiming). With `--claim` equivalent via `c`.
- `c` on a row claims it (`store.Claim.Claim(id, actor)`); conflicts surface as `4 conflict` inline.
- `u` unclaims (`store.Claim.Unclaim(id, actor)`).
- `Enter` opens **task detail** (3b).

### 3b - Task detail

A scrollable, sectioned view rendering `store.ShowWithContext(id)` — the exact payload `task show --with-context` returns, including the project guide (FR-017). The action bar at the bottom adapts to the focused section.

```
+--------------------------------------------------------------------------------+
| atm  ATM-0002  Fix claim race                  actor: human:alice  [r]efresh [q]|
| [1 Dashboard] [2 Projects] [3 Tasks] [4 Actors] [5 Help]                       |
+--------------------------------------------------------------------------------+
| ATM-0002  Fix claim race                                                        |
| status: in-progress   claim: agent:claude-1 (2h ago)                           |
| labels: type:bug, area:cli                                                     |
| created: 2026-06-23 10:30   updated: 2026-06-23 11:00                          |
| description:                                                                   |
|   Implement `atm task claim` with atomic locking.                             |
|                                                                                |
| PROJECT GUIDE (always-read)                                                    |
|   conventions:                                                                 |
|     [task] ATM-0001  PR conventions for bugs   [OK]                            |
|     [file] /abs/CONVENTIONS.md                 [MISS]                           |
|   testing:                                                                     |
|     [task] ATM-0006  Testing conventions       [OK]                            |
|                                                                                |
| LINKS                                                            [L] add link   |
|   out  blocks      ATM-0003  Blocked subtask                                   |
|   out  implements  ATM-0010  Epic: agent workflow                             |
|   in   blocked-by  ATM-0005  (computed)                                       |
|                                                                                |
| MATCHING CONVENTIONS                                                           |
|   ATM-0001  PR conventions for bugs   matched: type:bug                       |
|                                                                                |
| TIMELINE  (todos / followups / discussions / history merged by timestamp)       |
|   2026-06-23 10:30  history   h1  created by human:alice                       |
|   2026-06-23 11:00  history   h2  claimed by agent:claude-1                   |
|   2026-06-23 11:01  todo      t1  Write tests for claim   [ ]  agent:claude-1  |
|   2026-06-23 11:02  followup  f1  Decide storage format   open  -> human:alice  |
|   2026-06-23 11:03  discussion d1 Use file-level locking.  human:alice          |
|                                                                                |
|   [t] add todo  [Space] toggle todo  [o] add followup  [O] resolve followup      |
|   [d] add discussion  [s] set-status  [e] edit title/desc  [b] label add/remove|
+--------------------------------------------------------------------------------+
| t:todo Space:toggle o:followup O:resolve d:disc s:status e:edit b:label L:link |
+--------------------------------------------------------------------------------+
```

Actions (each maps 1:1 to a CLI command and calls the same `store` function):
- `s` set-status — popup with the allowed next statuses per the transition matrix (only valid transitions are enabled; others are disabled with a hint, e.g. `open -> review` is invalid). Calls `store.Task.SetStatus(id, status, actor)`; invalid transitions return `4 conflict` inline.
- `e` edit — sub-menu: title / description (multi-line). Calls `store.Task.SetTitle` / `SetDescription`.
- `b` label add/remove — sub-menu: add (form) / remove (pick from the task's labels). Calls `store.Task.LabelAdd` / `LabelRemove`.
- `L` add link — form: `type` (blocks/related-to/implements/documents), `target` (task id picker from the same project). Calls `store.Link.Add`. Stale targets are preserved with a `[STALE]` marker (matches CLI warning).
- `t` add todo (form: `text`) -> `store.Entry.TodoAdd`.
- `Space` toggle todo -> `store.Entry.TodoToggle`.
- `o` add followup (form: `text`, `assignee` optional, `due` optional RFC3339) -> `store.Entry.FollowupAdd`.
- `O` resolve followup (pick from open followups) -> `store.Entry.FollowupResolve`.
- `d` add discussion (form: `text`) -> `store.Entry.DiscussionAdd`.
- `n` (context-sensitive) claim this task if unclaimed; `u` unclaim.
- `v` request review (`store.Review.Request`, status -> review) when status is `in-progress`.

Every mutation appends the matching `HistoryEntry`, exactly as the CLI does; the TIMELINE section re-renders from the same `store.Entry.Timeline` call the CLI uses.

---

## Tab 4 - Actors

Mirrors: `actor list`, `actor show`.

```
+--------------------------------------------------------------------------------+
| atm  actor: human:alice                            [r]efresh [q]uit          |
| [1 Dashboard] [2 Projects] [3 Tasks] [4 Actors] [5 Help]                       |
+--------------------------------------------------------------------------------+
| ACTORS                                              filter: /___             |
|   ID                 KIND   NAME        FIRST SEEN   CLAIMED   OPEN FOLLOWUPS  |
|   agent:claude-1     agent  Claude 1   2026-06-23   2         0                |
|   agent:gemini-1     agent  Gemini 1   2026-06-23   3         0                |
|   human:alice        human  Alice      2026-06-23   0         1                |
|                                                                                |
|   [Enter] show detail                                                          |
+--------------------------------------------------------------------------------+
| Enter:show                                                                     |
+--------------------------------------------------------------------------------+
```

`Enter` opens an actor detail summarizing claimed tasks and open followups (the `actor show` payload). Actors are registered lazily (no `actor create`); the TUI therefore has no "add actor" action — an actor appears the first time it performs a mutation, matching the CLI.

---

## Tab 5 - Help

A scrollable parity table mapping every CLI command to its TUI path, so a human moving between the two surfaces can navigate confidently.

```
+--------------------------------------------------------------------------------+
| Help - CLI / TUI parity                                                        |
+--------------------------------------------------------------------------------+
| CLI command                          TUI path                                 |
| atm init                             Startup flow (no-store prompt) -> [I]      |
| atm store path                        Header -> store indicator                 |
| atm project create                   Tab 2 -> [a]                              |
| atm project list                      Tab 2 (list)                             |
| atm project show                      Tab 2 -> Enter                           |
| atm project set-type-axis             Tab 2 -> [T]                             |
| atm project set-name                  Tab 2 -> [N]                             |
| atm project label add/remove/list     Tab 2 -> [L]/[l]                         |
| atm project repo add/remove           Tab 2 -> [R]/[r]                         |
| atm project guide show                Tab 2 -> Guide pane                      |
| atm project guide section add/rename/ Tab 2 -> [S]/[s]/[X]/[M]                 |
| atm project guide ref add/remove/move Tab 2 -> [g]/[d]/[m]                     |
| atm project guide set-freshness       Tab 2 -> [F]                             |
| atm project guide status              Tab 1 -> GUIDE STATUS (or Tab 2 -> [D])  |
| atm task create                       Tab 3 -> [a]                             |
| atm task show [--with-context]        Tab 3 -> Enter                           |
| atm task list                         Tab 3 (list + filters)                   |
| atm task set-status                   Tab 3 detail -> [s]                      |
| atm task set-title / set-description  Tab 3 detail -> [e]                     |
| atm task label add/remove             Tab 3 detail -> [b]                     |
| atm task next [--claim]               Tab 3 -> [n] / [c]                      |
| atm task claim / unclaim               Tab 3 -> [c] / [u]                      |
| atm task link add/remove/list         Tab 3 detail -> [L]                      |
| atm task todo add / toggle            Tab 3 detail -> [t] / Space              |
| atm task followup add / resolve       Tab 3 detail -> [o] / [O]                |
| atm task discussion add               Tab 3 detail -> [d]                      |
| atm task timeline                     Tab 3 detail -> TIMELINE section          |
| atm review request / approve / reject  Tab 1 -> [v] / [a] / [r] (or Tab 3)     |
| atm review queue / followups          Tab 1                                    |
| atm review dashboard                 Tab 1                                    |
| atm actor list / show                 Tab 4 -> Enter                           |
+--------------------------------------------------------------------------------+
```

## Determinism and parity guarantees

- **Same payload**: every TUI screen renders data from the same `store.*` functions the CLI wraps. The task list order matches `task list --output json` for the same filters; the dashboard matches `review dashboard --output json`; the task detail matches `task show --with-context --output json`. A snapshot test can therefore drive both surfaces from the same fixture.
- **Same mutations**: every TUI action calls the same `store.*` mutation as the CLI command, including history append and actor/timestamp recording (FR-011/FR-012). There is no TUI-only mutation path.
- **Same error semantics**: TUI forms surface the same stable error codes (`4 conflict` for already-claimed / invalid transition / duplicate code, `3 not-found` for missing task/project) as inline messages, not ad-hoc text.
- **No auto-refresh** in v1 (matches research R5 / constitution IV scope); `r` refreshes on demand. A later version may add a debounced refresh; it is not part of v1.

## Performance

SC-005 targets the coordinator view under 1s on 1,000 tasks. Because every screen is a single `store` read composed into widgets (no per-cell fetch), the Dashboard, Task list, and Task detail all meet the same bound as the CLI. The TUI does no background work, so refresh cost equals one store read.

## Out of scope for v1 TUI

- Auto-refresh / live updates (deferred; `r` is manual).
- Mouse support beyond basic click-to-focus (keyboard-first; mouse is a nicety, not a v1 requirement).
- Custom themes / color schemes (a single readable theme; configuration deferred).
- A TUI-driven `atm init` beyond the no-store startup prompt (full store management via a settings tab is deferred).
- Cross-project task views (links are same-project for v1; cross-project views follow when links do).