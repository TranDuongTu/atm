# Tasks: Tasks Management System

**Input**: Design documents from `specs/001-tasks-management/`

**Prerequisites**: plan.md (required), spec.md (required for user stories), research.md, data-model.md, contracts/

**Tests**: Tests are included (the constitution's verification step requires `go build ./... && go test ./...`).

**Organization**: Tasks are grouped by user story (US1-US5) to enable independent implementation and testing of each story.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies)
- **[Story]**: Which user story this task belongs to (e.g. US1, US2)
- Include exact file paths in descriptions

## Path Conventions

- Single Go module at repo root; binary at `cmd/atm/main.go`.
- Implementation under `internal/store`, `internal/cli`, `internal/tui`.
- Tests alongside packages; golden fixtures under `testdata/`.

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: Project initialization and basic structure

- [ ] T001 [P] Initialize Go module `atm` with Go 1.22 in `go.mod` at repo root; add `cmd/atm/main.go` stub that prints version and exits 0.
- [ ] T002 [P] Add dependencies in `go.mod`: `github.com/spf13/cobra`, `github.com/charmbracelet/bubbletea`, `github.com/charmbracelet/lipgloss`, `golang.org/x/sys`; run `go mod tidy`.
- [ ] T003 [P] Configure linting/formatting: add a `.golangci.yml` with `gofmt`/`govet`/`gosimple`/`unused`; document `go build ./... && go test ./...` as the verify command in `README.md`.
- [ ] T004 [P] Create `internal/store/store.go` with the `Store` type (root path, open/close, `.atm` path resolution walking up from CWD), and an `Init(storePath string) error` that creates `.atm/{projects,projects/},actors.json` idempotently. Unit test in `internal/store/store_test.go`.

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: Core infrastructure that MUST be complete before ANY user story can be implemented.

- [ ] T005 [P] Implement per-project file locking in `internal/store/lock.go`: an exclusive `flock` on `.atm/projects/<CODE>.lock` held for a `WithLock(code, fn)` callback scope. Use `golang.org/x/sys/unix.Flock` on darwin/linux. Unit test: two goroutines racing for a lock; second blocks then proceeds.
- [ ] T006 [P] Implement deterministic JSON I/O helpers in `internal/store/json.go`: `MarshalSorted` (sorted object keys, stable 2-space indent), `WriteFileAtomic` (write temp + rename), `ReadJSON`. Unit test: same input -> byte-identical output (snapshot).
- [ ] T007 [P] Implement the Actor model in `internal/store/actor.go`: lazy registration in `actors.json` on first mutation; `Register(id, name)` and `List()`. Validate id format `^(agent|human):[A-Za-z0-9._-]+$`. Unit test.
- [ ] T008 Implement the Project model in `internal/store/project.go` per data-model: `Create(code,name,typeAxis,labels)`, `Get(code)`, `List()`, `SetTypeAxis`, `LabelAdd/Remove/List` with soft-removal + `retained_usage` reporting. Validate `code` regex and uniqueness. Atomic under project lock. Unit test covers create, duplicate-rejected, soft-removal warning.
- [ ] T009 Implement the Task model in `internal/store/task.go` per data-model: `Create(projectCode, title, description, labels)` assigning `<CODE>-<N>` from `next_task_n` under project lock; `Get(id)`; `SetTitle/SetDescription`; `SetStatus` enforcing the status transition matrix; `LabelAdd/LabelRemove`. Append a `HistoryEntry` on every mutation. Unit test covers id assignment, transition rejections, history growth.
- [ ] T010 [P] Implement the Link model in `internal/store/link.go`: `Add(id, type, target)`, `Remove`, `List(id)` returning stored `out` edges plus computed `in` edges (including implied `blocked-by`). Validate `type` enum and that `target` exists (else preserve + warning). `related-to` dedup symmetric. Unit test.
- [ ] T011 Implement the embedded entries in `internal/store/entry.go`: `TodoAdd/TodoToggle`, `FollowupAdd/FollowupResolve`, `DiscussionAdd`, `Timeline(id)` merging todos+followups+discussions+history by timestamp then entry id. Per-task monotonic counters for `t<n>/f<n>/d<n>/h<n>`. Unit test.
- [ ] T012 [P] Implement `internal/store/query.go`: `List(filters{project, labels, status, assignee, claimant})` with label AND-intersection, stable sort by id (project-then-numeric). Unit test with a fixture store.

**Checkpoint**: Foundation ready - user story implementation can now begin in parallel.

---

## Phase 3: User Story 1 - Agent queries next task and context (Priority: P1) MVP

**Goal**: An agent can run `atm task next`, claim the returned task, and `atm task show --with-context` to retrieve linked tasks + matching convention docs + timeline.

**Independent Test**: `quickstart.md` Scenario 1 passes end-to-end against a temp store.

### Tests for User Story 1

- [ ] T013 [P] [US1] Golden test in `internal/cli/task_test.go` for `task next` (empty result returns `{"task": null}`) and `task next --claim` (returns claimed task; second call returns the next or null).
- [ ] T014 [P] [US1] Golden test for `task show --with-context` asserting `context.conventions` contains the label-matched convention doc and `context.timeline` is sorted.

### Implementation for User Story 1

- [ ] T015 [P] [US1] Implement `internal/store/claim.go`: `Next(projectCode, claim bool, actor)` returns the next claimable, non-blocked, non-claimed, non-done task under the project lock; ordering = blocked-by count ascending then `created_at` ascending; if `claim`, set `claim` and append history. Returns `nil, nil` when none claimable. Unit test: two goroutines race `Next(claim=true)`; assert different tasks or one gets null.
- [ ] T016 [P] [US1] Implement `internal/store/context.go`: `ShowWithContext(id)` returning the task plus `links_out`/`links_in`, `conventions` (tasks with label `kind:convention` whose labels intersect the task's labels, weighted by type-axis match first), and `timeline`. Unit test against a fixture store.
- [ ] T017 [US1] Implement CLI subcommands in `internal/cli/task.go`, `internal/cli/workflow.go`, `internal/cli/output.go`: `task next`, `task claim`, `task unclaim`, `task show --with-context`, `task list`, `task create`, `task set-status`, `task label add/remove`, `task link add/remove/list`. Wire to `store` with deterministic JSON/text output and stable exit codes (3 not-found, 4 conflict). Integration test runs the compiled binary per `quickstart.md` Scenario 1.

**Checkpoint**: US1 fully functional and independently testable.

---

## Phase 4: User Story 2 - Human manages projects and labels (Priority: P2)

**Goal**: A human can create a project, configure its labels, declare the type axis, and soft-remove labels.

**Independent Test**: `quickstart.md` Scenario 2 passes.

### Tests for User Story 2

- [ ] T018 [P] [US2] Golden test for `project create` rejecting a duplicate code (exit 4) and `project label remove` reporting `retained_usage` in JSON.

### Implementation for User Story 2

- [ ] T019 [P] [US2] Implement CLI subcommands in `internal/cli/project.go`: `project create/list/show/set-type-axis`, `project label add/remove/list`. Output includes `retained_usage` for soft removal. Integration test per Scenario 2.

**Checkpoint**: US1 and US2 both work independently.

---

## Phase 5: User Story 3 - Create tasks and organize via labels and links (Priority: P3)

**Goal**: Tasks are created with `<CODE>-<NNNN>` ids, labeled, and linked (blocks/implements/documents/related-to) for hierarchy and context.

**Independent Test**: `quickstart.md` Scenario 3 passes (epic + impl link traversable both ways).

### Tests for User Story 3

- [ ] T020 [P] [US3] Golden test for id assignment ordering across creates and for `link list` returning both `out` and computed `in` edges.

### Implementation for User Story 3

- [ ] T021 [US3] This is largely covered by T009/T010/T017 (task create/label/link already implemented). Add an integration test in `internal/cli/task_test.go` per Scenario 3 verifying the `implements` link is traversable from both endpoints and that `blocks` excludes the target from `task next`.

**Checkpoint**: US3 fully functional (authoring side complete).

---

## Phase 6: User Story 4 - Todos, followups, discussions (Priority: P4)

**Goal**: Any task carries todos/followups/discussions with full actor provenance; `task timeline` merges them chronologically.

**Independent Test**: `quickstart.md` Scenario 4 passes.

### Tests for User Story 4

- [ ] T022 [P] [US4] Golden test for `task timeline` ordering (history + todo + followup + discussion interleaved by timestamp) and for `followup resolve` setting `resolved_at`/`resolved_by`.

### Implementation for User Story 4

- [ ] T023 [US4] Implement CLI subcommands in `internal/cli/entry.go`: `task todo add/toggle`, `task followup add/resolve`, `task discussion add`, `task timeline`. Integration test per Scenario 4.

**Checkpoint**: US4 fully functional.

---

## Phase 7: User Story 5 - Human coordinator review (Priority: P5)

**Goal**: The human coordinator sees claimed tasks and the review queue, and can approve/reject with a comment.

**Independent Test**: `quickstart.md` Scenario 5 passes.

### Tests for User Story 5

- [ ] T024 [P] [US5] Golden test for `review queue` grouped by claimant and for `review approve`/`reject` transitioning status and recording the comment as a discussion entry by the human.

### Implementation for User Story 5

- [ ] T025 [P] [US5] Implement `internal/store/review.go`: `Request(id, actor)` (status -> review), `Approve(id, actor, comment)` (-> done), `Reject(id, actor, comment)` (-> in-progress/open, comment appended as discussion), `Queue(projectCode)` grouped by claimant. Unit test.
- [ ] T026 [US5] Implement CLI subcommands in `internal/cli/review.go`: `review request/approve/reject/queue/followups`. Integration test per Scenario 5.

**Checkpoint**: US5 fully functional; agent<->human coordination loop closes.

---

## Phase 8: TUI (Thin Client)

**Purpose**: Bubble Tea front-end over `store` for the human coordinator.

- [ ] T027 [P] Implement `internal/tui/app.go`: root Bubble Tea model with alt-screen setup and a top-level menu (Coordinator / Projects / Tasks / Quit).
- [ ] T028 [P] Implement `internal/tui/coordinator.go`: the coordinator view rendering `review queue` + open followups + claimed-by-agent groups (calls `store.Review.Queue` + `store.Query`). Updates on a keypress (no auto-refresh in v1).
- [ ] T029 [P] Implement `internal/tui/components/` list and detail widgets for browsing projects/tasks and showing a task's context view (reusing `store.ShowWithContext`).
- [ ] T030 [US5] Wire the `tui` subcommand in `cmd/atm/main.go` (`atm tui` launches the Bubble Tea program). Manual smoke test; ensure `go build ./...` succeeds.

**Checkpoint**: TUI renders the coordinator view under 1s on a 1,000-task fixture (SC-005).

---

## Phase 9: Polish & Cross-Cutting Concerns

**Purpose**: Improvements that affect multiple user stories.

- [ ] T031 [P] Add a determinism snapshot suite: commit `testdata/golden/*.json` for every CLI command in `quickstart.md`; a test in `internal/cli` runs each and diffs byte-for-byte (SC-002a).
- [ ] T032 [P] Document the CLI in `README.md` with the command groups from `contracts/cli.md` and the verify command (`go build ./... && go test ./...`).
- [ ] T033 Run `golangci-lint run` and `gofmt -l .`; fix any findings.
- [ ] T034 Run `go build ./... && go test ./...` from repo root; ensure green. Tag the commit `atm-v0.1.0`.
- [ ] T035 [P] Dogfood: create project `ATM` in the repo's own `.atm/` store and register this feature's follow-on tasks there (bootstrap the dogfooding loop).

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: No dependencies - start immediately. T001-T004 are all `[P]`.
- **Foundational (Phase 2)**: Depends on Setup. T008/T009 depend on T005-T007; T010-T012 are `[P]` given T006. **BLOCKS all user stories.**
- **User Stories (Phase 3-7)**: All depend on Foundational completion.
  - US1 (Phase 3) is the MVP; implement first.
  - US2 (Phase 4) depends only on Foundational (independent of US1's CLI; reuses Project store).
  - US3 (Phase 5) reuses US1's task/label/link CLI (T017); add integration tests only.
  - US4 (Phase 6) reuses Foundational `entry.go`; needs US1's CLI wiring pattern.
  - US5 (Phase 7) depends on US4 (comment-as-discussion) and US1 (claim/next).
- **TUI (Phase 8)**: depends on US1-US5 store APIs being present.
- **Polish (Phase 9)**: depends on all prior phases.

### Within Each User Story

- Store layer before CLI layer.
- Unit tests before integration tests.
- Tests MUST fail before implementation (write test, see it fail, then implement).

### Parallel Opportunities

- All Setup tasks marked `[P]` run in parallel.
- Foundational `[P]` tasks (T005-T007, T010-T012) run in parallel once their few prerequisites are done.
- US2 (T019) can be implemented in parallel with US1's CLI (T017) since they touch different files.
- TUI components (T027-T029) are `[P]` with each other.

---

## Implementation Strategy

### MVP First (User Story 1 Only)

1. Complete Phase 1: Setup.
2. Complete Phase 2: Foundational (CRITICAL - blocks all stories).
3. Complete Phase 3: User Story 1 (next/claim/show-with-context).
4. **STOP and VALIDATE**: run `quickstart.md` Scenario 1 + `go build ./... && go test ./...`.
5. The system is already useful to a single agent at this point.

### Incremental Delivery

1. Setup + Foundational -> foundation ready.
2. + US1 -> MVP (agent self-serve).
3. + US2 -> humans can configure projects.
4. + US3 -> authoring side complete (links/hierarchy).
5. + US4 -> coordination primitives (todos/followups/discussions).
6. + US5 -> human coordinator review loop.
7. + TUI -> human-facing front-end.
8. + Polish -> determinism suite, docs, dogfooding.

---

## Notes

- `[P]` tasks = different files, no dependencies.
- `[Story]` label maps a task to its user story for traceability.
- Each user story is independently completable and testable.
- Verify tests fail before implementing (constitution principle: verify before declaring done).
- Commit after each task or logical group; no emojis in commits.
- Stop at any checkpoint to validate the story independently with `go build ./... && go test ./...`.