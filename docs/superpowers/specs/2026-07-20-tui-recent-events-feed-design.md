# TUI Recent Events feed — design (ATM-793b19)

Ledger: ATM-793b19 (Events view). Decisions taken with the user on 2026-07-20.

> **Revision 2 (2026-07-20), after the first implementation shipped.** The
> feed is now a bordered box aligned with the summary chart boxes, and the
> `L` subfocus mode is replaced by modeless `Shift`+arrow navigation. See
> **Revision 2 — boxed feed and modeless navigation** at the end of this
> document, which supersedes the Interaction section and decisions 1 and 4.

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
events slot would get fewer than 4 rows — 2 content lines under 2 lines of
frame (top/bottom border in the boxed form, a caption line plus a padding
line in the compact form; see Revision 2's R2-8) — it collapses to zero and
the previous 30/70 behavior is restored. The feed is scoped to the selected
project (`projectScope`), like the summary; with no selection it shows the
summary's muted "select a project" treatment.

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
| message | rest | Digest wording per action (table below), truncated with `...` (the shared `truncateRunes` helper appends ASCII `...`, not `…`). |
| age | 3–4, dim, right-aligned | `2m` / `3h` / `2d`. |

Degradation on narrow panes, three rungs, each yielding its columns to the
message: below 60 inner columns drop the id column (revised from the ~36
figure above at spec-writing time — review of the rendering task measured
that at a 120-column terminal the Projects pane's inner width is only ~46
columns, where the 7-column id plus its separator consumed 8 columns and
left roughly 16 for the digest message; the id now yields to the message
below 60 columns); below 30 drop the age; below 28 drop the actor
(`feedActorMinWidth`, a later review fix, R2-9, later widened from an initial
26) — the id and the age had already gone by the 60-70-column terminal a box
renders at (~19-22 inner columns there), and the message column was still
empty or near-empty; the actor yields last, after the age, because at these
widths it is the only column left besides the subject worth trading for
message room, and the subject stays in every rung — it names WHAT changed,
which the message wording alone cannot always recover. The threshold sits at
28 rather than the original 26 because 26 is exactly an 80-column terminal's
box inner width: at 26 the actor was still retained, and the fixed cost
before the message (gutter + subject + actor = 19 columns) left only
26 − 19 = 7 columns for the digest — narrower than the 9-12 columns the same
rule protects at 60-70 columns, and the narrowest point anywhere in the
ladder, even though nothing before this fix flagged it as broken. Raising
the threshold to 28 moves the 80-column terminal onto the actor-dropped rung
instead, where the fixed cost drops to 10 and the message column widens to
26 − 10 = 16.

Implementation delta (Task 3): `eventGraphRows` draws fork/merge as
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

---

# Revision 2 — boxed feed and modeless navigation

Decisions taken with the user on 2026-07-20, after the first implementation
shipped. **This section supersedes the Interaction section above, and
decisions 1 and 4.** Everything else — the line format, digest vocabulary,
read-model change, graph rendering, error handling — is unchanged.

## Why

Two problems with the shipped version. The feed rendered as a bare caption
plus rows while the two summary charts below it are bordered boxes, so the
pane read as three sections in two visual languages. And `L` subfocus was
modal: while it was on, `handleListKey` short-circuited every key into the
feed handler, so `a`, `s`, `enter` and `x` were silently swallowed, and the
mode could survive a resize that hid the box it belonged to.

## R2-1. The feed is a bordered box

`renderEventsFeed` renders through the existing `renderChartBox`, with the
title `Recent Events  [Shift-↑↓]` — the key hint appended to the title, the
same shape as the persona chart's `activity by persona  [P]expand`. This
holds only at pane heights where the summary charts also box; below that
threshold the feed renders a compact form instead, matched to the summary
section's own degradation — see R2-8, a later review fix.

Alignment with the summary charts is automatic and needs no new code: every
box computes its width as `chartBoxWidth(p.width)` (96% of pane width) and
is centered with `prefix := spaces((p.width - boxW) / 2)`. Identical pane
width in, identical edges out.

Two adjustments are required because `renderChartBox` was written for
charts, and both are satisfied by the body the feed hands it rather than by
changing the box helper — so the existing charts cannot regress:

- The box **center-pads each body line** (`leftPad = (innerW - width) / 2`).
  Feed lines must be left-aligned, so the body emits lines already exactly
  `chartBoxInnerWidth(p.width)` wide; the centering arithmetic then yields
  zero and is a no-op.
- The box **top-pads a short body**, floating content to the vertical
  middle. The body emits exactly `maxLines - 2` lines, blank-filled at the
  bottom, so `topPad` is zero and the feed sits at the top.

Feed lines are therefore sized to the box's inner width, not the pane
width. The 60/30 degradation thresholds now measure that inner width
(~42 columns at a 120-column terminal, versus ~46 before).

The events slot still collapses below 4 rows, and still renders nothing when
no project is selected.

## R2-2. Navigation is modeless

There is no focus state. The `Shift` modifier is the only discriminator for
which widget the arrows drive — the same pattern the Tasks pane already uses
for board thumbnails (`tasks_list.go`: plain `j/k` move the task list while
`shift+up/down` move the thumbnail's chart cursor, in one switch, with no
mode flag).

| Key | Drives |
|---|---|
| `shift+↑` / `shift+↓` | events box, one line |
| `shift+←` / `shift+→` | events box, one page (visible rows − 1) |
| `j` `k` `↑` `↓` `g` `[` `]` `a` `s` `enter` `e` `x` | project list, unchanged |

`shift+←`/`shift+→` are free in the Projects pane; only the Tasks pane binds
them (thumbnail drill in/out).

## R2-3. Pure scroll, no cursor

`logsCursor` (a cursor index with a highlighted row) becomes `logsOffset` (a
viewport offset). No row is highlighted: a persistent highlight in a box
that never holds focus reads as noise, and the feed is a scanning surface.

`logsOffset` is clamped in the key handler against the feed length and the
visible row count — never in render, which stays pure of model writes. It
resets to 0 on project switch and when the project scope is cleared.

## R2-4. Deletions

The modal machinery goes away entirely: `logsFocus`, the `L` binding,
`handleLogsKey`'s early-return that stole every key, the app-level `esc`
branch for feed exit, the `SetSize` guard that released focus when the box
collapsed, the `confirmYes` defensive reset, and the `[L]ogs` status hint.

The stranding hazard those last two guarded against stops being expressible:
with no mode, there is nothing to be stranded in.

Keymap: drop the `L` row; fill the Projects column on the existing
`Shift+Up/Down` and `Shift+Right/Left` rows, which read `-` there today.

## R2-5. Consequence for jump-to-task

The original decision 1 reserved `enter` for a future jump-to-the-selected-
event. With no cursor there is no selected event, so that path is closed.
If jump-to-task is built later it needs a different entry point — a
full-screen events overlay is the natural one. Accepted deliberately: the
feed is an ambient scanning surface, not a navigation surface.

## R2-6. Testing

- box body is left-aligned and top-aligned (assert against a rendered line,
  not the helper's internals)
- events box edges align with the summary chart boxes at several widths
- `shift+↑/↓` scroll the feed while plain `j/k` still move the project list,
  in the same test
- `shift+←/→` page; offset clamps at both ends
- the offset resets on project switch
- no key that worked in the project list before is swallowed now
- (R2-8, later review fix) at a pane height where the persona chart renders
  unboxed, the events feed is also unboxed; at a height where the persona
  chart boxes, the feed boxes too — asserted against both sections' actual
  rendered output in the same render
- (R2-9, later review fix) the actor column's degradation is pinned at
  `feedActorMinWidth`'s exact boundary on both sides, and a 60-column
  terminal's digest message is asserted non-empty

## R2-7. Non-goals unchanged

Still no filtering, no search, no overlay, no `atm store log` changes.

## R2-8. Framing follows the summary charts (final review fix, I1)

R2-1 shipped the feed as always-boxed. That reintroduced the pane's original
problem, inverted: below the height where the persona chart itself stops
boxing (`renderPersonaActivityChart`'s own "4 lines and up" rule), the feed
still boxed unconditionally, so the pane again read as sections in two
visual languages — at the classic 80x24 terminal, among others.

The feed does not decide its own frame. `renderList`
(`internal/tui/projects.go`) computes `eventsH` and `summaryH` together
already, so it also computes `summaryChartsBoxed(summaryH)` once there and
hands the result to `renderEventsFeed` as a `boxed bool`.
`summaryChartsBoxed` replays only the arithmetic `renderSummary` and
`renderPersonaActivityChart` already use to pick their own frame — subtract
the "Project Summary" header and `project: … tasks: …` line (2 lines), then
apply `chartBoxHeights` and the same "boxed once the persona chart's own
height is 4 or more" rule — without touching either function. The feed and
the persona chart therefore always agree on which visual language the pane
speaks, even though the activity-stripe chart directly below the persona
chart can still box on its own at a smaller height than the persona chart
does; that's a pre-existing wrinkle inside the summary section, unchanged by
this fix and out of scope for it, since the feed is compared against the
persona chart specifically.

Below the threshold, `renderEventsFeed` renders a compact form instead of a
box: a caption line (`Recent Events  [Shift-↑↓]`, the same title constant
and key hint the box uses) followed by left-aligned rows sized to the pane
width directly — the way the feed rendered before it was ever boxed.
Content, ordering, column degradation, and the offset/scroll window are
identical between the two forms — `eventsFeedBody` is the single content
path both share, taking an inner width and a row count as parameters; only
the frame differs. Both forms budget the same 2 lines of frame
(`eventsFeedVisibleRows`, unchanged) even though the compact form's own
caption is a single line, so the offset clamp `scrollEventsFeed` enforces
never disagrees with what either form actually draws — a mismatch there
would reintroduce the stranded-events bug R2-3's clamp exists to prevent.

## R2-9. A third narrow-width rung: the actor column also yields (final review fix, I2)

See the "Line format" section's degradation paragraph, updated in place: a
third rung, `feedActorMinWidth` (28, widened from an initial 26 — see that
paragraph for the arithmetic), drops the actor column below its own
threshold — tighter than `feedAgeMinWidth` (30), so the actor is always the
last column to yield, after the id and the age, and never ahead of them.
Without it, a 60-70-column terminal's box inner width (~19-22 columns) left
the message column empty or barely non-empty even after the id and age had
already gone: a box of feed rows showing only a graph glyph and a truncated
actor name, conveying nothing. The subject column never drops in any rung —
it is the one piece of "what changed" the message wording cannot always
recover on its own.
