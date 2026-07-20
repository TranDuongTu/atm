# Store Stats Status Bar Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the status bar's `STORE:`/`SELECTED:`/`theme:` text with a compact store-stats cluster (format version · event count · size), and add a fixed `[?]help [C]conv [T]theme` key cluster plus the app version on the right.

**Architecture:** A new `StoreStats()` method crosses the hexagonal seam on `core.MaintenanceService`; the concrete store delegates to a new read-only helper on the eventlog `Engine` that walks `projects/<CODE>/` log files. The TUI caches the result on the `Model` during `refreshAll()` and renders it in `renderStatusLine` — `View()` never touches the filesystem.

**Tech Stack:** Go, Bubble Tea / lipgloss TUI, standard `testing` package. Spec: `docs/superpowers/specs/2026-07-20-store-stats-status-bar-design.md`.

## Global Constraints

- `StoreStats()` takes **no locks** and must never fail a refresh: TUI ignores its error and keeps the last good value.
- Event count = number of `'\n'` bytes in each log file (committed lines only); missing file contributes zero, not an error.
- `Version` is `"v1"`, `"v2"`, or `"mixed"` (per-project formats disagree); with zero projects it is the store's `ActiveFormat`.
- Size formatting is adaptive: `<n> KB` under 1 MiB, `<n.1f> MB` at or above (`0 KB`, `842 KB`, `1.2 MB`).
- Key *behavior* of `?`, `C`, `T` is unchanged — only status-bar text moves.
- Stage explicit paths in git (never `git add -A`); commit style `feat(ATM-789528): …` / `test(ATM-789528): …`.
- Module import root is `atm` (e.g. `atm/internal/core`).

---

### Task 1: `core.StoreStats` seam + engine/store implementation

**Files:**
- Modify: `internal/core/service.go` (add `StoreStats` type; extend `MaintenanceService`)
- Create: `internal/store/eventlog/stats.go`
- Modify: `internal/store/store.go` (delegation, next to `StorePath` at ~line 225)
- Test: `internal/store/store_stats_test.go` (new)

**Interfaces:**
- Consumes: `Engine.ProjectCodesOnDisk()`, `Engine.ProjectFormat(code)`, `Engine.EventsV2Path/LogPath(code)`, `Engine.ReadStoreMeta()` — all existing (`internal/store/eventlog/meta.go`, `engine.go`).
- Produces: `core.StoreStats{SizeBytes int64; EventCount int; Version string}` and `(*Store).StoreStats() (core.StoreStats, error)` — Task 2 calls this through `core.Service`.

- [ ] **Step 1: Write the failing tests**

Create `internal/store/store_stats_test.go`:

```go
package store

import (
	"os"
	"testing"

	"atm/internal/store/eventlog"
)

// A fresh store has no projects: stats are zero and Version falls back to
// the store's ActiveFormat (v1 on a store.json materialized by Init with
// no explicit active_format).
func TestStoreStatsEmptyStore(t *testing.T) {
	s := newTestStore(t)
	st, err := s.StoreStats()
	if err != nil {
		t.Fatal(err)
	}
	if st.SizeBytes != 0 || st.EventCount != 0 {
		t.Fatalf("empty store stats = %+v, want zeros", st)
	}
	if st.Version != "v1" {
		t.Fatalf("empty store Version = %q, want v1 (ActiveFormat fallback)", st.Version)
	}
}

// Projects are born v2 on a fresh store: every mutation appends one line to
// events.v2.jsonl, so EventCount is the file's line count and SizeBytes its
// byte size.
func TestStoreStatsCountsV2Events(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateProject("ATM", "x", testActor); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateTask("ATM", "one", "", nil, testActor); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateTask("ATM", "two", "", nil, testActor); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(s.eng.EventsV2Path("ATM"))
	if err != nil {
		t.Fatal(err)
	}
	wantCount := 0
	for _, b := range raw {
		if b == '\n' {
			wantCount++
		}
	}
	if wantCount == 0 {
		t.Fatal("test setup wrote no events")
	}
	st, err := s.StoreStats()
	if err != nil {
		t.Fatal(err)
	}
	if st.EventCount != wantCount {
		t.Errorf("EventCount = %d, want %d", st.EventCount, wantCount)
	}
	if st.SizeBytes != int64(len(raw)) {
		t.Errorf("SizeBytes = %d, want %d", st.SizeBytes, len(raw))
	}
	if st.Version != "v2" {
		t.Errorf("Version = %q, want v2", st.Version)
	}
}

// Two projects whose effective formats disagree report Version "mixed".
// Flipping BBB's format entry to v1 also exercises the missing-file path:
// BBB has no log.jsonl, which must contribute zero, not an error.
func TestStoreStatsMixedFormats(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateProject("AAA", "a", testActor); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateProject("BBB", "b", testActor); err != nil {
		t.Fatal(err)
	}
	if err := s.eng.SetProjectFormat("BBB", eventlog.StoreFormatV1); err != nil {
		t.Fatal(err)
	}
	st, err := s.StoreStats()
	if err != nil {
		t.Fatal(err)
	}
	if st.Version != "mixed" {
		t.Errorf("Version = %q, want mixed", st.Version)
	}
	if st.EventCount == 0 || st.SizeBytes == 0 {
		t.Errorf("AAA's v2 events should still count, got %+v", st)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/store/ -run TestStoreStats -v`
Expected: compile error — `s.StoreStats undefined (type *Store has no field or method StoreStats)`

- [ ] **Step 3: Add the core type and interface method**

In `internal/core/service.go`, immediately above `MaintenanceService` (~line 100):

```go
// StoreStats is the store-wide display summary the TUI status bar renders:
// total event-log bytes and lines across all projects, and the storage
// format version ("v1", "v2", or "mixed" when per-project formats disagree).
type StoreStats struct {
	SizeBytes  int64
	EventCount int
	Version    string
}
```

and extend `MaintenanceService`:

```go
type MaintenanceService interface {
	Init(storePath string) error
	StorePath() string
	StoreStats() (StoreStats, error)
	Now() time.Time
}
```

- [ ] **Step 4: Implement the engine helper**

Create `internal/store/eventlog/stats.go`:

```go
package eventlog

import (
	"bytes"
	"os"

	"atm/internal/core"
)

// StoreStats sums event-log size and line count across every project on
// disk and derives the store-wide format version. It is a read-only,
// advisory display path: no locks are taken (a torn read is corrected on
// the next refresh), and a missing log file contributes zero. Committed
// events are newline-terminated lines, so counting '\n' bytes never counts
// an uncommitted partial tail.
func (e *Engine) StoreStats() (core.StoreStats, error) {
	var st core.StoreStats
	codes, err := e.ProjectCodesOnDisk()
	if err != nil {
		return st, err
	}
	formats := map[StoreFormat]bool{}
	for _, code := range codes {
		f, err := e.ProjectFormat(code)
		if err != nil {
			return st, err
		}
		formats[f] = true
		path := e.LogPath(code)
		if f == StoreFormatV2 {
			path = e.EventsV2Path(code)
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return st, err
		}
		st.SizeBytes += int64(len(raw))
		st.EventCount += bytes.Count(raw, []byte{'\n'})
	}
	switch len(formats) {
	case 0:
		m, err := e.ReadStoreMeta()
		if err != nil {
			return st, err
		}
		st.Version = string(m.ActiveFormat)
	case 1:
		for f := range formats {
			st.Version = string(f)
		}
	default:
		st.Version = "mixed"
	}
	return st, nil
}
```

- [ ] **Step 5: Delegate from the store facade**

In `internal/store/store.go`, next to `StorePath` (~line 225):

```go
func (s *Store) StoreStats() (core.StoreStats, error) { return s.eng.StoreStats() }
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/store/ -run TestStoreStats -v`
Expected: PASS (3 tests)

Then the full packages touched: `go test ./internal/core/... ./internal/store/...`
Expected: PASS (no existing test asserts on `MaintenanceService`'s method set; `*Store` is the only `core.Service` implementation, pinned by `var _ core.Service = (*Store)(nil)`)

- [ ] **Step 7: Commit**

```bash
git add internal/core/service.go internal/store/eventlog/stats.go internal/store/store.go internal/store/store_stats_test.go
git commit -m "feat(ATM-789528): add StoreStats to the maintenance seam"
```

---

### Task 2: Status bar left side — stats cluster replaces STORE/SELECTED/theme

**Files:**
- Modify: `internal/tui/app.go` (`Model` struct ~line 67, `refreshAll` ~line 329, `renderStatusLine` ~line 847)
- Test: `internal/tui/app_test.go` (update `TestThemeCycleKeyUpdatesThemeAndStatus` ~line 383; add new tests)

**Interfaces:**
- Consumes: `core.StoreStats` and `m.store.StoreStats()` from Task 1; existing styles `m.styles.StatusLabel`, `m.styles.Status`.
- Produces: `m.storeStats core.StoreStats` field and `formatSize(int64) string` helper — Task 3 renders around them but does not change them.

- [ ] **Step 1: Write the failing tests**

In `internal/tui/app_test.go`, add:

```go
func TestFormatSize(t *testing.T) {
	cases := []struct {
		b    int64
		want string
	}{
		{0, "0 KB"},
		{512, "0 KB"},
		{1024, "1 KB"},
		{862208, "842 KB"},
		{1 << 20, "1.0 MB"},
		{1258291, "1.2 MB"},
	}
	for _, c := range cases {
		if got := formatSize(c.b); got != c.want {
			t.Errorf("formatSize(%d) = %q, want %q", c.b, got, c.want)
		}
	}
}

func TestStatusLineShowsStoreStatsNotPathSelectionOrTheme(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme Task Manager")
	seedTask(t, m, "ATM", "one", "ATM:status:open")
	m.refreshAll()
	if m.storeStats.EventCount == 0 {
		t.Fatal("refreshAll should populate storeStats from the seeded events")
	}
	line := m.renderStatusLine()
	mustContain(t, line, "⛃ v2")
	mustContain(t, line, "events")
	mustContain(t, line, "KB")
	for _, gone := range []string{"STORE:", "SELECTED:", "theme:"} {
		if strings.Contains(line, gone) {
			t.Errorf("status line still contains %q:\n%s", gone, line)
		}
	}
}
```

and change `TestThemeCycleKeyUpdatesThemeAndStatus` (~line 383) to stop asserting status-bar text (the segment is being removed) while still pinning that `T` cycles:

```go
func TestThemeCycleKeyUpdatesTheme(t *testing.T) {
	m := newTestModel(t)
	if m.themeName != themeGraphite {
		t.Fatalf("initial themeName = %q want %q", m.themeName, themeGraphite)
	}
	update(t, m, "T")
	if m.themeName != themeLight {
		t.Fatalf("after T: themeName = %q want %q", m.themeName, themeLight)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/tui/ -run 'TestFormatSize|TestStatusLineShowsStoreStats' -v`
Expected: compile error — `undefined: formatSize` / `m.storeStats undefined`

- [ ] **Step 3: Implement**

In `internal/tui/app.go`:

1. Add the field to `Model` (next to `store`/`storeSet`, ~line 67):

```go
	store      core.Service
	storeSet   bool
	storeStats core.StoreStats
```

2. Populate it in `refreshAll` (~line 329) — errors keep the previous value:

```go
func (m *Model) refreshAll() {
	m.projects.refresh()
	m.tasks.refresh()
	m.boards.refresh()
	m.help.refresh()
	if st, err := m.store.StoreStats(); err == nil {
		m.storeStats = st
	}
	m.lastRefreshAt = core.Now()
}
```

3. In `renderStatusLine` (~line 847), replace the three leading segments — delete the `STORE:`, `SELECTED:`, and `theme:` appends — with:

```go
	var parts []string
	parts = append(parts, m.styles.StatusLabel.Render("⛃ "+m.storeStats.Version)+
		m.styles.Status.Render(fmt.Sprintf(" · %d events · %s", m.storeStats.EventCount, formatSize(m.storeStats.SizeBytes))))
```

(`fmt` is already imported in app.go; if not, add it.)

4. Add the size formatter next to `shortenPath` (~line 922):

```go
// formatSize renders a byte count for the status bar: whole KB under 1 MiB
// (a flat "0.0 MB" for small stores reads as broken), one-decimal MB above.
func formatSize(b int64) string {
	const mb = 1 << 20
	if b < mb {
		return fmt.Sprintf("%d KB", b/1024)
	}
	return fmt.Sprintf("%.1f MB", float64(b)/float64(mb))
}
```

Do NOT remove `shortenPath` even if now unused by the status line — first check `grep -n "shortenPath" internal/tui/*.go`; if the status line was its only caller, delete the function AND its tests in the same commit (dead code is worse than a smaller diff).

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/tui/`
Expected: PASS — including `TestStatusLineHintsFollowFocusedPane` and `refresh_tick_test.go` (unchanged behavior), and the renamed `TestThemeCycleKeyUpdatesTheme`.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/app.go internal/tui/app_test.go
git commit -m "feat(ATM-789528): status bar leads with store stats"
```

---

### Task 3: Status bar right side — key cluster + app version; de-duplicate pane hints

**Files:**
- Modify: `internal/tui/app.go` (`renderStatusLine` right side; `Model.statusHint` fallback ~line 830)
- Modify: `internal/tui/projects.go:983-993`, `internal/tui/tasks_list.go:583-588`, `internal/tui/labels.go:1636-1649`
- Test: `internal/tui/app_test.go` (new tests), `internal/tui/tasks_test.go:386` (update expected hint)

**Interfaces:**
- Consumes: `version.Version` from `atm/internal/version`; styles `m.styles.KeyMenu`, `m.styles.KeyMenuDim`; `m.refreshRecencySegment()` (existing, stays rightmost).
- Produces: nothing consumed later; final rendering change.

- [ ] **Step 1: Write the failing tests**

In `internal/tui/app_test.go` (add `"atm/internal/version"` to imports):

```go
func TestStatusLineShowsKeyClusterAndAppVersion(t *testing.T) {
	m := newTestModel(t)
	line := m.renderStatusLine()
	mustContain(t, line, "[?]help [C]conv [T]theme")
	mustContain(t, line, "atm "+version.Version)
}

func TestPaneHintsNoLongerAdvertiseHelpKeys(t *testing.T) {
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme Task Manager")
	for _, pane := range []string{"1", "2"} {
		update(t, m, pane)
		line := m.renderStatusLine()
		if strings.Contains(line, "[?]keys") || strings.Contains(line, "[C]conventions") {
			t.Errorf("pane %s hint still advertises help keys:\n%s", pane, line)
		}
	}
}
```

In `internal/tui/tasks_test.go:386`, change the expected hint:

```go
	want := "[↑/↓]tasks  [ [ / ] ]board  [s]ort  [a]dd  [p]pin/unpin  [Enter]detail"
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/tui/ -run 'TestStatusLineShowsKeyCluster|TestPaneHintsNoLonger' -v`
Expected: FAIL — `[?]help [C]conv [T]theme` not in line; `[?]keys` still present.

- [ ] **Step 3: Implement**

1. `internal/tui/app.go` `renderStatusLine`: guard the hint (it can now be empty) and extend the right side. The hint/toast/right block becomes:

```go
	if hint := m.statusHint(); hint != "" {
		parts = append(parts, m.styles.KeyMenu.Render(hint))
	}
	if m.toastMsg != "" {
		parts = append(parts, m.styles.Toast.Render(m.toastMsg))
	}
	left := strings.Join(parts, "  ")
	rightSegments := dockSegments(m)
	rightSegments = append(rightSegments,
		m.styles.KeyMenu.Render("[?]help [C]conv [T]theme"),
		m.styles.KeyMenuDim.Render("atm "+version.Version),
		m.refreshRecencySegment())
	right := strings.Join(rightSegments, "  ")
```

Add `"atm/internal/version"` to app.go's imports. The width-gap math below this block is unchanged.

2. `internal/tui/app.go:830` — `Model.statusHint` fallback: `return "[?]keys [C]conventions"` → `return ""`.

3. Strip the redundant fragments (the fixed right cluster now covers them):
- `internal/tui/projects.go:987`: `"[a]add [p]ersona [?]keys"` → `"[a]add [p]ersona"`
- `internal/tui/projects.go:989`: drop trailing `" [?]keys"` → `"[a]dd [s]elect [Enter]detail [x]remove [P]ersona [p]new"`
- `internal/tui/projects.go:993`: `"[?]keys"` → `""`
- `internal/tui/tasks_list.go:583`: `"[?]keys"` → `""`
- `internal/tui/tasks_list.go:588`: drop trailing `"  [?]keys"` → `"[↑/↓]tasks  [ [ / ] ]board  [s]ort  [a]dd  [p]pin/unpin  [Enter]detail"`
- `internal/tui/labels.go:1636`: `"[?]keys"` → `""`
- `internal/tui/labels.go:1649`: drop trailing `" [?]keys"` → `"[Enter]open [n]ew [e]dit [a]dd [S]eed"`

- [ ] **Step 4: Run the full TUI suite**

Run: `go test ./internal/tui/`
Expected: PASS. `TestStatusLineShowsRefreshRecencyRightmost` must still pass (the recency segment is appended last, so it stays rightmost). If `TestStatusLineHintsFollowFocusedPane` fails because both panes now render identical hints, the pane hints themselves still differ (`[a]dd…` vs `[↑/↓]tasks…`) — debug the actual strings before touching the test.

- [ ] **Step 5: Run everything and commit**

Run: `go build ./... && go test ./...`
Expected: full suite PASS (CLI golden files don't render the TUI status line, but the full run proves it).

```bash
git add internal/tui/app.go internal/tui/app_test.go internal/tui/projects.go internal/tui/tasks_list.go internal/tui/labels.go internal/tui/tasks_test.go
git commit -m "feat(ATM-789528): fixed ?/C/T key cluster and app version in status bar"
```

---

### Task 4: Verify in the real app + close the ledger

**Files:** none (verification only)

- [ ] **Step 1: Build and eyeball the live status bar**

Run: `go build -o /tmp/claude-1000/-home-ttran-projects-scyllas-atm/ffba7df7-66b2-4e72-af9e-ba31a48b6ef2/scratchpad/atm ./cmd/atm` then launch the TUI against a throwaway copy of a real store (never the live one):

```bash
cp -r ~/.config/atm /tmp/claude-1000/-home-ttran-projects-scyllas-atm/ffba7df7-66b2-4e72-af9e-ba31a48b6ef2/scratchpad/store-copy
```

and confirm (e.g. via a tmux capture) the bar reads like `⛃ v2 · 142 events · 1.2 MB … [?]help [C]conv [T]theme  atm v1.2.11 ✓` with no `STORE:`/`SELECTED:`/`theme:` text, and that `?`, `C`, `T` still work.

- [ ] **Step 2: Record the outcome on the ATM ledger**

```bash
atm task comment add --task ATM-789528 --label ATM:comment:progress --body "<what shipped, commits, verification result>"
atm capability workflow complete --task ATM-789528
```
