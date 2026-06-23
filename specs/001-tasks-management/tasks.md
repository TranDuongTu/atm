# Tasks: Tasks Management System

**Input**: Design documents from `specs/001-tasks-management/`

**Prerequisites**: plan.md (required), spec.md (required for user stories), research.md, data-model.md, contracts/cli.md, contracts/tui.md, tui-mockups.md

**Tests**: Tests are included (the constitution's verification step requires `make verify`, i.e. `make build && make test`).

**Organization**: Tasks are grouped by user story (US1-US5) to enable independent implementation and testing of each story. The Guide entity (FR-016/017/018, v1.1.0) is folded into US2 (editing) and US5 (dashboard) per the clarifications session, with its always-read inclusion wired into US1's `next`/`show --with-context`.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies on incomplete tasks)
- **[Story]**: Which user story this task belongs to (e.g. US1, US2). Setup/Foundational/TUI/Polish phases carry no story label.
- Include exact file paths in descriptions

## Path Conventions

- Single Go module at repo root; binary entrypoint at `cmd/atm/main.go`.
- Implementation under `internal/store` (stable in-process API), `internal/cli` (stable out-of-process API), `internal/tui` (thin client over store).
- Tests alongside packages; golden fixtures under `testdata/golden/`, fixture stores under `testdata/stores/`.
- Store resolution: `--store` flag > `ATM_HOME` env > `~/.config/atm` default. NO walk-up-from-CWD (research R10).

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: Project initialization and basic structure.

- [X] T001 [P] Initialize Go module `atm` with Go 1.22 in `go.mod` at repo root; add `cmd/atm/main.go` stub that prints `atm version dev` and exits 0.
- [X] T002 [P] Add dependencies in `go.mod`: `github.com/spf13/cobra`, `github.com/charmbracelet/bubbletea`, `github.com/charmbracelet/lipgloss`, `golang.org/x/sys`; run `go mod tidy`.
- [X] T003 [P] Create `Makefile` with `build` (`go build -o bin/atm ./cmd/atm`), `test` (`go test ./...`), `lint` (`golangci-lint run`), `verify` (`make build && make test`), and `clean` targets; outputs go to gitignored `bin/`. Add `.gitignore` entries for `bin/` and `*.test`.
- [X] T004 [P] Add `.golangci.yml` enabling `gofmt`/`govet`/`gosimple`/`unused`; document the verify command (`make verify`) in `README.md`.

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: Core `internal/store` infrastructure that MUST be complete before ANY user story CLI can be implemented.

**CRITICAL**: No user story work can begin until this phase is complete.

- [X] T005 [P] Implement `internal/store/store.go`: `Store` type (root path, open/close), `$ATM_HOME` resolution (`--store` flag > `ATM_HOME` env > `~/.config/atm`, NO walk-up-from-CWD per R10), `Init(storePath string) error` creating `projects/` and `actors.json` idempotently, `StorePath() string`. Unit test in `internal/store/store_test.go` covers resolution order and idempotent init.
- [X] T006 [P] Implement `internal/store/lock.go`: `WithLock(code string, fn func() error) error` taking an exclusive `flock` on `$ATM_HOME/projects/<CODE>.lock` for the callback scope via `golang.org/x/sys/unix.Flock` (darwin/linux). Unit test: two goroutines race for the lock; second blocks then proceeds.
- [X] T007 [P] Implement `internal/store/json.go`: `MarshalSorted` (object keys sorted lexicographically, stable 2-space indent, RFC 3339 UTC timestamps), `WriteFileAtomic` (write temp + `os.Rename`), `ReadJSON`. Unit test: same input -> byte-identical output across runs (SC-002a).
- [X] T008 [P] Implement `internal/store/actor.go`: lazy registration in `actors.json` on first mutation; `Register(id, name string) error` and `List() []Actor` and `Get(id string)`. Validate id format `^(agent|human):[A-Za-z0-9._-]+$`. Unit test covers lazy registration and invalid id rejection.
- [X] T009 Implement `internal/store/project.go` per data-model: `Create(code, name, typeAxis, labels, repoPaths, actor)`, `Get(code)`, `List()`, `SetName`, `SetTypeAxis` (namespace must have >=1 label in set), `RepoAdd`/`RepoRemove`, `LabelAdd`/`LabelRemove` (soft removal; report `retained_usage`), `LabelList`. Validate `code` regex `^[A-Z][A-Z0-9-]{1,15}$` and uniqueness; atomic under project lock; project-level history append on guide edits is added in T022. Unit test covers create, duplicate-rejected (exit 4), soft-removal `retained_usage`, type-axis validation.
- [X] T010 Implement `internal/store/task.go` per data-model: `Create(projectCode, title, description, labels, actor)` assigning `<CODE>-<N>` from `next_task_n` under project lock (render with >=4 digits up to 9999, then natural width), `Get(id)`, `SetTitle`/`SetDescription`, `SetStatus` enforcing the transition matrix, `LabelAdd`/`LabelRemove`. Append a `HistoryEntry` on every mutation; per-task monotonic counters `t<n>/f<n>/d<n>/h<n>`. Unit test covers id assignment + widening, transition rejections, history growth.
- [X] T011 [P] Implement `internal/store/link.go`: `Add(id, linkType, target, actor)`, `Remove`, `List(id)` returning stored `out` edges plus computed `in` edges (including implied `blocked-by` from `blocks`). Validate `type` enum (`blocks`/`related-to`/`implements`/`documents`); preserve+warn on stale target (deleted task); `related-to` dedup symmetric. Unit test.
- [X] T012 Implement `internal/store/entry.go`: `TodoAdd`/`TodoToggle`, `FollowupAdd`/`FollowupResolve`, `DiscussionAdd`, `Timeline(id)` merging todos+followups+discussions+history by timestamp ascending then entry id. Unit test.
- [X] T013 [P] Implement `internal/store/query.go`: `List(filters{project, labels AND-intersect, status, assignee, claimant})` with stable sort by id (project-then-numeric). Unit test with a fixture store.
- [X] T014 [P] Implement `internal/cli/errors.go`: stable error codes + exit codes (`0` success, `1` generic, `2` usage, `3` not-found, `4` conflict) and a JSON error envelope `{"error":{"code":"...","message":"..."}}` for stderr in JSON mode. Foundational for all CLI subcommands.

**Checkpoint**: Foundation ready - user story implementation can now begin in parallel.

---

## Phase 3: User Story 1 - Agent queries next task and context (Priority: P1) MVP

**Goal**: An agent can run `atm task next [--claim]`, `atm task claim/unclaim`, and `atm task show --with-context` to retrieve the next claimable task with its full context (linked tasks, matching convention docs, timeline, and the project guide harness).

**Independent Test**: `quickstart.md` Scenario 1 passes end-to-end against a temp store.

### Tests for User Story 1

- [X] T015 [P] [US1] Golden test in `internal/cli/task_test.go` for `task next` empty result (`{"task": null, "guide": null}`) and `task next --claim` returning a claimed task with a second call returning the next or null.
- [X] T016 [P] [US1] Golden test for `task show --with-context` asserting `context.conventions` contains the label-matched convention doc (type-axis match first), `context.timeline` is sorted by timestamp, and `context.guide` is `null` when the project has no guide.

### Implementation for User Story 1

- [X] T017 [P] [US1] Implement `internal/store/claim.go`: `Next(projectCode string, claim bool, actor string) (*Task, *Guide, error)` returns the next claimable, non-blocked, non-claimed, non-done task under the project lock; ordering = blocked-by count ascending, then `created_at` ascending (oldest first); if `claim`, set `claim` and append `claimed` history; returns `nil, guide, nil` when none claimable. The response always includes the project's guide (FR-017) so an idling agent still sees the harness. Unit test: two goroutines race `Next(claim=true)`; assert different tasks or one gets nil.
- [X] T018 [P] [US1] Implement `internal/store/context.go`: `ShowWithContext(id string)` returning the task plus `links_out`/`links_in` (via `store.Link.List`), `conventions` (tasks with label `kind:convention` whose labels intersect the task's labels, type-axis matches first), `timeline` (via `store.Entry.Timeline`), and `guide` (project's guide or `null`). Unit test against a fixture store.
- [X] T019 [US1] Implement CLI in `internal/cli/root.go` (cobra root + global flags `--store`/`--output json|text`/`--actor`/`--quiet` + `ATM_ACTOR` env), `internal/cli/output.go` (deterministic JSON/text renderers), `internal/cli/task.go` (`task create/show/list/set-status/set-title/set-description/label add/remove`), `internal/cli/workflow.go` (`task next [--claim]`/`claim`/`unclaim`). Wire to `store` with stable exit codes (3 not-found, 4 conflict). Integration test runs the compiled binary per `quickstart.md` Scenario 1.

**Checkpoint**: US1 fully functional and independently testable.

---

## Phase 4: User Story 2 - Human manages projects, labels, repo paths, and the project guide (Priority: P2)

**Goal**: A human can create a project, configure its labels, declare the type axis, add/remove repo paths, soft-remove labels, and edit the project guide (sections/refs/freshness) per FR-016/018.

**Independent Test**: `quickstart.md` Scenarios 2 and 6 pass.

### Tests for User Story 2

- [X] T020 [P] [US2] Golden test for `project create` rejecting a duplicate code (exit 4), `project label remove` reporting `retained_usage` in JSON, and `project set-type-axis` rejecting a namespace with no labels in the set.
- [X] T021 [P] [US2] Golden test for `project guide section add/rename/remove/move`, `project guide ref add/remove/move`, `project guide set-freshness`, and `project guide status` coverage + freshness output (including `stale`/`missing`/`unknown` states per R11).

### Implementation for User Story 2

- [X] T022 [P] [US2] Implement `internal/store/guide.go`: `Guide` as the `guide` field on the Project record (read/written under the project lock). `SectionAdd`/`SectionRename`/`SectionRemove`/`SectionMove` (reorder), `RefAdd`/`RefRemove`/`RefMove` (reorder within section), `SetFreshnessThreshold` (duration string or unset), `Get(code)` returning the whole guide, `Status(code)` returning coverage (empty sections + counts) and freshness (per `kind:task` ref: `fresh`/`stale`/`missing`/`unknown` when threshold unset; per `kind:file` ref: `present`/`missing`). Validate section name uniqueness, task `target` existence in the same project (preserve + flag missing, do NOT auto-remove), absolute file paths. Every edit sets `guide.updated_at`/`updated_by` and appends a `guide-updated` entry to the project-level history. Unit test.
- [X] T023 [US2] Implement CLI in `internal/cli/project.go` (`project create/list/show/set-name/set-type-axis/repo add/repo remove`, `project label add/remove/list`) and `internal/cli/project_guide.go` (`project guide show`, `project guide section add/rename/remove/move`, `project guide ref add/remove/move`, `project guide set-freshness`, `project guide status`). Output includes `retained_usage` for soft removal and `guide: null` when none. Integration test per `quickstart.md` Scenarios 2 and 6.

**Checkpoint**: US1 and US2 both work independently; the guide is editable and returned in `next`/`show --with-context`.

---

## Phase 5: User Story 3 - Create tasks and organize via labels and links (Priority: P3)

**Goal**: Tasks are created with `<CODE>-<NNNN>` ids, labeled, and linked (`blocks`/`implements`/`documents`/`related-to`) for hierarchy and context. Links are traversable both ways; `blocks` excludes the target from `next`.

**Independent Test**: `quickstart.md` Scenario 3 passes (epic + impl link traversable from both endpoints; `blocks` excludes target from `next`).

### Tests for User Story 3

- [X] T024 [P] [US3] Golden test for id assignment ordering across creates and for `task link list` returning both `out` and computed `in` edges (including implied `blocked-by`).

### Implementation for User Story 3

- [X] T025 [US3] Implement CLI in `internal/cli/link.go`: `task link add/remove/list` wired to `store.Link` (T011). `link list` returns both stored `out` edges and computed `in` edges tagged with `direction: out|in`. Integration test in `internal/cli/task_test.go` per `quickstart.md` Scenario 3 verifying the `implements` link is traversable from both endpoints and that `blocks` excludes the target from `task next`.

**Checkpoint**: US3 fully functional; the authoring side is complete.

---

## Phase 6: User Story 4 - Todos, followups, discussions (Priority: P4)

**Goal**: Any task carries todos/followups/discussions with full actor provenance; `task timeline` merges them chronologically with history.

**Independent Test**: `quickstart.md` Scenario 4 passes.

### Tests for User Story 4

- [X] T026 [P] [US4] Golden test for `task timeline` ordering (history + todo + followup + discussion interleaved by timestamp then entry id) and for `followup resolve` setting `resolved_at`/`resolved_by`.

### Implementation for User Story 4

- [X] T027 [US4] Implement CLI in `internal/cli/entry.go`: `task todo add/toggle`, `task followup add/resolve`, `task discussion add`, `task timeline` wired to `store.Entry` (T012). Integration test per `quickstart.md` Scenario 4.

**Checkpoint**: US4 fully functional; coordination primitives are in place.

---

## Phase 7: User Story 5 - Human coordinator reviews agent activity and steers work (Priority: P5)

**Goal**: The human coordinator sees claimed tasks grouped by claimant, open followups, the review queue, and the project guide status on a single dashboard, and can approve/reject with a comment. The dashboard composes `review queue` + `open followups` + `guide status` (FR-010/FR-018).

**Independent Test**: `quickstart.md` Scenario 5 passes (dashboard includes `guide_status`).

### Tests for User Story 5

- [X] T028 [P] [US5] Golden test for `review queue` grouped by claimant and for `review approve`/`reject` transitioning status and recording the comment as a discussion entry by the human.

### Implementation for User Story 5

- [X] T029 [P] [US5] Implement `internal/store/review.go`: `Request(id, actor)` (status -> `review`), `Approve(id, actor, comment)` (-> `done`), `Reject(id, actor, comment)` (-> `in-progress` or `open`; comment appended as a discussion entry), `Queue(projectCode)` grouped by claimant, `OpenFollowups(projectCode)`, `Dashboard(projectCode)` composing `queue + open_followups + guide_status` (calls `store.Guide.Status` from T022). Unit test.
- [X] T030 [US5] Implement CLI in `internal/cli/review.go`: `review request/approve/reject/queue/followups/dashboard` and `internal/cli/actor.go`: `actor list/show`. Integration test per `quickstart.md` Scenario 5; verify the dashboard payload matches the contract in `contracts/cli.md`.

**Checkpoint**: US5 fully functional; the agent<->human coordination loop closes.

---

## Phase 8: TUI (Thin Client, 5 tabs per contracts/tui.md)

**Purpose**: Bubble Tea front-end over `internal/store` mirroring every CLI command group (FR-002). Five tabs: Dashboard / Projects / Tasks / Actors / Help. Screen mockups and keymaps in `tui-mockups.md`; behavioral guarantees in `contracts/tui.md`.

- [X] T031 [P] Implement `internal/tui/app.go`: root Bubble Tea model with alt-screen setup, 5-tab tab bar (`1`-`5` / `Tab`/`Shift+Tab`), header (store indicator from `store.StorePath` + actor), footer, global keymap (`r` refresh, `q` quit, `?` help, `:` palette, `/` inline filter). Startup `[I]nit` prompt if the store is missing/empty (calls `store.Init`); prompt for `--actor`/`ATM_ACTOR` once per session if unset (mutating actions disabled until set).
- [X] T032 [P] Implement `internal/tui/keymap.go`: global + per-view keybindings from `tui-mockups.md`.
- [X] T033 [P] Implement `internal/tui/dashboard.go`: Tab 1 rendering `store.Review.Dashboard` (review queue + open followups + guide status). Actions: `[a]` approve form, `[r]` reject form, `[R]` resolve followup. Markers `[STALE]`/`[MISS]`/`[EMPTY]` for guide freshness/coverage. No auto-refresh; `r` refreshes on demand.
- [X] T034 [P] Implement `internal/tui/projects.go`: Tab 2 project list + project detail (facts/labels/guide/repos). Actions: `[a]` create, `[e]`/Enter detail, `[N]` set name, `[T]` set type-axis, `[L]`/`[l]` add/remove label (toast shows `retained_usage`), `[R]`/`[r]` add/remove repo, `[x]` remove project (zero-task guard via `store.Project.Remove`).
- [X] T035 [P] Implement `internal/tui/guide.go`: Tab 2 guide pane editing sections/refs/freshness. Actions: `[S]` section add, `[s]` section rename, `[X]` section remove, `[M]` section move, `[g]` ref add, `[m]` ref move, `[d]` ref remove, `[F]` set freshness, `[D]` jump to Dashboard scoped to the project. Stale-link/missing-ref markers per behavioral guarantee 6.
- [X] T036 [P] Implement `internal/tui/tasks.go`: Tab 3 task list (filters: project/labels/status/assignee/claimant) + task detail rendering `store.ShowWithContext` (guide + matching conventions + timeline + entries). Actions: `[a]` create, `[n]` next, `[c]` claim, `[u]` unclaim, `[s]` set-status (transition-guarded popup: invalid transitions disabled with a hint), `[e]` edit title/description, `[b]` label add/remove, `[L]` link add, link remove, `[t]` todo add, Space todo toggle, `[o]` followup add, `[O]` followup resolve, `[d]` discussion add, `[v]` request review. On-screen order matches `task list --output json` for the same filters (FR-002).
- [X] T037 [P] Implement `internal/tui/actors.go`: Tab 4 actor list + actor detail (claimed tasks + open followups summary via `store.Actor.Get`).
- [X] T038 [P] Implement `internal/tui/help.go`: Tab 5 CLI/TUI parity table (from `contracts/tui.md`) + keymap.
- [X] T039 [P] Implement `internal/tui/form.go`: reusable field-based Bubble Tea input forms for create/edit/ref actions.
- [X] T040 [P] Implement `internal/tui/components/`: list, detail, filter, toast, overlay widgets.
- [X] T041 Wire the `tui` subcommand in `cmd/atm/main.go` (`atm tui` launches the Bubble Tea program with `--store`/`--actor` passthrough). Verify `make build` succeeds and `atm tui --store <tmp> --actor human:alice` renders the 5 tabs; manual smoke against the `quickstart.md` TUI scenarios.

**Checkpoint**: TUI renders the Dashboard under 1s on a 1,000-task fixture (SC-005); TUI/CLI parity holds for the same store + filters (FR-002).

---

## Phase 9: Polish & Cross-Cutting Concerns

**Purpose**: Improvements that affect multiple user stories and finalize the release.

- [ ] T042 [P] Add a determinism + detachability snapshot suite in `internal/cli/determinism_test.go`: commit `testdata/golden/*.json` for every CLI command in `quickstart.md`; the test runs each command twice and diffs byte-for-byte (SC-002a), and copies the store wholesale to a temp dir and diffs output (SC-004/FR-001).
- [ ] T043 [P] Document the CLI in `README.md` with the command groups from `contracts/cli.md`, a TUI section pointing to `tui-mockups.md` + `contracts/tui.md`, the store resolution rule, and the verify command (`make verify`).
- [ ] T044 Run `make lint` (`golangci-lint run`) and `gofmt -l .`; fix any findings.
- [ ] T045 Run `make verify` from repo root; ensure green. Tag the commit `atm-v0.1.0`.
- [ ] T046 [P] Dogfood: create project `ATM` in the machine-global store and register this feature's follow-on tasks there (bootstrap the dogfooding loop).

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: No dependencies - start immediately. T001-T004 are all `[P]`.
- **Foundational (Phase 2)**: Depends on Setup. T009/T010 depend on T005-T008; T011-T014 are `[P]` once T007 is done. **BLOCKS all user stories.**
- **User Stories (Phase 3-7)**: All depend on Foundational completion.
  - US1 (Phase 3) is the MVP; implement first.
  - US2 (Phase 4) depends only on Foundational; reuses `store.Project` (T009) and adds `store.Guide` (T022). Independent of US1's CLI.
  - US3 (Phase 5) reuses US1's task/label CLI (T019) and `store.Link` (T011); adds the `link` CLI (T025) and integration tests.
  - US4 (Phase 6) reuses Foundational `store.Entry` (T012); needs US1's CLI wiring pattern (T019).
  - US5 (Phase 7) depends on US4 (comment-as-discussion via `store.Entry.DiscussionAdd`) and US1 (`store.Claim`), and consumes US2's `store.Guide.Status` (T022) in the dashboard.
- **TUI (Phase 8)**: depends on US1-US5 store APIs being present (TUI calls the same `store.*` functions per `contracts/tui.md`).
- **Polish (Phase 9)**: depends on all prior phases.

### Within Each User Story

- Store layer before CLI layer.
- Unit tests before integration tests.
- Tests MUST fail before implementation (write test, see it fail, then implement).

### Parallel Opportunities

- All Setup tasks marked `[P]` run in parallel.
- Foundational `[P]` tasks (T005-T008, T011, T013, T014) run in parallel once their few prerequisites are done.
- US2's guide store (T022) can be implemented in parallel with US1's CLI (T019) since they touch different files.
- TUI tabs (T031-T040) are `[P]` with each other once the store APIs they call exist.

---

## Parallel Example: User Story 1

```bash
# Launch US1 tests together:
Task: "T015 Golden test for task next + claim in internal/cli/task_test.go"
Task: "T016 Golden test for task show --with-context in internal/cli/task_test.go"

# Launch US1 store-layer tasks together:
Task: "T017 internal/store/claim.go: Next(claim) under project lock"
Task: "T018 internal/store/context.go: ShowWithContext"
```

---

## Implementation Strategy

### MVP First (User Story 1 Only)

1. Complete Phase 1: Setup.
2. Complete Phase 2: Foundational (CRITICAL - blocks all stories).
3. Complete Phase 3: User Story 1 (`next`/`claim`/`show --with-context` with `guide: null` placeholder).
4. **STOP and VALIDATE**: run `quickstart.md` Scenario 1 + `make verify`.
5. The system is already useful to a single agent at this point.

### Incremental Delivery

1. Setup + Foundational -> foundation ready.
2. + US1 -> MVP (agent self-serve).
3. + US2 -> humans can configure projects + edit the guide; US1's `next`/`show` now return the guide.
4. + US3 -> authoring side complete (links/hierarchy).
5. + US4 -> coordination primitives (todos/followups/discussions).
6. + US5 -> human coordinator review loop with dashboard (incl. guide status).
7. + TUI -> human-facing front-end mirroring every CLI op.
8. + Polish -> determinism suite, docs, dogfooding.

### Parallel Team Strategy

With multiple developers:

1. Team completes Setup + Foundational together.
2. Once Foundational is done:
   - Developer A: US1 (next/claim/context).
   - Developer B: US2 (project + guide editing).
   - Developer C: TUI scaffolding (app/keymap/components).
3. US3/US4/US5 follow in priority order; the TUI tabs fill in as their backing store APIs land.

---

## Notes

- `[P]` tasks = different files, no dependencies on incomplete tasks.
- `[Story]` label maps a task to its user story for traceability.
- Each user story is independently completable and testable.
- Verify tests fail before implementing (constitution principle: verify before declaring done).
- Commit after each task or logical group; no emojis in code, data, or commits.
- Stop at any checkpoint to validate the story independently with `make verify`.
- Avoid: vague tasks, same-file conflicts, cross-story dependencies that break independence.