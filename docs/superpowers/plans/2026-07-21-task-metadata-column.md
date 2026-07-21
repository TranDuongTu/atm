# Task Metadata Column + Capability View Hook Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A per-task, capability-keyed opaque metadata column in the event-sourced substrate, plus an `Annotate` view hook that renders each capability's interpreted cell into a contextual column in the TUI tasks list.

**Architecture:** One new v2 event (`task.capability-meta-set`) folds into per-capability scalar slots (`meta!<name>`) on the task entity, projects into `core.Task.Meta` and a new cache column, and is written only through capability-scoped store/changeset methods. The `Capability` interface gains `Annotate(core.Task) *Cell` (pure read, plain data); the TUI computes cells at refresh time and renders them in a column that follows the existing `capabilityModel.current` selection.

**Tech Stack:** Go (version per `go.mod`), SQLite cache, bubbletea/lipgloss TUI, cobra CLI.

**Spec:** `docs/superpowers/specs/2026-07-21-task-metadata-column-design.md` (revision 2). ATM ledger task: ATM-2e64a5 — journal progress with `atm task comment add --task ATM-2e64a5 --label ATM:comment:progress --body "..."` after each plan task's commit.

## Global Constraints

- The new action string is exactly `task.capability-meta-set`. NEVER reuse `task.meta-changed` — that retired v1 string must stay inert in upgraded logs forever.
- The action constant exists in THREE mirrors that must stay identical: `libs/eventsource/action.go` (exported), `internal/store/eventlog/author.go` (unexported), `internal/store/log.go` (exported, history views).
- Empty payload means "clear the key" — one action, no delete event. The fold treats an empty-string scalar as absent.
- Payloads are opaque strings; nothing outside the owning capability parses them. No board/query surface reads them.
- `Annotate` is pure: value in, data out. No store access, no ANSI/lipgloss in capability packages.
- Never run a dev build against the live store (`~/.config/atm`): this plan bumps `cacheSchemaVersion`, which rewrites the shared cache and breaks the installed binary. Manual testing uses a copy (`cp -r ~/.config/atm /tmp/atm-store-copy` + `--store /tmp/atm-store-copy` or `ATM_HOME`).
- Markdown files: no hard-wrapping; keep prose paragraphs as single lines.
- Commit messages follow the repo convention `<type>(ATM-2e64a5): <summary>` (`feat`/`test`/`fix`/`docs`).
- Run `make test` (all packages) before every commit; a task is done only when the full suite is green.

---

### Task 1: eventsource — action constant, fold slot writes, `TaskState.Meta`

**Files:**
- Modify: `libs/eventsource/action.go` (const block, lines 9-30)
- Modify: `libs/eventsource/fold.go` (field-prefix helpers ~line 20, `writesOf` ~line 61, `TaskState` ~line 216, Pass 2 ~line 347)
- Test: `libs/eventsource/fold_test.go`

**Interfaces:**
- Consumes: existing test helpers `testClock(n)`, `testEvent(t, clock, replica, parents, action, subject, payload)`, `fold(t, events...)`, `replicaA`/`replicaB` (all already in `fold_test.go` / `event_test.go`).
- Produces: `eventsource.ActionTaskCapabilityMetaSet` (string const), `TaskState.Meta map[string]string`, fold semantics: LWW per `(task, capability)` key, empty value = absent key. Later tasks rely on the exact payload field names `capability` and `payload`.

- [ ] **Step 1: Write the failing tests**

Append to `libs/eventsource/fold_test.go`:

```go
func TestFoldTaskCapabilityMeta(t *testing.T) {
	c := testClock(1000)
	created := testEvent(t, c, replicaA, nil, ActionTaskCreated, Subject{Kind: "task"},
		map[string]any{"alias": "T-1", "title": "t"})
	setWF := testEvent(t, c, replicaA, []string{created.ID}, ActionTaskCapabilityMetaSet,
		Subject{Kind: "task", ID: created.ID}, map[string]any{"capability": "workflow_ai", "payload": `{"v":1}`})
	setCM := testEvent(t, c, replicaA, []string{setWF.ID}, ActionTaskCapabilityMetaSet,
		Subject{Kind: "task", ID: created.ID}, map[string]any{"capability": "contextmap", "payload": "cm-state"})
	st := fold(t, created, setWF, setCM)
	tk := st.Tasks[created.ID]
	if tk == nil {
		t.Fatal("task missing")
	}
	if tk.Meta["workflow_ai"] != `{"v":1}` || tk.Meta["contextmap"] != "cm-state" {
		t.Errorf("meta = %+v, want both capabilities' payloads independent", tk.Meta)
	}

	// Overwrite: a later write to the same key wins.
	over := testEvent(t, c, replicaA, []string{setCM.ID}, ActionTaskCapabilityMetaSet,
		Subject{Kind: "task", ID: created.ID}, map[string]any{"capability": "workflow_ai", "payload": `{"v":2}`})
	st = fold(t, created, setWF, setCM, over)
	if got := st.Tasks[created.ID].Meta["workflow_ai"]; got != `{"v":2}` {
		t.Errorf("overwrite = %q, want v2 payload", got)
	}
	if got := st.Tasks[created.ID].Meta["contextmap"]; got != "cm-state" {
		t.Errorf("sibling key disturbed by overwrite: %q", got)
	}

	// Clear via empty payload: the key is absent, not empty.
	clr := testEvent(t, c, replicaA, []string{over.ID}, ActionTaskCapabilityMetaSet,
		Subject{Kind: "task", ID: created.ID}, map[string]any{"capability": "workflow_ai", "payload": ""})
	st = fold(t, created, setWF, setCM, over, clr)
	if _, ok := st.Tasks[created.ID].Meta["workflow_ai"]; ok {
		t.Errorf("cleared key still present: %+v", st.Tasks[created.ID].Meta)
	}

	// A meta-set with no capability field writes no slot (malformed, inert).
	bad := testEvent(t, c, replicaA, []string{clr.ID}, ActionTaskCapabilityMetaSet,
		Subject{Kind: "task", ID: created.ID}, map[string]any{"payload": "orphan"})
	if ws := writesOf(bad); len(ws) != 0 {
		t.Errorf("capability-less meta-set wrote slots: %+v", ws)
	}
}

func TestFoldTaskCapabilityMetaLWWAndContested(t *testing.T) {
	ca, cb := testClock(1000), testClock(2000) // B's stamps are later → B wins LWW
	created := testEvent(t, ca, replicaA, nil, ActionTaskCreated, Subject{Kind: "task"},
		map[string]any{"alias": "T-1", "title": "t"})
	a := testEvent(t, ca, replicaA, []string{created.ID}, ActionTaskCapabilityMetaSet,
		Subject{Kind: "task", ID: created.ID}, map[string]any{"capability": "workflow_ai", "payload": "from A"})
	b := testEvent(t, cb, replicaB, []string{created.ID}, ActionTaskCapabilityMetaSet,
		Subject{Kind: "task", ID: created.ID}, map[string]any{"capability": "workflow_ai", "payload": "from B"})
	st := fold(t, created, a, b)
	if got := st.Tasks[created.ID].Meta["workflow_ai"]; got != "from B" {
		t.Errorf("LWW winner = %q, want the higher HLC", got)
	}
	if len(st.Contested) != 1 {
		t.Fatalf("contested = %+v, want exactly the meta slot", st.Contested)
	}
	cs := st.Contested[0]
	if cs.Entity != created.ID || cs.Kind != SlotScalar || cs.Field != "meta!workflow_ai" {
		t.Errorf("contested slot = %+v", cs)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./libs/eventsource/ -run TestFoldTaskCapabilityMeta -v`
Expected: FAIL to compile — `undefined: ActionTaskCapabilityMetaSet` and `tk.Meta undefined`.

- [ ] **Step 3: Implement**

In `libs/eventsource/action.go`, append inside the existing `const (...)` block, after `ActionProjectCapabilityDisabled`:

```go
	// ActionTaskCapabilityMetaSet writes one capability's opaque payload slot
	// on a task ({capability, payload}); empty payload clears the key. This is
	// deliberately NOT the retired v1 "task.meta-changed" string, which rides
	// through upgraded logs as an unknown action and must stay inert forever.
	ActionTaskCapabilityMetaSet = "task.capability-meta-set"
```

In `libs/eventsource/fold.go`, add next to the `capabilityFieldPrefix` helpers (after line 23):

```go
// metaFieldPrefix namespaces per-capability metadata scalar slots on the task
// entity away from title/description. Same collision argument as
// capabilityFieldPrefix: '!' cannot occur in the fields it must stay disjoint
// from.
const metaFieldPrefix = "meta!"

func metaField(capability string) string { return metaFieldPrefix + capability }
func isMetaField(field string) bool      { return strings.HasPrefix(field, metaFieldPrefix) }
```

In `writesOf`, add a case (place it after `ActionTaskDescChanged`):

```go
	case ActionTaskCapabilityMetaSet:
		if c := str("capability"); c != "" {
			out = append(out, w(e.Subject.ID, SlotScalar, metaField(c), str("payload")))
		}
```

In `TaskState`, add the field:

```go
type TaskState struct {
	EntityMeta
	Title       string
	Description string
	Labels      []string
	// Meta maps capability name → opaque payload. A key whose maximal write
	// is the empty string is absent, not empty — clearing is writing "".
	Meta map[string]string
}
```

In Pass 2 (the sorted-keys loop, currently `if k.kind == SlotMembership && isCapabilityField(k.field) { ... } else if k.kind == SlotMembership { ... }`), add a third branch:

```go
		} else if k.kind == SlotScalar && isMetaField(k.field) {
			if tk := st.Tasks[k.entity]; tk != nil {
				if v := scalarValue(ws); v != "" {
					if tk.Meta == nil {
						tk.Meta = map[string]string{}
					}
					tk.Meta[strings.TrimPrefix(k.field, metaFieldPrefix)] = v
				}
			}
		}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./libs/eventsource/ -v -run TestFold`
Expected: PASS, including every pre-existing fold test (the new branch must not disturb title/description/label folding). Then `go test ./libs/eventsource/` all green — in particular the upgrade test still proves retired `task.meta-changed` writes no slots.

- [ ] **Step 5: Commit**

```bash
git add libs/eventsource/action.go libs/eventsource/fold.go libs/eventsource/fold_test.go
git commit -m "feat(ATM-2e64a5): task.capability-meta-set folds into per-capability meta slots"
```

---

### Task 2: substrate write path — event author, core types/interfaces, snapshot, cache, store facade

**Files:**
- Modify: `internal/core/types.go` (`Task`, line 35-46)
- Modify: `internal/core/repository.go` (`TaskWriter`, line 66-73)
- Modify: `internal/core/service.go` (`TaskService`, line 15-27)
- Modify: `internal/store/log.go` (exported action mirror, line 10-25)
- Modify: `internal/store/eventlog/author.go` (unexported action mirror, line 17-36)
- Modify: `internal/store/eventlog/changeset.go` (TaskWriter section, ~line 190)
- Modify: `internal/store/eventlog/snapshot.go` (`taskFromV2`, line 88-103)
- Modify: `internal/store/cache.go` (`cacheSchemaVersion` line 147, task cache helpers lines 264-320, `cacheListTasksForProject` line 362)
- Modify: `internal/store/cache_schema.go` (tasks DDL, line 25-37)
- Modify: `internal/store/task.go` (facade mutator, end of file)
- Test: `internal/store/task_meta_test.go` (create)

**Interfaces:**
- Consumes: `eventsource.ActionTaskCapabilityMetaSet` semantics from Task 1 (payload fields `capability`, `payload`); existing store test helpers `newTestStore(t)` and `testActor` from `internal/store/project_test.go`.
- Produces: `core.Task.Meta map[string]string` (json `meta,omitempty`); `core.TaskWriter.SetTaskCapabilityMeta(id, capability, payload, actor string) error` (also on `core.TaskService` and thus `core.Service`); `Store.SetTaskCapabilityMeta` with `core.ErrUsage` on empty capability. Tasks 3-5 rely on `GetTask`/`ListTasks` returning populated `Meta`.

- [ ] **Step 1: Write the failing test**

Create `internal/store/task_meta_test.go`:

```go
package store

import (
	"errors"
	"testing"

	"atm/internal/core"
)

func TestSetTaskCapabilityMetaRoundTrip(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateProject("PX", "Proj X", testActor); err != nil {
		t.Fatal(err)
	}
	tk, err := s.CreateTask("PX", "a task", "", nil, testActor)
	if err != nil {
		t.Fatal(err)
	}

	// Set two capabilities' payloads; they are independent keys.
	if err := s.SetTaskCapabilityMeta(tk.ID, "workflow_ai", `{"v":1,"stage":"planned"}`, testActor); err != nil {
		t.Fatal(err)
	}
	if err := s.SetTaskCapabilityMeta(tk.ID, "contextmap", "cm", testActor); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetTask(tk.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Meta["workflow_ai"] != `{"v":1,"stage":"planned"}` || got.Meta["contextmap"] != "cm" {
		t.Errorf("Meta = %+v", got.Meta)
	}

	// Overwrite one key; the sibling survives.
	if err := s.SetTaskCapabilityMeta(tk.ID, "workflow_ai", `{"v":2}`, testActor); err != nil {
		t.Fatal(err)
	}
	got, _ = s.GetTask(tk.ID)
	if got.Meta["workflow_ai"] != `{"v":2}` || got.Meta["contextmap"] != "cm" {
		t.Errorf("after overwrite Meta = %+v", got.Meta)
	}

	// Clear via empty payload: key absent.
	if err := s.SetTaskCapabilityMeta(tk.ID, "workflow_ai", "", testActor); err != nil {
		t.Fatal(err)
	}
	got, _ = s.GetTask(tk.ID)
	if _, ok := got.Meta["workflow_ai"]; ok {
		t.Errorf("cleared key present: %+v", got.Meta)
	}

	// The list path carries Meta too (the TUI reads through ListTasks).
	ts := s.ListTasks(core.QueryFilters{Project: "PX"})
	if len(ts) != 1 || ts[0].Meta["contextmap"] != "cm" {
		t.Errorf("ListTasks Meta = %+v", ts)
	}
}

func TestSetTaskCapabilityMetaGuards(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateProject("PX", "Proj X", testActor); err != nil {
		t.Fatal(err)
	}
	tk, err := s.CreateTask("PX", "a task", "", nil, testActor)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetTaskCapabilityMeta(tk.ID, "", "x", testActor); !errors.Is(err, core.ErrUsage) {
		t.Errorf("empty capability: err = %v, want ErrUsage", err)
	}
	if err := s.SetTaskCapabilityMeta(tk.ID, "workflow_ai", "x", ""); err == nil {
		t.Error("missing actor accepted")
	}
	if err := s.SetTaskCapabilityMeta("PX-ffffff", "workflow_ai", "x", testActor); !errors.Is(err, core.ErrNotFound) {
		t.Errorf("unknown task: err = %v, want ErrNotFound", err)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/store/ -run TestSetTaskCapabilityMeta -v`
Expected: FAIL to compile — `s.SetTaskCapabilityMeta undefined`, `got.Meta undefined`.

- [ ] **Step 3: Implement the core types and interfaces**

`internal/core/types.go` — add to `Task` after `Labels`:

```go
	// Meta maps capability name → opaque payload. The store never interprets
	// a payload; only the owning capability's verbs read or write its own key
	// (docs/architecture/label-substrate-and-capabilities.md, "The metadata
	// column"). Absent key = no state; clearing is writing the empty string.
	Meta map[string]string `json:"meta,omitempty"`
```

`internal/core/repository.go` — add to `TaskWriter`:

```go
	// SetTaskCapabilityMeta writes one capability's opaque payload slot on a
	// task; empty payload clears the key.
	SetTaskCapabilityMeta(id, capability, payload, actor string) error
```

`internal/core/service.go` — add the same signature line to `TaskService` (after `RemoveTask`).

- [ ] **Step 4: Implement the event author and changeset**

`internal/store/log.go` — add to the exported const block: `ActionTaskCapabilityMetaSet = "task.capability-meta-set"`.

`internal/store/eventlog/author.go` — add to the unexported const block: `actionTaskCapabilityMetaSet = "task.capability-meta-set"`.

`internal/store/eventlog/changeset.go` — add to the TaskWriter section (after `RemoveTask`):

```go
// SetTaskCapabilityMeta writes one capability's opaque payload slot on the
// task. The engine enforces nothing about the capability name or the payload
// bytes — which capabilities exist is the composition root's knowledge; the
// log just records the write (empty payload = clear, the fold's absent-key
// rule).
func (cs *changeSet) SetTaskCapabilityMeta(id, capability, payload, actor string) error {
	return cs.mutateTask(id, actionTaskCapabilityMetaSet, actor,
		map[string]any{"capability": capability, "payload": payload})
}
```

- [ ] **Step 5: Implement the snapshot mapping and cache projection**

`internal/store/eventlog/snapshot.go` — in `taskFromV2`, copy the map (never alias fold-owned state):

```go
func taskFromV2(code string, t *eventsource.TaskState, ordinal int) *core.Task {
	labels := append([]string(nil), t.Labels...)
	sort.Strings(labels)
	var meta map[string]string
	if len(t.Meta) > 0 {
		meta = make(map[string]string, len(t.Meta))
		for k, v := range t.Meta {
			meta[k] = v
		}
	}
	return &core.Task{
		ID:          t.Alias,
		ProjectCode: code,
		Title:       t.Title,
		Description: t.Description,
		Labels:      labels,
		Meta:        meta,
		Ordinal:     ordinal,
		CreatedAt:   t.CreatedAt,
		CreatedBy:   t.CreatedBy,
		UpdatedAt:   t.UpdatedAt,
		UpdatedBy:   t.UpdatedBy,
	}
}
```

`internal/store/cache_schema.go` — add to the `tasks` DDL after `alias TEXT NOT NULL DEFAULT ''`:

```sql
	-- meta is NULL when no capability holds state on the task, else a JSON
	-- object {capability: payload}. Opaque to every reader; mirrors
	-- core.Task.Meta. Never queried — boards select over labels only.
	meta TEXT
```

(Watch the comma on the preceding line.)

`internal/store/cache.go`:
1. Bump `const cacheSchemaVersion = 3` (line 147) to `4`.
2. Add helpers next to `capabilitiesToCache`/`capabilitiesFromCache`, same nil-vs-NULL contract:

```go
// taskMetaToCache and taskMetaFromCache round-trip core.Task.Meta through the
// cache's nullable TEXT column: NULL for an empty/absent map, else a JSON
// object {capability: payload}. Same robustness argument as
// capabilitiesToCache: JSON, never ad-hoc joining.
func taskMetaToCache(meta map[string]string) any {
	if len(meta) == 0 {
		return nil
	}
	b, _ := json.Marshal(meta)
	return string(b)
}

func taskMetaFromCache(v sql.NullString) map[string]string {
	if !v.Valid {
		return nil
	}
	var out map[string]string
	_ = json.Unmarshal([]byte(v.String), &out)
	return out
}
```

3. `cacheUpsertTask`: add the `meta` column to the INSERT column list, a `?` to VALUES, `meta=excluded.meta` to the UPDATE SET, and `taskMetaToCache(t.Meta)` to the args.
4. `cacheGetTask`: add `meta` to the SELECT, scan into `var meta sql.NullString`, set `t.Meta = taskMetaFromCache(meta)`.
5. `cacheListTasksForProject`: add `t.meta` to the SELECT, scan into `var meta sql.NullString` (per row), and set `tk.Meta = taskMetaFromCache(meta)` inside the `if !ok` new-task branch.
6. Check for any other task-row scanner: `grep -n "FROM tasks" internal/store/*.go` and extend every query that builds a `*Task` the same way (as of this plan: `cacheGetTask`, `cacheListTasksForProject`; `cacheListTaskIDs` selects ids only and needs nothing).

- [ ] **Step 6: Implement the store facade**

`internal/store/task.go` — append:

```go
// SetTaskCapabilityMeta writes one capability's opaque payload slot on a
// task; empty payload clears the key. The store validates nothing about the
// payload (advisory, always) — but an empty capability name is a caller bug,
// not a record.
func (s *Store) SetTaskCapabilityMeta(id, capability, payload, actor string) error {
	if capability == "" {
		return fmt.Errorf("%w: capability is required", core.ErrUsage)
	}
	return s.mutateTask(id, actor, func(cs core.ChangeSet) error {
		return cs.SetTaskCapabilityMeta(id, capability, payload, actor)
	})
}
```

- [ ] **Step 7: Run the tests**

Run: `go test ./internal/store/ -run TestSetTaskCapabilityMeta -v`
Expected: PASS.
Run: `make test`
Expected: all green. If a `core.Service` fake elsewhere fails to compile (it now misses `SetTaskCapabilityMeta`), add a one-line stub returning nil to that fake — find them with `go build ./... 2>&1`.

- [ ] **Step 8: Commit**

```bash
git add internal/core internal/store libs/eventsource
git commit -m "feat(ATM-2e64a5): task metadata column — write path, projection, cache (schema v4)"
```

---

### Task 3: `atm task show` presence display

**Files:**
- Modify: `internal/cli/output.go` (`jsonTask` struct + `taskToJSON`, ~line 90)
- Modify: `internal/cli/task.go` (`newTaskShowCmd` text branch, ~line 142)
- Test: `internal/cli/output_test.go` (create or append)

**Interfaces:**
- Consumes: `core.Task.Meta` from Task 2.
- Produces: `jsonTask.Meta []jsonMetaPresence` (fields `Capability string`, `Bytes int`), helper `metaPresence(t *core.Task) []jsonMetaPresence` sorted by capability name. Content is NEVER emitted — presence only.

- [ ] **Step 1: Write the failing test**

In `internal/cli/output_test.go` (create the file with `package cli` if absent):

```go
package cli

import (
	"testing"

	"atm/internal/core"
)

func TestMetaPresenceSortedSizesOnly(t *testing.T) {
	tk := &core.Task{ID: "PX-1", Meta: map[string]string{
		"workflow_ai": `{"v":1}`,
		"contextmap":  "cm",
	}}
	got := metaPresence(tk)
	if len(got) != 2 {
		t.Fatalf("presence = %+v", got)
	}
	if got[0].Capability != "contextmap" || got[0].Bytes != 2 {
		t.Errorf("first = %+v, want contextmap/2 (sorted by name, size only)", got[0])
	}
	if got[1].Capability != "workflow_ai" || got[1].Bytes != 7 {
		t.Errorf("second = %+v", got[1])
	}
	if metaPresence(&core.Task{ID: "PX-2"}) != nil {
		t.Error("no meta must yield nil, not empty slice noise")
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/cli/ -run TestMetaPresence -v`
Expected: FAIL to compile — `undefined: metaPresence`.

- [ ] **Step 3: Implement**

`internal/cli/output.go` — find the `jsonTask` struct (same file, above the mappers) and add the field `Meta []jsonMetaPresence \`json:"meta,omitempty"\`` after `Labels`. Then add near `taskToJSON`:

```go
// jsonMetaPresence reports THAT a capability holds metadata on a task and how
// big the payload is — never the content. Opaque is not invisible (degrade,
// never interpret): a disabled or unknown capability's key still lists here.
type jsonMetaPresence struct {
	Capability string `json:"capability"`
	Bytes      int    `json:"bytes"`
}

func metaPresence(t *core.Task) []jsonMetaPresence {
	if len(t.Meta) == 0 {
		return nil
	}
	names := make([]string, 0, len(t.Meta))
	for k := range t.Meta {
		names = append(names, k)
	}
	sort.Strings(names)
	out := make([]jsonMetaPresence, 0, len(names))
	for _, k := range names {
		out = append(out, jsonMetaPresence{Capability: k, Bytes: len(t.Meta[k])})
	}
	return out
}
```

Set `Meta: metaPresence(t)` inside `taskToJSON` (add `"sort"` to the imports if missing).

`internal/cli/task.go` — in `newTaskShowCmd`'s text closure, after the existing `fmt.Fprintf(os.Stdout, "%s\t%s\t%s\n", ...)` line, add:

```go
				for _, p := range metaPresence(t) {
					fmt.Fprintf(os.Stdout, "meta\t%s\t%d bytes\n", p.Capability, p.Bytes)
				}
```

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/cli/ -run TestMetaPresence -v` — PASS. Then `make test` — green.

- [ ] **Step 5: Commit**

```bash
git add internal/cli/output.go internal/cli/output_test.go internal/cli/task.go
git commit -m "feat(ATM-2e64a5): atm task show lists metadata presence (capability + size, never content)"
```

---

### Task 4: capability interface — `Cell`/`Tone`, `Annotate`, `Registry.Annotate`, both implementations

**Files:**
- Modify: `internal/capability/capability.go` (types + interface method + registry helper)
- Create: `internal/capability/workflow/annotate.go`, Test: `internal/capability/workflow/annotate_test.go`
- Create: `internal/capability/contextmap/annotate.go`, Test: `internal/capability/contextmap/annotate_test.go`
- Modify (compile fixes): every fake `capability.Capability` in tests — as of this plan `fakeCap` in `internal/capability/capability_test.go`, `fakeMountCap` in `internal/cli/env_test.go`, and the fake(s) built by `newTestModelWithCaps` in `internal/tui/labels_test.go`. Find the authoritative list with `go build ./... && go test ./... 2>&1 | grep "does not implement"`.

**Interfaces:**
- Consumes: `core.Task` (with `Meta`); workflow's label-name scheme `<CODE>:status:<v>` / `<CODE>:priority:<v>`; contextmap's `ContextKinds`, `LabelContextKind(code, kind)`, `LabelSuperseded(code)` (all existing in `vocabulary.go`).
- Produces: `capability.Cell{Text string; Tone Tone}`, `capability.Tone` constants `ToneNeutral`/`ToneOK`/`ToneAttention`/`ToneStale`, interface method `Annotate(task core.Task) *Cell`, and `Registry.Annotate(capName string, t core.Task) *Cell` (nil for unknown names — including the TUI's `unmanaged` pseudo-capability). Task 5 consumes exactly these.

- [ ] **Step 1: Write the failing tests**

`internal/capability/workflow/annotate_test.go`:

```go
package workflow

import (
	"testing"

	"atm/internal/capability"
	"atm/internal/core"
)

func TestAnnotateFromStatusAndPriority(t *testing.T) {
	cases := []struct {
		name   string
		labels []string
		want   *capability.Cell
	}{
		{"no status", []string{"PX:type:bug"}, nil},
		{"open", []string{"PX:status:open"}, &capability.Cell{Text: "open", Tone: capability.ToneNeutral}},
		{"in-progress", []string{"PX:status:in-progress"}, &capability.Cell{Text: "in-progress", Tone: capability.ToneOK}},
		{"blocked", []string{"PX:status:blocked"}, &capability.Cell{Text: "blocked", Tone: capability.ToneAttention}},
		{"done", []string{"PX:status:done"}, &capability.Cell{Text: "done", Tone: capability.ToneNeutral}},
		{"with priority", []string{"PX:status:open", "PX:priority:high"}, &capability.Cell{Text: "open · high", Tone: capability.ToneNeutral}},
	}
	for _, tc := range cases {
		got := New().Annotate(core.Task{ID: "PX-1", ProjectCode: "PX", Labels: tc.labels})
		if (got == nil) != (tc.want == nil) || (got != nil && *got != *tc.want) {
			t.Errorf("%s: Annotate = %+v, want %+v", tc.name, got, tc.want)
		}
	}
}
```

`internal/capability/contextmap/annotate_test.go`:

```go
package contextmap

import (
	"testing"

	"atm/internal/capability"
	"atm/internal/core"
)

func TestAnnotateFromContextLabels(t *testing.T) {
	cases := []struct {
		name   string
		labels []string
		want   *capability.Cell
	}{
		{"non-context task", []string{"PX:status:open"}, nil},
		{"current pointer", []string{"PX:context:agent"}, &capability.Cell{Text: "agent", Tone: capability.ToneOK}},
		{"superseded pointer", []string{"PX:context:documentation", "PX:knowledge:superseded"}, &capability.Cell{Text: "superseded", Tone: capability.ToneNeutral}},
	}
	for _, tc := range cases {
		got := New().Annotate(core.Task{ID: "PX-1", ProjectCode: "PX", Labels: tc.labels})
		if (got == nil) != (tc.want == nil) || (got != nil && *got != *tc.want) {
			t.Errorf("%s: Annotate = %+v, want %+v", tc.name, got, tc.want)
		}
	}
}
```

Append to `internal/capability/capability_test.go`:

```go
func TestRegistryAnnotateResolvesByName(t *testing.T) {
	var calls []string
	reg := NewRegistry(&fakeCap{name: "workflow", calls: &calls})
	if got := reg.Annotate("nope", core.Task{}); got != nil {
		t.Errorf("unknown name = %+v, want nil", got)
	}
	if got := reg.Annotate("unmanaged", core.Task{}); got != nil {
		t.Errorf("unmanaged pseudo-capability = %+v, want nil", got)
	}
	var nilReg *Registry
	if got := nilReg.Annotate("workflow", core.Task{}); got != nil {
		t.Errorf("nil registry = %+v, want nil", got)
	}
}
```

(Adapt the `fakeCap` construction to its actual fields if they differ; the `calls` field exists today.)

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./internal/capability/... -run "TestAnnotate|TestRegistryAnnotate" -v`
Expected: FAIL to compile — `undefined: capability.Cell`, `Annotate` not on the interface.

- [ ] **Step 3: Implement the types, interface method, and registry helper**

`internal/capability/capability.go` — add above the `Capability` interface:

```go
// Tone is a Cell's semantic emphasis. Capabilities say what a value MEANS;
// the TUI maps tones to theme colors. The contract is plain data — no ANSI,
// no styles — so it survives a future process boundary (third-party
// capability packaging, ATM-e39512).
type Tone int

const (
	ToneNeutral Tone = iota
	ToneOK
	ToneAttention
	ToneStale
)

// Cell is one interpreted annotation for the TUI's contextual column: short
// text plus emphasis, never raw payload bytes.
type Cell struct {
	Text string
	Tone Tone
}
```

Add to the `Capability` interface (after `Exposed`):

```go
	// Annotate renders this capability's interpreted cell for a task — its
	// reading of the task's labels and of its own Meta key. Pure read over
	// the task value: no store access, nil when the capability has nothing
	// to say. A capability whose own payload is unreadable reports that as a
	// cell (degrade, never panic, never leak raw bytes).
	Annotate(task core.Task) *Cell
```

Add the registry helper (near `OwnedLabels`):

```go
// Annotate resolves the named capability and renders its cell for the task.
// Nil for unknown names — including the TUI's "unmanaged" pseudo-capability,
// which is never registered — and when the capability has nothing to say.
func (r *Registry) Annotate(capName string, t core.Task) *Cell {
	if r == nil {
		return nil
	}
	for _, c := range r.caps {
		if c.Name() == capName {
			return c.Annotate(t)
		}
	}
	return nil
}
```

- [ ] **Step 4: Implement both capabilities**

`internal/capability/workflow/annotate.go`:

```go
package workflow

import (
	"strings"

	"atm/internal/capability"
	"atm/internal/core"
)

// Annotate renders the workflow cell from the task's own labels: the status
// value, with the priority value appended when present ("open · high").
// Pure; nil for tasks carrying no status label. This reads labels, not Meta —
// workflow's state IS its labels.
func (c Cap) Annotate(t core.Task) *capability.Cell {
	status := labelValue(t.Labels, t.ProjectCode+":status:")
	if status == "" {
		return nil
	}
	text := status
	if p := labelValue(t.Labels, t.ProjectCode+":priority:"); p != "" {
		text += " · " + p
	}
	tone := capability.ToneNeutral
	switch status {
	case "in-progress":
		tone = capability.ToneOK
	case "blocked":
		tone = capability.ToneAttention
	}
	return &capability.Cell{Text: text, Tone: tone}
}

// labelValue returns the value part of the first label carrying prefix
// ("<CODE>:status:"), or "".
func labelValue(labels []string, prefix string) string {
	for _, l := range labels {
		if strings.HasPrefix(l, prefix) {
			return strings.TrimPrefix(l, prefix)
		}
	}
	return ""
}
```

(If `Cap`'s existing methods use a pointer receiver, match it.)

`internal/capability/contextmap/annotate.go`:

```go
package contextmap

import (
	"slices"

	"atm/internal/capability"
	"atm/internal/core"
)

// Annotate renders the contextmap cell for context pointers, from labels
// only: the pointer kind while current, "superseded" once the lifecycle label
// lands. Nil for non-context tasks. Stamp-based staleness deliberately waits
// for the provenance migration (ATM-a2e902): Annotate is pure over the task,
// and the stamps still live in comments today.
func (c Cap) Annotate(t core.Task) *capability.Cell {
	kind := ""
	for _, k := range ContextKinds {
		if slices.Contains(t.Labels, LabelContextKind(t.ProjectCode, k)) {
			kind = k
			break
		}
	}
	if kind == "" {
		return nil
	}
	if slices.Contains(t.Labels, LabelSuperseded(t.ProjectCode)) {
		return &capability.Cell{Text: "superseded", Tone: capability.ToneNeutral}
	}
	return &capability.Cell{Text: kind, Tone: capability.ToneOK}
}
```

(Match `Cap`'s actual receiver form here too.)

- [ ] **Step 5: Fix every fake implementer**

Run `go build ./... && go test ./... 2>&1 | grep -B1 "does not implement"` and add to each fake capability:

```go
func (f *fakeCap) Annotate(core.Task) *capability.Cell { return nil }
```

(Adjust the receiver/type name per fake; in-package fakes drop the `capability.` qualifier.)

- [ ] **Step 6: Run the tests**

Run: `go test ./internal/capability/... -v` — PASS including the new tests. Then `make test` — green.

- [ ] **Step 7: Commit**

```bash
git add internal/capability internal/cli internal/tui
git commit -m "feat(ATM-2e64a5): Annotate view hook on the capability interface; workflow + contextmap cells"
```

---

### Task 5: TUI contextual column following `capabilityModel.current`

**Files:**
- Modify: `internal/tui/tasks.go` (`toRow`, line 205)
- Modify: `internal/tui/tasks_list.go` (`taskRow` line 11, `taskColumnWidths` line 292, `renderFlatList` line 309, `renderGroup` row branch ~line 444)
- Test: `internal/tui/tasks_test.go` (append)

**Interfaces:**
- Consumes: `Registry.Annotate(capName, task)` and `capability.Cell`/`Tone` from Task 4; existing TUI state `t.m.projectScope`, `t.m.capability.current`, `t.m.capability.unmanagedCurrent()`, `t.m.regFor(scope)`; test harness `newTestModelWithCaps(t, caps...)` (`labels_test.go:660`) and the fake capability it uses (which now has `Annotate`).
- Produces: the contextual column. No new exported surface.

- [ ] **Step 1: Write the failing test**

Append to `internal/tui/tasks_test.go` a test that follows the setup pattern of the nearest existing flat-list rendering test in that file (project created + scoped, a task created via `m.store`, `m.capability.refresh()` + board focus set, then asserting on the rendered pane string). Skeleton to adapt — keep the assertions exactly, adapt the setup lines to the file's existing helpers:

```go
func TestTasksListContextualColumn(t *testing.T) {
	m := newTestModel(t) // registry: workflow + contextmap (the default test registry — if newTestModel wires no caps, use newTestModelWithCaps with workflow.New(), contextmap.New())
	// setup: create project PX, scope it, create one task, then:
	if _, err := m.store.CreateTask("PX", "annotated task", "", nil, m.actor); err != nil {
		t.Fatal(err)
	}
	// give the task a status via the workflow labels the capability reads
	// (TaskLabelAdd "PX:status:in-progress"), refresh the pane.

	view := m.tasks.view()
	if !strings.Contains(view, "WORKFLOW") {
		t.Errorf("column header missing current capability name:\n%s", view)
	}
	if !strings.Contains(view, "in-progress") {
		t.Errorf("column missing workflow cell:\n%s", view)
	}

	// unmanaged hides the column.
	m.capability.switchTo("unmanaged")
	view = m.tasks.view()
	if strings.Contains(view, "WORKFLOW") {
		t.Errorf("unmanaged still shows capability column:\n%s", view)
	}
}
```

(`m.tasks.view()` — use whatever render entry the neighboring tests call; some call a pane-level `View()`.)

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/tui/ -run TestTasksListContextualColumn -v`
Expected: FAIL — header "WORKFLOW" absent (the column does not exist yet).

- [ ] **Step 3: Implement**

`internal/tui/tasks_list.go` — extend `taskRow`:

```go
type taskRow struct {
	id      string
	title   string
	labels  []string
	updated string
	cell    *capability.Cell // current capability's annotation, computed at refresh time
	task    *core.Task
}
```

`internal/tui/tasks.go` — extend `toRow` and add the annotation helper:

```go
func (t *tasksModel) toRow(tk *core.Task) taskRow {
	return taskRow{
		id:      tk.ID,
		title:   tk.Title,
		labels:  tk.Labels,
		updated: relTime(tk.UpdatedAt, core.Now()),
		cell:    t.annotate(tk),
		task:    tk,
	}
}

// annotate renders the current capability's cell at refresh time so the
// per-frame render path stays pure formatting. Nil (no cell, no column) when
// no project is scoped or the unmanaged pseudo-capability is current.
func (t *tasksModel) annotate(tk *core.Task) *capability.Cell {
	scope := t.m.projectScope
	if scope == "" || t.m.capability.unmanagedCurrent() {
		return nil
	}
	return t.m.regFor(scope).Annotate(t.m.capability.current, *tk)
}
```

(Add `"atm/internal/capability"` to both files' imports.)

`internal/tui/tasks_list.go` — column presence + widths. Add:

```go
// metaColumnName returns the contextual column's header (the current
// capability's name, upper-cased), or "" when the column is absent: no scoped
// project, or the unmanaged pseudo-capability (which annotates nothing).
func (t *tasksModel) metaColumnName() string {
	if t.m.projectScope == "" || t.m.capability.unmanagedCurrent() || t.m.capability.current == "" {
		return ""
	}
	return strings.ToUpper(t.m.capability.current)
}

const metaColumnWidth = 18
```

Rework `taskColumnWidths` to yield the meta width (0 when the column is off):

```go
func (t *tasksModel) taskColumnWidths() (idW, metaW, updatedW, titleW int) {
	idW, updatedW = 9, 8
	for _, r := range t.rows {
		if w := len(r.id); w > idW {
			idW = w
		}
	}
	if idW > 14 {
		idW = 14
	}
	if t.metaColumnName() != "" {
		metaW = metaColumnWidth
	}
	pad := 3
	if metaW > 0 {
		pad = 4
	}
	titleW = t.width - idW - metaW - updatedW - pad
	if titleW < 16 {
		titleW = 16
	}
	return
}
```

In `renderFlatList`, replace the header/row formatting with the four-column variant, keeping the cursor row unstyled-then-wrapped exactly as today (tone styling is skipped on the cursor row so ANSI never nests):

```go
	idW, metaW, updatedW, titleW := t.taskColumnWidths()
	var header string
	if metaW > 0 {
		header = fmt.Sprintf(" %-*s %-*s %-*s %*s", idW, "ID", titleW, "TITLE", metaW, t.metaColumnName(), updatedW, "UPDATED")
	} else {
		header = fmt.Sprintf(" %-*s %-*s %*s", idW, "ID", titleW, "TITLE", updatedW, "UPDATED")
	}
```

and per row:

```go
		r := t.rows[i]
		cellTxt := ""
		cellTone := capability.ToneNeutral
		if r.cell != nil {
			cellTxt, cellTone = r.cell.Text, r.cell.Tone
		}
		var line string
		if metaW > 0 {
			plain := fmt.Sprintf(" %-*s %-*s %-*s %*s", idW, truncateRunes(r.id, idW), titleW, truncateRunes(r.title, titleW), metaW, truncateRunes(cellTxt, metaW), updatedW, r.updated)
			if i == t.cursor {
				line = " " + t.m.styles.RowCursor.Render(strings.TrimPrefix(plain, " "))
			} else {
				line = fmt.Sprintf(" %-*s %-*s ", idW, truncateRunes(r.id, idW), titleW, truncateRunes(r.title, titleW)) +
					toneStyle(cellTone).Render(fmt.Sprintf("%-*s", metaW, truncateRunes(cellTxt, metaW))) +
					fmt.Sprintf(" %*s", updatedW, r.updated)
			}
		} else {
			line = fmt.Sprintf(" %-*s %-*s %*s", idW, truncateRunes(r.id, idW), titleW, truncateRunes(r.title, titleW), updatedW, r.updated)
			if i == t.cursor {
				line = " " + t.m.styles.RowCursor.Render(strings.TrimPrefix(line, " "))
			}
		}
```

Add the tone→style mapping in the same file:

```go
// toneStyle maps a Cell's semantic tone to a theme color. The capability
// says what a value means; this is the single place meaning becomes pixels.
func toneStyle(tone capability.Tone) lipgloss.Style {
	switch tone {
	case capability.ToneOK:
		return lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "28", Dark: "42"})
	case capability.ToneAttention:
		return lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "130", Dark: "214"})
	case capability.ToneStale:
		return lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "245", Dark: "246"})
	}
	return lipgloss.NewStyle()
}
```

(Add `"github.com/charmbracelet/lipgloss"` to the imports if the file lacks it.)

In `renderGroup`'s row branch, append the cell inline after `updated`:

```go
			line := fmt.Sprintf("%s%s   id %s   updated %s", rowIndent, truncateRunes(r.title, titleW), r.id, r.updated)
			if r.cell != nil {
				line += "   " + toneStyle(r.cell.Tone).Render(truncateRunes(r.cell.Text, metaColumnWidth))
			}
```

(Skip the append on the cursor row if it nests styles — mirror whatever the flat list does.)

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/tui/ -run TestTasksListContextualColumn -v` — PASS.
Run: `go test ./internal/tui/` — the pre-existing width/rendering tests (`fixedslot_test.go`, `tasks_test.go`, `tasks_boards_authoring_test.go`) will catch any width-math regression; fix until green.

- [ ] **Step 5: Commit**

```bash
git add internal/tui
git commit -m "feat(ATM-2e64a5): contextual column renders the current capability's cells"
```

---

### Task 6: full verification, changelog, ledger

**Files:**
- Modify: `CHANGELOG.md`

- [ ] **Step 1: Full suite + vet**

Run: `make test && go vet ./...`
Expected: green, no vet findings.

- [ ] **Step 2: Manual smoke against a store COPY**

```bash
cp -r ~/.config/atm /tmp/atm-meta-smoke
go build -o /tmp/atm-dev ./cmd/atm
/tmp/atm-dev --store /tmp/atm-meta-smoke task show --task ATM-2e64a5
```
Expected: the first invocation rebuilds the cache (schema v4) and the task renders; no `meta` lines yet (nothing writes metadata). Then open the TUI (`/tmp/atm-dev --store /tmp/atm-meta-smoke`), select project ATM: the tasks pane shows the WORKFLOW column with status cells; `[C]` → contextmap shows kind cells on context tasks; `[C]` → unmanaged hides the column. Delete the copy afterwards.

- [ ] **Step 3: Changelog + ledger + final commit**

Add a CHANGELOG.md entry under the current unreleased heading, following the existing entry style: task metadata column (`task.capability-meta-set`, cache schema v4, `atm task show` presence lines) and the capability-annotated contextual column in the tasks pane.

```bash
git add CHANGELOG.md
git commit -m "docs(ATM-2e64a5): changelog for the task metadata column and contextual column"
atm task comment add --task ATM-2e64a5 --label ATM:comment:progress --body "Implementation complete per docs/superpowers/plans/2026-07-21-task-metadata-column.md: substrate event task.capability-meta-set + fold meta! slots, core.Task.Meta, cache schema v4, Store.SetTaskCapabilityMeta, task show presence, Annotate on the Capability interface (workflow + contextmap cells), TUI contextual column following capabilityModel.current. Full suite green; smoke-tested against a store copy."
```

---

## Self-Review

**Spec coverage:** Meta field/event/fold (Task 1-2), changeset+mutator+actor guard (Task 2), cache column+schema bump+list path (Task 2), presence display (Task 3), no generic CLI writer (nothing added — verified by omission), Cell/Tone/Annotate + registry helper + both day-one implementations + fake fixes (Task 4), TUI column following `capabilityModel.current` with unmanaged hidden + refresh-time annotation + grouped view (Task 5), store-copy discipline + smoke (Task 6). Non-goals respected: no board access to meta (no query touches the column), no append semantics, no per-key audit projection, no new keybinding.

**Placeholder scan:** Task 5 Step 1 asks the implementer to adapt setup lines to the file's existing harness — the assertions and all implementation code are complete; this is deliberate deference to a test harness that varies by neighboring test, not a gap.

**Type consistency:** `SetTaskCapabilityMeta(id, capability, payload, actor string) error` is identical across `core.TaskWriter`, `core.TaskService`, `changeSet`, and `Store`. `metaField`/`isMetaField`/`metaFieldPrefix` match between `writesOf` and Pass 2. `jsonMetaPresence`/`metaPresence` consistent between Steps. `taskColumnWidths` callers updated in the same task that changes its arity.
