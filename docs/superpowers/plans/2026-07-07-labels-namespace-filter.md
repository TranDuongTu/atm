# Labels Pane Namespace Filtering Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let the Labels pane drive Tasks filtering — selecting a namespace header facets the Tasks pane by `<CODE>:<ns>:*` and turns the Labels pane into a per-label usage bar chart; selecting a label row still opens detail.

**Architecture:** TUI-only change in `internal/tui`. `labelsModel` gains a unified entry list (namespace headers + label rows + a `tags:` header) so headers become real cursor stops; `Enter` on a namespace header toggles the wildcard facet in `Model.tasks.filter` and toggles a `chartNS` view state. A new one-key clear resets the Tasks filter. No store or CLI changes.

**Tech Stack:** Go, Bubble Tea TUI, existing `internal/tui` helpers (`meterBar`, `dashboardLine`, `windowLines`, styles).

**Spec:** `docs/superpowers/specs/2026-07-07-labels-namespace-filter-design.md`

## Global Constraints

- No changes to `internal/store` or `internal/cli`. Presentation and filter-string composition only.
- No changes to Tasks filter syntax or wildcard/facet semantics (`parseFilter`, `buildNestedGroups`, `store.QueryFilters` untouched).
- Facet tokens in the Tasks filter are full label names: `<projectScope>:<ns>:*` (e.g. `ATM:status:*`).
- Namespace names in the Labels pane are the suffix's first segment (e.g. suffix `status:open` → ns `status`).
- Verification gate: `make verify` (runs build + test). Tests assert stable text/model state — no full-screen ANSI snapshots.
- Chart view is a static render: while it is showing, only `Esc` is active; `j/k/g/[/]` and `Enter` are inert.

---

### Task 1: Filter token helpers + Tasks pane clear-filter key

Adds pure helpers for composing the space-separated Tasks filter string, and a `c` key in the Tasks list view that clears the filter in one press. Independent of the Labels changes; the helpers are consumed by Task 3.

**Files:**
- Modify: `internal/tui/tasks.go` (add helpers near `parseFilter` ~line 199; add `case "c"` in `handleListKey` ~line 372)
- Test: `internal/tui/tasks_test.go`

**Interfaces:**
- Produces:
  - `func facetToken(scope, ns string) string` — returns `scope + ":" + ns + ":*"`.
  - `func filterHasToken(filter, token string) bool` — true if `token` is one of the space-separated fields of `filter`.
  - `func filterAddToken(filter, token string) string` — returns `filter` with `token` appended (single space separator) if not already present; otherwise unchanged.
  - `func filterRemoveToken(filter, token string) string` — returns `filter` with every occurrence of `token` removed, remaining fields re-joined by single spaces.

- [ ] **Step 1: Write the failing test**

Add to `internal/tui/tasks_test.go`:

```go
func TestFilterTokenHelpers(t *testing.T) {
	if got := facetToken("ATM", "status"); got != "ATM:status:*" {
		t.Fatalf("facetToken = %q want ATM:status:*", got)
	}
	if !filterHasToken("ATM:status:* ATM:type:*", "ATM:type:*") {
		t.Fatalf("filterHasToken should find ATM:type:*")
	}
	if filterHasToken("ATM:status:*", "ATM:type:*") {
		t.Fatalf("filterHasToken should not find absent token")
	}
	if got := filterAddToken("ATM:status:*", "ATM:type:*"); got != "ATM:status:* ATM:type:*" {
		t.Fatalf("filterAddToken = %q want two tokens", got)
	}
	if got := filterAddToken("ATM:status:*", "ATM:status:*"); got != "ATM:status:*" {
		t.Fatalf("filterAddToken should not duplicate, got %q", got)
	}
	if got := filterAddToken("", "ATM:status:*"); got != "ATM:status:*" {
		t.Fatalf("filterAddToken onto empty = %q want ATM:status:*", got)
	}
	if got := filterRemoveToken("ATM:status:* ATM:type:*", "ATM:status:*"); got != "ATM:type:*" {
		t.Fatalf("filterRemoveToken = %q want ATM:type:*", got)
	}
	if got := filterRemoveToken("ATM:status:*", "ATM:status:*"); got != "" {
		t.Fatalf("filterRemoveToken last token = %q want empty", got)
	}
}

func TestTasksClearFilterKey(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	update(t, m, "s") // select ATM
	update(t, m, "2") // Tasks pane
	m.tasks.filter = "ATM:status:*"
	m.tasks.refresh()
	update(t, m, "c") // clear filter
	if m.tasks.filter != "" {
		t.Fatalf("filter = %q want empty after clear", m.tasks.filter)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/tui/ -run 'TestFilterTokenHelpers|TestTasksClearFilterKey' -v`
Expected: FAIL — `undefined: facetToken` (and the `c` key is currently a no-op).

- [ ] **Step 3: Add the helpers**

In `internal/tui/tasks.go`, add just below `parseFilter` (after the `isWildcardTUI` function block, ~line 218):

```go
// facetToken returns the full wildcard label used to facet the Tasks pane by
// a namespace, e.g. facetToken("ATM","status") == "ATM:status:*".
func facetToken(scope, ns string) string { return scope + ":" + ns + ":*" }

// filterHasToken reports whether token is one of the space-separated fields of
// filter.
func filterHasToken(filter, token string) bool {
	for _, f := range strings.Fields(filter) {
		if f == token {
			return true
		}
	}
	return false
}

// filterAddToken appends token to filter (single-space separated) unless it is
// already present.
func filterAddToken(filter, token string) string {
	if filterHasToken(filter, token) {
		return filter
	}
	if strings.TrimSpace(filter) == "" {
		return token
	}
	return filter + " " + token
}

// filterRemoveToken removes every occurrence of token from filter and rejoins
// the remaining fields with single spaces.
func filterRemoveToken(filter, token string) string {
	var kept []string
	for _, f := range strings.Fields(filter) {
		if f != token {
			kept = append(kept, f)
		}
	}
	return strings.Join(kept, " ")
}
```

- [ ] **Step 4: Add the `c` key to the Tasks list handler**

In `internal/tui/tasks.go`, in `handleListKey` add a new case (place it after the `case "/":` block, before `case "s":` ~line 393):

```go
	case "c":
		if t.filter == "" {
			return nil
		}
		t.filter = ""
		t.cursor = 0
		t.refresh()
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/tui/ -run 'TestFilterTokenHelpers|TestTasksClearFilterKey' -v`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/tui/tasks.go internal/tui/tasks_test.go
git commit -m "Add filter token helpers and Tasks pane clear-filter key (ATM-0041)"
```

---

### Task 2: Labels pane unified entry list

Replaces `labelsModel`'s row-only cursor with an ordered `entries` list (namespace headers + label rows + `tags:` header) so namespace headers become cursor-addressable. Rewrites `renderList` to iterate the entries. Updates `selected()` and the two existing cursor tests whose expected math changes.

**Files:**
- Modify: `internal/tui/labels.go` (add `labelEntry` type; add `entries` field to `labelsModel` ~line 100; build entries in `refresh()` ~line 149; rewrite `renderList` ~line 278; update `selected()` ~line 251; clamp cursor to `entries`)
- Test: `internal/tui/labels_test.go` (new entries test; update `TestLabelsListScrollsWithCursor` and `TestLabelsBracketKeysPageThroughList`)

**Interfaces:**
- Consumes: nothing from earlier tasks.
- Produces:
  - `type labelEntryKind int` with `entryHeaderNS`, `entryHeaderTags`, `entryRow`.
  - `type labelEntry struct { kind labelEntryKind; ns string; row labelRow }`.
  - `labelsModel.entries []labelEntry` — rebuilt each `refresh()`.
  - `func (l *labelsModel) rebuildEntries()` — populates `l.entries` from `l.rows`.
  - `l.cursor` now indexes `l.entries`.
  - `func (l *labelsModel) selected() (labelRow, bool)` — returns the row under the cursor only when it is an `entryRow`.

- [ ] **Step 1: Write the failing test**

Add to `internal/tui/labels_test.go`:

```go
func TestLabelsEntriesIncludeNamespaceHeaders(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	update(t, m, "s")
	update(t, m, "3")

	if len(m.labels.entries) == 0 {
		t.Fatalf("entries not built")
	}
	// The seeded set has a status: namespace; there must be a header entry for
	// it that precedes its first row entry.
	headerIdx, rowIdx := -1, -1
	for i, e := range m.labels.entries {
		if e.kind == entryHeaderNS && e.ns == "status" && headerIdx == -1 {
			headerIdx = i
		}
		if e.kind == entryRow && strings.HasPrefix(e.row.suffix, "status:") && rowIdx == -1 {
			rowIdx = i
		}
	}
	if headerIdx == -1 {
		t.Fatalf("no status namespace header entry")
	}
	if rowIdx == -1 || rowIdx <= headerIdx {
		t.Fatalf("status row (%d) should follow its header (%d)", rowIdx, headerIdx)
	}
	// entries must contain more items than rows (headers add slots).
	if len(m.labels.entries) <= len(m.labels.rows) {
		t.Fatalf("entries (%d) should exceed rows (%d) due to headers", len(m.labels.entries), len(m.labels.rows))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tui/ -run TestLabelsEntriesIncludeNamespaceHeaders -v`
Expected: FAIL — `m.labels.entries undefined` / `entryHeaderNS undefined`.

- [ ] **Step 3: Add the entry types and struct field**

In `internal/tui/labels.go`, add after the `labelRow` type (~line 124):

```go
type labelEntryKind int

const (
	entryHeaderNS labelEntryKind = iota
	entryHeaderTags
	entryRow
)

// labelEntry is one navigable line in the Labels list: a namespace header, the
// tags header, or a label row. The cursor indexes the entries slice so headers
// are selectable (Enter on a namespace header facets the Tasks pane).
type labelEntry struct {
	kind labelEntryKind
	ns   string   // set for entryHeaderNS
	row  labelRow // set for entryRow
}
```

Add the `entries` field to `labelsModel` (in the struct ~line 100, after `rows []labelRow`):

```go
	entries []labelEntry
```

- [ ] **Step 4: Build entries in refresh() and add rebuildEntries()**

In `internal/tui/labels.go`, at the end of `refresh()` (after the cursor-clamp block ~line 170), replace the trailing clamp so it clamps against entries. Change the tail of `refresh()` from:

```go
	if l.cursor >= len(l.rows) && len(l.rows) > 0 {
		l.cursor = len(l.rows) - 1
	}
	if l.cursor < 0 {
		l.cursor = 0
	}
}
```

to:

```go
	l.rebuildEntries()
	if l.cursor >= len(l.entries) && len(l.entries) > 0 {
		l.cursor = len(l.entries) - 1
	}
	if l.cursor < 0 {
		l.cursor = 0
	}
}

// rebuildEntries flattens l.rows into the navigable entries list: namespace
// headers (alphabetical) each followed by their rows, then a tags header with
// unnamespaced rows. Mirrors the grouping renderList uses.
func (l *labelsModel) rebuildEntries() {
	l.entries = nil
	byNS := map[string][]labelRow{}
	var tags []labelRow
	var nsOrder []string
	seenNS := map[string]bool{}
	for _, r := range l.rows {
		parts := strings.SplitN(r.suffix, ":", 2)
		if len(parts) == 2 {
			if !seenNS[parts[0]] {
				seenNS[parts[0]] = true
				nsOrder = append(nsOrder, parts[0])
			}
			byNS[parts[0]] = append(byNS[parts[0]], r)
		} else {
			tags = append(tags, r)
		}
	}
	sort.Strings(nsOrder)
	for _, ns := range nsOrder {
		l.entries = append(l.entries, labelEntry{kind: entryHeaderNS, ns: ns})
		for _, r := range byNS[ns] {
			l.entries = append(l.entries, labelEntry{kind: entryRow, row: r})
		}
	}
	if len(tags) > 0 {
		l.entries = append(l.entries, labelEntry{kind: entryHeaderTags})
		for _, r := range tags {
			l.entries = append(l.entries, labelEntry{kind: entryRow, row: r})
		}
	}
}
```

- [ ] **Step 5: Run the entries test to verify it passes**

Run: `go test ./internal/tui/ -run TestLabelsEntriesIncludeNamespaceHeaders -v`
Expected: PASS

- [ ] **Step 6: Update selected() to use entries**

In `internal/tui/labels.go`, replace `selected()` (~line 251):

```go
func (l *labelsModel) selected() (labelRow, bool) {
	if l.cursor < 0 || l.cursor >= len(l.entries) {
		return labelRow{}, false
	}
	e := l.entries[l.cursor]
	if e.kind != entryRow {
		return labelRow{}, false
	}
	return e.row, true
}
```

- [ ] **Step 7: Rewrite renderList to iterate entries**

In `internal/tui/labels.go`, replace the entire `renderList` function (~line 278–375, from `func (l *labelsModel) renderList() string {` through its closing brace) with:

```go
func (l *labelsModel) renderList() string {
	if l.m.projectScope == "" {
		lines := []string{
			l.m.styles.EmptyHead.Render("no project selected"),
			"",
			l.m.styles.EmptyText.Render(fmt.Sprintf("press %s in the Projects pane to scope this view", l.m.styles.EmptyKey.Render("[s]"))),
		}
		return padToHeight(centerLinesBoth(lines, l.width, l.contentHeight), l.contentHeight)
	}
	if len(l.rows) == 0 {
		return padToHeight("no labels", l.contentHeight)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n", dashboardLine(l.width, fmt.Sprintf("project: %s   total labels: %d", l.m.projectScope, len(l.rows))))
	b.WriteString("\n")

	// Build one display line per entry; the cursor indexes l.entries directly,
	// so headers are highlightable cursor stops. lineRowOrd tracks the 1-based
	// label-row ordinal for each line (-1 for headers) to render the footer.
	var bodyLines []string
	var lineRowOrd []int
	cursorLine := 0
	rowOrd := 0
	for i, e := range l.entries {
		switch e.kind {
		case entryHeaderNS:
			line := l.m.styles.NamespaceHeader.Render(e.ns + ":")
			if i == l.cursor {
				line = l.m.styles.RowCursor.Render(e.ns + ":")
				cursorLine = len(bodyLines)
			}
			bodyLines = append(bodyLines, dashboardLine(l.width, line))
			lineRowOrd = append(lineRowOrd, -1)
		case entryHeaderTags:
			line := l.m.styles.NamespaceHeader.Render("tags:")
			if i == l.cursor {
				line = l.m.styles.RowCursor.Render("tags:")
				cursorLine = len(bodyLines)
			}
			bodyLines = append(bodyLines, dashboardLine(l.width, line))
			lineRowOrd = append(lineRowOrd, -1)
		case entryRow:
			rowOrd++
			r := e.row
			desc := r.description
			if desc == "" {
				desc = l.m.styles.Warning.Render("needs description")
			}
			line := fmt.Sprintf(" %-30s %5d %-5s  %s", r.full, r.usage, pluralTasks(r.usage), desc)
			if i == l.cursor {
				line = " " + l.m.styles.RowCursor.Render(strings.TrimPrefix(line, " "))
				cursorLine = len(bodyLines)
			} else {
				line = " " + line
			}
			bodyLines = append(bodyLines, dashboardLine(l.width, line))
			lineRowOrd = append(lineRowOrd, rowOrd)
		}
	}

	start, end := windowLines(len(bodyLines), cursorLine, l.pageSize)
	for i := start; i < end; i++ {
		b.WriteString(bodyLines[i])
		b.WriteString("\n")
	}
	firstOrd, lastOrd := -1, -1
	for i := start; i < end; i++ {
		if lineRowOrd[i] < 0 {
			continue
		}
		if firstOrd == -1 {
			firstOrd = lineRowOrd[i]
		}
		lastOrd = lineRowOrd[i]
	}
	if firstOrd == -1 {
		b.WriteString(dashboardLine(l.width, l.m.styles.Muted.Render("showing 0-0 of "+fmt.Sprint(len(l.rows)))))
	} else {
		b.WriteString(dashboardLine(l.width, l.m.styles.Muted.Render(fmt.Sprintf("showing %d-%d of %d", firstOrd, lastOrd, len(l.rows)))))
	}
	return padToHeight(b.String(), l.contentHeight)
}
```

- [ ] **Step 8: Update the two existing cursor tests**

In `internal/tui/labels_test.go`, in `TestLabelsListScrollsWithCursor`, the cursor now indexes entries. Replace:

```go
	m.labels.cursor = len(rows) - 1
```

with:

```go
	m.labels.cursor = len(m.labels.entries) - 1
```

`TestLabelsBracketKeysPageThroughList` asserts only relative cursor movement (`]` increases, `[` decreases), which still holds with entries — no change needed there unless it fails; if it fails because the seeded list fits one page after entry changes, shrink the pane by changing `m.SetSize(200, 20)` to `m.SetSize(200, 12)` in that test so paging is still required.

- [ ] **Step 9: Run the labels tests to verify they pass**

Run: `go test ./internal/tui/ -run 'TestLabels' -v`
Expected: PASS (all `TestLabels*`, including the updated scroll/bracket tests).

- [ ] **Step 10: Run the full package to catch regressions**

Run: `go test ./internal/tui/`
Expected: PASS

- [ ] **Step 11: Commit**

```bash
git add internal/tui/labels.go internal/tui/labels_test.go
git commit -m "Add unified entry list to Labels pane so namespace headers are selectable (ATM-0041)"
```

---

### Task 3: Namespace facet toggle + chart state

Wires `Enter` on a namespace header to toggle the `<CODE>:<ns>:*` facet in the Tasks filter and toggle the Labels chart state. Adds `chartNS` state, an `activeChartNS()` self-heal accessor, and `Esc` to close the chart. Chart rendering itself is Task 4; here `Enter`/`Esc` only manage state and the Tasks filter.

**Files:**
- Modify: `internal/tui/labels.go` (add `chartNS` field; add `activeChartNS()`; extend `handleListKey` for `enter`/`esc`)
- Test: `internal/tui/labels_test.go`

**Interfaces:**
- Consumes: `facetToken`, `filterHasToken`, `filterAddToken`, `filterRemoveToken` (Task 1); `labelEntry`/`entries`/`entryHeaderNS`/`entryHeaderTags`/`entryRow` (Task 2).
- Produces:
  - `labelsModel.chartNS string` — active chart namespace ("" = list view).
  - `func (l *labelsModel) activeChartNS() string` — returns `chartNS` if its facet token is still present in `l.m.tasks.filter`, else clears `chartNS` and returns "".
  - `func (l *labelsModel) toggleNamespaceFacet(ns string)` — adds/removes the facet token and sets/clears `chartNS`, then refreshes the Tasks pane.

- [ ] **Step 1: Write the failing tests**

Add to `internal/tui/labels_test.go`:

```go
// cursorToNamespaceHeader moves the Labels cursor onto the first header entry
// for ns and returns its index.
func cursorToNamespaceHeader(t *testing.T, m *Model, ns string) {
	t.Helper()
	for i, e := range m.labels.entries {
		if e.kind == entryHeaderNS && e.ns == ns {
			m.labels.cursor = i
			return
		}
	}
	t.Fatalf("no namespace header entry for %q", ns)
}

func TestLabelsEnterOnNamespaceTogglesFacetAndChart(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	update(t, m, "s")
	update(t, m, "3")

	cursorToNamespaceHeader(t, m, "status")
	update(t, m, "enter")
	if m.tasks.filter != "ATM:status:*" {
		t.Fatalf("filter = %q want ATM:status:*", m.tasks.filter)
	}
	if m.labels.chartNS != "status" {
		t.Fatalf("chartNS = %q want status", m.labels.chartNS)
	}

	// Enter again on the same namespace toggles it off.
	cursorToNamespaceHeader(t, m, "status")
	update(t, m, "enter")
	if m.tasks.filter != "" {
		t.Fatalf("filter = %q want empty after toggle off", m.tasks.filter)
	}
	if m.labels.chartNS != "" {
		t.Fatalf("chartNS = %q want empty after toggle off", m.labels.chartNS)
	}
}

func TestLabelsEnterOnTagsHeaderIsNoop(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	// Add an unnamespaced tag so a tags header exists.
	if err := m.store.LabelAdd("ATM:urgent", "", m.actor); err != nil {
		t.Fatal(err)
	}
	update(t, m, "s")
	update(t, m, "3")

	found := false
	for i, e := range m.labels.entries {
		if e.kind == entryHeaderTags {
			m.labels.cursor = i
			found = true
		}
	}
	if !found {
		t.Fatalf("no tags header entry")
	}
	update(t, m, "enter")
	if m.tasks.filter != "" {
		t.Fatalf("filter = %q want empty (tags header is a no-op)", m.tasks.filter)
	}
	if m.labels.chartNS != "" {
		t.Fatalf("chartNS = %q want empty", m.labels.chartNS)
	}
}

func TestLabelsChartSelfHealsWhenFilterEditedAway(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	update(t, m, "s")
	update(t, m, "3")
	cursorToNamespaceHeader(t, m, "status")
	update(t, m, "enter")
	if m.labels.chartNS != "status" {
		t.Fatalf("precondition: chartNS should be status")
	}
	// Simulate the user clearing the Tasks filter out from under the chart.
	m.tasks.filter = ""
	if got := m.labels.activeChartNS(); got != "" {
		t.Fatalf("activeChartNS = %q want empty after filter cleared", got)
	}
	if m.labels.chartNS != "" {
		t.Fatalf("chartNS should self-heal to empty")
	}
}

func TestLabelsEscClosesChartWithoutClearingFilter(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	update(t, m, "s")
	update(t, m, "3")
	cursorToNamespaceHeader(t, m, "status")
	update(t, m, "enter")
	update(t, m, "esc")
	if m.labels.chartNS != "" {
		t.Fatalf("chartNS = %q want empty after esc", m.labels.chartNS)
	}
	if m.tasks.filter != "ATM:status:*" {
		t.Fatalf("filter = %q want ATM:status:* preserved after esc", m.tasks.filter)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/tui/ -run 'TestLabelsEnterOnNamespace|TestLabelsEnterOnTags|TestLabelsChartSelfHeals|TestLabelsEscCloses' -v`
Expected: FAIL — `m.labels.chartNS undefined` / `activeChartNS undefined`.

- [ ] **Step 3: Add the chartNS field**

In `internal/tui/labels.go`, add to `labelsModel` (after `entries []labelEntry` from Task 2):

```go
	chartNS string // active "tasks by label" chart namespace ("" = list view)
```

- [ ] **Step 4: Add activeChartNS and toggleNamespaceFacet**

In `internal/tui/labels.go`, add these methods (place them after `selected()`):

```go
// activeChartNS returns the namespace currently charted, self-healing to "" if
// its facet token is no longer present in the Tasks filter (e.g. the user
// edited or cleared the filter directly).
func (l *labelsModel) activeChartNS() string {
	if l.chartNS == "" {
		return ""
	}
	if !filterHasToken(l.m.tasks.filter, facetToken(l.m.projectScope, l.chartNS)) {
		l.chartNS = ""
	}
	return l.chartNS
}

// toggleNamespaceFacet toggles the ns wildcard facet in the Tasks filter and
// the Labels chart view. If the facet is present it is removed and the chart
// closes; otherwise it is added and the chart opens for ns. Refreshes the
// Tasks pane so grouping updates immediately.
func (l *labelsModel) toggleNamespaceFacet(ns string) {
	token := facetToken(l.m.projectScope, ns)
	if filterHasToken(l.m.tasks.filter, token) {
		l.m.tasks.filter = filterRemoveToken(l.m.tasks.filter, token)
		l.chartNS = ""
	} else {
		l.m.tasks.filter = filterAddToken(l.m.tasks.filter, token)
		l.chartNS = ns
	}
	l.m.tasks.cursor = 0
	l.m.tasks.refresh()
}
```

- [ ] **Step 5: Wire Enter and Esc in handleListKey**

In `internal/tui/labels.go` `handleListKey`, when the chart is showing all list keys except `Esc` are inert. At the very top of `handleListKey` (before the `switch`), add:

```go
	if l.activeChartNS() != "" {
		if k.String() == "esc" {
			l.chartNS = ""
		}
		return nil
	}
```

Then replace the existing `case "enter":` block (currently opens detail) with a context-sensitive version:

```go
	case "enter":
		if l.cursor < 0 || l.cursor >= len(l.entries) {
			return nil
		}
		e := l.entries[l.cursor]
		switch e.kind {
		case entryHeaderNS:
			if l.m.projectScope == "" {
				return nil
			}
			l.toggleNamespaceFacet(e.ns)
		case entryHeaderTags:
			// no-op: bare tags have no namespace to facet on.
		case entryRow:
			l.detail = labelDetailState{row: e.row}
			l.view = lViewDetail
		}
```

Note: this replaces the old `case "enter":` that called `l.selected()`; the row branch above inlines the same detail-open behavior.

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/tui/ -run 'TestLabelsEnterOnNamespace|TestLabelsEnterOnTags|TestLabelsChartSelfHeals|TestLabelsEscCloses' -v`
Expected: PASS

- [ ] **Step 7: Run the full package**

Run: `go test ./internal/tui/`
Expected: PASS (confirm `TestLabelDetailDashboardSections` still passes — Enter on a row still opens detail).

- [ ] **Step 8: Commit**

```bash
git add internal/tui/labels.go internal/tui/labels_test.go
git commit -m "Toggle Tasks facet and chart state from Labels namespace headers (ATM-0041)"
```

---

### Task 4: Chart view rendering

Renders the "tasks by label" bar chart when `activeChartNS()` is non-empty, reusing `meterBar`. Rows are the labels in the active namespace with their usage counts (already loaded in `l.rows`). Matches the approved mockup: header line, blank, one meter row per label, blank, `Esc` hint.

**Files:**
- Modify: `internal/tui/labels.go` (`View()` dispatch ~line 268; add `renderChart()`)
- Test: `internal/tui/labels_test.go`

**Interfaces:**
- Consumes: `activeChartNS()`, `chartNS`, `entries`/`labelRow` usage data (Tasks 2–3); `meterBar` (from `internal/tui/projects.go`, package-level).
- Produces: `func (l *labelsModel) renderChart() string`.

- [ ] **Step 1: Write the failing test**

Add to `internal/tui/labels_test.go`:

```go
func TestLabelsChartShowsUsageBars(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	// Give status:open a usage count so a non-empty bar renders.
	if _, err := m.store.CreateTask("ATM", "t1", "", []string{"ATM:status:open"}, m.actor); err != nil {
		t.Fatal(err)
	}
	update(t, m, "s")
	update(t, m, "3")
	cursorToNamespaceHeader(t, m, "status")
	update(t, m, "enter") // open chart

	v := m.labels.View()
	mustContain(t, v, "chart: status")
	mustContain(t, v, "namespace: status")
	mustContain(t, v, "ATM:status:open")
	mustContain(t, v, "█")           // at least one filled meter cell
	mustContain(t, v, "[Esc] back")
}
```

The `store.CreateTask` signature is confirmed as
`CreateTask(projectCode, title, description string, labels []string, actor string) (*Task, error)`
(`internal/store/task.go:10`), which the test call above already matches.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tui/ -run TestLabelsChartShowsUsageBars -v`
Expected: FAIL — the view still renders the list; `chart: status` not present.

- [ ] **Step 3: Add renderChart and dispatch from View()**

In `internal/tui/labels.go`, change `View()` (~line 268) so it checks the chart state first:

```go
func (l *labelsModel) View() string {
	if l.view == lViewList && l.activeChartNS() != "" {
		return l.renderChart()
	}
	switch l.view {
	case lViewList:
		return l.renderList()
	case lViewDetail:
		return l.renderDetail()
	}
	return ""
}
```

Add `renderChart` (place it after `renderList`):

```go
// renderChart renders the "tasks by label" bar chart for the active namespace:
// one meter row per label carrying that namespace. Percentages are each
// label's share of total usage within the namespace (matching the approved
// mockup: shares sum to ~100%); counts are absolute project-wide usage.
func (l *labelsModel) renderChart() string {
	ns := l.chartNS
	var rows []labelRow
	total := 0
	for _, e := range l.entries {
		if e.kind == entryRow && strings.HasPrefix(e.row.suffix, ns+":") {
			rows = append(rows, e.row)
			total += e.row.usage
		}
	}

	var b strings.Builder
	fmt.Fprintf(&b, "%s\n", dashboardLine(l.width, fmt.Sprintf("chart: %s", ns)))
	fmt.Fprintf(&b, "%s\n", dashboardLine(l.width, l.m.styles.Muted.Render(fmt.Sprintf("project: %s   namespace: %s", l.m.projectScope, ns))))
	b.WriteString("\n")

	if len(rows) == 0 {
		b.WriteString(dashboardLine(l.width, l.m.styles.Muted.Render("no labels in this namespace")))
		b.WriteString("\n")
	} else {
		nameW := 0
		for _, r := range rows {
			if w := len(r.full); w > nameW {
				nameW = w
			}
		}
		meterW := l.width - nameW - 16
		if meterW < 10 {
			meterW = 10
		}
		for _, r := range rows {
			percent := 0
			if total > 0 {
				percent = (r.usage*100 + total/2) / total
			}
			line := fmt.Sprintf(" %-*s %s %3d%% %4d", nameW, r.full, meterBar(percent, meterW), percent, r.usage)
			b.WriteString(dashboardLine(l.width, line))
			b.WriteString("\n")
		}
	}

	b.WriteString("\n")
	b.WriteString(dashboardLine(l.width, l.m.styles.Muted.Render("[Esc] back to list")))
	return padToHeight(b.String(), l.contentHeight)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/tui/ -run TestLabelsChartShowsUsageBars -v`
Expected: PASS

- [ ] **Step 5: Run the full package**

Run: `go test ./internal/tui/`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/tui/labels.go internal/tui/labels_test.go
git commit -m "Render tasks-by-label bar chart in Labels pane (ATM-0041)"
```

---

### Task 5: Status hints, keymap, and help

Updates the Labels list status hint and the global keymap/help tables to document the new `Enter` (namespace select vs. label detail) and Tasks `c` (clear filter) bindings, plus the chart-view `Esc` hint.

**Files:**
- Modify: `internal/tui/labels.go` (`statusHint()` ~line 395)
- Modify: `internal/tui/keymap.go` (`keymapRows` ~line 24)
- Modify: `internal/tui/tasks.go` (Tasks list status hint — locate via grep in Step 2)
- Test: `internal/tui/labels_test.go`, and update any keymap/help golden assertions if present

**Interfaces:**
- Consumes: chart state from Task 3 (`activeChartNS()`).
- Produces: updated hint strings only; no new exported symbols.

- [ ] **Step 1: Write the failing test**

Add to `internal/tui/labels_test.go`:

```go
func TestLabelsChartStatusHint(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	update(t, m, "s")
	update(t, m, "3")
	cursorToNamespaceHeader(t, m, "status")
	update(t, m, "enter")
	hint := m.labels.statusHint()
	mustContain(t, hint, "[Esc]back")
}

func TestLabelsListStatusHintShowsSelect(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme")
	update(t, m, "s")
	update(t, m, "3")
	mustContain(t, m.labels.statusHint(), "[Enter]")
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tui/ -run 'TestLabelsChartStatusHint|TestLabelsListStatusHintShowsSelect' -v`
Expected: `TestLabelsChartStatusHint` FAILs (list hint returned while chart is active). `TestLabelsListStatusHintShowsSelect` likely already passes (existing hint has `[Enter]detail`); keep it as a guard.

- [ ] **Step 3: Update the Labels statusHint for chart mode**

In `internal/tui/labels.go`, update `statusHint()` (~line 395). Add a chart-mode branch at the top and keep the existing list/detail branches:

```go
func (l *labelsModel) statusHint() string {
	if l.m.projectScope == "" {
		return "[?]keys"
	}
	if l.activeChartNS() != "" {
		return "[Esc]back to list"
	}
	if l.view == lViewDetail {
		return "[d]esc [l]remove [Esc]back"
	}
	return "[a]dd [d]esc [l]remove [S]eed [Enter]select/detail [Esc]back [?]keys"
}
```

- [ ] **Step 4: Update the Tasks list status hint for `c`**

In `internal/tui/tasks.go`, in `statusHint()` (line 1028), change the list-mode hint from:

```go
	hint := "[/]filter [s]sort [a]dd [Enter]detail [?]keys"
```

to:

```go
	hint := "[/]filter [c]lear [s]sort [a]dd [Enter]detail [?]keys"
```

(Leave the `filterEditing` override on line 1030 unchanged.)

- [ ] **Step 5: Update the keymap reference table**

In `internal/tui/keymap.go`, update `keymapRows`:

Change the `Enter` row (line 28) so the Labels column names both behaviors:

```go
	{"Enter", "open detail", "open detail / toggle group", "select namespace / open label detail", "confirm overlay"},
```

Change the `/` row (line 30) to add a clear entry, and add a dedicated `c` row after it:

```go
	{"/", "-", "edit filter", "-", "-"},
	{"c", "-", "clear filter", "-", "-"},
```

- [ ] **Step 6: Update help parity table if it asserts specific text**

Run: `go test ./internal/tui/ -run 'Help|Keymap|Parity' -v`
If a help/keymap test fails on changed text, update its expected string to match the new hint/keymap wording. If no such test exists, skip. Do not add new golden snapshots.

- [ ] **Step 7: Run the hint tests to verify they pass**

Run: `go test ./internal/tui/ -run 'TestLabelsChartStatusHint|TestLabelsListStatusHintShowsSelect' -v`
Expected: PASS

- [ ] **Step 8: Run the full package**

Run: `go test ./internal/tui/`
Expected: PASS

- [ ] **Step 9: Commit**

```bash
git add internal/tui/labels.go internal/tui/keymap.go internal/tui/tasks.go internal/tui/labels_test.go
git commit -m "Document namespace-select and clear-filter keys in hints and keymap (ATM-0041)"
```

---

### Task 6: Full verification

**Files:** none (verification only).

- [ ] **Step 1: Run the verification gate**

Run: `make verify`
Expected: build succeeds and all tests pass.

- [ ] **Step 2: Manual smoke check (optional but recommended)**

Run the TUI, select a project, focus the Labels pane (`3`), move the cursor onto a namespace header, press `Enter`. Confirm: the Labels pane shows the bar chart, the Tasks pane (`2`) is now grouped by that namespace, `Esc` in Labels returns to the list with the Tasks filter still applied, and `c` in the Tasks pane clears the filter.

- [ ] **Step 3: Final commit if any fixes were needed**

```bash
git add -A
git commit -m "Fix issues found during ATM-0041 verification"
```

(Skip if `make verify` was already green and no changes were made.)
