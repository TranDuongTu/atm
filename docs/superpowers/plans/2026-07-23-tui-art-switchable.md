# TUI Art: Dual-pane pair, toggle per-project (default off) — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax.

**Goal:** Replace the five shipped art themes with six motion themes (galaxy, lorenz, matrix, tunnel, skyline, constellation). Each project gets a stable, code-derived pair of two distinct themes; when art is on for a scoped project, pair[0] renders in the Projects pane art slot and pair[1] in the Tasks pane gap. Art is off by default; a single key `A` toggles all art on/off for the scoped project (persisted). Art renders only when a project is selected (`projectScope != ""`), fixing the startup-art bug. The `atm project theme` CLI is removed.

**Architecture:** Builds on the completed ATM-4eae82 art feature (same worktree/branch). The registry is re-themed (six new files replace the five old ones); `art.For` is removed and `art.Effective` returns nil when unset (no hash fallback); a new `art.Pair(code)` gives each project a stable 2-theme pair. Persistence changes from `ProjectConfig.ArtTheme string` to `ProjectConfig.ArtOn bool`, and the store setter is renamed `SetProjectArtTheme` -> `SetProjectArtOn`. The TUI cache `m.artPins map[string]string` becomes `m.artOn map[string]bool`. `renderArt` resolves `Pair(scope)[0]` and `fillGapWithArt` resolves `Pair(scope)[1]`, both gated on scope + on. The `A` key in each pane handler calls a shared `toggleScopedArt` that flips the bool, persists, and flashes the status line. The CLI command and its tests are deleted.

**Tech Stack:** Go, bubbletea/lipgloss, existing `internal/tui/art` package.

**Spec:** `docs/superpowers/specs/2026-07-23-tui-art-switchable-design.md` (revised 2026-07-24).

## Global Constraints

- No new module dependencies. Deterministic art (phase-only animation) unchanged. No event-log entries for art changes.
- The `Frame`/`Theme`/`Render` pipeline and the 600ms idle-gated tick are unchanged; both panes share `m.artPhase` and `art.Seed(scope)`.
- `T` (UI palette) and `a` (add project / add task) keybindings are UNCHANGED. The new key is `A` (Shift+A), currently unbound.
- Schema change `art_theme` (string) -> `art_on` (bool) is in scope; no migration code is added (legacy `art_theme` is ignored on load).
- The tui and cli test suites are SLOW (>90s each) — run FOCUSED `-run` tests during tasks; the full suite runs once in the final task.
- Commit with EXPLICIT paths only — never `git add -A` (concurrent work in this repo). Commit prefix `feat(ATM-cac464):` / `test(ATM-cac464):` / `refactor(ATM-cac464):`.

---

### Task 1: art package — replace 5 themes with 6, `Pair`, nil-default `Effective`, remove `For`

**Files:**
- Delete: `internal/tui/art/circuit.go`, `internal/tui/art/ridges.go`, `internal/tui/art/specks.go` (the old waves/starfield/circuit/rain/dunes theme files) and their `_test.go` companions.
- Create: `internal/tui/art/galaxy.go`, `internal/tui/art/lorenz.go`, `internal/tui/art/matrix.go`, `internal/tui/art/tunnel.go`, `internal/tui/art/skyline.go`, `internal/tui/art/constellation.go` (port the theme `Draw` implementations from the `demo/main.go` prototypes in this worktree's `demo/` dir — they already target the real `art.Frame`/`CellHash`/`CellHashF` API).
- Modify: `internal/tui/art/art.go` (`init()` registry order lines 84-90; `Effective` ~127; remove `For` ~116-122; add `Pair`)
- Modify: `internal/tui/art/art_test.go` (rewrite registry/assignment tests for the 6 names; add `Pair` + nil-`Effective` tests)
- Delete: the old per-theme tests `circuit_test.go`, `ridges_test.go`, `specks_test.go` (their assertions name removed themes).

**Interfaces:**
- Consumes: existing `Seed`, `Names`, `ByName`, `registry`, `Frame`, `CellHash`, `CellHashF`.
- Produces:
  - Six `Theme` implementations registered in `init()` in this exact order: galaxy, lorenz, matrix, tunnel, skyline, constellation. Order is part of the `Pair` contract (append-only from here).
  - `func Pair(code string) [2]Theme` — two DISTINCT registered themes, deterministic per code, stable. Requires `len(registry) >= 2`.
  - `func Effective(pinned, code string) Theme` — returns `ByName(pinned)`'s theme if `pinned` names a registered theme, else `nil` (NO hash fallback). `code` is kept in the signature (callers pass it) but unused; document it as reserved.
  - `For` is REMOVED.

- [ ] **Step 1: Write the failing tests**

Add to `internal/tui/art/art_test.go`:

```go
func TestEffectiveNoFallback(t *testing.T) {
	if Effective("galaxy", "ATM") == nil || Effective("galaxy", "ATM").Name() != "galaxy" {
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
	q := Pair("ATM")
	if q[0].Name() != p[0].Name() || q[1].Name() != p[1].Name() {
		t.Fatal("Pair must be stable for a given code")
	}
	for _, th := range p {
		if _, ok := ByName(th.Name()); !ok {
			t.Fatalf("pair theme %q not in registry", th.Name())
		}
	}
}

func TestRegistryIsSixMotionThemes(t *testing.T) {
	want := []string{"galaxy", "lorenz", "matrix", "tunnel", "skyline", "constellation"}
	if got := Names(); !reflect.DeepEqual(got, want) {
		t.Fatalf("registry = %v, want %v", got, want)
	}
}
```

Also add a `TestThemesRenderWithoutPanic` that calls `Render` for each name at a few sizes (e.g. 60x8, 16x3, 10x2) and phases 0..3, asserting no panic and that below-`MinW`/`MinH` returns nil. FIND and update the existing `TestRegistryAndAssignment`-style test (grep `For(\|Effective(` in art_test.go): remove `For` calls and change the fallback expectation to `Effective("junk", code) == nil`.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/tui/art/ -run 'TestEffectiveNoFallback|TestPairIsStable|TestRegistryIsSix|TestThemesRender'`
Expected: FAIL — names don't match; `Pair` undefined; `Effective("","ATM")` currently returns a hashed theme.

- [ ] **Step 3: Implement**

Port the six theme `Draw` funcs from `demo/main.go` (this worktree) into their own files under `internal/tui/art/`, each `package art` with the same struct + `Name()` + `Draw` as in the demo. Delete `circuit.go`, `ridges.go`, `specks.go` and their tests. Update `init()` in `art.go` to register the six in order. Replace `Effective` and delete `For`:

```go
func Effective(pinned, code string) Theme {
	if t, ok := ByName(pinned); ok {
		return t
	}
	return nil
}

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

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/tui/art/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/art/
git commit -m "feat(ATM-cac464): replace 5 art themes with 6 motion themes; Pair() + nil Effective; drop For()"
```

---

### Task 2: persistence — `ArtTheme` -> `ArtOn bool`, store setter rename

**Files:**
- Modify: `internal/core/config.go` (`ArtTheme string` -> `ArtOn bool`, `json:"art_on,omitempty"`, lines ~37-40)
- Modify: `internal/core/service.go` (`SetProjectArtTheme` -> `SetProjectArtOn(code string, on bool, actor string) error`, line ~42)
- Modify: `internal/store/config.go` (the empty-guard `c.ArtTheme == ""` -> `!c.ArtOn` at line 18; rename `SetProjectArtTheme` -> `SetProjectArtOn` and change its `theme string` param to `on bool`, writing `merged.ArtOn = on`, lines ~177-196)
- Modify: `internal/store/config_test.go` (`TestSetProjectArtTheme` -> `TestSetProjectArtOn`; assert `ArtOn` true/false instead of a theme name; update the fixture/empty-guard test that checked `ArtTheme == ""` to check `!ArtOn`)
- Grep for any other `ArtTheme`/`SetProjectArtTheme` references outside the CLI (Task 4 removes the CLI ones) and update them.

**Interfaces:**
- Consumes: the existing read-modify-write-under-lock pattern of `SetProjectArtTheme`.
- Produces: `ProjectConfig.ArtOn bool` and `Store.SetProjectArtOn(code string, on bool, actor string) error`.

- [ ] **Step 1: Write the failing tests**

Rewrite `internal/store/config_test.go::TestSetProjectArtTheme`:

```go
func TestSetProjectArtOn(t *testing.T) {
	s := newTestStore(t)
	if err := s.SetProjectArtOn("ATM", true, testActor); err != nil {
		t.Fatal(err)
	}
	c, _ := s.GetProjectConfig("ATM")
	if c == nil || !c.ArtOn {
		t.Fatal("ArtOn = false, want true")
	}
	if err := s.SetProjectArtOn("ATM", false, testActor); err != nil {
		t.Fatal(err)
	}
	c, _ = s.GetProjectConfig("ATM")
	if c != nil && c.ArtOn {
		t.Fatalf("ArtOn still true after clear")
	}
}
```

Update the empty-guard fixture test (the one at ~344 that asserts `ArtTheme == ""` to prove the guard fires) to assert `!c.ArtOn` instead, and keep its structural intent (a config with only art set still reads as "empty" so a no-op write is skipped).

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/store/ -run TestSetProjectArtOn`
Expected: FAIL — `SetProjectArtOn` undefined; `ArtOn` field missing.

- [ ] **Step 3: Implement**

Rename the field and setter across `internal/core/config.go`, `internal/core/service.go`, `internal/store/config.go`. Change the empty-guard conjunct. Run `grep -rn "ArtTheme\|SetProjectArtTheme" internal/` and fix every non-CLI hit (CLI is Task 4).

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/store/... ./internal/core/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/core/config.go internal/core/service.go internal/store/config.go internal/store/config_test.go
git commit -m "refactor(ATM-cac464): persist art as ArtOn bool (was ArtTheme string); rename SetProjectArtOn"
```

---

### Task 3: TUI — dual-pane pair render, `A` toggle, keymap

**Files:**
- Modify: `internal/tui/app.go` (`artPins map[string]string` -> `artOn map[string]bool` at ~146; load `cfg.ArtOn` at ~325; add shared `toggleScopedArt` method)
- Modify: `internal/tui/projects.go` (`renderArt` ~686: gate on `projectScope != "" && m.artOn[scope]`, resolve `art.Pair(scope)[0]`)
- Modify: `internal/tui/tasks_list.go` (`fillGapWithArt` ~275: same gate, resolve `art.Pair(scope)[1]`; the `Effective` call at ~288 is replaced)
- Modify: `internal/tui/keymap.go` (add the `A` row: "toggle project art")
- Test: `internal/tui/art_switch_test.go` (new) and update `internal/tui/art_wiring_test.go` (Task-8 render tests now need art ON + scope before asserting non-blank art)

**Interfaces:**
- Consumes: `art.Pair`, `art.Render`, `m.store.SetProjectArtOn`, `m.artOn`, `m.actor`, the toast setter.
- Produces: `func (m *Model) toggleScopedArt()` — flips `artOn[scope]`, persists, refreshes the cache, flashes "art: on"/"art: off"; no-op with a hint when nothing is scoped.

- [ ] **Step 1: Confirm the toast setter and actor**

Run: `grep -n "func (m \*Model).*[Tt]oast\|m.toastMsg =" internal/tui/app.go`. Use the real setter (or set `m.toastMsg` directly the way other call sites do). Mutations use `m.actor`.

- [ ] **Step 2: Write the failing tests**

Create `internal/tui/art_switch_test.go` (mirror how `art_wiring_test.go` builds a model, seeds a project, scopes it with `update(t, m, "s")`):

```go
func TestToggleScopedArtFlipsAndPersists(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	seedProject(t, m, "ATM", "Acme")
	update(t, m, "s") // scope ATM
	pair := art.Pair("ATM")

	m.toggleScopedArt()
	if !m.artOn["ATM"] {
		t.Fatal("after toggle, artOn[ATM] = false, want true")
	}
	cfg, _ := m.store.GetProjectConfig("ATM")
	if cfg == nil || !cfg.ArtOn {
		t.Fatal("config ArtOn not persisted true")
	}
	m.toggleScopedArt()
	if m.artOn["ATM"] {
		t.Fatal("after 2nd toggle, artOn[ATM] = true, want false")
	}
}

func TestToggleScopedArtNoopWithoutScope(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(120, 40)
	seedProject(t, m, "ATM", "Acme")
	m.toggleScopedArt()
	if m.artOn["ATM"] {
		t.Fatal("toggle without scope must not set artOn")
	}
}

func TestRenderArtBlankWhenOff(t *testing.T) {
	m := newTestModel(t)
	m.SetSize(60, 40)
	seedProject(t, m, "ATM", "Acme")
	update(t, m, "s")
	// art off by default
	if out := m.projects.renderArt(8); out != "" {
		t.Fatalf("renderArt with art off must be blank, got non-empty")
	}
}
```

Update `art_wiring_test.go::TestProjectsRenderListIncludesArtRegion` and the Tasks-pane gap test: because default is now off, they must scope a project AND set art on before asserting the art region is non-blank. Add after scoping: `m.store.SetProjectArtOn("ATM", true, m.actor)` then refresh (or set `m.artOn["ATM"] = true` directly), and assert that the pane shows `Pair("ATM")[0]` / `[1]` content (non-blank lines).

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test ./internal/tui/ -run 'TestToggleScopedArt|TestRenderArtBlankWhenOff'`
Expected: FAIL — `toggleScopedArt`/`artOn` undefined; `renderArt` still resolves via the old `Effective(artPins[code], code)`.

- [ ] **Step 4: Implement**

In `internal/tui/app.go`, rename the cache field and its loader:

```go
artOn map[string]bool
// in the refresh:
on := map[string]bool{}
// ... for each listed project:
if cfg.ArtOn { on[r.code] = true }
m.artOn = on
```

In `internal/tui/projects.go`, gate `renderArt` and resolve pair[0]:

```go
func (p *projectsModel) renderArt(height int) string {
	code := p.m.projectScope
	if code == "" || !p.m.artOn[code] {
		return ""
	}
	theme := art.Pair(code)[0]
	lines := art.Render(theme, p.width, height, art.Seed(code), p.m.artPhase,
		p.m.styles.ArtBase, p.m.styles.ArtAccent)
	if lines == nil {
		return ""
	}
	return strings.Join(lines, "\n")
}
```

In `internal/tui/tasks_list.go` `fillGapWithArt`, the same gate + `art.Pair(code)[1]` (replace the `Effective` call at ~288).

In `internal/tui/app.go`, add:

```go
func (m *Model) toggleScopedArt() {
	code := m.projectScope
	if code == "" {
		m.setToast("select a project first (s) to toggle art")
		return
	}
	next := !m.artOn[code]
	if err := m.store.SetProjectArtOn(code, next, m.actor); err != nil {
		m.setToast("art: " + err.Error())
		return
	}
	if m.artOn == nil {
		m.artOn = map[string]bool{}
	}
	m.artOn[code] = next
	if next {
		m.setToast("art: on")
	} else {
		m.setToast("art: off")
	}
}
```

Add `case "A": p.m.toggleScopedArt()` in `projects.go` `handleListKey` (after `case "a":`), and `case "A": t.m.toggleScopedArt()` in `tasks_list.go` `handleListKey`. In `keymap.go` add `{"A", "toggle project art", "toggle project art", "-", "-"}`.

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/tui/ -run 'TestToggleScopedArt|TestRenderArtBlank|TestProjectsRenderList|TestArt'`
Expected: PASS, including the updated render tests.

- [ ] **Step 6: Commit**

```bash
git add internal/tui/projects.go internal/tui/tasks_list.go internal/tui/keymap.go internal/tui/app.go internal/tui/art_switch_test.go internal/tui/art_wiring_test.go
git commit -m "feat(ATM-cac464): A toggles scoped project art; render Pair()[0]/[1] in the two panes when on"
```

---

### Task 4: remove the `atm project theme` CLI

**Files:**
- Modify: `internal/cli/project.go` (delete `newProjectThemeCmd` and its `cmd.AddCommand(newProjectThemeCmd(st))` registration; remove the now-unused `atm/internal/tui/art` import if this was its only user in the file)
- Modify: `internal/cli/project_test.go` (delete `TestProjectTheme` and any theme-only helpers it introduced)

**Interfaces:** none produced; removes a command.

- [ ] **Step 1: Confirm what references the command**

Run: `grep -n "newProjectThemeCmd\|project theme\|TestProjectTheme\|art\\.Effective\|art\\.Names\|art\\.ByName" internal/cli/project.go internal/cli/project_test.go`. This is the exact set to remove. The store method (now `SetProjectArtOn`) and `ProjectConfig.ArtOn` STAY — do not touch them here.

- [ ] **Step 2: Remove the command and its tests**

Delete `func newProjectThemeCmd(...)` entirely, delete its `AddCommand` line in `newProjectCmd`, and delete `func TestProjectTheme(...)` from the test file. If `internal/tui/art` is no longer imported in `project.go`, remove the import line.

- [ ] **Step 3: Verify build and remaining project tests**

Run: `go build ./... && go vet ./internal/cli/... && go test ./internal/cli/ -run 'TestProject' -count=1`
Expected: clean build (no unused-import error), remaining `TestProject*` pass. `atm project theme` no longer exists.

- [ ] **Step 4: Commit**

```bash
git add internal/cli/project.go internal/cli/project_test.go
git commit -m "refactor(ATM-cac464): remove atm project theme CLI; art switching is TUI-only"
```

---

### Task 5: full verification + changelog

**Files:**
- Modify: `CHANGELOG.md` (amend the ATM-4eae82 / add ATM-cac464 entry per repo style)

- [ ] **Step 1: Full build + vet + suite**

Run: `go build ./... && go vet ./... && go test ./...`
Expected: all green. Fix any regression before proceeding (likely candidates: an art or render test still assuming auto-assignment, the old theme names, or the removed CLI). Then run `make verify`.

- [ ] **Step 2: Live CLI + render sanity**

Confirm the command is gone and art still renders when toggled: build the binary, and against a throwaway `--store`, create a project, confirm `atm project theme ATM galaxy` now errors ("unknown command"), and that a focused `go test ./internal/tui/ -run TestToggleScopedArt` proves persistence. Optionally re-run the standalone art render preview to eyeball a theme.

- [ ] **Step 3: Update the changelog**

Add/amend a concise bullet consistent with the surrounding style: art is off by default; each project has a stable code-derived pair of two of the six motion themes; `A` toggles all art on/off for the scoped project; pair[0] renders in the Projects pane, pair[1] in the Tasks pane; rendering only when a project is selected; there is no `atm project theme` CLI.

- [ ] **Step 4: Commit**

```bash
git add CHANGELOG.md
git commit -m "docs(ATM-cac464): changelog for dual-pane toggle art"
```

- [ ] **Step 5: Ledger**

Record completion on ATM-cac464: behavior revised to default-off + `A`-toggle + dual-pane pair render + selected-only render + six motion themes + CLI removed; full suite green; `make verify` clean. Advance the workflow_ai stage per its guide.