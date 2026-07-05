# Task Comments v1 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a comment entity to ATM — a per-task, append-mostly narrative thread a human or agent writes prose into and classifies with labels — purely additive to the v2 / audit-log-v1 store.

**Architecture:** Comments reuse the existing per-project `log.jsonl` as source of truth and the existing `tasks/` cache pattern as materialized views. Six new log actions (five `comment.*` + one `task.meta-changed`) extend the closed-by-verb enum by a new subject kind. Classification lives in label space — no intrinsic workflow knowledge added to the store.

**Tech Stack:** Go 1.22+, cobra (CLI), Bubble Tea (TUI). No new dependencies. Spec: `docs/superpowers/specs/2026-07-05-task-comments-v1-design.md`.

## Global Constraints

- **Verify gate:** `make verify` (runs `make build && make test`) must pass after every commit. The tree must build at every commit (strictly additive rollout — no "may not build" windows as in v2 / audit-log rollouts, because nothing existing is being deleted or rewritten).
- **Style:** No emojis in code or commits. Follow existing style in neighboring files. No comments unless asked.
- **Error sentinels:** Reuse existing `ErrUsage`/`ErrNotFound`/`ErrIntegrity`/`ErrConflict` from `internal/store/store.go`. No new sentinels.
- **Exit codes:** 0 success; 1 generic; 2 usage; 3 not-found; 5 integrity. No new codes.
- **Determinism:** JSON output uses `store.MarshalSorted` (sorted keys, stable whitespace, RFC3339 UTC timestamps).
- **CLI flag conventions:** `--label` is `StringArrayVar` (repeatable, full names); `--actor` resolved via `st.resolveActor(true)` (required on mutating commands, optional on reads); `--output json|text` honored globally.
- **Conventions:** `M` (capital) is the add-comment TUI key — `C` is reserved for "open conventions" (`internal/tui/keymap.go:43`). `H` opens the task history overlay.
- **No migration:** v2/audit-log users keep their `$ATM_HOME`. Existing tasks read `NextCommentN=0` (`omitempty`). The first comment on any existing task starts at `c0001`.

**Helpful references (do not re-read in every task — read once if needed):**
- v2 spec: `docs/superpowers/specs/2026-07-02-tasks-management-v2-design.md`
- Audit-log spec: `docs/superpowers/specs/2026-07-04-audit-log-redesign-design.md`
- Comments spec: `docs/superpowers/specs/2026-07-05-task-comments-v1-design.md`

---

### Task 1: Extend types, log actions, and replay

**Files:**
- Modify: `internal/store/types.go` (add `Comment` struct; add `NextCommentN int` to `Task`; add `Comments []*Comment` to `ReplayState`)
- Modify: `internal/store/log.go` (add six action constants; extend `validActions`; extend `Replay`; extend `subjectMatch`)
- Test: `internal/store/comment_log_test.go` (new)

**Interfaces:**
- Consumes: existing `LogEntry`, `Subject`, `Project`, `Task`, `Label`, `ReplayState`, `mustMarshal`, `HistoryView`, `subjectMatch` from `internal/store/log.go` and `types.go`.
- Produces:
  - `Comment` struct (exported, in `types.go`)
  - `ActionTaskMetaChanged`, `ActionCommentCreated`, `ActionCommentBodyChanged`, `ActionCommentLabelAdded`, `ActionCommentLabelRemoved`, `ActionCommentRemoved` constants (exported, in `log.go`)
  - `ReplayState.Comments []*Comment` field (in `types.go`/`log.go`)
  - `Replay()` arm `case "comment"` that upserts/removes comments by `e.Subject.ID`
  - `subjectMatch` arm `case "comment"` comparing `a.ID == b.ID`

- [ ] **Step 1: Write the failing test**

Create `internal/store/comment_log_test.go`:

```go
package store

import (
	"encoding/json"
	"os"
	"testing"
)

func TestReplayCommentCreatedAndRemoved(t *testing.T) {
	s := newTestStore(t)
	_ = os.MkdirAll(s.projectDir("ATM"), 0o755)
	_, _ = s.AppendLog("ATM", newLogEntry(0, ActionProjectCreated, Subject{Kind: "project", Code: "ATM"}, Project{Code: "ATM", Name: "x", NextTaskN: 1}))
	_, _ = s.AppendLog("ATM", newLogEntry(0, ActionTaskCreated, Subject{Kind: "task", ID: "ATM-0001"}, Task{ID: "ATM-0001", ProjectCode: "ATM", Title: "t", Labels: []string{}}))
	_, _ = s.AppendLog("ATM", newLogEntry(0, ActionCommentCreated, Subject{Kind: "comment", ID: "ATM-0001-c0001"}, Comment{ID: "ATM-0001-c0001", TaskID: "ATM-0001", Body: "first"}))
	_, _ = s.AppendLog("ATM", newLogEntry(0, ActionCommentCreated, Subject{Kind: "comment", ID: "ATM-0001-c0002"}, Comment{ID: "ATM-0001-c0002", TaskID: "ATM-0001", Body: "second"}))
	_, _ = s.AppendLog("ATM", newLogEntry(0, ActionCommentBodyChanged, Subject{Kind: "comment", ID: "ATM-0001-c0001"}, Comment{ID: "ATM-0001-c0001", TaskID: "ATM-0001", Body: "edited first"}))
	_, _ = s.AppendLog("ATM", newLogEntry(0, ActionCommentRemoved, Subject{Kind: "comment", ID: "ATM-0001-c0002"}, Comment{ID: "ATM-0001-c0002", TaskID: "ATM-0001", Body: "second"}))
	st, err := s.Replay("ATM")
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Comments) != 1 {
		t.Fatalf("expected 1 live comment, got %d", len(st.Comments))
	}
	if st.Comments[0].ID != "ATM-0001-c0001" || st.Comments[0].Body != "edited first" {
		t.Fatalf("rebuilt comment = %+v", st.Comments[0])
	}
}

func TestReplayTaskMetaChanged(t *testing.T) {
	s := newTestStore(t)
	_ = os.MkdirAll(s.projectDir("ATM"), 0o755)
	_, _ = s.AppendLog("ATM", newLogEntry(0, ActionProjectCreated, Subject{Kind: "project", Code: "ATM"}, Project{Code: "ATM", NextTaskN: 1}))
	_, _ = s.AppendLog("ATM", newLogEntry(0, ActionTaskCreated, Subject{Kind: "task", ID: "ATM-0001"}, Task{ID: "ATM-0001", ProjectCode: "ATM", Title: "t"}))
	_, _ = s.AppendLog("ATM", newLogEntry(0, ActionTaskMetaChanged, Subject{Kind: "task", ID: "ATM-0001"}, Task{ID: "ATM-0001", ProjectCode: "ATM", Title: "t", NextCommentN: 3}))
	st, err := s.Replay("ATM")
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Tasks) != 1 || st.Tasks[0].NextCommentN != 3 {
		t.Fatalf("replay did not apply task.meta-changed: %+v", st.Tasks)
	}
}

func TestHistoryForCommentSubject(t *testing.T) {
	s := newTestStore(t)
	_ = os.MkdirAll(s.projectDir("ATM"), 0o755)
	_, _ = s.AppendLog("ATM", newLogEntry(0, ActionProjectCreated, Subject{Kind: "project", Code: "ATM"}, Project{Code: "ATM", NextTaskN: 1}))
	_, _ = s.AppendLog("ATM", newLogEntry(0, ActionTaskCreated, Subject{Kind: "task", ID: "ATM-0001"}, Task{ID: "ATM-0001", ProjectCode: "ATM", Title: "t"}))
	_, _ = s.AppendLog("ATM", newLogEntry(0, ActionCommentCreated, Subject{Kind: "comment", ID: "ATM-0001-c0001"}, Comment{ID: "ATM-0001-c0001", Body: "x"}))
	hv := s.History("ATM", Subject{Kind: "comment", ID: "ATM-0001-c0001"})
	if len(hv) != 1 || hv[0].Action != ActionCommentCreated {
		t.Fatalf("history = %+v", hv)
	}
}

func TestAppendLogRejectsUnknownCommentAction(t *testing.T) {
	s := newTestStore(t)
	_ = os.MkdirAll(s.projectDir("ATM"), 0o755)
	_, err := s.AppendLog("ATM", LogEntry{At: Now(), Actor: "a", Action: "comment.bogus", Subject: Subject{Kind: "comment", ID: "ATM-0001-c0001"}})
	if !IsUsage(err) {
		t.Fatalf("expected ErrUsage for unknown comment action, got %v", err)
	}
}

func TestReplayDeterministicComments(t *testing.T) {
	s := newTestStore(t)
	_ = os.MkdirAll(s.projectDir("ATM"), 0o755)
	_, _ = s.AppendLog("ATM", newLogEntry(0, ActionProjectCreated, Subject{Kind: "project", Code: "ATM"}, Project{Code: "ATM", NextTaskN: 1}))
	_, _ = s.AppendLog("ATM", newLogEntry(0, ActionCommentCreated, Subject{Kind: "comment", ID: "ATM-0001-c0002"}, Comment{ID: "ATM-0001-c0002", TaskID: "ATM-0001", Body: "z"}))
	_, _ = s.AppendLog("ATM", newLogEntry(0, ActionCommentCreated, Subject{Kind: "comment", ID: "ATM-0001-c0001"}, Comment{ID: "ATM-0001-c0001", TaskID: "ATM-0001", Body: "a"}))
	st1, _ := s.Replay("ATM")
	st2, _ := s.Replay("ATM")
	if len(st1.Comments) != 2 || len(st2.Comments) != 2 {
		t.Fatalf("replay count mismatch: %d vs %d", len(st1.Comments), len(st2.Comments))
	}
	if st1.Comments[0].ID != st2.Comments[0].ID {
		t.Fatalf("non-deterministic comment sort")
	}
}

func init() {
	// Suppress "unused import" if json is only used in some compile paths.
	_ = json.RawMessage(nil)
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/store/ -run 'TestReplayComment|TestReplayTaskMetaChanged|TestHistoryForCommentSubject|TestAppendLogRejectsUnknownCommentAction' -v`
Expected: FAIL with compile errors — `ActionCommentCreated` undefined, `Comment` undefined, etc.

- [ ] **Step 3: Add types and constants**

Edit `internal/store/types.go`. Append the `Comment` struct after the `Task` struct, and add `NextCommentN` to `Task`.

The `Task` struct becomes (add the new field at the bottom; keep all existing fields/JSON tags exactly as they are):

```go
type Task struct {
	ID          string    `json:"id"`
	ProjectCode string    `json:"project_code"`
	Title       string    `json:"title"`
	Description string    `json:"description,omitempty"`
	Labels      []string  `json:"labels"`
	LogSeq      int       `json:"log_seq"`
	CreatedAt   time.Time `json:"created_at"`
	CreatedBy   string    `json:"created_by"`
	UpdatedAt   time.Time `json:"updated_at"`
	UpdatedBy   string    `json:"updated_by"`
	NextCommentN int      `json:"next_comment_n,omitempty"`
}
```

After `Task`, append:

```go
type Comment struct {
	ID          string    `json:"id"`
	TaskID      string    `json:"task_id"`
	ReplyTo     string    `json:"reply_to,omitempty"`
	Body        string    `json:"body"`
	Labels      []string  `json:"labels"`
	LogSeq      int       `json:"log_seq"`
	CreatedAt   time.Time `json:"created_at"`
	CreatedBy   string    `json:"created_by"`
	UpdatedAt   time.Time `json:"updated_at"`
	UpdatedBy   string    `json:"updated_by"`
}
```

Edit `internal/store/log.go`:

Extend the action constant block (after `ActionLabelRemoved`):

```go
	ActionTaskMetaChanged      = "task.meta-changed"
	ActionCommentCreated      = "comment.created"
	ActionCommentBodyChanged  = "comment.body-changed"
	ActionCommentLabelAdded   = "comment.label-added"
	ActionCommentLabelRemoved = "comment.label-removed"
	ActionCommentRemoved      = "comment.removed"
```

Extend `validActions` with these six constants.

Extend `ReplayState`:

```go
type ReplayState struct {
	Project  *Project
	Tasks    []*Task
	Labels   []Label
	Comments []*Comment
}
```

In `Replay()`, add `comments := map[string]*Comment{}` next to the existing `tasks := ...` line. Inside the loop, add a new `case "comment"` arm:

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

Add a new `ActionTaskMetaChanged` arm to the existing `case "task"` block, alongside `ActionTaskCreated, ActionTaskTitleChanged, ActionTaskDescChanged, ActionTaskLabelAdded, ActionTaskLabelRemoved`:

```go
			case ActionTaskCreated, ActionTaskTitleChanged, ActionTaskDescChanged,
				ActionTaskLabelAdded, ActionTaskLabelRemoved, ActionTaskMetaChanged:
				tasks[e.Subject.ID] = &tk
```

After the loop (next to where `st.Tasks` is assembled), add:

```go
	for _, c := range comments {
		st.Comments = append(st.Comments, c)
	}
	sort.Slice(st.Comments, func(i, j int) bool { return st.Comments[i].ID < st.Comments[j].ID })
```

In `subjectMatch`, add:

```go
	case "comment":
		return a.ID == b.ID
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/store/ -run 'TestReplayComment|TestReplayTaskMetaChanged|TestHistoryForCommentSubject|TestAppendLogRejectsUnknownCommentAction' -v`
Expected: PASS.

Then run the full store test suite to confirm no regressions: `go test ./internal/store/ -v`
Expected: PASS — all existing tests still pass (`NextCommentN` is `omitempty`, so existing tasks serialize unchanged).

- [ ] **Step 5: Commit**

```bash
git add internal/store/types.go internal/store/log.go internal/store/comment_log_test.go
git commit -m "store: add Comment type, six log actions, replay arm"
```

---

### Task 2: Comment ID parsing and rendering

**Files:**
- Modify: `internal/store/store.go` (add `commentIDRe`, `ParseCommentID`, `RenderCommentID`)
- Test: `internal/store/comment_id_test.go` (new)

**Interfaces:**
- Consumes: existing `ParseTaskID`, `RenderTaskID`, `TaskIDRe` from `internal/store/store.go`.
- Produces:
  - `CommentIDRe *regexp.Regexp` (exported)
  - `ParseCommentID(id string) (code string, taskN, commentN int, ok bool)` (exported)
  - `RenderCommentID(taskID string, n int) string` (exported)

Comment IDs follow `<CODE>-<NNNN>-c<NNNN>`, 4-digit zero-padded, then natural width past 9999 (mirrors `RenderTaskID`).

- [ ] **Step 1: Write the failing test**

Create `internal/store/comment_id_test.go`:

```go
package store

import "testing"

func TestParseCommentID(t *testing.T) {
	cases := []struct {
		in   string
		ok   bool
		code string
		taskN int
		commentN int
	}{
		{"ATM-0001-c0001", true, "ATM", 1, 1},
		{"ATM-9999-c9999", true, "ATM", 9999, 9999},
		{"ATM-0001-c0001", true, "ATM", 1, 1},
		{"ATM-10000-c0001", true, "ATM", 10000, 1},
		{"ATM-c0001", false, "", 0, 0},
		{"ATM-0001", false, "", 0, 0},
		{"c0001", false, "", 0, 0},
		{"", false, "", 0, 0},
		{"atm-0001-c0001", false, "", 0, 0},
		{"ATM-0001-C0001", false, "", 0, 0},
		{"XYZ-0001-c0001", true, "XYZ", 1, 1},
	}
	for _, tc := range cases {
		code, taskN, commentN, ok := ParseCommentID(tc.in)
		if ok != tc.ok || (ok && (code != tc.code || taskN != tc.taskN || commentN != tc.commentN)) {
			t.Errorf("ParseCommentID(%q) = (%q, %d, %d, %v), want (%q, %d, %d, %v)",
				tc.in, code, taskN, commentN, ok, tc.code, tc.taskN, tc.commentN, tc.ok)
		}
	}
}

func TestRenderCommentID(t *testing.T) {
	if got := RenderCommentID("ATM-0001", 1); got != "ATM-0001-c0001" {
		t.Fatalf("RenderCommentID(1) = %q want ATM-0001-c0001", got)
	}
	if got := RenderCommentID("ATM-0001", 9999); got != "ATM-0001-c9999" {
		t.Fatalf("RenderCommentID(9999) = %q want ATM-0001-c9999", got)
	}
	if got := RenderCommentID("ATM-0001", 10000); got != "ATM-0001-c10000" {
		t.Fatalf("RenderCommentID(10000) = %q want ATM-0001-c10000", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/store/ -run 'TestParseCommentID|TestRenderCommentID' -v`
Expected: FAIL — `ParseCommentID` and `RenderCommentID` undefined.

- [ ] **Step 3: Implement**

Add to `internal/store/store.go` (right after the `SortTaskIDs` function at line 73):

```go
var CommentIDRe = regexp.MustCompile(`^([A-Z]{3,6})-(\d+)-c(\d+)$`)

func ParseCommentID(id string) (code string, taskN int, commentN int, ok bool) {
	m := CommentIDRe.FindStringSubmatch(id)
	if m == nil {
		return "", 0, 0, false
	}
	var t, c int
	for _, r := range m[2] {
		t = t*10 + int(r-'0')
	}
	for _, r := range m[3] {
		c = c*10 + int(r-'0')
	}
	return m[1], t, c, true
}

func RenderCommentID(taskID string, n int) string {
	if n < 10000 {
		return fmt.Sprintf("%s-c%04d", taskID, n)
	}
	return fmt.Sprintf("%s-c%d", taskID, n)
}
```

Also add path helpers right after `taskPath` (around line 157):

```go
func (s *Store) commentsDir(code string) string {
	return filepath.Join(s.projectDir(code), "comments")
}
func (s *Store) commentPath(id string) string {
	code, _, _, ok := ParseCommentID(id)
	if !ok {
		return ""
	}
	return filepath.Join(s.commentsDir(code), id+".json")
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/store/ -run 'TestParseCommentID|TestRenderCommentID' -v`
Expected: PASS.

Then `go build ./...` and the full store test suite: `go test ./internal/store/ -v`.
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/store/store.go internal/store/comment_id_test.go
git commit -m "store: add ParseCommentID/RenderCommentID and path helpers"
```

---

### Task 3: Comment store operations — CreateComment and GetComment

**Files:**
- Create: `internal/store/comment.go` (new)
- Test: `internal/store/comment_test.go` (new)

**Interfaces:**
- Consumes: `Task`, `Comment`, `WithLock`, `GetTask`, `appendLogLocked`, `appendLabelUpsertsLocked`, `refreshDerivedLabelsLocked`, `WriteJSON`, `ReadJSON`, `ValidateLabelName`, `labelProjectExists`, `ParseCommentID`, `RenderCommentID`, `ParseTaskID`, `mustMarshal`, `Now`, `LastLogSeq`, `ReadLog`, `IsIntegrity`, `ErrNotFound`, `ErrUsage`, `ErrIntegrity`, `commentsDir`, `commentPath`, `taskPath`.
- Produces:
  - `func (s *Store) CreateComment(taskID, body string, labels []string, replyTo, actor string) (*Comment, error)`
  - `func (s *Store) GetComment(id string) (*Comment, error)`
  - `func (s *Store) ListComments(taskID string) ([]*Comment, error)`
  - `func (s *Store) lastCommentEventSeq(code, id string) (int, error)` (unexported)
  - `func (s *Store) rebuildCommentFromLog(id, code string) error` (unexported)

`CreateComment` flow (all under `WithLock`):
1. Verify `body != ""` and `actor != ""` (else `ErrUsage`).
2. `GetTask(taskID)` — ensures parent exists; returns its `code`, `NextCommentN` (`n`).
3. For each label: `ValidateLabelName(l)` + `s.labelProjectExists(l)`.
4. If `replyTo != ""`: `ParseCommentID(replyTo)` must `ok` and the parsed `code`+`taskN` must match `taskID`. No existence check.
5. `id := RenderCommentID(taskID, n)`. Compose `Comment` struct.
6. `appendLabelUpsertsLocked(code, labels, actor, ts)` — auto-registers new labels first.
7. `appendLogLocked(code, LogEntry{Action: ActionCommentCreated, Subject:{Kind:"comment", ID:id}, Payload: mustMarshal(c)})` — captures seq → `c.LogSeq`.
8. Bump `t.NextCommentN = n+1`; `t.UpdatedAt, t.UpdatedBy = ts, actor`. `appendLogLocked(code, LogEntry{Action: ActionTaskMetaChanged, Subject:{Kind:"task", ID:taskID}, Payload: mustMarshal(t)})` — captures seq → `t.LogSeq`.
9. `MkdirAll(commentsDir(code))`; `WriteJSON(commentPath(id), c)`; `WriteJSON(taskPath(taskID), t)`.
10. If new labels: `refreshDerivedLabelsLocked(code)`.

`GetComment(id)` flow (mirrors `GetTask`):
1. `ParseCommentID(id)` → `code`. Malformed → `ErrUsage`.
2. Read cache file. If missing or corrupt JSON → `rebuildCommentFromLog(id, code)` under `WithLock`, re-read.
3. Cache present: compare `cache.LogSeq` vs `lastCommentEventSeq(code, id)`. Stale → `rebuildCommentFromLog` under lock, re-read. Future seq (`> last`) → `ErrIntegrity`.
4. Return comment.

`ListComments(taskID)`:
1. `ParseTaskID(taskID) → code`. Malformed → `ErrUsage`.
2. `os.ReadDir(commentsDir(code))`; filter names to those whose prefix is `<taskID>-c` (the file name is `<comment-id>.json`); load each via `ReadJSON`; sort by ID; return.

`rebuildCommentFromLog(id, code)`:
- Walk `ReadLog(code)`, find entries with `Subject.Kind == "comment"` and `Subject.ID == id`, last-write-wins; `comment.removed` deletes; if nil at end → `ErrNotFound`. Write `WriteJSON(commentPath(id), &c)` with `c.LogSeq = lastSeq`.

`lastCommentEventSeq(code, id)`:
- Walk `ReadLog(code)`, track the last seq with `Subject.Kind=="comment"` and `Subject.ID==id`. Returns 0 if no event.

- [ ] **Step 1: Write the failing tests**

Create `internal/store/comment_test.go`:

```go
package store

import (
	"encoding/json"
	"os"
	"testing"
)

func TestCreateCommentAssignsPerTaskCounter(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", "claude")
	tk, _ := s.CreateTask("ATM", "t", "", nil, "claude")
	c1, err := s.CreateComment(tk.ID, "first", nil, "", "agent")
	if err != nil {
		t.Fatal(err)
	}
	c2, _ := s.CreateComment(tk.ID, "second", nil, "", "agent")
	if c1.ID != "ATM-0001-c0001" || c2.ID != "ATM-0001-c0002" {
		t.Fatalf("ids = %s, %s", c1.ID, c2.ID)
	}
	got, _ := s.GetTask(tk.ID)
	if got.NextCommentN != 3 {
		t.Fatalf("NextCommentN = %d want 3", got.NextCommentN)
	}
}

func TestCreateCommentAppendsLogEntriesInOrder(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", "claude")
	tk, _ := s.CreateTask("ATM", "t", "", nil, "claude")
	before, _ := s.LastLogSeq("ATM")
	_, _ = s.CreateComment(tk.ID, "first", []string{"ATM:comment:open-question"}, "", "claude")
	after, _ := s.LastLogSeq("ATM")
	// 1 label.upserted + 1 comment.created + 1 task.meta-changed = 3 entries.
	if after != before+3 {
		t.Fatalf("seq jumped %d → %d, want %d (label+comment+meta)", before, after, before+3)
	}
	entries, _ := s.ReadLog("ATM")
	var actions []string
	for _, e := range entries {
		if e.Seq > before {
			actions = append(actions, e.Action)
		}
	}
	want := []string{ActionLabelUpserted, ActionCommentCreated, ActionTaskMetaChanged}
	if len(actions) != 3 || actions[0] != want[0] || actions[1] != want[1] || actions[2] != want[2] {
		t.Fatalf("action order = %v want %v", actions, want)
	}
}

func TestCreateCommentReplyToSameTaskValidated(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", "claude")
	tk, _ := s.CreateTask("ATM", "t", "", nil, "claude")
	c1, _ := s.CreateComment(tk.ID, "first", nil, "", "claude")
	// Same task: ok
	c2, err := s.CreateComment(tk.ID, "reply", nil, c1.ID, "claude")
	if err != nil {
		t.Fatalf("same-task reply should be ok: %v", err)
	}
	if c2.ReplyTo != c1.ID {
		t.Fatalf("ReplyTo = %q want %q", c2.ReplyTo, c1.ID)
	}
	// Cross-task comment ID: reject
	tk2, _ := s.CreateTask("ATM", "other", "", nil, "claude")
	other1, _ := s.CreateComment(tk2.ID, "on other", nil, "", "claude")
	if _, err := s.CreateComment(tk.ID, "bad reply", nil, other1.ID, "claude"); !IsUsage(err) {
		t.Fatalf("cross-task ReplyTo should be ErrUsage, got %v", err)
	}
	// Malformed ReplyTo: reject
	if _, err := s.CreateComment(tk.ID, "bad", nil, "c0001", "claude"); !IsUsage(err) {
		t.Fatalf("malformed ReplyTo should be ErrUsage, got %v", err)
	}
	// Non-existent parent ID (no orphan check): ok — dangling pointer tolerated
	if _, err := s.CreateComment(tk.ID, "ok dangling", nil, "ATM-0001-c0099", "claude"); err != nil {
		t.Fatalf("non-existent ReplyTo should be allowed (no orphan check): %v", err)
	}
}

func TestCreateCommentRequiresBodyAndActor(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", "claude")
	tk, _ := s.CreateTask("ATM", "t", "", nil, "claude")
	if _, err := s.CreateComment(tk.ID, "", nil, "", "claude"); !IsUsage(err) {
		t.Fatalf("empty body should be ErrUsage, got %v", err)
	}
	if _, err := s.CreateComment(tk.ID, "x", nil, "", ""); !IsUsage(err) {
		t.Fatalf("empty actor should be ErrUsage, got %v", err)
	}
}

func TestGetCommentReturnsCreated(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", "claude")
	tk, _ := s.CreateTask("ATM", "t", "", nil, "claude")
	c, _ := s.CreateComment(tk.ID, "hello", []string{"ATM:comment:open-question"}, "", "claude")
	got, err := s.GetComment(c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Body != "hello" || len(got.Labels) != 1 || got.Labels[0] != "ATM:comment:open-question" {
		t.Fatalf("got = %+v", got)
	}
	if got.LogSeq != c.LogSeq {
		t.Fatalf("LogSeq mismatch: got %d want %d", got.LogSeq, c.LogSeq)
	}
}

func TestGetCommentMalformedID(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", "claude")
	if _, err := s.GetComment("ATM-0001"); !IsUsage(err) {
		t.Fatalf("malformed comment id should be ErrUsage, got %v", err)
	}
}

func TestGetCommentLazyMissRebuildsFromLog(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", "claude")
	tk, _ := s.CreateTask("ATM", "t", "", nil, "claude")
	c, _ := s.CreateComment(tk.ID, "persist", nil, "", "claude")
	// Hand-delete cache; next read must rebuild.
	_ = os.Remove(s.commentPath(c.ID))
	got, err := s.GetComment(c.ID)
	if err != nil {
		t.Fatalf("GetComment after cache delete: %v", err)
	}
	if got.Body != "persist" {
		t.Fatalf("rebuilt comment body = %q want %q", got.Body, "persist")
	}
	if _, err := os.Stat(s.commentPath(c.ID)); os.IsNotExist(err) {
		t.Fatal("cache file was not rewritten after lazy miss")
	}
}

func TestGetCommentFutureLogSeqIntegrity(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", "claude")
	tk, _ := s.CreateTask("ATM", "t", "", nil, "claude")
	c, _ := s.CreateComment(tk.ID, "x", nil, "", "claude")
	c.LogSeq = 9999
	newRaw, _ := json.Marshal(c)
	_ = os.WriteFile(s.commentPath(c.ID), newRaw, 0o644)
	_, err := s.GetComment(c.ID)
	if !IsIntegrity(err) {
		t.Fatalf("expected ErrIntegrity, got %v", err)
	}
}

func TestListCommentsSortedAndFilteredByTask(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", "claude")
	tk, _ := s.CreateTask("ATM", "t1", "", nil, "claude")
	tk2, _ := s.CreateTask("ATM", "t2", "", nil, "claude")
	_, _ = s.CreateComment(tk.ID, "a", nil, "", "claude")
	_, _ = s.CreateComment(tk2.ID, "on other", nil, "", "claude")
	_, _ = s.CreateComment(tk.ID, "c", nil, "", "claude")
	got, err := s.ListComments(tk.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 comments on tk, got %d", len(got))
	}
	if got[0].ID >= got[1].ID {
		t.Fatalf("comments not sorted ascending: %s, %s", got[0].ID, got[1].ID)
	}
	for _, c := range got {
		if c.TaskID != tk.ID {
			t.Fatalf("comment from other task in list: %+v", c)
		}
	}
}

func TestListCommentsEmpty(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", "claude")
	tk, _ := s.CreateTask("ATM", "t", "", nil, "claude")
	got, err := s.ListComments(tk.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got != nil && len(got) != 0 {
		t.Fatalf("expected empty slice, got %+v", got)
	}
}

func TestParseReplayNextCommentNFromMetaChanged(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", "claude")
	tk, _ := s.CreateTask("ATM", "t", "", nil, "claude")
	_, _ = s.CreateComment(tk.ID, "first", nil, "", "claude")
	// Delete the task cache and let it rebuild from log; counter must come back.
	_ = os.Remove(s.taskPath(tk.ID))
	got, err := s.GetTask(tk.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.NextCommentN != 1 {
		t.Fatalf("replay-derived NextCommentN = %d want 1", got.NextCommentN)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/store/ -run 'TestCreateComment|TestGetComment|TestListComments|TestParseReplayNextCommentN' -v`
Expected: FAIL with compile errors — `CreateComment`/`GetComment`/`ListComments` undefined.

- [ ] **Step 3: Implement `comment.go`**

Create `internal/store/comment.go`:

```go
package store

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
)

func (s *Store) CreateComment(taskID, body string, labels []string, replyTo, actor string) (*Comment, error) {
	if body == "" {
		return nil, fmt.Errorf("%w: body is required", ErrUsage)
	}
	if actor == "" {
		return nil, fmt.Errorf("%w: actor is required", ErrUsage)
	}
	code, _, ok := ParseTaskID(taskID)
	if !ok {
		return nil, fmt.Errorf("%w: invalid task id %q", ErrUsage, taskID)
	}
	if replyTo != "" {
		rcode, _, _, ok := ParseCommentID(replyTo)
		if !ok {
			return nil, fmt.Errorf("%w: invalid reply-to %q", ErrUsage, replyTo)
		}
		if rcode != code {
			return nil, fmt.Errorf("%w: reply-to %q must belong to the same project as task %q", ErrUsage, replyTo, taskID)
		}
		_, rtaskN, _, _ := ParseCommentID(replyTo)
		_, ttaskN, _, _ := ParseTaskID(taskID)
		if rtaskN != ttaskN {
			return nil, fmt.Errorf("%w: reply-to %q must belong to task %q", ErrUsage, replyTo, taskID)
		}
	}
	for _, l := range labels {
		if err := ValidateLabelName(l); err != nil {
			return nil, err
		}
		if err := s.labelProjectExists(l); err != nil {
			return nil, err
		}
	}
	var created *Comment
	err := s.WithLock(code, func() error {
		t, err := s.GetTask(taskID)
		if err != nil {
			return err
		}
		n := t.NextCommentN
		id := RenderCommentID(taskID, n)
		ts := Now()
		labelsSorted := append([]string(nil), labels...)
		sort.Strings(labelsSorted)
		c := &Comment{
			ID:        id,
			TaskID:    taskID,
			ReplyTo:   replyTo,
			Body:      body,
			Labels:    labelsSorted,
			CreatedAt: ts,
			CreatedBy: actor,
			UpdatedAt: ts,
			UpdatedBy: actor,
		}
		labelEntries, err := s.appendLabelUpsertsLocked(code, labels, actor, ts)
		if err != nil {
			return err
		}
		entry, err := s.appendLogLocked(code, LogEntry{
			At:      ts,
			Actor:   actor,
			Action:  ActionCommentCreated,
			Subject: Subject{Kind: "comment", ID: id},
			Payload: mustMarshal(c),
		})
		if err != nil {
			return err
		}
		c.LogSeq = entry.Seq
		t.NextCommentN = n + 1
		t.UpdatedAt = ts
		t.UpdatedBy = actor
		metaEntry, err := s.appendLogLocked(code, LogEntry{
			At:      ts,
			Actor:   actor,
			Action:  ActionTaskMetaChanged,
			Subject: Subject{Kind: "task", ID: taskID},
			Payload: mustMarshal(t),
		})
		if err != nil {
			return err
		}
		t.LogSeq = metaEntry.Seq
		if err := os.MkdirAll(s.commentsDir(code), 0o755); err != nil {
			return err
		}
		if err := WriteJSON(s.commentPath(id), c); err != nil {
			return err
		}
		if err := WriteJSON(s.taskPath(taskID), t); err != nil {
			return err
		}
		if len(labelEntries) > 0 {
			if err := s.refreshDerivedLabelsLocked(code); err != nil {
				return err
			}
		}
		created = c
		return nil
	})
	if err != nil {
		return nil, err
	}
	return created, nil
}

func (s *Store) GetComment(id string) (*Comment, error) {
	code, _, _, ok := ParseCommentID(id)
	if !ok {
		return nil, fmt.Errorf("%w: invalid comment id %q", ErrUsage, id)
	}
	var c Comment
	cachePath := s.commentPath(id)
	if err := ReadJSON(cachePath, &c); err != nil {
		if !os.IsNotExist(err) {
			if err := s.WithLock(code, func() error {
				return s.rebuildCommentFromLog(id, code)
			}); err != nil {
				return nil, err
			}
			if err := ReadJSON(cachePath, &c); err != nil {
				return nil, err
			}
			return &c, nil
		}
		if err := s.WithLock(code, func() error {
			return s.rebuildCommentFromLog(id, code)
		}); err != nil {
			return nil, err
		}
		if err := ReadJSON(cachePath, &c); err != nil {
			return nil, err
		}
		return &c, nil
	}
	last, lastErr := s.LastLogSeq(code)
	if lastErr != nil {
		return nil, lastErr
	}
	if c.LogSeq > last {
		return nil, fmt.Errorf("%w: comment %q cache LogSeq=%d > log LastSeq=%d", ErrIntegrity, id, c.LogSeq, last)
	}
	commentLast, err := s.lastCommentEventSeq(code, id)
	if err != nil {
		return nil, err
	}
	if c.LogSeq < commentLast {
		if err := s.WithLock(code, func() error {
			return s.rebuildCommentFromLog(id, code)
		}); err != nil {
			return nil, err
		}
		if err := ReadJSON(cachePath, &c); err != nil {
			return nil, err
		}
	}
	return &c, nil
}

func (s *Store) lastCommentEventSeq(code, id string) (int, error) {
	entries, err := s.ReadLog(code)
	if err != nil {
		return 0, err
	}
	last := 0
	for _, e := range entries {
		if e.Subject.Kind == "comment" && e.Subject.ID == id {
			last = e.Seq
		}
	}
	return last, nil
}

func (s *Store) rebuildCommentFromLog(id, code string) error {
	entries, err := s.ReadLog(code)
	if err != nil && !IsIntegrity(err) {
		return err
	}
	var c *Comment
	lastSeq := 0
	for _, e := range entries {
		if e.Subject.Kind != "comment" || e.Subject.ID != id {
			continue
		}
		lastSeq = e.Seq
		if e.Action == ActionCommentRemoved {
			c = nil
			continue
		}
		var cc Comment
		if err := json.Unmarshal(e.Payload, &cc); err == nil {
			c = &cc
		}
	}
	if c == nil {
		return fmt.Errorf("%w: comment %q", ErrNotFound, id)
	}
	c.LogSeq = lastSeq
	if err := os.MkdirAll(s.commentsDir(code), 0o755); err != nil {
		return err
	}
	return WriteJSON(s.commentPath(id), c)
}

func (s *Store) ListComments(taskID string) ([]*Comment, error) {
	code, _, ok := ParseTaskID(taskID)
	if !ok {
		return nil, fmt.Errorf("%w: invalid task id %q", ErrUsage, taskID)
	}
	entries, err := os.ReadDir(s.commentsDir(code))
	if err != nil {
		if os.IsNotExist(err) {
			return []*Comment{}, nil
		}
		return nil, err
	}
	prefix := taskID + "-c"
	var out []*Comment
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if len(name) < len(prefix) || name[:len(prefix)] != prefix {
			continue
		}
		var c Comment
		if err := ReadJSON(filepath.Join(s.commentsDir(code), name), &c); err != nil {
			continue
		}
		out = append(out, &c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}
```

You'll need to add `path/filepath` to the imports.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/store/ -run 'TestCreateComment|TestGetComment|TestListComments|TestParseReplayNextCommentN' -v`
Expected: PASS.

Then full store suite: `go test ./internal/store/ -v`
Expected: PASS — no regressions.

- [ ] **Step 5: Commit**

```bash
git add internal/store/comment.go internal/store/comment_test.go
git commit -m "store: add CreateComment/GetComment/ListComments with lazy self-heal"
```

---

### Task 4: Comment mutations — SetCommentBody, label add/remove, RemoveComment

**Files:**
- Modify: `internal/store/comment.go` (add three functions)
- Test: `internal/store/comment_test.go` (extend with new tests)

**Interfaces:**
- Consumes: same as Task 3 plus `LogEntry`, `WriteJSON`.
- Produces:
  - `func (s *Store) SetCommentBody(id, body, actor string) error`
  - `func (s *Store) CommentLabelAdd(id, label, actor string) error`
  - `func (s *Store) CommentLabelRemove(id, label, actor string) error`
  - `func (s *Store) RemoveComment(id, actor string) error`

All four mirror the existing task helpers (`mutateTask`, `TaskLabelAdd`, `TaskLabelRemove`, `RemoveTask`) one-for-one. They take the same project lock, route label events through `appendLabelUpsertsLocked` + `refreshDerivedLabelsLocked`, write the full after-state to log + cache, and write a tombstone for `RemoveComment`.

- [ ] **Step 1: Write the failing tests**

Append to `internal/store/comment_test.go`:

```go
func TestSetCommentBodyAppendsAndUpdates(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", "claude")
	tk, _ := s.CreateTask("ATM", "t", "", nil, "claude")
	c, _ := s.CreateComment(tk.ID, "original", nil, "", "claude")
	before, _ := s.LastLogSeq("ATM")
	if err := s.SetCommentBody(c.ID, "edited", "ttran"); err != nil {
		t.Fatal(err)
	}
	after, _ := s.LastLogSeq("ATM")
	if after != before+1 {
		t.Fatalf("seq jumped %d → %d, want %d (comment.body-changed)", before, after, before+1)
	}
	got, _ := s.GetComment(c.ID)
	if got.Body != "edited" {
		t.Fatalf("body = %q want edited", got.Body)
	}
	if got.UpdatedBy != "ttran" {
		t.Fatalf("updated_by = %q want ttran", got.UpdatedBy)
	}
	hv := s.History("ATM", Subject{Kind: "comment", ID: c.ID})
	if len(hv) != 2 || hv[1].Action != ActionCommentBodyChanged {
		t.Fatalf("history = %+v", hv)
	}
}

func TestCommentLabelAddAutoRegistersAndAppends(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", "claude")
	tk, _ := s.CreateTask("ATM", "t", "", nil, "claude")
	c, _ := s.CreateComment(tk.ID, "body", nil, "", "claude")
	before, _ := s.LastLogSeq("ATM")
	if err := s.CommentLabelAdd(c.ID, "ATM:comment:clarification", "claude"); err != nil {
		t.Fatal(err)
	}
	after, _ := s.LastLogSeq("ATM")
	if after != before+2 {
		t.Fatalf("seq jumped %d → %d, want %d (label.upserted + comment.label-added)", before, after, before+2)
	}
	if _, err := s.LabelShow("ATM:comment:clarification"); err != nil {
		t.Fatalf("label not auto-registered: %v", err)
	}
}

func TestCommentLabelAddDedup(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", "claude")
	tk, _ := s.CreateTask("ATM", "t", "", nil, "claude")
	c, _ := s.CreateComment(tk.ID, "body", []string{"ATM:comment:open-question"}, "", "claude")
	before, _ := s.LastLogSeq("ATM")
	_ = s.CommentLabelAdd(c.ID, "ATM:comment:open-question", "claude")
	after, _ := s.LastLogSeq("ATM")
	if after != before {
		t.Fatalf("dup label add should append nothing, got %d → %d", before, after)
	}
}

func TestCommentLabelRemoveDoesNotTouchRegistry(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", "claude")
	tk, _ := s.CreateTask("ATM", "t", "", nil, "claude")
	c, _ := s.CreateComment(tk.ID, "body", []string{"ATM:comment:open-question"}, "", "claude")
	before, _ := s.LastLogSeq("ATM")
	if err := s.CommentLabelRemove(c.ID, "ATM:comment:open-question", "claude"); err != nil {
		t.Fatal(err)
	}
	after, _ := s.LastLogSeq("ATM")
	if after != before+1 {
		t.Fatalf("seq jumped %d → %d, want %d (comment.label-removed)", before, after, before+1)
	}
	if _, err := s.LabelShow("ATM:comment:open-question"); err != nil {
		t.Fatalf("registry must still contain label: %v", err)
	}
}

func TestRemoveCommentAppendsTombstoneAndDeletesCache(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", "claude")
	tk, _ := s.CreateTask("ATM", "t", "", nil, "claude")
	c, _ := s.CreateComment(tk.ID, "doomed", nil, "", "claude")
	before, _ := s.LastLogSeq("ATM")
	if err := s.RemoveComment(c.ID, "claude"); err != nil {
		t.Fatal(err)
	}
	after, _ := s.LastLogSeq("ATM")
	if after != before+1 {
		t.Fatalf("seq jumped %d → %d, want %d (comment.removed tombstone)", before, after, before+1)
	}
	if _, err := s.GetComment(c.ID); !IsNotFound(err) {
		t.Fatalf("GetComment after remove: %v want ErrNotFound", err)
	}
	if _, err := os.Stat(s.commentPath(c.ID)); !os.IsNotExist(err) {
		t.Fatal("cache file must be deleted")
	}
	hv := s.History("ATM", Subject{Kind: "comment", ID: c.ID})
	if len(hv) == 0 || hv[len(hv)-1].Action != ActionCommentRemoved {
		t.Fatalf("tombstone missing from history: %+v", hv)
	}
	st, _ := s.Replay("ATM")
	for _, cc := range st.Comments {
		if cc.ID == c.ID {
			t.Fatal("tombstoned comment appeared in replay live set")
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/store/ -run 'TestSetCommentBody|TestCommentLabel|TestRemoveComment' -v`
Expected: FAIL — functions undefined.

- [ ] **Step 3: Implement the four functions**

Append to `internal/store/comment.go`:

```go
func (s *Store) SetCommentBody(id, body, actor string) error {
	if body == "" {
		return fmt.Errorf("%w: body is required", ErrUsage)
	}
	if actor == "" {
		return fmt.Errorf("%w: actor is required", ErrUsage)
	}
	return s.mutateComment(id, actor, func(c *Comment, now time.Time) {
		c.Body = body
	}, ActionCommentBodyChanged)
}

func (s *Store) CommentLabelRemove(id, label, actor string) error {
	return s.mutateComment(id, actor, func(c *Comment, now time.Time) {
		out := c.Labels[:0]
		for _, l := range c.Labels {
			if l != label {
				out = append(out, l)
			}
		}
		c.Labels = out
	}, ActionCommentLabelRemoved)
}

func (s *Store) RemoveComment(id, actor string) error {
	if actor == "" {
		return fmt.Errorf("%w: actor is required", ErrUsage)
	}
	code, _, _, ok := ParseCommentID(id)
	if !ok {
		return fmt.Errorf("%w: invalid comment id %q", ErrUsage, id)
	}
	return s.WithLock(code, func() error {
		c, err := s.GetComment(id)
		if err != nil {
			return err
		}
		now := Now()
		c.UpdatedAt = now
		c.UpdatedBy = actor
		if _, err := s.appendLogLocked(code, LogEntry{
			At:      now,
			Actor:   actor,
			Action:  ActionCommentRemoved,
			Subject: Subject{Kind: "comment", ID: id},
			Payload: mustMarshal(c),
		}); err != nil {
			return err
		}
		return os.Remove(s.commentPath(id))
	})
}

func (s *Store) CommentLabelAdd(id, label, actor string) error {
	if err := ValidateLabelName(label); err != nil {
		return err
	}
	if err := s.labelProjectExists(label); err != nil {
		return err
	}
	if actor == "" {
		return fmt.Errorf("%w: actor is required", ErrUsage)
	}
	code, _, _, ok := ParseCommentID(id)
	if !ok {
		return fmt.Errorf("%w: invalid comment id %q", ErrUsage, id)
	}
	return s.WithLock(code, func() error {
		c, err := s.GetComment(id)
		if err != nil {
			return err
		}
		for _, l := range c.Labels {
			if l == label {
				return nil
			}
		}
		c.Labels = append(c.Labels, label)
		sort.Strings(c.Labels)
		labelEntries, err := s.appendLabelUpsertsLocked(code, []string{label}, actor, Now())
		if err != nil {
			return err
		}
		now := Now()
		c.UpdatedAt = now
		c.UpdatedBy = actor
		entry, err := s.appendLogLocked(code, LogEntry{
			At:      now,
			Actor:   actor,
			Action:  ActionCommentLabelAdded,
			Subject: Subject{Kind: "comment", ID: id},
			Payload: mustMarshal(c),
		})
		if err != nil {
			return err
		}
		c.LogSeq = entry.Seq
		if err := WriteJSON(s.commentPath(id), c); err != nil {
			return err
		}
		if len(labelEntries) > 0 {
			return s.refreshDerivedLabelsLocked(code)
		}
		return nil
	})
}

func (s *Store) mutateComment(id, actor string, fn func(c *Comment, now time.Time), action string) error {
	if actor == "" {
		return fmt.Errorf("%w: actor is required", ErrUsage)
	}
	code, _, _, ok := ParseCommentID(id)
	if !ok {
		return fmt.Errorf("%w: invalid comment id %q", ErrUsage, id)
	}
	return s.WithLock(code, func() error {
		c, err := s.GetComment(id)
		if err != nil {
			return err
		}
		now := Now()
		fn(c, now)
		c.UpdatedAt = now
		c.UpdatedBy = actor
		entry, err := s.appendLogLocked(code, LogEntry{
			At:      now,
			Actor:   actor,
			Action:  action,
			Subject: Subject{Kind: "comment", ID: id},
			Payload: mustMarshal(c),
		})
		if err != nil {
			return err
		}
		c.LogSeq = entry.Seq
		return WriteJSON(s.commentPath(id), c)
	})
}
```

Add `time` to the `comment.go` import block.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/store/ -run 'TestSetCommentBody|TestCommentLabel|TestRemoveComment' -v`
Expected: PASS.

Then full store suite: `go test ./internal/store/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/store/comment.go internal/store/comment_test.go
git commit -m "store: add SetCommentBody/CommentLabelAdd/Remove/RemoveComment"
```

---

### Task 5: Rebuild and Verify support for comments

**Files:**
- Modify: `internal/store/rebuild.go`
- Modify: `internal/store/verify.go`
- Test: `internal/store/rebuild_test.go` (extend)
- Test: `internal/store/verify_test.go` (extend)

**Interfaces:**
- Consumes: `ReplayState.Comments` (Task 1).
- Produces: `Rebuild` writes comment caches and sweeps orphans; `VerifyProject` reports per-comment `CacheCheck`s.

- [ ] **Step 1: Write the failing tests**

Append to `internal/store/rebuild_test.go`:

```go
func TestRebuildWritesCommentCachesAndSweepsOrphans(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", "claude")
	tk, _ := s.CreateTask("ATM", "t", "", nil, "claude")
	c, _ := s.CreateComment(tk.ID, "hello", nil, "", "claude")
	// Hand-add an orphan comment cache file (no log entry for it).
	orphan := Comment{ID: "ATM-0001-c0099", TaskID: tk.ID, Body: "orphan"}
	orphanRaw, _ := json.Marshal(orphan)
	_ = os.MkdirAll(s.commentsDir("ATM"), 0o755)
	_ = os.WriteFile(s.commentPath("ATM-0001-c0099"), orphanRaw, 0o644)
	// Hand-delete the live comment cache.
	_ = os.Remove(s.commentPath(c.ID))
	if _, err := s.Rebuild(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(s.commentPath(c.ID)); os.IsNotExist(err) {
		t.Fatal("live comment cache not rebuilt")
	}
	if _, err := os.Stat(s.commentPath("ATM-0001-c0099")); !os.IsNotExist(err) {
		t.Fatal("orphan comment cache not swept")
	}
}
```

(This file already imports `encoding/json` and `os`; verify the imports are present.)

Append to `internal/store/verify_test.go`:

```go
func TestVerifyReportsCommentCacheStale(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", "claude")
	tk, _ := s.CreateTask("ATM", "t", "", nil, "claude")
	c, _ := s.CreateComment(tk.ID, "x", nil, "", "claude")
	// Stomp cache to a stale seq.
	raw, _ := os.ReadFile(s.commentPath(c.ID))
	var cc Comment
	_ = json.Unmarshal(raw, &cc)
	cc.LogSeq = 0
	newRaw, _ := json.Marshal(cc)
	_ = os.WriteFile(s.commentPath(c.ID), newRaw, 0o644)
	rep, err := s.VerifyProject("ATM")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, ck := range rep.Caches {
		if ck.Path == s.commentPath(c.ID) {
			if ck.Status == "stale" || ck.Status == "corrupt" {
				found = true
			}
		}
	}
	if !found {
		t.Fatalf("comment cache stale not reported: %+v", rep.Caches)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/store/ -run 'TestRebuildWritesComment|TestVerifyReportsComment' -v`
Expected: FAIL — orphan not swept, stale not reported.

- [ ] **Step 3: Extend `Rebuild`**

In `internal/store/rebuild.go`, inside `func (s *Store) Rebuild()` (after the task-cache sweep loop, before `// Merge labels.`):

```go
		// Rebuild comment caches. Delete caches for tombstoned comments.
		liveComments := map[string]bool{}
		for _, c := range st.Comments {
			if err := WriteJSON(s.commentPath(c.ID), c); err != nil {
				return rep, err
			}
			liveComments[c.ID] = true
		}
		commentEntries, _ := os.ReadDir(s.commentsDir(p.Code))
		for _, e := range commentEntries {
			if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
				continue
			}
			cid := e.Name()[:len(e.Name())-len(".json")]
			if !liveComments[cid] {
				_ = os.Remove(filepath.Join(s.commentsDir(p.Code), e.Name()))
			}
		}
```

- [ ] **Step 4: Extend `VerifyProject`**

In `internal/store/verify.go` `VerifyProject`, after the task cache loop (around line 67):

```go
	// Verify each comment cache.
	for _, c := range st.Comments {
		report.Caches = append(report.Caches, s.checkCommentCache(code, c.ID, c.LogSeq))
	}
```

Add the helper:

```go
func (s *Store) checkCommentCache(code, id string, expectedLogSeq int) CacheCheck {
	path := s.commentPath(id)
	var c Comment
	if err := ReadJSON(path, &c); err != nil {
		if os.IsNotExist(err) {
			return CacheCheck{Path: path, Status: "missing"}
		}
		return CacheCheck{Path: path, Status: "corrupt"}
	}
	last, _ := s.lastCommentEventSeq(code, id)
	if c.LogSeq > last {
		return CacheCheck{Path: path, Status: "corrupt", CacheLogSeq: c.LogSeq, LastEventSeq: last}
	}
	if c.LogSeq < last {
		return CacheCheck{Path: path, Status: "stale", CacheLogSeq: c.LogSeq, LastEventSeq: last}
	}
	return CacheCheck{Path: path, Status: "ok", CacheLogSeq: c.LogSeq, LastEventSeq: last}
}
```

Also add a sweep for orphan comment caches in `VerifyProject` (after the comment cache check loop):

```go
	// Sweep orphan comment caches (no replay comment for the file).
	commentEntries, _ := os.ReadDir(s.commentsDir(code))
	for _, e := range commentEntries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		cid := e.Name()[:len(e.Name())-len(".json")]
		live := false
		for _, c := range st.Comments {
			if c.ID == cid {
				live = true
				break
			}
		}
		if !live {
			report.Caches = append(report.Caches, CacheCheck{
				Path:   filepath.Join(s.commentsDir(code), e.Name()),
				Status: "corrupt",
			})
			report.Diverged = true
		}
	}
```

Add `path/filepath` to `verify.go` imports.

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/store/ -run 'TestRebuildWritesComment|TestVerifyReportsComment' -v`
Expected: PASS.

Then full store suite: `go test ./internal/store/ -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/store/rebuild.go internal/store/verify.go internal/store/rebuild_test.go internal/store/verify_test.go
git commit -m "store: rebuild+verify handle comment caches"
```

---

### Task 6: CLI — output mappers and `atm task comment` commands

**Files:**
- Modify: `internal/cli/output.go` (add `jsonComment`, `commentToJSON`, `commentsToJSON`, `renderCommentListText`, `renderCommentText`)
- Create: `internal/cli/comment.go` (new — `newTaskCommentCmd` and 7 verbs)
- Modify: `internal/cli/task.go` (one line: `cmd.AddCommand(newTaskCommentCmd(st))`)
- Test: `internal/cli/comment_test.go` (new)
- Test fixtures: `internal/cli/testdata/golden/comment-*.json` (new, generated via `-update`)

**Interfaces:**
- Consumes: store functions from Tasks 3 & 4 (`CreateComment`, `GetComment`, `ListComments`, `SetCommentBody`, `CommentLabelAdd`, `CommentLabelRemove`, `RemoveComment`); existing CLI helpers `cliState`, `resolveActor`, `openStore`, `emit`, `writeJSON`, `MarshalSorted`, `historyToJSON`, `formatLabels`, `compareGolden`, `goldenHarness.seedScenario1`.
- Produces:
  - `jsonComment`, `commentToJSON(c *store.Comment, hv []store.HistoryView) jsonComment`, `commentsToJSON(cs []*store.Comment) []jsonComment`
  - `renderCommentListText(cs []jsonComment) string`, `renderCommentText(c jsonComment) string`
  - `newTaskCommentCmd(st *cliState) *cobra.Command` with subcommands `add`, `list`, `show`, `set-body`, `label add`, `label remove`, `remove`

- [ ] **Step 1: Write the failing test**

Create `internal/cli/comment_test.go`:

```go
package cli

import "testing"

func TestGoldenCommentAdd(t *testing.T) {
	h := newGoldenHarness(t)
	h.seedScenario1()
	out, _, code := h.run("task", "comment", "add", "--task", "ATM-0001", "--body", "First comment",
		"--label", "ATM:comment:open-question", "--actor", "claude")
	if code != 0 {
		t.Fatalf("exit = %d stderr=%s", code, h.stderr.String())
	}
	compareGolden(t, "comment-add", out)
}

func TestGoldenCommentList(t *testing.T) {
	h := newGoldenHarness(t)
	h.seedScenario1()
	h.run("task", "comment", "add", "--task", "ATM-0001", "--body", "First", "--actor", "claude")
	h.run("task", "comment", "add", "--task", "ATM-0001", "--body", "Second",
		"--label", "ATM:comment:clarification", "--actor", "claude")
	out, _, code := h.run("task", "comment", "list", "--task", "ATM-0001")
	if code != 0 {
		t.Fatalf("exit = %d stderr=%s", code, h.stderr.String())
	}
	compareGolden(t, "comment-list", out)
}

func TestGoldenCommentShow(t *testing.T) {
	h := newGoldenHarness(t)
	h.seedScenario1()
	h.run("task", "comment", "add", "--task", "ATM-0001", "--body", "Body here", "--actor", "claude")
	out, _, code := h.run("task", "comment", "show", "--id", "ATM-0001-c0001")
	if code != 0 {
		t.Fatalf("exit = %d stderr=%s", code, h.stderr.String())
	}
	compareGolden(t, "comment-show", out)
}

func TestGoldenCommentSetBody(t *testing.T) {
	h := newGoldenHarness(t)
	h.seedScenario1()
	h.run("task", "comment", "add", "--task", "ATM-0001", "--body", "orig", "--actor", "claude")
	out, _, code := h.run("task", "comment", "set-body", "--id", "ATM-0001-c0001", "--body", "new", "--actor", "claude")
	if code != 0 {
		t.Fatalf("exit = %d stderr=%s", code, h.stderr.String())
	}
	compareGolden(t, "comment-set-body", out)
}

func TestGoldenCommentLabelAddRemove(t *testing.T) {
	h := newGoldenHarness(t)
	h.seedScenario1()
	h.run("task", "comment", "add", "--task", "ATM-0001", "--body", "x", "--actor", "claude")
	outAdd, _, code := h.run("task", "comment", "label", "add", "--id", "ATM-0001-c0001",
		"--label", "ATM:comment:open-question", "--actor", "claude")
	if code != 0 {
		t.Fatalf("add exit = %d stderr=%s", code, h.stderr.String())
	}
	compareGolden(t, "comment-label-add", outAdd)
	outRem, _, code := h.run("task", "comment", "label", "remove", "--id", "ATM-0001-c0001",
		"--label", "ATM:comment:open-question", "--actor", "claude")
	if code != 0 {
		t.Fatalf("remove exit = %d stderr=%s", code, h.stderr.String())
	}
	compareGolden(t, "comment-label-remove", outRem)
}

func TestGoldenCommentRemove(t *testing.T) {
	h := newGoldenHarness(t)
	h.seedScenario1()
	h.run("task", "comment", "add", "--task", "ATM-0001", "--body", "gone", "--actor", "claude")
	out, _, code := h.run("task", "comment", "remove", "--id", "ATM-0001-c0001", "--actor", "claude")
	if code != 0 {
		t.Fatalf("exit = %d stderr=%s", code, h.stderr.String())
	}
	compareGolden(t, "comment-remove", out)
}

func TestCommentAddRequiresActor(t *testing.T) {
	h := newGoldenHarness(t)
	h.seedScenario1()
	_, _, code := h.run("task", "comment", "add", "--task", "ATM-0001", "--body", "x")
	if code != ExitUsage {
		t.Fatalf("expected exit 2 (missing actor), got %d", code)
	}
}

func TestCommentListEmpty(t *testing.T) {
	h := newGoldenHarness(t)
	h.seedScenario1()
	out, _, code := h.run("task", "comment", "list", "--task", "ATM-0001")
	if code != 0 {
		t.Fatalf("exit = %d stderr=%s", code, h.stderr.String())
	}
	compareGolden(t, "comment-list-empty", out)
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli/ -run 'TestGoldenComment|TestComment' -v`
Expected: FAIL — `task comment` subcommand unknown.

- [ ] **Step 3: Add output mappers in `output.go`**

Append to `internal/cli/output.go`:

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

func commentToJSON(c *store.Comment, hv []store.HistoryView) jsonComment {
	return jsonComment{
		ID:        c.ID,
		TaskID:    c.TaskID,
		ReplyTo:   c.ReplyTo,
		Body:      c.Body,
		Labels:    normalizeStrSlice(c.Labels),
		LogSeq:    c.LogSeq,
		History:   historyToJSON(hv),
		CreatedAt: store.RFC3339UTC(c.CreatedAt),
		CreatedBy: c.CreatedBy,
		UpdatedAt: store.RFC3339UTC(c.UpdatedAt),
		UpdatedBy: c.UpdatedBy,
	}
}

func commentsToJSON(cs []*store.Comment) []jsonComment {
	out := make([]jsonComment, 0, len(cs))
	for _, c := range cs {
		out = append(out, commentToJSON(c, nil))
	}
	return out
}

func renderCommentListText(cs []jsonComment) string {
	var b strings.Builder
	for _, c := range cs {
		fmt.Fprintf(&b, "%s\t%s\t%s\t%s\n", c.ID, c.CreatedAt, c.CreatedBy, formatLabels(c.Labels))
		for _, line := range strings.Split(c.Body, "\n") {
			fmt.Fprintf(&b, "    %s\n", line)
		}
	}
	return b.String()
}

func renderCommentText(c jsonComment) string {
	var b strings.Builder
	fmt.Fprintf(&b, "id      %s\n", c.ID)
	fmt.Fprintf(&b, "task    %s\n", c.TaskID)
	if c.ReplyTo != "" {
		fmt.Fprintf(&b, "reply-to %s\n", c.ReplyTo)
	}
	fmt.Fprintf(&b, "actor   %s\n", c.CreatedBy)
	fmt.Fprintf(&b, "created %s\n", c.CreatedAt)
	fmt.Fprintf(&b, "updated %s  by %s\n", c.UpdatedAt, c.UpdatedBy)
	fmt.Fprintf(&b, "labels  %s\n", formatLabels(c.Labels))
	b.WriteString("\n")
	b.WriteString(c.Body)
	b.WriteString("\n")
	return b.String()
}
```

If `strings` is not already imported in `output.go`, it is (line 5). `fmt` is also already imported.

- [ ] **Step 4: Create `internal/cli/comment.go`**

```go
package cli

import (
	"fmt"
	"os"

	"atm/internal/store"

	"github.com/spf13/cobra"
)

func newTaskCommentCmd(st *cliState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "comment",
		Short: "Task comment commands",
	}
	cmd.AddCommand(newCommentAddCmd(st))
	cmd.AddCommand(newCommentListCmd(st))
	cmd.AddCommand(newCommentShowCmd(st))
	cmd.AddCommand(newCommentSetBodyCmd(st))
	cmd.AddCommand(newCommentLabelCmd(st))
	cmd.AddCommand(newCommentRemoveCmd(st))
	return cmd
}

func newCommentAddCmd(st *cliState) *cobra.Command {
	var task, body, replyTo string
	var labels []string
	cmd := &cobra.Command{
		Use:   "add",
		Short: "Add a comment to a task",
		RunE: func(cmd *cobra.Command, args []string) error {
			actor, err := st.resolveActor(true)
			if err != nil {
				return err
			}
			s, err := st.openStore()
			if err != nil {
				return err
			}
			c, err := s.CreateComment(task, body, labels, replyTo, actor)
			if err != nil {
				return err
			}
			return st.emit(st.stdout(), map[string]any{"comment": commentToJSON(c, nil)}, func() {
				fmt.Fprintf(os.Stdout, "created comment %s\n", c.ID)
			})
		},
	}
	cmd.Flags().StringVar(&task, "task", "", "task id")
	cmd.Flags().StringVar(&body, "body", "", "comment body (free-form prose)")
	cmd.Flags().StringArrayVar(&labels, "label", nil, "comment label (repeatable; full name e.g. ATM:comment:open-question)")
	cmd.Flags().StringVar(&replyTo, "reply-to", "", "optional comment id this replies to (same task)")
	_ = cmd.MarkFlagRequired("task")
	_ = cmd.MarkFlagRequired("body")
	return cmd
}

func newCommentListCmd(st *cliState) *cobra.Command {
	var task string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List comments on a task",
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := st.openStore()
			if err != nil {
				return err
			}
			cs, err := s.ListComments(task)
			if err != nil {
				return err
			}
			return st.emit(st.stdout(), map[string]any{"comments": commentsToJSON(cs)}, func() {
				fmt.Fprint(os.Stdout, renderCommentListText(commentsToJSON(cs)))
			})
		},
	}
	cmd.Flags().StringVar(&task, "task", "", "task id")
	_ = cmd.MarkFlagRequired("task")
	return cmd
}

func newCommentShowCmd(st *cliState) *cobra.Command {
	var id string
	cmd := &cobra.Command{
		Use:   "show",
		Short: "Show a comment",
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := st.openStore()
			if err != nil {
				return err
			}
			c, err := s.GetComment(id)
			if err != nil {
				return err
			}
			code, _, _, _ := store.ParseCommentID(id)
			hv := s.History(code, store.Subject{Kind: "comment", ID: c.ID})
			return st.emit(st.stdout(), map[string]any{"comment": commentToJSON(c, hv)}, func() {
				fmt.Fprint(os.Stdout, renderCommentText(commentToJSON(c, hv)))
			})
		},
	}
	cmd.Flags().StringVar(&id, "id", "", "comment id")
	_ = cmd.MarkFlagRequired("id")
	return cmd
}

func newCommentSetBodyCmd(st *cliState) *cobra.Command {
	var id, body string
	cmd := &cobra.Command{
		Use:   "set-body",
		Short: "Set a comment body",
		RunE: func(cmd *cobra.Command, args []string) error {
			actor, err := st.resolveActor(true)
			if err != nil {
				return err
			}
			s, err := st.openStore()
			if err != nil {
				return err
			}
			if err := s.SetCommentBody(id, body, actor); err != nil {
				return err
			}
			c, err := s.GetComment(id)
			if err != nil {
				return err
			}
			return st.emit(st.stdout(), map[string]any{"comment": commentToJSON(c, nil)}, func() {
				fmt.Fprintf(os.Stdout, "updated body %s\n", c.ID)
			})
		},
	}
	cmd.Flags().StringVar(&id, "id", "", "comment id")
	cmd.Flags().StringVar(&body, "body", "", "new body")
	_ = cmd.MarkFlagRequired("id")
	_ = cmd.MarkFlagRequired("body")
	return cmd
}

func newCommentLabelCmd(st *cliState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "label",
		Short: "Comment label commands",
	}
	cmd.AddCommand(newCommentLabelAddCmd(st))
	cmd.AddCommand(newCommentLabelRemoveCmd(st))
	return cmd
}

func newCommentLabelAddCmd(st *cliState) *cobra.Command {
	var id, label string
	cmd := &cobra.Command{
		Use:   "add",
		Short: "Add a label to a comment",
		RunE: func(cmd *cobra.Command, args []string) error {
			actor, err := st.resolveActor(true)
			if err != nil {
				return err
			}
			s, err := st.openStore()
			if err != nil {
				return err
			}
			if err := s.CommentLabelAdd(id, label, actor); err != nil {
				return err
			}
			c, err := s.GetComment(id)
			if err != nil {
				return err
			}
			return st.emit(st.stdout(), map[string]any{"comment": commentToJSON(c, nil)}, func() {
				fmt.Fprintf(os.Stdout, "added label %s to %s\n", label, c.ID)
			})
		},
	}
	cmd.Flags().StringVar(&id, "id", "", "comment id")
	cmd.Flags().StringVar(&label, "label", "", "label name")
	_ = cmd.MarkFlagRequired("id")
	_ = cmd.MarkFlagRequired("label")
	return cmd
}

func newCommentLabelRemoveCmd(st *cliState) *cobra.Command {
	var id, label string
	cmd := &cobra.Command{
		Use:   "remove",
		Short: "Remove a label from a comment",
		RunE: func(cmd *cobra.Command, args []string) error {
			actor, err := st.resolveActor(true)
			if err != nil {
				return err
			}
			s, err := st.openStore()
			if err != nil {
				return err
			}
			if err := s.CommentLabelRemove(id, label, actor); err != nil {
				return err
			}
			c, err := s.GetComment(id)
			if err != nil {
				return err
			}
			return st.emit(st.stdout(), map[string]any{"comment": commentToJSON(c, nil)}, func() {
				fmt.Fprintf(os.Stdout, "removed label %s from %s\n", label, c.ID)
			})
		},
	}
	cmd.Flags().StringVar(&id, "id", "", "comment id")
	cmd.Flags().StringVar(&label, "label", "", "label name")
	_ = cmd.MarkFlagRequired("id")
	_ = cmd.MarkFlagRequired("label")
	return cmd
}

func newCommentRemoveCmd(st *cliState) *cobra.Command {
	var id string
	cmd := &cobra.Command{
		Use:   "remove",
		Short: "Remove a comment",
		RunE: func(cmd *cobra.Command, args []string) error {
			actor, err := st.resolveActor(true)
			if err != nil {
				return err
			}
			s, err := st.openStore()
			if err != nil {
				return err
			}
			if err := s.RemoveComment(id, actor); err != nil {
				return err
			}
			return st.emit(st.stdout(), map[string]any{"removed": id}, func() {
				fmt.Fprintf(os.Stdout, "removed comment %s\n", id)
			})
		},
	}
	cmd.Flags().StringVar(&id, "id", "", "comment id")
	_ = cmd.MarkFlagRequired("id")
	return cmd
}
```

- [ ] **Step 5: Wire `comment` under `atm task`**

In `internal/cli/task.go` `newTaskCmd` (after `cmd.AddCommand(newTaskLabelCmd(st))`):

```go
	cmd.AddCommand(newTaskCommentCmd(st))
```

- [ ] **Step 6: Run tests and regenerate golden files**

Run: `go test ./internal/cli/ -run 'TestGoldenComment|TestComment' -update -v`
Expected: PASS — golden files created.

Then run again without `-update` to confirm:

Run: `go test ./internal/cli/ -run 'TestGoldenComment|TestComment' -v`
Expected: PASS.

Then full CLI test suite: `go test ./internal/cli/ -v`
Expected: PASS — no regressions.

- [ ] **Step 7: Commit**

```bash
git add internal/cli/output.go internal/cli/comment.go internal/cli/comment_test.go internal/cli/task.go internal/cli/testdata/golden/comment-*.json
git commit -m "cli: add atm task comment subcommands"
```

---

### Task 7: Conventions update

**Files:**
- Modify: `internal/cli/conventions.go` (add `comment:`/`activity:` mention; mention `atm task comment add` in the agent first-contact sequence)
- Test: regenerate `internal/cli/testdata/golden/conventions-text.json` and `conventions-json.json` via `-update`

- [ ] **Step 1: Read the current conventions text**

The conventions in `internal/cli/conventions.go` is a single Go raw-string literal (`conventionsText`) plus a structured-map renderer (`conventionsStructured()`). We need two edits: a mention in the human text and a `comment:`/`activity:` seed-namespace note; plus mention `atm task comment add` in the agent first-contact sequence.

- [ ] **Step 2: Update the text**

In `conventionsText`:

After the "## How to read a task and its labels" paragraph, insert a new paragraph:

```
## How to narrate progress

Comments are the running narrative on a task: a clarification, an implementation PR/commit reference, a bug detected by QA, an open question, a pointer to a design doc. Comments live in the store as a per-task append-mostly thread. Add a comment with `atm task comment add --task <ID> --body "<text>" --label <CODE>:<kind>`. The label is the classification — `ATM:comment:clarification`, `ATM:comment:implementation`, `ATM:comment:qa-bug`, `ATM:comment:open-question`, `ATM:comment:design-doc` (advisory; the store treats these like any other label). Comments support `--reply-to <COMMENT-ID>` for threading within the same task. Edit a comment's body with `atm task comment set-body`; remove one with `atm task comment remove`.
```

In the "Agent first-contact sequence" numbered list, append a new item (item 7):

```
7. `atm task comment list --task <ID>` — read the running narrative on a task before acting on it.
```

- [ ] **Step 3: Update the structured map**

In `conventionsStructured()`, add a new key alongside `"where_tasks_live"`:

```go
	"how_to_narrate_progress":           "Comments are the running narrative on a task: a clarification, an implementation PR/commit reference, a bug detected by QA, an open question, a pointer to a design doc. Comments live in the store as a per-task append-mostly thread. Add a comment with atm task comment add --task <ID> --body <text> --label <CODE>:<kind>. The label is the classification (ATM:comment:clarification, ATM:comment:implementation, ATM:comment:qa-bug, ATM:comment:open-question, ATM:comment:design-doc — advisory; the store treats these like any other label). Comments support --reply-to <COMMENT-ID> for threading within the same task. Edit a comment's body with atm task comment set-body; remove one with atm task comment remove.",
```

In the `agent_first_contact_sequence` slice, append:

```go
			"atm task comment list --task <ID> — read the running narrative on a task before acting on it",
```

- [ ] **Step 4: Regenerate conventions golden files**

Run: `go test ./internal/cli/ -run 'TestGoldenConventions' -update -v`
Expected: PASS — goldens rewritten.

Then run without `-update`: `go test ./internal/cli/ -run 'TestGoldenConventions' -v`
Expected: PASS.

Then full CLI test suite: `go test ./internal/cli/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/cli/conventions.go internal/cli/testdata/golden/conventions-*.json
git commit -m "conventions: document task comments and agent progress narrative"
```

---

### Task 8: TUI — comment section in task detail, history hidden by default

**Files:**
- Modify: `internal/tui/tasks.go` (`renderDetail` — remove inline History section, addComments section, add `[H for history]` line; add `M`/`H` key handlers in `handleDetailKey`)
- Modify: `internal/tui/keymap.go` (add `M` row scoped to Detail; the help overlay rebuilds from this)
- Modify: `internal/tui/app.go` (add `formCommentAdd`, `formCommentSetBody`, `formCommentLabelAdd`, `formCommentLabelRemove`, `confirmRemoveComment`; route the form/confirm dispatch and invocation)
- Test: `internal/tui/comments_test.go` (new)
- Test: extend `internal/tui/tasks_test.go` (or create `internal/tui/task_detail_test.go`) with view-snapshot assertions

**Interfaces:**
- Consumes: store functions from Tasks 3 & 4 (`ListComments`, `CreateComment`, `SetCommentBody`, `CommentLabelAdd`, `CommentLabelRemove`, `RemoveComment`, `GetComment`).
- Produces: TUI model fields and handlers for the comments section.

**Scope note:** History is **removed from the default task detail render**. A new `H` key opens a **history overlay** covering `task.*` + `task.meta-changed` events. Comments are rendered between facts and the `[press H for history]` line. Each comment row shows `actor` + `relative time` + `labels` + indented body (no comment IDs in rows — IDs surface only in the comment detail overlay).

Because the TUI scope here is substantial, **this task delivers the comments-section render and the `M`/`H` key handling for opening form/overlay**, but the comment detail overlay itself lives in Task 9.

- [ ] **Step 1: Write the failing test**

Create `internal/tui/comments_test.go`:

```go
package tui

import (
	"strings"
	"testing"

	"atm/internal/store"
)

func TestTaskDetailRendersCommentsSection(t *testing.T) {
	m := newTestModel(t)
	_, _ = m.store.CreateProject("ATM", "Agent Tasks Management", "claude")
	tk, _ := m.store.CreateTask("ATM", "Fix thing", "work on it", nil, "claude")
	_, _ = m.store.CreateComment(tk.ID, "first comment body", []string{"ATM:comment:open-question"}, "", "agent")
	_, _ = m.store.CreateComment(tk.ID, "second reply", nil, "ATM-0001-c0001", "ttran")

	m.projectScope = "ATM"
	m.tasks.openDetail(tk.ID)
	view := m.tasks.View()
	if !strings.Contains(view, "Comments") {
		t.Fatalf("missing Comments section:\n%s", view)
	}
	if !strings.Contains(view, "agent") {
		t.Fatalf("missing first comment actor:\n%s", view)
	}
	if !strings.Contains(view, "ttran") {
		t.Fatalf("missing second comment actor:\n%s", view)
	}
	if !strings.Contains(view, "first comment body") {
		t.Fatalf("missing first comment body:\n%s", view)
	}
	if !strings.Contains(view, "second reply") {
		t.Fatalf("missing second comment body:\n%s", view)
	}
	if !strings.Contains(view, "[M] add comment") {
		t.Fatalf("missing [M] hint:\n%s", view)
	}
	if !strings.Contains(view, "[H] history") {
		t.Fatalf("missing [H] hint:\n%s", view)
	}
}

func TestTaskDetailHidesHistoryInline(t *testing.T) {
	m := newTestModel(t)
	_, _ = m.store.CreateProject("ATM", "x", "claude")
	tk, _ := m.store.CreateTask("ATM", "t", "", nil, "claude")
	m.projectScope = "ATM"
	m.tasks.openDetail(tk.ID)
	view := m.tasks.View()
	// The full History section (with event rows) must not inline-render by default.
	hv := m.store.History(tk.ProjectCode, store.Subject{Kind: "task", ID: tk.ID})
	if len(hv) > 0 && strings.Contains(view, "task.created") {
		t.Fatalf("history must be hidden behind [H], but found task.created in detail:\n%s", view)
	}
}

func TestTaskDetailMKeyOpensCommentForm(t *testing.T) {
	m := newTestModel(t)
	_, _ = m.store.CreateProject("ATM", "x", "claude")
	tk, _ := m.store.CreateTask("ATM", "t", "", nil, "claude")
	m.projectScope = "ATM"
	m.tasks.openDetail(tk.ID)
	if m.form != nil {
		t.Fatal("expected nil form before [M]")
	}
	m.tasks.handleDetailKey(keyMsg("M"))
	if m.form == nil || m.formKind != formCommentAdd {
		t.Fatalf("expected formCommentAdd, got form=%v kind=%v", m.form, m.formKind)
	}
}
```

Notes:
- `newTestModel(t)` is an existing helper — find it with `grep 'func newTestModel' internal/tui/`. If it doesn't exist, follow the pattern in `internal/tui/tasks_test.go` to construct a `Model`.
- `keyMsg(s)` is shorthand — find the existing constructor in the TUI tests (likely `tea.NewKeyMsg` or a small local helper). Match the existing pattern in `tasks_test.go`.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/tui/ -run 'TestTaskDetailRendersComments|TestTaskDetailHides|TestTaskDetailMKey' -v`
Expected: FAIL — comments section absent; `M`/`H` keys unknown.

- [ ] **Step 3: Add form-kind constants**

In `internal/tui/app.go`, in the `formAction` enum block (after `formProjectSetName`):

```go
	formProjectSetName  // project detail: set name
	formCommentAdd      // task detail: add comment
	formCommentSetBody  // comment detail: edit body
	formCommentLabelAdd // comment detail: add label
	formCommentLabelRemove // comment detail: remove label
```

In the `confirmAction` enum (after `confirmRemoveTask`):

```go
	confirmRemoveComment
```

In the form submit dispatch switch (around line 458), add handlers:

```go
	case formCommentAdd:
		return m.doCommentAdd(vals)
	case formCommentSetBody:
		return m.doCommentSetBody(vals)
	case formCommentLabelAdd:
		return m.doCommentLabelAdd(vals)
	case formCommentLabelRemove:
		return m.doCommentLabelRemove(vals)
```

Add those methods (Task 9 fills in the comment-detail-overlay plumbing that uses them; for now we only need `doCommentAdd` since `M` opens only that form):

```go
func (m *Model) doCommentAdd(vals map[string]string) tea.Cmd {
	taskID := m.tasks.detail.id
	body := vals["body"]
	var labels []string
	for _, tok := range strings.Fields(vals["labels"]) {
		labels = append(labels, m.projectScope+":"+tok)
	}
	replyTo := vals["reply-to"]
	c, err := m.store.CreateComment(taskID, body, labels, replyTo, m.actor)
	if err != nil {
		m.showToast("error: " + err.Error())
		return nil
	}
	_ = c
	m.refreshAll()
	m.tasks.openDetail(taskID)
	return nil
}

func (m *Model) doCommentSetBody(vals map[string]string) tea.Cmd {
	// Wired in Task 9.
	return nil
}

func (m *Model) doCommentLabelAdd(vals map[string]string) tea.Cmd {
	// Wired in Task 9.
	return nil
}

func (m *Model) doCommentLabelRemove(vals map[string]string) tea.Cmd {
	// Wired in Task 9.
	return nil
}
```

- [ ] **Step 4: Add `M`/`H` key handlers + history overlay field**

In `internal/tui/tasks.go`:

In the `taskDetailState` struct (around line 85), add a `historyOpen bool` field:

```go
type taskDetailState struct {
	id         string
	task       *store.Task
	lines      []string
	offset     int
	historyOpen bool
}
```

In `handleDetailKey`, after the existing cases (`e`/`d`/`b`/`B`/`x`), add:

```go
	case "M":
		t.openCommentAddForm()
	case "H":
		t.detail.historyOpen = !t.detail.historyOpen
		t.renderDetail()
```

(The comment-detail-overlay `Enter` and reply `R` keys are wired in Task 9 — Task 8 only delivers the section render + `M`/`H` keys. `Enter` continues to do nothing in detail mode for now.)

- [ ] **Step 5: Render comments section + remove inline history**

In `renderDetail`, replace the existing History-section block with this structure:

After the Labels section block (the existing `b.WriteString(sectionDivider(... "Labels"))` ... `b.WriteString("\n")`), insert before the existing `b.WriteString(sectionDivider(t.m.styles, t.width, "History"))`:

```go
	b.WriteString(sectionDivider(t.m.styles, t.width, "Comments"))
	b.WriteString("\n")
	cs, _ := t.m.store.ListComments(tk.ID)
	if len(cs) == 0 {
		b.WriteString(dashboardLine(t.width, " (no comments)"))
		b.WriteString("\n")
	} else {
		for _, c := range cs {
			labels := "(no labels)"
			if len(c.Labels) > 0 {
				labels = strings.Join(c.Labels, " ")
			}
			fmt.Fprintf(&b, "%s\n", dashboardLine(t.width, fmt.Sprintf(" %s   %s   %s", c.CreatedBy, relTime(c.CreatedAt, store.Now()), truncateRunes(labels, 36))))
			bodyLines := strings.Split(c.Body, "\n")
			maxLines := 6
			for i := 0; i < len(bodyLines) && i < maxLines; i++ {
				fmt.Fprintf(&b, "%s\n", dashboardLine(t.width, fmt.Sprintf("     %s", bodyLines[i])))
			}
			if len(bodyLines) > maxLines {
				fmt.Fprintf(&b, "%s\n", dashboardLine(t.width, "     …"))
			}
		}
	}
	b.WriteString("\n")
```

Replace the existing History section block (from `b.WriteString(sectionDivider(t.m.styles, t.width, "History"))` through the post-loop `b.WriteString("\n")`) with:

```go
	if t.detail.historyOpen {
		b.WriteString(sectionDivider(t.m.styles, t.width, "History"))
		b.WriteString("\n")
		hv := t.m.store.History(tk.ProjectCode, store.Subject{Kind: "task", ID: tk.ID})
		if len(hv) == 0 {
			b.WriteString(dashboardLine(t.width, " (no history)"))
			b.WriteString("\n")
		} else {
			for _, e := range hv {
				fmt.Fprintf(&b, "%s\n", dashboardLine(t.width, fmt.Sprintf("[%d] %s %s %s", e.Seq, store.RFC3339UTC(e.At), e.Actor, e.Action)))
			}
		}
		b.WriteString("\n")
	}
```

Replace the existing Actions menu line at the bottom of `renderDetail`:

```go
	b.WriteString(dashboardLine(t.width, t.m.styles.KeyMenuDim.Render("[e] edit title   [d] edit description   [b] add label   [B] remove label   [M] add comment   [H] history   [x] remove   [Esc] back")))
```

- [ ] **Step 6: Add `openCommentAddForm` helper**

In `internal/tui/tasks.go` (next to `openCreateForm`):

```go
func (t *tasksModel) openCommentAddForm() {
	tk := t.detail.task
	if tk == nil {
		return
	}
	labelsValidator := func(field, value string) error {
		if value == "" {
			return nil
		}
		for _, tok := range strings.Fields(value) {
			if !labelSuffixRe.MatchString(tok) {
				return fmt.Errorf("bad label %q: use <namespace>:<value> or <tag>", tok)
			}
		}
		return nil
	}
	fields := []formField{
		{Label: "body", Required: true, Hint: "comment body (free-form prose)"},
		{Label: "labels", Required: false, Hint: "space-separated suffixes, e.g. 'comment:open-question' (prefix auto-added)", Validator: labelsValidator},
		{Label: "reply-to", Required: false, Hint: "optional comment id this replies to (same task)"},
	}
	f := NewForm("New comment  "+tk.ID+":", fields)
	f.Title = "New comment  " + tk.ID + ":"
	t.m.form = f
	t.m.formKind = formCommentAdd
}
```

- [ ] **Step 7: Update `keymap.go` with `M` row**

In `internal/tui/keymap.go`, add a new entry to `keymapRows` (insert near the existing `N`/`H` rows):

```go
	{"M", "-", "-", "-", "add comment (task)"},
	{"H", "toggle history (project detail)", "-", "-", "toggle history (task detail)"},
```

Note: the existing row for `H` says `"toggle history (project detail)"` for Projects. Verify whether that row exists as `{"H", "toggle history (project detail)", "-", "-", "-"}` (the spec audit-log spec kept `H` for project history toggle on project detail). If yes, **update** that row's Detail column to `"toggle history (task detail)"` rather than adding a new row. Examine the actual current row content first:

Run a grep: `rg '"H"' internal/tui/keymap.go`.
- If the existing row is `{"H", "toggle history (project detail)", "-", "-", "-"},` then replace the Detail column with `"toggle history (task detail)"`:
  ```go
  {"H", "toggle history (project detail)", "-", "-", "toggle history (task detail)"},
  ```
- Otherwise add a fresh `H` row.

Either way, add the `M` row.

- [ ] **Step 8: Run tests to verify they pass**

Run: `go test ./internal/tui/ -run 'TestTaskDetailRendersComments|TestTaskDetailHides|TestTaskDetailMKey' -v`
Expected: PASS.

Then full TUI: `go test ./internal/tui/ -v`
Expected: PASS.

- [ ] **Step 9: Commit**

```bash
git add internal/tui/tasks.go internal/tui/keymap.go internal/tui/app.go internal/tui/comments_test.go
git commit -m "tui: comments section in task detail, history behind [H] overlay"
```

---

### Task 9: TUI — comment detail overlay, full key wiring, help tab update

**Files:**
- Create: `internal/tui/comments.go` (new — comment detail overlay model; `R` reply form; `e`/`b`/`B`/`x`/`H` keys)
- Modify: `internal/tui/tasks.go` (handle `Enter` on a comment row to open the overlay; route comment-detail keys to the overlay)
- Modify: `internal/tui/app.go` (`formCommentSetBody`/`LabelAdd`/`LabelRemove` handlers wired; `confirmRemoveComment` dispatch)
- Modify: `internal/tui/help.go` (add Comment Detail overlay rows to the help overlay)
- Test: `internal/tui/comments_test.go` (extend)

This task is large — split internally into (a) data plumbing, (b) overlay view render, (c) key wiring, (d) help. Each step below corresponds to a logical piece; commit together at the end of Step 5.

- [ ] **Step 1: Write the failing tests**

Append to `internal/tui/comments_test.go`:

```go
func TestEnterOnCommentOpensDetailOverlay(t *testing.T) {
	m := newTestModel(t)
	_, _ = m.store.CreateProject("ATM", "x", "claude")
	tk, _ := m.store.CreateTask("ATM", "t", "", nil, "claude")
	_, _ = m.store.CreateComment(tk.ID, "body", []string{"ATM:comment:open-question"}, "", "agent")
	m.projectScope = "ATM"
	m.tasks.openDetail(tk.ID)
	// Cursor at the comments section row.
	m.tasks.commentsCursor = 0
	m.tasks.handleDetailKey(keyMsg("enter"))
	if m.tasks.commentOverlay.id != "ATM-0001-c0001" {
		t.Fatalf("comment overlay not opened: %+v", m.tasks.commentOverlay)
	}
}

func TestCommentOverlayShowsIDAndBody(t *testing.T) {
	m := newTestModel(t)
	_, _ = m.store.CreateProject("ATM", "x", "claude")
	tk, _ := m.store.CreateTask("ATM", "t", "", nil, "claude")
	_, _ = m.store.CreateComment(tk.ID, "the body text", nil, "", "agent")
	m.projectScope = "ATM"
	m.tasks.openDetail(tk.ID)
	m.tasks.commentsCursor = 0
	m.tasks.handleDetailKey(keyMsg("enter"))
	view := m.tasks.commentOverlay.view(m)
	if !strings.Contains(view, "ATM-0001-c0001") || !strings.Contains(view, "the body text") {
		t.Fatalf("overlay view missing id/body:\n%s", view)
	}
}

func TestCommentOverlayKeysEditRemove(t *testing.T) {
	m := newTestModel(t)
	_, _ = m.store.CreateProject("ATM", "x", "claude")
	tk, _ := m.store.CreateTask("ATM", "t", "", nil, "claude")
	c, _ := m.store.CreateComment(tk.ID, "orig", nil, "", "agent")
	m.projectScope = "ATM"
	m.tasks.openDetail(tk.ID)
	m.tasks.commentsCursor = 0
	m.tasks.handleDetailKey(keyMsg("enter"))

	// e -> body edit form
	m.tasks.handleCommentOverlayKey(keyMsg("e"))
	if m.form == nil || m.formKind != formCommentSetBody {
		t.Fatalf("[e] should open set-body form: form=%v kind=%v", m.form, m.formKind)
	}

	// x -> confirm remove
	m.tasks.handleCommentOverlayKey(keyMsg("x"))
	if m.confirm != confirmRemoveComment {
		t.Fatalf("[x] should open remove-confirm: %v", m.confirm)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/tui/ -run 'TestEnterOnComment|TestCommentOverlay' -v`
Expected: FAIL.

- [ ] **Step 3: Create `internal/tui/comments.go`**

```go
package tui

import (
	"fmt"
	"strings"

	"atm/internal/store"
	"github.com/charmbracelet/bubbletea"
)

type commentOverlayModel struct {
	id     string
	comment *store.Comment
	historyOpen bool
	offset int
	lines  []string
}

func (co *commentOverlayModel) view(m *Model) string {
	end := co.offset + m.tasks.contentHeight
	if end > len(co.lines) {
		end = len(co.lines)
	}
	var b strings.Builder
	for i := co.offset; i < end; i++ {
		b.WriteString(co.lines[i])
		b.WriteString("\n")
	}
	return padToHeight(b.String(), m.tasks.contentHeight)
}

func (co *commentOverlayModel) render(m *Model) {
	var b strings.Builder
	c := co.comment
	if c == nil {
		return
	}
	fmt.Fprintf(&b, "Comment %s\n", c.ID)
	b.WriteString(sepLine("─", 78, m.tasks.width, 2))
	b.WriteString("\n")
	fmt.Fprintf(&b, "%s\n", dashboardLine(m.tasks.width, fmt.Sprintf("id       %s", c.ID)))
	fmt.Fprintf(&b, "%s\n", dashboardLine(m.tasks.width, fmt.Sprintf("task     %s", c.TaskID)))
	if c.ReplyTo != "" {
		fmt.Fprintf(&b, "%s\n", dashboardLine(m.tasks.width, fmt.Sprintf("reply-to %s", c.ReplyTo)))
	}
	fmt.Fprintf(&b, "%s\n", dashboardLine(m.tasks.width, fmt.Sprintf("actor    %s", c.CreatedBy)))
	fmt.Fprintf(&b, "%s\n", dashboardLine(m.tasks.width, fmt.Sprintf("created  %s", store.RFC3339UTC(c.CreatedAt))))
	fmt.Fprintf(&b, "%s\n", dashboardLine(m.tasks.width, fmt.Sprintf("updated  %s by %s", store.RFC3339UTC(c.UpdatedAt), c.UpdatedBy)))
	fmt.Fprintf(&b, "%s\n", dashboardLine(m.tasks.width, fmt.Sprintf("labels   %s", formatLabelsTUI(c.Labels))))
	b.WriteString("\n")
	b.WriteString(sectionDivider(m.styles, m.tasks.width, "Body"))
	b.WriteString("\n")
	for _, line := range strings.Split(c.Body, "\n") {
		fmt.Fprintf(&b, "%s\n", dashboardLine(m.tasks.width, line))
	}
	b.WriteString("\n")
	if co.historyOpen {
		b.WriteString(sectionDivider(m.styles, m.tasks.width, "History"))
		b.WriteString("\n")
		code, _, _, _ := store.ParseCommentID(c.ID)
		hv := m.store.History(code, store.Subject{Kind: "comment", ID: c.ID})
		if len(hv) == 0 {
			b.WriteString(dashboardLine(m.tasks.width, " (no history)"))
			b.WriteString("\n")
		} else {
			for _, e := range hv {
				fmt.Fprintf(&b, "%s\n", dashboardLine(m.tasks.width, fmt.Sprintf("[%d] %s %s %s", e.Seq, store.RFC3339UTC(e.At), e.Actor, e.Action)))
			}
		}
		b.WriteString("\n")
	}
	b.WriteString(dashboardLine(m.tasks.width, m.styles.KeyMenuDim.Render("[e] edit body   [b] add label   [B] remove label   [R] reply   [H] history   [x] remove   [Esc] back")))
	co.lines = strings.Split(b.String(), "\n")
}

func formatLabelsTUI(labels []string) string {
	if len(labels) == 0 {
		return "(no labels)"
	}
	return strings.Join(labels, " ")
}

// handleCommentOverlayKey dispatches a key pressed while the comment overlay is open.
// The receiver is *tasksModel because the overlay is owned by the tasks pane state
// and we need access to tasksModel.m for opening forms/confirm overlays.
func (t *tasksModel) handleCommentOverlayKey(k tea.KeyMsg) tea.Cmd {
	co := &t.commentOverlay
	if co.comment == nil {
		return nil
	}
	switch k.String() {
	case "j", "down":
		co.offset++
		t.clampCommentOverlay()
	case "k", "up":
		if co.offset > 0 {
			co.offset--
		}
	case "g":
		co.offset = 0
	case "e":
		t.openCommentBodyForm(co.comment)
	case "b":
		t.openCommentLabelAddForm(co.comment)
	case "B":
		t.openCommentLabelRemoveForm(co.comment)
	case "R":
		t.openCommentReplyForm(co.comment)
	case "H":
		co.historyOpen = !co.historyOpen
		co.render(t.m)
	case "x":
		t.m.confirm = confirmRemoveComment
		t.m.confirmMsg = fmt.Sprintf("Remove comment %s?", co.id)
		t.m.confirmArg = "History is preserved in the audit log."
		return nil
	case "esc":
		t.commentOverlay = commentOverlayModel{}
		t.renderDetail()
	}
	return nil
}

func (t *tasksModel) clampCommentOverlay() {
	maxOff := len(t.commentOverlay.lines) - t.contentHeight
	if maxOff < 0 {
		maxOff = 0
	}
	if t.commentOverlay.offset > maxOff {
		t.commentOverlay.offset = maxOff
	}
}
```

- [ ] **Step 4: Wire the overlay into `tasksModel`**

In `internal/tui/tasks.go`:

Add two fields to `tasksModel`:

```go
	commentsCursor  int
	commentOverlay  commentOverlayModel
```

In `handleDetailKey`, route to the overlay when open:

```go
func (t *tasksModel) handleDetailKey(k tea.KeyMsg) tea.Cmd {
	if t.commentOverlay.id != "" {
		return t.handleCommentOverlayKey(k)
	}
	// ... existing switch ...
}
```

In `handleDetailKey`'s `enter` case (add as a new case):

```go
	case "enter":
		// If the comments section is focused and the cursor is on a comment row, open the overlay.
		if t.commentsFocus && t.commentsCursor >= 0 {
			cs, _ := t.m.store.ListComments(t.detail.id)
			if t.commentsCursor < len(cs) {
				return t.openCommentOverlay(cs[t.commentsCursor].ID)
			}
		}
		// Otherwise no-op in detail mode (the existing behavior).
```

(`commentsFocus bool` is a new field on `tasksModel` to mark the focus ring; default `false` = task-facts block focused, cursor not on a comment row. `Tab` (next step) toggles it.)

In `handleDetailKey`, add a `tab` case:

```go
	case "tab":
		t.commentsFocus = !t.commentsFocus
		t.renderDetail()
```

`openCommentOverlay`:

```go
func (t *tasksModel) openCommentOverlay(id string) tea.Cmd {
	c, err := t.m.store.GetComment(id)
	if err != nil {
		t.m.showToast("error: " + err.Error())
		return nil
	}
	t.commentOverlay = commentOverlayModel{id: id, comment: c}
	t.commentOverlay.render(t.m)
	return nil
}
```

Form openers:

```go
func (t *tasksModel) openCommentBodyForm(c *store.Comment) {
	fields := []formField{
		{Label: "body", Required: true, Value: c.Body, Hint: "new body"},
	}
	f := NewForm("Edit comment body", fields)
	t.m.form = f
	t.m.formKind = formCommentSetBody
	t.m.formPayload = c.ID
}

func (t *tasksModel) openCommentLabelAddForm(c *store.Comment) {
	validator := func(field, value string) error {
		if value == "" {
			return nil
		}
		if !labelSuffixRe.MatchString(value) {
			return fmt.Errorf("use <namespace>:<value> or <tag>")
		}
		return nil
	}
	fields := []formField{
		{Label: "name", Required: true, Hint: "<namespace>:<value> or <tag>", Validator: validator},
	}
	f := NewForm("Add comment label  "+t.m.projectScope+":", fields)
	t.m.form = f
	t.m.formKind = formCommentLabelAdd
	t.m.formPayload = c.ID
}

func (t *tasksModel) openCommentLabelRemoveForm(c *store.Comment) {
	validator := func(field, value string) error {
		if value == "" {
			return nil
		}
		if !labelSuffixRe.MatchString(value) {
			return fmt.Errorf("use <namespace>:<value> or <tag>")
		}
		return nil
	}
	fields := []formField{
		{Label: "name", Required: true, Hint: "<namespace>:<value> or <tag>", Validator: validator},
	}
	f := NewForm("Remove comment label  "+t.m.projectScope+":", fields)
	t.m.form = f
	t.m.formKind = formCommentLabelRemove
	t.m.formPayload = c.ID
}

func (t *tasksModel) openCommentReplyForm(parent *store.Comment) {
	labelsValidator := func(field, value string) error {
		if value == "" {
			return nil
		}
		for _, tok := range strings.Fields(value) {
			if !labelSuffixRe.MatchString(tok) {
				return fmt.Errorf("bad label %q: use <namespace>:<value> or <tag>", tok)
			}
		}
		return nil
	}
	fields := []formField{
		{Label: "body", Required: true, Hint: "reply body"},
		{Label: "labels", Required: false, Hint: "space-separated suffixes", Validator: labelsValidator},
	}
	f := NewForm("Reply to  "+parent.ID+":", fields)
	f.Title = "Reply to  " + parent.ID + ":"
	t.m.form = f
	t.m.formKind = formCommentAdd
	t.m.formPayload = parent.ID // doCommentAdd reads this as reply-to
}
```

- [ ] **Step 5: Wire form submit handlers in `app.go`**

Replace the placeholders in Task 8 with real handlers:

```go
func (m *Model) doCommentAdd(vals map[string]string) tea.Cmd {
	taskID := m.tasks.detail.id
	body := vals["body"]
	var labels []string
	for _, tok := range strings.Fields(vals["labels"]) {
		labels = append(labels, m.projectScope+":"+tok)
	}
	replyTo := m.formPayload
	if rt, ok := vals["reply-to"]; ok && rt != "" {
		replyTo = rt
	}
	_, err := m.store.CreateComment(taskID, body, labels, replyTo, m.actor)
	if err != nil {
		m.showToast("error: " + err.Error())
		return nil
	}
	m.formPayload = ""
	m.refreshAll()
	m.tasks.openDetail(taskID)
	return nil
}

func (m *Model) doCommentSetBody(vals map[string]string) tea.Cmd {
	id := m.formPayload
	if err := m.store.SetCommentBody(id, vals["body"], m.actor); err != nil {
		m.showToast("error: " + err.Error())
		return nil
	}
	m.refreshAll()
	// Reopen overlay with refreshed comment.
	c, err := m.store.GetComment(id)
	if err == nil {
		m.tasks.commentOverlay = commentOverlayModel{id: id, comment: c}
		m.tasks.commentOverlay.render(m)
	}
	return nil
}

func (m *Model) doCommentLabelAdd(vals map[string]string) tea.Cmd {
	id := m.formPayload
	full := m.projectScope + ":" + vals["name"]
	if err := m.store.CommentLabelAdd(id, full, m.actor); err != nil {
		m.showToast("error: " + err.Error())
		return nil
	}
	m.refreshAll()
	c, err := m.store.GetComment(id)
	if err == nil {
		m.tasks.commentOverlay = commentOverlayModel{id: id, comment: c}
		m.tasks.commentOverlay.render(m)
	}
	return nil
}

func (m *Model) doCommentLabelRemove(vals map[string]string) tea.Cmd {
	id := m.formPayload
	full := m.projectScope + ":" + vals["name"]
	if err := m.store.CommentLabelRemove(id, full, m.actor); err != nil {
		m.showToast("error: " + err.Error())
		return nil
	}
	m.refreshAll()
	c, err := m.store.GetComment(id)
	if err == nil {
		m.tasks.commentOverlay = commentOverlayModel{id: id, comment: c}
		m.tasks.commentOverlay.render(m)
	}
	return nil
}
```

Also handle the confirm dispatch (find where `confirmRemoveTask` is handled in `app.go` — likely the `doConfirm` switch — and add `confirmRemoveComment`):

```go
	case confirmRemoveComment:
		id := m.tasks.commentOverlay.id
		if err := m.store.RemoveComment(id, m.actor); err != nil {
			m.showToast("error: " + err.Error())
			m.confirm = confirmNone
			return nil
		}
		m.confirm = confirmNone
		m.tasks.commentOverlay = commentOverlayModel{}
		m.refreshAll()
		m.tasks.openDetail(m.tasks.detail.id)
		return nil
```

- [ ] **Step 6: Update `tasks.View()` to render the overlay**

In `internal/tui/tasks.go` `renderDetailView` (or wherever the detail view is composed), if `t.commentOverlay.id != ""`, return the overlay view instead of the plain detail view:

```go
func (t *tasksModel) renderDetailView() string {
	if t.commentOverlay.id != "" {
		return t.commentOverlay.view(t.m)
	}
	// ... existing code ...
}
```

- [ ] **Step 7: Update the help overlay (`internal/tui/help.go`)**

In the help overlay's CLI/TUI parity table and key summary, add Comment Detail overlay rows. Find the existing rows for the task detail in `help.go` and mirror the format.

Run a quick grep first: `rg -n 'edit title|add label' internal/tui/help.go`. Mirror an existing row, e.g.:

```go
	{"Comment Detail (overlay)", "M add   [e] edit body   [b] add label   [B] remove label   [H] history   [R] reply   [x] remove", "match the CLI: atm task comment add/show/set-body/label/remove"},
```

(Inspection of the actual `help.go` table shape will tell you the exact field structure — match it.)

- [ ] **Step 8: Run tests to verify they pass**

Run: `go test ./internal/tui/ -run 'TestEnterOnComment|TestCommentOverlay|TestTaskDetail' -v`
Expected: PASS.

Then full TUI suite: `go test ./internal/tui/ -v`
Expected: PASS.

- [ ] **Step 9: Commit**

```bash
git add internal/tui/comments.go internal/tui/comments_test.go internal/tui/tasks.go internal/tui/app.go internal/tui/help.go internal/tui/keymap.go
git commit -m "tui: comment detail overlay + reply/mutation keys + help tab"
```

---

### Task 10: Full verification and spec parity sweep

**Files:** none modified.

**Goal:** Final cross-check against the spec and `make verify` green. No new code.

- [ ] **Step 1: Run make verify**

Run: `make verify`
Expected: PASS.

- [ ] **Step 2: Spec parity sweep**

Open `docs/superpowers/specs/2026-07-05-task-comments-v1-design.md` and check each section against the implementation:

1. **Driver + Decisions (locked):** verify the 10 decisions are actually implemented:
   1. Classification: labels only, no `kind` field. — check `Comment` struct has no `kind` field.
   2. Lifecycle: create + edit-body + label-manage + soft-remove. — check all five actions exist and `RemoveComment` writes a tombstone.
   3. Threading: `ReplyTo` with same-task invariant. — check `CreateComment` validates.
   4. References: pure prose body. — no `refs` field on `Comment`.
   5. Target: tasks only. — no project-attached comment path.
   6. ID scheme: `<TASK-ID>-c<NNNN>` 4-digit per-task counter. — check `RenderCommentID` and `Task.NextCommentN`.
   7. Cache layout: `projects/<CODE>/comments/<id>.json`. — check `commentPath`.
   8. CLI: `atm task comment <verb>` with 7 verbs. — check `comment.go` wires all 7.
   9. TUI: COMMENTS section; no IDs in rows; `M` add; `Enter` overlay; `H` overlay; history hidden by default. — check tasks.go renderDetail.
   10. Log actions: 5 comment verbs + `task.meta-changed`. — check `validActions` in log.go.

2. **Architecture & data flow:** check that `Rebuild` writes comment caches and sweeps orphans; `Verify` adds per-comment `CacheCheck`s.

3. **Conventions update:** check `atm conventions` mentions `atm task comment add`.

4. **Rollout (strictly additive):** git log shows the tree builds at every commit. Verify `git log --oneline` shows the 10 commits in order and `make verify` passes after each.

If any spec section is unimplemented: add a task back into this plan and execute it. Otherwise continue.

- [ ] **Step 3: Spec parity golden sweep**

Run a one-shot script-style check: for each store-cli-tui invariant in the spec, identify the test function that covers it. List any invariants without coverage. (This step is documentary — record the matrix in your final report to the user, but no code change unless a gap is found.)

- [ ] **Step 4: Final commit (if any gap-fixes were needed)**

If gaps were found in Step 2/3, fix and commit. Otherwise:

```bash
git log --oneline -10
make verify
```

Output the final report to the user, including:
- The 10 commit shas and their messages.
- The spec parity matrix (decision → implementation file:line).
- `make verify` status.

---

## Summary

**10 tasks** spanning store (`types.go`/`log.go`/`comment.go`/`verify.go`/`rebuild.go`), CLI (`comment.go`/`output.go`/`conventions.go`), and TUI (`comments.go`/`tasks.go`/`app.go`/`help.go`/`keymap.go`). Each commit is independently testable; the tree builds at every step (strictly additive). `make verify` is the gate at every commit. No new dependencies, no new sentinels, no new exit codes, no migration.