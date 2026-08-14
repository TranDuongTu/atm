# Channels-scoped concierge dispatch and the [?] main menu

- **Task:** ATM-3714db
- **Date:** 2026-08-14
- **Status:** Design approved in session; spec awaiting user review

## Problem

Two UX gaps discovered after the channels overlay (ATM-097849) and the universal dispatch dialog (ATM-29f8b0) merged, both hurting the channels onboarding experience:

1. **The concierge dispatched from the channels overlay is unfocused.** `channels.go`'s `c` key opens the universal dispatch dialog preset to `concierge`, but the launched session renders the full concierge onboarding prompt: orient over every capability, interview the user about their whole problem space, map capabilities one at a time — channel wiring only appears inside Step 4's triage. A user who pressed "dispatch concierge" on an empty channels list sits through a general onboarding interview before touching channels. The `--capability` launcher flag exists (`internal/cli/session.go:251`) but is inert beyond validation and the `ATM_CAPABILITY` env var — `renderSessionContext` renders nothing capability-specific.
2. **The channels overlay is undiscoverable and the key map has decayed.** `E` (channels) and `V` (personas) are bound but advertised nowhere. `[C]` is overloaded: capabilities switcher in the tasks pane with a project scope, conventions help everywhere else — and the status bar unconditionally advertises `[C]conv` while the tasks-pane hint says `[C]apabilities`. The projects pane advertises dispatch as `[Ctrl+Shift+→]`, which is not the real binding (`D`). `keymap.go` is a documentation-only table that drifts from the real dispatch switch in `app.go`. The `[?]` help overlay greets the user with a CLI↔TUI parity table that is not useful inside the TUI. The status bar is out of room.

Also confirmed as design input: channel records are tier-1 ledger tasks (synced via events) while wiring is tier-2 per-machine project config (`internal/store/channel.go:194-197`). A fresh machine that synced an existing ledger therefore shows every channel as `○ unwired` — not the empty state. The empty state appears only when the ledger has never had channel records. The scoped concierge must handle both.

## Goals

- Dispatching the concierge from the channels overlay produces a session focused on channel setup for that project, skipping the general onboarding flow.
- The scoped session branches on observed state: empty ledger → guided interview plus local repo auto-discovery to propose channels; records unwired (fresh machine) → investigate the ledger, find matching local clones, propose re-wiring; partial/stale → repair and re-stamp.
- `[?]` opens a context-aware main menu that is the single discovery surface for every TUI action: the focused pane's actions plus global overlays and reference material, each entry showing its key so the menu teaches the shortcuts.
- The status bar is completely freed: no global key hints, no per-pane hints — only store stats, toast, `[?]menu`, version, and refresh recency.
- One declarative table is the source of truth for key labels and menu structure; menu activation and direct keypress share one behavior path so they cannot drift.

## Non-goals

- **No new persona.** The concierge remains the channels onboarding agent; a dedicated channel-setup persona is deferred until the concierge prompt demonstrably bloats (a built-in persona is one markdown file plus a one-word edit if that day comes).
- **No dispatch dialog fork or branching.** The universal dialog stays one generic form per the ATM-29f8b0 decision of record; context supplies defaults only.
- **No new probing or discovery code.** Auto-discovery is prompt guidance executed by the agent with existing tools; ATM's local probe surface is unchanged.
- **No filterable command palette.** The menu is a plain navigable list; type-to-filter can be layered on later.
- **No key rebinding beyond removing the conventions binding.** `D`, `E`, `V`, `T`, `1`, `2`, pane-local keys all keep their current meanings.

## Decisions of record

1. **Tailor the launched session, not the launcher.** The dispatch dialog is a launcher; the onboarding experience lives in the session it spawns. Scoping is delivered through the rendered session context, keyed on `--capability`.
2. **`--capability` gains rendered meaning.** When a session launches with a capability scope, the session context gains a `Session scope` section. The section is capability-agnostic template infrastructure; capability-specific flow lives in that capability's guide.
3. **The channels overlay passes `capability=channel` as a dispatch default.** Shown in the dialog as a read-only context line, appended to argv as `--capability channel`. No dialog logic branches on it.
4. **Concierge persona file is untouched.** The scope section plus the channel capability guide carry the specialization.
5. **`[?]` is the main menu; the help overlay is absorbed into it.** The keymap reference and CLI↔TUI parity table become menu reference entries rendered on demand, reusing the existing `help.go` table renderers.
6. **The conventions binding is removed entirely.** Conventions become a menu reference entry. `C` means the capabilities switcher wherever a project scope exists, and is otherwise unbound — the collision and the `[C]conv` advertisement disappear.
7. **Menu activation replays the entry's key.** Activating an entry closes the menu and feeds the entry's bound key through the normal key dispatch path. One behavior path; the menu cannot drift from the keys.
8. **The menu overlay is hand-rolled in the existing sub-model idiom** (`menuModel` with `handleKey`/`renderOverlay`, like `channelsModel`), not built on the unused `internal/tui/components` package. A plain list does not need the dead `List`'s filter machinery; `components/` stays as-is, out of scope.
9. **Per-pane keys stay implemented in their pane handlers.** The declarative table describes them (key, label, scope) for menu rendering; replay delivers behavior. A parity test keeps table and handlers honest.

## Design

### 1. Session scope section

`session.ContextData` gains a `Capability` field; `renderSessionContext` and the launch path thread `opts.Capability` into it. When non-empty, the context template renders after the persona prompt:

```
## Session scope

This session is scoped to the `<capability>` capability for project <CODE>. Skip any general onboarding or survey flow your persona defines — orient only as far as this capability needs. Read `atm capability <capability> guide` first, then work solely on this capability's setup and health, and hand off when it is healthy.
```

The context cache key (`internal/cli/launcher_shared.go`, `cacheKey`) is `session-<persona>[-<task>]` today and does NOT include the capability, so a scoped and an unscoped launch of the same persona would share one cache file. `cacheKey` gains a capability segment: `session-<persona>[-<task>][-<capability>]`.

### 2. Channel capability guide: scoped session flow

The channel capability guide (`skills/capability/channel.md`) gains a `Scoped session` section the concierge reads via the scope instruction:

- Run `atm channel list --project <CODE>` first and branch on what it shows.
- **No records:** interview the user (repos, Notion workspaces, other flows — in their words), and in parallel discover candidates: scan the working directory, its parent, and sibling checkouts/worktrees for git repositories; propose each discovered repo (remote URL as address, folder as wiring) for confirmation before `atm channel add` + `atm channel wire` + verify + `atm channel stamp`.
- **Records without wiring (fresh machine):** for each unwired record, search local checkouts for a repo whose remote matches the record's address; propose found paths for `atm channel wire`, offer to clone missing ones, re-authorize Notion MCP servers, re-stamp after verifying. This is the synced-ledger case the concierge prompt already names; the guide makes it the scoped session's primary branch.
- **Partial or stale (`◐`/`○` with wiring):** report status notes and repair — re-verify, re-wire moved paths, re-stamp.
- Never expose flag shapes to the user; never ask for tokens (unchanged concierge doctrine).

### 3. Dispatch dialog: capability as a default

`dispatchModel` gains a `capability` string. `open()` accepts it alongside persona/project/task (all existing call sites pass `""`; `channels.go` passes `"channel"`). Rendering: a read-only `Scope` line shown only when set, in the same style as the `Task` line. `submit()` appends `--capability <name>` to argv when set. No cursor, no cycling, no branching.

Bug fix folded in: `submit()` currently appends `--project` only when the persona *requires* it, so a concierge dispatched from the channels overlay launches without `--project` even though the overlay resolved one — the scoped session would render with literal project placeholders. `submit()` changes to append `--project` (and permit `--task`) for any non-admin persona whenever a project scope is present; project-optional personas accept `--project` at the CLI already. The admin exclusion mirrors the existing `title()`/`projectRequired()` special case.

### 4. The key/menu table

`keymap.go`'s `keymapRows` evolves into the single declarative table. Each entry: `key` (display string and replay sequence), `label`, `scope` (`global`, `pane:projects`, `pane:projects-detail`, `pane:tasks`, `pane:tasks-detail`, `pane:boards`, …), and `section` (`actions`, `views`, `reference`). Reference entries (`conventions`, `keymap`, `cli-parity`) have no key and open menu detail views instead of replaying.

Pure navigation entries (`↑/↓` movement, `j/k` scroll, paired cycle keys) are marked menu-hidden: they appear in the keymap reference but not as activatable menu entries, since replaying one step of a movement pair is not a meaningful action. Menu Actions list only single-shot verbs (add, edit, toggle, remove, drill, sort…).

The table drives: menu content and labels, the keymap reference rendering, and the parity test. It does not execute behavior — replay does. The keymap reference view renders the table flat (`Key | Where | Action`, grouped by scope) instead of today's hand-maintained four-column matrix, which is deleted with `keymapRows`.

### 5. `menuModel`

- `?` opens the menu overlay (replacing the help overlay's slot in the key-consumption order).
- Content, in sections: **Actions** — entries whose scope matches the focused pane and its drill state; **Views** — global entries (Dispatch `D`, Channels `E`, Personas `V`, Capabilities `C` when project-scoped, Theme `T`, panes `1`/`2` — exact set finalized from the table during implementation); **Reference** — Keymap, CLI ↔ TUI parity, Conventions, rendered inline as scrollable detail views (reusing `help.go`'s `keymapTable`/`parityTable` and the conventions text).
- Keys: `↑/↓`/`j/k` move, `→`/`Enter` activate, `←`/`Esc` back/close. Activating a keyed entry closes the menu and replays the key; activating a reference entry drills into its detail view.
- Entries render as `label` right-aligned with `[key]`, so the menu is also the keymap teacher.

### 6. Status bar cleanup

`renderStatusLine` right side becomes `[?]menu` + version + refresh recency. The hardcoded `[?]help [C]conv [T]theme` cluster and the entire `statusHint()` mechanism (all three per-pane implementations) are removed. The left side (store stats, pane hint, toast) is unchanged.

### 7. Key dispatch changes in `app.go`

- `?` opens `menuModel` (menu consumes keys first while open, same pattern as other overlays).
- The `C` conventions fallback branch is removed; `C` outside a project-scoped tasks context is a no-op.
- The old help overlay model is deleted; its renderers move under the menu's reference views.
- All other bindings unchanged.

## Error handling

- Menu replay of a key that the current context ignores behaves exactly like pressing that key directly (no-op or toast) — acceptable by construction since entries are scope-filtered before display.
- A scoped dispatch for a capability the project has not enabled is already rejected at launch by `validateCapabilityScope` with a remediation message; the dialog does not pre-validate (dispatch failures surface in the spawned target, as today). The channels overlay only offers `c` when a project is selected, and the channel capability being present is what put channels in the ledger in the first place; the edge (capability disabled after records exist) fails with the CLI's clear message.

## Testing

- **Context rendering:** golden for a session context with `--capability channel` (scope section present, correct capability name and code); golden asserting no scope section when the flag is absent.
- **Dispatch dialog:** argv includes `--capability channel` when opened from channels; `Capability` line renders when set and is absent otherwise; existing dispatch tests stay green.
- **Menu:** opens on `?`; sections filter by focused pane and drill state; activating a keyed entry closes the menu and the replayed key takes effect (e.g. activating Channels opens the channels overlay); reference entries render their detail views; `Esc`/`←` navigation.
- **Parity test:** every keyed table entry's replay sequence is consumed by the corresponding handler in its scope (guards decision 9's drift risk); the table contains no entry advertising an unbound key (guards a regression of the `[Ctrl+Shift+→]` bug).
- **Status bar:** renders `[?]menu` and no other key hints in every pane and drill state.
- **Guide/prompt:** channel capability guide golden updated for the new section.
