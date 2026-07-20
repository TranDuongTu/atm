# TUI Recent Events Feed Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A lazygit-commits-style "Recent Events" feed in the Projects pane (pane [1]), between the project list and the summary charts, rendering the selected project's event log as git-commit-shaped digest lines (ATM-793b19).

**Architecture:** The feed is a third stacked section in the list view of `internal/tui/projects.go`, fed by the existing `Store.ReadLogCached(code)` read. Two new fields (`ID`, `Parents`) flow from the v2 event envelope through `core.LogEntry` so the TUI can draw a commit-graph gutter and short hashes. All formatting logic lives in a new `internal/tui/events_feed.go` as pure, unit-tested helpers.

**Tech Stack:** Go 1.22+, Bubble Tea + Lipgloss, existing `internal/store` / `internal/store/eventlog` read APIs.

**Spec:** `docs/superpowers/specs/2026-07-20-tui-recent-events-feed-design.md` (committed 4c15835).

## Global Constraints

- Run `make verify` before declaring done; all existing CLI goldens must stay byte-identical.
- No store schema changes and no CLI output changes; the only read-model change is the two new `core.LogEntry` fields.
- Strict 1:1: one event = one feed line; no coalescing.
- No emojis in code or commits. Follow existing comment density and idiom.
- The digest minus sign is U+2212 `−` (matches the spec), not ASCII `-`.
- Feed line column budget (spec): graph gutter, id (7, dim), subject (7), actor (8), message (flex), age (right-aligned, dim). Below 36 inner columns drop the id column; below 30 drop the age.
- Commit after every task with a `feat(ATM-793b19): …` (or `test:`/`docs:` as fitting) message; stage explicit paths only (never `git add -A` — other sessions edit this repo concurrently).

## File Structure

- Modify `internal/core/activity.go` — `LogEntry` gains `ID`, `Parents`.
- Modify `internal/store/eventlog/views.go` — populate them in `v2LogEntriesFrom`.
- Test `internal/store/eventsource_views_test.go` — read-model contract test.
- Create `internal/tui/events_feed.go` — all feed logic: digest wording, column helpers, graph lanes, line assembly, section renderer, subfocus key handler.
- Create `internal/tui/events_feed_test.go` — unit tests for the above.
- Modify `internal/tui/projects.go` — 3-way split, section stacking, `L` entry point, subfocus routing, status hints, state fields.
- Modify `internal/tui/app.go` — the global `esc` branch learns about feed subfocus.
- Modify `internal/tui/keymap.go` — `L` row in the keymap reference table.
- Modify `internal/tui/app_test.go` — the 30/70 split test becomes the 3-way split test.
- Modify `docs/superpowers/specs/2026-07-20-tui-recent-events-feed-design.md` — record the two interaction deltas discovered in the code (Task 5).
- Modify `CHANGELOG.md` — Unreleased/feat line (Task 6).

---

### Task 1: Read model — `LogEntry` carries event id and parents

**Files:**
- Modify: `internal/core/activity.go:8-15`
- Modify: `internal/store/eventlog/views.go:183`
- Test: `internal/store/eventsource_views_test.go`

**Interfaces:**
- Consumes: `eventsource.Event.ID string`, `eventsource.Event.Parents []string` (already on the envelope, `libs/eventsource/event.go`).
- Produces: `core.LogEntry.ID string` (`"sha256:…"`, empty for v1 entries) and `core.LogEntry.Parents []string` (nil for v1 entries). Tasks 3–4 rely on exactly these two names.

- [ ] **Step 1: Write the failing test** — append to `internal/store/eventsource_views_test.go` (add `"strings"` to its imports if absent):

```go
// TestLogEntriesCarryEventIDAndParents pins the read-model contract the TUI
// events feed draws its commit-graph from: every v2 entry carries its
// content-addressed id, and each entry's parents reference earlier ids.
func TestLogEntriesCarryEventIDAndParents(t *testing.T) {
	s := testStore(t)
	_, _ = s.CreateProject("ATM", "x", "admin@cli:unset")
	if _, err := s.CreateTask("ATM", "t", "", nil, "admin@cli:unset"); err != nil {
		t.Fatal(err)
	}
	entries, err := s.ReadLogCached("ATM")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) < 2 {
		t.Fatalf("want >=2 entries, got %d", len(entries))
	}
	ids := map[string]bool{}
	for i, e := range entries {
		if !strings.HasPrefix(e.ID, "sha256:") {
			t.Fatalf("entry %d ID = %q, want sha256: prefix", i, e.ID)
		}
		ids[e.ID] = true
	}
	last := entries[len(entries)-1]
	if len(last.Parents) == 0 {
		t.Fatal("last entry has no parents")
	}
	for _, p := range last.Parents {
		if !ids[p] {
			t.Fatalf("parent %q not among earlier event ids", p)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/store -run TestLogEntriesCarryEventIDAndParents -v`
Expected: FAIL — `entry 0 ID = "", want sha256: prefix` (fields don't exist yet → compile error first; add the fields' *declarations* only if needed to get a red test, but normally the compile error is the failing signal).

- [ ] **Step 3: Add the fields** in `internal/core/activity.go`:

```go
type LogEntry struct {
	Seq     int             `json:"seq"`
	At      time.Time       `json:"at"`
	Actor   string          `json:"actor"`
	Action  string          `json:"action"`
	Subject Subject         `json:"subject"`
	Payload json.RawMessage `json:"payload,omitempty"`
	// ID is the v2 content-addressed event id ("sha256:…"); empty for v1
	// entries. Parents is the event's causal frontier (event ids); nil for
	// v1 entries. Both exist for DAG-aware display (the TUI events feed) —
	// the fold order in Seq remains the only ordering authority.
	ID      string   `json:"id,omitempty"`
	Parents []string `json:"parents,omitempty"`
}
```

And in `internal/store/eventlog/views.go` change line 183 to:

```go
		out = append(out, core.LogEntry{Seq: i + 1, At: ev.At, Actor: ev.Actor, Action: ev.Action, Subject: subj, Payload: ev.Payload, ID: ev.ID, Parents: ev.Parents})
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/store -run TestLogEntriesCarryEventIDAndParents -v`
Expected: PASS

- [ ] **Step 5: Guard the goldens** — `core.LogEntry` should not be JSON-marshaled on any CLI path; confirm:

Run: `grep -rn "LogEntry" internal/cli/` — expected: no hits.
Run: `go test ./internal/store ./internal/cli` — expected: PASS (goldens unchanged).

- [ ] **Step 6: Commit**

```bash
git add internal/core/activity.go internal/store/eventlog/views.go internal/store/eventsource_views_test.go
git commit -m "feat(ATM-793b19): LogEntry carries v2 event id and parents"
```

---

### Task 2: Digest wording and column helpers

**Files:**
- Create: `internal/tui/events_feed.go`
- Test: `internal/tui/events_feed_test.go`

**Interfaces:**
- Consumes: `core.LogEntry` (with Task 1's `ID`).
- Produces (Task 4 consumes these exact names):
  - `shortEventID(id string) string` — 7-char hex, `""` for v1.
  - `compactAge(t, now time.Time) string` — `now`/`5m`/`3h`/`2d`/`4mo`/`1y`, `""` for zero time.
  - `eventFeedActor(actor string) string` — `persona@agent:model` → ≤8-char `per@agen`.
  - `eventFeedSubject(e core.LogEntry, projectCode string) string` — task alias sans project prefix; comments show their parent task's alias; `–` otherwise.
  - `eventDigestMessage(e core.LogEntry, projectCode string) string` — per-action digest wording.
  - `feedLabel(label, projectCode string) string` — label sans project prefix, bare value for the `status:` facet.

- [ ] **Step 1: Write the failing tests** — create `internal/tui/events_feed_test.go`:

```go
package tui

import (
	"encoding/json"
	"testing"
	"time"

	"atm/internal/core"
)

func feedEntry(action string, payload string) core.LogEntry {
	e := core.LogEntry{Action: action}
	if payload != "" {
		e.Payload = json.RawMessage(payload)
	}
	return e
}

func TestShortEventID(t *testing.T) {
	if got := shortEventID("sha256:84fbf586004add7e"); got != "84fbf58" {
		t.Fatalf("shortEventID = %q, want 84fbf58", got)
	}
	if got := shortEventID(""); got != "" {
		t.Fatalf("shortEventID(v1) = %q, want empty", got)
	}
}

func TestCompactAge(t *testing.T) {
	now := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		at   time.Time
		want string
	}{
		{now.Add(-30 * time.Second), "now"},
		{now.Add(-5 * time.Minute), "5m"},
		{now.Add(-3 * time.Hour), "3h"},
		{now.Add(-49 * time.Hour), "2d"},
		{time.Time{}, ""},
	}
	for _, c := range cases {
		if got := compactAge(c.at, now); got != c.want {
			t.Errorf("compactAge(%v) = %q, want %q", c.at, got, c.want)
		}
	}
}

func TestEventFeedActor(t *testing.T) {
	cases := []struct{ in, want string }{
		{"developer@claude:opus-4.8", "dev@clau"},
		{"admin@tui:unset", "adm@tui"},
		{"manager@ollama:unset", "man@olla"},
		{"default", "def"}, // v1-era actor without @
	}
	for _, c := range cases {
		if got := eventFeedActor(c.in); got != c.want {
			t.Errorf("eventFeedActor(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestEventFeedSubject(t *testing.T) {
	task := core.LogEntry{Subject: core.Subject{Kind: "task", ID: "ATM-90171b"}}
	if got := eventFeedSubject(task, "ATM"); got != "90171b" {
		t.Fatalf("task subject = %q, want 90171b", got)
	}
	comment := core.LogEntry{Subject: core.Subject{Kind: "comment", ID: "ATM-90171b-cdf66"}}
	if got := eventFeedSubject(comment, "ATM"); got != "90171b" {
		t.Fatalf("comment subject = %q, want parent task alias 90171b", got)
	}
	project := core.LogEntry{Subject: core.Subject{Kind: "project", Code: "ATM"}}
	if got := eventFeedSubject(project, "ATM"); got != "–" {
		t.Fatalf("project subject = %q, want –", got)
	}
}

func TestFeedLabel(t *testing.T) {
	if got := feedLabel("ATM:status:done", "ATM"); got != "done" {
		t.Fatalf("status label = %q, want bare value done", got)
	}
	if got := feedLabel("ATM:type:bug", "ATM"); got != "type:bug" {
		t.Fatalf("non-status label = %q, want type:bug", got)
	}
}

func TestEventDigestMessage(t *testing.T) {
	cases := []struct {
		action, payload, want string
	}{
		{"task.created", `{"title":"Fix the cache"}`, `created "Fix the cache"`},
		{"task.created", `{}`, "created"}, // v1-upgrade payloads may lack keys
		{"task.title-changed", `{"title":"New title"}`, `retitled "New title"`},
		{"task.description-changed", "", "description edited"},
		{"task.label-added", `{"label":"ATM:status:done"}`, "+done"},
		{"task.label-removed", `{"label":"ATM:status:in-progress"}`, "−in-progress"},
		{"task.label-added", `{"labels":["x"]}`, "+label"}, // v1 snapshot payload
		{"task.removed", "", "removed"},
		{"task.meta-changed", "", "meta"},
		{"comment.created", "", "commented"},
		{"comment.body-changed", "", "comment edited"},
		{"comment.label-added", `{"label":"ATM:comment:decision"}`, "comment +comment:decision"},
		{"comment.removed", "", "comment removed"},
		{"label.upserted", `{"name":"ATM:status:open"}`, "label status:open"},
		{"project.created", "", "project created"},
		{"project.name-changed", `{"name":"Acme"}`, `renamed "Acme"`},
		{"project.capability-enabled", `{"capability":"contextmap"}`, "+contextmap"},
		{"project.capability-disabled", `{"capability":"contextmap"}`, "−contextmap"},
	}
	for _, c := range cases {
		e := feedEntry(c.action, c.payload)
		if got := eventDigestMessage(e, "ATM"); got != c.want {
			t.Errorf("%s: digest = %q, want %q", c.action, got, c.want)
		}
	}
	// label.removed names its subject, not a payload field.
	e := core.LogEntry{Action: "label.removed", Subject: core.Subject{Kind: "label", Name: "ATM:status:open"}}
	if got := eventDigestMessage(e, "ATM"); got != "−label status:open" {
		t.Errorf("label.removed digest = %q, want −label status:open", got)
	}
}
```

Note: `feedLabel("ATM:status:done", "ATM")` returning the *bare value* (`done`) intentionally drops the facet; `status:` is the project's dominant, unambiguous facet (spec decision). Non-status labels keep their facet.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/tui -run 'TestShortEventID|TestCompactAge|TestEventFeedActor|TestEventFeedSubject|TestFeedLabel|TestEventDigestMessage' -v`
Expected: compile FAIL — helpers undefined.

- [ ] **Step 3: Implement the helpers** — create `internal/tui/events_feed.go`:

```go
package tui

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"atm/internal/core"
)

// The Recent Events feed (ATM-793b19) renders the selected project's event
// log git-log-style: one event per line, newest first, with a commit-graph
// gutter derived from the v2 parents DAG. Everything in this file is a pure
// formatting helper over core.LogEntry; the pane wiring lives in projects.go.

// shortEventID renders the 7-char short form of a "sha256:…" event id; a v1
// entry (no id) renders empty and the column shows blank.
func shortEventID(id string) string {
	h := strings.TrimPrefix(id, "sha256:")
	if len(h) > 7 {
		h = h[:7]
	}
	return h
}

// compactAge is relTime's terser cousin for the feed's right-aligned age
// column: no " ago" suffix, so the column stays ≤4 cells.
func compactAge(t, now time.Time) string {
	if t.IsZero() {
		return ""
	}
	d := now.Sub(t)
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	case d < 30*24*time.Hour:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	case d < 365*24*time.Hour:
		return fmt.Sprintf("%dmo", int(d.Hours()/(24*30)))
	default:
		return fmt.Sprintf("%dy", int(d.Hours()/(24*365)))
	}
}

// eventFeedActor abbreviates the actor grammar persona@agent:model to a
// ≤8-cell "per@agen": persona to 3 runes, agent to 4, model dropped.
func eventFeedActor(actor string) string {
	persona, rest, hasAgent := strings.Cut(actor, "@")
	agent, _, _ := strings.Cut(rest, ":")
	p := []rune(persona)
	if len(p) > 3 {
		p = p[:3]
	}
	if !hasAgent || agent == "" {
		return string(p)
	}
	a := []rune(agent)
	if len(a) > 4 {
		a = a[:4]
	}
	return string(p) + "@" + string(a)
}

// eventFeedSubject renders the subject column: the TASK the event touched,
// as its alias with the project-code prefix stripped (the feed is scoped to
// one project, so the prefix carries no information). A comment's alias is
// <task-alias>-c<hex>; the trailing comment segment is cut so the column
// names the task. Project/label subjects render "–".
func eventFeedSubject(e core.LogEntry, projectCode string) string {
	switch e.Subject.Kind {
	case "task", "comment":
		alias := strings.TrimPrefix(e.Subject.ID, projectCode+"-")
		if e.Subject.Kind == "comment" {
			if i := strings.LastIndex(alias, "-c"); i > 0 {
				alias = alias[:i]
			}
		}
		return alias
	}
	return "–"
}

// feedLabel renders a label for the feed: the project prefix is always
// stripped, and the status facet — the dominant, unambiguous one — renders
// as its bare value ("done"). Other facets keep their name ("type:bug").
func feedLabel(label, projectCode string) string {
	l := strings.TrimPrefix(label, projectCode+":")
	if v, ok := strings.CutPrefix(l, "status:"); ok && v != "" {
		return v
	}
	return l
}

// eventDigestMessage renders the digest wording for one event. Payload
// shapes differ between native-v2 events ({"label": …}) and v1-upgraded
// snapshots (a full entity dump), so every payload field is optional and
// absence degrades to the bare verb.
func eventDigestMessage(e core.LogEntry, projectCode string) string {
	var p struct {
		Title      string `json:"title"`
		Name       string `json:"name"`
		Label      string `json:"label"`
		Capability string `json:"capability"`
	}
	if len(e.Payload) > 0 {
		_ = json.Unmarshal(e.Payload, &p)
	}
	switch e.Action {
	case "task.created":
		if p.Title != "" {
			return fmt.Sprintf("created %q", p.Title)
		}
		return "created"
	case "task.title-changed":
		if p.Title != "" {
			return fmt.Sprintf("retitled %q", p.Title)
		}
		return "retitled"
	case "task.description-changed":
		return "description edited"
	case "task.label-added", "task.label-removed", "comment.label-added", "comment.label-removed":
		sign := "+"
		if strings.HasSuffix(e.Action, "-removed") {
			sign = "−"
		}
		prefix := ""
		if strings.HasPrefix(e.Action, "comment.") {
			prefix = "comment "
		}
		if p.Label != "" {
			return prefix + sign + feedLabel(p.Label, projectCode)
		}
		return prefix + sign + "label"
	case "task.removed":
		return "removed"
	case "task.meta-changed":
		return "meta"
	case "comment.created":
		return "commented"
	case "comment.body-changed":
		return "comment edited"
	case "comment.removed":
		return "comment removed"
	case "label.upserted":
		if p.Name != "" {
			return "label " + feedLabel(p.Name, projectCode)
		}
		return "label upserted"
	case "label.removed":
		if e.Subject.Name != "" {
			return "−label " + feedLabel(e.Subject.Name, projectCode)
		}
		return "−label"
	case "project.created":
		return "project created"
	case "project.name-changed":
		if p.Name != "" {
			return fmt.Sprintf("renamed %q", p.Name)
		}
		return "renamed"
	case "project.removed":
		return "project removed"
	case "project.capability-enabled":
		if p.Capability != "" {
			return "+" + p.Capability
		}
		return "+capability"
	case "project.capability-disabled":
		if p.Capability != "" {
			return "−" + p.Capability
		}
		return "−capability"
	}
	return e.Action
}
```

> **Correction (post-launch review):** the two code samples above are now
> known-stale and would mislead a reader implementing from this plan
> directly:
> - The `TestEventDigestMessage` sample's `label.upserted` case reads the
>   label name from the event payload (`{"name":"ATM:status:open"}`).
>   Production `label.upserted` emitters only ever put the name on
>   `Subject.Name`, never in the payload, so the shipped code reads
>   `e.Subject.Name` (see `internal/tui/events_feed.go`), matching how
>   `label.removed` was already handled in this same sample.
> - The sample's `label.upserted`/`label.removed` cases route the name
>   through `feedLabel`, which strips the `status:` facet to its bare value
>   (`feedLabel("ATM:status:open", "ATM")` → `"open"`). That contradicts the
>   sample's own test expectation of `"label status:open"`. The shipped code
>   does not call `feedLabel` for these two label-registry actions — it
>   strips only the project prefix (`strings.TrimPrefix(e.Subject.Name,
>   projectCode+":")`) and preserves the full facet, since for a label
>   *management* event (as opposed to a label being applied to a task) the
>   facet is the information being reported on, not noise to fold away.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/tui -run 'TestShortEventID|TestCompactAge|TestEventFeedActor|TestEventFeedSubject|TestFeedLabel|TestEventDigestMessage' -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/tui/events_feed.go internal/tui/events_feed_test.go
git commit -m "feat(ATM-793b19): event digest wording and feed column helpers"
```

---

### Task 3: Commit-graph lane assignment

**Files:**
- Modify: `internal/tui/events_feed.go`
- Test: `internal/tui/events_feed_test.go`

**Interfaces:**
- Consumes: `core.LogEntry.ID`, `core.LogEntry.Parents` (Task 1).
- Produces: `const maxGraphLanes = 3` and `eventGraphRows(entries []core.LogEntry) [][]rune` — entries NEWEST-FIRST (`entries[0]` is the newest); row i has one glyph per active lane (`●` event lane, `│` pass-through). Task 4 consumes both names.

Design note (spec deviation, recorded in Task 5's spec amendment): no diagonal
junction glyphs (`├─╮`) in this iteration — a fork/merge appears as a second
parallel lane appearing/disappearing. One event stays one row.

- [ ] **Step 1: Write the failing tests** — append to `internal/tui/events_feed_test.go`:

```go
func TestEventGraphRowsLinearChain(t *testing.T) {
	entries := []core.LogEntry{ // newest first
		{ID: "sha256:cc", Parents: []string{"sha256:bb"}},
		{ID: "sha256:bb", Parents: []string{"sha256:aa"}},
		{ID: "sha256:aa"},
	}
	rows := eventGraphRows(entries)
	if len(rows) != 3 {
		t.Fatalf("rows = %d, want 3", len(rows))
	}
	for i, r := range rows {
		if string(r) != "●" {
			t.Fatalf("row %d = %q, want single ●", i, string(r))
		}
	}
}

func TestEventGraphRowsForkAndMerge(t *testing.T) {
	// DAG: root a; concurrent b and c (both parent a); d merges b+c.
	// Newest-first display order: d, c, b, a.
	entries := []core.LogEntry{
		{ID: "sha256:dd", Parents: []string{"sha256:bb", "sha256:cc"}},
		{ID: "sha256:cc", Parents: []string{"sha256:aa"}},
		{ID: "sha256:bb", Parents: []string{"sha256:aa"}},
		{ID: "sha256:aa"},
	}
	rows := eventGraphRows(entries)
	want := []string{"●", "│●", "●│", "●│"}
	for i, w := range want {
		if string(rows[i]) != w {
			t.Fatalf("row %d = %q, want %q\nall rows: %q", i, string(rows[i]), w, rows)
		}
	}
}

func TestEventGraphRowsLaneCap(t *testing.T) {
	// Four concurrent tips (no lane awaits them): lanes must cap at 3.
	entries := []core.LogEntry{
		{ID: "sha256:t1", Parents: []string{"sha256:aa"}},
		{ID: "sha256:t2", Parents: []string{"sha256:aa"}},
		{ID: "sha256:t3", Parents: []string{"sha256:aa"}},
		{ID: "sha256:t4", Parents: []string{"sha256:aa"}},
		{ID: "sha256:aa"},
	}
	for i, r := range eventGraphRows(entries) {
		if len(r) > maxGraphLanes {
			t.Fatalf("row %d has %d lanes, cap is %d", i, len(r), maxGraphLanes)
		}
	}
}

func TestEventGraphRowsV1EntriesSingleLane(t *testing.T) {
	entries := []core.LogEntry{{Action: "task.created"}, {Action: "project.created"}}
	for i, r := range eventGraphRows(entries) {
		if string(r) != "●" {
			t.Fatalf("v1 row %d = %q, want ●", i, string(r))
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/tui -run TestEventGraphRows -v`
Expected: compile FAIL — `eventGraphRows` undefined.

- [ ] **Step 3: Implement** — append to `internal/tui/events_feed.go`:

```go
// maxGraphLanes caps the commit-graph gutter width. Local histories are
// linear (one lane); lanes >1 appear only after a sync merges concurrent
// replicas, and 3 parallel branches is already an extraordinary display.
const maxGraphLanes = 3

// eventGraphRows assigns commit-graph lanes to entries rendered NEWEST-FIRST
// (entries[0] is the newest event). Walking downward, each lane "awaits" an
// event id: the row's event takes the first lane awaiting its id (or opens a
// new lane — a branch tip), other lanes awaiting the same id converge into
// it, and the event's lane then awaits its first parent while extra parents
// open new lanes (a fork, i.e. a merge event read top-down). Overflow beyond
// maxGraphLanes reuses the last lane; extra parents past the cap are simply
// not tracked — the chain re-anchors when an awaited id appears. Entries
// without ids (v1 logs) render a single static lane.
func eventGraphRows(entries []core.LogEntry) [][]rune {
	rows := make([][]rune, len(entries))
	var lanes []string
	for i, e := range entries {
		if e.ID == "" {
			rows[i] = []rune{'●'}
			continue
		}
		lane := -1
		for j, want := range lanes {
			if want == e.ID {
				lane = j
				break
			}
		}
		if lane == -1 {
			if len(lanes) < maxGraphLanes {
				lanes = append(lanes, e.ID)
				lane = len(lanes) - 1
			} else {
				lane = len(lanes) - 1
				lanes[lane] = e.ID
			}
		}
		row := make([]rune, len(lanes))
		for j := range lanes {
			if j == lane {
				row[j] = '●'
			} else {
				row[j] = '│'
			}
		}
		rows[i] = row
		// Converge: every other lane awaiting this id merges into `lane`.
		next := lanes[:0:0]
		for j, want := range lanes {
			if j != lane && want == e.ID {
				continue
			}
			if j == lane {
				lane = len(next)
			}
			next = append(next, want)
		}
		lanes = next
		// Advance: await the first parent; extra parents open lanes (capped).
		// A parentless event is the root — its lane closes.
		if len(e.Parents) == 0 {
			lanes = append(lanes[:lane], lanes[lane+1:]...)
			continue
		}
		lanes[lane] = e.Parents[0]
		for _, parent := range e.Parents[1:] {
			if len(lanes) >= maxGraphLanes {
				break
			}
			lanes = append(lanes, parent)
		}
	}
	return rows
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/tui -run TestEventGraphRows -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/tui/events_feed.go internal/tui/events_feed_test.go
git commit -m "feat(ATM-793b19): commit-graph lane assignment for the events feed"
```

---

### Task 4: Feed section rendering and the 3-way pane split

**Files:**
- Modify: `internal/tui/projects.go:17-32` (state), `:67-88` (split), `:200-226` (split callers), `:491-504` (stacking)
- Modify: `internal/tui/events_feed.go` (renderer + line assembly)
- Modify: `internal/tui/app_test.go:859-875` (split test)
- Test: `internal/tui/events_feed_test.go`

**Interfaces:**
- Consumes: `eventGraphRows`, `maxGraphLanes` (Task 3); digest/column helpers (Task 2); `windowLines`, `dashboardLine`, `padToHeight`, `truncateRunes` (existing, `internal/tui/styles.go`); `p.m.store.ReadLogCached`, `core.IsIntegrity`, `core.Now()` (existing).
- Produces:
  - `projectPaneSplitHeights(total int) (listH, eventsH, summaryH int)` — CHANGED SIGNATURE (was 2 returns).
  - `projectsModel.logsFocus bool`, `projectsModel.logsCursor int` — Task 5 consumes these names.
  - `(p *projectsModel) renderEventsFeed(height int) string`
  - `(p *projectsModel) eventFeedLine(e core.LogEntry, lanes []rune, laneW int, now time.Time, plain bool) string`

- [ ] **Step 1: Write the failing tests.** Replace `TestProjectsViewUsesThirtySeventySplit` in `internal/tui/app_test.go` (lines 859-875) with:

```go
func TestProjectsViewUsesThreeWaySplit(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 30)
	seedProject(t, m, "ATM", "Acme Task Manager")
	body := m.projects.View()
	lines := strings.Split(body, "\n")
	find := func(sub string) int {
		for i, line := range lines {
			if strings.Contains(line, sub) {
				return i
			}
		}
		return -1
	}
	// contentHeight 27: list 8 (30%), events 9 (35%), summary 10 (rest).
	if got := find("Recent Events"); got != 8 {
		t.Fatalf("events caption on line %d, want 8\n--- body ---\n%s", got, body)
	}
	if got := find("Project Summary"); got != 17 {
		t.Fatalf("summary caption on line %d, want 17\n--- body ---\n%s", got, body)
	}
}
```

Append to `internal/tui/events_feed_test.go`:

```go
func TestProjectPaneSplitHeightsThreeWay(t *testing.T) {
	cases := []struct {
		total, list, events, summary int
	}{
		{27, 8, 9, 10},
		{10, 3, 0, 7}, // events slot would be 3 (<4): collapses to old 30/70
		{2, 1, 0, 1},
		{1, 1, 0, 0},
		{0, 0, 0, 0},
	}
	for _, c := range cases {
		l, e, s := projectPaneSplitHeights(c.total)
		if l != c.list || e != c.events || s != c.summary {
			t.Errorf("split(%d) = (%d,%d,%d), want (%d,%d,%d)", c.total, l, e, s, c.list, c.events, c.summary)
		}
	}
}

func TestRecentEventsFeedRendersDigestLines(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	seedProject(t, m, "ATM", "Acme Task Manager")
	update(t, m, "s")
	seedTask(t, m, "ATM", "Fix the cache", "ATM:status:open")
	body := m.projects.View()
	mustContain(t, body, "Recent Events")
	mustContain(t, body, `created "Fix the cache"`)
	mustContain(t, body, "dev@clau") // testActor developer@claude:test
	mustContain(t, body, "●")
	if !regexp.MustCompile(`[0-9a-f]{7}`).MatchString(body) {
		t.Fatalf("feed shows no short event id\n--- body ---\n%s", body)
	}
	// Newest-first: the task creation renders above the project creation.
	taskIdx := strings.Index(body, `created "Fix the cache"`)
	projIdx := strings.Index(body, "project created")
	if projIdx >= 0 && taskIdx > projIdx {
		t.Fatalf("feed is not newest-first\n--- body ---\n%s", body)
	}
}

func TestRecentEventsFeedPlaceholders(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	seedProject(t, m, "ATM", "Acme Task Manager")
	// No selection: muted placeholder, no digest lines.
	mustContain(t, m.projects.View(), "select a project to see events")
}

func TestEventFeedLineDegradesOnNarrowPane(t *testing.T) {
	m := newTestModel(t)
	m.projectScope = "ATM"
	p := &m.projects
	now := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	e := core.LogEntry{
		ID:      "sha256:84fbf586004a",
		Action:  "comment.created",
		At:      now.Add(-time.Hour),
		Actor:   "developer@claude:test",
		Subject: core.Subject{Kind: "task", ID: "ATM-90171b"},
	}
	lanes := []rune{'●'}
	p.width = 46 // full budget: id and age both present
	line := p.eventFeedLine(e, lanes, 1, now, true)
	mustContain(t, line, "84fbf58")
	mustContain(t, line, "1h")
	p.width = 34 // below 36: id column drops, age stays
	line = p.eventFeedLine(e, lanes, 1, now, true)
	mustNotContain(t, line, "84fbf58")
	mustContain(t, line, "1h")
	p.width = 28 // below 30: age drops too
	line = p.eventFeedLine(e, lanes, 1, now, true)
	mustNotContain(t, line, "84fbf58")
	mustNotContain(t, line, "1h")
}
```

(`events_feed_test.go` needs `"regexp"` and `"strings"` added to its imports.)

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/tui -run 'TestProjectPaneSplitHeightsThreeWay|TestRecentEventsFeed|TestProjectsViewUsesThreeWaySplit' -v`
Expected: compile FAIL — `projectPaneSplitHeights` returns 2 values; `renderEventsFeed` undefined.

- [ ] **Step 3: Implement the 3-way split.** Replace `projectPaneSplitHeights` in `internal/tui/projects.go:67-88`:

```go
// projectPaneSplitHeights allocates the list view's vertical space three
// ways: project list ~30%, recent-events feed ~35%, summary the rest. An
// events slot under 4 rows (caption + 3 lines) is not worth rendering — it
// collapses to 0 and the pre-feed 30/70 list/summary split is restored.
func projectPaneSplitHeights(total int) (int, int, int) {
	if total <= 0 {
		return 0, 0, 0
	}
	if total == 1 {
		return 1, 0, 0
	}
	listH := total * 30 / 100
	if listH < 1 {
		listH = 1
	}
	eventsH := total * 35 / 100
	if eventsH < 4 {
		eventsH = 0
	}
	summaryH := total - listH - eventsH
	if summaryH < 1 {
		summaryH = 1
		listH = total - summaryH - eventsH
		if listH < 1 {
			listH = 1
			eventsH = 0
			summaryH = total - listH
		}
	}
	return listH, eventsH, summaryH
}
```

Update every caller (find them all: `grep -rn projectPaneSplitHeights internal/tui/`):
- `projects.go:213` and `:222` (the `]`/`[` handlers): `listH, _, _ := projectPaneSplitHeights(p.contentHeight)`
- `projects.go:495` (`renderList`) — see Step 4.
- Any test callers the grep surfaces: adjust to the 3-return signature, preserving their existing assertions about list height.

- [ ] **Step 4: Stack the feed section.** In `internal/tui/projects.go` replace `renderList` (lines 491-504):

```go
func (p *projectsModel) renderList() string {
	if len(p.list) == 0 {
		return p.renderEmpty()
	}
	listH, eventsH, summaryH := projectPaneSplitHeights(p.contentHeight)
	var parts []string
	if listH > 0 {
		parts = append(parts, padToHeight(p.renderListRows(listH), listH))
	}
	if eventsH > 0 {
		parts = append(parts, padToHeight(p.renderEventsFeed(eventsH), eventsH))
	}
	if summaryH > 0 {
		parts = append(parts, padToHeight(p.renderSummary(summaryH), summaryH))
	}
	return padToHeight(strings.Join(parts, "\n"), p.contentHeight)
}
```

Add the state fields to `projectsModel` (after `showHistory bool`, line 31):

```go
	// Recent Events feed subfocus state (list view, ATM-793b19). logsCursor
	// indexes the newest-first feed; it is clamped at render time.
	logsFocus  bool
	logsCursor int
```

- [ ] **Step 5: Implement the renderer.** Append to `internal/tui/events_feed.go` (add `"atm/internal/core"` is already imported; the file also needs `"github.com/charmbracelet/lipgloss"` now):

```go
// renderEventsFeed renders the Recent Events section: caption, then a
// windowed, newest-first page of digest lines for the selected project.
// Unfocused it is a tail pinned to the newest event; subfocused (L) the
// cursor row is highlighted and windowLines follows it.
func (p *projectsModel) renderEventsFeed(height int) string {
	lines := []string{dashboardLine(p.width, p.m.styles.HeaderLabel.Render("Recent Events  [L]ogs"))}
	muted := func(s string) string {
		return dashboardLine(p.width, p.m.styles.Muted.Render(s))
	}
	if p.m.projectScope == "" {
		lines = append(lines, muted("select a project to see events"))
		return padToHeight(strings.Join(lines, "\n"), height)
	}
	entries, err := p.m.store.ReadLogCached(p.m.projectScope)
	if err != nil && !core.IsIntegrity(err) {
		lines = append(lines, muted("events could not be loaded"))
		return padToHeight(strings.Join(lines, "\n"), height)
	}
	if len(entries) == 0 {
		lines = append(lines, muted("no events yet"))
		return padToHeight(strings.Join(lines, "\n"), height)
	}
	feed := make([]core.LogEntry, len(entries))
	for i, e := range entries {
		feed[len(entries)-1-i] = e // fold order is oldest-first; feed is newest-first
	}
	if p.logsCursor > len(feed)-1 {
		p.logsCursor = len(feed) - 1
	}
	cursor := 0
	if p.logsFocus {
		cursor = p.logsCursor
	}
	rows := height - 1 // caption
	start, end := windowLines(len(feed), cursor, rows)
	graph := eventGraphRows(feed)
	laneW := 1
	for i := start; i < end; i++ {
		if len(graph[i]) > laneW {
			laneW = len(graph[i])
		}
	}
	now := core.Now()
	for i := start; i < end; i++ {
		onCursor := p.logsFocus && i == p.logsCursor
		line := p.eventFeedLine(feed[i], graph[i], laneW, now, onCursor)
		if onCursor {
			line = p.m.styles.RowCursor.Render(line)
		}
		lines = append(lines, dashboardLine(p.width, line))
	}
	return padToHeight(strings.Join(lines, "\n"), height)
}

// eventFeedLine assembles one digest line. Column budget (spec): gutter,
// id(7, dim), subject(7), actor(8), message(flex), age(right, dim). The id
// column drops below 36 inner columns, then the age below 30. `plain`
// suppresses the inner dim styles so a cursor row can be re-styled whole.
func (p *projectsModel) eventFeedLine(e core.LogEntry, lanes []rune, laneW int, now time.Time, plain bool) string {
	dim := func(s string) string {
		if plain {
			return s
		}
		return p.m.styles.Muted.Render(s)
	}
	gutter := make([]rune, laneW)
	for i := range gutter {
		gutter[i] = ' '
	}
	copy(gutter, lanes)
	code := p.m.projectScope
	var b strings.Builder
	b.WriteString(string(gutter))
	b.WriteString(" ")
	used := laneW + 1
	if p.width >= 36 {
		b.WriteString(dim(fmt.Sprintf("%-7s", shortEventID(e.ID))))
		b.WriteString(" ")
		used += 8
	}
	b.WriteString(fmt.Sprintf("%-7s ", truncateRunes(eventFeedSubject(e, code), 7)))
	b.WriteString(fmt.Sprintf("%-8s ", truncateRunes(eventFeedActor(e.Actor), 8)))
	used += 17
	age := ""
	if p.width >= 30 {
		age = compactAge(e.At, now)
	}
	msgW := p.width - used - lipgloss.Width(age)
	if age != "" {
		msgW-- // the space before the age
	}
	if msgW < 4 {
		msgW = 4
	}
	b.WriteString(fmt.Sprintf("%-*s", msgW, truncateRunes(eventDigestMessage(e, code), msgW)))
	if age != "" {
		b.WriteString(" ")
		b.WriteString(dim(age))
	}
	return b.String()
}
```

> **Correction (post-launch review):** the renderEventsFeed sample code above
> contains a stale line: line 918 reads `graph := eventGraphRows(feed)` but the
> shipped code is `eventGraphRows(feed[:end])`. The truncation to the rendered
> window (by `end`) bounds row computation: row i's lane state depends only on
> entries 0..i, so computing the whole feed every frame was unnecessary work.

- [ ] **Step 6: Run the tests**

Run: `go test ./internal/tui -run 'TestProjectPaneSplitHeightsThreeWay|TestRecentEventsFeed|TestProjectsViewUsesThreeWaySplit' -v`
Expected: PASS

- [ ] **Step 7: Run the whole tui package** — the split change ripples into height-sensitive tests:

Run: `go test ./internal/tui`
Expected: PASS. If a pre-existing test fails, it is asserting the old 30/70 geometry (summary position, chart heights, "showing N-M" windows at a given size). Update ONLY such geometry assertions to the new list/events/summary heights (recompute via the split formula: list = h*30/100, events = h*35/100 if ≥4 else 0, summary = rest). Do not weaken non-geometry assertions.

- [ ] **Step 8: Commit**

```bash
git add internal/tui/projects.go internal/tui/events_feed.go internal/tui/events_feed_test.go internal/tui/app_test.go
git commit -m "feat(ATM-793b19): Recent Events feed section with 3-way pane split"
```

---

### Task 5: Subfocus interaction (L, j/k, paging, esc)

**Files:**
- Modify: `internal/tui/projects.go` (`handleListKey`, `statusHint`, the `s` handler)
- Modify: `internal/tui/events_feed.go` (`handleLogsKey`)
- Modify: `internal/tui/app.go:641-657` (global esc branch)
- Modify: `internal/tui/keymap.go:24-57` (reference row)
- Modify: `docs/superpowers/specs/2026-07-20-tui-recent-events-feed-design.md` (interaction deltas)
- Test: `internal/tui/events_feed_test.go`

**Interfaces:**
- Consumes: `logsFocus`, `logsCursor`, `projectPaneSplitHeights` (Task 4).
- Produces: `(p *projectsModel) handleLogsKey(k tea.KeyMsg) tea.Cmd`.

Two code-driven deltas from the spec, to record in the spec doc (see Step 6):
`g` is globally reserved as the plugin prefix (`app.go:632`), so the feed
pages with `[`/`]` instead of jumping with `g`; and `esc` is intercepted by
the app-level esc branch, so leaving the feed on esc is wired there.

- [ ] **Step 1: Write the failing tests** — append to `internal/tui/events_feed_test.go`:

```go
func TestRecentEventsSubfocusScrolls(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	seedProject(t, m, "ATM", "Acme Task Manager")
	update(t, m, "s")
	for i := 0; i < 12; i++ {
		seedTask(t, m, "ATM", fmt.Sprintf("Task %02d", i))
	}
	update(t, m, "L")
	if !m.projects.logsFocus {
		t.Fatal("L should focus the events feed")
	}
	update(t, m, "j")
	update(t, m, "j")
	if m.projects.logsCursor != 2 {
		t.Fatalf("cursor = %d, want 2", m.projects.logsCursor)
	}
	update(t, m, "]")
	if m.projects.logsCursor <= 2 {
		t.Fatalf("] should page the feed cursor, got %d", m.projects.logsCursor)
	}
	listCursor := m.projects.cursor
	update(t, m, "esc")
	if m.projects.logsFocus {
		t.Fatal("esc should leave the events feed")
	}
	update(t, m, "j")
	if m.projects.cursor != listCursor+1 {
		t.Fatal("after esc, j should drive the project list again")
	}
	update(t, m, "L")
	if !m.projects.logsFocus || m.projects.logsCursor != 0 {
		t.Fatal("re-entering the feed should reset the cursor to newest")
	}
	update(t, m, "L")
	if m.projects.logsFocus {
		t.Fatal("L should also toggle the feed subfocus off")
	}
}

func TestRecentEventsFocusRequiresSelection(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	seedProject(t, m, "ATM", "Acme Task Manager")
	update(t, m, "L")
	if m.projects.logsFocus {
		t.Fatal("L without a selected project must not focus the feed")
	}
}

func TestRecentEventsStatusHintFollowsSubfocus(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	seedProject(t, m, "ATM", "Acme Task Manager")
	update(t, m, "s")
	if !strings.Contains(m.projects.statusHint(), "[L]ogs") {
		t.Fatalf("list hint should advertise [L]ogs: %q", m.projects.statusHint())
	}
	update(t, m, "L")
	hint := m.projects.statusHint()
	if !strings.Contains(hint, "[j/k]") || !strings.Contains(hint, "[L/Esc]back") {
		t.Fatalf("feed hint = %q", hint)
	}
}
```

(This batch uses `fmt.Sprintf` — add `"fmt"` to `events_feed_test.go`'s imports.)

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/tui -run 'TestRecentEventsSubfocus|TestRecentEventsFocusRequires|TestRecentEventsStatusHint' -v`
Expected: FAIL — `L` does nothing, hints unchanged.

- [ ] **Step 3: Implement the routing.** In `internal/tui/projects.go`:

At the top of `handleListKey` (line 201):

```go
func (p *projectsModel) handleListKey(k tea.KeyMsg) tea.Cmd {
	if p.logsFocus {
		return p.handleLogsKey(k)
	}
	switch k.String() {
```

Add an `L` case alongside the other list keys:

```go
	case "L":
		if p.m.projectScope == "" {
			p.m.showToast("select a project first")
			return nil
		}
		p.logsFocus = true
		p.logsCursor = 0
```

In the `s` handler, reset the feed with the rest of the project-switch state (next to `p.m.capability.current = ""`, line 245):

```go
			p.logsFocus = false
			p.logsCursor = 0
```

Update `statusHint` (line 992):

```go
	case pViewList:
		if p.logsFocus {
			return "[j/k]scroll [[/]]page [L/Esc]back"
		}
		if len(p.list) == 0 {
			return "[a]add [p]ersona"
		}
		return "[a]dd [s]elect [Enter]detail [L]ogs [x]remove [P]ersona [p]new"
```

Append `handleLogsKey` to `internal/tui/events_feed.go` (add the bubbletea import: `tea "github.com/charmbracelet/bubbletea"`):

```go
// handleLogsKey drives the Recent Events feed while it holds the pane's
// subfocus. The cursor is clamped against the feed length at render time
// (renderEventsFeed), so the handler only moves it. Note: esc is ALSO
// handled at the app level (handleKey's esc branch) because it never
// reaches pane handlers; the case here documents the intended pair.
func (p *projectsModel) handleLogsKey(k tea.KeyMsg) tea.Cmd {
	_, eventsH, _ := projectPaneSplitHeights(p.contentHeight)
	page := eventsH - 1
	if page < 1 {
		page = 1
	}
	switch k.String() {
	case "j", "down":
		p.logsCursor++
	case "k", "up":
		if p.logsCursor > 0 {
			p.logsCursor--
		}
	case "]":
		p.logsCursor += page
	case "[":
		p.logsCursor -= page
		if p.logsCursor < 0 {
			p.logsCursor = 0
		}
	case "L", "esc":
		p.logsFocus = false
	}
	return nil
}
```

In `internal/tui/app.go`, extend the esc branch (line 641) so esc leaves the feed before the detail/list logic runs:

```go
	if k.String() == "esc" {
		if m.focused == paneProjects && m.projects.view == pViewList && m.projects.logsFocus {
			m.projects.logsFocus = false
			return nil
		}
		if m.focused == paneProjects && m.projects.view == pViewDetail {
```

In `internal/tui/keymap.go`, add a row after the `"P"` entry (line 50):

```go
	{"L", "focus recent-events feed (list)", "-", "-", "-"},
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/tui -run 'TestRecentEventsSubfocus|TestRecentEventsFocusRequires|TestRecentEventsStatusHint' -v`
Expected: PASS

Run: `go test ./internal/tui`
Expected: PASS (a hint-asserting test may need the new `[L]ogs` token added; `TestStatusLineHintsFollowFocusedPane` is the likely one).

- [ ] **Step 5: Amend the spec** — in `docs/superpowers/specs/2026-07-20-tui-recent-events-feed-design.md`, Interaction section: replace the `g` jump with `[`/`]` paging (`g` is the global plugin prefix in the TUI) and note esc-exit is wired in the app-level esc branch. In the Line format section, note the graph gutter draws parallel `│` lanes without diagonal junction glyphs in this iteration.

- [ ] **Step 6: Commit**

```bash
git add internal/tui/projects.go internal/tui/events_feed.go internal/tui/app.go internal/tui/keymap.go internal/tui/events_feed_test.go docs/superpowers/specs/2026-07-20-tui-recent-events-feed-design.md
git commit -m "feat(ATM-793b19): L subfocus scrolling for the Recent Events feed"
```

---

### Task 6: Full verification and changelog

**Files:**
- Modify: `CHANGELOG.md` (Unreleased → feat)

- [ ] **Step 1: Run the full gate**

Run: `make verify`
Expected: PASS — all packages, CLI goldens byte-identical. Fix any fallout before proceeding (geometry-only test updates per Task 4 Step 7 rules).

- [ ] **Step 2: Changelog** — add under `## Unreleased` / `### feat` in `CHANGELOG.md`:

```markdown
- ATM-793b19: Recent Events feed in the TUI Projects pane — a git-log-style digest of the selected project's event stream (commit-graph gutter, short event ids, per-action wording), with `L` subfocus scrolling.
```

- [ ] **Step 3: Commit**

```bash
git add CHANGELOG.md
git commit -m "docs(ATM-793b19): changelog for the Recent Events feed"
```

- [ ] **Step 4: Ledger** — journal completion on the task:

```bash
atm task comment add --task ATM-793b19 --label ATM:comment:progress --body "Recent Events feed implemented per docs/superpowers/plans/2026-07-20-tui-recent-events-feed.md: LogEntry.ID/Parents read-model addition, events_feed.go digest+graph helpers, 3-way pane split, L subfocus. make verify green."
```

---

# Revision 2 — boxed feed and modeless navigation

> Tasks 1-6 above are COMPLETE and merged into the branch. Tasks 7-8 below
> implement Revision 2 of the design spec
> (`docs/superpowers/specs/2026-07-20-tui-recent-events-feed-design.md`,
> section "Revision 2 — boxed feed and modeless navigation"), which
> supersedes the Interaction section and decisions 1 and 4 of that spec.
>
> Read the spec's Revision 2 section before starting either task.

## Revision 2 Global Constraints

Everything in the original Global Constraints still binds. Additionally:

- `renderChartBox` in `internal/tui/projects.go` MUST NOT be modified. Both
  alignment requirements are satisfied by the body the feed hands it. The
  existing persona and stripe charts must render byte-identically.
- `renderEventsFeed` MUST stay pure with respect to model state — no writes
  to any model field from the render path. Clamping lives in the key handler.
- Strict 1:1 still holds: one event renders as exactly one line.
- The box title is exactly `Recent Events  [Shift-↑↓]` (two spaces before the
  bracket, matching `activity by persona  [P]expand`).
- `↑` is U+2191, `↓` is U+2193. `●` U+25CF, `│` U+2502, `−` U+2212, `–` U+2013.

---

### Task 7: Render the feed as an aligned bordered box

**Files:**
- Modify: `internal/tui/events_feed.go` (`renderEventsFeed`, `eventFeedLine` sizing)
- Test: `internal/tui/events_feed_test.go`

**Interfaces:**
- Consumes: `renderChartBox(title, body string, maxLines int) string`,
  `chartBoxInnerWidth(width int) int` — both existing in `internal/tui/projects.go`, unchanged.
- Produces: `renderEventsFeed(height int) string` rendering a bordered box
  whose edges align with the summary chart boxes.

**Context the implementer needs:** `renderChartBox` center-pads every body
line and top-pads a short body. The feed needs left-and-top alignment. Both
become no-ops if the body is exactly `chartBoxInnerWidth(p.width)` wide and
exactly `maxLines - 2` lines tall. Do not change `renderChartBox` to
achieve this.

- [ ] **Step 1: Write the failing tests.** Add to `internal/tui/events_feed_test.go`:

```go
func TestEventsFeedRendersAsBoxAlignedWithCharts(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(200, 40)
	seedProject(t, m, "ATM", "Acme Task Manager")
	update(t, m, "s")
	seedTask(t, m, "ATM", "Fix cache")
	body := m.projects.View()
	mustContain(t, body, "Recent Events  [Shift-↑↓]")
	// The events box and the persona chart box must share left and right edges.
	lines := strings.Split(body, "\n")
	edgesOf := func(marker string) (int, int) {
		t.Helper()
		for i, line := range lines {
			if strings.Contains(line, marker) {
				plain := stripANSI(line)
				return strings.Index(plain, "╭"), strings.LastIndex(plain, "╮")
			}
			_ = i
		}
		t.Fatalf("no box top border containing %q\n--- body ---\n%s", marker, body)
		return -1, -1
	}
	el, er := edgesOf("Recent Events")
	pl, pr := edgesOf("activity by persona")
	if el != pl || er != pr {
		t.Fatalf("events box edges (%d,%d) != persona box edges (%d,%d)\n--- body ---\n%s", el, er, pl, pr, body)
	}
}

func TestEventsFeedBoxBodyIsLeftAndTopAligned(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(200, 40)
	seedProject(t, m, "ATM", "Acme Task Manager")
	update(t, m, "s")
	seedTask(t, m, "ATM", "Fix cache")
	lines := strings.Split(m.projects.View(), "\n")
	top := -1
	for i, line := range lines {
		if strings.Contains(line, "Recent Events") {
			top = i
			break
		}
	}
	if top < 0 {
		t.Fatal("no events box")
	}
	// Top-aligned: the row directly under the top border carries an event.
	first := stripANSI(lines[top+1])
	if !strings.Contains(first, "●") {
		t.Fatalf("first body row has no event glyph (not top-aligned): %q", first)
	}
	// Left-aligned: the graph glyph sits immediately after the left border,
	// not pushed rightward by centering.
	bar := strings.Index(first, "│")
	dot := strings.Index(first, "●")
	if dot-bar > 2 {
		t.Fatalf("event glyph is %d cols from the left border (centered, not left-aligned): %q", dot-bar, first)
	}
}
```

There is no shared `stripANSI` helper yet, but the regex already exists
inline at `internal/tui/app_test.go:1144`
(`regexp.MustCompile("\x1b\\[[0-9;]*m")`). Extract it into a package-level
`stripANSI(s string) string` helper next to `mustContain` in
`app_test.go`, and have that existing caller use it too — do not add a
second copy. (Note `internal/tui/actors_test.go:107` uses a narrower
`strings.NewReplacer` for two specific codes; leave it alone.)

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/tui -run 'TestEventsFeedRendersAsBox|TestEventsFeedBoxBody' -v`
Expected: FAIL — the feed renders a bare caption, so no `╭` top border containing "Recent Events" exists.

- [ ] **Step 3: Implement.** In `renderEventsFeed`:
  - Compute `innerW := chartBoxInnerWidth(p.width)` and size every feed line
    to `innerW` (pass it down to `eventFeedLine` instead of `p.width`).
  - Build exactly `height - 2` body lines: the windowed event lines, then
    blank lines padded to `innerW` to fill the remainder.
  - Return `p.renderChartBox("Recent Events  [Shift-↑↓]", strings.Join(body, "\n"), height)`.
  - The placeholder states ("could not be loaded", "no events yet") render as
    a single body line inside the box, padded to `innerW`, not as bare lines.
  - Keep the render path free of model-state writes.

  Note `eventFeedLine`'s degradation thresholds (`feedIDMinWidth`,
  `feedAgeMinWidth`) now compare against the box inner width. Do not change
  their values.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/tui -run 'TestEventsFeedRendersAsBox|TestEventsFeedBoxBody' -v`
Expected: PASS

- [ ] **Step 5: Run the package and fix geometry fallout**

Run: `go test ./internal/tui`
Expected: PASS. Existing feed tests assert against pane-width lines and a
`Recent Events  [L]ogs` caption; update ONLY assertions encoding the old
unboxed geometry or the old caption text. Do not weaken a non-geometry
assertion — if one fails, the change is wrong, not the test.

- [ ] **Step 6: Commit**

```bash
git add internal/tui/events_feed.go internal/tui/events_feed_test.go internal/tui/app_test.go
git commit -m "feat(ATM-793b19): render the events feed as an aligned bordered box"
```

---

### Task 8: Modeless Shift+arrow navigation, delete the subfocus mode

**Files:**
- Modify: `internal/tui/events_feed.go` (`handleLogsKey` → scroll handler, `logsCursor` → `logsOffset`)
- Modify: `internal/tui/projects.go` (state field, routing, `L` entry, status hint, `SetSize` guard, `confirmYes` reset)
- Modify: `internal/tui/app.go` (remove the feed's esc branch)
- Modify: `internal/tui/keymap.go` (drop the `L` row, fill the Projects column on two Shift rows)
- Test: `internal/tui/events_feed_test.go`

**Interfaces:**
- Produces: `projectsModel.logsOffset int` replacing `logsCursor`; no `logsFocus`.
- Removed: `logsFocus`, `handleLogsKey`'s modal early-return, the app-level feed esc branch.

**Context:** the Tasks pane already does exactly this pattern — see
`internal/tui/tasks_list.go`, where `case "j", "down"` moves the task list
and `case "shift+up", "shift+down"` moves the board thumbnail's chart
cursor, in one switch with no mode flag. Mirror it.

- [ ] **Step 1: Write the failing tests.** Add to `internal/tui/events_feed_test.go`:

```go
func TestShiftArrowsScrollFeedWhilePlainKeysDriveList(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(200, 40)
	seedProject(t, m, "ATM", "Acme Task Manager")
	seedProject(t, m, "ZZZ", "Second")
	update(t, m, "s")
	for i := 0; i < 30; i++ {
		seedTask(t, m, "ATM", fmt.Sprintf("Task %02d", i))
	}
	listBefore := m.projects.cursor
	update(t, m, "shift+down")
	update(t, m, "shift+down")
	if m.projects.logsOffset != 2 {
		t.Fatalf("shift+down x2: logsOffset = %d, want 2", m.projects.logsOffset)
	}
	if m.projects.cursor != listBefore {
		t.Fatalf("shift+down moved the project list cursor (%d -> %d)", listBefore, m.projects.cursor)
	}
	update(t, m, "j")
	if m.projects.cursor != listBefore+1 {
		t.Fatalf("plain j should move the list cursor, got %d", m.projects.cursor)
	}
	if m.projects.logsOffset != 2 {
		t.Fatalf("plain j moved the feed offset to %d", m.projects.logsOffset)
	}
	update(t, m, "shift+up")
	if m.projects.logsOffset != 1 {
		t.Fatalf("shift+up: logsOffset = %d, want 1", m.projects.logsOffset)
	}
}

func TestShiftLeftRightPageFeedAndOffsetClamps(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(200, 40)
	seedProject(t, m, "ATM", "Acme Task Manager")
	update(t, m, "s")
	for i := 0; i < 30; i++ {
		seedTask(t, m, "ATM", fmt.Sprintf("Task %02d", i))
	}
	update(t, m, "shift+right")
	paged := m.projects.logsOffset
	if paged <= 1 {
		t.Fatalf("shift+right should page by more than one line, got %d", paged)
	}
	for i := 0; i < 200; i++ {
		update(t, m, "shift+right")
	}
	if m.projects.logsOffset >= m.projects.feedLen() {
		t.Fatalf("offset %d ran past the feed length %d", m.projects.logsOffset, m.projects.feedLen())
	}
	for i := 0; i < 300; i++ {
		update(t, m, "shift+left")
	}
	if m.projects.logsOffset != 0 {
		t.Fatalf("shift+left to the top: logsOffset = %d, want 0", m.projects.logsOffset)
	}
}

func TestFeedOffsetResetsOnProjectSwitch(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(200, 40)
	seedProject(t, m, "ATM", "Acme Task Manager")
	seedProject(t, m, "ZZZ", "Second")
	update(t, m, "s")
	for i := 0; i < 20; i++ {
		seedTask(t, m, "ATM", fmt.Sprintf("Task %02d", i))
	}
	update(t, m, "shift+down")
	update(t, m, "shift+down")
	if m.projects.logsOffset == 0 {
		t.Fatal("setup: offset should be non-zero before the switch")
	}
	update(t, m, "j")
	update(t, m, "s") // select ZZZ
	if m.projects.logsOffset != 0 {
		t.Fatalf("project switch should reset logsOffset, got %d", m.projects.logsOffset)
	}
}

func TestNoSubfocusModeRemains(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(200, 40)
	seedProject(t, m, "ATM", "Acme Task Manager")
	update(t, m, "s")
	seedTask(t, m, "ATM", "Fix cache")
	// L is no longer a feed binding: it must not swallow subsequent list keys.
	update(t, m, "L")
	before := m.projects.cursor
	seedProject(t, m, "ZZZ", "Second")
	update(t, m, "j")
	if m.projects.cursor != before+1 {
		t.Fatalf("after L, plain j must still drive the project list (cursor %d -> %d)", before, m.projects.cursor)
	}
	mustNotContain(t, m.projects.statusHint(), "[L]ogs")
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/tui -run 'TestShiftArrows|TestShiftLeftRight|TestFeedOffsetResets|TestNoSubfocusMode' -v`
Expected: compile FAIL — `logsOffset` does not exist.

- [ ] **Step 3: Implement.**
  - Rename the field `logsCursor` → `logsOffset` in `projects.go`; delete `logsFocus`.
  - In `events_feed.go`, replace `handleLogsKey` with a scroll handler taking
    a direction and a magnitude (1 line, or a page), clamping `logsOffset` to
    `[0, max(0, feedLen()-1)]`. Keep `feedLen()` as-is.

> **Correction (post-launch review):** the clamp range above is now
> known-stale and would mislead a reader implementing from this plan
> directly. `[0, max(0, feedLen()-1)]` ignores the visible row count: with a
> short feed in a tall box, one shift+down would push the newest event off
> the top and render nothing but blank rows below it, and at maximum scroll
> on any overflowing feed it strands one event above a column of blanks. The
> design spec (R2-3) is authoritative and requires clamping against the feed
> length *and* the visible row count. The shipped code clamps to
> `[0, max(0, feedLen()-rows)]`, where `rows` is the same visible-row count
> `renderEventsFeed` windows by (`eventsFeedVisibleRows`, in
> `internal/tui/events_feed.go`) — the box height minus its two border rows,
> floored at 1, the same arithmetic `eventsPageSize` already applied to its
> page magnitude — so the render window, the page magnitude, and the scroll
> clamp all fold through one function and cannot drift apart.
  - In `handleListKey` (`projects.go`), delete the `logsFocus` early-return
    and the `L` case; add `case "shift+up"`, `case "shift+down"`,
    `case "shift+left"`, `case "shift+right"` calling the scroll handler.
    Page magnitude is visible rows − 1, derived from
    `projectPaneSplitHeights(p.contentHeight)`'s events height minus the two
    border rows.
  - `renderEventsFeed` windows from `logsOffset` with no cursor row and no
    `RowCursor` highlight.
  - Reset `logsOffset` to 0 on project switch (the `s` handler) and where
    `confirmYes` clears `m.projectScope`; drop the `logsFocus` resets there
    and in `SetSize`.
  - Remove the feed's esc branch from `app.go`.
  - Status hint: drop `[L]ogs`.
  - `keymap.go`: delete the `L` row; set the Projects column to
    `scroll events feed` on the `Shift+Up/Down` row and `page events feed` on
    the `Shift+Right/Left` row.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/tui -run 'TestShiftArrows|TestShiftLeftRight|TestFeedOffsetResets|TestNoSubfocusMode' -v`
Expected: PASS

- [ ] **Step 5: Run the package**

Run: `go test ./internal/tui`
Expected: PASS. The Task 5 subfocus tests (`TestRecentEventsSubfocusScrolls`,
`TestRecentEventsFocusRequiresSelection`, `TestRecentEventsStatusHintFollowsSubfocus`,
`TestRecentEventsCursorClampsToFeedLength`, the resize-collapse test, and the
scroll-render test) assert a mode that no longer exists. DELETE the ones whose
subject is gone; REWRITE the ones whose subject survives in new form (feed
scrolling still exists; it is now driven by shift+arrows and an offset). Do
not leave a test asserting the old mode.

- [ ] **Step 6: Commit**

```bash
git add internal/tui/events_feed.go internal/tui/projects.go internal/tui/app.go internal/tui/keymap.go internal/tui/events_feed_test.go
git commit -m "feat(ATM-793b19): modeless shift+arrow feed navigation, drop L subfocus"
```

---

### Task 9: Verify, changelog, ledger

- [ ] **Step 1:** Run `make verify`. Expected PASS. Do not patch tests to go green; report a real failure instead.

- [ ] **Step 2:** Update the CHANGELOG entry added in Task 6 — it currently
      says the feed has "`L` subfocus scrolling", which is no longer true.
      Reword to describe the boxed feed and `Shift`+arrow scrolling. Keep it
      one entry; do not add a second.

- [ ] **Step 3: Commit**

```bash
git add CHANGELOG.md
git commit -m "docs(ATM-793b19): changelog reflects boxed feed and shift-arrow scrolling"
```

- [ ] **Step 4: Ledger**

```bash
atm task comment add --task ATM-793b19 --label ATM:comment:progress --body "Revision 2 shipped: events feed is a bordered box aligned with the summary chart boxes, and L subfocus is replaced by modeless Shift+arrow navigation (Shift+up/down scroll a line, Shift+left/right page). No focus mode, no cursor row - pure viewport offset. make verify green."
```
