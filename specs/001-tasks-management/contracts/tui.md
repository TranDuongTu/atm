# TUI Contract: Tasks Management System

**Feature**: 001-tasks-management | **Date**: 2026-06-23 | **Spec revision**: v1.1.0

The TUI (`atm tui`) is the human-facing management surface. It is a thin client over `internal/store` and **mirrors every CLI operation** in `contracts/cli.md` (FR-002: the TUI is a thin client over the same operations). The TUI adds no data path and no mutation that the CLI does not also expose; every screen renders the same `store.*` payloads the CLI emits, and every action calls the same `store.*` mutation (including history append and actor/timestamp recording, FR-011/FR-012).

Launch: `atm tui [--store <path>] [--actor <id>]`. Store resolution is identical to the CLI (`--store` > `ATM_HOME` > `~/.config/atm`; no walk-up-from-CWD). If the store is missing/empty, a startup prompt offers to `Init` it (the same `store.Init` as `atm init`). If `--actor`/`ATM_ACTOR` is unset, the TUI prompts for an actor id once per session (it does not persist the choice); mutating actions are disabled until an actor is set.

Visual mockups and keymaps live in `tui-mockups.md`. This contract captures the *behavioral* guarantees the TUI must uphold.

## Surface structure

Five tabs, switched by `1`-`5` / `Tab`/`Shift+Tab`; `r` refresh; `q` quit; `?` help; `:` command palette; `/` inline filter.

| Tab | Mirrors CLI group | Primary screens |
|-----|--------------------|-----------------|
| 1 Dashboard | `review dashboard`, `review queue`, `review followups`, `project guide status` | Coordinator overview; default landing view |
| 2 Projects | `project *`, `project label *`, `project guide *`, `project repo *` | Project list + project detail (facts/labels/guide/repos) |
| 3 Tasks | `task *`, `task link *`, `task todo/followup/discussion *`, `task timeline`, `task next/claim/unclaim` | Task list + task detail (context + timeline + entries) |
| 4 Actors | `actor list`, `actor show` | Actor list + actor detail |
| 5 Help | (none) | CLI/TUI parity table + keymap |

## Parity matrix (TUI action -> CLI command -> store function)

Every row is a round-trip guarantee: the TUI action and the CLI command call the same `store` function, produce the same persistent effect, and (for reads) render the same payload.

### Store / setup
| TUI | CLI | store function |
|-----|-----|----------------|
| Startup -> [I]nit | `atm init` | `store.Init` |
| Header -> store indicator | `atm store path` | (resolved path) |

### Projects
| TUI | CLI | store function |
|-----|-----|----------------|
| Tab2 list | `atm project list` | `store.Project.List` |
| Tab2 [a]dd | `atm project create` | `store.Project.Create` |
| Tab2 [e]/Enter detail | `atm project show` | `store.Project.Get` |
| Tab2 [N] set name | `atm project set-name` | `store.Project.SetName` |
| Tab2 [T] set type-axis | `atm project set-type-axis` | `store.Project.SetTypeAxis` |
| Tab2 [L] add label | `atm project label add` | `store.Project.LabelAdd` |
| Tab2 [l] remove label | `atm project label remove` | `store.Project.LabelRemove` |
| Tab2 [R]/[r] add/remove repo | `atm project repo add`/`remove` | `store.Project.RepoAdd`/`RepoRemove` |
| Tab2 [x] remove project | (new CLI equivalent) | `store.Project.Remove` (zero-task guard) |
| Tab2 Guide pane show | `atm project guide show` | `store.Guide.Get` |
| Tab2 [S] section add | `atm project guide section add` | `store.Guide.SectionAdd` |
| Tab2 [s] section rename | `atm project guide section rename` | `store.Guide.SectionRename` |
| Tab2 [X] section remove | `atm project guide section remove` | `store.Guide.SectionRemove` |
| Tab2 [M] section move | `atm project guide section move` | `store.Guide.SectionMove` |
| Tab2 [g] ref add | `atm project guide ref add` | `store.Guide.RefAdd` |
| Tab2 [m] ref move | `atm project guide ref move` | `store.Guide.RefMove` |
| Tab2 [d] ref remove | `atm project guide ref remove` | `store.Guide.RefRemove` |
| Tab2 [F] set freshness | `atm project guide set-freshness` | `store.Guide.SetFreshness` |
| Tab2 [D] guide status | `atm project guide status` | `store.Guide.Status` |

### Tasks
| TUI | CLI | store function |
|-----|-----|----------------|
| Tab3 list (with filters) | `atm task list` | `store.Query` (same filters + stable sort) |
| Tab3 [a]dd | `atm task create` | `store.Task.Create` |
| Tab3 [n]ext | `atm task next` | `store.Claim.Next` (claim=false) |
| Tab3 [c]laim | `atm task next --claim`/`claim` | `store.Claim.Next(claim=true)`/`Claim.Claim` |
| Tab3 [u]nclaim | `atm task unclaim` | `store.Claim.Unclaim` |
| Tab3 Enter detail | `atm task show --with-context` | `store.ShowWithContext` (includes guide) |
| Tab3 detail [s]tatus | `atm task set-status` | `store.Task.SetStatus` |
| Tab3 detail [e]dit title/desc | `atm task set-title`/`set-description` | `store.Task.SetTitle`/`SetDescription` |
| Tab3 detail [b] label add/remove | `atm task label add`/`remove` | `store.Task.LabelAdd`/`LabelRemove` |
| Tab3 detail [L] link add | `atm task link add` | `store.Link.Add` |
| Tab3 link remove | `atm task link remove` | `store.Link.Remove` |
| Tab3 link list (detail) | `atm task link list` | `store.Link.List` (out + computed in) |
| Tab3 detail [t] todo add | `atm task todo add` | `store.Entry.TodoAdd` |
| Tab3 detail Space toggle | `atm task todo toggle` | `store.Entry.TodoToggle` |
| Tab3 detail [o] followup add | `atm task followup add` | `store.Entry.FollowupAdd` |
| Tab3 detail [O] resolve | `atm task followup resolve` | `store.Entry.FollowupResolve` |
| Tab3 detail [d] discussion | `atm task discussion add` | `store.Entry.DiscussionAdd` |
| Tab3 detail TIMELINE | `atm task timeline` | `store.Entry.Timeline` |
| Tab3 detail [v] request review | `atm review request` | `store.Review.Request` |

### Review / dashboard
| TUI | CLI | store function |
|-----|-----|----------------|
| Tab1 queue | `atm review queue` | `store.Review.Queue` |
| Tab1 followups | `atm review followups` | `store.Review.OpenFollowups` |
| Tab1 [a]pprove | `atm review approve` | `store.Review.Approve` |
| Tab1 [r]eject | `atm review reject` | `store.Review.Reject` |
| Tab1 (whole view) | `atm review dashboard` | `store.Review.Dashboard` |
| Tab1 GUIDE STATUS | `atm project guide status` | `store.Guide.Status` |

### Actors
| TUI | CLI | store function |
|-----|-----|----------------|
| Tab4 list | `atm actor list` | `store.Actor.List` |
| Tab4 Enter | `atm actor show` | `store.Actor.Get` (with claimed/open-followups summary) |

## Behavioral guarantees

1. **Same payload**: screens render the same data the CLI emits for the same store + arguments (task list order == `task list --output json`; dashboard == `review dashboard --output json`; task detail == `task show --with-context --output json`). A snapshot test can drive both surfaces from one fixture.
2. **Same mutations**: every TUI action calls the same `store.*` mutation as the CLI, including history append + actor/timestamp (FR-011/FR-012). No TUI-only mutation path exists.
3. **Same error semantics**: forms surface the same stable error codes as inline messages: `3 not-found` (missing task/project), `4 conflict` (already-claimed, invalid status transition, duplicate project code, removed label assignment), `2 usage` (invalid format). No ad-hoc error text.
4. **Status-transition guard**: the set-status popup enables only the transitions allowed by the data-model matrix; invalid transitions are disabled with a hint (not silently rejected after submit).
5. **Soft-removal surfacing**: removing a label shows the `retained_usage` count inline (matches CLI JSON `retained_usage`).
6. **Stale-link surfacing**: a `blocks/implements/documents` link whose target is deleted is preserved with a `[STALE]` marker; a guide `kind:task` ref whose task is deleted is flagged `[MISS]`/`[STALE]` but not auto-removed (matches the spec's stale-link edge case and FR-018).
7. **No auto-refresh** in v1; `r` refreshes on demand (research R5 / constitution IV scope). Auto-refresh is deferred.
8. **Keyboard-first**: all primary actions have single-key bindings; forms are field-based Bubble Tea inputs. Mouse click-to-focus is a nicety, not a v1 requirement.
9. **No emojis** (constitution). Status/labels use text tokens (`open`, `type:bug`, `[STALE]`, `[EMPTY]`).

## Performance

SC-005: the Dashboard renders under 1s on a 1,000-task project. Because each screen is a single `store` read composed into widgets (no per-cell fetch), Task list and Task detail meet the same bound. The TUI does no background work, so refresh cost equals one store read.

## Out of scope for v1 TUI

- Auto-refresh / live updates (deferred; `r` is manual).
- Custom themes / color schemes (single readable theme; configuration deferred).
- A full store-management/settings tab (only the no-store startup prompt ships in v1).
- Cross-project task views (links are same-project for v1; cross-project views follow when links do).
- Mouse beyond basic click-to-focus.