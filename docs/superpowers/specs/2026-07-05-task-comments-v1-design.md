# Task Comments v1 — Design Spec

**Status:** Approved
**Approach:** Approach A — Comments as a first-class additive entity (purely additive; no migration, no breaking change to v2 / audit-log-v1).
**Parent specs:** `2026-07-02-tasks-management-v2-design.md`, `2026-07-04-audit-log-redesign-design.md`.

## Driver

ATM is built for agents to increase their visibility and enhance their context.
An agent tracks a feature, bug, or design as a Task; today the only narrative
record on a task is the machine-generated, immutable history log
(`task.*`/`label.*` events surfaced via `atm store log` and the TUI history
overlay). History records *what happened and when*, not *why* or *what the
agent decided*, and it is not a channel a human or agent can write prose into.

Agents need to narrate progress on a task: a clarification, an implementation
PR/commit reference, a bug detected by QA, an open question from a manager, a
pointer to a design doc/plan/spec. Today they have nowhere to put that prose
except the task description (a single field, last-writer-wins, no narrative
thread) or external chat systems ATM cannot see.

This spec adds a comment entity: a per-task, append-mostly thread a human or
agent writes prose into and classifies with labels. The audit-log v1 spec
(§Out of scope, §Extensibility) explicitly anticipated this — *"Comment
entities. Confirmed supported by the design, but not implemented in v1. Adding
`comment.*` events is purely additive when ready."* This spec is the "when
ready."

The v2 principle survives intact: **the store has no intrinsic workflow
knowledge.** Classification of a comment (clarification vs. implementation vs.
QA-bug vs. open-question vs. design-doc) lives in label space, not in a
dedicated `kind` field. The log's action enum is closed-by-verb, open-by-
subject-kind — `Comment` inherits the four existing verbs scoped to a new
`comment` subject kind. No `LogEntry` schema change. No replay logic change
beyond one new `case "comment"` arm.

## Decisions (locked)

1. **Classification:** labels only, no `kind` field. A comment carries
   `Labels []string` exactly as a Task does. New classifications are invented
   at assign time with no code change.
2. **Lifecycle:** create + edit-body + label-manage + soft-remove. Five new
   `comment.*` actions on the per-project log; `comment.removed` writes a
   tombstone carrying the last full Comment state and deletes the cache file,
   exactly mirroring `task.removed`.
3. **Threading:** optional `ReplyTo string` field on Comment pointing to
   another comment ID within the same task. No `reply-to:` label, no first-
   class edge kind, no orphan/cycle checks. If the parent is later removed the
   reply stays with a dangling pointer — acceptable under v2's "no
   referential-integrity enforcement" principle (labels and tasks work this
   way too).
4. **References:** pure prose body. No structured `refs []string` field in v1.
   Agents write references (file paths, URLs, commit SHAs, task IDs) as
   markdown in the body. The classifier label conveys what a reference *is*.
5. **Target:** tasks only. A comment attaches to one task; the comment ID
   encodes its parent task by construction (`<TASK-ID>-c<N>`).
6. **ID scheme:** `<TASK-ID>-c<NNNN>`, 4-digit zero-padded (e.g.
   `ATM-0001-c0001`). Counter is per-task and lives on the Task entity as
   `NextCommentN int`, bumped under the project lock — exactly mirrors
   `Project.NextTaskN`.
7. **Cache layout:** `projects/<CODE>/comments/<comment-id>.json`, one file
   per comment, lazy self-heal via `log_seq` per file (mirrors the existing
   `projects/<CODE>/tasks/<task-id>.json` layout).
8. **CLI:** `atm task comment <verb>` nested. Seven verbs: `add`, `list`,
   `show`, `set-body`, `label add`, `label remove`, `remove`.
9. **TUI:** COMMENTS section in the task detail between facts and the
   history-overlay trigger. Rows show `actor`, `relative time`, `labels`, and
   an indented body block — no comment IDs in rows. `M` adds a comment;
   `Enter` opens a comment detail overlay (full body + ID + history); `H`
   opens a task history overlay (history removed from inline task detail).
10. **Log actions:** 5 new comment verbs + 1 new `task.meta-changed` action
    for opaque Task-struct mutations (the `NextCommentN` counter bump).
    `validActions` map extended; closed-enum invariant preserved. `task.meta-
    changed` follows the existing `<entity>.<aspect>-<verb>` pattern
    (`task.title-changed`, `task.description-changed`) — `changed` is the
    verb (one of the four closed verbs), `meta` the aspect; the action is
    not a new verb.

## Architecture & data flow

### File layout

```
$ATM_HOME/
  labels.json                              # unchanged (derived registry)
  projects/
    <CODE>.json                            # Project cache (unchanged shape)
    <CODE>/
      log.jsonl                            # THE source of truth (gains comment.* + task.meta-changed lines)
      tasks/<CODE>-<NNNN>.json             # Task cache (gains NextCommentN field)
      comments/<CODE>-<NNNN>-c<NNNN>.json  # Comment cache (new; one per comment)
    <CODE>.lock                            # per-project file lock (unchanged)
```

The existing `tasks/` directory is untouched. A new sibling `comments/`
directory holds one cache file per comment. The `<CODE>-<NNNN>-c<NNNN>` prefix
encodes the parent task by construction, so a `ReadDir` filtered by
`<CODE>-<NNNN>-c*` is the per-task comment list, with no separate index file.

### Write flow (e.g. `CreateComment(taskID, body, labels, replyTo, actor)`)

All steps run under `WithLock(code)` for the parent task's project lock:

1. `GetTask(taskID)` — validates parent exists.
2. Validate each label: `ValidateLabelName(l) + s.labelProjectExists(l)`.
   If `replyTo != ""`, validate `ParseCommentID(replyTo)` parses and that the
   parsed parent task prefix equals `taskID`'s prefix (same-task invariant).
   No existence check on `replyTo`.
3. `n := t.NextCommentN` (reads as zero if unset — `omitempty` keeps existing
   tasks unaffected on read until their first comment is added).
4. `id := RenderCommentID(taskID, n)` — `"ATM-0001-c0001"`.
5. Compose the Comment struct.
6. Append `label.upserted` for any newly-registered labels (BEFORE the comment
   event), reusing the existing `appendLabelUpsertsLocked` helper.
7. Append `comment.created` (payload = full Comment after-state). Capture the
   assigned `seq`; stamp it into the Comment's `LogSeq`.
8. Bump `t.NextCommentN = n + 1`; `t.UpdatedAt = ts; t.UpdatedBy = actor`.
   Append `task.meta-changed` (payload = full Task after-state with the
   bumped counter). Stamp the new `seq` into the Task's `LogSeq`.
9. `MkdirAll(commentsDir(code))`; `WriteJSON(commentPath(id), c)`. `WriteJSON(
   taskPath(taskID), t)`.
10. If any new labels were registered, `refreshDerivedLabelsLocked(code)`.

The audit-log spec's atomicity invariant holds: *all log entries for one
mutation append before any cache file writes*. Label upserts precede the
comment event; the comment event precedes the task-counter bump event; all
three log lines append under one lock, then caches write.

### Read flow (lazy self-heal, identical pattern to `GetTask`)

`GetComment(id)`:
1. `ParseCommentID(id)` → resolve parent code.
2. Read cache file. If missing or corrupt JSON, hand off to
   `rebuildCommentFromLog(id, code)` under the project lock, then re-read.
3. Cache present: compare `cache.LogSeq` to `lastCommentEventSeq(code, id)`.
   - `cache.LogSeq > last` → `ErrIntegrity` (cache claims a future seq).
   - `cache.LogSeq < last` → stale: `rebuildCommentFromLog(id, code)` under
     lock, re-read.
4. Return the cached Comment.

`comment.removed` deletes the cache file (tombstone in log only), so
`GetComment` on a removed comment returns `ErrNotFound` exactly as `GetTask`
does on a removed task.

`ListComments(taskID)` = `os.ReadDir(commentsDir(code))` filtered by
`<TASK>-c*`, load each cache, sort by ID. Cache-trust reads — lazy miss fires
on the per-file `GetComment` path.

### Replay (`internal/store/log.go`)

`ReplayState` gains `Comments []*Comment`. `Replay()` learns one new
`case "comment"` arm:

```go
case "comment":
    var c Comment
    _ = json.Unmarshal(e.Payload, &c)
    switch e.Action {
    case ActionCommentCreated, ActionCommentBodyChanged,
         ActionCommentLabelAdded, ActionCommentLabelRemoved:
        comments[e.Subject.ID] = &c
    case ActionCommentRemoved:
        delete(comments, e.Subject.ID)
    }
```

The existing `task:` case gains a new `ActionTaskMetaChanged` arm that
upserts the Task (carrying the bumped `NextCommentN`), so replay rebuilds the
counter from `task.meta-changed` events.

Tombstones replay correctly: `comment.removed` deletes the subject from the
replay map; the live comment cache set is `{created events} − {removed
events, last-write-wins on subject}` — identical to the task rule.

## Components & store API surface

### New types (`internal/store/types.go`)

```go
type Comment struct {
    ID          string    `json:"id"`                      // <TASK-ID>-c<NNNN>
    TaskID      string    `json:"task_id"`                 // parent task; redundant but explicit for consumers
    ReplyTo     string    `json:"reply_to,omitempty"`      // optional; same-task comment ID
    Body        string    `json:"body"`                    // free-form prose (markdown by convention; store is format-agnostic)
    Labels      []string  `json:"labels"`                  // classification here, in label space
    LogSeq      int       `json:"log_seq"`
    CreatedAt   time.Time `json:"created_at"`
    CreatedBy   string    `json:"created_by"`
    UpdatedAt   time.Time `json:"updated_at"`
    UpdatedBy   string    `json:"updated_by"`
}
```

`Task` gains one field:

```go
type Task struct {
    // ...existing fields unchanged...
    NextCommentN int `json:"next_comment_n,omitempty"`
}
```

`ReplayState` gains `Comments []*Comment` (sorted by ID).

### New log actions (`internal/store/log.go`)

```go
const (
    // ...existing 11 actions unchanged (closed-by-verb, open-by-subject-kind)...
    ActionTaskMetaChanged      = "task.meta-changed"        // opaque Task-struct mutation (counter bump)
    ActionCommentCreated      = "comment.created"
    ActionCommentBodyChanged  = "comment.body-changed"
    ActionCommentLabelAdded   = "comment.label-added"
    ActionCommentLabelRemoved = "comment.label-removed"
    ActionCommentRemoved      = "comment.removed"
)
```

`validActions` map extended with the six new constants. Unknown action →
`ErrUsage` before logging, no line appended (existing invariant).

`Subject.Kind` gains the value `"comment"`. `Subject.ID = "<TASK-ID>-c<NNNN>"`.
`subjectMatch` gains one `case "comment"` arm comparing `a.ID == b.ID`.

### New store file: `internal/store/comment.go`

```go
// ParseCommentID recognizes "<CODE>-<NNNN>-c<NNNN>". Reuses ParseTaskID on the
// parent prefix. Returns ok=false on malformed input.
func ParseCommentID(id string) (code string, taskN, commentN int, ok bool)

// RenderCommentID composes "ATM-0001-c0001" from a task ID and a per-task
// counter. Comment counter is 4-digit zero-padded up to 9999, then natural width
// — mirrors RenderTaskID.
func RenderCommentID(taskID string, n int) string

func (s *Store) CreateComment(taskID, body string, labels []string, replyTo, actor string) (*Comment, error)
func (s *Store) GetComment(id string) (*Comment, error)
func (s *Store) ListComments(taskID string) ([]*Comment, error)
func (s *Store) SetCommentBody(id, body, actor string) error
func (s *Store) CommentLabelAdd(id, label, actor string) error
func (s *Store) CommentLabelRemove(id, label, actor string) error
func (s *Store) RemoveComment(id, actor string) error
```

`CreateComment` enforces: `body != ""`, `actor != ""`, label regex + project
prefix, `replyTo` same-task invariant if set. Reuses the existing
`appendLabelUpsertsLocked` helper for auto-registration.

`GetComment` reuses the same lazy-miss + `log_seq` compare + corrupt-cache
rebuild pattern as `GetTask`. On `comment.removed` (cache file deleted),
returns `ErrNotFound`.

`SetCommentBody` uses the existing `mutateTask`-style log-first helper
adapted to comments. `CommentLabelAdd` mirrors `TaskLabelAdd`: validates +
auto-registers + appends `label.upserted` for new labels BEFORE
`comment.label-added`; refreshes `labels.json` if any new labels registered.
`CommentLabelRemove` and `RemoveComment` mirror their task counterparts
one-for-one.

### Rewritten store files

`internal/store/log.go`: `validActions` extended; `Replay` gains the comment
arm; `subjectMatch` gains the comment case. No change to `AppendLog` /
`ReadLog` / `LastLogSeq` — the log substrate is comment-agnostic.

`internal/store/rebuild.go`: `Rebuild()` writes comment caches from replay
state and removes orphan comment caches (mirrors the task-cache sweep at
lines 43–52). One new pass per project: after writing task caches, write
every replayed Comment cache and sweep the `comments/` directory for orphans
(caches for comment IDs no longer in the replay state — including
tombstoned comments).

`internal/store/verify.go`: `Verify()` gains one `CacheCheck` line per
comment file, same staleness/missing/corrupt detection as task caches.

`internal/store/store.go`: new path helpers `commentsDir(code)` and
`commentPath(id)` mirroring `tasksDir` / `taskPath`.

### CLI surface (`internal/cli/comment.go` — new file)

```
atm task comment add         --task <ID> --body <TEXT> [--label <L>]... [--reply-to <COMMENT-ID>] [--actor <id>]
atm task comment list         --task <ID>
atm task comment show          --id <COMMENT-ID>
atm task comment set-body      --id <COMMENT-ID> --body <TEXT> [--actor <id>]
atm task comment label add     --id <COMMENT-ID> --label <L> [--actor <id>]
atm task comment label remove  --id <COMMENT-ID> --label <L> [--actor <id>]
atm task comment remove        --id <COMMENT-ID> [--actor <id>]
```

Flag parity with existing task verbs: `--label` is `StringArrayVar`
(repeatable, full names, validated); `--actor` resolved via
`st.resolveActor(true)` (required on mutating commands, optional on reads);
`--output json|text` honored globally.

`newTaskCmd` in `task.go` adds one line: `cmd.AddCommand(newTaskCommentCmd(st))`.
`newTaskCommentCmd` returns a `cobra.Command{Use:"comment"}` and adds the
seven subcommands; `comment label` is a sub-group with `add`/`remove`
mirroring `newTaskLabelCmd`'s two-level nesting exactly.

**Text output** (default):

- `comment add` → `created comment <ID>` (single line).
- `comment list` → one block per comment: `<ID>\t<CREATED-AT-RFC3339>\t<ACTOR>\t<LABELS>` header line then body indented 4 spaces; each subsequent body line indented to keep one comment as a visual block.
- `comment show` → detailed block: `ID`, `TASK`, `REPLY-TO` (if set), `ACTOR`, `CREATED`, `UPDATED`, `LABELS`, blank line, body.
- `comment set-body` → `updated body <ID>`.
- `comment label add/remove` → `added label <L> to <ID>` / `removed label <L> from <ID>`.
- `comment remove` → `removed comment <ID>`.

A new `renderCommentListText` helper in `output.go`. The task/list text
renderer stays one-line-per-row; comments need the multi-line body block.

**JSON output** (new mappers in `output.go`, sorted keys via `MarshalSorted`,
RFC3339 UTC timestamps):

```go
type jsonComment struct {
    ID        string        `json:"id"`
    TaskID    string        `json:"task_id"`
    ReplyTo   string        `json:"reply_to,omitempty"`
    Body      string        `json:"body"`
    Labels    []string      `json:"labels"`
    LogSeq    int           `json:"log_seq"`
    History   []jsonHistory `json:"history"`
    CreatedAt string        `json:"created_at"`
    CreatedBy string        `json:"created_by"`
    UpdatedAt string        `json:"updated_at"`
    UpdatedBy string        `json:"updated_by"`
}
```

- `comment add` / `comment show` / `comment set-body` / `comment label add/remove` → `{"comment": jsonComment}` (`history` populated only for `show`; empty array otherwise — same convention as `task show` vs `task create`).
- `comment list` → `{"comments": [jsonComment, ...]}`.
- `comment remove` → `{"removed": "<ID>"}` (mirrors `task remove`).

**History integration:** `comment show --output json` (and text, in the detailed block) calls `s.History(code, Subject{Kind:"comment", ID:commentID})` and renders the same `[seq] action actor at` rows the task detail renders. `historyToJSON` and the text history renderer are reused as-is; they consume `HistoryView{Seq, Action, Actor, At}` which is already subject-agnostic.

**No new global flags, no new exit codes.** Errors reuse existing sentinels:
`ErrUsage` (2) for malformed comment ID, bad label, missing required flag;
`ErrNotFound` (3) for missing comment or task; `ErrIntegrity` (5)
propagates from log reads inside `GetComment`/`History`.

## TUI surface

The TUI keeps the existing three-tab structure (Projects / Tasks / Help) and
the single-pane task detail. Comments live on the task detail — no new tab.

**Task detail layout** (revised):

```
TASK FACTS
ID  TITLE  LABELS  UPDATED  (existing)
DESCRIPTION (existing)

COMMENTS                                          # new section
  <ACTOR>  <RELATIVE-TIME>  [<LABELS>]
      <body line 1>
      <body line 2>
  <ACTOR>  <RELATIVE-TIME>  [<LABELS>]
      re: <REPLY-TO> — <body line 1>
      <body line 2>

[press H for history]                              # replaces inline HISTORY section
```

Each comment row shows `actor`, `relative time`, `labels`, then an indented
body block (truncated to a configurable max height per row — 6 lines, with
`…` if longer; the full body opens in the comment detail overlay). No
comment IDs in rows (TUI is human-facing; IDs are agent-facing and surface
only in the comment detail overlay).

**HISTORY is removed from the default task detail render.** The audit-log v1
spec kept a TUI log viewer out of v1; we extend the same principle to the
inline history section. History is a specialty affordance for audit/debug, not
part of "current state" the human consults.

**Task detail keys** (additive):

| Key | Action |
|---|---|
| `M` | **add comment** — opens form: body (multi-line text area), labels (comma list), reply-to (optional; defaults to the currently selected comment if one is highlighted in the COMMENTS section) |
| `Enter` on a comment row (when focus is in COMMENTS section) | open **comment detail overlay** |
| `H` | open **history overlay** for this task (covers `task.*` + `task.meta-changed` events; same overlay pattern works for comments — `comment.*` events — when opened from a comment) |
| `Esc` | close any open overlay (history / comment detail) |

`M` is the chosen add-comment key. `C` was rejected — it is the global
"open conventions" binding (`keymap.go` row 43). `M` reads as "message"
(human-facing), upper-case consistent with the detail-view convention
(`N` name, `H` history, `S` seed, `T` theme).

**Focus ring:** the existing detail view is single-pane (task facts,
description, history all render as one scroll). A lightweight focus ring is
added: `Tab` cycles between `(task-facts block)` and `(comments block)`;
when the comments block is focused, `j/k` moves between comment rows and
`Enter` opens the comment detail overlay. Without the focus ring you
cannot select a comment to operate on. This is the smallest possible
change to the existing single-pane detail.

**Comment detail overlay** (`internal/tui/comments.go`, new): a focused
overlay (same component pattern as the existing detail overlays) showing
one comment's ID + full body + labels + meta + the comment's own history
rows. Keys in the overlay:

| Key | Action |
|---|---|
| `e` | edit body (form) |
| `b` | add label (form: name) |
| `B` | remove label (form: name) |
| `R` | reply — opens add-comment form with `--reply-to` preset to this comment's ID |
| `H` | show this comment's history inside the overlay |
| `x` | remove comment (confirm overlay) |
| `Esc` | close overlay |

**Help tab / parity table** (`internal/tui/help.go`): gains new rows
mapping the CLI verbs to TUI keys (`M` add comment, `H` history overlay,
`Enter` open comment, comment-detail keys `e`/`b`/`B`/`R`/`x`), mirroring
the existing CLI/TUI parity rows. `keymap.go` gains a new `M` row scoped to
Detail only ("add comment (task)").

**Forms:** reuse the existing `form.go` primitives. Comment-add form is
body (multi-line text area), labels (comma list), reply-to (single input).
Label add/remove forms are single-input, identical to task label forms.

**No new TUI tab.** Comments live on the task detail; cross-task listing is
the CLI's job in v1. View snapshot tests update the expected task detail
render to include the new COMMENTS section and the `[press H for history]`
note (no inline history rows).

## Conventions update

`atm conventions` output (and TUI Help tab parity table) gains two new
advisory seed-namespace rows. The earlier-considered `reply-to:<ID>` row
was rolled back — reply relationships are expressed in the `ReplyTo`
field, not the label registry.

| Namespace | Examples | Purpose |
|---|---|---|
| `comment:<kind>` | `ATM:comment:clarification`, `ATM:comment:implementation`, `ATM:comment:qa-bug`, `ATM:comment:open-question`, `ATM:comment:design-doc` | classify a comment's role in the workflow |
| `activity:<kind>` | `ATM:activity:analysis`, `ATM:activity:implementation`, `ATM:activity:qa` | optional coarser activity facet aggregating across comments and tasks |

Plus a one-line note in the agent first-contact sequence: *"To narrate
progress on a task, use `atm task comment add` with a `comment:` classification
label."*

All conventions are advisory only — the store treats
`ATM:comment:clarification` identically to any other label string. Nothing in
the store validates the documented namespaces.

## Testing, verification & rollout

### Testing approach

Same layered structure as v2 / audit-log v1: unit tests per store file,
CLI tests via the `testdata/golden/` pattern, TUI tests via view snapshot
assertions.

**New store test file:** `internal/store/comment_test.go` mirrors
`task_test.go`. Invariants covered:

| Area | Invariant |
|---|---|
| `CreateComment` | Per-task counter monotone; `comment.created` event appended after `label.upserted` events for newly-registered labels, before `task.meta-changed` for the counter bump; cache file written; task cache updated with bumped `NextCommentN`. |
| ID scheme | `<TASK-ID>-c<NNNN>` (4-digit zero-padded); `ParseCommentID` round-trips; rejects malformed (`c3` alone, wrong task prefix, no `-c`). `RenderCommentID` produces zero-padded IDs. |
| `ReplyTo` | Empty allowed; non-empty must parse as a comment ID and the parent task prefix must equal this comment's task — rejection cases covered. No existence check (dangling reference tolerated). |
| `SetCommentBody` / `CommentLabelAdd` / `CommentLabelRemove` | Each appends its respective action, full after-state in payload, cache and `LogSeq` updated. Label add auto-registers new labels via `label.upserted` first. |
| `RemoveComment` | Appends `comment.removed` tombstone; deletes the cache file; `GetComment` returns `ErrNotFound`; `History` view for that subject still renders the events. |
| Cache self-heal (lazy miss) | Hand-delete cache → next `GetComment` still returns correct entity. Hand-write stale `log_seq` → next read re-triggers `rebuildCommentFromLog`. Hand-write `log_seq > log.LastSeq` → `ErrIntegrity`. |
| Replay (`log.go`) | New `case "comment"` arm: created/body-changed/label-* upsert the comment; `comment.removed` deletes. Tombstones replay correctly; multi-subject mutations (label+comment) replay in order. `NextCommentN` derives from `task.meta-changed` events. |
| `ListComments(taskID)` | Returns comments for that task only, sorted by ID; ignores comments on other tasks (per-task prefix filter). |
| Action enum | Unknown `comment.*` action passed to `AppendLog` → `ErrUsage`, no line appended. |
| Ordering | `label.*` events appear before any `comment.*` event referencing the label. Verified by `Replay` after every test mutation. |
| Counter bump atomicity | The `task.meta-changed` event appears after `comment.created` in the log; crash between them → next `CreateComment` recomputes from log replay (lazy miss on task cache). |

**Carryover invariants** (re-tested against the new pipeline): label
prefix-project match, auto-registration, soft removal of label registry
entries, determinism (sorted JSON, RFC3339 UTC). No new sentinel errors.

**CLI tests** (`internal/cli/comment_test.go`, golden pattern, text + JSON):
each of the seven verbs has text and JSON cases. Malformed comment ID on
`show`/`set-body`/`label add`/`label remove`/`remove` → exit 2. `comment
list --task <ID>` returns empty `{"comments":[]}` for a task with no
comments. `--actor` required on mutating commands. `comment add --reply-to`
with a cross-task comment ID → exit 2.

**TUI tests** (`internal/tui/comments_test.go`): `M` opens add-comment form;
submitting writes the comment and the COMMENTS section re-renders with the
new row (actor + relative time + labels + indented body); `Enter` on a
comment row opens the comment detail overlay showing the hidden comment ID
+ full body; `H` opens a task history overlay; `H` from within a comment
detail overlay shows that comment's history; `Esc` closes overlays. View
snapshot assertion on the simplified task detail render (no inline history;
comments section between facts and the `[press H for history]` note).

**Verify/Rebuild integration**: `verify.go` and `rebuild.go` gain comment-
cache sweep arms — `Rebuild` writes comment caches from replay state and
removes orphan comment caches, mirroring the task-cache sweep in
`rebuild.go`. One new `CacheCheck` line per comment file in the verify
report.

### Verification gate

Unchanged: `make verify` (`make build && make test`). No new make targets.
`.golangci.yml` carries over unchanged. All new test files participate in
`make test`.

### Rollout (Approach A — strictly additive)

Unlike the v2 / audit-log rollouts, this one is **strictly additive** —
the tree builds and tests pass at every commit. No delete commit. Suggested
commit sequence:

1. **`types.go` + `log.go` enum extension + log tests.** New `Comment`
   struct, `NextCommentN` on Task (omitempty), six new action constants,
   `ReplayState.Comments`, `Replay` comment arm, `subjectMatch` comment case,
   `validActions` extension. Tests: action enum rejects unknown `comment.*`;
   replay handles comment events + tombstones.
2. **`comment.go` + `comment_test.go`.** `ParseCommentID`, `RenderCommentID`,
   `CreateComment`, `GetComment` (lazy self-heal), `ListComments`,
   `SetCommentBody`, `CommentLabelAdd`/`Remove`, `RemoveComment`. Path
   helpers `commentsDir`/`commentPath` in `store.go`. All store invariants
   covered.
3. **`verify.go` + `rebuild.go` extension + tests.** Comment-cache sweep in
   `Rebuild`; `CacheCheck` per comment file in `Verify`.
4. **`cli/comment.go` + output mappers + golden files + tests.** Seven
   verbs, text and JSON outputs, error exit codes, `--actor` requirement.
5. **`cli/task.go` one-line wiring + keymap/conventions updates + tests.**
   `newTaskCommentCmd` added under `task`; conventions output updated;
   `atm conventions` test golden updated.
6. **TUI: data plumbing + comment section render + tests.** COMMENTS section
   in task detail, comment-list rendering with hidden IDs, view snapshot
   test updates.
7. **TUI: detail + history overlays + forms + tests.** `M` add-comment form,
   comment detail overlay (`e`/`b`/`B`/`R`/`x`/`H`/`Esc`), task history
   overlay (`H`), focus ring, help tab + keymap updates.

Per-commit `make verify` must stay green — no "tree may not build" window as
in the v2 / audit-log rollouts, because no existing types or actions are
being removed or rewritten.

No data migration tool. v2 / audit-log-v1 users keep their `$ATM_HOME` —
existing tasks read `NextCommentN=0` (the `omitempty` field reads as zero);
the first comment on any existing task starts at `c0001`. Old logs have no
`comment.*` or `task.meta-changed` events; replay simply has no comment arm
to execute for them — backward compatible by construction.

## Out of scope (v1 of comments)

- **Cross-task comment feed / TUI activity tab.** Comments live on the task
  detail only; cross-task aggregation is a CLI-side concern for now.
- **Inline comment body edit (edit-in-place on the row).** Body edits go
  through the comment detail overlay form.
- **Comment search / filter within the TUI.** Reuses the CLI
  (`atm task comment list`).
- **Attachment payload beyond body prose.** Per decision 4, references live
  in the body; no structured `refs []string` field in v1.
- **Label migration tooling.** Renaming/migrating a `comment:` label across
  all comments inherits today's primitive (manual `comment label remove` +
  `add` per comment); a batch tool is out of scope, consistent with the
  audit-log v1 spec which kept custom action verbs out of the enum.
- **Threading depth / collapse / expand UI.** `ReplyTo` is stored; the TUI
  renders it as a row in a flat chronological list with the reply-to line
  shown in the comment detail. Collapsible nesting is deferred.
- **Comment pinning / ordering by votes.** `ReplyTo` and chronological order
  are the only ordering signals in v1.
- **Per-comment raw-log viewer in the TUI.** A `--follow`/`tail -f`-equivalent
  for comment events on a task is deferred, mirroring the audit-log v1's
  general streaming deferral.
- **Project-attached comments.** Comments attach to tasks only (decision 5);
  a project-level narrative channel would overlap with the existing
  `atm store log <CODE>` surface.