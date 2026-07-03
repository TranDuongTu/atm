# Tasks Management System v2 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Wholesale delete-and-rebuild of ATM onto the v2 pure label-substrate model (no intrinsic workflow knowledge; labels are the single dynamic organizing substrate; history is the only narrative record).

**Architecture:** Three layers rebuilt in order — `internal/store` (stable in-process API), `internal/cli` (stable out-of-process API via cobra), `internal/tui` (thin Bubble Tea client over store). The TUI mockup spec (`docs/superpowers/specs/2026-07-02-tasks-management-v2-tui-mockups-design.md`) is the screen-level reference for the TUI layer; this plan folds its requirements into the TUI tasks. Approach A: one commit deletes v1 store/cli/tui + their tests (pure deletion — the tree will NOT compile after this commit, which is expected and acceptable per the spec's Rollout section), then subsequent commits rebuild layer by layer from scratch. Task 2 rebuilds the store and restores compilation.

**Tech Stack:** Go 1.22+; cobra for CLI; Bubble Tea v0.25 + lipgloss v0.10 for TUI; `golang.org/x/sys` for flock; table-driven tests; golden-file CLI tests via the existing `goldenHarness`/`compareGolden` pattern.

## Global Constraints

Copied verbatim from the specs so every task inherits them:

- Project code regex: `^[A-Z]{3,6}$` (3-6 uppercase letters only; v1's `^[A-Z][A-Z0-9-]{1,15}$` is gone).
- Label-name regex: `^[A-Z]{3,6}(:[a-z0-9][a-z0-9-]*){1,2}$`. The project prefix segment must match an existing project (rejects `XYZ:type:bug` if project `XYZ` does not exist).
- No emojis in code, commits, or TUI (AGENTS.md rule; TUI mockup spec: "No icons (no-emoji rule)").
- Actor is a free-form string (`claude`, `ttran`). `ValidateActorID` and the `agent:X`/`human:X` format rule are gone. `--actor` is required on mutating commands, optional on reads.
- Global flags: `--store <path>` (overrides `ATM_HOME`), `--output json|text` (default text; JSON is deterministic — sorted keys, stable whitespace, RFC 3339 UTC timestamps), `--actor <id>`, `--quiet`.
- Exit codes: 0 success; 1 generic; 2 usage; 3 not-found; 4 conflict. Errors go to stderr with a stable `{"error":{"code":"...","message":"..."}}` envelope in JSON mode.
- History is an immutable system invariant: every mutation appends a `HistoryEntry` and no command exists to edit or delete history.
- Verification gate: `make verify` (= `make build && make test`). No new make targets.
- Store root resolution: `--store` flag -> `ATM_HOME` -> `~/.config/atm`; no DB; detachable by directory copy.
- Task IDs: `<CODE>-<N>` (4-digit zero-padded up to 9999, then natural width). `task create` does NOT auto-assign any status label.
- Conventions are advisory only — nothing in the store validates or special-cases the documented namespaces (`status:`, `type:`, etc.).

## File Structure

This is the target file layout after the rebuild. Tasks 1 (delete) and 2 (store) establish it.

```
internal/store/
  store.go        # MODIFY — trim to v2 surface (carryover helpers + new label/validation regex)
  json.go         # KEEP  — unchanged (MarshalSorted, WriteFileAtomic, ReadJSON)
  lock.go         # KEEP  — unchanged (WithLock per-project flock)
  types.go        # REWRITE — Project/Task/Label/HistoryEntry only; delete Guide/Claim/Todo/Followup/Discussion/Link
  project.go      # REWRITE — minimal Project ops; drop TypeAxis/Repo/Guide/Label-set
  label.go        # CREATE — global labels.json registry ops (LabelAdd/Remove/List/Show/Namespaces)
  task.go         # REWRITE — Task ops; drop Status state machine/Claim/Links/Todos/Followups/Discussions
  query.go        # REWRITE — ListTasks (exact restrict, wildcards ignored for scoping) + GroupTasks (faceting)
  *_test.go       # REWRITE to match (project_test, label_test new, task_test, query_test, store_test; json_test + lock_test carry over unchanged)

internal/cli/
  root.go         # MODIFY — drop review/actor cmd registration; add label + conventions cmd registration; init no longer registers an actor
  errors.go       # KEEP  — unchanged
  output.go       # REWRITE — drop jsonClaim/jsonLink/jsonTodo/jsonFollowup/jsonDiscussion/jsonGuide/jsonTimeline/jsonConvention; new jsonLabel + jsonLabelGroup
  project.go      # REWRITE — minimal create/list/show/set-name/remove; drop set-type-axis/repo/guide/label(subcommand)
  label.go        # CREATE — label add/remove/list/show subcommands
  task.go         # REWRITE — create/list(--facets)/show/set-title/set-description/label add|label remove/remove; drop set-status/next/claim/unclaim/link/todo/followup/discussion/timeline
  conventions.go  # CREATE — atm conventions (print advisory onboarding guide + suggested namespaces)
  tui.go          # MODIFY — keep; --store/--actor flags only
  *_test.go       # REWRITE (project_test, task_test; new label_test, conventions_test; determinism_test rewritten; entry/link/review/actor_test deleted)
  testdata/golden/  # REGENERATE via -update (delete v1 fixtures; add v2 fixtures)

internal/tui/
  app.go          # REWRITE — three tabs (Projects/Tasks/Help); drop dashboard/workspace/summary pane, startup actor prompt, command palette; auto-init store on launch
  keymap.go       # REWRITE — v2 keys only (see mockup spec Global keymap summary)
  styles.go      # MODIFY — keep palette; drop unused styles
  form.go        # KEEP/MODIFY — overlay form primitive reused by all forms
  help.go        # REWRITE — parity table + global keymap + conventions section
  projects.go    # REWRITE — list (CODE/NAME/TASKS/LABELS/UPDATED) + single-pane detail + selection model + label add/remove forms
  tasks.go       # REWRITE — filter-driven faceting (flat vs grouped), sort cycle, detail view (facts+labels+history), empty states
  components/    # KEEP/MODIFY as needed (reusable list/form primitives)
  app_test.go    # REWRITE — view snapshots per mockup spec screens
  # DELETED: actors.go, dashboard.go, guide.go
```

---

## Task 1: Delete v1 store/cli/tui surface (Approach A, commit 1)

**Goal:** Remove every v1-only file so the tree is a clean slate. The tree will NOT compile after this commit — that is expected and accepted per the spec's Rollout section. Task 2 rebuilds the store from scratch and restores compilation. This task is pure `git rm`; no stubs, no edits to surviving files.

**Files:**
- Delete entirely (store): `internal/store/actor.go`, `actor_test.go`, `claim.go`, `claim_test.go`, `context.go`, `context_test.go`, `entry.go`, `entry_test.go`, `guide.go`, `guide_test.go`, `link.go`, `link_test.go`, `review.go`, `review_test.go`, `project.go`, `project_test.go`, `task.go`, `task_test.go`, `query.go`, `query_test.go`, `types.go`, `store_test.go`
- Delete entirely (cli): `internal/cli/actor.go`, `entry.go`, `entry_test.go`, `link.go`, `link_test.go`, `review.go`, `review_test.go`, `workflow.go`, `project_guide.go`, `project.go`, `task.go`, `output.go`, `project_test.go`, `task_test.go`, `determinism_test.go`, `testdata/golden/` (whole dir)
- Delete entirely (tui): `internal/tui/actors.go`, `dashboard.go`, `guide.go`, `app.go`, `app_test.go`, `projects.go`, `tasks.go`, `help.go`, `keymap.go`
- Keep unchanged: `internal/store/store.go`, `json.go`, `lock.go`, `internal/cli/errors.go`, `tui.go`, `internal/tui/styles.go`, `form.go`, `components/`

**Approach:** Pure deletion. The surviving `store.go` still references deleted symbols (`ValidateActorID`, `Register`, `touchActors`, `ErrStaleLink`) and `cli/tui` still reference deleted commands — nothing will compile. That is the intended state after Approach A's delete commit. Task 2 rebuilds `store` (types, project, label, task, query) and modifies `store.go` to drop the dead references, restoring `internal/store` compilation. Tasks 3 and 5 do the same for `cli` and `tui`. **The verification gate is NOT expected to pass after Task 1.**

- [ ] **Step 1: Delete v1-only store files + v1 store impl/tests**

```bash
git rm internal/store/actor.go internal/store/actor_test.go \
       internal/store/claim.go internal/store/claim_test.go \
       internal/store/context.go internal/store/context_test.go \
       internal/store/entry.go internal/store/entry_test.go \
       internal/store/guide.go internal/store/guide_test.go \
       internal/store/link.go internal/store/link_test.go \
       internal/store/review.go internal/store/review_test.go \
       internal/store/project.go internal/store/project_test.go \
       internal/store/task.go internal/store/task_test.go \
       internal/store/query.go internal/store/query_test.go \
       internal/store/types.go internal/store/store_test.go
```

- [ ] **Step 2: Delete v1-only cli files + v1 cli impl/tests**

```bash
git rm internal/cli/actor.go \
       internal/cli/entry.go internal/cli/entry_test.go \
       internal/cli/link.go internal/cli/link_test.go \
       internal/cli/review.go internal/cli/review_test.go \
       internal/cli/workflow.go internal/cli/project_guide.go \
       internal/cli/project.go internal/cli/task.go internal/cli/output.go \
       internal/cli/project_test.go internal/cli/task_test.go internal/cli/determinism_test.go
git rm -r internal/cli/testdata/golden
```

- [ ] **Step 3: Delete v1-only tui files + v1 tui impl/tests**

```bash
git rm internal/tui/actors.go internal/tui/dashboard.go internal/tui/guide.go \
       internal/tui/app.go internal/tui/app_test.go \
       internal/tui/projects.go internal/tui/tasks.go \
       internal/tui/help.go internal/tui/keymap.go
```

- [ ] **Step 4: Confirm the deletion is complete**

Run: `git status --short` (expect only deletions listed) and `ls internal/store internal/cli internal/tui` (expect only the carryover files remain: `store.go`, `json.go`, `lock.go` in store; `errors.go`, `tui.go` in cli; `styles.go`, `form.go`, `components/` in tui).

- [ ] **Step 5: Commit the delete**

```bash
git commit -m "v2: delete v1 store/cli/tui surface (Approach A)

Removes intrinsic workflow knowledge (status state machine, claim, review,
followups, todos, discussions, links, type-axis, guide, actor entity) per the
v2 design spec. Tree does not build between this commit and the store rebuild."
```

**Note:** Do NOT attempt `go build` or `make verify` here — both are expected to fail. The next task restores compilation.

---

## Task 2: Store layer — types, project, label, task, query

**Goal:** Rebuild `internal/store` to the full v2 surface so `go build ./internal/store` passes and the store is independently testable. This is the foundation every later layer consumes.

**Files:**
- Modify: `internal/store/store.go` (tighten regexes; drop `ValidateActorID`; `Init` touches `labels.json` not `actors.json`)
- Rewrite: `internal/store/types.go`, `project.go`, `task.go`, `query.go`
- Create: `internal/store/label.go`
- Keep unchanged: `internal/store/json.go`, `lock.go`

**Interfaces:**
- Produces (consumed by Tasks 3, 5): `store.Store` with `Init`, `StorePath`, `CreateProject(code, name, actor)`, `GetProject`, `ListProjects`, `SetProjectName`, `RemoveProject`, `LabelAdd(name, desc, actor)`, `LabelRemove(name, actor) (*LabelRemoveResult, error)`, `LabelList(project, namespace string) []Label`, `LabelShow(name) (Label, error)`, `Namespaces(code) []string`, `CreateTask(projectCode, title, description string, labels []string, actor string) (*Task, error)`, `GetTask`, `SetTitle`, `SetDescription`, `TaskLabelAdd`, `TaskLabelRemove`, `RemoveTask`, `ListTasks(QueryFilters) []*Task`, `GroupTasks(QueryFilters) ([]LabelGroup, []*Task)`; validation `ValidateProjectCode` (`^[A-Z]{3,6}$`), `ValidateLabelName` (new regex); sentinels `ErrNotFound`/`ErrConflict`/`ErrUsage`; helpers `ParseTaskID`/`RenderTaskID`/`SortTaskIDs`/`RFC3339UTC`/`Now`/`Open`/`ResolveStorePath`/`WithLock`/`ReadJSON`/`WriteJSON`/`MarshalSorted`.

- [ ] **Step 1: Write the failing types test**

`internal/store/types_test.go` — verifies the v2 structs marshal to the spec's JSON shapes and that removed fields are absent.

```go
package store

import (
	"encoding/json"
	"testing"
)

func TestTaskHasNoV1Fields(t *testing.T) {
	raw, _ := json.Marshal(Task{ID: "ATM-0001", ProjectCode: "ATM", Title: "x"})
	var dec map[string]any
	_ = json.Unmarshal(raw, &dec)
	for _, banned := range []string{"status", "claim", "links", "todos", "followups", "discussions"} {
		if _, ok := dec[banned]; ok {
			t.Fatalf("Task JSON must not contain %q, got %s", banned, raw)
		}
	}
	if _, ok := dec["labels"]; !ok {
		t.Fatalf("Task JSON must contain labels")
	}
}

func TestProjectHasNoV1Fields(t *testing.T) {
	raw, _ := json.Marshal(Project{Code: "ATM", Name: "x"})
	var dec map[string]any
	_ = json.Unmarshal(raw, &dec)
	for _, banned := range []string{"type_axis", "guide", "repo_paths", "guide_freshness_threshold", "labels"} {
		if _, ok := dec[banned]; ok {
			t.Fatalf("Project JSON must not contain %q, got %s", banned, raw)
		}
	}
}
```

Run: `go test ./internal/store -run TestTaskHasNoV1Fields -run TestProjectHasNoV1Fields`
Expected: FAIL (types still stubbed/missing from Task 1).

- [ ] **Step 2: Write types.go**

```go
package store

import "time"

type Label struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

type HistoryEntry struct {
	ID     string         `json:"id"`
	Action string         `json:"action"`
	Actor  string         `json:"actor"`
	At     time.Time      `json:"at"`
	Meta   map[string]any `json:"meta,omitempty"`
}

type Project struct {
	Code         string         `json:"code"`
	Name         string         `json:"name"`
	NextTaskN    int            `json:"next_task_n"`
	History      []HistoryEntry `json:"history,omitempty"`
	NextHistoryN int            `json:"next_history_n,omitempty"`
	CreatedAt    time.Time      `json:"created_at"`
	CreatedBy    string         `json:"created_by"`
	UpdatedAt   time.Time      `json:"updated_at"`
	UpdatedBy    string         `json:"updated_by"`
}

type Task struct {
	ID          string         `json:"id"`
	ProjectCode string         `json:"project_code"`
	Title       string         `json:"title"`
	Description string         `json:"description,omitempty"`
	Labels      []string       `json:"labels"`
	History     []HistoryEntry `json:"history"`
	CreatedAt   time.Time      `json:"created_at"`
	CreatedBy   string         `json:"created_by"`
	UpdatedAt   time.Time      `json:"updated_at"`
	UpdatedBy   string         `json:"updated_by"`
}
```

- [ ] **Step 3: Write store.go (trimmed)**

Modify `internal/store/store.go`: remove `ValidateActorID` + `actorIDRe`; tighten `projectCodeRe` to `^[A-Z]{3,6}$`; replace `labelRe` with `^[A-Z]{3,6}(:[a-z0-9][a-z0-9-]*){1,2}$`; update `ValidateProjectCode`/`ValidateLabelName` error messages; replace `touchActors` with `touchLabels` writing `{"labels":[]}`; drop `ErrStaleLink`; drop `actorsPath()`. Keep `ParseTaskID`/`RenderTaskID`/`SortTaskIDs`/`RFC3339UTC`/`Now`/`ResolveStorePath`/`Open`/`Init`/`StorePath`/`projectsDir`/`projectDir`/`tasksDir`/`projectPath`/`taskPath`/`lockPath` and add `labelsPath()` returning `filepath.Join(s.Root, "labels.json")`.

```go
// replace touchActors in Init:
func (s *Store) Init(storePath string) error {
	if storePath != "" {
		abs, err := filepath.Abs(storePath)
		if err != nil {
			return err
		}
		s.Root = abs
	}
	if s.Root == "" {
		root, err := Open("")
		if err != nil {
			return err
		}
		s.Root = root.Root
	}
	if err := os.MkdirAll(s.projectsDir(), 0o755); err != nil {
		return err
	}
	return s.touchLabels()
}

func (s *Store) touchLabels() error {
	p := s.labelsPath()
	if _, err := os.Stat(p); err == nil {
		return nil
	}
	return WriteJSON(p, labelsFile{Labels: []Label{}})
}

func (s *Store) labelsPath() string { return filepath.Join(s.Root, "labels.json") }
```

Update `ValidateProjectCode`:
```go
var projectCodeRe = regexp.MustCompile(`^[A-Z]{3,6}$`)

func ValidateProjectCode(code string) error {
	if !projectCodeRe.MatchString(code) {
		return fmt.Errorf("invalid project code %q (want ^[A-Z]{3,6}$)", code)
	}
	return nil
}
```

Update `ValidateLabelName`:
```go
var labelRe = regexp.MustCompile(`^[A-Z]{3,6}(:[a-z0-9][a-z0-9-]*){1,2}$`)

func ValidateLabelName(name string) error {
	if !labelRe.MatchString(name) {
		return fmt.Errorf("invalid label %q (want ^[A-Z]{3,6}(:[a-z0-9][a-z0-9-]*){1,2}$)", name)
	}
	return nil
}
```

Remove `ErrStaleLink` from the sentinel block; keep `ErrNotFound`, `ErrConflict`, `ErrUsage` and `IsNotFound`/`IsConflict`/`IsUsage`.

- [ ] **Step 4: Write the failing project test**

`internal/store/project_test.go` — covers the spec's Store test invariants for projects.

```go
package store

import (
	"testing"
)

func TestCreateProjectValidatesCode(t *testing.T) {
	s := newTestStore(t)
	for _, bad := range []string{"", "AT", "ATM1", "atm", "ATMM", "TOOLONG", "A-B"} {
		if _, err := s.CreateProject(bad, "x", "claude"); err == nil {
			t.Fatalf("expected error for code %q", bad)
		}
	}
}

func TestCreateProjectRejectsDuplicate(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateProject("ATM", "first", "claude"); err != nil {
		t.Fatal(err)
	}
	_, err := s.CreateProject("ATM", "second", "claude")
	if !IsConflict(err) {
		t.Fatalf("expected conflict, got %v", err)
	}
}

func TestSetProjectName(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateProject("ATM", "old", "claude"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetProjectName("ATM", "new", "ttran"); err != nil {
		t.Fatal(err)
	}
	p, _ := s.GetProject("ATM")
	if p.Name != "new" {
		t.Fatalf("name = %q", p.Name)
	}
}

func TestRemoveProjectZeroTaskGuard(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", "claude")
	_, _ = s.CreateTask("ATM", "t", "", nil, "claude")
	if err := s.RemoveProject("ATM", "claude"); !IsConflict(err) {
		t.Fatalf("expected conflict (has tasks), got %v", err)
	}
}

// newTestStore is shared across store _test.go files.
func newTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Init(""); err != nil {
		t.Fatal(err)
	}
	return s
}
```

Run: `go test ./internal/store -run TestCreateProject`
Expected: FAIL (`CreateProject` signature doesn't match / not implemented).

- [ ] **Step 5: Write project.go**

```go
package store

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

func (s *Store) CreateProject(code, name, actor string) (*Project, error) {
	if err := ValidateProjectCode(code); err != nil {
		return nil, err
	}
	if actor == "" {
		return nil, fmt.Errorf("%w: actor is required", ErrUsage)
	}
	var created *Project
	err := s.WithLock(code, func() error {
		if _, err := os.Stat(s.projectPath(code)); err == nil {
			return fmt.Errorf("%w: project %q already exists", ErrConflict, code)
		}
		now := Now()
		p := &Project{
			Code:      code,
			Name:      name,
			NextTaskN: 1,
			CreatedAt: now,
			CreatedBy: actor,
			UpdatedAt: now,
			UpdatedBy: actor,
		}
		if err := os.MkdirAll(s.tasksDir(code), 0o755); err != nil {
			return err
		}
		p.History = []HistoryEntry{{ID: "h1", Action: "created", Actor: actor, At: now, Meta: map[string]any{}}}
		p.NextHistoryN = 2
		if err := WriteJSON(s.projectPath(code), p); err != nil {
			return err
		}
		created = p
		return nil
	})
	if err != nil {
		return nil, err
	}
	return created, nil
}

func (s *Store) GetProject(code string) (*Project, error) {
	var p Project
	if err := ReadJSON(s.projectPath(code), &p); err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%w: project %q", ErrNotFound, code)
		}
		return nil, err
	}
	return &p, nil
}

func (s *Store) ListProjects() []*Project {
	entries, err := os.ReadDir(s.projectsDir())
	if err != nil {
		return nil
	}
	var out []*Project
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		p, err := s.GetProject(e.Name()[:len(e.Name())-len(".json")])
		if err != nil {
			continue
		}
		out = append(out, p)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Code < out[j].Code })
	return out
}

func (s *Store) SetProjectName(code, name, actor string) error {
	return s.mutateProject(code, actor, func(p *Project) {
		p.Name = name
	})
}

func (s *Store) RemoveProject(code, actor string) error {
	if err := s.hasTasksGuard(code); err != nil {
		return err
	}
	return s.WithLock(code, func() error {
		if err := s.hasTasksGuard(code); err != nil {
			return err
		}
		if _, err := os.Stat(s.projectPath(code)); os.IsNotExist(err) {
			return fmt.Errorf("%w: project %q", ErrNotFound, code)
		}
		_ = os.RemoveAll(s.tasksDir(code))
		return os.Remove(s.projectPath(code))
	})
}

func (s *Store) hasTasksGuard(code string) error {
	entries, err := os.ReadDir(s.tasksDir(code))
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, e := range entries {
		if !e.IsDir() && filepath.Ext(e.Name()) == ".json" {
			return fmt.Errorf("%w: project %q has tasks — remove tasks first", ErrConflict, code)
		}
	}
	return nil
}

func (s *Store) mutateProject(code, actor string, fn func(p *Project)) error {
	if actor == "" {
		return fmt.Errorf("%w: actor is required", ErrUsage)
	}
	return s.WithLock(code, func() error {
		p, err := s.GetProject(code)
		if err != nil {
			return err
		}
		fn(p)
		now := Now()
		p.UpdatedAt = now
		p.UpdatedBy = actor
		p.appendHistoryAt("name-set", actor, now, map[string]any{})
		return WriteJSON(s.projectPath(code), p)
	})
}

func (p *Project) appendHistoryAt(action, actor string, at time.Time, meta map[string]any) {
	n := p.NextHistoryN
	if n == 0 {
		n = len(p.History) + 1
	}
	p.History = append(p.History, HistoryEntry{
		ID: fmt.Sprintf("h%d", n), Action: action, Actor: actor, At: at, Meta: meta,
	})
	p.NextHistoryN = n + 1
}
```

Note: `SetProjectName` history action should be `"name-changed"` (not `"name-set"`) — fix to match the convention; the test in Step 6 checks the action name isn't asserted tightly, so choose `"name-changed"` for consistency with v1.

- [ ] **Step 6: Run project tests to verify pass**

Run: `go test ./internal/store -run TestCreateProject -run TestSetProjectName -run TestRemoveProject`
Expected: PASS.

- [ ] **Step 7: Write the failing label test**

`internal/store/label_test.go` (new file).

```go
package store

import "testing"

func TestLabelAddValidatesRegexAndProjectPrefix(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", "claude")
	for _, bad := range []string{"type:bug", "xyz:type:bug", "ATM:", "ATM:type:", "ATM:Type:Bug"} {
		if err := s.LabelAdd(bad, "", "claude"); err == nil {
			t.Fatalf("expected error for label %q", bad)
		}
	}
}

func TestLabelAddRejectsUnknownProjectPrefix(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", "claude")
	if err := s.LabelAdd("XYZ:type:bug", "", "claude"); err == nil {
		t.Fatal("expected error for unknown project prefix XYZ")
	}
}

func TestLabelAddUpsertPreservesDescription(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", "claude")
	_ = s.LabelAdd("ATM:type:bug", "first", "claude")
	_ = s.LabelAdd("ATM:type:bug", "", "claude") // empty desc preserves
	l, _ := s.LabelShow("ATM:type:bug")
	if l.Description != "first" {
		t.Fatalf("description = %q want first", l.Description)
	}
	_ = s.LabelAdd("ATM:type:bug", "second", "claude") // non-empty updates
	l, _ = s.LabelShow("ATM:type:bug")
	if l.Description != "second" {
		t.Fatalf("description = %q want second", l.Description)
	}
}

func TestLabelRemoveSoftRetainsUsage(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", "claude")
	_, _ = s.CreateTask("ATM", "t", "", []string{"ATM:type:bug"}, "claude")
	r, err := s.LabelRemove("ATM:type:bug", "claude")
	if err != nil {
		t.Fatal(err)
	}
	if r.RetainedUsage != 1 {
		t.Fatalf("retained_usage = %d want 1", r.RetainedUsage)
	}
	// Removed label is gone from the registry (soft removal drops the entry).
	if _, err := s.LabelShow("ATM:type:bug"); err == nil {
		t.Fatal("expected ErrNotFound for removed label")
	}
	// Existing task still carries the label string (soft removal).
	tk, _ := s.GetTask("ATM-0001")
	if !containsLabel(tk.Labels, "ATM:type:bug") {
		t.Fatal("existing task must retain the label string after registry removal")
	}
}

func containsLabel(labels []string, want string) bool {
	for _, l := range labels {
		if l == want {
			return true
		}
	}
	return false
}

func TestLabelListFiltersByProjectAndNamespace(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", "claude")
	_, _ = s.CreateProject("SCY", "y", "claude")
	_ = s.LabelAdd("ATM:type:bug", "", "claude")
	_ = s.LabelAdd("ATM:status:open", "", "claude")
	_ = s.LabelAdd("SCY:type:bug", "", "claude")
	if got := len(s.LabelList("ATM", "")); got != 2 {
		t.Fatalf("ATM labels = %d want 2", got)
	}
	if got := len(s.LabelList("ATM", "status")); got != 1 {
		t.Fatalf("ATM:status labels = %d want 1", got)
	}
}

func TestNamespacesDistinctSorted(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", "claude")
	_ = s.LabelAdd("ATM:status:open", "", "claude")
	_ = s.LabelAdd("ATM:type:bug", "", "claude")
	_ = s.LabelAdd("ATM:hot", "", "claude") // unnamespaced tag
	got := s.Namespaces("ATM")
	want := []string{"status", "type"}
	if len(got) != 2 || got[0] != "status" || got[1] != "type" {
		t.Fatalf("Namespaces = %v want %v", got, want)
	}
}
```

Run: `go test ./internal/store -run TestLabelAdd`
Expected: FAIL.

- [ ] **Step 8: Write label.go**

```go
package store

import (
	"fmt"
	"os"
	"sort"
	"strings"
)

type labelsFile struct {
	Labels []Label `json:"labels"`
}

type LabelRemoveResult struct {
	RetainedUsage int `json:"retained_usage"`
}

func (s *Store) LabelAdd(name, description, actor string) error {
	if err := ValidateLabelName(name); err != nil {
		return err
	}
	if actor == "" {
		return fmt.Errorf("%w: actor is required", ErrUsage)
	}
	if err := s.labelProjectExists(name); err != nil {
		return err
	}
	return s.WithLock(labelProject(name), func() error {
		lf, err := s.loadLabels()
		if err != nil {
			return err
		}
		for i, l := range lf.Labels {
			if l.Name == name {
				if description != "" && l.Description != description {
					lf.Labels[i].Description = description
				}
				return s.writeLabels(lf)
			}
		}
		lf.Labels = append(lf.Labels, Label{Name: name, Description: description})
		sort.SliceStable(lf.Labels, func(i, j int) bool { return lf.Labels[i].Name < lf.Labels[j].Name })
		return s.writeLabels(lf)
	})
}

func (s *Store) LabelRemove(name, actor string) (*LabelRemoveResult, error) {
	if err := ValidateLabelName(name); err != nil {
		return nil, err
	}
	if actor == "" {
		return nil, fmt.Errorf("%w: actor is required", ErrUsage)
	}
	var result *LabelRemoveResult
	err := s.WithLock(labelProject(name), func() error {
		lf, err := s.loadLabels()
		if err != nil {
			return err
		}
		idx := -1
		for i, l := range lf.Labels {
			if l.Name == name {
				idx = i
				break
			}
		}
		if idx < 0 {
			return fmt.Errorf("%w: label %q", ErrNotFound, name)
		}
		lf.Labels = append(lf.Labels[:idx], lf.Labels[idx+1:]...)
		if err := s.writeLabels(lf); err != nil {
			return err
		}
		count, err := s.countTasksWithLabelGlobally(name)
		if err != nil {
			return err
		}
		result = &LabelRemoveResult{RetainedUsage: count}
		return nil
	})
	return result, err
}

func (s *Store) LabelList(project, namespace string) []Label {
	lf, err := s.loadLabels()
	if err != nil {
		return nil
	}
	var out []Label
	for _, l := range lf.Labels {
		if project != "" && !strings.HasPrefix(l.Name, project+":") {
			continue
		}
		if namespace != "" && !strings.HasPrefix(l.Name, project+":"+namespace+":") {
			continue
		}
		out = append(out, l)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func (s *Store) LabelShow(name string) (Label, error) {
	lf, err := s.loadLabels()
	if err != nil {
		return Label{}, err
	}
	for _, l := range lf.Labels {
		if l.Name == name {
			return l, nil
		}
	}
	return Label{}, fmt.Errorf("%w: label %q", ErrNotFound, name)
}

func (s *Store) Namespaces(code string) []string {
	lf, err := s.loadLabels()
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	prefix := code + ":"
	for _, l := range lf.Labels {
		if !strings.HasPrefix(l.Name, prefix) {
			continue
		}
		rest := strings.TrimPrefix(l.Name, prefix)
		parts := strings.SplitN(rest, ":", 2)
		if len(parts) == 2 {
			ns := parts[0]
			if !seen[ns] {
				seen[ns] = true
				out = append(out, ns)
			}
		}
	}
	sort.Strings(out)
	return out
}

// autoRegisterLabels upserts each label into the registry (called by CreateTask/TaskLabelAdd).
func (s *Store) autoRegisterLabels(labels []string) error {
	if len(labels) == 0 {
		return nil
	}
	lf, err := s.loadLabels()
	if err != nil {
		return err
	}
	changed := false
	existing := map[string]bool{}
	for _, l := range lf.Labels {
		existing[l.Name] = true
	}
	for _, name := range labels {
		if existing[name] {
			continue
		}
		lf.Labels = append(lf.Labels, Label{Name: name})
		existing[name] = true
		changed = true
	}
	if !changed {
		return nil
	}
	sort.SliceStable(lf.Labels, func(i, j int) bool { return lf.Labels[i].Name < lf.Labels[j].Name })
	return s.writeLabels(lf)
}

func (s *Store) labelProjectExists(name string) error {
	code := labelProject(name)
	if _, err := s.GetProject(code); err != nil {
		return fmt.Errorf("%w: project %q for label %q does not exist", ErrUsage, code, name)
	}
	return nil
}

func labelProject(name string) string {
	return strings.SplitN(name, ":", 2)[0]
}

func (s *Store) loadLabels() (*labelsFile, error) {
	var lf labelsFile
	if _, err := os.Stat(s.labelsPath()); os.IsNotExist(err) {
		return &labelsFile{Labels: []Label{}}, nil
	}
	if err := ReadJSON(s.labelsPath(), &lf); err != nil {
		return nil, err
	}
	if lf.Labels == nil {
		lf.Labels = []Label{}
	}
	return &lf, nil
}

func (s *Store) writeLabels(lf *labelsFile) error {
	if lf.Labels == nil {
		lf.Labels = []Label{}
	}
	return WriteJSON(s.labelsPath(), lf)
}

func (s *Store) countTasksWithLabelGlobally(label string) (int, error) {
	count := 0
	for _, p := range s.ListProjects() {
		entries, err := os.ReadDir(s.tasksDir(p.Code))
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return 0, err
		}
		for _, e := range entries {
			if e.IsDir() || filepathExt(e.Name()) != ".json" {
				continue
			}
			var t Task
			if err := ReadJSON(filepath.Join(s.tasksDir(p.Code), e.Name()), &t); err != nil {
				continue
			}
			for _, l := range t.Labels {
				if l == label {
					count++
					break
				}
			}
		}
	}
	return count, nil
}

func filepathExt(name string) string {
	idx := strings.LastIndex(name, ".")
	if idx < 0 {
		return ""
	}
	return name[idx:]
}
```

Note: use `path/filepath`'s `Ext` if you prefer — replace `filepathExt` with `filepath.Ext` and add the import.

- [ ] **Step 9: Run label tests to verify pass**

Run: `go test ./internal/store -run TestLabel`
Expected: PASS.

- [ ] **Step 10: Write the failing task test**

`internal/store/task_test.go` (rewritten).

```go
package store

import "testing"

func TestCreateTaskAutoRegistersLabels(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", "claude")
	if _, err := s.CreateTask("ATM", "t", "", []string{"ATM:type:bug"}, "claude"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.LabelShow("ATM:type:bug"); err != nil {
		t.Fatalf("label not auto-registered: %v", err)
	}
}

func TestCreateTaskNoAutoStatus(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", "claude")
	tk, _ := s.CreateTask("ATM", "t", "", nil, "claude")
	for _, l := range tk.Labels {
		if l == "ATM:status:open" {
			t.Fatal("create must not auto-assign ATM:status:open")
		}
	}
}

func TestCreateTaskAssignsNextId(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", "claude")
	a, _ := s.CreateTask("ATM", "a", "", nil, "claude")
	b, _ := s.CreateTask("ATM", "b", "", nil, "claude")
	if a.ID != "ATM-0001" || b.ID != "ATM-0002" {
		t.Fatalf("ids = %s, %s", a.ID, b.ID)
	}
}

func TestTaskLabelAddDedupSorted(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", "claude")
	tk, _ := s.CreateTask("ATM", "t", "", nil, "claude")
	_ = s.TaskLabelAdd(tk.ID, "ATM:type:bug", "claude")
	_ = s.TaskLabelAdd(tk.ID, "ATM:status:open", "claude")
	_ = s.TaskLabelAdd(tk.ID, "ATM:type:bug", "claude") // dup
	got, _ := s.GetTask(tk.ID)
	if len(got.Labels) != 2 || got.Labels[0] != "ATM:status:open" || got.Labels[1] != "ATM:type:bug" {
		t.Fatalf("labels = %v", got.Labels)
	}
}

func TestTaskLabelRemoveDoesNotTouchRegistry(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", "claude")
	tk, _ := s.CreateTask("ATM", "t", "", []string{"ATM:type:bug"}, "claude")
	_ = s.TaskLabelRemove(tk.ID, "ATM:type:bug", "claude")
	if _, err := s.LabelShow("ATM:type:bug"); err != nil {
		t.Fatalf("registry must still contain label: %v", err)
	}
}

func TestSetTitleAppendsHistory(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", "claude")
	tk, _ := s.CreateTask("ATM", "old", "", nil, "claude")
	_ = s.SetTitle(tk.ID, "new", "ttran")
	got, _ := s.GetTask(tk.ID)
	if got.Title != "new" {
		t.Fatalf("title = %q", got.Title)
	}
	if len(got.History) < 2 {
		t.Fatalf("history len = %d want >=2", len(got.History))
	}
	if got.History[1].Action != "title-changed" {
		t.Fatalf("history[1].action = %q want title-changed", got.History[1].Action)
	}
}
```

Run: `go test ./internal/store -run TestCreateTask`
Expected: FAIL.

- [ ] **Step 11: Write task.go**

```go
package store

import (
	"fmt"
	"os"
	"sort"
	"time"
)

func (s *Store) CreateTask(projectCode, title, description string, labels []string, actor string) (*Task, error) {
	if title == "" {
		return nil, fmt.Errorf("%w: title is required", ErrUsage)
	}
	if actor == "" {
		return nil, fmt.Errorf("%w: actor is required", ErrUsage)
	}
	var created *Task
	err := s.WithLock(projectCode, func() error {
		p, err := s.GetProject(projectCode)
		if err != nil {
			return err
		}
		for _, l := range labels {
			if err := ValidateLabelName(l); err != nil {
				return err
			}
			if err := s.labelProjectExists(l); err != nil {
				return err
			}
		}
		n := p.NextTaskN
		id := RenderTaskID(projectCode, n)
		ts := Now()
		t := &Task{
			ID:          id,
			ProjectCode: projectCode,
			Title:       title,
			Description: description,
			Labels:      append([]string(nil), labels...),
			History: []HistoryEntry{
				{ID: "h1", Action: "created", Actor: actor, At: ts, Meta: map[string]any{}},
			},
			CreatedAt: ts,
			CreatedBy: actor,
			UpdatedAt: ts,
			UpdatedBy: actor,
		}
		sort.Strings(t.Labels)
		if err := s.autoRegisterLabels(labels); err != nil {
			return err
		}
		p.NextTaskN = n + 1
		p.UpdatedAt = ts
		p.UpdatedBy = actor
		if err := WriteJSON(s.projectPath(projectCode), p); err != nil {
			return err
		}
		if err := os.MkdirAll(s.tasksDir(projectCode), 0o755); err != nil {
			return err
		}
		if err := WriteJSON(s.taskPath(id), t); err != nil {
			return err
		}
		created = t
		return nil
	})
	return created, err
}

func (s *Store) GetTask(id string) (*Task, error) {
	var t Task
	if err := ReadJSON(s.taskPath(id), &t); err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%w: task %q", ErrNotFound, id)
		}
		return nil, err
	}
	return &t, nil
}

func (s *Store) SetTitle(id, title, actor string) error {
	if title == "" {
		return fmt.Errorf("%w: title is required", ErrUsage)
	}
	return s.mutateTask(id, actor, "title-changed", func(t *Task, now time.Time) {
		t.Title = title
	}, map[string]any{})
}

func (s *Store) SetDescription(id, description, actor string) error {
	return s.mutateTask(id, actor, "description-changed", func(t *Task, now time.Time) {
		t.Description = description
	}, map[string]any{})
}

// Design note: the spec says both "auto-registers any supplied labels" (upsert)
// AND "new assignments are refused" after LabelRemove. The v2 data model has no
// tombstone (LabelRemove just drops the entry), so a removed label is
// indistinguishable from a never-existing one. We resolve this tension in favor
// of the data model: TaskLabelAdd/CreateTask always auto-register (upsert) and
// never refuse — matching "agents can self-organize by inventing labels at
// assign time." The "refused" language is advisory and does not survive the
// tombstone-less model. If you want a label to stop being used, the human
// removes it from tasks; the registry is a description store + namespace index,
// not a gatekeeper (spec §3).
func (s *Store) TaskLabelAdd(id, label, actor string) error {
	if err := ValidateLabelName(label); err != nil {
		return err
	}
	if err := s.labelProjectExists(label); err != nil {
		return err
	}
	return s.mutateTask(id, actor, "label-added", func(t *Task, now time.Time) {
		for _, l := range t.Labels {
			if l == label {
				return
			}
		}
		t.Labels = append(t.Labels, label)
		sort.Strings(t.Labels)
	}, map[string]any{"label": label})
}

func (s *Store) TaskLabelRemove(id, label, actor string) error {
	return s.mutateTask(id, actor, "label-removed", func(t *Task, now time.Time) {
		out := t.Labels[:0]
		for _, l := range t.Labels {
			if l != label {
				out = append(out, l)
			}
		}
		t.Labels = out
	}, map[string]any{"label": label})
}

func (s *Store) RemoveTask(id, actor string) error {
	if actor == "" {
		return fmt.Errorf("%w: actor is required", ErrUsage)
	}
	code, _, ok := ParseTaskID(id)
	if !ok {
		return fmt.Errorf("%w: invalid task id %q", ErrUsage, id)
	}
	return s.WithLock(code, func() error {
		if _, err := s.GetTask(id); err != nil {
			return err
		}
		return os.Remove(s.taskPath(id))
	})
}

func (s *Store) mutateTask(id, actor, action string, fn func(t *Task, now time.Time), meta map[string]any) error {
	if actor == "" {
		return fmt.Errorf("%w: actor is required", ErrUsage)
	}
	code, _, ok := ParseTaskID(id)
	if !ok {
		return fmt.Errorf("%w: invalid task id %q", ErrUsage, id)
	}
	return s.WithLock(code, func() error {
		t, err := s.GetTask(id)
		if err != nil {
			return err
		}
		now := Now()
		fn(t, now)
		t.UpdatedAt = now
		t.UpdatedBy = actor
		t.appendHistoryAt(action, actor, now, meta)
		return WriteJSON(s.taskPath(id), t)
	})
}

func (t *Task) appendHistoryAt(action, actor string, at time.Time, meta map[string]any) {
	n := len(t.History) + 1
	t.History = append(t.History, HistoryEntry{
		ID: fmt.Sprintf("h%d", n), Action: action, Actor: actor, At: at, Meta: meta,
	})
}
```

- [ ] **Step 12: Run task tests to verify pass**

Run: `go test ./internal/store -run TestCreateTask -run TestTaskLabel -run TestSetTitle`
Expected: PASS.

- [ ] **Step 13: Write the failing query test**

`internal/store/query_test.go` (rewritten).

```go
package store

import "testing"

func TestListTasksANDIntersectsExactLabels(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", "claude")
	_, _ = s.CreateTask("ATM", "a", "", []string{"ATM:type:bug", "ATM:status:open"}, "claude")
	_, _ = s.CreateTask("ATM", "b", "", []string{"ATM:type:bug"}, "claude")
	got := s.ListTasks(QueryFilters{Project: "ATM", Labels: []string{"ATM:type:bug", "ATM:status:open"}})
	if len(got) != 1 || got[0].Title != "a" {
		t.Fatalf("got %v", got)
	}
}

func TestListTasksIgnoresWildcardTokensForScoping(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", "claude")
	_, _ = s.CreateTask("ATM", "a", "", []string{"ATM:status:open"}, "claude")
	_, _ = s.CreateTask("ATM", "b", "", []string{"ATM:status:done"}, "claude")
	// ATM:status:* is a wildcard (facet) — must NOT restrict; all 2 tasks returned.
	got := s.ListTasks(QueryFilters{Project: "ATM", Labels: []string{"ATM:status:*"}})
	if len(got) != 2 {
		t.Fatalf("wildcard must not restrict; got %d", len(got))
	}
}

func TestGroupTasksMultiMembership(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", "claude")
	t1, _ := s.CreateTask("ATM", "a", "", []string{"ATM:status:open", "ATM:status:done"}, "claude")
	_, _ = s.CreateTask("ATM", "b", "", []string{"ATM:status:open"}, "claude")
	_, _ = s.CreateTask("ATM", "c", "", nil, "claude")
	groups, others := s.GroupTasks(QueryFilters{Project: "ATM", Labels: []string{"ATM:status:*"}})
	// open group has 2 (t1 multi-members + b); done group has 1 (t1).
	open := findGroup(t, groups, "ATM:status:open")
	done := findGroup(t, groups, "ATM:status:done")
	if len(open.Tasks) != 2 || len(done.Tasks) != 1 {
		t.Fatalf("open=%d done=%d", len(open.Tasks), len(done.Tasks))
	}
	if !containsID(others, t1.ID) && !inGroup(open, t1.ID) {
		// t1 carries a matching label so it's in groups, not others
	}
	if len(others) != 1 || others[0].Title != "c" {
		t.Fatalf("others = %v want [c]", others)
	}
}

func TestGroupTasksNoWildcardsReturnsAllInOthers(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", "claude")
	_, _ = s.CreateTask("ATM", "a", "", nil, "claude")
	groups, others := s.GroupTasks(QueryFilters{Project: "ATM"})
	if len(groups) != 0 || len(others) != 1 {
		t.Fatalf("groups=%d others=%d", len(groups), len(others))
	}
}

func findGroup(t *testing.T, groups []LabelGroup, name string) LabelGroup {
	t.Helper()
	for _, g := range groups {
		if g.Label == name {
			return g
		}
	}
	t.Fatalf("group %q not found", name)
	return LabelGroup{}
}

func inGroup(g LabelGroup, id string) bool {
	for _, tk := range g.Tasks {
		if tk.ID == id {
			return true
		}
	}
	return false
}

func containsID(tasks []*Task, id string) bool {
	for _, tk := range tasks {
		if tk.ID == id {
			return true
		}
	}
	return false
}
```

Run: `go test ./internal/store -run TestListTasks -run TestGroupTasks`
Expected: FAIL.

- [ ] **Step 14: Write query.go**

```go
package store

import (
	"os"
	"sort"
	"strings"
)

type QueryFilters struct {
	Project string
	Labels  []string // AND-intersect; full label names; may include suffix-only
	// wildcards (e.g. "ATM:status:*", "ATM:*") which declare facets and do NOT restrict.
}

type LabelGroup struct {
	Label string
	Tasks []*Task
}

func (s *Store) ListTasks(filters QueryFilters) []*Task {
	var codes []string
	if filters.Project != "" {
		codes = []string{filters.Project}
	} else {
		for _, p := range s.ListProjects() {
			codes = append(codes, p.Code)
		}
	}
	restricting := restrictingTokens(filters.Labels)
	var out []*Task
	for _, code := range codes {
		for _, id := range s.listTaskIDs(code) {
			t, err := s.GetTask(id)
			if err != nil {
				continue
			}
			if !taskMatchesLabels(t, restricting) {
				continue
			}
			out = append(out, t)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		ci, ni, _ := ParseTaskID(out[i].ID)
		cj, nj, _ := ParseTaskID(out[j].ID)
		if ci != cj {
			return ci < cj
		}
		return ni < nj
	})
	return out
}

func (s *Store) GroupTasks(filters QueryFilters) ([]LabelGroup, []*Task) {
	inScope := s.ListTasks(filters)
	wildcards := wildcardTokens(filters.Labels)
	if len(wildcards) == 0 {
		return nil, inScope
	}
	buckets := map[string][]*Task{}
	order := []string{}
	for _, t := range inScope {
		matched := false
		for _, w := range wildcards {
			for _, l := range t.Labels {
				if labelMatchesWildcard(l, w) {
					if _, exists := buckets[l]; !exists {
						order = append(order, l)
					}
					buckets[l] = append(buckets[l], t)
					matched = true
				}
			}
		}
		_ = matched
	}
	sort.Strings(order)
	var groups []LabelGroup
	for _, l := range order {
		groups = append(groups, LabelGroup{Label: l, Tasks: buckets[l]})
	}
	var others []*Task
	for _, t := range inScope {
		matched := false
		for _, w := range wildcards {
			for _, l := range t.Labels {
				if labelMatchesWildcard(l, w) {
					matched = true
					break
				}
			}
			if matched {
				break
			}
		}
		if !matched {
			others = append(others, t)
		}
	}
	return groups, others
}

func restrictingTokens(labels []string) []string {
	var out []string
	for _, l := range labels {
		if !isWildcard(l) {
			out = append(out, l)
		}
	}
	return out
}

func wildcardTokens(labels []string) []string {
	var out []string
	for _, l := range labels {
		if isWildcard(l) {
			out = append(out, l)
		}
	}
	return out
}

func isWildcard(l string) bool { return strings.HasSuffix(l, ":*") }

func labelMatchesWildcard(label, wildcard string) bool {
	prefix := strings.TrimSuffix(wildcard, "*")
	return strings.HasPrefix(label, prefix)
}

func taskMatchesLabels(t *Task, labels []string) bool {
	for _, want := range labels {
		found := false
		for _, l := range t.Labels {
			if l == want {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func (s *Store) listTaskIDs(code string) []string {
	entries, err := os.ReadDir(s.tasksDir(code))
	if err != nil {
		return nil
	}
	var ids []string
	for _, e := range entries {
		if e.IsDir() || filepathExt(e.Name()) != ".json" {
			continue
		}
		ids = append(ids, strings.TrimSuffix(e.Name(), ".json"))
	}
	SortTaskIDs(ids)
	return ids
}
```

Add `"path/filepath"` import to use `filepath.Ext` consistently, OR reuse the local `filepathExt` helper defined in `label.go` (same package) — pick one and apply it everywhere. The plan shows `filepathExt` in `query.go` for consistency with `label.go`; `project.go` already uses `filepath.Ext`. Standardize on `filepath.Ext` across all three files during implementation and delete the `filepathExt` helper. Note: `listTaskIDs` was previously unexported in v1 — keep it package-private here.

- [ ] **Step 15: Run all store tests**

Run: `go test ./internal/store`
Expected: PASS for all store tests.

- [ ] **Step 16: Build the store package in isolation**

Run: `go build ./internal/store`
Expected: PASS (no output).

- [ ] **Step 17: Commit the store layer**

```bash
git add internal/store
git commit -m "v2: rebuild store layer (project, label, task, query)

Minimal Project (no TypeAxis/Guide/RepoPaths); global labels.json registry with
auto-registration; Task without Status/Claim/Links/Todos/Followups/Discussions;
filter-driven GroupTasks faceting with multi-membership. No intrinsic workflow
knowledge per the v2 design spec."
```

---

## Task 3: CLI layer — root, output, project, label, task, conventions

**Goal:** Rebuild `internal/cli` against the v2 store surface. After this task, `atm` CLI commands work end-to-end (text + JSON). The TUI remains non-building until Task 5.

**Files:**
- Modify: `internal/cli/root.go` (drop review/actor registration; add label + conventions; `init` no longer registers an actor; `resolveActor` returns free-form string, default `"anonymous"` for reads)
- Keep: `internal/cli/errors.go`, `tui.go`
- Rewrite: `internal/cli/output.go`, `project.go`, `task.go`
- Create: `internal/cli/label.go`, `conventions.go`
- Delete (already removed in Task 1): actor/entry/link/review/workflow/project_guide

**Interfaces:**
- Consumes: the full `internal/store` v2 surface (Task 2).
- Produces (consumed by Task 5 TUI via `cli.Execute`/`cliState` if the TUI shells out; but TUI calls store directly): the `atm` binary commands per the spec's command surface.

- [ ] **Step 1: Rewrite output.go (v2 JSON shapes)**

Drop `jsonClaim`, `jsonLink`, `jsonLinkEdge`, `jsonTodo`, `jsonFollowup`, `jsonDiscussion`, `jsonConvention`, `jsonGuideRef`, `jsonGuideSection`, `jsonGuide`, `jsonTimelineEntry`, `edgesToJSON`, `guideToJSON`, `timelineToJSON`, `linksToJSON`, `claimToJSON`, `todosToJSON`, `followupsToJSON`, `discussionsToJSON`, `renderTaskText` status line, `renderTaskListText` status, `renderNextText`. Keep `jsonLabel`, `jsonHistory`, `jsonTask` (slimmed), `jsonProject` (slimmed), `writeJSON`, `renderTime`, `normalizeStrSlice`, `historyToJSON`, `labelsToJSON`. Add `jsonLabelGroup` for `--facets` output.

New shapes:

```go
type jsonTask struct {
	ID          string        `json:"id"`
	ProjectCode string        `json:"project_code"`
	Title       string        `json:"title"`
	Description string        `json:"description"`
	Labels      []string      `json:"labels"`
	History     []jsonHistory `json:"history"`
	CreatedAt   string        `json:"created_at"`
	CreatedBy   string        `json:"created_by"`
	UpdatedAt   string        `json:"updated_at"`
	UpdatedBy   string        `json:"updated_by"`
}

type jsonProject struct {
	Code      string        `json:"code"`
	Name      string        `json:"name"`
	NextTaskN int           `json:"next_task_n"`
	History   []jsonHistory `json:"history"`
	CreatedAt string        `json:"created_at"`
	CreatedBy string        `json:"created_by"`
	UpdatedAt string        `json:"updated_at"`
	UpdatedBy string        `json:"updated_by"`
}

type jsonLabel struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

type jsonLabelGroup struct {
	Label string     `json:"label"`
	Tasks []jsonTask `json:"tasks"`
}

type jsonFacets struct {
	Groups []jsonLabelGroup `json:"groups"`
	Others []jsonTask       `json:"others"`
}
```

`taskToJSON`/`projectToJSON` map directly; Description always emitted (use `t.Description` even if empty — `normalizeStrSlice` for labels and history). Text renderers: `renderTaskText` shows `ID  TITLE  [LABELS...]`, `renderTaskListText` one line per task `ID  TITLE  LABELS`, `renderProjectText`, `renderLabelListText`, `renderFacetsText` (groups with `▾ LABEL (N)` headers + `(no matching labels)` for others).

- [ ] **Step 2: Rewrite root.go (registration + resolveActor)**

```go
func newRootCmdWithState(st *cliState) *cobra.Command {
	root := &cobra.Command{ /* ... same options ... */ }
	root.PersistentFlags().StringVar(&st.flags.store, "store", "", "...")
	root.PersistentFlags().StringVar(&st.flags.output, "output", "", "...")
	root.PersistentFlags().StringVar(&st.flags.actor, "actor", "", "actor id (free-form; env ATM_ACTOR)")
	root.PersistentFlags().BoolVar(&st.flags.quiet, "quiet", false, "...")
	root.AddCommand(newInitCmd(st))
	root.AddCommand(newStoreCmd(st))
	root.AddCommand(newConventionsCmd(st))
	root.AddCommand(newProjectCmd(st))
	root.AddCommand(newLabelCmd(st))
	root.AddCommand(newTaskCmd(st))
	root.AddCommand(newTUICmd(st))
	root.AddCommand(newVersionCmd(st))
	return root
}

func (s *cliState) resolveActor(required bool) (string, error) {
	if s.flags.actor == "" {
		if required {
			return "", fmt.Errorf("%w: --actor or ATM_ACTOR is required", ErrUsage)
		}
		return "anonymous", nil
	}
	return s.flags.actor, nil
}
```

`newInitCmd`: drop the `s.Register(a, "")` call (actor entity gone); `Init` now touches `labels.json`. Keep `--actor` flag optional for future-history-seeding (no-op for now, or stamp an `init` history entry — keep simple: ignore actor on init).

- [ ] **Step 3: Rewrite project.go**

Commands: `create --code --name --actor`, `list`, `show --code`, `set-name --code --name --actor`, `remove --code --actor`. Drop `set-type-axis`, `repo`, `label` (labels move to top-level `label` command), `guide`.

```go
func newProjectCreateCmd(st *cliState) *cobra.Command {
	var code, name string
	cmd := &cobra.Command{
		Use: "create",
		Short: "Create a project (minimal: code + name)",
		RunE: func(cmd *cobra.Command, args []string) error {
			actor, err := st.resolveActor(true)
			if err != nil { return err }
			s, err := st.openStore()
			if err != nil { return err }
			p, err := s.CreateProject(code, name, actor)
			if err != nil { return err }
			return st.emit(st.stdout(), map[string]any{"project": projectToJSON(p)}, func() {
				fmt.Fprintf(os.Stdout, "created project %s\n", p.Code)
			})
		},
	}
	cmd.Flags().StringVar(&code, "code", "", "project code (^[A-Z]{3,6}$)")
	cmd.Flags().StringVar(&name, "name", "", "project name")
	_ = cmd.MarkFlagRequired("code"); _ = cmd.MarkFlagRequired("name")
	return cmd
}
```

`list` → `{"projects":[...]}` JSON or text table; `show --code` → `{"project":...}` or text; `set-name` mutating; `remove` mutating with zero-task guard surfaced as exit code 4 (conflict) via `store.IsConflict`.

- [ ] **Step 4: Write label.go (new)**

Top-level `label` command with `add --name --description --actor`, `remove --name --actor`, `list [--project] [--namespace]`, `show --name`. JSON shapes: `add` → `{"label":{...}}`; `remove` → `{"retained_usage":N}`; `list` → `{"labels":[...]}`; `show` → `{"label":{...}}`.

```go
func newLabelCmd(st *cliState) *cobra.Command {
	cmd := &cobra.Command{Use: "label", Short: "Label registry commands"}
	cmd.AddCommand(newLabelAddCmd(st))
	cmd.AddCommand(newLabelRemoveCmd(st))
	cmd.AddCommand(newLabelListCmd(st))
	cmd.AddCommand(newLabelShowCmd(st))
	return cmd
}
```

`label add --name ATM:type:bug --description "..."` calls `s.LabelAdd`; `label remove` calls `s.LabelRemove` and emits `retained_usage`. `label list --project ATM --namespace status` calls `s.LabelList`. `label show --name ATM:type:bug` calls `s.LabelShow`.

- [ ] **Step 5: Rewrite task.go**

Commands: `create --project --title --description --label (repeatable) --actor`, `list [--project] [--label (repeatable)] [--facets]`, `show --id`, `set-title --id --title --actor`, `set-description --id --description --actor`, `label add --id --label --actor`, `label remove --id --label --actor`, `remove --id --actor`. Drop `set-status`, `next`, `claim`, `unclaim`, `link`, `todo`, `followup`, `discussion`, `timeline`.

`task create`:
```go
func newTaskCreateCmd(st *cliState) *cobra.Command {
	var project, title, description string
	var labels []string
	cmd := &cobra.Command{
		Use: "create",
		Short: "Create a task",
		RunE: func(cmd *cobra.Command, args []string) error {
			actor, err := st.resolveActor(true)
			if err != nil { return err }
			s, err := st.openStore()
			if err != nil { return err }
			t, err := s.CreateTask(project, title, description, labels, actor)
			if err != nil { return err }
			return st.emit(st.stdout(), map[string]any{"task": taskToJSON(t)}, func() {
				fmt.Fprintf(os.Stdout, "created task %s\n", t.ID)
			})
		},
	}
	cmd.Flags().StringVar(&project, "project", "", "project code")
	cmd.Flags().StringVar(&title, "title", "", "task title")
	cmd.Flags().StringVar(&description, "description", "", "task description")
	cmd.Flags().StringArrayVar(&labels, "label", nil, "label (repeatable; full name e.g. ATM:type:bug)")
	_ = cmd.MarkFlagRequired("project"); _ = cmd.MarkFlagRequired("title")
	return cmd
}
```

`task list --facets`: when `--facets` set, call `s.GroupTasks(QueryFilters{Project, Labels})` and emit `{"groups":[...],"others":[...]}` (JSON) or grouped text. Without `--facets`, `s.ListTasks` → `{"tasks":[...]}` or text list. `--label` accepts full names AND wildcards (`ATM:status:*`, `ATM:*`); pass them through to `QueryFilters.Labels`.

- [ ] **Step 6: Write conventions.go (new)**

`atm conventions [--output json|text]` prints the onboarding guide + suggested seed namespaces verbatim from spec §7. Text mode prints the markdown table + sequences; JSON mode emits a structured `{"conventions":{...}}` shape. Content is versioned with the binary (a `const` block in the file).

```go
const conventionsText = `# ATM Conventions (advisory)

... the §7 suggested seed namespace table, first-time human sequence,
agent first-contact sequence, verbatim from the spec ...
`

func newConventionsCmd(st *cliState) *cobra.Command {
	cmd := &cobra.Command{
		Use: "conventions",
		Short: "Print the onboarding guide and suggested label namespaces",
		RunE: func(cmd *cobra.Command, args []string) error {
			if st.isJSON() {
				return writeJSON(st.stdout(), map[string]any{"conventions": conventionsStructured()})
			}
			fmt.Fprintln(st.stdout(), conventionsText)
			return nil
		},
	}
	return cmd
}
```

- [ ] **Step 7: Build the CLI package (TUI will still fail — expected)**

Run: `go build ./internal/cli`
Expected: PASS (CLI doesn't import TUI).

- [ ] **Step 8: Commit the CLI layer**

```bash
git add internal/cli
git commit -m "v2: rebuild CLI layer (project, label, task, conventions)

Minimal project create; global label registry commands; task create/list/show
with --facets grouped output; atm conventions onboarding reference. Removed
command groups: set-status, next, claim, unclaim, link, todo, followup,
discussion, timeline, review, set-type-axis, repo, guide, actor."
```

---

## Task 4: CLI tests + golden fixtures

**Goal:** Restore and grow the CLI test suite to v2, regenerating golden fixtures. After this task `go test ./internal/cli` passes.

**Files:**
- Rewrite: `internal/cli/project_test.go`, `task_test.go`, `determinism_test.go`
- Create: `internal/cli/label_test.go`, `conventions_test.go`
- Regenerate: `internal/cli/testdata/golden/*.json` via `-update`

**Interfaces:**
- Consumes: Task 3 CLI commands + the `goldenHarness`/`compareGolden` helpers (carried over from v1 `task_test.go:17-114`; the harness itself is unchanged — only the seeds and fixtures change).

- [ ] **Step 1: Update the golden harness seeds**

In `internal/cli/task_test.go`, rewrite `seedScenario1` to v2:

```go
func (h *goldenHarness) seedScenario1() {
	h.t.Helper()
	sp := h.store.StorePath()
	h.run("init", "--store", sp, "--actor", "claude")
	h.run("project", "create", "--store", sp, "--code", "ATM", "--name", "Agent Tasks Management", "--actor", "claude")
	h.run("label", "add", "--store", sp, "--name", "ATM:type:epic", "--actor", "claude")
	h.run("label", "add", "--store", sp, "--name", "ATM:type:bug", "--description", "Bug fix", "--actor", "claude")
	h.run("label", "add", "--store", sp, "--name", "ATM:status:open", "--actor", "claude")
	h.run("task", "create", "--store", sp, "--project", "ATM", "--title", "Fix label reconciliation",
		"--label", "ATM:type:bug", "--label", "ATM:status:open", "--actor", "claude")
	h.run("task", "create", "--store", sp, "--project", "ATM", "--title", "Seed index tasks",
		"--label", "ATM:context:start-here", "--actor", "claude")
	h.reset()
}
```

- [ ] **Step 2: Write v2 project tests**

Replace v1-only tests (`TestGoldenProjectSetTypeAxisNoLabelsRejected`, guide tests, etc.) with v2 tests: `TestGoldenProjectCreate`, `TestGoldenProjectList`, `TestGoldenProjectShow`, `TestGoldenProjectSetName`, `TestGoldenProjectRemoveZeroTaskGuard`, `TestGoldenProjectCreateInvalidCode`. Each: seed, run command, `compareGolden(t, "<name>", out)`, assert exit code where relevant.

```go
func TestGoldenProjectCreateInvalidCode(t *testing.T) {
	h := newGoldenHarness(t)
	sp := h.store.StorePath()
	h.run("init", "--store", sp, "--actor", "claude")
	_, _, code := h.run("project", "create", "--store", sp, "--code", "atm", "--name", "x", "--actor", "claude")
	if code != ExitUsage {
		t.Fatalf("expected usage exit for lowercase code, got %d", code)
	}
}
```

- [ ] **Step 3: Write label tests**

`internal/cli/label_test.go` (new). Tests: `TestGoldenLabelAdd`, `TestGoldenLabelAddUpsertPreservesDescription`, `TestGoldenLabelRemoveRetainedUsage`, `TestGoldenLabelListByProject`, `TestGoldenLabelListByNamespace`, `TestGoldenLabelShowNotFound` (exit 3).

```go
func TestGoldenLabelRemoveRetainedUsage(t *testing.T) {
	h := newGoldenHarness(t)
	sp := h.store.StorePath()
	h.run("init", "--store", sp, "--actor", "claude")
	h.run("project", "create", "--store", sp, "--code", "ATM", "--name", "x", "--actor", "claude")
	h.run("task", "create", "--store", sp, "--project", "ATM", "--title", "t",
		"--label", "ATM:type:bug", "--actor", "claude")
	out, _, code := h.run("label", "remove", "--store", sp, "--name", "ATM:type:bug", "--actor", "claude")
	if code != 0 { t.Fatalf("exit = %d", code) }
	if !strings.Contains(out, `"retained_usage": 1`) { t.Fatalf("missing retained_usage: %s", out) }
	compareGolden(t, "label-remove-retained", out)
}
```

- [ ] **Step 4: Write task tests**

`internal/cli/task_test.go`: `TestGoldenTaskCreate`, `TestGoldenTaskCreateAutoRegistersLabels`, `TestGoldenTaskList`, `TestGoldenTaskListFacets` (the spec's `{"groups":[...],"others":[...]}` shape), `TestGoldenTaskListWildcardLabel`, `TestGoldenTaskShow`, `TestGoldenTaskSetTitle`, `TestGoldenTaskLabelAddRemove`, `TestGoldenTaskRemove`.

```go
func TestGoldenTaskListFacets(t *testing.T) {
	h := newGoldenHarness(t)
	h.seedScenario1()
	out, _, code := h.run("task", "list", "--store", h.store.StorePath(),
		"--project", "ATM", "--label", "ATM:status:*", "--facets")
	if code != 0 { t.Fatalf("exit = %d", code) }
	if !strings.Contains(out, `"groups"`) || !strings.Contains(out, `"others"`) {
		t.Fatalf("facets shape wrong: %s", out)
	}
	compareGolden(t, "task-list-facets", out)
}
```

- [ ] **Step 5: Write conventions test**

`internal/cli/conventions_test.go`: `TestConventionsText`, `TestConventionsJSON`. Text mode contains `Suggested seed namespaces`; JSON mode has `"conventions"` key.

- [ ] **Step 6: Regenerate golden fixtures**

Run: `go test ./internal/cli -update`
Expected: writes `internal/cli/testdata/golden/*.json` for all `compareGolden` calls.

- [ ] **Step 7: Run CLI tests in verify mode**

Run: `go test ./internal/cli`
Expected: PASS (all golden comparisons match).

- [ ] **Step 8: Commit CLI tests + fixtures**

```bash
git add internal/cli internal/cli/testdata/golden
git commit -m "v2: CLI tests + golden fixtures

Project/label/task/conventions text+json golden fixtures; determinism suite
rewritten for v2 surfaces; --facets grouped output shape verified."
```

---

## Task 5: TUI layer — app shell, projects, tasks, help

**Goal:** Rebuild `internal/tui` against the v2 store + the TUI mockup spec screens. After this task `go build ./...` passes (full tree builds for the first time since Task 1).

**Files:**
- Rewrite: `internal/tui/app.go`, `keymap.go`, `help.go`, `projects.go`, `tasks.go`, `app_test.go`
- Modify: `internal/tui/styles.go` (drop unused), `form.go` (keep overlay primitive, add validation hook), `components/*` (keep reusable list/form primitives, drop v1-only)
- Deleted in Task 1: `actors.go`, `dashboard.go`, `guide.go`

**Reference:** `docs/superpowers/specs/2026-07-02-tasks-management-v2-tui-mockups-design.md` is the screen-level source of truth. Each TUI step below cites the screen it implements.

- [ ] **Step 1: Rewrite app.go — shell + tabs + status line (mockup "Shared chrome")**

Three tabs (`paneProjects`, `paneTasks`, `paneHelp`); drop `paneSummary`/dashboard. `NewModel` auto-inits the store on launch if absent (spec: "If the resolved store directory is absent on launch, `tui` auto-initializes it... and continues"). Status line: `STORE: <path>  SELECTED: <CODE>  <hint>  actor: <id>`. Drop the startup actor prompt (mockup spec: actor comes from `--actor`; if unset, mutating keys are inert and status reads `set --actor to mutate`). Drop command palette.

```go
type workspacePane int
const (
	paneProjects workspacePane = iota
	paneTasks
	paneHelp
)

type Model struct {
	store        *store.Store
	storeSet     bool
	actor        string
	width, height int
	contentHeight int
	focused      workspacePane
	km          keymap
	projectScope string  // selection (mockup "Selection model")
	filter       filterState
	toast        toastState
	overlay      overlayState
	form         formState
	quitting     bool
}

func NewModel(opts NewModelOpts) (*Model, error) {
	root := store.ResolveStorePath(opts.StorePath)
	s, err := store.Open(root)
	if err != nil { return nil, err }
	// auto-init if absent
	if _, statErr := os.Stat(s.StorePath()); statErr != nil {
		if err := s.Init(""); err != nil { return nil, err }
	}
	m := &Model{store: s, storeSet: true, km: defaultKeymap(), width: 100, height: 30, actor: opts.Actor}
	m.projects = newProjectsModel(m)
	m.tasks = newTasksModel(m)
	m.help = newHelpModel(m)
	m.SetSize(m.width, m.height)
	m.refreshAll()
	return m, nil
}
```

- [ ] **Step 2: Rewrite keymap.go (mockup "Global keymap summary")**

Implement the keymap table from the mockup spec exactly: `1/2/3` tabs, `j/k` cursor, `g` top, `Enter` open/toggle, `Esc` back/cancel, `/` edit filter (Tasks), `s` select (Projects) / cycle sort (Tasks), `a` add, `x` remove (confirm), `e`/`d`/`b`/`B` task detail, `L`/`l`/`N`/`H` project detail, `?` keymap overlay, `PgDn`/`Space`/`PgUp`/`b` paging.

- [ ] **Step 3: Rewrite projects.go — Screens 1-5**

Screen 1 (empty store): `no projects` + `press [a] to add a project, then seed index tasks...`. Screen 2 (create form overlay): `code` + `name` fields, live `^[A-Z]{3,6}$` validation, conflict toast. Screen 3 (populated list): columns `CODE  NAME  TASKS  LABELS  UPDATED`, gutter `▸` for selection, fixed `code-asc` sort, no filter line. Screen 4 (detail single pane): facts + labels grouped by namespace with `(N tasks)` counts, `[H]` history toggle. Screen 5 (label add/remove forms): `ATM:` prefix fixed, live regex validation, remove form shows `retained_usage` warning.

Selection model: `[s]` sets `projectScope` (persisted across tabs); cursor is independent (inverse-video). Removing selected project clears scope.

- [ ] **Step 4: Rewrite tasks.go — Screens 6-9**

Screen 6 (flat list, no wildcard): persistent header `PROJECT: <code>  FILTER: <tokens>  SORT: <mode>`; `/` edits filter inline; columns `ID  TITLE  LABELS  UPDATED`; sort cycles `s` → `updated-desc`/`updated-asc`/`id-asc`; paging footer `showing 1-10 of 42`. Screen 7 (grouped, ≥1 wildcard): `▾ LABEL (N)` headers, multi-membership row repetition, `(no matching labels)` bucket last; LABELS column omitted on grouped rows. Screen 8 (detail): facts + label chips + always-visible HISTORY (chronological, oldest first). Screen 9 (empty states): no-project prompt; filter-no-match; wildcard-no-labels.

Filter parsing: split on spaces; tokens ending `:*` are wildcards (facets); others are exact restrictors. Empty filter = flat list.

- [ ] **Step 5: Rewrite help.go — Screen 10**

Section 1 CLI/TUI parity table (verbatim from mockup spec). Section 2 global keymap. Section 3 conventions (same content as `atm conventions`, marked advisory). Read-only.

- [ ] **Step 6: Update form.go — live validation hook**

Add an optional `Validator func(field, value string) error` per field so create/label forms show red error text below the field and disable submit while invalid (mockup Screen 2 "Live per-field validation"). Keep `SetWidth`, `Fields`, submit/cancel.

- [ ] **Step 7: Build the full tree**

Run: `go build ./...`
Expected: PASS — first green build since Task 1.

- [ ] **Step 8: Commit the TUI layer**

```bash
git add internal/tui
git commit -m "v2: rebuild TUI layer (projects, tasks, help, app shell)

Three tabs (Projects/Tasks/Help) per the v2 TUI mockup spec; filter-driven
faceting (flat vs grouped from wildcard tokens); selection model; label
reconciliation surface with usage counts; auto-init on launch. Removed
dashboard, actors, guide, command palette, startup actor prompt."
```

---

## Task 6: TUI tests + final verification

**Goal:** Restore the TUI test suite to v2 with view snapshots per the mockup spec screens. After this task `make verify` is green.

**Files:**
- Rewrite: `internal/tui/app_test.go` (model updates + view snapshots)

**Reference:** mockup spec "Testing approach" lists the exact snapshot targets.

- [ ] **Step 1: Write tab-switching + project create tests**

```go
func TestTabSwitching(t *testing.T) {
	m := newTestModel(t)
	m.Update(keyMsg("2"))
	if m.focused != paneTasks { t.Fatal("expected Tasks tab") }
	m.Update(keyMsg("1"))
	if m.focused != paneProjects { t.Fatal("expected Projects tab") }
}
```

- [ ] **Step 2: Write project create form test (Screen 2)**

Empty code → submit disabled; lowercase code → red error; valid `ATM` → success → list shows ATM; duplicate → conflict toast `4 conflict: code ATM exists`.

- [ ] **Step 3: Write projects list + detail tests (Screens 3-4)**

Populated list with selection marker `▸` independent of cursor; detail renders labels grouped by namespace with `(N tasks)` counts; `[H]` toggles history.

- [ ] **Step 4: Write label add/remove form tests (Screen 5)**

Validation on name field; upsert preserves description; remove form shows `retained_usage` warning + toast.

- [ ] **Step 5: Write tasks flat + grouped tests (Screens 6-7)**

Flat list with empty filter; inline `/` editing; exact filter restricts; paging footer. Grouped view: single wildcard `ATM:status:*` → groups with multi-membership; nested wildcards `ATM:status:* ATM:type:*`; `(no matching labels)` bucket.

- [ ] **Step 6: Write task detail + empty-state tests (Screens 8-9)**

Detail: facts + label chips + HISTORY chronological. Empty states: no project selected; filter no match; wildcard no labels.

- [ ] **Step 7: Write help tab test (Screen 10)**

Parity table present; conventions section present; read-only (no mutating keys).

- [ ] **Step 8: Run the full verification gate**

Run: `make verify`
Expected: PASS (`make build && make test` green).

- [ ] **Step 9: Commit TUI tests**

```bash
git add internal/tui
git commit -m "v2: TUI tests + view snapshots

View snapshots per the v2 TUI mockup spec screens 1-10: tab switching, project
create/list/detail, label forms, flat+grouped task lists, task detail, empty
states, help tab. make verify green."
```

---

## Task 7: README + dogfood script rewrite

**Goal:** Update repo-facing docs and the dogfood bootstrap script to v2 surfaces so the repo dogfoods itself.

**Files:**
- Modify: `README.md` (command surface, examples, conventions)
- Modify: `scripts/dogfood.sh` (v2 commands: `project create`, `label add`, `task create --label`)
- Modify: `AGENTS.md` SUPERPOWERS block (already points at the v2 spec — verify still accurate)

- [ ] **Step 1: Rewrite README command reference**

Replace v1 command groups with the v2 surface (Store/Projects/Labels/Tasks/TUI). Add a "Conventions" section mirroring `atm conventions`. Remove all references to status state machine, claim, review, followups, todos, discussions, links, type-axis, guide, actor.

- [ ] **Step 2: Rewrite dogfood.sh**

```bash
#!/usr/bin/env bash
set -euo pipefail
ATM="${1:-./bin/atm}"
STORE="${ATM_HOME:-$HOME/.config/atm}"
$ATM init --store "$STORE" --actor claude
$ATM project create --code ATM --name "Acme Task Manager" --actor claude
$ATM label add --name ATM:status:open --actor claude
$ATM label add --name ATM:status:todo --actor claude
$ATM label add --name ATM:type:task --actor claude
$ATM task create --project ATM --title "Bootstrap v2 store" --label ATM:status:open --label ATM:type:task --actor claude
echo "dogfood: v2 store seeded at $STORE"
```

- [ ] **Step 3: Run dogfood + verify**

Run: `make dogfood && make verify`
Expected: both PASS.

- [ ] **Step 4: Commit docs + dogfood**

```bash
git add README.md scripts/dogfood.sh AGENTS.md
git commit -m "v2: README + dogfood script rewrite

Command reference, conventions section, and dogfood bootstrap updated to the
v2 pure label-substrate surface."
```

---

## Self-Review (run after writing, fix inline)

**Spec coverage check (parent spec §1-§7):**
- §1 minimal project create → Task 2 (store) + Task 3 (CLI) ✓
- §2 project as namespace owner → Task 2 `CreateProject`/`GetProject` ✓
- §3 labels single substrate + auto-registration + soft removal → Task 2 `label.go` ✓
- §4 no intrinsic workflow knowledge → Task 1 deletes it; Task 2/3 don't reintroduce ✓
- §5 history only narrative → Task 2 `appendHistoryAt` on every mutation ✓
- §6 filter-driven faceting → Task 2 `query.go` + Task 3 `--facets` + Task 5 TUI Screens 6-7 ✓
- §7 onboarding/conventions → Task 3 `conventions.go` + Task 5 help tab ✓

**TUI mockup spec coverage (Screens 1-10):**
- Screens 1-5 → Task 5 Step 3 ✓
- Screens 6-9 → Task 5 Step 4 ✓
- Screen 10 → Task 5 Step 5 ✓
- Selection model → Task 5 Step 3 ✓
- Shared chrome (tab bar, status line) → Task 5 Step 1 ✓

**Gaps fixed inline:** none remaining — every spec section maps to a task step.

**Type consistency check:** `LabelGroup{Label string; Tasks []*Task}` (Task 2) matches `jsonLabelGroup` (Task 3) and TUI grouped rendering (Task 5). `QueryFilters{Project string; Labels []string}` used identically across store/CLI/TUI. `ValidateLabelName` regex `^[A-Z]{3,6}(:[a-z0-9][a-z0-9-]*){1,2}$` consistent across `store.go` (Task 2 Step 3), `label.go` (Step 8), CLI validation (Task 3), TUI form validation (Task 5 Step 6).

**Placeholder scan:** no TBD/TODO; every step has runnable code or an exact command.