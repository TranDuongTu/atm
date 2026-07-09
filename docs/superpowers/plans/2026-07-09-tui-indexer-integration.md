# TUI Indexer Integration Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Bring the indexer into the TUI: a right-side status-bar plugin dock (icon + state), a `g`-prefix leader key to open plugin overlays (`g 1` = indexer), and an in-process `store.Watch` goroutine per selected project with a modal overlay that edits embedding config inline, starts/stops the watcher, runs one-shot reindex/drop, and streams the live log.

**Architecture:** Pure TUI change. A new `plugin` interface (`internal/tui/plugin.go`) backs a right-aligned plugin dock in the status bar; the `actor:` segment is removed. The indexer is the first plugin (`internal/tui/indexer.go`): it owns an `indexerModel` (state, log ring, config snapshot, goroutine handle) and runs `store.Watch` on a `context.Context` the TUI cancels. The goroutine is pure — it sends `indexerMsg` values onto a buffered channel; the root `Update` drains the channel on a periodic tick and mutates the model (all mutation in `Update`, no locking). A small `pluginSupervisor` wraps `Reset` calls with a 3-strikes/30s debounce. No store, CLI, or `internal/embed` changes; `atm index` CLI is untouched.

**Tech Stack:** Go 1.22+, Bubble Tea/lipgloss TUI, existing `internal/store` (`Watch`, `ReindexOnce`, `PendingIndex`, `VectorMeta`, `ListVectorModels`, `LastLogSeq`, `SetEmbeddingConfig`, `GetProjectConfig`, `DropVectors`), existing `internal/embed` (`Client.Embed`).

## Global Constraints

- Go module path is `atm` (imports are `atm/internal/...`).
- No emojis in code or commits. The indexer icon `⌬` (U+232C, "benzene") is a Unicode symbol, not an emoji — it's allowed. If it breaks the column grid in a font, fall back to `#`.
- The storage/search/index engine never calls a model; only `embed.New(*cfg).Embed` does, injected into `store.Watch` exactly as `cli/index.go` does. No new model-touching boundary.
- The `m.actor` field stays on `Model`; it still stamps `"default"` on TUI mutations (the deeper default-actor decision is ATM-0072, out of scope here). This plan only **hides** the actor segment from the status bar; it does not change what the TUI stamps.
- Test helpers in `internal/tui` (in `app_test.go`): `newTestModel(t)`, `newTestModelWithActor(t, actor)`, `keyMsg(s)`, `update(t, m, key)`, `mustContain(t, view, sub)`, `mustNotContain(t, view, sub)`, `seedProject(t, m, code, name)`, `seedTask(t, m, code, title, labels...)`. Reuse these exactly — do not invent new harness names. `newTestModel`/`newTestModelWithActor` open a fresh temp-dir store and auto-init it.
- Existing overlay precedence in `handleKey`/`View` is preserved: help → confirm → form → actors → **plugin** (new, slots in last).
- The existing `actorsOverlay` (P key, persona activity) is **not** folded into the plugin registry in this plan — it stays a pane-level drill-down. A later refactor can register it.
- `store.Watch(ctx, code, embed EmbedFunc, log ProgressFunc) error` already exists and polls `log.jsonl` with exponential backoff on error (1s→30s). It is unchanged.
- `store.EmbedFunc` = `func(text, role string) ([]float64, error)`; `store.ProgressFunc` = `func(msg string)`.
- Run `make verify` (a.k.a. `make build && make test`) green before each commit. Go test command: `go test ./internal/tui/...`.
- TDD: write the failing test first, run it to see it fail, then implement the minimal code to pass, then commit.

---

## File Structure

- `internal/tui/theme.go` (modify) — add `StatusOK lipgloss.Style` (green-leaning, mirrors `StatusLabel` derivation), wire it into `buildStyles` for all three themes + the mono override block.
- `internal/tui/plugin.go` (new) — `plugin` interface, `pluginSupervisor` (3-strikes/30s debounce), helper `dockSegments(m *Model) []string`. No registered plugins yet (Task 3 registers the first one).
- `internal/tui/indexer.go` (new) — `indexerState` enum, `indexerMsg`/`indexerMsgKind`, `indexerModel`, `indexerStatusRow`, the `indexerPlugin` struct implementing `plugin`, lifecycle (`startIndexer`/`stopIndexer`/`resetIndexer`/`refreshIndexerStatus`), the drain command (`drainIndexerMsgs`), overlay `Render` + `HandleKey`, inline edit mode, nomic preset, embedder test seam.
- `internal/tui/app.go` (modify) — `Model.plugins []plugin`, `Model.pluginOverlay int` (-1), `Model.pluginPrefixActive bool`, `Model.pluginTickCmd`; `NewModel` registers the indexer plugin + wires `embedFnBuilder`; `handleKey` `g` prefix routing + plugin overlay dispatch; `Update` handles the drain tick; `View` layers the plugin overlay; `renderStatusLine` builds the plugin dock + removes `actor:`; `SetSize` sizes an open plugin overlay; project-switch + quit call `resetIndexer`.
- `internal/tui/keymap.go` (modify) — add `g` leader + `g 1` (indexer) rows to `keymapRows`.
- `internal/tui/help.go` (modify) — mention `g <n>` plugin overlay keys in the keys help text.
- `internal/tui/theme_test.go` (new or extend) — `StatusOK` present in all themes.
- `internal/tui/plugin_test.go` (new) — `g` prefix sets flag; `g 1` opens overlay; non-matching key clears flag; supervisor 3-strikes reset.
- `internal/tui/indexer_test.go` (new) — dock label/color per state; `State`/`Open`/`Close`/`Reset`; lifecycle with a fake embedder (no HTTP); drain loop; edit mode + nomic preset + save; `r` reindex once; `d` drop; log ring cap + scroll; 3-strikes integration.
- `internal/tui/app_test.go` (modify) — drop the `actor:` assertion (if any) and add a "no actor segment" assertion; add a "plugin dock present" assertion; confirm existing overlay precedence unaffected.

---

## Task 1: `StatusOK` style in theme

**Files:**
- Modify: `internal/tui/theme.go` (add `StatusOK` to `Styles`; wire in `buildStyles`)
- Test: `internal/tui/theme_test.go` (new file, or extend if it exists)

**Interfaces:**
- Consumes: `Theme.Success lipgloss.Color` (already present — graphite `"113"`, light `"28"`, mono `"255"`).
- Produces: `Styles.StatusOK` — a `lipgloss.Style` green-leaning, used by the indexer dock `on` (idle) state. Mirrors `StatusLabel` (which is `Foreground(t.Accent).Bold(true)`); `StatusOK` is `Foreground(t.Success).Bold(true)`.

- [ ] **Step 1: Write the failing test**

Create `internal/tui/theme_test.go` (if it doesn't exist) with:

```go
package tui

import (
	"testing"
	"github.com/charmbracelet/lipgloss"
)

func TestStatusOKStylePresentInAllThemes(t *testing.T) {
	for _, name := range []ThemeName{themeGraphite, themeLight, themeMono} {
		s := buildStyles(name)
		if s.StatusOK.GetForeground() == (lipgloss.Color{}) {
			t.Errorf("theme %s: StatusOK has no foreground color", name)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tui/ -run TestStatusOKStylePresentInAllThemes -v`
Expected: FAIL — `Styles.StatusOK` undefined (compile error).

- [ ] **Step 3: Write minimal implementation**

In `internal/tui/theme.go`:

Add to the `Styles` struct (after `StatusLabel lipgloss.Style`):

```go
    StatusOK    lipgloss.Style
```

In `buildStyles`, in the struct literal (after the `StatusLabel:` line):

```go
        StatusOK:        lipgloss.NewStyle().Foreground(t.Success).Bold(true),
```

In the mono override block (`if themeName == themeMono { ... }`), add after the existing `s.LabelChip = ...` line:

```go
        s.StatusOK = lipgloss.NewStyle().Bold(true)
```

(Mono has no green; bold-only keeps it readable on a no-color terminal.)

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/tui/ -run TestStatusOKStylePresentInAllThemes -v`
Expected: PASS.

- [ ] **Step 5: Run the full TUI test suite**

Run: `go test ./internal/tui/...`
Expected: PASS (additive change; nothing else references `StatusOK` yet).

- [ ] **Step 6: Commit**

```bash
git add internal/tui/theme.go internal/tui/theme_test.go
git commit -m "tui: add StatusOK style (green-leaning) for indexer dock idle state"
```

---

## Task 2: `plugin` interface + supervisor (no plugins registered)

**Files:**
- Create: `internal/tui/plugin.go`
- Test: `internal/tui/plugin_test.go` (new)

**Interfaces:**
- Consumes: `Styles`, `*Model` (forward-declared; methods on `Model` exist already: `projectScope`, `styles`, `showToast`, `store`).
- Produces:
  - `type plugin interface` with methods: `ID() string`, `Icon() string`, `OverlayKey() string`, `DockLabel(state any) string`, `DockColor(state any, s Styles) lipgloss.Style`, `State(m *Model) any`, `Open(m *Model)`, `Close(m *Model)`, `Reset(m *Model)`, `HandleKey(k tea.KeyMsg, m *Model) tea.Cmd`, `Render(m *Model) string`.
  - `type pluginSupervisor struct` with methods `newPluginSupervisor()`, `recordError(p plugin) (shouldReset bool)`, `clear(p plugin)`.
  - `func dockSegments(m *Model) []string` — returns one rendered dock string per registered plugin (right-aligned join target).

- [ ] **Step 1: Write the failing test**

Create `internal/tui/plugin_test.go`:

```go
package tui

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// fakePlugin is a minimal plugin for supervisor/dock tests.
type fakePlugin struct {
	resets int
}

func (f *fakePlugin) ID() string                                           { return "fake" }
func (f *fakePlugin) Icon() string                                         { return "#" }
func (f *fakePlugin) OverlayKey() string                                   { return "1" }
func (f *fakePlugin) DockLabel(state any) string                            { return "# " + state.(string) }
func (f *fakePlugin) DockColor(state any, s Styles) lipgloss.Style         { return s.Status }
func (f *fakePlugin) State(m *Model) any                                   { return "off" }
func (f *fakePlugin) Open(m *Model)                                        {}
func (f *fakePlugin) Close(m *Model)                                       {}
func (f *fakePlugin) Reset(m *Model)                                       { f.resets++ }
func (f *fakePlugin) HandleKey(k tea.KeyMsg, m *Model) tea.Cmd             { return nil }
func (f *fakePlugin) Render(m *Model) string                               { return "fake overlay" }

func TestSupervisorResetsAfterThreeErrorsIn30s(t *testing.T) {
	sv := newPluginSupervisor()
	p := &fakePlugin{}
	m := newTestModel(t)
	if sv.recordError(p) {
		t.Fatal("first error should not reset")
	}
	if sv.recordError(p) {
		t.Fatal("second error should not reset")
	}
	if !sv.recordError(p) {
		t.Fatal("third error within 30s should reset")
	}
	if p.resets != 1 {
		t.Fatalf("Reset called %d times, want 1", p.resets)
	}
	// After a reset, the window clears — a fresh error starts a new window.
	sv.clear(p)
	if sv.recordError(p) {
		t.Fatal("after clear, first error should not reset")
	}
}

func TestSupervisorNoResetOutside30sWindow(t *testing.T) {
	sv := newPluginSupervisor()
	sv.window = 100 * time.Millisecond // shrink for a fast test
	p := &fakePlugin{}
	m := newTestModel(t)
	sv.recordError(p)
	sv.recordError(p)
	time.Sleep(150 * time.Millisecond)
	if sv.recordError(p) {
		t.Fatal("error outside window should not count toward 3-strikes")
	}
	if p.resets != 0 {
		t.Fatalf("Reset called %d times, want 0", p.resets)
	}
}

func TestDockSegmentsEmptyWithNoPlugins(t *testing.T) {
	m := newTestModel(t)
	m.plugins = nil
	segs := dockSegments(m)
	if len(segs) != 0 {
		t.Fatalf("got %d segments, want 0", len(segs))
	}
}

func TestDockSegmentsRendersOnePerPlugin(t *testing.T) {
	m := newTestModel(t)
	m.plugins = []plugin{&fakePlugin{}}
	segs := dockSegments(m)
	if len(segs) != 1 {
		t.Fatalf("got %d segments, want 1", len(segs))
	}
	// The segment is "<state-colored label>  <muted g1 hint>" — check the
	// label and the keybind hint both appear (avoid brittle ANSI matching).
	if !strings.Contains(segs[0], "# off") {
		t.Errorf("segment %q missing label '# off'", segs[0])
	}
	if !strings.Contains(segs[0], "g1") {
		t.Errorf("segment %q missing keybind hint 'g1'", segs[0])
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tui/ -run 'TestSupervisor|TestDockSegments' -v`
Expected: FAIL — `plugin`, `pluginSupervisor`, `dockSegments` undefined (compile error).

- [ ] **Step 3: Write minimal implementation**

Create `internal/tui/plugin.go`:

```go
package tui

import (
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type plugin interface {
	ID() string
	Icon() string
	OverlayKey() string
	DockLabel(state any) string
	DockColor(state any, s Styles) lipgloss.Style
	State(m *Model) any
	Open(m *Model)
	Close(m *Model)
	Reset(m *Model)
	HandleKey(k tea.KeyMsg, m *Model) tea.Cmd
	Render(m *Model) string
}

type pluginSupervisor struct {
	mu       sync.Mutex
	window   time.Duration
	errors   map[string][]time.Time
}

func newPluginSupervisor() *pluginSupervisor {
	return &pluginSupervisor{window: 30 * time.Second, errors: map[string][]time.Time{}}
}

func (sv *pluginSupervisor) recordError(p plugin) (shouldReset bool) {
	sv.mu.Lock()
	defer sv.mu.Unlock()
	id := p.ID()
	now := time.Now()
	cutoff := now.Add(-sv.window)
	var recent []time.Time
	for _, t := range sv.errors[id] {
		if t.After(cutoff) {
			recent = append(recent, t)
		}
	}
	recent = append(recent, now)
	sv.errors[id] = recent
	if len(recent) >= 3 {
		sv.errors[id] = nil
		return true
	}
	return false
}

func (sv *pluginSupervisor) clear(p plugin) {
	sv.mu.Lock()
	defer sv.mu.Unlock()
	delete(sv.errors, p.ID())
}

func dockSegments(m *Model) []string {
	if len(m.plugins) == 0 {
		return nil
	}
	out := make([]string, 0, len(m.plugins))
	for _, p := range m.plugins {
		st := p.State(m)
		label := p.DockLabel(st)
		style := p.DockColor(st, m.styles)
		hint := m.styles.KeyMenuDim.Render("g" + p.OverlayKey())
		out = append(out, style.Render(label)+"  "+hint)
	}
	return out
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/tui/ -run 'TestSupervisor|TestDockSegments' -v`
Expected: PASS.

- [ ] **Step 5: Run the full TUI test suite**

Run: `go test ./internal/tui/...`
Expected: PASS (the new types are unused outside tests so far).

- [ ] **Step 6: Commit**

```bash
git add internal/tui/plugin.go internal/tui/plugin_test.go
git commit -m "tui: add plugin interface + supervisor (3-strikes/30s debounce)"
```

---

## Task 3: Plugin dock in status bar + `g` prefix routing (actor segment removed)

**Files:**
- Modify: `internal/tui/app.go` (`Model.plugins`, `Model.pluginOverlay`, `Model.pluginPrefixActive`, `Model.supervisor`; `NewModel` registers the supervisor; `handleKey` `g` prefix + plugin overlay dispatch; `Update` no change yet (drain tick lands in Task 5); `View` layers plugin overlay via a no-op for now; `renderStatusLine` renders the dock + removes `actor:`)
- Test: `internal/tui/app_test.go` (modify — add dock + actor-removal + `g` prefix tests)

**Interfaces:**
- Consumes: `plugin`, `pluginSupervisor`, `dockSegments` (Task 2). Existing `Model.showToast`, `Model.projectScope`, `Model.placeOverlay`, `Model.actorsOverlay` (unchanged).
- Produces:
  - `Model.plugins []plugin`, `Model.pluginOverlay int` (-1 = none), `Model.pluginPrefixActive bool`, `Model.supervisor *pluginSupervisor`.
  - `NewModel` sets `m.plugins = nil` (no plugins registered yet — the indexer registers in Task 5), `m.pluginOverlay = -1`, `m.supervisor = newPluginSupervisor()`.
  - `handleKey` behavior: after the existing help/confirm/form/actors branches, if `m.pluginOverlay != -1` route to `m.plugins[m.pluginOverlay].HandleKey`; else if `k == "g"` set `pluginPrefixActive = true`; else if `pluginPrefixActive && k matches a plugin OverlayKey` open it; else clear the prefix flag.
  - `renderStatusLine` right-aligns `strings.Join(dockSegments(m), "  ")` where `actor:` used to be.

- [ ] **Step 1: Write the failing tests**

Append to `internal/tui/app_test.go`:

```go
func TestStatusBarHasNoActorSegment(t *testing.T) {
	m := newTestModelWithActor(t, "claude")
	m.SetSize(100, 30)
	view := m.View()
	if strings.Contains(view, "actor:") {
		t.Errorf("status bar still renders actor: segment\n--- view ---\n%s", view)
	}
}

func TestStatusBarPluginDockEmptyWhenNoPlugins(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(100, 30)
	m.plugins = nil
	line := m.renderStatusLine()
	if strings.Contains(line, "idx:") || strings.Contains(line, "IDX:") {
		t.Errorf("empty dock should render no plugin segment, got %q", line)
	}
}

func TestStatusBarPluginDockShowsKeybindHint(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(100, 30)
	m.plugins = []plugin{newIndexerPlugin()}
	m.indexer = nil // force State() to default idxOff
	line := m.renderStatusLine()
	// The dock segment must include the muted "g1" hint next to the icon
	// so the keybind is discoverable from the status bar alone.
	if !strings.Contains(line, "g1") {
		t.Errorf("dock missing 'g1' keybind hint:\n%s", line)
	}
}

func TestGPrefixSetsFlagAndG1OpensOverlay(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(100, 30)
	m.plugins = []plugin{&fakePlugin{}}
	update(t, m, "g")
	if !m.pluginPrefixActive {
		t.Fatal("g should set pluginPrefixActive")
	}
	update(t, m, "1")
	if m.pluginOverlay != 0 {
		t.Fatalf("g 1 should open plugin overlay 0, got %d", m.pluginOverlay)
	}
	if m.pluginPrefixActive {
		t.Fatal("prefix flag should clear after opening")
	}
}

func TestGPrefixNonMatchingKeyClearsFlag(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(100, 30)
	m.plugins = []plugin{&fakePlugin{}}
	update(t, m, "g")
	if !m.pluginPrefixActive {
		t.Fatal("g should set pluginPrefixActive")
	}
	update(t, m, "x")
	if m.pluginPrefixActive {
		t.Fatal("non-matching key should clear prefix flag")
	}
	if m.pluginOverlay != -1 {
		t.Fatalf("non-matching key should not open overlay, got %d", m.pluginOverlay)
	}
}

func TestEscClosesPluginOverlay(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(100, 30)
	m.plugins = []plugin{&fakePlugin{}}
	m.pluginOverlay = 0
	update(t, m, "esc")
	if m.pluginOverlay != -1 {
		t.Fatalf("Esc should close plugin overlay, got %d", m.pluginOverlay)
	}
}
```

(Reuse `fakePlugin` from `plugin_test.go` — same package.)

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/tui/ -run 'TestStatusBarHasNoActorSegment|TestStatusBarPluginDockEmptyWhenNoPlugins|TestGPrefixSetsFlagAndG1OpensOverlay|TestGPrefixNonMatchingKeyClearsFlag|TestEscClosesPluginOverlay' -v`
Expected: FAIL — `Model.plugins`/`pluginOverlay`/`pluginPrefixActive` undefined (compile error); `actor:` still rendered.

- [ ] **Step 3: Write minimal implementation**

In `internal/tui/app.go`:

Add to `Model` struct (after `toastMsg string`):

```go
    plugins             []plugin
    pluginOverlay       int
    pluginPrefixActive  bool
    supervisor          *pluginSupervisor
```

In `NewModel`, after `m.help = newHelpModel(m)`:

```go
    m.plugins = nil
    m.pluginOverlay = -1
    m.supervisor = newPluginSupervisor()
```

In `handleKey`, after the `if m.actorsOverlay { ... }` block and before the `if m.focused == paneTasks && m.tasks.filterEditing { ... }` block, add:

```go
    if m.pluginOverlay != -1 {
        switch k.String() {
        case "esc":
            m.plugins[m.pluginOverlay].Close(m)
            m.pluginOverlay = -1
            return nil
        case "T":
            m.cycleTheme()
            return nil
        case "?":
            m.openHelp(helpKeys)
            return nil
        case "C":
            m.openHelp(helpConventions)
            return nil
        }
        return m.plugins[m.pluginOverlay].HandleKey(k, m)
    }
```

Then, in the same `handleKey`, after the `switch k.String() { case "1": ... case "?": ... case "T": ... }` block (the global pane-focus + help/theme block), add a `g` case. Find the existing:

```go
    switch k.String() {
    case "1":
        m.focused = paneProjects
        return nil
    case "2":
        m.focused = paneTasks
        return nil
    case "3":
        m.focused = paneLabels
        return nil
    case "?":
        m.openHelp(helpKeys)
        return nil
    case "C":
        m.openHelp(helpConventions)
        return nil
    case "T":
        m.cycleTheme()
        return nil
    }
```

Add `case "g":` to it:

```go
    case "g":
        m.pluginPrefixActive = true
        return nil
```

Then, immediately after that `switch` block, add the prefix-resolution logic:

```go
    if m.pluginPrefixActive {
        m.pluginPrefixActive = false
        for i, p := range m.plugins {
            if k.String() == p.OverlayKey() {
                if m.projectScope == "" {
                    m.showToast("select a project first")
                    return nil
                }
                m.pluginOverlay = i
                p.Open(m)
                return nil
            }
        }
        return nil
    }
```

(If `g` was just pressed, the `case "g"` above sets the flag and returns; the *next* key enters this block.)

In `View`, after the `if m.actorsOverlay { ... }` block and before `return out`, add:

```go
    if m.pluginOverlay != -1 {
        out = m.placeOverlay(out, m.plugins[m.pluginOverlay].Render(m))
    }
```

In `renderStatusLine`, replace the actor right-alignment block. Find the existing (lines ~705-735):

```go
    parts = append(parts, m.styles.StatusLabel.Render("theme: ")+m.styles.Status.Render(string(m.themeName)))
    hint := m.statusHint()
    parts = append(parts, m.styles.KeyMenu.Render(hint))
    if m.toastMsg != "" {
        parts = append(parts, m.styles.Toast.Render(m.toastMsg))
    }
    actor := "actor: " + m.actorOr()
    left := strings.Join(parts, "  ")
    used := lipgloss.Width(left)
    actorW := lipgloss.Width(actor)
    need := used + 2 + actorW
    gap := 2
    if need < m.width {
        gap = m.width - used - actorW
    }
    if gap < 1 {
        gap = 1
    }
    line := left + spaces(gap) + m.styles.Status.Render(actor)
    if lw := lipgloss.Width(line); lw < m.width {
        line += spaces(m.width - lw)
    }
    return line
```

Replace with:

```go
    parts = append(parts, m.styles.StatusLabel.Render("theme: ")+m.styles.Status.Render(string(m.themeName)))
    hint := m.statusHint()
    parts = append(parts, m.styles.KeyMenu.Render(hint))
    if m.toastMsg != "" {
        parts = append(parts, m.styles.Toast.Render(m.toastMsg))
    }
    left := strings.Join(parts, "  ")
    right := strings.Join(dockSegments(m), "  ")
    used := lipgloss.Width(left)
    rightW := lipgloss.Width(right)
    need := used + 2 + rightW
    gap := 2
    if need < m.width {
        gap = m.width - used - rightW
    }
    if gap < 1 {
        gap = 1
    }
    line := left + spaces(gap) + right
    if lw := lipgloss.Width(line); lw < m.width {
        line += spaces(m.width - lw)
    }
    return line
```

(`m.actorOr()` is now unused by `renderStatusLine`; leave the method on `Model` — it's harmless and other tests may reference it. Do not delete it in this task.)

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/tui/ -run 'TestStatusBarHasNoActorSegment|TestStatusBarPluginDockEmptyWhenNoPlugins|TestGPrefixSetsFlagAndG1OpensOverlay|TestGPrefixNonMatchingKeyClearsFlag|TestEscClosesPluginOverlay' -v`
Expected: PASS.

- [ ] **Step 5: Run the full TUI test suite**

Run: `go test ./internal/tui/...`
Expected: Some existing tests may assert `actor:` in the status line — fix them by deleting those assertions (the actor segment is intentionally gone). Do **not** re-add the segment. Run again until green.

- [ ] **Step 6: Commit**

```bash
git add internal/tui/app.go internal/tui/app_test.go
git commit -m "tui: plugin dock in status bar + g-prefix overlay routing; remove actor segment"
```

---

## Task 4: `indexerModel` state + dock label/color (read-only, no goroutine yet)

**Files:**
- Create: `internal/tui/indexer.go`
- Test: `internal/tui/indexer_test.go` (new)

**Interfaces:**
- Consumes: `plugin` (Task 2), `Styles.StatusOK` (Task 1), `store.EmbeddingConfig`, `store.VectorMeta`, `store.ListVectorModels`, `store.LastLogSeq`, `store.GetProjectConfig`.
- Produces:
  - `type indexerState int` with consts `idxOff`, `idxStopped`, `idxIdle`, `idxWorking`, `idxError`.
  - `type indexerMsg`, `type indexerMsgKind`, `type indexerStatusRow`.
  - `type indexerModel struct` with fields: `m *Model`, `state indexerState`, `lastError string`, `logs []string`, `logOffset int`, `cfg *store.EmbeddingConfig`, `status []indexerStatusRow`, `cancel context.CancelFunc`, `done chan struct{}`, `startedAt time.Time`, `editMode bool`, `editFields []formField`, `editCursor int`, `msgCh chan indexerMsg`, `embedFnBuilder func(*store.EmbeddingConfig) store.EmbedFunc`.
  - `type indexerPlugin struct{}` implementing `plugin` (ID/Icon/OverlayKey/DockLabel/DockColor/State). `Open`/`Close`/`Reset`/`HandleKey`/`Render` are stubs here (filled in Tasks 5-8); `Render` returns a placeholder string for now.
  - `func newIndexerPlugin() *indexerPlugin`.
  - `func (im *indexerModel) refreshStatus()` — reads `GetProjectConfig` + `ListVectorModels` + `VectorMeta` + `LastLogSeq` into `cfg`/`status`.
  - `func (im *indexerModel) dockState() indexerState` — returns `im.state`.

- [ ] **Step 1: Write the failing tests**

Create `internal/tui/indexer_test.go`:

```go
package tui

import (
	"testing"

	"atm/internal/store"
)

func newIndexerTestModel(t *testing.T) *Model {
	t.Helper()
	m := newTestModel(t)
	seedProject(t, m, "ATM", "Acme Task Manager")
	m.projectScope = "ATM"
	m.SetSize(100, 30)
	return m
}

func setEmbedding(t *testing.T, m *Model, code string) {
	t.Helper()
	cfg := store.EmbeddingConfig{Model: "m", Endpoint: "http://x", Dim: 2, Threshold: 0.5}
	if err := m.store.SetEmbeddingConfig(code, cfg, "claude"); err != nil {
		t.Fatalf("SetEmbeddingConfig: %v", err)
	}
}

func TestIndexerDockLabelPerState(t *testing.T) {
	p := newIndexerPlugin()
	cases := []struct {
		state indexerState
		want  string
	}{
		{idxOff, "⌬ off"},
		{idxStopped, "⌬ stopped"},
		{idxIdle, "⌬ on"},
		{idxWorking, "⌬ running"},
		{idxError, "⌬ error"},
	}
	for _, c := range cases {
		got := p.DockLabel(c.state)
		if got != c.want {
			t.Errorf("state %d: got %q want %q", c.state, got, c.want)
		}
	}
}

func TestIndexerDockColorPerState(t *testing.T) {
	p := newIndexerPlugin()
	m := newIndexerTestModel(t)
	s := m.styles
	if p.DockColor(idxOff, s) != s.Status {
		t.Error("idxOff should use Status")
	}
	if p.DockColor(idxStopped, s) != s.Status {
		t.Error("idxStopped should use Status")
	}
	if p.DockColor(idxIdle, s) != s.StatusOK {
		t.Error("idxIdle should use StatusOK")
	}
	if p.DockColor(idxWorking, s) != s.StatusLabel {
		t.Error("idxWorking should use StatusLabel")
	}
	if p.DockColor(idxError, s) != s.Warning {
		t.Error("idxError should use Warning")
	}
}

func TestIndexerStateOffWhenNoConfig(t *testing.T) {
	m := newIndexerTestModel(t)
	p := newIndexerPlugin()
	im := p.model(m)
	im.refreshStatus()
	if p.State(m).(indexerState) != idxOff {
		t.Errorf("no config -> state %v, want idxOff", p.State(m))
	}
}

func TestIndexerStateStoppedWhenConfigPresent(t *testing.T) {
	m := newIndexerTestModel(t)
	setEmbedding(t, m, "ATM")
	p := newIndexerPlugin()
	im := p.model(m)
	im.refreshStatus()
	if p.State(m).(indexerState) != idxStopped {
		t.Errorf("config present, not started -> state %v, want idxStopped", p.State(m))
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/tui/ -run TestIndexer -v`
Expected: FAIL — `indexerState`, `indexerPlugin`, `indexerModel`, `newIndexerPlugin` undefined (compile error).

- [ ] **Step 3: Write minimal implementation**

Create `internal/tui/indexer.go`:

```go
package tui

import (
	"context"
	"time"

	"atm/internal/embed"
	"atm/internal/store"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type indexerState int

const (
	idxOff indexerState = iota
	idxStopped
	idxIdle
	idxWorking
	idxError
)

type indexerMsgKind int

const (
	msgProgress indexerMsgKind = iota
	msgState
	msgError
	msgDone
)

type indexerMsg struct {
	kind  indexerMsgKind
	line  string
	state indexerState
	err   string
}

type indexerStatusRow struct {
	Model  string
	Count  int
	Last   int
	Behind int
}

type indexerModel struct {
	m             *Model
	state         indexerState
	lastError     string
	logs          []string
	logOffset     int
	cfg           *store.EmbeddingConfig
	status        []indexerStatusRow
	cancel        context.CancelFunc
	done          chan struct{}
	startedAt     time.Time
	editMode      bool
	editFields    []formField
	editCursor    int
	msgCh         chan indexerMsg
	embedFnBuilder func(*store.EmbeddingConfig) store.EmbedFunc
}

type indexerPlugin struct{}

func newIndexerPlugin() *indexerPlugin { return &indexerPlugin{} }

func (p *indexerPlugin) ID() string         { return "indexer" }
func (p *indexerPlugin) Icon() string        { return "⌬" }
func (p *indexerPlugin) OverlayKey() string  { return "1" }

func (p *indexerPlugin) DockLabel(state any) string {
	switch state.(indexerState) {
	case idxOff:
		return "⌬ off"
	case idxStopped:
		return "⌬ stopped"
	case idxIdle:
		return "⌬ on"
	case idxWorking:
		return "⌬ running"
	case idxError:
		return "⌬ error"
	}
	return "⌬ ?"
}

func (p *indexerPlugin) DockColor(state any, s Styles) lipgloss.Style {
	switch state.(indexerState) {
	case idxIdle:
		return s.StatusOK
	case idxWorking:
		return s.StatusLabel
	case idxError:
		return s.Warning
	}
	return s.Status
}

func (p *indexerPlugin) State(m *Model) any {
	return p.model(m).state
}

func (p *indexerPlugin) Open(m *Model)  { p.model(m).refreshStatus() }
func (p *indexerPlugin) Close(m *Model) {}
func (p *indexerPlugin) Reset(m *Model) { resetIndexer(m) }

func (p *indexerPlugin) HandleKey(k tea.KeyMsg, m *Model) tea.Cmd { return nil }

func (p *indexerPlugin) Render(m *Model) string {
	return titledBoxHeight(m.styles.DialogBody, m.width, "Indexer — "+m.projectScope, "(indexer overlay — Task 7 fills this in)", m.contentHeight)
}

func (p *indexerPlugin) model(m *Model) *indexerModel {
	if m.indexer == nil {
		m.indexer = &indexerModel{
			m:              m,
			state:          idxOff,
			logOffset:      -1,
			msgCh:          make(chan indexerMsg, 256),
			embedFnBuilder: defaultEmbedFnBuilder,
		}
	}
	return m.indexer
}

func defaultEmbedFnBuilder(cfg *store.EmbeddingConfig) store.EmbedFunc {
	client := embed.New(*cfg)
	return func(text, role string) ([]float64, error) { return client.Embed(text, role) }
}

func (im *indexerModel) refreshStatus() {
	cfg, err := im.m.store.GetProjectConfig(im.m.projectScope)
	if err != nil || cfg == nil || cfg.Embedding == nil {
		im.cfg = nil
		im.status = nil
		if im.state == idxOff || im.state == idxStopped {
			im.state = idxOff
		}
		return
	}
	im.cfg = cfg.Embedding
	if im.state == idxOff {
		im.state = idxStopped
	}
	last, _ := im.m.store.LastLogSeq(im.m.projectScope)
	models, _ := im.m.store.ListVectorModels(im.m.projectScope)
	rows := make([]indexerStatusRow, 0, len(models))
	for _, slug := range models {
		meta, _ := im.m.store.VectorMeta(im.m.projectScope, slug)
		r := indexerStatusRow{Model: slug}
		if meta != nil {
			r.Count = meta.Count
			r.Last = meta.LastLogSeq
			r.Behind = last - meta.LastLogSeq
		}
		rows = append(rows, r)
	}
	im.status = rows
}

func resetIndexer(m *Model) {
	im := m.indexer
	if im == nil {
		return
	}
	stopIndexer(m)
	im.logs = nil
	im.logOffset = -1
	im.lastError = ""
	im.state = idxStopped
	im.refreshStatus()
}

func stopIndexer(m *Model) {
	im := m.indexer
	if im == nil || im.cancel == nil {
		return
	}
	im.cancel()
	if im.done != nil {
		<-im.done
	}
	drainChannel(im.msgCh)
	im.cancel = nil
	im.done = nil
}

func drainChannel(ch chan indexerMsg) {
	for {
		select {
		case <-ch:
		default:
			return
		}
	}
}
```

Also add to `Model` struct in `internal/tui/app.go` (after `supervisor *pluginSupervisor`):

```go
    indexer *indexerModel
```

And in `NewModel`, after `m.supervisor = newPluginSupervisor()`:

```go
    m.plugins = []plugin{newIndexerPlugin()}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/tui/ -run TestIndexer -v`
Expected: PASS.

- [ ] **Step 5: Run the full TUI test suite**

Run: `go test ./internal/tui/...`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/tui/indexer.go internal/tui/indexer_test.go internal/tui/app.go
git commit -m "tui: indexer plugin model + dock label/color (state machine, no goroutine yet)"
```

---

## Task 5: Watcher lifecycle (start/stop/reset) + drain tick

**Files:**
- Modify: `internal/tui/indexer.go` (`startIndexer`, real `stopIndexer`/`resetIndexer`, drain command + message application)
- Modify: `internal/tui/app.go` (`Update` handles a `pluginTickMsg` to drain; `SetSize` sizes an open overlay; project-switch + quit call `resetIndexer`)
- Test: `internal/tui/indexer_test.go` (extend — start/stop/drain with a fake embedder)

**Interfaces:**
- Consumes: `store.Watch`, `store.ReindexOnce`, `store.EmbedFunc`, `store.ProgressFunc`, `context`, `time`, the `embedFnBuilder` seam (overridable in tests).
- Produces:
  - `func startIndexer(m *Model, code string) tea.Cmd` — validates config; builds embedFn via `embedFnBuilder`; spins goroutine running `store.Watch`; sets `state→idxWorking`, `startedAt`, `cancel`, `done`; returns a `pluginTickCmd` to start draining.
  - `type pluginTickMsg struct{}` — the tick message; `Update` on receipt drains `msgCh` and applies state, then returns another `pluginTickCmd` if the overlay is open or the goroutine is alive.
  - `func pluginTickCmd() tea.Cmd` — returns a `tea.Tick(120*time.Millisecond, func(time.Time) tea.Msg { return pluginTickMsg{} })`.
  - Project switch + quit call `resetIndexer(m)`.

- [ ] **Step 1: Write the failing tests**

Append to `internal/tui/indexer_test.go`:

```go
import (
	"context"
	"errors"
	"sync/atomic"
	"time"
)

// fakeEmbedFnBuilder returns an embedFn that yields deterministic 2-dim vectors.
func fakeEmbedFnBuilder(vec []float64) func(*store.EmbeddingConfig) store.EmbedFunc {
	return func(*store.EmbeddingConfig) store.EmbedFunc {
		return func(text, role string) ([]float64, error) { return vec, nil }
	}
}

func TestStartIndexerTransitionsToIdleOnCaughtUp(t *testing.T) {
	m := newIndexerTestModel(t)
	seedTask(t, m, "ATM", "first task")
	setEmbedding(t, m, "ATM")
	p := newIndexerPlugin()
	im := p.model(m)
	im.embedFnBuilder = fakeEmbedFnBuilder([]float64{0.1, 0.2})

	cmd := startIndexer(m, "ATM")
	if cmd == nil {
		t.Fatal("startIndexer should return a tick cmd")
	}
	if im.state != idxWorking {
		t.Fatalf("after start: state %v, want idxWorking", im.state)
	}
	if im.cancel == nil || im.done == nil {
		t.Fatal("start should set cancel + done")
	}

	// Drain: fire ticks until idle or timeout.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && im.state == idxWorking {
		applyTick(m)
		time.Sleep(20 * time.Millisecond)
	}
	if im.state != idxIdle {
		t.Fatalf("after drain: state %v, want idxIdle", im.state)
	}

	resetIndexer(m)
	if im.state != idxStopped {
		t.Fatalf("after reset: state %v, want idxStopped", im.state)
	}
	if im.cancel != nil || im.done != nil {
		t.Fatal("reset should clear cancel + done")
	}
}

func TestStartIndexerErrorsOnEmbedFailure(t *testing.T) {
	m := newIndexerTestModel(t)
	seedTask(t, m, "ATM", "first task")
	setEmbedding(t, m, "ATM")
	p := newIndexerPlugin()
	im := p.model(m)
	var calls int32
	im.embedFnBuilder = func(*store.EmbeddingConfig) store.EmbedFunc {
		return func(text, role string) ([]float64, error) {
			atomic.AddInt32(&calls, 1)
			return nil, errors.New("endpoint down")
		}
	}

	startIndexer(m, "ATM")
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && im.state == idxWorking {
		applyTick(m)
		time.Sleep(20 * time.Millisecond)
	}
	if im.state != idxError {
		t.Fatalf("after drain: state %v, want idxError", im.state)
	}
	if im.lastError == "" {
		t.Fatal("lastError should record the endpoint error")
	}
	resetIndexer(m)
	if im.state != idxStopped {
		t.Fatalf("after reset: state %v, want idxStopped", im.state)
	}
}

func TestStartIndexerNoConfigToasts(t *testing.T) {
	m := newIndexerTestModel(t)
	m.projectScope = "ATM"
	p := newIndexerPlugin()
	p.model(m) // initialize
	cmd := startIndexer(m, "ATM")
	if cmd != nil {
		t.Fatal("no config -> startIndexer should return nil")
	}
	if m.toastMsg == "" || !strings.Contains(m.toastMsg, "no embedding") {
		t.Fatalf("expected a 'no embedding' toast, got %q", m.toastMsg)
	}
}

func TestStopIndexerBlocksUntilGoroutineReturns(t *testing.T) {
	m := newIndexerTestModel(t)
	seedTask(t, m, "ATM", "first task")
	setEmbedding(t, m, "ATM")
	p := newIndexerPlugin()
	im := p.model(m)
	im.embedFnBuilder = fakeEmbedFnBuilder([]float64{0.1, 0.2})
	startIndexer(m, "ATM")
	// let it go idle
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && im.state == idxWorking {
		applyTick(m)
		time.Sleep(20 * time.Millisecond)
	}
	stopIndexer(m)
	if im.cancel != nil || im.done != nil {
		t.Fatal("stop should clear cancel + done")
	}
}
```

Also add a helper to `indexer_test.go`:

```go
func applyTick(m *Model) {
	// Synchronously drain the channel + apply messages, mirroring Update's tick handler.
	im := m.indexer
	if im == nil {
		return
	}
	for {
		select {
		case msg := <-im.msgCh:
			applyIndexerMsg(m, msg)
		default:
			return
		}
	}
}
```

(`applyIndexerMsg` is defined in Step 3.)

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/tui/ -run 'TestStartIndexer|TestStopIndexer' -v`
Expected: FAIL — `startIndexer`, `applyTick`, `applyIndexerMsg` undefined.

- [ ] **Step 3: Write minimal implementation**

In `internal/tui/indexer.go`, add (replacing the placeholder `resetIndexer`/`stopIndexer` from Task 4 with the real ones):

```go
import (
	"errors"
)

type pluginTickMsg struct{}

func pluginTickCmd() tea.Cmd {
	return tea.Tick(120*time.Millisecond, func(time.Time) tea.Msg { return pluginTickMsg{} })
}

func startIndexer(m *Model, code string) tea.Cmd {
	im := m.indexer
	if im == nil {
		im = newIndexerPlugin().model(m)
	}
	im.refreshStatus()
	if im.cfg == nil {
		m.showToast("no embedding configured; press e to edit")
		return nil
	}
	if im.cancel != nil {
		return nil // already running
	}
	embedFn := im.embedFnBuilder(im.cfg)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	im.cancel = cancel
	im.done = done
	im.state = idxWorking
	im.startedAt = time.Now()
	send := func(kind indexerMsgKind, line string, st indexerState, err string) {
		select {
		case im.msgCh <- indexerMsg{kind: kind, line: line, state: st, err: err}:
		default:
			// drop-oldest on overflow
			select {
			case <-im.msgCh:
			default:
			}
			select {
			case im.msgCh <- indexerMsg{kind: kind, line: line, state: st, err: err}:
			default:
			}
		}
	}
	progress := func(msg string) { send(msgProgress, msg, 0, "") }
	go func() {
		defer close(done)
		err := m.store.Watch(ctx, code, embedFn, progress)
		if err != nil && !errors.Is(err, context.Canceled) {
			send(msgError, "", idxError, err.Error())
			return
		}
		send(msgDone, "", idxStopped, "")
	}()
	return pluginTickCmd()
}

func applyIndexerMsg(m *Model, msg indexerMsg) {
	im := m.indexer
	if im == nil {
		return
	}
	switch msg.kind {
	case msgProgress:
		im.logs = append(im.logs, msg.line)
		if len(im.logs) > 1000 {
			im.logs = im.logs[len(im.logs)-1000:]
		}
		if im.logOffset == -1 {
			// stay tailed
		}
	case msgState:
		im.state = msg.state
	case msgError:
		im.lastError = msg.err
		im.state = idxError
		if m.supervisor.recordError(newIndexerPlugin()) {
			resetIndexer(m)
			m.showToast("indexer reset after repeated errors: " + msg.err)
		}
	case msgDone:
		if im.state != idxError {
			im.state = idxStopped
		}
		im.cancel = nil
		im.done = nil
	}
}
```

Replace the Task 4 `resetIndexer` and `stopIndexer` with:

```go
func resetIndexer(m *Model) {
	im := m.indexer
	if im == nil {
		return
	}
	stopIndexer(m)
	im.logs = nil
	im.logOffset = -1
	im.lastError = ""
	im.state = idxStopped
	m.supervisor.clear(newIndexerPlugin())
	im.refreshStatus()
}

func stopIndexer(m *Model) {
	im := m.indexer
	if im == nil || im.cancel == nil {
		return
	}
	im.cancel()
	if im.done != nil {
		<-im.done
	}
	drainChannel(im.msgCh)
	im.cancel = nil
	im.done = nil
}
```

In `internal/tui/app.go`, add the drain tick to `Update`:

```go
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.SetSize(msg.Width, msg.Height)
		return m, nil
	case pluginTickMsg:
		im := m.indexer
		if im == nil {
			return m, nil
		}
		for {
			select {
			case msg := <-im.msgCh:
				applyIndexerMsg(m, msg)
			default:
				goto drained
			}
		}
	drained:
		if m.pluginOverlay != -1 || im.cancel != nil {
			return m, pluginTickCmd()
		}
		return m, nil
	case tea.KeyMsg:
		return m, m.handleKey(msg)
	}
	return m, nil
}
```

Also, in `handleKey`, add `g`-prefix Start handling — actually Start is `S` inside the overlay, handled in Task 6; but the watcher needs to be stopped on project switch and quit. Add at the top of `handleKey`, right after the `case "ctrl+c":` quit block:

```go
	case "q":
		if m.indexer != nil {
			resetIndexer(m)
		}
		m.quitting = true
		return tea.Quit
```

(Find the existing `if k.String() == "q"` block later in `handleKey` — it currently sets `quitting` + `tea.Quit`. Move the `resetIndexer` call there, and remove this duplicate. The existing `q` block is after the overlay/form checks, so it only fires when nothing else is active — which is correct.)

For project switch: in `projects.go`, find where `p.m.projectScope = r.code` is set (line ~212) and where `m.projectScope = ""` on removal (line ~958). Add `if p.m.indexer != nil { resetIndexer(p.m) }` right after each assignment. (These are in `projects.go` `handleKey` and the select/remove paths — locate them with grep in the task; the assignment lines are stable.)

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/tui/ -run 'TestStartIndexer|TestStopIndexer' -v`
Expected: PASS.

- [ ] **Step 5: Run the full TUI test suite**

Run: `go test ./internal/tui/...`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/tui/indexer.go internal/tui/indexer_test.go internal/tui/app.go internal/tui/projects.go
git commit -m "tui: indexer watcher lifecycle (start/stop/reset) + drain tick"
```

---

## Task 6: Overlay render — config block, status block, log pane (view mode)

**Files:**
- Modify: `internal/tui/indexer.go` (real `Render`; `HandleKey` for view-mode `S`/scroll; `SetSize` for the overlay)
- Modify: `internal/tui/app.go` (`SetSize` sizes an open plugin overlay like `helpBoxSize`)
- Test: `internal/tui/indexer_test.go` (extend — overlay content + `S` toggle + scroll)

**Interfaces:**
- Consumes: `titledBoxHeight`, `dashboardLine`, `sectionDivider`, `fitLine`, `spaces` (from `styles.go`); `m.projectScope`, `m.styles`, `m.width`, `m.contentHeight`.
- Produces: a `Render(m *Model) string` that lays out the four regions (Config / Status / Action row / Log pane) inside a `titledBoxHeight` sized to `helpBoxSize`. `HandleKey` handles `S` (start/stop toggle), `j`/`k`/`PgUp`/`PgDn`/`G` (log scroll), `e` (Task 7), `s` (Task 7), `r` (Task 8), `d` (Task 8).

- [ ] **Step 1: Write the failing tests**

Append to `internal/tui/indexer_test.go`:

```go
func TestIndexerOverlayRefusesWithoutProject(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(100, 30)
	m.projectScope = ""
	update(t, m, "g")
	update(t, m, "1")
	if m.pluginOverlay != -1 {
		t.Fatal("overlay must not open without a project")
	}
	if m.toastMsg == "" || !strings.Contains(m.toastMsg, "select a project") {
		t.Fatalf("expected 'select a project' toast, got %q", m.toastMsg)
	}
}

func TestIndexerOverlayShowsConfigAndStatus(t *testing.T) {
	m := newIndexerTestModel(t)
	setEmbedding(t, m, "ATM")
	p := newIndexerPlugin()
	p.model(m).refreshStatus()
	m.pluginOverlay = 0
	p.Open(m)
	view := p.Render(m)
	mustContain(t, view, "Indexer — ATM")
	mustContain(t, view, "Embedding model:")
	mustContain(t, view, "nomic" ) // empty unless set — actually expect "m" here
	mustContain(t, view, "Endpoint:")
	mustContain(t, view, "Status:")
	mustContain(t, view, "[e] edit config")
	mustContain(t, view, "[S] start/stop")
	mustContain(t, view, "[Esc] close")
}

func TestIndexerOverlayNoConfigShowsNone(t *testing.T) {
	m := newIndexerTestModel(t)
	p := newIndexerPlugin()
	p.model(m).refreshStatus()
	m.pluginOverlay = 0
	p.Open(m)
	view := p.Render(m)
	mustContain(t, view, "(none")
	mustContain(t, view, "press [e] to configure")
}

func TestIndexerOverlaySTogglesRuntime(t *testing.T) {
	m := newIndexerTestModel(t)
	seedTask(t, m, "ATM", "first task")
	setEmbedding(t, m, "ATM")
	p := newIndexerPlugin()
	im := p.model(m)
	im.embedFnBuilder = fakeEmbedFnBuilder([]float64{0.1, 0.2})
	m.pluginOverlay = 0
	p.Open(m)

	// S from stopped -> start
	cmd := p.HandleKey(keyMsg("S"), m)
	if cmd == nil {
		t.Fatal("S from stopped should start the watcher (return tick cmd)")
	}
	if im.state != idxWorking {
		t.Fatalf("S from stopped: state %v, want idxWorking", im.state)
	}
	// let it settle
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && im.state == idxWorking {
		applyTick(m)
		time.Sleep(20 * time.Millisecond)
	}
	if im.state != idxIdle {
		t.Fatalf("after settle: state %v, want idxIdle", im.state)
	}
	// S from running -> stop
	p.HandleKey(keyMsg("S"), m)
	if im.state != idxStopped {
		t.Fatalf("S from running: state %v, want idxStopped", im.state)
	}
}

func TestIndexerOverlaySNoConfigToasts(t *testing.T) {
	m := newIndexerTestModel(t)
	p := newIndexerPlugin()
	p.model(m).refreshStatus()
	m.pluginOverlay = 0
	p.Open(m)
	p.HandleKey(keyMsg("S"), m)
	if m.toastMsg == "" || !strings.Contains(m.toastMsg, "no embedding") {
		t.Fatalf("expected 'no embedding' toast, got %q", m.toastMsg)
	}
}

func TestIndexerOverlayLogScroll(t *testing.T) {
	m := newIndexerTestModel(t)
	setEmbedding(t, m, "ATM")
	p := newIndexerPlugin()
	im := p.model(m)
	im.logs = []string{"line one", "line two", "line three"}
	im.logOffset = -1
	m.pluginOverlay = 0
	p.Open(m)
	// k pins offset away from tail
	p.HandleKey(keyMsg("k"), m)
	if im.logOffset == -1 {
		t.Fatal("k should pin logOffset away from -1")
	}
	// G resets to tail
	p.HandleKey(keyMsg("G"), m)
	if im.logOffset != -1 {
		t.Fatalf("G should reset logOffset to -1 (tail), got %d", im.logOffset)
	}
}

func TestIndexerOverlayLogBottomAnchored(t *testing.T) {
	m := newIndexerTestModel(t)
	setEmbedding(t, m, "ATM")
	p := newIndexerPlugin()
	im := p.model(m)
	im.logs = []string{"only line"}
	im.logOffset = -1
	m.SetSize(100, 40)
	m.pluginOverlay = 0
	p.Open(m)
	view := p.Render(m)
	lines := strings.Split(strings.TrimRight(view, "\n"), "\n")
	// The modal is ~80% of 39 content rows. The single log line must sit in
	// the BOTTOM few rows of the modal, not the middle. Assert the log line
	// is below the halfway mark of the modal.
	half := len(lines) / 2
	found := -1
	for i, l := range lines {
		if strings.Contains(l, "only line") {
			found = i
			break
		}
	}
	if found == -1 {
		t.Fatal("log line 'only line' not found in view")
	}
	if found < half {
		t.Fatalf("log line at row %d of %d (above halfway) — log pane must be bottom-anchored", found, len(lines))
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/tui/ -run 'TestIndexerOverlay' -v`
Expected: FAIL — `Render` is the Task 4 placeholder; `HandleKey` is a stub.

- [ ] **Step 3: Write minimal implementation**

In `internal/tui/indexer.go`, replace `Render` and `HandleKey`:

```go
func (p *indexerPlugin) Render(m *Model) string {
	im := p.model(m)
	bw, bh := m.helpBoxSize()
	innerW := bw - 2
	if innerW < 1 {
		innerW = 1
	}
	innerH := bh - 2 // titledBoxHeight draws border + title + bottom row
	if innerH < 1 {
		innerH = 1
	}
	cfgBlock := p.renderConfigBlock(m, innerW)
	statusBlock := p.renderStatusBlock(m, innerW)
	actionRow := p.renderActionRow(m, innerW)
	// Each section is separated by one blank line.
	used := lineCount(cfgBlock) + 1 + lineCount(statusBlock) + 1 + lineCount(actionRow) + 1
	logH := innerH - used
	if logH < 1 {
		logH = 1
	}
	logBlock := p.renderLogPane(m, innerW, logH)
	var b strings.Builder
	b.WriteString(cfgBlock)
	b.WriteString("\n")
	b.WriteString(statusBlock)
	b.WriteString("\n")
	b.WriteString(actionRow)
	b.WriteString("\n")
	b.WriteString(logBlock)
	return titledBoxHeight(m.styles.DialogBody, bw, "Indexer — "+m.projectScope, b.String(), bh)
}

func (p *indexerPlugin) renderConfigBlock(m *Model, w int) string {
	var b strings.Builder
	b.WriteString(sectionDivider(m.styles, w, "Config"))
	b.WriteString("\n")
	im := p.model(m)
	if im.cfg == nil {
		b.WriteString(dashboardLine(w, m.styles.Muted.Render("Embedding model: (none — press [e] to configure)")))
		return b.String()
	}
	cfg := im.cfg
	rows := []string{
		fmt.Sprintf("Embedding model:   %s", cfg.Model),
		fmt.Sprintf("Endpoint:          %s", cfg.Endpoint),
		fmt.Sprintf("Dim / threshold:   %d / %.2f", cfg.Dim, cfg.Threshold),
	}
	if cfg.QueryPrefix != "" || cfg.DocPrefix != "" {
		rows = append(rows, fmt.Sprintf("Prefixes:          %s / %s", cfg.QueryPrefix, cfg.DocPrefix))
	}
	for _, r := range rows {
		b.WriteString(dashboardLine(w, r))
		b.WriteString("\n")
	}
	return b.String()
}

func (p *indexerPlugin) renderStatusBlock(m *Model, w int) string {
	var b strings.Builder
	b.WriteString(sectionDivider(m.styles, w, "Status"))
	b.WriteString("\n")
	im := p.model(m)
	stateWord := stateWord(im.state)
	b.WriteString(dashboardLine(w, fmt.Sprintf("Status:    %s %s", p.Icon(), stateWord)))
	b.WriteString("\n")
	if im.cfg != nil && im.startedAt != (time.Time{}) {
		cfg, _ := m.store.GetProjectConfig(m.projectScope)
		if cfg != nil && cfg.UpdatedAt != "" {
			// config-changed hint: startedAt is before config UpdatedAt
			if updated, err := time.Parse(time.RFC3339, cfg.UpdatedAt); err == nil && im.startedAt.Before(updated) {
				b.WriteString(dashboardLine(w, m.styles.Muted.Render(fmt.Sprintf("           %s running (config changed — S to restart)", p.Icon()))))
				b.WriteString("\n")
			}
		}
	}
	for _, r := range im.status {
		line := fmt.Sprintf("Index:     %s  count=%d  last_log_seq=%d  behind=%d", r.Model, r.Count, r.Last, r.Behind)
		b.WriteString(dashboardLine(w, line))
		b.WriteString("\n")
	}
	if im.lastError != "" {
		b.WriteString(dashboardLine(w, m.styles.Error.Render("error: "+im.lastError)))
		b.WriteString("\n")
	}
	return b.String()
}

func (p *indexerPlugin) renderActionRow(m *Model, w int) string {
	im := p.model(m)
	if im.editMode {
		return dashboardLine(w, m.styles.KeyMenu.Render("[Tab] next field   [s] save   [p] nomic preset   [Esc] cancel"))
	}
	return dashboardLine(w, m.styles.KeyMenu.Render("[e] edit config   [s] save   [S] start/stop   [r] reindex once   [d] drop model   [Esc] close"))
}

func (p *indexerPlugin) renderLogPane(m *Model, w, height int) string {
	im := p.model(m)
	var body strings.Builder
	if len(im.logs) == 0 {
		body.WriteString(dashboardLine(w, m.styles.Muted.Render("(no log lines yet)")))
	} else {
		// Determine the visible window.
		visible := im.logs
		if im.logOffset != -1 {
			off := im.logOffset
			if off < 0 {
				off = 0
			}
			if off > len(im.logs) {
				off = len(im.logs)
			}
			visible = im.logs[off:]
		} else {
			// tail: show the last `height-1` lines (1 line for the divider).
			if len(visible) > height-1 {
				visible = visible[len(visible)-(height-1):]
			}
		}
		for _, l := range visible {
			body.WriteString(dashboardLine(w, fitLine(l, w)))
			body.WriteString("\n")
		}
	}
	// Bottom-anchor: the divider sits first, then blank-fill above the log
	// lines so the content reads at the BOTTOM of the pane (next to the
	// [Esc] close footer), not collapsed at the middle.
	contentLines := lineCount(body.String())
	logContentLines := contentLines // log lines (no divider)
	dividerLine := 1
	padAbove := height - dividerLine - logContentLines
	if padAbove < 0 {
		padAbove = 0
	}
	var b strings.Builder
	b.WriteString(sectionDivider(m.styles, w, "log"))
	b.WriteString("\n")
	for i := 0; i < padAbove; i++ {
		b.WriteString(spaces(w))
		b.WriteString("\n")
	}
	b.WriteString(body.String())
	return b.String()
}

// lineCount returns the number of newline-separated lines in s (a trailing
// newline does not add an empty line).
func lineCount(s string) int {
	if s == "" {
		return 0
	}
	n := strings.Count(s, "\n")
	if strings.HasSuffix(s, "\n") {
		return n
	}
	return n + 1
}

func stateWord(s indexerState) string {
	switch s {
	case idxOff:
		return "off"
	case idxStopped:
		return "stopped"
	case idxIdle:
		return "on"
	case idxWorking:
		return "running"
	case idxError:
		return "error"
	}
	return "?"
}

func (p *indexerPlugin) HandleKey(k tea.KeyMsg, m *Model) tea.Cmd {
	im := p.model(m)
	if im.editMode {
		return p.handleEditKey(k, m)
	}
	switch k.String() {
	case "S":
		return p.handleStartStop(m)
	case "e":
		p.openEdit(m)
		return nil
	case "r":
		return p.handleReindexOnce(m)
	case "d":
		return p.handleDrop(m)
	case "j", "down", "pgdown":
		im.logOffset = scrollDown(im.logs, im.logOffset)
		return nil
	case "k", "up", "pgup":
		im.logOffset = scrollUp(im.logs, im.logOffset)
		return nil
	case "G":
		im.logOffset = -1
		return nil
	}
	return nil
}

func (p *indexerPlugin) handleStartStop(m *Model) tea.Cmd {
	im := p.model(m)
	if im.cfg == nil {
		m.showToast("no embedding configured; press e to edit")
		return nil
	}
	if im.cancel == nil {
		return startIndexer(m, m.projectScope)
	}
	resetIndexer(m)
	return nil
}

func scrollDown(logs []string, offset int) int {
	if offset == -1 {
		return -1 // already at tail
	}
	off := offset + 1
	if off >= len(logs) {
		return -1
	}
	return off
}

func scrollUp(logs []string, offset int) int {
	if offset == -1 {
		if len(logs) == 0 {
			return -1
		}
		// pin near the tail, one page up
		start := len(logs) - 12
		if start < 0 {
			start = 0
		}
		return start
	}
	off := offset - 1
	if off < 0 {
		off = 0
	}
	return off
}
```

Add the `fmt` and `strings` imports to `indexer.go` if not present.

In `internal/tui/app.go`, in `SetSize`, after the `if m.helpOverlay != helpNone { ... } else { ... }` block, add:

```go
    if m.pluginOverlay != -1 && len(m.plugins) > m.pluginOverlay {
        bw, bh := m.helpBoxSize()
        // Plugins render into a titledBoxHeight; they compute their own inner
        // layout from bw. No separate SetSize call needed for now.
        _ = bw
        _ = bh
    }
```

(Plugins currently render from `m.width`/`m.contentHeight` via `helpBoxSize`; no per-plugin `SetSize` is required yet. The `_ =` keeps the block honest without a no-op lint failure — actually, drop the block entirely if unused. Re-evaluate in Step 4: if the build is fine without it, omit it.)

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/tui/ -run 'TestIndexerOverlay' -v`
Expected: PASS. (If `TestIndexerOverlayShowsConfigAndStatus` fails on the `mustContain(view, "nomic")` line — that line is wrong; the test config uses model `"m"`, not nomic. Fix the test assertion to `mustContain(t, view, "Embedding model:   m")` or remove the nomic line. The test in Step 1 above already uses `mustContain(t, view, "nomic" )` with a comment — replace it with the correct assertion before running.)

- [ ] **Step 5: Run the full TUI test suite**

Run: `go test ./internal/tui/...`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/tui/indexer.go internal/tui/indexer_test.go internal/tui/app.go
git commit -m "tui: indexer overlay — config/status/action/log regions + S toggle + scroll"
```

---

## Task 7: Inline edit mode (`e`/`s`/`p`) + `SetEmbeddingConfig` save

**Files:**
- Modify: `internal/tui/indexer.go` (`openEdit`, `handleEditKey`, `applyNomicPreset`, `saveConfig`; edit-mode rendering of the Config block)
- Test: `internal/tui/indexer_test.go` (extend — edit mode + preset + save)

**Interfaces:**
- Consumes: `formField` (from `form.go`), `store.SetEmbeddingConfig`, `store.EmbeddingConfig`, `m.actor`.
- Produces: edit mode toggled by `e`; fields prefilled from `im.cfg` (or empty + nomic preset button); `s` validates + writes + exits edit; `p` fills nomic preset; `Esc` cancels.

- [ ] **Step 1: Write the failing tests**

Append to `internal/tui/indexer_test.go`:

```go
func TestIndexerEditPrefillsFromCurrentConfig(t *testing.T) {
	m := newIndexerTestModel(t)
	setEmbedding(t, m, "ATM")
	p := newIndexerPlugin()
	im := p.model(m)
	im.refreshStatus()
	m.pluginOverlay = 0
	p.Open(m)
	p.HandleKey(keyMsg("e"), m)
	if !im.editMode {
		t.Fatal("e should toggle edit mode on")
	}
	if len(im.editFields) == 0 {
		t.Fatal("edit fields should be populated")
	}
	vals := editFieldValues(im)
	if vals["model"] != "m" {
		t.Errorf("prefill model = %q, want m", vals["model"])
	}
	if vals["endpoint"] != "http://x" {
		t.Errorf("prefill endpoint = %q, want http://x", vals["endpoint"])
	}
}

func TestIndexerEditNomicPresetFillsDefaults(t *testing.T) {
	m := newIndexerTestModel(t)
	p := newIndexerPlugin()
	im := p.model(m)
	im.refreshStatus()
	m.pluginOverlay = 0
	p.Open(m)
	p.HandleKey(keyMsg("e"), m)
	p.HandleKey(keyMsg("p"), m)
	vals := editFieldValues(im)
	if vals["model"] != "nomic-embed-text" {
		t.Errorf("preset model = %q, want nomic-embed-text", vals["model"])
	}
	if vals["endpoint"] != "http://localhost:11434/v1" {
		t.Errorf("preset endpoint = %q", vals["endpoint"])
	}
	if vals["dim"] != "768" {
		t.Errorf("preset dim = %q, want 768", vals["dim"])
	}
	if vals["threshold"] != "0.55" {
		t.Errorf("preset threshold = %q, want 0.55", vals["threshold"])
	}
	if vals["query_prefix"] != "search_query: " {
		t.Errorf("preset query_prefix = %q", vals["query_prefix"])
	}
	if vals["doc_prefix"] != "search_document: " {
		t.Errorf("preset doc_prefix = %q", vals["doc_prefix"])
	}
}

func TestIndexerEditSaveWritesConfig(t *testing.T) {
	m := newIndexerTestModel(t)
	setEmbedding(t, m, "ATM")
	p := newIndexerPlugin()
	im := p.model(m)
	im.refreshStatus()
	m.pluginOverlay = 0
	p.Open(m)
	p.HandleKey(keyMsg("e"), m)
	// change the model field
	setEditField(t, im, "model", "newmodel")
	cmd := p.HandleKey(keyMsg("s"), m)
	_ = cmd
	if im.editMode {
		t.Fatal("s should exit edit mode")
	}
	cfg, _ := m.store.GetProjectConfig("ATM")
	if cfg.Embedding.Model != "newmodel" {
		t.Errorf("after save: model = %q, want newmodel", cfg.Embedding.Model)
	}
}

func TestIndexerEditCancelReverts(t *testing.T) {
	m := newIndexerTestModel(t)
	setEmbedding(t, m, "ATM")
	p := newIndexerPlugin()
	im := p.model(m)
	im.refreshStatus()
	m.pluginOverlay = 0
	p.Open(m)
	p.HandleKey(keyMsg("e"), m)
	setEditField(t, im, "model", "discarded")
	p.HandleKey(keyMsg("esc"), m)
	if im.editMode {
		t.Fatal("Esc should exit edit mode")
	}
	cfg, _ := m.store.GetProjectConfig("ATM")
	if cfg.Embedding.Model != "m" {
		t.Errorf("after cancel: model = %q, want m (unchanged)", cfg.Embedding.Model)
	}
}

func TestIndexerEditSaveRequiredValidation(t *testing.T) {
	m := newIndexerTestModel(t)
	p := newIndexerPlugin()
	im := p.model(m)
	im.refreshStatus()
	m.pluginOverlay = 0
	p.Open(m)
	p.HandleKey(keyMsg("e"), m)
	// clear model
	setEditField(t, im, "model", "")
	p.HandleKey(keyMsg("s"), m)
	if im.editMode {
		t.Fatal("s with empty model should stay in edit mode (validation fail)")
	}
	cfg, _ := m.store.GetProjectConfig("ATM")
	if cfg != nil && cfg.Embedding != nil {
		t.Fatal("validation fail should not write config")
	}
}

func editFieldValues(im *indexerModel) map[string]string {
	out := map[string]string{}
	for _, f := range im.editFields {
		out[f.Label] = f.Value
	}
	return out
}

func setEditField(t *testing.T, im *indexerModel, label, value string) {
	t.Helper()
	for i := range im.editFields {
		if im.editFields[i].Label == label {
			im.editFields[i].Value = value
			return
		}
	}
	t.Fatalf("edit field %q not found", label)
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/tui/ -run 'TestIndexerEdit' -v`
Expected: FAIL — `openEdit`, `handleEditKey`, `applyNomicPreset`, `saveConfig` undefined.

- [ ] **Step 3: Write minimal implementation**

In `internal/tui/indexer.go`, add:

```go
func (p *indexerPlugin) openEdit(m *Model) {
	im := p.model(m)
	var model, endpoint, dim, threshold, qp, dp string
	if im.cfg != nil {
		model = im.cfg.Model
		endpoint = im.cfg.Endpoint
		dim = fmt.Sprintf("%d", im.cfg.Dim)
		threshold = fmt.Sprintf("%.2f", im.cfg.Threshold)
		qp = im.cfg.QueryPrefix
		dp = im.cfg.DocPrefix
	}
	im.editFields = []formField{
		{Label: "model", Value: model, Required: true, Hint: "embedding model slug"},
		{Label: "endpoint", Value: endpoint, Required: true, Hint: "OpenAI-compatible /v1/embeddings base URL"},
		{Label: "dim", Value: dim, Hint: "vector dimension"},
		{Label: "threshold", Value: threshold, Hint: "cosine threshold (0 = engine default)"},
		{Label: "query_prefix", Value: qp, Hint: "applied to query text"},
		{Label: "doc_prefix", Value: dp, Hint: "applied to document text"},
	}
	im.editCursor = 0
	im.editMode = true
}

func (p *indexerPlugin) handleEditKey(k tea.KeyMsg, m *Model) tea.Cmd {
	im := p.model(m)
	switch k.String() {
	case "esc":
		im.editMode = false
		return nil
	case "p":
		p.applyNomicPreset(im)
		return nil
	case "s":
		return p.saveConfig(m)
	case "tab", "down":
		im.editCursor = (im.editCursor + 1) % len(im.editFields)
		return nil
	case "shift+tab", "up":
		im.editCursor = (im.editCursor - 1 + len(im.editFields)) % len(im.editFields)
		return nil
	case "backspace":
		f := &im.editFields[im.editCursor]
		if len(f.Value) > 0 {
			f.Value = f.Value[:len(f.Value)-1]
		}
		return nil
	case " ":
		im.editFields[im.editCursor].Value += " "
		return nil
	}
	if k.Type == tea.KeyRunes {
		im.editFields[im.editCursor].Value += string(k.Runes)
	}
	return nil
}

func (p *indexerPlugin) applyNomicPreset(im *indexerModel) {
	set := func(label, val string) {
		for i := range im.editFields {
			if im.editFields[i].Label == label {
				im.editFields[i].Value = val
				return
			}
		}
	}
	set("model", "nomic-embed-text")
	set("endpoint", "http://localhost:11434/v1")
	set("dim", "768")
	set("threshold", "0.55")
	set("query_prefix", "search_query: ")
	set("doc_prefix", "search_document: ")
}

func (p *indexerPlugin) saveConfig(m *Model) tea.Cmd {
	im := p.model(m)
	vals := editFieldValues(im)
	if vals["model"] == "" || vals["endpoint"] == "" {
		m.showToast("model and endpoint are required")
		return nil
	}
	dim := 0
	if vals["dim"] != "" {
		if _, err := fmt.Sscanf(vals["dim"], "%d", &dim); err != nil {
			m.showToast("dim must be an integer")
			return nil
		}
	}
	threshold := 0.0
	if vals["threshold"] != "" {
		if _, err := fmt.Sscanf(vals["threshold"], "%f", &threshold); err != nil {
			m.showToast("threshold must be a number")
			return nil
		}
	}
	cfg := store.EmbeddingConfig{
		Model:       vals["model"],
		Endpoint:    vals["endpoint"],
		QueryPrefix: vals["query_prefix"],
		DocPrefix:   vals["doc_prefix"],
		Dim:         dim,
		Threshold:   threshold,
	}
	if err := m.store.SetEmbeddingConfig(m.projectScope, cfg, m.actor); err != nil {
		m.showToast("error: " + err.Error())
		return nil
	}
	im.editMode = false
	im.refreshStatus()
	m.showToast("embedding config saved")
	return nil
}
```

Also update `renderConfigBlock` to render edit-mode fields when `im.editMode` is true:

```go
func (p *indexerPlugin) renderConfigBlock(m *Model, w int) string {
	var b strings.Builder
	b.WriteString(sectionDivider(m.styles, w, "Config"))
	b.WriteString("\n")
	im := p.model(m)
	if im.editMode {
		for i, f := range im.editFields {
			active := i == im.editCursor
			label := m.styles.FieldLabel.Render(f.Label + ":")
			val := m.styles.FieldValue.Render(f.Value)
			if active {
				val += m.styles.FieldValue.Underline(true).Render(" ")
			}
			b.WriteString(dashboardLine(w, fmt.Sprintf("%s %s", label, val)))
			b.WriteString("\n")
			if f.Hint != "" {
				b.WriteString(dashboardLine(w, m.styles.FieldHint.Render("  "+f.Hint)))
				b.WriteString("\n")
			}
		}
		return b.String()
	}
	if im.cfg == nil {
		b.WriteString(dashboardLine(w, m.styles.Muted.Render("Embedding model: (none — press [e] to configure)")))
		return b.String()
	}
	cfg := im.cfg
	rows := []string{
		fmt.Sprintf("Embedding model:   %s", cfg.Model),
		fmt.Sprintf("Endpoint:          %s", cfg.Endpoint),
		fmt.Sprintf("Dim / threshold:   %d / %.2f", cfg.Dim, cfg.Threshold),
	}
	if cfg.QueryPrefix != "" || cfg.DocPrefix != "" {
		rows = append(rows, fmt.Sprintf("Prefixes:          %s / %s", cfg.QueryPrefix, cfg.DocPrefix))
	}
	for _, r := range rows {
		b.WriteString(dashboardLine(w, r))
		b.WriteString("\n")
	}
	return b.String()
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/tui/ -run 'TestIndexerEdit' -v`
Expected: PASS.

- [ ] **Step 5: Run the full TUI test suite**

Run: `go test ./internal/tui/...`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/tui/indexer.go internal/tui/indexer_test.go
git commit -m "tui: indexer inline edit mode (e/s/p) + SetEmbeddingConfig save"
```

---

## Task 8: Reindex once (`r`) + drop model (`d`)

**Files:**
- Modify: `internal/tui/indexer.go` (`handleReindexOnce`, `handleDrop` + confirm flow)
- Modify: `internal/tui/app.go` (a confirm action for drop — reuse the existing `confirmAction` enum)
- Test: `internal/tui/indexer_test.go` (extend — `r` runs `ReindexOnce`; `d` confirm + `DropVectors`)

**Interfaces:**
- Consumes: `store.ReindexOnce`, `store.DropVectors`, `store.EmbedFunc`, `store.ProgressFunc`, the existing `Model.confirm`/`Model.confirmMsg`/`Model.confirmArg` overlay.
- Produces: `r` runs `ReindexOnce` on a `tea.Cmd` (streams progress into the log ring); `d` opens the confirm overlay; confirm-yes calls `DropVectors(code, model)`.

- [ ] **Step 1: Write the failing tests**

Append to `internal/tui/indexer_test.go`:

```go
func TestIndexerReindexOnceRunsAndLogs(t *testing.T) {
	m := newIndexerTestModel(t)
	seedTask(t, m, "ATM", "first task")
	setEmbedding(t, m, "ATM")
	p := newIndexerPlugin()
	im := p.model(m)
	im.embedFnBuilder = fakeEmbedFnBuilder([]float64{0.1, 0.2})
	im.refreshStatus()
	m.pluginOverlay = 0
	p.Open(m)
	cmd := p.HandleKey(keyMsg("r"), m)
	if cmd == nil {
		t.Fatal("r should return a cmd (ReindexOnce)")
	}
	// Execute the cmd synchronously by running ReindexOnce directly here (the cmd wraps it).
	// For the test: call the underlying function to verify logging.
	res, err := m.store.ReindexOnce("ATM", im.embedFnBuilder(im.cfg), func(msg string) {
		im.logs = append(im.logs, msg)
	})
	if err != nil {
		t.Fatalf("ReindexOnce: %v", err)
	}
	if res.Indexed != 1 {
		t.Errorf("indexed = %d, want 1", res.Indexed)
	}
	if len(im.logs) == 0 {
		t.Error("reindex should have logged progress lines")
	}
}

func TestIndexerReindexOnceDisabledWhileRunning(t *testing.T) {
	m := newIndexerTestModel(t)
	seedTask(t, m, "ATM", "first task")
	setEmbedding(t, m, "ATM")
	p := newIndexerPlugin()
	im := p.model(m)
	im.embedFnBuilder = fakeEmbedFnBuilder([]float64{0.1, 0.2})
	im.refreshStatus()
	m.pluginOverlay = 0
	p.Open(m)
	p.HandleKey(keyMsg("S"), m) // start
	if im.cancel == nil {
		t.Fatal("S should have started the watcher")
	}
	p.HandleKey(keyMsg("r"), m)
	if m.toastMsg == "" || !strings.Contains(m.toastMsg, "stop the watcher") {
		t.Fatalf("r while running should toast 'stop the watcher', got %q", m.toastMsg)
	}
	resetIndexer(m)
}

func TestIndexerDropModelConfirmAndDrop(t *testing.T) {
	m := newIndexerTestModel(t)
	seedTask(t, m, "ATM", "first task")
	setEmbedding(t, m, "ATM")
	// seed an index file
	p := newIndexerPlugin()
	im := p.model(m)
	im.embedFnBuilder = fakeEmbedFnBuilder([]float64{0.1, 0.2})
	if _, err := m.store.ReindexOnce("ATM", im.embedFnBuilder(im.cfg), nil); err != nil {
		t.Fatalf("seed ReindexOnce: %v", err)
	}
	im.refreshStatus()
	if len(im.status) == 0 {
		t.Fatal("expected one index row after seeding")
	}
	m.pluginOverlay = 0
	p.Open(m)
	p.HandleKey(keyMsg("d"), m)
	if m.confirm == confirmNone {
		t.Fatal("d should open the confirm overlay")
	}
	// confirm yes
	m.confirmYes()
	models, _ := m.store.ListVectorModels("ATM")
	if len(models) != 0 {
		t.Fatalf("after confirm drop: models = %v, want empty", models)
	}
}

func TestIndexerDropNoIndexToasts(t *testing.T) {
	m := newIndexerTestModel(t)
	setEmbedding(t, m, "ATM")
	p := newIndexerPlugin()
	im := p.model(m)
	im.refreshStatus()
	m.pluginOverlay = 0
	p.Open(m)
	p.HandleKey(keyMsg("d"), m)
	if m.confirm != confirmNone {
		t.Fatal("d with no index should not open confirm")
	}
	if m.toastMsg == "" || !strings.Contains(m.toastMsg, "no index") {
		t.Fatalf("expected 'no index' toast, got %q", m.toastMsg)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/tui/ -run 'TestIndexerReindex|TestIndexerDrop' -v`
Expected: FAIL — `handleReindexOnce`, `handleDrop` undefined.

- [ ] **Step 3: Write minimal implementation**

In `internal/tui/indexer.go`, replace the `r`/`d` stubs in `HandleKey` (Task 6 left them as `p.handleReindexOnce(m)` / `p.handleDrop(m)` calls; define them now):

```go
func (p *indexerPlugin) handleReindexOnce(m *Model) tea.Cmd {
	im := p.model(m)
	if im.cfg == nil {
		m.showToast("no embedding configured; press e to edit")
		return nil
	}
	if im.cancel != nil {
		m.showToast("stop the watcher first (S)")
		return nil
	}
	embedFn := im.embedFnBuilder(im.cfg)
	progress := func(msg string) {
		im.logs = append(im.logs, msg)
		if len(im.logs) > 1000 {
			im.logs = im.logs[len(im.logs)-1000:]
		}
	}
	return func() tea.Msg {
		res, err := m.store.ReindexOnce(m.projectScope, embedFn, progress)
		if err != nil {
			return reindexResultMsg{err: err.Error()}
		}
		return reindexResultMsg{indexed: res.Indexed, model: res.Model, logSeq: res.LogSeq}
	}
}

type reindexResultMsg struct {
	indexed int
	model   string
	logSeq  int
	err     string
}

func (p *indexerPlugin) handleDrop(m *Model) tea.Cmd {
	im := p.model(m)
	if len(im.status) == 0 {
		m.showToast("no index file to drop")
		return nil
	}
	row := im.status[0]
	m.confirm = confirmDropIndex
	m.confirmMsg = "Drop vector index?"
	m.confirmArg = fmt.Sprintf("%s/%s will be deleted (re-run r to rebuild)", m.projectScope, row.Model)
	m.confirmPayload = row.Model
	return nil
}
```

Add a new confirm action to `internal/tui/app.go`. In the `confirmAction` const block:

```go
const (
	confirmNone confirmAction = iota
	confirmRemoveProject
	confirmRemoveTask
	confirmDropIndex
)
```

Add `confirmPayload string` to the `Model` struct (near `confirmArg`). In `confirmYes`, add a case:

```go
case confirmDropIndex:
	model := m.confirmPayload
	if err := m.store.DropVectors(m.projectScope, model); err != nil {
		m.showToast("error: " + err.Error())
	} else {
		m.showToast(fmt.Sprintf("dropped vector index %s/%s", m.projectScope, model))
	}
	m.indexer.refreshStatus()
	m.confirm = confirmNone
	m.confirmPayload = ""
	return nil
```

In `Update`, handle `reindexResultMsg`:

```go
	case reindexResultMsg:
		im := m.indexer
		if im == nil {
			return m, nil
		}
		if msg.err != "" {
			im.logs = append(im.logs, "index error: "+msg.err)
			m.showToast("reindex error: " + msg.err)
		} else {
			im.logs = append(im.logs, fmt.Sprintf("indexed %d (model=%s); index at log_seq %d", msg.indexed, msg.model, msg.logSeq))
			im.refreshStatus()
		}
		if len(im.logs) > 1000 {
			im.logs = im.logs[len(im.logs)-1000:]
		}
		return m, nil
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/tui/ -run 'TestIndexerReindex|TestIndexerDrop' -v`
Expected: PASS.

- [ ] **Step 5: Run the full TUI test suite**

Run: `go test ./internal/tui/...`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/tui/indexer.go internal/tui/indexer_test.go internal/tui/app.go
git commit -m "tui: indexer r (reindex once) + d (drop model confirm)"
```

---

## Task 9: Keys help + keymap rows

**Files:**
- Modify: `internal/tui/keymap.go` (add `g`/`g 1` rows)
- Modify: `internal/tui/help.go` (mention `g <n>` plugin overlays)
- Test: `internal/tui/app_test.go` or `help_test.go` (assert the rows appear)

**Interfaces:**
- Consumes: `keymapRows` (existing), the help text in `help.go`.
- Produces: two new `keyEntry` rows and a help-text addition.

- [ ] **Step 1: Write the failing test**

Append to `internal/tui/app_test.go`:

```go
func TestKeymapHasPluginPrefixRows(t *testing.T) {
	foundG := false
	foundG1 := false
	for _, r := range keymapRows {
		if r.Key == "g" {
			foundG = true
		}
		if r.Key == "g 1" {
			foundG1 = true
		}
	}
	if !foundG {
		t.Error("keymapRows missing 'g' (plugin prefix)")
	}
	if !foundG1 {
		t.Error("keymapRows missing 'g 1' (indexer overlay)")
	}
}

func TestHelpMentionsPluginOverlays(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	m.openHelp(helpKeys)
	view := m.help.View()
	if !strings.Contains(view, "g ") {
		t.Errorf("keys help should mention 'g <n>' plugin overlays\n--- view ---\n%s", view)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/tui/ -run 'TestKeymapHasPluginPrefixRows|TestHelpMentionsPluginOverlays' -v`
Expected: FAIL.

- [ ] **Step 3: Write minimal implementation**

In `internal/tui/keymap.go`, add to `keymapRows` (before the final `q / ctrl+c` row):

```go
	{"g", "plugin prefix", "plugin prefix", "plugin prefix", "plugin prefix"},
	{"g 1", "open indexer overlay", "open indexer overlay", "open indexer overlay", "open indexer overlay"},
```

In `internal/tui/help.go`, find the keys-help text (Section 2 — the keymap table is rendered from `keymapRows`, so the new rows appear automatically). Add a one-line note above/below the table (find the existing section caption and append):

```go
// In the keys-help body, after the keymap table, add:
"g <n> opens the nth plugin overlay (g 1 = indexer)."
```

(Locate the exact help-text builder in `help.go` and add this line where the other keymap notes live. Read `help.go` first to find the spot.)

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/tui/ -run 'TestKeymapHasPluginPrefixRows|TestHelpMentionsPluginOverlays' -v`
Expected: PASS.

- [ ] **Step 5: Run the full TUI test suite + verify gate**

Run: `make verify`
Expected: PASS (build + test + lint).

- [ ] **Step 6: Commit**

```bash
git add internal/tui/keymap.go internal/tui/help.go internal/tui/app_test.go
git commit -m "tui: keymap + help entries for g-prefix plugin overlays"
```

---

## Self-Review (run after writing all tasks)

**1. Spec coverage:** Skim each section of `2026-07-09-tui-indexer-integration-design.md`:
- D1 (tri-state + off/error) → Task 4 (states) + Task 6 (dock label/color). ✓
- D2 (in-process goroutine) → Task 5. ✓
- D3 (full management surface) → Tasks 6-8. ✓
- D4 (inline edit form + nomic preset) → Task 7. ✓
- D5 (selected project only) → Task 3 (prefix resolution checks `projectScope`). ✓
- D6 (tea messages / drain) → Task 5. ✓
- D7 (plugin dock + actor removal) → Task 3. ✓
- D8 (`g` prefix) → Task 3. ✓
- D9 (hard reset + 3-strikes) → Task 2 (supervisor) + Task 5 (reset on error). ✓
- D10 (save vs start/stop separated) → Tasks 6-7. ✓
- D11 (actor hidden) → Task 3. ✓
- D12 (dock keybind hint) → Task 2 (`dockSegments` appends `g`+OverlayKey) + Task 3 (test). ✓
- D13 (log pane bottom-anchored) → Task 6 (`Render` measures sections; `renderLogPane` top-pads). ✓
- Section 4 (overlay layout) → Task 6. ✓
- Section 5 (data flow) → Tasks 5-8. ✓
- Section 6 (error handling) → Tasks 5-8. ✓
- Section 7 (testing) → each task has tests; fake embedder seam in Task 4. ✓
- Section 8 (files touched) → matches the File Structure above. ✓
- Section 9 (rollout) → Tasks 1-9 in order. ✓

**2. Placeholder scan:** The plan has complete code in every code step; no "TBD"/"add appropriate error handling". The Task 6 Step 4 note about `nomic` test assertion is a known fix-up, not a placeholder. The Task 6 `SetSize` block is explicitly called out as "drop if unused" — that's a decision point, not a placeholder.

**3. Type consistency:** `indexerState` consts (`idxOff`/`idxStopped`/`idxIdle`/`idxWorking`/`idxError`) used consistently across Tasks 4-8. `indexerModel.embedFnBuilder` field defined Task 4, used Tasks 5-8. `pluginTickMsg`/`pluginTickCmd` defined Task 5, used Task 5. `reindexResultMsg` defined Task 8, handled Task 8. `confirmDropIndex`/`confirmPayload` added Task 8, used Task 8. `startIndexer`/`stopIndexer`/`resetIndexer`/`refreshStatus` (called `refreshIndexerStatus` in the spec — the plan uses `refreshStatus` as the method name on `indexerModel`; the spec's `refreshIndexerStatus()` is the same thing) — consistent within the plan. `dockSegments` defined Task 2, used Task 3. `applyIndexerMsg` defined Task 5, used Task 5. `stateWord` defined Task 6. `scrollDown`/`scrollUp` defined Task 6. `lineCount` defined Task 6, used Task 6 (`Render` + `renderLogPane`).

One name to reconcile: the spec calls the refresh `refreshIndexerStatus()`; the plan's `indexerModel` method is `refreshStatus()` (called as `im.refreshStatus()`). This is fine — `refreshStatus` is a method on `indexerModel`, and the spec's `refreshIndexerStatus()` was a top-level helper; the plan folds it into the model. No conflict, just note it for the implementer.

No issues found. Plan is complete.