# TUI Recent Events feed — design (ATM-793b19)

Ledger: ATM-793b19 (Events view). Decisions taken with the user on 2026-07-20.

## Problem

The Projects pane summarizes activity only in aggregate (persona meters, a
per-day density stripe). There is no way to see *what just happened* — the
recent event stream that `atm store log` exposes on the CLI — without leaving
the TUI. The goal is a lazygit-commits-style feed of recent events in pane
[1], directly above the project summary section.

## Decisions

1. **Purpose: ambient activity feed.** Glanceable "what have agents been doing
   lately," newest first. Navigation (jump to task) is explicitly deferred;
   `enter` is reserved for it but a no-op in this iteration.
2. **Git-commit rendering, strict 1:1.** Every event is one line with a
   commit-graph gutter and a short content-hash id. No coalescing: a status
   flip renders as two adjacent lines (`−in-progress`, `+done`). The digest
   lives in the message wording, not in event folding.
3. **Subject-first line anatomy** with actor included and relative age
   right-aligned (see Line format).
4. **Subfocus interaction.** `L` toggles focus into the feed; `j/k` scroll a
   cursor; `esc`/`L` return. Unfocused, the feed is a passive tail pinned to
   newest.
5. **Height: 30/35/35.** The list keeps 30% of the pane; the old 70% summary
   block splits evenly between the feed and the charts.

## Placement & height

`renderList` (internal/tui/projects.go) stacks a third section:
project list, **recent events**, project summary. `projectPaneSplitHeights`
becomes a 3-way split: list 30%, events 35%, summary the remainder. If the
events slot would get fewer than 4 rows (caption + 3 lines), it collapses to
zero and the previous 30/70 behavior is restored. The feed is scoped to the
selected project (`projectScope`), like the summary; with no selection it
shows the summary's muted "select a project" treatment.

## Data & read-model change

Source: the existing `Store.ReadLogCached(code) []core.LogEntry` — memoized,
invalidated by `LastLogSeq` change; already loaded by the pane for the
summary charts. Display order: fold order (`Seq`) descending.

One read-model addition: `core.LogEntry` gains

```go
ID      string   // "sha256:…" event id (display: first 7 hex chars)
Parents []string // causal parents (event ids)
```

populated by `Engine.LogEntries` from the v2 envelope, which already carries
both. V1-format projects have neither: id renders blank and the chain is
implicitly linear (each event's parent is its predecessor). Integrity errors
keep today's behavior — render the recoverable prefix.

## Line format

One event = one line, newest at top, width budget ≈ 38–46 inner columns:

```
● 84fbf58 90171b dev@olm −in-progress   2m
● f634755 90171b dev@olm +done          2m
● e6e0380 –      adm@tui +contextmap    3h
● 99fc74f 90171b dev@olm commented      3h
```

| Column | Width | Content |
|---|---|---|
| graph | 2 | Commit-graph lanes computed from `Parents`: straight `●` line while history is linear; fork/merge lanes (`├─╮`) once synced replicas diverge. Lanes capped at 3; overflow flattens to the first lane. |
| id | 7, dim | Short event hash (first 7 hex chars). Blank for v1 entries. |
| subject | 7 | Task alias with the project-code prefix stripped (`90171b`). Comment events show their parent task's alias. Project-subject events show `–`. |
| actor | 8 | `persona@agent`, each part truncated to fit (`dev@olm`, `adm@tui`). Model suffix dropped. |
| message | rest | Digest wording per action (table below), truncated with `…`. |
| age | 3–4, dim, right-aligned | `2m` / `3h` / `2d`. |

Degradation on narrow panes: below 60 inner columns drop the id column
(revised from the ~36 figure above at spec-writing time — the id column
turned out to cost more relative to the message column than estimated, so
it now yields sooner), then below 30 drop the age.

Implementation delta (Task 5): `eventGraphRows` draws fork/merge as
parallel `│` lanes converging/branching at the `●` row, not the diagonal
`├─╮` junction glyphs sketched above — ATM's history is overwhelmingly
linear, and the diagonal glyphs were dropped as unnecessary polish for an
event shape (>1 lane) that only appears after a sync merges concurrent
replicas.

### Digest vocabulary (closed action set, internal/store/log.go)

| Action | Message |
|---|---|
| `task.created` | `created "<title…>"` |
| `task.title-changed` | `retitled "<title…>"` |
| `task.description-changed` | `description edited` |
| `task.label-added` / `-removed` | `+<label>` / `−<label>`; project prefix always stripped; bare value for the `status:` facet (`+done`), facet kept otherwise (`+type:bug`) |
| `task.removed` | `removed` |
| `task.meta-changed` (v1 legacy) | dim `meta` |
| `comment.created` | `commented` |
| `comment.body-changed` | `comment edited` |
| `comment.label-added` / `-removed` | `comment +<label>` / `comment −<label>` |
| `comment.removed` | `comment removed` |
| `label.upserted` / `label.removed` | `label <name>` / `−label <name>` |
| `project.created` / `.name-changed` / `.removed` | `project created` / `renamed "<name…>"` / `project removed` |
| `project.capability-enabled` / `-disabled` | `+<capability>` / `−<capability>` |

## Interaction

- `L` (free in the list view) toggles subfocus to the events section; the
  caption carries a `[L]ogs` hint mirroring the persona chart's `[P]expand`.
- Subfocused: `j/k` move a highlighted cursor line (windowed via
  `windowLines`), `[`/`]` page by (visible rows − 1), `esc`/`L` return focus
  to the project list.
- Unfocused: passive tail pinned to newest; project selection changes refresh
  the feed.
- `enter`: reserved for jump-to-task; no-op in this iteration.

Implementation deltas (Task 5):
- `g` jumps to newest in the sketch above, but `g` is globally reserved as
  the plugin-command prefix (`app.go`), so the feed pages with `[`/`]`
  instead — matching the project list's own paging keys — rather than
  jumping with `g`.
- `esc` never reaches `projectsModel.handleLogsKey`: it's intercepted by the
  app-level esc branch in `app.go` (the same branch that returns from
  project detail to the list), so feed-exit-on-esc is wired there, ahead of
  the detail-view check. `L` still toggles the feed off from within the
  pane handler as a second path to the same effect.
- `handleLogsKey` clamps `logsCursor` to the feed's last row itself (via a
  `feedLen` helper next to the feed code in `events_feed.go`, reusing
  `newestFeedEntries`'s `maxFeedEvents` cap) rather than relying on
  render-time clamping — `renderEventsFeed` clamps only a local copy for
  display, by design, to keep the render path pure of model writes.

## Error handling

- No project selected → muted placeholder (same as summary).
- `ReadLogCached` integrity error → recoverable prefix, matching current
  summary behavior; no new error UI.
- Empty log → muted `no events yet`.

## Testing

Pure helpers, unit-tested in `internal/tui` following `app_test.go` patterns,
TDD-first per repo convention:

- digest message rendering per action (payload → wording, prefix stripping)
- graph-lane assignment (linear chain, fork, merge, lane cap)
- 3-way `projectPaneSplitHeights` (including the collapse-below-4-rows rule)
- line truncation/degradation at narrow widths
- subfocus key handling (enter/leave, cursor clamp)

`make verify` gates completion. No CLI or store-schema changes beyond the two
read-model fields; no output changes to existing commands (goldens must stay
green).

## Non-goals

- No jump-to-task navigation (reserved, not built).
- No event filtering or search.
- No full-screen events overlay.
- No changes to `atm store log` output.
