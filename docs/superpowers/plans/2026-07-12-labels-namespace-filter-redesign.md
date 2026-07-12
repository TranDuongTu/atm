# Labels Pane Namespace-Filter Redesign Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the Labels pane the single task-filtering surface via a three-level namespace drill-down (table -> chart -> detail), turning the Tasks pane into a selection-only, read-only mirror driven entirely by Labels navigation.

**Architecture:** TUI-only. The Tasks pane gains a `taskFocus` value that selects which subset of the store's already-computed `groups`/`others` (or a trivial predicate) it renders; the Labels pane owns a level state machine that, on entering each level, sets the entire Tasks-pane filter+focus and refreshes. No store or CLI changes.

**Tech Stack:** Go 1.22, Bubble Tea TUI (internal/tui), existing in-process store API (internal/store).

## Global Constraints

- Go 1.22+. Build: `make build`. Test: `make test`. Verify gate: `make verify` (build + test + scripts-test) must pass before a task is done (AGENTS.md).
- No emojis in code or commit messages.
- Store and CLI API surface unchanged: `internal/store` and `internal/cli` are not modified by this plan.
- No hard-wrapping in Markdown edits to docs (project convention).
- Follow existing internal/tui patterns: raw-string key matching in `handleKey`, `dashboardLine(width, ...)` for pane lines, `padToHeight(s, contentHeight)` to fill a pane, table-driven tests using the `newTestModel(t)` / `seedProject(t, m, ...)` / `update(t, m, key)` / `mustContain(t, v, ...)` helpers.
- Spec: docs/superpowers/specs/2026-07-12-labels-namespace-filter-redesign.md (commit 6e7633b) is the source of truth.

---

## File structure

- `internal/tui/app.go` — pane height split (`splitRightColumnHeights`); remove the Labels-specific Esc interception; reset Labels/Tasks state on project switch is delegated to a helper called from projects.go.
- `internal/tui/tasks.go` — new `taskFocus`/`taskFocusMode`, `taskHasBareTag`, focus-aware `refresh()`, focus caption in `headerLine()`, removal of `/`/`c` keys and the `filterEditing` input path.
- `internal/tui/labels.go` — namespace table (L0), cursor chart with `(unset)` (L1), label detail + leaves (L2), the level state machine and per-level state-application methods, synthetic-row counts, removal of the `i` key.
- `internal/tui/projects.go` — call the Labels/Tasks reset on project select.
- `internal/tui/keymap.go`, `internal/tui/help.go` — key documentation.
- `internal/tui/*_test.go` — new tests; obsolete tests updated/removed within the task that changes their behavior.

---

## Task 1: 75/25 Tasks/Labels height split

**Files:**
- Modify: `internal/tui/app.go:296-303` (`splitRightColumnHeights`)
- Test: `internal/tui/app_test.go`

**Interfaces:**
- Produces: `splitRightColumnHeights(height int) (top, bottom int)` — unchanged signature; `top` becomes ~75% of `height`, `bottom` the remainder, with `bottom >= 1` whenever `height >= 2`.

- [ ] **Step 1: Write the failing test**

Add to `internal/tui/app_test.go`:

```go
func TestSplitRightColumnHeights75_25(t *testing.T) {
	cases := []struct {
		height, wantTop, wantBottom int
	}{
		{0, 0, 0},
		{1, 1, 0},
		{2, 1, 1},   // bottom must stay >= 1
		{4, 3, 1},
		{40, 30, 10},
		{100, 75, 25},
	}
	for _, c := range cases {
		top, bottom := splitRightColumnHeights(c.height)
		if top != c.wantTop || bottom != c.wantBottom {
			t.Errorf("splitRightColumnHeights(%d) = (%d,%d) want (%d,%d)", c.height, top, bottom, c.wantTop, c.wantBottom)
		}
		if c.height >= 2 && bottom < 1 {
			t.Errorf("splitRightColumnHeights(%d): bottom=%d must be >= 1", c.height, bottom)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tui/ -run TestSplitRightColumnHeights75_25 -v`
Expected: FAIL (current split is 50/50, so `{40,30,10}` and `{100,75,25}` mismatch).

- [ ] **Step 3: Implement the 75/25 split**

Replace `internal/tui/app.go:296-303` with:

```go
func splitRightColumnHeights(height int) (int, int) {
	if height < 2 {
		return height, 0
	}
	top := height * 75 / 100
	bottom := height - top
	if bottom < 1 {
		bottom = 1
		top = height - bottom
	}
	return top, bottom
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/tui/ -run TestSplitRightColumnHeights75_25 -v`
Expected: PASS

- [ ] **Step 5: Run full package tests**

Run: `go test ./internal/tui/`
Expected: PASS (no other test asserts a specific right-column split).

- [ ] **Step 6: Commit**

```bash
git add internal/tui/app.go internal/tui/app_test.go
git commit -m "feat(tui): split right column 75/25 tasks/labels"
```

---

## Task 2: Tasks pane focus model + selection-only behavior

Adds the `taskFocus` value that Labels navigation will set, makes `refresh()` render the correct subset per focus, replaces the editable filter caption with a read-only focus caption, and removes the `/` and `c` keys. After this task the Labels pane still drives `t.filter` as before (focus defaults to `focusOff`, which preserves today's grouped/flat behavior), so nothing regresses.

**Files:**
- Modify: `internal/tui/tasks.go` — struct (12-40), `refresh` (117-159), `handleFilterEditKey`/`cancelFilterEdit` (380-409), `handleListKey` (411-459), `headerLine` (790-806)
- Test: `internal/tui/tasks_test.go`

**Interfaces:**
- Produces:
  - `type taskFocusMode int` with `focusOff, focusPresent, focusAbsent, focusUnlabeled`.
  - `type taskFocus struct { mode taskFocusMode; ns string; bareTags bool }`.
  - `tasksModel.focus taskFocus` field.
  - `func taskHasBareTag(scope string, t *store.Task) bool`.
  - `func (t *tasksModel) setFocus(f taskFocus, filter string)` — sets `focus` + `filter`, resets cursor, calls `refresh()`. This is the single entry point the Labels pane uses in later tasks.
- Consumes: existing `store.ListTasks`, `store.GroupTasks`, `t.parseFilter`, `t.applySort`, `t.toRow`, `t.clampCursor`.

- [ ] **Step 1: Write failing tests for the predicate + focus rendering**

Add to `internal/tui/tasks_test.go`:

```go
func TestTaskHasBareTag(t *testing.T) {
	mk := func(labels ...string) *store.Task { return &store.Task{ID: "ATM-0001", Labels: labels} }
	if taskHasBareTag("ATM", mk("ATM:status:open")) {
		t.Error("namespaced label must not count as a bare tag")
	}
	if !taskHasBareTag("ATM", mk("ATM:urgent")) {
		t.Error("unnamespaced label must count as a bare tag")
	}
	if taskHasBareTag("ATM", mk()) {
		t.Error("no labels means no bare tag")
	}
	if !taskHasBareTag("ATM", mk("ATM:status:open", "ATM:urgent")) {
		t.Error("mixed labels with one bare tag must count")
	}
}

func TestTasksFocusRendersSubset(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	mustCreate := func(title string, labels ...string) {
		if _, err := m.store.CreateTask("ATM", title, "", labels, m.actor); err != nil {
			t.Fatal(err)
		}
	}
	mustCreate("has-open", "ATM:status:open")
	mustCreate("has-done", "ATM:status:done")
	mustCreate("prio-no-status", "ATM:priority:high")
	mustCreate("bare", "ATM:urgent")
	mustCreate("naked")
	m.projectScope = "ATM"

	// present on status -> grouped, only tasks with a status (others hidden).
	m.tasks.setFocus(taskFocus{mode: focusPresent, ns: "status"}, "ATM:status:*")
	v := m.tasks.View()
	mustContain(t, v, "has-open")
	mustContain(t, v, "has-done")
	mustNotContain(t, v, "prio-no-status")
	mustNotContain(t, v, "naked")

	// absent on status -> only tasks lacking a status.
	m.tasks.setFocus(taskFocus{mode: focusAbsent, ns: "status"}, "ATM:status:*")
	v = m.tasks.View()
	mustContain(t, v, "prio-no-status")
	mustContain(t, v, "bare")
	mustContain(t, v, "naked")
	mustNotContain(t, v, "has-open")

	// present on bare tags -> only tasks carrying a bare tag.
	m.tasks.setFocus(taskFocus{mode: focusPresent, bareTags: true}, "")
	v = m.tasks.View()
	mustContain(t, v, "bare")
	mustNotContain(t, v, "has-open")
	mustNotContain(t, v, "naked")

	// unlabeled -> only the naked task.
	m.tasks.setFocus(taskFocus{mode: focusUnlabeled}, "")
	v = m.tasks.View()
	mustContain(t, v, "naked")
	mustNotContain(t, v, "has-open")
	mustNotContain(t, v, "bare")

	// off with empty filter -> everything.
	m.tasks.setFocus(taskFocus{mode: focusOff}, "")
	v = m.tasks.View()
	mustContain(t, v, "has-open")
	mustContain(t, v, "naked")
	mustContain(t, v, "bare")
}
```

Add this helper near `mustContain` in `internal/tui/tasks_test.go` (only if the package does not already define it — grep first: `grep -rn "func mustNotContain" internal/tui/`):

```go
func mustNotContain(t *testing.T, haystack, needle string) {
	t.Helper()
	if strings.Contains(haystack, needle) {
		t.Fatalf("expected output NOT to contain %q\n---\n%s", needle, haystack)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/tui/ -run 'TestTaskHasBareTag|TestTasksFocusRendersSubset' -v`
Expected: FAIL to compile (`taskFocus`, `focusPresent`, `setFocus`, `taskHasBareTag` undefined).

- [ ] **Step 3: Add the focus types, field, predicate, and setter**

In `internal/tui/tasks.go`, add after the `sortMode` const block (after line 55):

```go
type taskFocusMode int

const (
	// focusOff renders whatever t.filter yields: empty filter -> all tasks
	// flat (L0); an exact label token -> that label's tasks flat (L2).
	focusOff taskFocusMode = iota
	// focusPresent renders tasks that carry the namespace. Real namespace:
	// grouped via GroupTasks with others hidden. bareTags: flat predicate.
	focusPresent
	// focusAbsent renders tasks that do NOT carry the namespace. Real
	// namespace: the GroupTasks others bucket, flat. bareTags: flat predicate.
	focusAbsent
	// focusUnlabeled renders tasks with zero labels.
	focusUnlabeled
)

// taskFocus is the Tasks-pane view state the Labels pane sets on each level
// entry. ns names a real namespace for present/absent; bareTags switches
// present/absent to operate on unnamespaced (bare) labels instead.
type taskFocus struct {
	mode     taskFocusMode
	ns       string
	bareTags bool
}
```

Add the `focus` field to `tasksModel` (inside the struct at ~26, next to `filter`):

```go
	// filter / sort / focus
	filter   string
	sortMode sortMode
	focus    taskFocus
```

(Delete the now-unused `filterEdit` and `filterEditing` fields from the struct — they are removed in Step 5.)

Add the predicate and setter (near `parseFilter`, after line 207):

```go
// taskHasBareTag reports whether t carries at least one unnamespaced (bare)
// label — a label whose suffix after the "<scope>:" prefix contains no colon.
func taskHasBareTag(scope string, t *store.Task) bool {
	for _, full := range t.Labels {
		suffix := strings.TrimPrefix(full, scope+":")
		if !strings.Contains(suffix, ":") {
			return true
		}
	}
	return false
}

// setFocus applies a complete Tasks-pane view state (focus + filter) in one
// step, resets the cursor, and refreshes. This is the single channel the
// Labels pane drives; the Tasks pane never edits its own filter.
func (t *tasksModel) setFocus(f taskFocus, filter string) {
	t.focus = f
	t.filter = filter
	t.cursor = 0
	t.offset = 0
	t.refresh()
}
```

- [ ] **Step 4: Make `refresh()` branch on focus**

Replace the body of `refresh()` (`internal/tui/tasks.go:117-159`) with:

```go
func (t *tasksModel) refresh() {
	t.rows = nil
	t.groups = nil
	t.others = nil
	if t.m.projectScope == "" {
		t.clampCursor()
		return
	}
	scope := t.m.projectScope
	switch t.focus.mode {
	case focusUnlabeled:
		for _, tk := range t.applySort(t.m.store.ListTasks(store.QueryFilters{Project: scope})) {
			if len(tk.Labels) == 0 {
				t.rows = append(t.rows, t.toRow(tk))
			}
		}
	case focusPresent, focusAbsent:
		if t.focus.bareTags {
			for _, tk := range t.applySort(t.m.store.ListTasks(store.QueryFilters{Project: scope})) {
				has := taskHasBareTag(scope, tk)
				if (t.focus.mode == focusPresent) == has {
					t.rows = append(t.rows, t.toRow(tk))
				}
			}
			break
		}
		filters := t.parseFilter()
		groups, others := t.m.store.GroupTasks(store.QueryFilters{Project: scope, Labels: filters})
		if t.focus.mode == focusPresent {
			wildcards := wildcardTokens(filters)
			for _, g := range groups {
				rows := make([]taskRow, 0, len(g.Tasks))
				for _, tk := range g.Tasks {
					rows = append(rows, t.toRow(tk))
				}
				tg := taskGroup{label: g.Label, rows: rows}
				if len(wildcards) >= 2 {
					tg.subgroups = buildNestedGroups(g.Tasks, wildcards[1:], t.toRow)
					tg.rows = nil
				}
				t.groups = append(t.groups, tg)
			}
		} else {
			for _, tk := range t.applySort(others) {
				t.rows = append(t.rows, t.toRow(tk))
			}
		}
	default: // focusOff
		filters := t.parseFilter()
		ts := t.applySort(t.m.store.ListTasks(store.QueryFilters{Project: scope, Labels: filters}))
		if len(wildcardTokens(filters)) > 0 {
			groups, others := t.m.store.GroupTasks(store.QueryFilters{Project: scope, Labels: filters})
			for _, g := range groups {
				rows := make([]taskRow, 0, len(g.Tasks))
				for _, tk := range g.Tasks {
					rows = append(rows, t.toRow(tk))
				}
				tg := taskGroup{label: g.Label, rows: rows}
				if len(wildcardTokens(filters)) >= 2 {
					tg.subgroups = buildNestedGroups(g.Tasks, wildcardTokens(filters)[1:], t.toRow)
					tg.rows = nil
				}
				t.groups = append(t.groups, tg)
			}
			for _, tk := range others {
				t.others = append(t.others, t.toRow(tk))
			}
		} else {
			for _, tk := range ts {
				t.rows = append(t.rows, t.toRow(tk))
			}
		}
	}
	t.clampCursor()
}
```

Note: `focusPresent` on a real namespace populates `t.groups` (so the grouped renderer shows only the groups — `t.others` stays nil, hiding the "no matching labels" bucket). `focusAbsent`, `focusUnlabeled`, and bareTags-present populate `t.rows` (flat). Because `focusAbsent`'s filter still contains the wildcard, the render choice must key off focus, not `t.hasWildcard()` — Step 4b adds that.

- [ ] **Step 4b: Route rendering by focus, not by wildcard presence**

Add a `grouped()` predicate to `internal/tui/tasks.go` and use it in `renderList`:

```go
// grouped reports whether the list should render as grouped facets (vs a flat
// row list). Only a real-namespace present focus (or a legacy focusOff wildcard
// filter) groups; absent/unlabeled/bare-tag focuses are always flat, even
// though their filter may still carry a wildcard token.
func (t *tasksModel) grouped() bool {
	switch t.focus.mode {
	case focusPresent:
		return !t.focus.bareTags
	case focusOff:
		return t.hasWildcard()
	default:
		return false
	}
}
```

In `renderList` (`internal/tui/tasks.go:823`), change `if t.hasWildcard() {` to `if t.grouped() {`.

- [ ] **Step 5: Remove the `/` and `c` keys and the filter-edit input**

In `internal/tui/tasks.go`:

Delete the `filterEditing` short-circuit at the top of `handleKey` (lines 367-370) so it reads:

```go
func (t *tasksModel) handleKey(k tea.KeyMsg) tea.Cmd {
	switch t.view {
	case tViewList:
		return t.handleListKey(k)
	case tViewDetail:
		return t.handleDetailKey(k)
	}
	return nil
}
```

Delete `handleFilterEditKey` (380-404) and `cancelFilterEdit` (406-409) entirely.

Delete the `case "/":` (426-431) and `case "c":` (432-438) branches from `handleListKey`.

Update the Tasks list status hint (`internal/tui/tasks.go:1074-1077`) — it lists the removed keys and references `filterEditing`. Replace those four lines so the function ends:

```go
	return "[s]ort [a]dd [Enter]detail [?]keys"
}
```

(Remove the `if t.filterEditing { ... }` branch and the `[/]filter [c]lear` text; the surrounding `if t.view == tViewDetail` branch above it is unchanged.)

- [ ] **Step 6: Replace the filter caption with a read-only focus caption**

Replace `headerLine` (`internal/tui/tasks.go:790-806`) with:

```go
func (t *tasksModel) headerLine() string {
	proj := t.m.projectScope
	if proj == "" {
		proj = "(none)"
	}
	return fmt.Sprintf("PROJECT: %s    FOCUS: %s    SORT: %s", proj, t.focusCaption(), t.sortMode)
}

// focusCaption is a read-only description of why the Tasks list is scoped,
// derived from the focus set by the Labels pane. Empty focus reads "(all)".
func (t *tasksModel) focusCaption() string {
	switch t.focus.mode {
	case focusPresent:
		if t.focus.bareTags {
			return "bare tags"
		}
		return t.focus.ns
	case focusAbsent:
		if t.focus.bareTags {
			return "no bare tags"
		}
		return "no " + t.focus.ns
	case focusUnlabeled:
		return "unlabeled"
	default: // focusOff
		if f := strings.TrimSpace(t.filter); f != "" {
			return f // exact-label token at L2
		}
		return "(all)"
	}
}
```

Also update the empty-flat-list hint in `renderFlatList` (`internal/tui/tasks.go:855-861`): it references the removed `[/]` key. Replace that `renderEmptyState` call's last line so it no longer tells the user to edit the filter:

```go
		t.renderEmptyState(b, []string{
			t.m.styles.EmptyHead.Render("no tasks match this focus"),
			"",
			t.m.styles.EmptyText.Render("choose a namespace or label in the Labels pane to change focus"),
		})
```

- [ ] **Step 7: Update obsolete Tasks tests**

Delete `TestTasksClearFilterKey` (`internal/tui/tasks_test.go:216-226`) — the `c` clear-filter key no longer exists. Its intent (filter changes) is now covered by `TestTasksFocusRendersSubset`.

Grep for other references to the removed identifiers and fix or delete them:

```bash
grep -rn "filterEditing\|filterEdit\|cancelFilterEdit\|handleFilterEditKey\|FILTER:" internal/tui/
```

Any test asserting `FILTER:` in the Tasks header must assert `FOCUS:` instead. Any test driving `/` or `c` in the Tasks pane must be removed.

- [ ] **Step 8: Run tests**

Run: `go test ./internal/tui/ -run 'TestTaskHasBareTag|TestTasksFocusRendersSubset' -v`
Expected: PASS

Run: `go test ./internal/tui/`
Expected: PASS (after Step 7 fixes). Removing the `filterEditing`/`filterEdit` struct fields breaks two `app.go` references that must be deleted in this same task to keep the package compiling:
- `internal/tui/app.go:530-532` — delete the whole block:
  ```go
  	if m.focused == paneTasks && m.tasks.filterEditing {
  		return m.tasks.handleKey(k)
  	}
  ```
- `internal/tui/app.go:610-613` — delete the `if m.tasks.filterEditing { m.tasks.cancelFilterEdit(); return nil }` block inside the Esc handler (it is dead once the keys and the field are gone).

After these deletions, `grep -rn "filterEditing\|filterEdit\b" internal/tui/` must return nothing.

Run: `make verify`
Expected: PASS

- [ ] **Step 9: Commit**

```bash
git add internal/tui/tasks.go internal/tui/tasks_test.go internal/tui/app.go
git commit -m "feat(tui): tasks pane focus model, drop filter input"
```

---

## Task 3: Labels L0 namespace table + level state machine (L0/L1-basic/none-leaf)

Replaces the flat entries list with a namespace table (L0), introduces the level state machine and the per-level state-application methods that drive the Tasks pane via `setFocus`, wires L0 navigation (Enter drills into a namespace; `(none)` filters unlabeled; Esc clears), removes the `i` key, removes the Labels Esc interception in app.go, and resets Labels/Tasks state on project switch. The L1 chart in this task is a simple non-cursor list of the namespace's labels with counts; Task 4 makes it cursor-navigable and adds `(unset)` and detail.

**Files:**
- Modify: `internal/tui/labels.go` — struct (101-148), `refresh`/`rebuildEntries` (169-229), `handleListKey`/`handleDetailKey` (241-336), `toggleNamespaceFacet`/`toggleLabelFacet` (362-391), `View`/`renderList`/`renderChart`/`statusHint` (403-579)
- Modify: `internal/tui/app.go` — Esc interception (615-622)
- Modify: `internal/tui/projects.go` — project select (`s` at 228-247)
- Test: `internal/tui/labels_test.go`

**Interfaces:**
- Produces:
  - `type lLevel int` with `lLevelTable, lLevelChart, lLevelDetail`.
  - `labelsModel` fields: `level lLevel`, `ns string` (active namespace at chart/detail; `bareTags bool` when the active namespace is the tags pseudo-namespace), `cursor int` (indexes the current level's rows).
  - `type nsRow struct { key string; display string; tasks int; labels int; bareTags bool; none bool }` — one L0 table row. `key` is the namespace name (or "" for the tags/none synthetic rows, distinguished by `bareTags`/`none`).
  - `labelsModel.nsRows []nsRow`.
  - `func (l *labelsModel) enterTable()`, `func (l *labelsModel) enterChart(r nsRow)`, `func (l *labelsModel) enterNoneLeaf()` — each sets the full Tasks-pane state via `l.m.tasks.setFocus(...)`.
  - `func (l *labelsModel) reset()` — return to L0 and clear Tasks focus; called on project switch.
- Consumes: `store.LabelList`, `store.ListTasks`, `facetToken`, `tasksModel.setFocus`, `taskHasBareTag`, `dashboardLine`, `padToHeight`, `windowLines`, `meterBar` (Task 4).

- [ ] **Step 1: Write failing tests for the namespace table + counts**

Replace the obsolete entries/header tests. First delete these now-invalid tests from `internal/tui/labels_test.go`: `TestLabelsEntriesIncludeNamespaceHeaders`, `TestLabelsCursorCanReachNamespaceHeader`, `TestLabelsEnterOnNamespaceTogglesFacetAndChart`, `TestLabelsEnterOnTagsHeaderIsNoop`, `TestLabelsChartSelfHealsWhenFilterEditedAway`, `TestLabelsEscClosesChartWithoutClearingFilter`, and the `cursorToNamespaceHeader` helper. (Tests for `i` and exact-label toggle are removed in this task too: `TestLabelDetailDashboardSections`, `TestLabelsEnterOnRowTogglesExactLabelFilter`, `TestLabelsIKeyOpensLabelDetail`, `TestLabelsIKeyOnHeaderIsNoop` — grep to confirm their line ranges before deleting.)

Add:

```go
func TestLabelsL0NamespaceTableCounts(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	mk := func(title string, labels ...string) {
		if _, err := m.store.CreateTask("ATM", title, "", labels, m.actor); err != nil {
			t.Fatal(err)
		}
	}
	mk("a", "ATM:status:open")
	mk("b", "ATM:status:done", "ATM:priority:high")
	mk("c", "ATM:priority:high")
	mk("d", "ATM:urgent") // bare tag
	mk("e")               // no labels
	m.store.LabelAdd("ATM:urgent", "", m.actor)
	update(t, m, "s")
	update(t, m, "3")

	byKey := map[string]nsRow{}
	for _, r := range m.labels.nsRows {
		k := r.key
		if r.bareTags {
			k = "__tags__"
		}
		if r.none {
			k = "__none__"
		}
		byKey[k] = r
	}
	if got := byKey["status"].tasks; got != 2 {
		t.Errorf("status tasks = %d want 2", got)
	}
	if got := byKey["priority"].tasks; got != 2 {
		t.Errorf("priority tasks = %d want 2", got)
	}
	if got := byKey["__tags__"].tasks; got != 1 {
		t.Errorf("tags tasks = %d want 1", got)
	}
	if got := byKey["__none__"].tasks; got != 1 {
		t.Errorf("none tasks = %d want 1", got)
	}
	// Table view renders the header and a namespace row.
	v := m.labels.View()
	mustContain(t, v, "NAMESPACE")
	mustContain(t, v, "status")
}

func TestLabelsL0EnterDrillsIntoNamespaceAndFocusesTasks(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	if _, err := m.store.CreateTask("ATM", "a", "", []string{"ATM:status:open"}, m.actor); err != nil {
		t.Fatal(err)
	}
	update(t, m, "s")
	update(t, m, "3")
	cursorToNamespaceRow(t, m, "status")
	update(t, m, "enter")
	if m.labels.level != lLevelChart {
		t.Fatalf("level = %v want chart", m.labels.level)
	}
	if m.tasks.filter != "ATM:status:*" {
		t.Fatalf("filter = %q want ATM:status:*", m.tasks.filter)
	}
	if m.tasks.focus.mode != focusPresent || m.tasks.focus.ns != "status" {
		t.Fatalf("focus = %+v want present/status", m.tasks.focus)
	}
	// Esc returns to the table and clears the focus.
	update(t, m, "esc")
	if m.labels.level != lLevelTable {
		t.Fatalf("level = %v want table after esc", m.labels.level)
	}
	if m.tasks.filter != "" || m.tasks.focus.mode != focusOff {
		t.Fatalf("focus/filter not cleared after esc: %q %+v", m.tasks.filter, m.tasks.focus)
	}
}

func TestLabelsL0EnterNoneFiltersUnlabeled(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	if _, err := m.store.CreateTask("ATM", "naked", "", nil, m.actor); err != nil {
		t.Fatal(err)
	}
	update(t, m, "s")
	update(t, m, "3")
	cursorToNoneRow(t, m)
	update(t, m, "enter")
	if m.tasks.focus.mode != focusUnlabeled {
		t.Fatalf("focus = %+v want unlabeled", m.tasks.focus)
	}
	update(t, m, "esc")
	if m.tasks.focus.mode != focusOff || m.labels.level != lLevelTable {
		t.Fatalf("esc from none leaf did not return to table/clear: %+v %v", m.tasks.focus, m.labels.level)
	}
}
```

Add these test helpers to `internal/tui/labels_test.go`:

```go
func cursorToNamespaceRow(t *testing.T, m *Model, ns string) {
	t.Helper()
	for i, r := range m.labels.nsRows {
		if r.key == ns && !r.bareTags && !r.none {
			m.labels.cursor = i
			return
		}
	}
	t.Fatalf("namespace row %q not found", ns)
}

func cursorToNoneRow(t *testing.T, m *Model) {
	t.Helper()
	for i, r := range m.labels.nsRows {
		if r.none {
			m.labels.cursor = i
			return
		}
	}
	t.Fatalf("(none) row not found")
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/tui/ -run 'TestLabelsL0' -v`
Expected: FAIL to compile (`nsRow`, `lLevelChart`, `l.level`, etc. undefined).

- [ ] **Step 3: Rewrite the labels model struct + level types**

In `internal/tui/labels.go`, replace the `labelsModel` struct and the `lView` block (101-148) with:

```go
type labelsModel struct {
	m             *Model
	width         int
	contentHeight int
	rows          []labelRow // all project labels (flat, used to build tables/charts)
	nsRows        []nsRow    // L0 table rows
	level         lLevel
	ns            string // active namespace at chart/detail (real ns name; "" when bareTags)
	bareTags      bool   // active pseudo-namespace is the bare-tags bucket
	cursor        int    // indexes the current level's row slice
	offset        int
	pageSize      int
	detail        labelDetailState
}

type lLevel int

const (
	lLevelTable  lLevel = iota // L0 namespace table
	lLevelChart                // L1 per-namespace chart
	lLevelDetail               // L2 label detail (or unset/none leaf)
)

type labelRow struct {
	suffix      string
	full        string
	description string
	usage       int
}

// nsRow is one row of the L0 namespace table. For real namespaces key is the
// namespace name; the synthetic rows set bareTags or none instead.
type nsRow struct {
	key      string
	display  string
	tasks    int
	labels   int
	bareTags bool
	none     bool
}

type labelDetailState struct {
	row  labelRow
	leaf string // "" for a real label; "unset" or "none" for the synthetic leaves
}
```

Delete the old `labelEntryKind`/`labelEntry` types and `entryHeaderNS`/`entryHeaderTags`/`entryRow` consts (129-144) — the entries list is replaced by `nsRows` and the chart's own row list.

- [ ] **Step 4: Rewrite `refresh` to build the namespace table**

Replace `refresh` and `rebuildEntries` (169-229) with:

```go
func (l *labelsModel) refresh() {
	l.rows = nil
	l.nsRows = nil
	if l.m.projectScope == "" {
		return
	}
	scope := l.m.projectScope
	ls := l.m.store.LabelList(scope, "")
	usage, _ := l.m.store.LabelUsageGrouped(scope)
	for _, lab := range ls {
		l.rows = append(l.rows, labelRow{
			suffix:      strings.TrimPrefix(lab.Name, scope+":"),
			full:        lab.Name,
			description: lab.Description,
			usage:       usage[lab.Name],
		})
	}
	l.nsRows = l.buildNamespaceRows()
	if l.cursor >= len(l.nsRows) && len(l.nsRows) > 0 {
		l.cursor = len(l.nsRows) - 1
	}
	if l.cursor < 0 {
		l.cursor = 0
	}
}

// buildNamespaceRows aggregates the project's labels and tasks into the L0
// table: one row per real namespace (alphabetical), then a "tags" row for
// bare labels, then a "(none)" row for zero-label tasks. Synthetic rows are
// omitted when empty. TASKS counts distinct tasks; LABELS counts labels.
func (l *labelsModel) buildNamespaceRows() []nsRow {
	scope := l.m.projectScope
	labelCount := map[string]int{}
	bareLabelCount := 0
	var nsOrder []string
	seenNS := map[string]bool{}
	for _, r := range l.rows {
		parts := strings.SplitN(r.suffix, ":", 2)
		if len(parts) == 2 {
			ns := parts[0]
			if !seenNS[ns] {
				seenNS[ns] = true
				nsOrder = append(nsOrder, ns)
			}
			labelCount[ns]++
		} else {
			bareLabelCount++
		}
	}
	sort.Strings(nsOrder)

	nsTaskCount := map[string]int{}
	bareTaskCount := 0
	noneTaskCount := 0
	for _, tk := range l.m.store.ListTasks(store.QueryFilters{Project: scope}) {
		if len(tk.Labels) == 0 {
			noneTaskCount++
			continue
		}
		seen := map[string]bool{}
		hasBare := false
		for _, full := range tk.Labels {
			suffix := strings.TrimPrefix(full, scope+":")
			parts := strings.SplitN(suffix, ":", 2)
			if len(parts) == 2 {
				if !seen[parts[0]] {
					seen[parts[0]] = true
					nsTaskCount[parts[0]]++
				}
			} else {
				hasBare = true
			}
		}
		if hasBare {
			bareTaskCount++
		}
	}

	var out []nsRow
	for _, ns := range nsOrder {
		out = append(out, nsRow{key: ns, display: ns, tasks: nsTaskCount[ns], labels: labelCount[ns]})
	}
	if bareLabelCount > 0 {
		out = append(out, nsRow{display: "tags", tasks: bareTaskCount, labels: bareLabelCount, bareTags: true})
	}
	if noneTaskCount > 0 {
		out = append(out, nsRow{display: "(none)", tasks: noneTaskCount, none: true})
	}
	return out
}
```

Add `"atm/internal/store"` to the labels.go import block if not present (grep: `grep -n 'internal/store' internal/tui/labels.go`).

- [ ] **Step 5: Add the state-application methods**

Add to `internal/tui/labels.go`:

```go
// enterTable returns the pane to L0 and clears the Tasks-pane focus so the
// Tasks pane shows all tasks. cursor is preserved so Esc lands where the user
// drilled from.
func (l *labelsModel) enterTable() {
	l.level = lLevelTable
	l.ns = ""
	l.bareTags = false
	l.m.tasks.setFocus(taskFocus{mode: focusOff}, "")
}

// enterChart drills into a namespace row's chart and focuses the Tasks pane on
// tasks carrying that namespace. cursor is reset to the top of the chart.
func (l *labelsModel) enterChart(r nsRow) {
	l.level = lLevelChart
	l.ns = r.key
	l.bareTags = r.bareTags
	l.cursor = 0
	l.offset = 0
	if r.bareTags {
		l.m.tasks.setFocus(taskFocus{mode: focusPresent, bareTags: true}, "")
		return
	}
	l.m.tasks.setFocus(taskFocus{mode: focusPresent, ns: r.key}, facetToken(l.m.projectScope, r.key))
}

// enterNoneLeaf filters the Tasks pane to zero-label tasks. It is a leaf: the
// Labels pane shows a minimal detail and Esc returns to the table.
func (l *labelsModel) enterNoneLeaf() {
	l.level = lLevelDetail
	l.detail = labelDetailState{leaf: "none"}
	l.m.tasks.setFocus(taskFocus{mode: focusUnlabeled}, "")
}

// reset returns the pane to L0 and clears Tasks focus. Called on project switch
// so no stale filter survives.
func (l *labelsModel) reset() {
	l.level = lLevelTable
	l.ns = ""
	l.bareTags = false
	l.cursor = 0
	l.offset = 0
	l.detail = labelDetailState{}
}
```

- [ ] **Step 6: Rewrite `handleKey` for the level state machine (table + basic chart + leaf)**

Replace `handleKey`, `handleListKey`, `handleDetailKey` (231-336) with:

```go
func (l *labelsModel) handleKey(k tea.KeyMsg) tea.Cmd {
	switch l.level {
	case lLevelTable:
		return l.handleTableKey(k)
	case lLevelChart:
		return l.handleChartKey(k)
	case lLevelDetail:
		return l.handleDetailKey(k)
	}
	return nil
}

func (l *labelsModel) handleTableKey(k tea.KeyMsg) tea.Cmd {
	switch k.String() {
	case "j", "down":
		if l.cursor < len(l.nsRows)-1 {
			l.cursor++
		}
	case "k", "up":
		if l.cursor > 0 {
			l.cursor--
		}
	case "g":
		l.cursor = 0
	case "]":
		l.cursor += l.pageSize
		if l.cursor > len(l.nsRows)-1 {
			l.cursor = len(l.nsRows) - 1
		}
		if l.cursor < 0 {
			l.cursor = 0
		}
	case "[":
		l.cursor -= l.pageSize
		if l.cursor < 0 {
			l.cursor = 0
		}
	case "a":
		if l.m.projectScope == "" {
			return nil
		}
		l.m.openLabelAddForm(l.m.projectScope)
	case "S":
		if l.m.projectScope == "" {
			return nil
		}
		return l.seedDefaults()
	case "enter":
		if l.cursor < 0 || l.cursor >= len(l.nsRows) {
			return nil
		}
		r := l.nsRows[l.cursor]
		if r.none {
			l.enterNoneLeaf()
			return nil
		}
		l.enterChart(r)
	}
	return nil
}

// handleChartKey is expanded in Task 4 (cursor over label rows, Enter -> detail,
// (unset) row). For now it only handles Esc back to the table.
func (l *labelsModel) handleChartKey(k tea.KeyMsg) tea.Cmd {
	switch k.String() {
	case "esc":
		l.enterTable()
	}
	return nil
}

func (l *labelsModel) handleDetailKey(k tea.KeyMsg) tea.Cmd {
	switch k.String() {
	case "d":
		if l.detail.leaf == "" {
			l.m.openLabelDescribeFormFor(l.detail.row.suffix, l.detail.row.description)
		}
	case "l":
		if l.detail.leaf == "" {
			l.m.openLabelRemoveForm(l.m.projectScope)
		}
	case "esc":
		// A real label detail and the (unset) leaf sit above the chart; the
		// (none) leaf sits above the table.
		if l.detail.leaf == "none" {
			l.enterTable()
			return nil
		}
		l.reenterChart()
	}
	return nil
}
```

Add a helper that re-enters the chart for the currently active namespace (used by Esc from L2; fully exercised in Task 4):

```go
// reenterChart re-applies the L1 chart state for the active namespace. Used by
// Esc from a label detail or the (unset) leaf.
func (l *labelsModel) reenterChart() {
	r := nsRow{key: l.ns, bareTags: l.bareTags, display: l.ns}
	if l.bareTags {
		r.display = "tags"
	}
	l.enterChart(r)
}
```

Delete `toggleNamespaceFacet`, `toggleLabelFacet`, `activeChartNS`, and `selected` (338-391) — they are replaced by the state-application methods.

- [ ] **Step 7: Rewrite `View`, table render, basic chart, detail, and status hint**

Replace `View`, `renderList`, `renderChart`, `renderDetail`, `statusHint` (403-579) with:

```go
func (l *labelsModel) View() string {
	if l.m.projectScope == "" {
		lines := []string{
			l.m.styles.EmptyHead.Render("no project selected"),
			"",
			l.m.styles.EmptyText.Render(fmt.Sprintf("press %s in the Projects pane to scope this view", l.m.styles.EmptyKey.Render("[s]"))),
		}
		return padToHeight(centerLinesBoth(lines, l.width, l.contentHeight), l.contentHeight)
	}
	switch l.level {
	case lLevelChart:
		return l.renderChart()
	case lLevelDetail:
		return l.renderDetail()
	default:
		return l.renderTable()
	}
}

func (l *labelsModel) renderTable() string {
	if len(l.nsRows) == 0 {
		return padToHeight("no labels", l.contentHeight)
	}
	var b strings.Builder
	header := fmt.Sprintf(" %-20s %8s %8s", "NAMESPACE", "TASKS", "LABELS")
	b.WriteString(dashboardLine(l.width, l.m.styles.HeaderLabel.Render(header)))
	b.WriteString("\n")

	var lines []string
	for i, r := range l.nsRows {
		tasks := fmt.Sprintf("%d", r.tasks)
		labels := fmt.Sprintf("%d", r.labels)
		if r.none {
			labels = "-"
		}
		line := fmt.Sprintf(" %-20s %8s %8s", r.display, tasks, labels)
		if i == l.cursor {
			line = " " + l.m.styles.RowCursor.Render(strings.TrimPrefix(line, " "))
		}
		lines = append(lines, dashboardLine(l.width, line))
	}
	start, end := windowLines(len(lines), l.cursor, l.pageSize)
	for i := start; i < end; i++ {
		b.WriteString(lines[i])
		b.WriteString("\n")
	}
	return padToHeight(b.String(), l.contentHeight)
}

// renderChart is the basic (non-cursor) chart; Task 4 replaces it with a
// cursor-navigable chart carrying an (unset) row.
func (l *labelsModel) renderChart() string {
	var b strings.Builder
	title := l.ns
	if l.bareTags {
		title = "tags"
	}
	b.WriteString(dashboardLine(l.width, fmt.Sprintf("chart: %s", title)))
	b.WriteString("\n")
	for _, r := range l.rows {
		if l.labelInActiveNamespace(r) {
			b.WriteString(dashboardLine(l.width, fmt.Sprintf(" %-30s %5d", r.full, r.usage)))
			b.WriteString("\n")
		}
	}
	b.WriteString("\n")
	b.WriteString(dashboardLine(l.width, l.m.styles.Muted.Render("[Esc] back")))
	return padToHeight(b.String(), l.contentHeight)
}

// labelInActiveNamespace reports whether label row r belongs to the active
// chart namespace (real namespace prefix, or a bare label when bareTags).
func (l *labelsModel) labelInActiveNamespace(r labelRow) bool {
	if l.bareTags {
		return !strings.Contains(r.suffix, ":")
	}
	return strings.HasPrefix(r.suffix, l.ns+":")
}

func (l *labelsModel) renderDetail() string {
	var b strings.Builder
	switch l.detail.leaf {
	case "none":
		b.WriteString(dashboardLine(l.width, "tasks with no labels"))
		b.WriteString("\n")
		b.WriteString(dashboardLine(l.width, l.m.styles.Muted.Render("[Esc] back to namespaces")))
		return padToHeight(b.String(), l.contentHeight)
	case "unset":
		ns := l.ns
		if l.bareTags {
			ns = "bare tag"
		}
		b.WriteString(dashboardLine(l.width, fmt.Sprintf("tasks with no %s", ns)))
		b.WriteString("\n")
		b.WriteString(dashboardLine(l.width, l.m.styles.Muted.Render("[Esc] back to chart")))
		return padToHeight(b.String(), l.contentHeight)
	}
	r := l.detail.row
	fmt.Fprintf(&b, "Label %s\n", r.full)
	b.WriteString(sepLine("─", 78, l.width, 2))
	b.WriteString("\n")
	b.WriteString(sectionCaption(l.m.styles, l.width, "FACTS"))
	b.WriteString("\n")
	fmt.Fprintf(&b, "%s\n", dashboardLine(l.width, fmt.Sprintf("name        %s", r.full)))
	fmt.Fprintf(&b, "%s\n", dashboardLine(l.width, fmt.Sprintf("usage       %d %s", r.usage, pluralUses(r.usage))))
	desc := r.description
	if desc == "" {
		desc = l.m.styles.Warning.Render("needs description")
	}
	fmt.Fprintf(&b, "%s\n", dashboardLine(l.width, fmt.Sprintf("description %s", desc)))
	return padToHeight(b.String(), l.contentHeight)
}

func (l *labelsModel) statusHint() string {
	if l.m.projectScope == "" {
		return "[?]keys"
	}
	switch l.level {
	case lLevelChart:
		return "[Enter]inspect [d]esc [l]remove [Esc]back"
	case lLevelDetail:
		if l.detail.leaf != "" {
			return "[Esc]back"
		}
		return "[d]esc [l]remove [Esc]back"
	default:
		return "[Enter]open [a]dd [S]eed [?]keys"
	}
}
```

- [ ] **Step 8: Remove the Labels Esc interception in app.go**

Delete the two Labels branches in `internal/tui/app.go` (615-622):

```go
		if m.focused == paneLabels && m.labels.view == lViewDetail {
			m.labels.view = lViewList
			return nil
		}
		if m.focused == paneLabels && m.labels.chartNS != "" {
			m.labels.chartNS = ""
			return nil
		}
```

They referenced removed fields (`view`, `chartNS`). Esc now falls through to `m.labels.handleKey(k)` which owns the level ladder.

- [ ] **Step 9: Reset Labels/Tasks state on project switch**

In `internal/tui/projects.go`, in the `s` handler (228-247), after `p.m.projectScope = r.code` and before the refresh calls, add:

```go
			p.m.labels.reset()
			p.m.tasks.focus = taskFocus{}
			p.m.tasks.filter = ""
```

(The subsequent `p.m.tasks.refresh()` / `p.m.labels.refresh()` then rebuild against the cleared state.)

- [ ] **Step 10: Run tests**

Run: `go test ./internal/tui/ -run 'TestLabelsL0' -v`
Expected: PASS

Run: `go test ./internal/tui/`
Expected: PASS. Fix any remaining compile references to deleted identifiers (`chartNS`, `lViewDetail`, `activeChartNS`, `toggleNamespaceFacet`, `selected`, `rebuildEntries`, `entries`) — grep: `grep -rn "chartNS\|lViewDetail\|activeChartNS\|toggleNamespaceFacet\|rebuildEntries\|\.entries\b\|\.selected()" internal/tui/`. Any surviving test referencing them was listed for deletion in Step 1; delete it.

Run: `make verify`
Expected: PASS

- [ ] **Step 11: Commit**

```bash
git add internal/tui/labels.go internal/tui/app.go internal/tui/projects.go internal/tui/labels_test.go
git commit -m "feat(tui): labels namespace table + level state machine"
```

---

## Task 4: Labels chart cursor, (unset) row, task-count bars, and label detail (L2)

Makes the L1 chart cursor-navigable with task-count bars and a trailing `(unset)` row, and wires Enter on a label row into the L2 detail (flat exact-label focus) and Enter on `(unset)` into the absent-focus leaf.

**Files:**
- Modify: `internal/tui/labels.go` — `handleChartKey`, `renderChart`, add `chartRows`, `enterDetail`, `enterUnsetLeaf`
- Test: `internal/tui/labels_test.go`

**Interfaces:**
- Consumes: `store.GroupTasks`, `facetToken`, `meterBar`, `tasksModel.setFocus`, the `nsRow`/`labelRow`/`lLevel` types from Task 3.
- Produces:
  - `type chartRow struct { full string; count int; unset bool }`.
  - `func (l *labelsModel) chartRows() []chartRow` — label rows for the active namespace (task counts) plus a trailing `(unset)` row when non-empty.
  - `func (l *labelsModel) enterDetail(r labelRow)` — L2 detail + exact-label flat focus.
  - `func (l *labelsModel) enterUnsetLeaf()` — absent focus for the active namespace.

- [ ] **Step 1: Write failing tests**

Add to `internal/tui/labels_test.go`:

```go
func TestLabelsChartCursorAndUnsetRow(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	mk := func(title string, labels ...string) {
		if _, err := m.store.CreateTask("ATM", title, "", labels, m.actor); err != nil {
			t.Fatal(err)
		}
	}
	mk("a", "ATM:status:open")
	mk("b", "ATM:status:open")
	mk("c", "ATM:status:done")
	mk("d", "ATM:priority:high") // no status -> unset

	update(t, m, "s")
	update(t, m, "3")
	cursorToNamespaceRow(t, m, "status")
	update(t, m, "enter") // into chart

	rows := m.labels.chartRows()
	// open(2), done(1), unset(1) in this fixture.
	var openCount, unsetCount int
	sawUnset := false
	for _, r := range rows {
		if r.unset {
			sawUnset = true
			unsetCount = r.count
		}
		if r.full == "ATM:status:open" {
			openCount = r.count
		}
	}
	if openCount != 2 {
		t.Errorf("open count = %d want 2", openCount)
	}
	if !sawUnset || unsetCount != 1 {
		t.Errorf("unset row missing or wrong: saw=%v count=%d want 1", sawUnset, unsetCount)
	}
	v := m.labels.View()
	mustContain(t, v, "(unset)")
	mustContain(t, v, "█")
}

func TestLabelsChartEnterRowOpensDetailAndFocusesExactLabel(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	if _, err := m.store.CreateTask("ATM", "a", "", []string{"ATM:status:open"}, m.actor); err != nil {
		t.Fatal(err)
	}
	update(t, m, "s")
	update(t, m, "3")
	cursorToNamespaceRow(t, m, "status")
	update(t, m, "enter") // chart
	cursorToChartLabel(t, m, "ATM:status:open")
	update(t, m, "enter") // detail

	if m.labels.level != lLevelDetail {
		t.Fatalf("level = %v want detail", m.labels.level)
	}
	if m.tasks.filter != "ATM:status:open" || m.tasks.focus.mode != focusOff {
		t.Fatalf("tasks focus/filter = %+v %q want off/exact", m.tasks.focus, m.tasks.filter)
	}
	mustContain(t, m.labels.View(), "Label ATM:status:open")

	// Esc returns to the chart and re-applies present focus.
	update(t, m, "esc")
	if m.labels.level != lLevelChart {
		t.Fatalf("level = %v want chart after esc", m.labels.level)
	}
	if m.tasks.filter != "ATM:status:*" || m.tasks.focus.mode != focusPresent {
		t.Fatalf("chart focus not restored: %+v %q", m.tasks.focus, m.tasks.filter)
	}
}

func TestLabelsChartEnterUnsetFiltersAbsent(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	mk := func(title string, labels ...string) {
		if _, err := m.store.CreateTask("ATM", title, "", labels, m.actor); err != nil {
			t.Fatal(err)
		}
	}
	mk("a", "ATM:status:open")
	mk("b", "ATM:priority:high") // no status

	update(t, m, "s")
	update(t, m, "3")
	cursorToNamespaceRow(t, m, "status")
	update(t, m, "enter") // chart
	cursorToChartUnset(t, m)
	update(t, m, "enter") // unset leaf

	if m.tasks.focus.mode != focusAbsent || m.tasks.focus.ns != "status" {
		t.Fatalf("focus = %+v want absent/status", m.tasks.focus)
	}
	update(t, m, "esc")
	if m.labels.level != lLevelChart || m.tasks.focus.mode != focusPresent {
		t.Fatalf("esc from unset leaf did not restore chart present focus: %v %+v", m.labels.level, m.tasks.focus)
	}
}
```

Add helpers to `internal/tui/labels_test.go`:

```go
func cursorToChartLabel(t *testing.T, m *Model, full string) {
	t.Helper()
	for i, r := range m.labels.chartRows() {
		if r.full == full {
			m.labels.cursor = i
			return
		}
	}
	t.Fatalf("chart label %q not found", full)
}

func cursorToChartUnset(t *testing.T, m *Model) {
	t.Helper()
	for i, r := range m.labels.chartRows() {
		if r.unset {
			m.labels.cursor = i
			return
		}
	}
	t.Fatalf("chart (unset) row not found")
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/tui/ -run 'TestLabelsChart' -v`
Expected: FAIL to compile (`chartRows`, `chartRow`, etc. undefined).

- [ ] **Step 3: Add `chartRow`/`chartRows` and the entry methods**

In `internal/tui/labels.go`, add:

```go
type chartRow struct {
	full  string
	count int
	unset bool
}

// chartRows returns the active namespace's per-label task counts plus a
// trailing (unset) row for tasks lacking the namespace. Real namespaces use
// GroupTasks; the tags pseudo-namespace is counted locally since no single
// wildcard selects bare labels.
func (l *labelsModel) chartRows() []chartRow {
	scope := l.m.projectScope
	var rows []chartRow
	unset := 0
	if l.bareTags {
		bareCount := map[string]int{}
		for _, tk := range l.m.store.ListTasks(store.QueryFilters{Project: scope}) {
			if taskHasBareTag(scope, tk) {
				for _, full := range tk.Labels {
					if !strings.Contains(strings.TrimPrefix(full, scope+":"), ":") {
						bareCount[full]++
					}
				}
			} else {
				unset++
			}
		}
		for _, r := range l.rows {
			if !strings.Contains(r.suffix, ":") {
				rows = append(rows, chartRow{full: r.full, count: bareCount[r.full]})
			}
		}
	} else {
		groups, others := l.m.store.GroupTasks(store.QueryFilters{Project: scope, Labels: []string{facetToken(scope, l.ns)}})
		counts := map[string]int{}
		for _, g := range groups {
			counts[g.Label] = len(g.Tasks)
		}
		for _, r := range l.rows {
			if strings.HasPrefix(r.suffix, l.ns+":") {
				rows = append(rows, chartRow{full: r.full, count: counts[r.full]})
			}
		}
		unset = len(others)
	}
	if unset > 0 {
		rows = append(rows, chartRow{full: "(unset)", count: unset, unset: true})
	}
	return rows
}

// enterDetail opens a label's detail (L2) and focuses the Tasks pane on that
// exact label as a flat list.
func (l *labelsModel) enterDetail(r labelRow) {
	l.level = lLevelDetail
	l.detail = labelDetailState{row: r}
	l.m.tasks.setFocus(taskFocus{mode: focusOff}, r.full)
}

// enterUnsetLeaf focuses the Tasks pane on tasks lacking the active namespace.
func (l *labelsModel) enterUnsetLeaf() {
	l.level = lLevelDetail
	l.detail = labelDetailState{leaf: "unset"}
	if l.bareTags {
		l.m.tasks.setFocus(taskFocus{mode: focusAbsent, bareTags: true}, "")
		return
	}
	l.m.tasks.setFocus(taskFocus{mode: focusAbsent, ns: l.ns}, facetToken(l.m.projectScope, l.ns))
}
```

- [ ] **Step 4: Rewrite `handleChartKey` for cursor navigation + Enter**

Replace `handleChartKey` (added in Task 3) with:

```go
func (l *labelsModel) handleChartKey(k tea.KeyMsg) tea.Cmd {
	rows := l.chartRows()
	switch k.String() {
	case "j", "down":
		if l.cursor < len(rows)-1 {
			l.cursor++
		}
	case "k", "up":
		if l.cursor > 0 {
			l.cursor--
		}
	case "g":
		l.cursor = 0
	case "]":
		l.cursor += l.pageSize
		if l.cursor > len(rows)-1 {
			l.cursor = len(rows) - 1
		}
		if l.cursor < 0 {
			l.cursor = 0
		}
	case "[":
		l.cursor -= l.pageSize
		if l.cursor < 0 {
			l.cursor = 0
		}
	case "d":
		if r, ok := l.chartLabelRow(); ok {
			l.m.openLabelDescribeFormFor(r.suffix, r.description)
		}
	case "l":
		if _, ok := l.chartLabelRow(); ok {
			l.m.openLabelRemoveForm(l.m.projectScope)
		}
	case "enter":
		if l.cursor < 0 || l.cursor >= len(rows) {
			return nil
		}
		if rows[l.cursor].unset {
			l.enterUnsetLeaf()
			return nil
		}
		if r, ok := l.chartLabelRow(); ok {
			l.enterDetail(r)
		}
	case "esc":
		l.enterTable()
	}
	return nil
}

// chartLabelRow returns the labelRow under the chart cursor, or ok=false when
// the cursor is on the (unset) row or out of range.
func (l *labelsModel) chartLabelRow() (labelRow, bool) {
	rows := l.chartRows()
	if l.cursor < 0 || l.cursor >= len(rows) || rows[l.cursor].unset {
		return labelRow{}, false
	}
	full := rows[l.cursor].full
	for _, r := range l.rows {
		if r.full == full {
			return r, true
		}
	}
	return labelRow{}, false
}
```

- [ ] **Step 5: Rewrite `renderChart` with cursor + bars + (unset)**

Replace `renderChart` (from Task 3) with:

```go
func (l *labelsModel) renderChart() string {
	title := l.ns
	if l.bareTags {
		title = "tags"
	}
	rows := l.chartRows()
	total := 0
	for _, r := range rows {
		total += r.count
	}

	var b strings.Builder
	b.WriteString(dashboardLine(l.width, fmt.Sprintf("%s  ·  %d tasks", title, total)))
	b.WriteString("\n")

	nameW := 0
	for _, r := range rows {
		if w := len(r.full); w > nameW {
			nameW = w
		}
	}
	if nameW < 8 {
		nameW = 8
	}
	meterW := l.width - nameW - 14
	if meterW < 10 {
		meterW = 10
	}

	var lines []string
	for i, r := range rows {
		percent := 0
		if total > 0 {
			percent = (r.count*100 + total/2) / total
		}
		line := fmt.Sprintf(" %-*s %s %4d", nameW, r.full, meterBar(percent, meterW), r.count)
		if i == l.cursor {
			line = " " + l.m.styles.RowCursor.Render(strings.TrimPrefix(line, " "))
		}
		lines = append(lines, dashboardLine(l.width, line))
	}
	start, end := windowLines(len(lines), l.cursor, l.pageSize)
	for i := start; i < end; i++ {
		b.WriteString(lines[i])
		b.WriteString("\n")
	}
	b.WriteString(dashboardLine(l.width, l.m.styles.Muted.Render("[Enter]inspect  [Esc]back")))
	return padToHeight(b.String(), l.contentHeight)
}
```

Delete the now-unused `labelInActiveNamespace` helper from Task 3 if nothing else references it (grep: `grep -n labelInActiveNamespace internal/tui/labels.go`).

- [ ] **Step 6: Clamp the chart cursor on entry**

In `enterChart` (Task 3), the cursor is reset to 0, which is always valid. No extra clamp needed. Verify `handleChartKey` guards `len(rows)` (it does).

- [ ] **Step 7: Run tests**

Run: `go test ./internal/tui/ -run 'TestLabelsChart' -v`
Expected: PASS

Run: `go test ./internal/tui/`
Expected: PASS

Run: `make verify`
Expected: PASS

- [ ] **Step 8: Commit**

```bash
git add internal/tui/labels.go internal/tui/labels_test.go
git commit -m "feat(tui): cursor chart with unset row and label detail"
```

---

## Task 5: Keymap + help documentation

Update the declarative keymap and help prose to match the new bindings, and confirm no stale key documentation remains.

**Files:**
- Modify: `internal/tui/keymap.go:24-55` (`keymapRows`)
- Modify: `internal/tui/help.go` (any prose describing the removed `/`, `c`, `i` keys or the old Labels chart toggle)
- Test: `internal/tui/help_test.go` if one exists (grep: `ls internal/tui/help_test.go`)

**Interfaces:**
- Produces: updated `keymapRows` reflecting: Labels `Enter` = "drill into namespace / open label detail"; remove `i` row; Tasks column no longer lists `/` or `c`; `Esc` Labels column = "back up a level".

- [ ] **Step 1: Update the keymap rows**

In `internal/tui/keymap.go`, edit `keymapRows`:

- `{"Enter", ...}` row: Tasks stays "open detail / toggle group"; Labels becomes `"drill into namespace / open label detail"`.
- Delete the `{"i", "-", "-", "open label detail", "-"}` row.
- `{"Esc", ...}` row: Tasks becomes `"back"` (no "cancel filter"); Labels becomes `"back up a level"`.
- Delete the `{"/", "-", "edit filter", "-", "-"}` row.
- Delete the `{"c", "-", "clear filter", "-", "-"}` row.
- `{"a", ...}` row: Labels stays "add label" (L0 only, but the table is where `a` lives).
- `{"d", ...}` and `{"l", ...}` rows: Labels stays "describe label" / "remove label" (now active in chart + detail).

- [ ] **Step 2: Update help prose**

Grep and fix: `grep -rn "edit filter\|clear filter\|\[/\]\|\[c\]\|toggle namespace facet\|toggle exact label\|inspect" internal/tui/help.go`. Reword any sentence describing the old Tasks filter or the old Labels chart-toggle model to the new drill-down (namespace table -> chart -> detail; Tasks pane is selection-only, focus driven by Labels).

- [ ] **Step 3: Run tests**

Run: `go test ./internal/tui/`
Expected: PASS (fix any help_test.go string assertions that referenced removed keys).

Run: `make verify`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add internal/tui/keymap.go internal/tui/help.go
git commit -m "docs(tui): update keymap/help for labels drill-down"
```

---

## Final verification

- [ ] Run `make verify` on the branch tip; confirm build + all tests + scripts-test pass.
- [ ] Manual smoke (optional but recommended): launch the TUI (`bin/atm`), scope a project, focus the Labels pane (`3`), confirm: the namespace table renders with TASKS/LABELS columns; Enter on a namespace opens the chart and the Tasks pane groups by that namespace with unlabeled-for-that-namespace tasks hidden; cursor to `(unset)` + Enter shows only tasks lacking the namespace; Enter on a label row shows that label's tasks flat; Esc steps back up clearing focus at each level; `(none)` filters to zero-label tasks; the Tasks pane header shows `FOCUS:` and no longer responds to `/` or `c`.
