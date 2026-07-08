# Persona Activity in Projects Pane + Overlay — Design Spec

**Status:** Draft (awaiting user review)
**Depends on:** Personas & actor activity (2026-07-07), TUI three-pane workspace (2026-07-04).
**Follow-up to:** ATM-0052.

## Driver

ATM-0052 shipped personas, alias migration, and a `[4] Actors` **maximized workspace
pane** showing persona-grouped activity. Two gaps emerged in dogfooding:

1. The **Projects pane** (`[1]`) still shows an "activity by actor" chart built from
   *raw actor strings* (`opencode-dev`, `codex`, …) counted verbatim
   (`projects.go:actorActivityRows`). After `atm actor migrate`, the alias table
   resolves those strings to personas — but the Projects pane never consults it, so
   the chart keeps listing stale actor names even though the activity data is
   persona-grouped in `[4]`. The user sees two contradictory views.
2. The `[4]` maximized pane is a heavy surface for what is really a drilldown of the
   Projects pane's chart: it replaces the whole 3-pane workspace just to show a
   ranking + breakdown. And its bar widths are misaligned (the meter-width formula
   over-reserves by 4 columns vs. the fixed Projects/Labels charts).
3. Personas can only be created via the CLI (`atm persona create`). A TUI-first user
   has no in-app way to add one.

This spec closes all three: it moves persona activity **into the Projects pane**
(both as a compact chart and as an expandable overlay), removes the separate `[4]`
pane, and adds an in-TUI `p` form to create a persona.

### Relationship to ATM-0052

ATM-0052's store layer, alias table, `activity.Build`/`Aggregate`, `atm activity`
CLI, and `atm persona` CLI are **unchanged**. This spec is a pure **TUI
re-surfacing** of the same read-side aggregation: the same `activity.Group` data
feeds the Projects chart and the overlay instead of a dedicated pane. The store
never rewrites log entries; alias resolution stays a read-side concern.

## Decisions (locked during brainstorming)

| # | Decision |
|---|----------|
| D1 | **In-place chart swap.** The Projects pane's "activity by actor" chart box keeps its exact dashboard footprint (shares the summary height budget with the activity stripe and bubble chart as today) but renders **persona-grouped** bars via `activity.Build` + aliases. Title becomes "activity by persona". |
| D2 | **Remove the `[4] Actors` maximized pane.** `numPanes` returns to 3; the `4` tab key is removed; the maximized-`paneActors` render branch is deleted. Actor/persona activity surfaces only through the Projects pane. |
| D3 | **`P` expand overlay.** Pressing `P` while focused on the Projects pane (list or detail) opens a centered modal overlay containing the full persona list + drilldown (the content that lived in `[4]`). `Enter` drills into a persona's agents/models/actions breakdown; `Esc` returns to list; `Esc` again closes the overlay. |
| D4 | **`p` add persona.** Pressing `p` while focused on the Projects pane (anytime — list, detail, or while the `P` overlay is open) opens a `New persona` form collecting **name + description only**. The prompt is left empty; the user sets it later via `atm persona edit --prompt-file` (CLI). |
| D5 | **"Remove old actor names everywhere" = activity views only.** HISTORY lines in project/task detail and FACTS `created by` / `updated by` keep showing the **raw actor string** (audit provenance is the literal stamp; display-rewriting it would misrepresent the record). Only the activity chart and overlay resolve through aliases. The store's `log.jsonl` is never rewritten. |
| D6 | **Bar-width alignment.** The Projects chart and the overlay list both use the already-fixed Projects/Labels formula: `meterW = availableWidth - nameW - fixedSuffixWidth`, where the fixed suffix is the percent + count columns (and the cursor prefix in the overlay list). This eliminates the current `[4]` misalignment where `meterW = a.width - nameW - 16` over-reserved by 4. |

## Components

### 1. Projects pane — persona activity chart (replaces "activity by actor")

`internal/tui/projects.go` `renderProjectSummary` keeps its height budgeting
(`remaining == 1/2/3`, `>= 9` three-chart split) **unchanged**. Only the chart
content changes.

**Removed:**
- `actorActivityRow` struct (projects.go:54-58)
- `actorActivityRows` (projects.go:92-131)
- `renderActorActivityChart` (projects.go:515-548)
- `longestActorNameWidth` (projects.go:550-558)

**Added:** `renderPersonaActivityChart(entries []store.LogEntry, maxLines int) []string`:
1. `aliases, _ := p.m.store.LoadAliases()` (best-effort; empty map if absent).
2. `groups := activity.Aggregate(activity.Build(entries, aliases), "persona")`.
3. Box title: `"activity by persona"`. Caption line for the `remaining == 1`
   degenerate case (projects.go:457) becomes `"activity by persona"`.
4. Row format (byte-safe width via `lipgloss.Width(g.Key)`):
   ```
   nameW := longestPersonaKeyWidth(groups)
   meterW := chartBoxInnerWidth(p.width) - nameW - 10
   if meterW < 10 { meterW = 10 }
   line := fmt.Sprintf("%-*s %s %3d%% %3d", nameW, g.Key, meterBar(percent, meterW), percent, g.Count)
   ```
   `longestPersonaKeyWidth` uses `lipgloss.Width` (not `len`) for display-width
   safety.
5. `entryCap` (10) still folds overflow into a single `(others)` row, but
   aggregation collapses to few personas so this is rarely hit; keep it for the
   `(none)` + many custom-persona case.
6. The box itself is rendered by the existing `renderChartBox` — no layout math
   changes.

The activity stripe and bubble chart are **untouched**.

### 2. `P` expand overlay

**State on `Model`** (`internal/tui/app.go`): new field `actorsOverlay bool`.
When true, `View()` renders a centered modal on top of the workspace via
`placeOverlay` (the same mechanism used by help/forms/confirms).

**Sizing:** the overlay box uses the existing `helpBoxSize()` dimensions (the
larger centered modal). The inner `actorsModel` is sized to the box's inner
width/height via `actorsModel.SetSize`.

**Opening:** in `handleKey`, a new `case "P"` in the Projects-pane block:
- If `m.projectScope == ""`: `m.showToast("select a project first")`; return.
- Else: `m.actorsOverlay = true; m.actors.refresh()`.

`actorsModel.refresh` already reads `m.projectScope`, loads aliases, and calls
`activity.Aggregate(activity.Build(...), "persona")` — no change needed.

**Rendering:** `m.renderActorsOverlay()` returns a
`titledBoxHeight(m.styles.DialogBody, bw, "Activity by persona", m.actors.View(), bh)`
centered via `placeOverlay` in `View()`, layered after form/confirm.

**Key handling while overlay is open:** `handleKey` routes keys to the overlay
*before* pane dispatch when `m.actorsOverlay` is true:
- `j`/`k`/`up`/`down`/`enter` → `m.actors.handleKey(k)` (existing; `enter` toggles
  `detail`, arrows move cursor).
- `p` → open the `New persona` form (layers on top of the overlay; `Esc` on the
  form returns to the overlay, not the workspace).
- `esc`:
  - If `m.actors.detail` → `m.actors.handleKey(k)` sets `detail = false` (back to
    list).
  - Else → `m.actorsOverlay = false` (close overlay, return to workspace).
- `?` / `C` / `T` keep working (help/theme), consistent with other overlays.
- `q` does **not** quit while the overlay is open (Esc closes it first),
  matching the form/confirm overlay convention.

### 3. `actorsModel` refactor (`internal/tui/actors.go`)

`actorsModel` loses its maximized-pane role and becomes the **overlay renderer**.
It already renders list + detail and handles `up/down/j/k/enter/esc`; the changes
are:

- `SetSize(w, h)` is called with the overlay box's inner dimensions when the
  overlay opens (instead of the full workspace).
- **`renderList` bar-width fix:** the current `meterW = a.width - nameW - 16`
  over-reserves by 4 (the format string's fixed suffix is 12, not 16). Change to:
  ```
  // cursor(1) + nameW + space(1) + meter + space(1) + %3d%%(4) + space(1) + %4d(4) = nameW + meter + 12
  meterW := a.width - nameW - 12
  if meterW < 10 { meterW = 10 }
  line := fmt.Sprintf("%s%-*s %s %3d%% %4d", cursor, nameW, g.Key, meterBar(percent, meterW), percent, g.Count)
  ```
  This makes bars align to the right edge like the Projects/Labels charts.
- `renderDetail` / `writeBreakdown` keep their structure; `meterW = a.width - 24`
  is fine inside the larger overlay box.
- The `select a project (pane 1) to see actor activity` empty-state line
  (actors.go:89) becomes `select a project to see actor activity` (no pane
  number, since the Actors pane is gone).
- The maximized-pane `statusHint()` (actors.go:43-48) is removed — the overlay's
  hint is rendered in the modal, not the status line. The status line falls back
  to the global hint while the overlay is open (or shows an overlay-specific hint;
  see §5).

### 4. `p` add persona form

**Form** (`internal/tui/form.go` reused; new `formKind` value `formPersonaCreate`):

```go
fields := []formField{
    {Label: "name", Required: true, Hint: "lowercase slug, e.g. staff-engineer",
     Validator: personaNameValidator},
    {Label: "description", Hint: "one-line summary (optional)"},
}
f := NewForm("New persona", fields)
m.form = f
m.formKind = formPersonaCreate
```

`personaNameValidator` calls `store.ValidatePersonaName(value)` and returns its
error wrapped as a user-readable string. No prompt field (per D4).

**Submit** (`doPersonaCreate` in app.go):
```go
name := vals["name"]
desc := vals["description"]
_, err := m.store.CreatePersona(name, "", desc, m.actor)
if err != nil {
    if store.IsConflict(err) {
        m.showToast(fmt.Sprintf("persona %s already exists", name))
    } else {
        m.showToast("error: " + err.Error())
    }
    return nil
}
m.showToast(fmt.Sprintf("created persona %s", name))
m.actors.refresh()   // update overlay/chart if open
m.refreshAll()
return nil
```

**Key dispatch:** `case "p"` in the Projects-pane block of `handleKey`. `p` is
currently free across the entire TUI (verified: no `case "p"` in any pane), so
there is no collision. While the `P` overlay is open, `p` is forwarded to form-open
(see §2 overlay key handling) — the form layers on top of the overlay; `Esc` on
the form returns to the overlay.

### 5. Workspace integration (`internal/tui/app.go`)

**Removed:**
- `paneActors` from the `workspacePane` enum; `numPanes` returns to 3.
- The `case "4"` tab key in `handleKey` (app.go:381-384).
- The `if m.focused == paneActors` maximized-render branch in `renderWorkspace`
  (app.go:567-569).
- The `case paneActors` in `statusHint` (app.go:598-600).
- The `if m.focused == paneActors && m.actors.detail` Esc branch (app.go:426-429).
- `m.actors.SetSize(...)` in `SetSize` (app.go:164) — the overlay sizes itself on
  open.
- `m.actors.refresh()` on `4` (app.go:383).
- The `case paneActors` in the per-pane key dispatch (app.go:441-442).

**Added:**
- `actorsOverlay bool` field on `Model`.
- `case "P"` in the Projects-pane key dispatch.
- `case "p"` in the Projects-pane key dispatch.
- Overlay-open key routing block in `handleKey` (before pane dispatch), as
  described in §2.
- `m.renderActorsOverlay()` and the `placeOverlay` call in `View()`.
- `formPersonaCreate` in the `formAction` enum; `case formPersonaCreate` in
  `submitForm`; `doPersonaCreate`.

`Model.actors` stays (the `actorsModel`); it's just rendered into the overlay
instead of a pane.

### 6. Keymap & help (`internal/tui/keymap.go`)

Update `keymapRows`:
- Add: `{"P", "expand activity by persona", "-", "-", "-"}`.
- Add: `{"p", "add persona", "-", "-", "-"}`.
- Remove the `4` row if present (the keymapRows table doesn't currently list `4`,
  but the `1/2/3` row becomes `1/2/3` only — it already is; no change needed
  there).

Update the help overlay text to reflect the overlay (the help source is whatever
`atm conventions` renders; add a short note that `P` expands the persona activity
chart and `p` adds a persona).

## Data flow

```
Projects pane summary render:
  ReadLog(projectScope) -> LoadAliases -> activity.Build -> activity.Aggregate("persona")
    -> renderPersonaActivityChart -> boxed persona bars (developer/manager/...)

P key (Projects pane focused):
  actorsOverlay = true; actors.refresh()
    -> actorsModel.View() -> titledBoxHeight("Activity by persona", list/detail)
    -> placeOverlay(workspace, overlay)

p key (Projects pane, anytime or while overlay open):
  open New persona form (name + description)
    -> submit -> store.CreatePersona(name, "", desc, actor)
    -> toast; actors.refresh(); refreshAll()

Esc while overlay open:
  if actors.detail: detail = false (back to list)
  else: actorsOverlay = false (close overlay)
```

## Error handling

- `P` with no project selected → toast `"select a project first"`; overlay does
  not open. (Consistent with how the chart itself shows "select a project to see
  summaries".)
- `p` form with an invalid name (fails `ValidatePersonaName`: uppercase, `@`,
  `:`, empty, bad slug) → live field error in the form (existing `fieldError`
  mechanism); submit is disabled while invalid.
- `doPersonaCreate` on `ErrConflict` (persona exists) → toast
  `"persona %s already exists"`; form stays closed (it already closed via
  `submitForm`'s `defer closeForm`). The user re-opens `p` to try again.
- `LoadAliases` failure (corrupt `actor-aliases.json`) → best-effort empty map;
  the chart/overlay falls back to convention-parse resolution, so personas
  still attribute (unaliased legacy strings bucket to `(none)` or
  `developer`/`manager` via convention). No hard error in the TUI.
- `ReadLog` integrity error → existing `projectSummaryData` already tolerates it
  (returns `ok=false` → "selected project could not be loaded"). The overlay's
  `actors.refresh` returns silently on error (already does).

## Testing

- **Persona chart:** `renderPersonaActivityChart` over a synthetic log with
  legacy + convention actors resolves to persona rows (`developer`, `manager`,
  `(none)`) with correct counts/percent; bars align (assert the line width
  equals `chartBoxInnerWidth`); empty log renders "no activity yet"; title is
  "activity by persona".
- **`P` overlay:** pressing `P` sets `actorsOverlay=true` and refreshes;
  `View()` contains the persona bars inside the modal; `Enter` drills to detail
  (agents/models/actions breakdown visible); `Esc` returns to list; second `Esc`
  closes the overlay (`actorsOverlay=false`); `P` with no project selected
  toasts and does not open.
- **`p` form:** pressing `p` opens `New persona` form; name validator rejects
  uppercase/`@`/`:`/bad-slug live; submit with valid name calls
  `CreatePersona(name, "", desc, actor)`; on conflict toasts "already exists";
  on success toasts "created persona %s" and the overlay/chart refresh to
  include the new persona; `Esc` cancels without creating.
- **`p` while overlay open:** `p` opens the form on top of the overlay; `Esc`
  on the form returns to the overlay (not the workspace).
- **Removal regression:** `numPanes == 3`; the `case "4"` handler is removed from
  `handleKey`, so pressing `4` falls through to default rune handling (no focus
  change); `View()` never contains `[4] Actors`.
- **Keymap/help:** the help overlay lists `P` (expand) and `p` (add persona);
  the `4` row is absent.
- **Bar alignment:** golden snapshot of the overlay list and the Projects chart
  assert bar ends align (the meter fills exactly to the column before the
  percent).

## Out of scope (YAGNI)

- Editing or removing personas from the TUI (CLI-only; `p` only adds).
- Setting a persona's prompt from the TUI (single-line form can't do it well;
  CLI `--prompt-file` remains the path for multi-line prose).
- A global / cross-project activity roll-up (unchanged from ATM-0052's YAGNI).
- Display-rewriting raw actor strings in HISTORY/FACTS (D5 — audit provenance
  stays literal).
- Binding a favorite agent/model into the persona entity (unchanged from
  ATM-0052).
- Re-running `atm actor migrate` from the TUI (it remains an explicit CLI action).

## Rollout / compatibility

Pure TUI change. No store schema change, no new log action, no CLI change. The
`[4] Actors` tab simply disappears; users who pressed `4` now press `P` in the
Projects pane for the same content (plus drilldown). Existing alias tables and
personas are read as-is. `make verify` (build + tests) is the gate; a manual
smoke (`atm tui`, `P`, `p`) confirms the surface.

The bar-width fix is a pure rendering correction; no data or behavior change.