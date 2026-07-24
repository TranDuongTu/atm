# TUI Art: Switchable Per-Project (default off) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax.

**Goal:** Make the per-project background art opt-in and keyboard-switchable: default none, a stable 2-theme pair per project, key `A` cycles none→art1→art2→none on the scoped project, art renders only when a project is selected, and the `atm project theme` CLI is removed.

**Architecture:** Builds on the completed ATM-4eae82 art feature (same worktree/branch). `art.Effective` stops falling back to a hashed theme (returns nil when unset); a new `art.Pair(code)` gives each project a stable 2-theme pool; a pure `nextArtTheme` helper drives the cycle; the `A` key in each workspace pane's handler mutates `ProjectConfig.ArtTheme` and refreshes the in-memory pin cache; `renderArt` gates on `projectScope`. The CLI command and its tests are deleted.

**Tech Stack:** Go, bubbletea/lipgloss, existing `internal/tui/art` package.

**Spec:** `docs/superpowers/specs/2026-07-23-tui-art-switchable-design.md`

## Global Constraints

- No new module dependencies. Deterministic art (phase-only animation) unchanged. No event-log entries for theme changes.
- Persistence stays `ProjectConfig.ArtTheme` (empty = none). Store method `SetProjectArtTheme` unchanged.
- `T` (UI palette) and `a` (add project / add task) keybindings are UNCHANGED. The new key is `A` (Shift+A), currently unbound.
- The tui and cli test suites are SLOW (>90s each) — run FOCUSED `-run` tests during tasks; the full suite runs once in the final task.
- Commit with EXPLICIT paths only — never `git add -A` (concurrent work in this repo). Commit prefix `feat(ATM-4eae82):` / `test(ATM-4eae82):` / `refactor(ATM-4eae82):`.

---

### Task 1: art package — `Pair`, nil-default `Effective`, remove `For`

**Files:**
- Modify: `internal/tui/art/art.go` (`Effective` ~127, remove `For` ~116-122, add `Pair`)
- Modify: `internal/tui/art/art_test.go` (update tests that used `For`/fallback; add `Pair` tests)

**Interfaces:**
- Consumes: existing `Seed`, `Names`, `ByName`, `registry`.
- Produces:
  - `func Pair(code string) [2]Theme` — two DISTINCT registered themes, deterministic per code, stable. Requires `len(registry) >= 2`.
  - `func Effective(pinned, code string) Theme` — returns `ByName(pinned)`'s theme if `pinned` names a registered theme, else `nil` (NO hash fallback). `code` is now unused by Effective but kept in the signature (callers pass it; avoids a churny signature change) — mark it `_ string` if the linter complains, or keep named and document it's reserved.
  - `For` is REMOVED.

- [ ] **Step 1: Write the failing tests**

Add to `internal/tui/art/art_test.go`:

```go
func TestEffectiveNoFallback(t *testing.T) {
	// Effective returns the named theme when valid, nil otherwise — no hash fallback.
	if Effective("waves", "ATM") == nil || Effective("waves", "ATM").Name() != "waves" {
		t.Fatal("valid pin must resolve to that theme")
	}
	if Effective("", "ATM") != nil {
		t.Fatal("empty pin must resolve to nil (none), not a hashed theme")
	}
	if Effective("bogus", "ATM") != nil {
		t.Fatal("unknown pin must resolve to nil, not a hashed theme")
	}
}

func TestPairIsStableDistinctAndVaried(t *testing.T) {
	p := Pair("ATM")
	if p[0] == nil || p[1] == nil {
		t.Fatal("pair must have two themes")
	}
	if p[0].Name() == p[1].Name() {
		t.Fatalf("pair themes must be distinct, got %q twice", p[0].Name())
	}
	// Stable per code.
	q := Pair("ATM")
	if q[0].Name() != p[0].Name() || q[1].Name() != p[1].Name() {
		t.Fatal("Pair must be stable for a given code")
	}
	// Both are registered themes.
	for _, th := range p {
		if _, ok := ByName(th.Name()); !ok {
			t.Fatalf("pair theme %q not in registry", th.Name())
		}
	}
	// Varies across codes (with 5 themes, these two codes should differ in at
	// least one slot; chosen so they do — adjust if the registry order changes).
	if Pair("ATM") == Pair("ZZZZZZ") {
		t.Skip("codes collided on identical pair; not a failure, just uninformative")
	}
}
```

Also FIND and update the existing test that exercised the old behavior: `grep -n "For(\|Effective(" internal/tui/art/art_test.go`. The Task-1-era `TestRegistryAndAssignment` asserted `For` stability and that `Effective("junk", code)` fell back to `For(code)`. Rewrite those assertions: remove the `For` calls; change the fallback expectation to `Effective("junk", code) == nil`. Do NOT delete unrelated assertions in that test (registry Names/ByName/Register).

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/tui/art/ -run 'TestEffectiveNoFallback|TestPairIsStable'`
Expected: FAIL — `Pair` undefined; `Effective("","ATM")` currently returns a hashed theme (not nil).

- [ ] **Step 3: Implement**

In `internal/tui/art/art.go`, replace `Effective` and delete `For`:

```go
// Effective resolves a pinned theme name to its theme, or nil when the pin is
// empty or names no registered theme. There is no auto-assignment: a project
// with no valid pin shows no art (the default-off model).
func Effective(pinned, code string) Theme {
	if t, ok := ByName(pinned); ok {
		return t
	}
	return nil
}

// Pair returns two distinct registered themes for a project code, sampled
// deterministically from the registry so a project always offers the same two
// (and different projects generally differ). Panics only if the registry has
// fewer than two themes, which never happens in a built binary (5 are
// registered in init).
func Pair(code string) [2]Theme {
	n := len(registry)
	s := Seed(code)
	i := int(s) % n
	j := int(s/uint32(n)) % (n - 1)
	if j >= i {
		j++
	}
	return [2]Theme{registry[i], registry[j]}
}
```

Delete the `For` function entirely. (`code` stays in `Effective`'s signature so the two render callers and any test don't need editing; it's intentionally unused now — add a one-line comment or rename to `_` if `go vet`/linters object. Prefer keeping it named with the doc note above.)

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/tui/art/...`
Expected: PASS (all art tests, including the rewritten registry/assignment test).

- [ ] **Step 5: Commit**

```bash
git add internal/tui/art/art.go internal/tui/art/art_test.go
git commit -m "feat(ATM-4eae82): art Pair() + Effective returns nil when unset, drop For()"
```

---

### Task 2: pure `nextArtTheme` cycle helper

**Files:**
- Modify: `internal/tui/projects.go` (add the helper near `renderArt`) OR create `internal/tui/art_switch.go` for it — pick whichever the reviewer of Task 3 will find natural; the helper is tui-package-level, not a method.
- Test: `internal/tui/art_switch_test.go` (new)

**Interfaces:**
- Consumes: `art.Theme` (for the pair type).
- Produces: `func nextArtTheme(pair [2]art.Theme, cur string) string` — the cycle: `"" → pair[0].Name() → pair[1].Name() → ""`; any value not matching none/pair0/pair1 → `""` (normalizes stale values to none).

- [ ] **Step 1: Write the failing test**

Create `internal/tui/art_switch_test.go`:

```go
package tui

import (
	"testing"

	"atm/internal/tui/art"
)

func TestNextArtThemeCycle(t *testing.T) {
	pair := art.Pair("ATM") // two real, distinct themes
	a, b := pair[0].Name(), pair[1].Name()

	if got := nextArtTheme(pair, ""); got != a {
		t.Fatalf("none -> %q, want %q", got, a)
	}
	if got := nextArtTheme(pair, a); got != b {
		t.Fatalf("%q -> %q, want %q", a, got, b)
	}
	if got := nextArtTheme(pair, b); got != "" {
		t.Fatalf("%q -> %q, want none", b, got)
	}
	// A stale value not in the pair normalizes to none.
	if got := nextArtTheme(pair, "not-in-pair"); got != "" {
		t.Fatalf("stale -> %q, want none", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tui/ -run TestNextArtThemeCycle`
Expected: FAIL — `nextArtTheme` undefined.

- [ ] **Step 3: Implement**

Add (in `internal/tui/projects.go` near `renderArt`, or a new `internal/tui/art_switch.go` with `package tui` and `import "atm/internal/tui/art"`):

```go
// nextArtTheme advances the art selection for a project through the cycle
// none -> pair[0] -> pair[1] -> none. A current value that is neither none nor
// a pair member (e.g. a stale pin) normalizes to none.
func nextArtTheme(pair [2]art.Theme, cur string) string {
	switch cur {
	case "":
		return pair[0].Name()
	case pair[0].Name():
		return pair[1].Name()
	default:
		return ""
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/tui/ -run TestNextArtThemeCycle`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/projects.go internal/tui/art_switch_test.go  # or art_switch.go instead of projects.go
git commit -m "feat(ATM-4eae82): nextArtTheme cycle helper (none->art1->art2->none)"
```

---

### Task 3: `A` key, scope-gated render, keymap

**Files:**
- Modify: `internal/tui/projects.go` (`renderArt` scope gate ~686; `A` case in `handleListKey` after the `case "a":` at ~432)
- Modify: `internal/tui/tasks_list.go` (`A` case in `handleListKey` after `case "a":` at ~107)
- Modify: `internal/tui/keymap.go` (add the `A` row)
- Modify: `internal/tui/app.go` (a shared `switchScopedArt` method on `*Model`, and confirm the toast setter name)
- Test: `internal/tui/art_switch_test.go` (extend) and update `internal/tui/art_wiring_test.go` (Task-8 render test now needs a theme set)

**Interfaces:**
- Consumes: `art.Pair`, `art.Effective`, `nextArtTheme`, `m.store.SetProjectArtTheme`, `m.artPins`, `m.actor`, the toast setter.
- Produces: `func (m *Model) switchScopedArt()` — cycles the scoped project's art, persists, updates the pin cache, flashes the status line; no-op (with a hint toast) when nothing is scoped.

- [ ] **Step 1: Confirm the toast setter and actor**

Run: `grep -n "func (m \*Model).*[Tt]oast\|m.toastMsg =" internal/tui/app.go`. Use the real setter (there is a `m.toastMsg = msg` at ~823 — find its enclosing method name, e.g. `setToast`/`flashToast`/`toast`; if there is no method, set `m.toastMsg` directly the way other call sites do). Mutations use `m.actor`.

- [ ] **Step 2: Write the failing tests**

Extend `internal/tui/art_switch_test.go` (uses the package model-test harness — mirror how `art_wiring_test.go` builds a model, seeds a project, and scopes it with `update(t, m, "s")`):

```go
func TestSwitchScopedArtCyclesAndPersists(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	seedProject(t, m, "ATM", "Acme")
	update(t, m, "s") // scope ATM
	if m.projectScope != "ATM" {
		t.Fatalf("scope = %q", m.projectScope)
	}
	pair := art.Pair("ATM")

	m.switchScopedArt()
	if m.artPins["ATM"] != pair[0].Name() {
		t.Fatalf("after 1st switch pin = %q want %q", m.artPins["ATM"], pair[0].Name())
	}
	// Persisted to config.
	cfg, _ := m.store.GetProjectConfig("ATM")
	if cfg == nil || cfg.ArtTheme != pair[0].Name() {
		t.Fatalf("config ArtTheme = %v want %q", cfg, pair[0].Name())
	}
	m.switchScopedArt()
	if m.artPins["ATM"] != pair[1].Name() {
		t.Fatalf("after 2nd switch pin = %q want %q", m.artPins["ATM"], pair[1].Name())
	}
	m.switchScopedArt()
	if m.artPins["ATM"] != "" {
		t.Fatalf("after 3rd switch pin = %q want none", m.artPins["ATM"])
	}
}

func TestSwitchScopedArtNoopWithoutScope(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	seedProject(t, m, "ATM", "Acme")
	// no scope
	m.switchScopedArt()
	if m.artPins["ATM"] != "" {
		t.Fatal("switch without scope must not change any pin")
	}
}

func TestRenderArtBlankWithoutScope(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(60, 40)
	seedProject(t, m, "ATM", "Acme")
	// Cursor is on ATM but nothing is scoped.
	out := m.projects.renderArt(8)
	if out != "" {
		t.Fatalf("renderArt without scope must be blank, got %q", out)
	}
}
```

Then UPDATE the Task-8 test `TestProjectsRenderListIncludesArtRegion` (in `art_wiring_test.go`): because default is now none, it must scope a project AND set a theme before asserting the art region is non-blank. Add after scoping: `m.store.SetProjectArtTheme("ATM", art.Pair("ATM")[0].Name(), m.actor)` then `m.refreshAll()` (to refresh `artPins`), or set `m.artPins["ATM"]` directly. Similarly check `art_wiring_test.go`'s other render assertions still hold under default-none and adjust.

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test ./internal/tui/ -run 'TestSwitchScopedArt|TestRenderArtBlankWithoutScope'`
Expected: FAIL — `switchScopedArt` undefined; `renderArt` still falls back to the cursor row (non-blank without scope).

- [ ] **Step 4: Implement**

In `internal/tui/projects.go`, gate `renderArt` on scope (remove the cursor fallback):

```go
func (p *projectsModel) renderArt(height int) string {
	code := p.m.projectScope
	if code == "" {
		return ""
	}
	theme := art.Effective(p.m.artPins[code], code)
	lines := art.Render(theme, p.width, height, art.Seed(code), p.m.artPhase,
		p.m.styles.ArtBase, p.m.styles.ArtAccent)
	if lines == nil {
		return ""
	}
	return strings.Join(lines, "\n")
}
```

In `internal/tui/app.go`, add the shared handler (use the real toast setter found in Step 1):

```go
// switchScopedArt cycles the scoped project's background art through
// none -> pair[0] -> pair[1] -> none, persists it, refreshes the in-memory
// pin cache, and flashes the result. No-op with a hint when nothing is scoped.
func (m *Model) switchScopedArt() {
	code := m.projectScope
	if code == "" {
		m.setToast("select a project first (s) to switch art") // use the real setter
		return
	}
	next := nextArtTheme(art.Pair(code), m.artPins[code])
	if err := m.store.SetProjectArtTheme(code, next, m.actor); err != nil {
		m.setToast("art: " + err.Error())
		return
	}
	if m.artPins == nil {
		m.artPins = map[string]string{}
	}
	m.artPins[code] = next
	if next == "" {
		m.setToast("art: none")
	} else {
		m.setToast("art: " + next)
	}
}
```

Import `atm/internal/tui/art` in app.go if not already imported. In `internal/tui/projects.go` `handleListKey`, after the `case "a":` block:

```go
	case "A":
		p.m.switchScopedArt()
```

In `internal/tui/tasks_list.go` `handleListKey`, after its `case "a":` block:

```go
	case "A":
		t.m.switchScopedArt()
```

In `internal/tui/keymap.go`, add a row (columns are Projects / Tasks / <third> / Detail — match the existing 5-column shape; put "switch art" in the Projects and Tasks columns):

```go
	{"A", "switch project art", "switch project art", "-", "-"},
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/tui/ -run 'TestSwitchScopedArt|TestRenderArtBlank|TestNextArtTheme|TestProjectsRenderList|TestArt'`
Expected: PASS, including the updated Task-8 render test.

- [ ] **Step 6: Commit**

```bash
git add internal/tui/projects.go internal/tui/tasks_list.go internal/tui/keymap.go internal/tui/app.go internal/tui/art_switch_test.go internal/tui/art_wiring_test.go
git commit -m "feat(ATM-4eae82): A key cycles scoped project art; render only when selected"
```

---

### Task 4: remove the `atm project theme` CLI

**Files:**
- Modify: `internal/cli/project.go` (delete `newProjectThemeCmd` and its `cmd.AddCommand(newProjectThemeCmd(st))` registration; remove the now-unused `atm/internal/tui/art` import if this was its only user in the file)
- Modify: `internal/cli/project_test.go` (delete `TestProjectTheme` and any theme-only helpers it introduced)

**Interfaces:** none produced; removes a command.

- [ ] **Step 1: Confirm what references the command**

Run: `grep -n "newProjectThemeCmd\|project theme\|TestProjectTheme\|art\\.Effective\|art\\.Names\|art\\.ByName" internal/cli/project.go internal/cli/project_test.go`. This is the exact set to remove. Note the store method `SetProjectArtTheme` and `ProjectConfig.ArtTheme` STAY (used by the TUI) — do not touch the store or core config.

- [ ] **Step 2: Remove the command and its tests**

Delete `func newProjectThemeCmd(...)` entirely, delete its `AddCommand` line in `newProjectCmd`, and delete `func TestProjectTheme(...)` from the test file. If `internal/tui/art` is no longer imported anywhere in `project.go`, remove the import line.

- [ ] **Step 3: Verify build and remaining project tests**

Run: `go build ./... && go vet ./internal/cli/... && go test ./internal/cli/ -run 'TestProject' -count=1`
Expected: clean build (no unused-import error), and the remaining `TestProject*` tests pass. `atm project theme` no longer exists.

- [ ] **Step 4: Commit**

```bash
git add internal/cli/project.go internal/cli/project_test.go
git commit -m "refactor(ATM-4eae82): remove atm project theme CLI; art switching is TUI-only"
```

---

### Task 5: full verification + changelog

**Files:**
- Modify: `CHANGELOG.md` (amend the ATM-4eae82 entry)

- [ ] **Step 1: Full build + vet + suite**

Run: `go build ./... && go vet ./... && go test ./...`
Expected: all green. Fix any regression before proceeding (likely candidates: an art or render test still assuming auto-assignment or the removed CLI).

- [ ] **Step 2: Live CLI + render sanity**

Confirm the command is gone and art still renders when set: build the binary, and against a throwaway `--store`, create a project, confirm `atm project theme ATM circuit` now errors ("unknown command"), and that the store still round-trips a theme via the TUI path (a focused `go test ./internal/tui/ -run TestSwitchScopedArt` already proves persistence). Optionally re-run the standalone art render preview to eyeball a theme.

- [ ] **Step 3: Update the changelog**

Amend the existing `ATM-4eae82` bullet in `CHANGELOG.md` to reflect the final behavior: art is off by default and switched per project with `A` (cycles none → two code-sampled themes → none), rendering only when a project is selected; there is no `atm project theme` CLI. Keep it one concise bullet consistent with the surrounding style.

- [ ] **Step 4: Commit**

```bash
git add CHANGELOG.md
git commit -m "docs(ATM-4eae82): changelog for switchable default-off art"
```

- [ ] **Step 5: Ledger**

Record completion on the revision task (and note it on ATM-4eae82): behavior revised to default-off + `A`-switch + selected-only render + CLI removed; full suite green. Advance the workflow_ai stage per its guide.
