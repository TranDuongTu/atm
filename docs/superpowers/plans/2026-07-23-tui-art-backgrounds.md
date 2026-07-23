# TUI Per-Project Procedural Art Backgrounds Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fill the dead vertical space in the Projects and Tasks panes with per-project procedural ASCII art that gives each project a stable visual identity.

**Architecture:** A new `internal/tui/art` package draws glyph `Frame`s (rune grid + accent mask) via five procedural `Theme` implementations, blitted through the existing ntcharts canvas with palette-derived dim/accent styles. Auto-assignment is `fnv32(code) % len(registry)`; an optional pin lives in `ProjectConfig.ArtTheme` (RMW like boards config) with a new `atm project theme` CLI verb. The Projects pane becomes a 4-way split (list fixed at 5 rows → art flex → events → summary); the Tasks pane replaces its trailing blank padding above the boards ring with art.

**Tech Stack:** Go, bubbletea v1.2.2, lipgloss v1.0.0, ntcharts canvas (all already vendored — no new dependencies).

**Spec:** `docs/superpowers/specs/2026-07-23-tui-art-backgrounds-design.md` (task ATM-4eae82). Two refinements vs the spec, decided here: `phase` is an `int` tick counter (not float64) so tests are exact, and themes draw into a plain `Frame` grid that `Render` blits to the canvas — same behavior, trivially testable.

## Global Constraints

- No new module dependencies; imports limited to stdlib, lipgloss, ntcharts canvas.
- Drawing is deterministic: no `math/rand`, no `time.Now` in any draw path. Variation comes only from `seed`, animation only from `phase`.
- Art collapse thresholds: render nothing below **3 lines or 16 columns** (`art.MinH`, `art.MinW`).
- Theme registry order is meaningful (auto-assign is `hash % len`); append new themes at the end.
- No store event-log entries for theme changes — display preference, mirroring `BoardsConfig`.
- `T` keybinding is untouched (stays UI-palette cycling).
- Commit prefix: `feat(ATM-4eae82): …` / `test(ATM-4eae82): …`. Stage explicit paths only — **never `git add -A`** (other sessions edit this worktree concurrently).
- Run package tests with `go test ./internal/<pkg>/...` from the repo root.

---

### Task 1: Art engine — Frame, Theme interface, registry, Render

**Files:**
- Create: `internal/tui/art/art.go`
- Test: `internal/tui/art/art_test.go`

**Interfaces:**
- Consumes: nothing (leaf package).
- Produces (used by Tasks 2–4, 6, 8, 9):
  - `type Frame struct` with `NewFrame(w, h int) *Frame`, methods `W() int`, `H() int`, `Set(x, y int, r rune)`, `SetAccent(x, y int, r rune)`, `At(x, y int) rune`, `IsAccent(x, y int) bool`. Out-of-bounds Set/At are safe no-ops / return `' '`.
  - `type Theme interface { Name() string; Draw(f *Frame, seed uint32, phase int) }`
  - `func Register(t Theme)` (called from theme files' `init()`), `func Names() []string`, `func ByName(name string) (Theme, bool)`
  - `func For(code string) Theme` — fnv32a(code) % len(registry); nil if registry empty.
  - `func Effective(pinned, code string) Theme` — `ByName(pinned)` if valid, else `For(code)`.
  - `func Seed(code string) uint32` — fnv32a of the code (shared by For and callers).
  - `func Render(t Theme, w, h int, seed uint32, phase int, base, accent lipgloss.Style) []string` — nil when `t == nil || w < MinW || h < MinH`; otherwise **exactly h** styled lines, each w cells wide.
  - `const MinW = 16`, `const MinH = 3`
  - `func CellHash(x, y int, seed uint32) uint32` and `func CellHashF(x, y int, seed uint32) float64` (in [0,1)) — shared PRN helpers for themes.

- [ ] **Step 1: Write the failing tests**

```go
package art

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

// stubTheme fills the whole frame with 'x', accenting (0,0).
type stubTheme struct{ name string }

func (s stubTheme) Name() string { return s.name }
func (s stubTheme) Draw(f *Frame, seed uint32, phase int) {
	for y := 0; y < f.H(); y++ {
		for x := 0; x < f.W(); x++ {
			f.Set(x, y, 'x')
		}
	}
	f.SetAccent(0, 0, 'A')
}

func plain() (lipgloss.Style, lipgloss.Style) {
	return lipgloss.NewStyle(), lipgloss.NewStyle()
}

func TestFrameBoundsAreSafe(t *testing.T) {
	f := NewFrame(4, 3)
	f.Set(-1, 0, 'z')
	f.Set(0, -1, 'z')
	f.Set(4, 0, 'z')
	f.Set(0, 3, 'z')
	f.SetAccent(99, 99, 'z')
	if got := f.At(2, 1); got != ' ' {
		t.Fatalf("empty cell = %q, want space", got)
	}
	if f.At(-5, 0) != ' ' || f.At(0, 99) != ' ' {
		t.Fatal("out-of-bounds At must return space")
	}
	f.Set(2, 1, 'q')
	if f.At(2, 1) != 'q' {
		t.Fatal("Set/At round-trip failed")
	}
	if f.IsAccent(2, 1) {
		t.Fatal("Set must not mark accent")
	}
	f.SetAccent(2, 1, 'p')
	if !f.IsAccent(2, 1) || f.At(2, 1) != 'p' {
		t.Fatal("SetAccent must set rune and mark accent")
	}
}

func TestRenderDimensionsAndMinimums(t *testing.T) {
	base, accent := plain()
	st := stubTheme{name: "stub"}
	if Render(nil, 40, 8, 1, 0, base, accent) != nil {
		t.Fatal("nil theme must render nil")
	}
	if Render(st, MinW-1, 8, 1, 0, base, accent) != nil {
		t.Fatal("narrow render must be nil")
	}
	if Render(st, 40, MinH-1, 1, 0, base, accent) != nil {
		t.Fatal("short render must be nil")
	}
	lines := Render(st, 40, 8, 1, 0, base, accent)
	if len(lines) != 8 {
		t.Fatalf("got %d lines, want 8", len(lines))
	}
	for i, ln := range lines {
		if w := lipgloss.Width(ln); w != 40 {
			t.Fatalf("line %d width = %d, want 40", i, w)
		}
	}
}

func TestRenderIsDeterministic(t *testing.T) {
	base, accent := plain()
	st := stubTheme{name: "stub"}
	a := strings.Join(Render(st, 30, 5, 7, 3, base, accent), "\n")
	b := strings.Join(Render(st, 30, 5, 7, 3, base, accent), "\n")
	if a != b {
		t.Fatal("same inputs must render identical output")
	}
}

func TestRegistryAndAssignment(t *testing.T) {
	old := registry
	defer func() { registry = old }()
	registry = nil
	if For("ATM") != nil {
		t.Fatal("empty registry must yield nil theme")
	}
	Register(stubTheme{name: "alpha"})
	Register(stubTheme{name: "beta"})
	if got := Names(); len(got) != 2 || got[0] != "alpha" || got[1] != "beta" {
		t.Fatalf("Names() = %v", got)
	}
	if _, ok := ByName("nope"); ok {
		t.Fatal("unknown name must not resolve")
	}
	// Stability: same code always picks the same theme.
	first := For("ATM").Name()
	for i := 0; i < 10; i++ {
		if For("ATM").Name() != first {
			t.Fatal("For must be stable per code")
		}
	}
	// Pin resolution: valid pin wins, invalid pin falls back to auto.
	if Effective("beta", "ATM").Name() != "beta" {
		t.Fatal("valid pin must win")
	}
	if Effective("junk", "ATM").Name() != first {
		t.Fatal("invalid pin must fall back to For(code)")
	}
	if Effective("", "ATM").Name() != first {
		t.Fatal("empty pin must fall back to For(code)")
	}
}

func TestCellHashDistributionBasics(t *testing.T) {
	if CellHash(1, 2, 3) != CellHash(1, 2, 3) {
		t.Fatal("CellHash must be deterministic")
	}
	if CellHash(1, 2, 3) == CellHash(2, 1, 3) {
		t.Fatal("CellHash should differ across transposed coords")
	}
	v := CellHashF(5, 5, 42)
	if v < 0 || v >= 1 {
		t.Fatalf("CellHashF out of [0,1): %f", v)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/tui/art/...`
Expected: FAIL — package does not exist / undefined symbols.

- [ ] **Step 3: Implement `internal/tui/art/art.go`**

```go
// Package art renders per-project procedural background art for the TUI's
// spare vertical space. Themes draw glyphs into a Frame (rune grid + accent
// mask); Render blits the frame through the ntcharts canvas with the two
// palette-derived styles. All drawing is deterministic: layout varies only
// with seed, animation only with phase.
package art

import (
	"hash/fnv"

	"github.com/NimbleMarkets/ntcharts/canvas"
	"github.com/charmbracelet/lipgloss"
)

// Minimum region size worth drawing; below this the caller keeps blank
// padding (spec: collapse threshold).
const (
	MinW = 16
	MinH = 3
)

// Frame is a w×h glyph grid with an accent mask. Zero cells are spaces.
type Frame struct {
	w, h   int
	cells  [][]rune
	accent [][]bool
}

func NewFrame(w, h int) *Frame {
	cells := make([][]rune, h)
	accent := make([][]bool, h)
	for y := range cells {
		cells[y] = make([]rune, w)
		accent[y] = make([]bool, w)
		for x := range cells[y] {
			cells[y][x] = ' '
		}
	}
	return &Frame{w: w, h: h, cells: cells, accent: accent}
}

func (f *Frame) W() int { return f.w }
func (f *Frame) H() int { return f.h }

func (f *Frame) in(x, y int) bool { return x >= 0 && x < f.w && y >= 0 && y < f.h }

func (f *Frame) Set(x, y int, r rune) {
	if f.in(x, y) {
		f.cells[y][x] = r
		f.accent[y][x] = false
	}
}

func (f *Frame) SetAccent(x, y int, r rune) {
	if f.in(x, y) {
		f.cells[y][x] = r
		f.accent[y][x] = true
	}
}

func (f *Frame) At(x, y int) rune {
	if !f.in(x, y) {
		return ' '
	}
	return f.cells[y][x]
}

func (f *Frame) IsAccent(x, y int) bool { return f.in(x, y) && f.accent[y][x] }

// Theme draws one procedural motif at the frame's exact size.
type Theme interface {
	Name() string
	Draw(f *Frame, seed uint32, phase int)
}

// registry order is part of the auto-assign contract (hash % len): the same
// code maps to the same theme for a fixed registry. Append only.
var registry []Theme

func Register(t Theme) { registry = append(registry, t) }

func Names() []string {
	out := make([]string, 0, len(registry))
	for _, t := range registry {
		out = append(out, t.Name())
	}
	return out
}

func ByName(name string) (Theme, bool) {
	for _, t := range registry {
		if t.Name() == name {
			return t, true
		}
	}
	return nil, false
}

// Seed derives the stable per-project variation seed from the project code.
func Seed(code string) uint32 {
	h := fnv.New32a()
	h.Write([]byte(code))
	return h.Sum32()
}

// For auto-assigns a theme to a project code. Stable for a fixed registry.
func For(code string) Theme {
	if len(registry) == 0 {
		return nil
	}
	return registry[int(Seed(code))%len(registry)]
}

// Effective resolves a pinned theme name, falling back to auto-assignment
// when the pin is empty or names nothing (renamed theme, typo). Never errors:
// display preference is not worth failing a render over.
func Effective(pinned, code string) Theme {
	if t, ok := ByName(pinned); ok {
		return t
	}
	return For(code)
}

// CellHash is the shared per-cell PRN for themes: deterministic, cheap, and
// decorrelated across neighboring cells.
func CellHash(x, y int, seed uint32) uint32 {
	h := uint32(x)*374761393 + uint32(y)*668265263 + seed*2246822519
	h = (h ^ (h >> 13)) * 1274126177
	return h ^ (h >> 16)
}

// CellHashF maps CellHash into [0,1).
func CellHashF(x, y int, seed uint32) float64 {
	return float64(CellHash(x, y, seed)%1000) / 1000.0
}

// Render draws the theme at w×h and returns exactly h styled lines (base
// style for ordinary cells, accent for accent-marked ones). Returns nil when
// there is no theme or the region is below the collapse threshold.
func Render(t Theme, w, h int, seed uint32, phase int, base, accent lipgloss.Style) []string {
	if t == nil || w < MinW || h < MinH {
		return nil
	}
	f := NewFrame(w, h)
	t.Draw(f, seed, phase)
	c := canvas.New(w, h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			st := base
			if f.IsAccent(x, y) {
				st = accent
			}
			c.SetRuneWithStyle(canvas.Point{X: x, Y: y}, f.At(x, y), st)
		}
	}
	lines := make([]string, 0, h)
	for _, ln := range splitLines(c.View()) {
		lines = append(lines, ln)
	}
	for len(lines) < h {
		lines = append(lines, "")
	}
	return lines[:h]
}

func splitLines(s string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	return append(out, s[start:])
}
```

Note: check `canvas.New`'s return type before wiring (`grep -n "canvas.New" internal/tui/projects.go`) — the existing caller uses it as a value (`c := canvas.New(...); c.SetRuneWithStyle(...)`); mirror that usage exactly.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/tui/art/...`
Expected: PASS (all 5 tests).

- [ ] **Step 5: Commit**

```bash
git add internal/tui/art/art.go internal/tui/art/art_test.go
git commit -m "feat(ATM-4eae82): art engine - Frame, Theme registry, Render"
```

---

### Task 2: Themes `waves` and `dunes` (ridge family)

**Files:**
- Create: `internal/tui/art/ridges.go`
- Test: `internal/tui/art/ridges_test.go`

**Interfaces:**
- Consumes: `Frame`, `Register`, `CellHashF` from Task 1.
- Produces: registry entries `"waves"` (index 0) and `"dunes"` (index 4 after all registrations — see registration ordering note below).

**Registration ordering note (applies to Tasks 2–4):** the registry must end up in spec order `waves, starfield, circuit, rain, dunes`. Per-file `init()` functions would make the order depend on filename initialization order — too fragile for something that is the auto-assign contract. So: **theme files define types only; `art.go` owns a single explicit registration block:**

```go
// in art.go — replaces per-file init() registration; order is the
// auto-assign contract (append only).
func init() {
	Register(wavesTheme{})
	Register(starfieldTheme{})
	Register(circuitTheme{})
	Register(rainTheme{})
	Register(dunesTheme{})
}
```

Since `starfieldTheme`/`circuitTheme`/`rainTheme` don't exist until Tasks 3–4, add this block **incrementally**: in this task register only `wavesTheme{}` and `dunesTheme{}` with a `// TODO(ATM-4eae82): insert starfield, circuit, rain here in spec order` comment; Tasks 3 and 4 insert their lines in the spec positions and Task 4 removes the comment. Final order must be exactly: waves, starfield, circuit, rain, dunes.

- [ ] **Step 1: Write the failing tests**

```go
package art

import "testing"

// drawCount returns how many cells differ between two frames.
func drawCount(a, b *Frame) int {
	n := 0
	for y := 0; y < a.H(); y++ {
		for x := 0; x < a.W(); x++ {
			if a.At(x, y) != b.At(x, y) || a.IsAccent(x, y) != b.IsAccent(x, y) {
				n++
			}
		}
	}
	return n
}

// nonBlank returns how many cells are not spaces.
func nonBlank(f *Frame) int {
	n := 0
	for y := 0; y < f.H(); y++ {
		for x := 0; x < f.W(); x++ {
			if f.At(x, y) != ' ' {
				n++
			}
		}
	}
	return n
}

func frameOf(t Theme, w, h int, seed uint32, phase int) *Frame {
	f := NewFrame(w, h)
	t.Draw(f, seed, phase)
	return f
}

// assertThemeContract checks the properties every theme must hold:
// determinism, seed variation, bounded animation delta, and drawing
// something at both a typical and a cramped size.
func assertThemeContract(t *testing.T, th Theme) {
	t.Helper()
	// Draws something recognizable at typical and cramped sizes.
	for _, size := range [][2]int{{44, 8}, {30, 4}} {
		w, h := size[0], size[1]
		a := frameOf(th, w, h, 7, 0)
		if nonBlank(a) == 0 {
			t.Fatalf("%s draws nothing at %dx%d", th.Name(), w, h)
		}
		b := frameOf(th, w, h, 7, 0)
		if drawCount(a, b) != 0 {
			t.Fatalf("%s not deterministic at %dx%d", th.Name(), w, h)
		}
	}
	// Absolute minimum size: must not panic (blankness is acceptable —
	// sparse themes may legitimately place nothing in 16x3 for some seeds).
	_ = frameOf(th, MinW, MinH, 7, 0)
	// Different seeds should give different layouts (44x8 is roomy enough
	// that a collision would indicate the seed is unused).
	if drawCount(frameOf(th, 44, 8, 7, 0), frameOf(th, 44, 8, 8, 0)) == 0 {
		t.Fatalf("%s ignores seed", th.Name())
	}
	// Animation budget: adjacent phases change some cells but no more than
	// half the grid. (The spec's "~10%" aspiration holds for twinkle/pulse
	// themes; drift themes like rain touch old+new cells for every moving
	// glyph, so the enforced ceiling is 50% — still shimmer, never a full
	// redraw.)
	delta := drawCount(frameOf(th, 44, 8, 7, 3), frameOf(th, 44, 8, 7, 4))
	if delta == 0 {
		t.Fatalf("%s does not animate", th.Name())
	}
	if max := 44 * 8 / 2; delta > max {
		t.Fatalf("%s animation too busy: %d cells changed (max %d)", th.Name(), delta, max)
	}
}

func TestWavesContract(t *testing.T) { assertThemeContract(t, wavesTheme{}) }
func TestDunesContract(t *testing.T) { assertThemeContract(t, dunesTheme{}) }

func TestRegistrySpecOrderSoFar(t *testing.T) {
	names := Names()
	if len(names) == 0 || names[0] != "waves" {
		t.Fatalf("registry must start with waves, got %v", names)
	}
	if names[len(names)-1] != "dunes" {
		t.Fatalf("registry must end with dunes, got %v", names)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/tui/art/...`
Expected: FAIL — `wavesTheme`/`dunesTheme` undefined.

- [ ] **Step 3: Implement `internal/tui/art/ridges.go`**

```go
package art

import "math"

// wavesTheme: layered rolling sine ridges. Layers roll horizontally at
// different speeds; occasional crest sparkles carry the accent color.
type wavesTheme struct{}

func (wavesTheme) Name() string { return "waves" }

func (wavesTheme) Draw(f *Frame, seed uint32, phase int) {
	glyphs := []rune{'~', '≈', '-'}
	layers := 3
	if f.H() < 5 {
		layers = 2
	}
	ph := float64(phase) * 0.35
	for l := 0; l < layers; l++ {
		base := float64(f.H()) * (0.35 + 0.25*float64(l))
		amp := 0.8 + 0.5*float64(l)
		freq := 0.25 - 0.05*float64(l)
		speed := 1.0 - 0.3*float64(l)
		off := CellHashF(l, 0, seed) * 2 * math.Pi
		for x := 0; x < f.W(); x++ {
			y := base + amp*math.Sin(float64(x)*freq+ph*speed+off)
			f.Set(x, int(y), glyphs[l%len(glyphs)])
		}
	}
	// Crest sparkles: sparse, phase-keyed, accented.
	for x := 0; x < f.W(); x++ {
		if CellHash(x, phase, seed)%97 == 0 {
			f.SetAccent(x, int(float64(f.H())*0.35)-1, '·')
		}
	}
}

// dunesTheme: layered noise ridges with density fills; nearer layers drift
// slowly with phase. Ridge crests carry the accent occasionally.
type dunesTheme struct{}

func (dunesTheme) Name() string { return "dunes" }

func (dunesTheme) Draw(f *Frame, seed uint32, phase int) {
	fills := []rune{'░', '▒', '▓'}
	layers := 3
	if f.H() < 5 {
		layers = 2
	}
	for l := 0; l < layers; l++ {
		base := float64(f.H()) * (0.3 + 0.25*float64(l))
		off := CellHashF(l, 3, seed) * 2 * math.Pi
		drift := float64(phase) * 0.05 * float64(l+1)
		for x := 0; x < f.W(); x++ {
			fx := float64(x)
			ridge := base + 1.2*math.Sin(fx*0.11+off+drift) + 0.6*math.Sin(fx*0.29+off*2)
			ry := int(ridge)
			if CellHash(x, l, seed)%53 == 0 {
				f.SetAccent(x, ry, '_')
			} else {
				f.Set(x, ry, '_')
			}
			for y := ry + 1; y < f.H(); y++ {
				f.Set(x, y, fills[l%len(fills)])
			}
		}
	}
}
```

And in `art.go`, add the registration block from the ordering note above (waves + dunes only, with the TODO placeholder comment).

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/tui/art/...`
Expected: PASS. If `TestRegistryAndAssignment` from Task 1 now fails because the registry is non-empty at test start, fix that test to snapshot/restore `registry` (it already does via `old := registry`) — verify it still passes.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/art/ridges.go internal/tui/art/ridges_test.go internal/tui/art/art.go
git commit -m "feat(ATM-4eae82): waves and dunes art themes"
```

---

### Task 3: Themes `starfield` and `rain` (speck family)

**Files:**
- Create: `internal/tui/art/specks.go`
- Test: `internal/tui/art/specks_test.go`

**Interfaces:**
- Consumes: `Frame`, `CellHash`/`CellHashF` from Task 1; test helpers `assertThemeContract`, `frameOf`, `nonBlank`, `drawCount` from Task 2 (same package — reuse, do not redefine).
- Produces: `starfieldTheme{}` registered at index 1, `rainTheme{}` at index 3 (insert into the `init()` block in `art.go` at the spec positions: `waves, starfield, [circuit], rain, dunes`).

- [ ] **Step 1: Write the failing tests**

```go
package art

import "testing"

func TestStarfieldContract(t *testing.T) { assertThemeContract(t, starfieldTheme{}) }
func TestRainContract(t *testing.T)      { assertThemeContract(t, rainTheme{}) }

// Starfield must be sparse: mostly empty sky.
func TestStarfieldIsSparse(t *testing.T) {
	f := frameOf(starfieldTheme{}, 44, 8, 7, 0)
	if n := nonBlank(f); n > 44*8/5 {
		t.Fatalf("starfield too dense: %d/%d cells", n, 44*8)
	}
}

// Rain must actually drift between phases: at least one column's head moves.
func TestRainDrifts(t *testing.T) {
	if drawCount(frameOf(rainTheme{}, 44, 8, 7, 0), frameOf(rainTheme{}, 44, 8, 7, 1)) == 0 {
		t.Fatal("rain must move with phase")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/tui/art/...`
Expected: FAIL — `starfieldTheme`/`rainTheme` undefined.

- [ ] **Step 3: Implement `internal/tui/art/specks.go`**

```go
package art

// starfieldTheme: sparse hash-placed stars; each star twinkles through a
// 4-state brightness cycle on its own offset. The brightest state is
// accent-colored.
type starfieldTheme struct{}

func (starfieldTheme) Name() string { return "starfield" }

func (starfieldTheme) Draw(f *Frame, seed uint32, phase int) {
	for y := 0; y < f.H(); y++ {
		for x := 0; x < f.W(); x++ {
			h := CellHash(x, y, seed)
			if h%23 != 0 { // sparse sky
				continue
			}
			switch (h/23 + uint32(phase)) % 4 {
			case 0:
				f.Set(x, y, '·')
			case 1:
				f.SetAccent(x, y, '✦')
			case 2:
				f.Set(x, y, '*')
			case 3:
				f.Set(x, y, '.')
			}
		}
	}
}

// rainTheme: droplet columns falling at per-column speeds, with dry gaps.
// Drop heads are accent-colored; tails are dim dots.
type rainTheme struct{}

func (rainTheme) Name() string { return "rain" }

func (rainTheme) Draw(f *Frame, seed uint32, phase int) {
	for x := 0; x < f.W(); x++ {
		h := CellHash(x, 0, seed)
		if h%3 == 0 { // dry column
			continue
		}
		speed := 1 + int(h%3)
		span := f.H() + 4 // fall past the bottom before wrapping
		head := (int(h%uint32(span)) + phase*speed) % span
		f.SetAccent(x, head, '╷')
		f.Set(x, head-1, '·')
		if h%5 == 0 {
			f.Set(x, head-2, '·')
		}
	}
}
```

And insert `Register(starfieldTheme{})` (after waves) and `Register(rainTheme{})` (before dunes) into the `init()` block in `art.go`.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/tui/art/...`
Expected: PASS, including `TestRegistrySpecOrderSoFar` (waves still first, dunes still last).

- [ ] **Step 5: Commit**

```bash
git add internal/tui/art/specks.go internal/tui/art/specks_test.go internal/tui/art/art.go
git commit -m "feat(ATM-4eae82): starfield and rain art themes"
```

---

### Task 4: Theme `circuit` (with correct corner bookkeeping)

**Files:**
- Create: `internal/tui/art/circuit.go`
- Test: `internal/tui/art/circuit_test.go`

**Interfaces:**
- Consumes: `Frame`, `CellHash`/`CellHashF`, test helpers from Task 2.
- Produces: `circuitTheme{}` registered at index 2 (between starfield and rain); registry now complete — remove the TODO comment from the `init()` block. Final `Names()` == `["waves","starfield","circuit","rain","dunes"]`.

**Known prototype bug being fixed here:** the glyph prototype emitted stray corner runs (`┐─┘`, `└──└`) because traces jogged without checking occupancy and overwrote each other. The fix: each trace claims cells in the Frame and a jog only happens when the two corner cells and the landing cell are still blank (`f.At(...) == ' '`); otherwise the trace continues horizontally.

- [ ] **Step 1: Write the failing tests**

```go
package art

import "testing"

func TestCircuitContract(t *testing.T) { assertThemeContract(t, circuitTheme{}) }

func TestRegistryCompleteSpecOrder(t *testing.T) {
	want := []string{"waves", "starfield", "circuit", "rain", "dunes"}
	got := Names()
	if len(got) != len(want) {
		t.Fatalf("registry = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("registry[%d] = %q, want %q (order is the auto-assign contract)", i, got[i], want[i])
		}
	}
}

// Corner legality: a corner glyph must connect to compatible neighbors.
// '┐' turns right-coming flow downward, so the cell below must be occupied
// and the cell to its right must NOT be a horizontal continuation drawn by
// the same pass. We assert the cheap invariant that catches the prototype's
// stray-corner bug: no horizontally-adjacent corner pairs like "┐─" or "└─"
// where the corner's vertical partner cell is blank.
func TestCircuitCornersAreConnected(t *testing.T) {
	f := frameOf(circuitTheme{}, 44, 8, 7, 0)
	for y := 0; y < f.H(); y++ {
		for x := 0; x < f.W(); x++ {
			switch f.At(x, y) {
			case '┐': // must connect downward
				if f.At(x, y+1) == ' ' {
					t.Fatalf("dangling ┐ at (%d,%d): nothing below", x, y)
				}
			case '┘': // must connect upward
				if f.At(x, y-1) == ' ' {
					t.Fatalf("dangling ┘ at (%d,%d): nothing above", x, y)
				}
			case '┌': // must connect downward
				if f.At(x, y+1) == ' ' {
					t.Fatalf("dangling ┌ at (%d,%d): nothing below", x, y)
				}
			case '└': // must connect upward
				if f.At(x, y-1) == ' ' {
					t.Fatalf("dangling └ at (%d,%d): nothing above", x, y)
				}
			}
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/tui/art/...`
Expected: FAIL — `circuitTheme` undefined.

- [ ] **Step 3: Implement `internal/tui/art/circuit.go`**

```go
package art

// circuitTheme: horizontal traces that occasionally jog one row up or down
// through proper corner pairs, ending in nodes at the right edge. A rotating
// subset of nodes pulses with the accent color. Jogs happen only into blank
// cells, so traces never overwrite each other into dangling corners (the
// prototype bug).
type circuitTheme struct{}

func (circuitTheme) Name() string { return "circuit" }

func (circuitTheme) Draw(f *Frame, seed uint32, phase int) {
	traces := f.H() / 2
	if traces < 2 {
		traces = 2
	}
	for tr := 0; tr < traces; tr++ {
		y := 1 + int(CellHashF(tr, 1, seed)*float64(maxInt(f.H()-2, 1)))
		if y >= f.H() {
			y = f.H() - 1
		}
		x := 0
		for x < f.W()-1 {
			seg := 3 + int(CellHashF(x, tr, seed)*8)
			for i := 0; i < seg && x < f.W()-1; i++ {
				if f.At(x, y) == ' ' {
					f.Set(x, y, '─')
				}
				x++
			}
			if x >= f.W()-1 {
				break
			}
			// Maybe jog one row; corners drawn only when the corner cell,
			// the landing cell, and the landing continuation are blank.
			if CellHashF(x, tr+7, seed) < 0.5 {
				ny := y + 1
				corner, landing := '┐', '└'
				if CellHashF(x, tr+13, seed) < 0.5 {
					ny = y - 1
					corner, landing = '┘', '┌'
				}
				if ny >= 0 && ny < f.H() &&
					f.At(x, y) == '─' && // we own the approach cell
					f.At(x, ny) == ' ' && f.At(x+1, ny) == ' ' {
					f.Set(x, y, corner)
					f.Set(x, ny, landing)
					y = ny
					x++
				}
			}
		}
		// Terminal node; a phase-rotating subset pulses accented.
		if (uint32(tr)+uint32(phase))%3 == 0 {
			f.SetAccent(f.W()-1, y, '◉')
		} else {
			f.Set(f.W()-1, y, '○')
		}
	}
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
```

Insert `Register(circuitTheme{})` between starfield and rain in `art.go`'s `init()` and delete the TODO comment.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/tui/art/...`
Expected: PASS — all art package tests including corner-connection invariant and full registry order.

- [ ] **Step 5: Eyeball the real output**

Write a throwaway `internal/tui/art` example run (do NOT commit it):

Run: `cat > /tmp/claude-1000/artcheck_test.go` is not needed — instead run the existing tests verbosely and then render one frame via a quick test:

```bash
cd /home/ttran/projects/scyllas/atm && cat > internal/tui/art/preview_test.go <<'EOF'
package art

import (
	"fmt"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

// TestPreview dumps each theme once for human eyeballing; always passes.
func TestPreview(t *testing.T) {
	for _, th := range registry {
		lines := Render(th, 44, 8, Seed("ATM"), 0, lipgloss.NewStyle(), lipgloss.NewStyle())
		fmt.Printf("--- %s ---\n", th.Name())
		for _, ln := range lines {
			fmt.Println(ln)
		}
	}
}
EOF
go test ./internal/tui/art/ -run TestPreview -v
```

Expected: five recognizable, glitch-free motifs printed. **Then delete the preview file:** `rm internal/tui/art/preview_test.go`. If circuit still shows dangling corners, fix the jog conditions before proceeding.

- [ ] **Step 6: Commit**

```bash
git add internal/tui/art/circuit.go internal/tui/art/circuit_test.go internal/tui/art/art.go
git commit -m "feat(ATM-4eae82): circuit art theme with connected-corner jogs"
```

---

### Task 5: Config substrate — `ArtTheme` field + `SetProjectArtTheme`

**Files:**
- Modify: `internal/core/config.go:31-37` (ProjectConfig)
- Modify: `internal/store/config.go:10-22` (GetProjectConfig nil-check) and append setter
- Modify: `internal/core/service.go` (interface; near `SetProjectBoards` at line ~41)
- Test: alongside the existing store config tests — find them first: `grep -rn "SetProjectBoards" internal/store/*_test.go tests/ 2>/dev/null | head`. Add tests in the same file/pattern (if none exist, create `internal/store/config_art_test.go` using the store-opening helper other store tests use — copy their setup).

**Interfaces:**
- Consumes: existing `Store.GetProjectConfig`, `WithLock`, `WriteFileAtomic`, `validateActor` patterns.
- Produces (used by Tasks 6, 8):
  - `core.ProjectConfig.ArtTheme string` (JSON `art_theme,omitempty`)
  - `Store.SetProjectArtTheme(code, theme, actor string) error` — empty `theme` clears the pin. **No validation of theme names in the store** (readers ignore unknown names — the `BoardsConfig` precedent); validation for UX happens in the CLI (Task 6).
  - Same method on the `core.Service` interface.

- [ ] **Step 1: Find every `core.Service` implementor**

Run: `grep -rln "SetProjectBoards" internal/ | grep -v _test`
Expected: the interface file (`internal/core/service.go`), the store implementation (`internal/store/config.go`), and possibly wrappers/fakes. Every file listed needs the new method too.

- [ ] **Step 2: Write the failing test**

Adapt setup from the existing boards-config test found above (same store constructor and project bootstrap). The test body:

```go
func TestSetProjectArtTheme(t *testing.T) {
	s := newTestStore(t) // ← use the actual helper name from the boards tests
	// bootstrap a project the same way the boards config tests do

	// Set a pin.
	if err := s.SetProjectArtTheme("ATM", "circuit", "dev@t:m"); err != nil {
		t.Fatal(err)
	}
	c, err := s.GetProjectConfig("ATM")
	if err != nil || c == nil {
		t.Fatalf("config = %v, %v", c, err)
	}
	if c.ArtTheme != "circuit" {
		t.Fatalf("ArtTheme = %q, want circuit", c.ArtTheme)
	}
	if c.UpdatedBy != "dev@t:m" {
		t.Fatalf("UpdatedBy = %q", c.UpdatedBy)
	}

	// Clearing with empty string removes the pin but keeps the config file
	// readable.
	if err := s.SetProjectArtTheme("ATM", "", "dev@t:m"); err != nil {
		t.Fatal(err)
	}
	c, err = s.GetProjectConfig("ATM")
	if err != nil {
		t.Fatal(err)
	}
	if c != nil && c.ArtTheme != "" {
		t.Fatalf("ArtTheme not cleared: %q", c.ArtTheme)
	}

	// A config holding ONLY art_theme must not read back as nil
	// (regression guard for GetProjectConfig's emptiness check).
	if err := s.SetProjectArtTheme("ATM", "waves", "dev@t:m"); err != nil {
		t.Fatal(err)
	}
	c, err = s.GetProjectConfig("ATM")
	if err != nil || c == nil {
		t.Fatalf("art-theme-only config must be readable, got %v, %v", c, err)
	}
	if c.ArtTheme != "waves" {
		t.Fatalf("ArtTheme = %q, want waves", c.ArtTheme)
	}

	// Invalid actor is rejected.
	if err := s.SetProjectArtTheme("ATM", "waves", "not-an-actor"); err == nil {
		t.Fatal("invalid actor must be rejected")
	}
}
```

Note: the third assertion is only meaningful on a fresh project whose config carries nothing else; if the shared helper pre-populates config, bootstrap a second bare project for that block.

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/store/ -run TestSetProjectArtTheme`
Expected: FAIL — `SetProjectArtTheme` undefined.

- [ ] **Step 4: Implement**

In `internal/core/config.go`, add the field:

```go
type ProjectConfig struct {
	UpdatedAt string            `json:"updated_at,omitempty"`
	UpdatedBy string            `json:"updated_by,omitempty"`
	Embedding *EmbeddingConfig  `json:"embedding,omitempty"`
	Remotes   map[string]string `json:"remotes,omitempty"`
	Boards    *BoardsConfig     `json:"boards,omitempty"`
	// ArtTheme pins the TUI background art theme. Display preference, not
	// substrate state: no event-log entry, and a value naming no registered
	// theme is ignored by readers (auto-assignment applies).
	ArtTheme string `json:"art_theme,omitempty"`
}
```

In `internal/store/config.go`, extend the emptiness check in `GetProjectConfig` (line 18):

```go
	if c.Embedding == nil && c.UpdatedAt == "" && len(c.Remotes) == 0 && c.Boards == nil && c.ArtTheme == "" {
		return nil, nil
	}
```

Append the setter (mirrors `SetProjectBoards`):

```go
// SetProjectArtTheme writes the project's TUI art-theme pin under the
// project lock, read-modify-write like SetProjectBoards. An empty theme
// clears the pin (auto-assignment applies). Theme names are not validated
// here — readers fall back to auto-assignment on unknown names, the same
// defensive posture as BoardsConfig entries. No store event: display
// preference, not substrate state.
func (s *Store) SetProjectArtTheme(code, theme, actor string) error {
	if err := s.validateActor(actor); err != nil {
		return err
	}
	return s.WithLock(code, func() error {
		existing, err := s.GetProjectConfig(code)
		if err != nil && !os.IsNotExist(err) {
			return err
		}
		merged := &ProjectConfig{}
		if existing != nil {
			merged = existing
		}
		merged.ArtTheme = theme
		merged.UpdatedAt = core.RFC3339UTC(core.Now())
		merged.UpdatedBy = actor
		return WriteFileAtomic(s.configPath(code), merged)
	})
}
```

In `internal/core/service.go`, next to `SetProjectBoards`:

```go
	SetProjectArtTheme(code, theme, actor string) error
```

Add the method to every other implementor found in Step 1 (delegating wrappers copy the adjacent `SetProjectBoards` delegation pattern).

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/store/... ./internal/core/...`
Expected: PASS, and `go build ./...` compiles (all Service implementors updated).

- [ ] **Step 6: Commit**

```bash
git add internal/core/config.go internal/core/service.go internal/store/config.go
# plus the test file and any wrapper files touched in step 4
git commit -m "feat(ATM-4eae82): ProjectConfig.ArtTheme + SetProjectArtTheme"
```

---

### Task 6: CLI — `atm project theme <CODE> [<name>|auto]`

**Files:**
- Modify: `internal/cli/project.go` (register at line ~32, add command constructor at end)
- Test: `internal/cli/project_test.go` (follow the existing subcommand test pattern — read one boards-command test first and mirror its harness usage)

**Interfaces:**
- Consumes: `st.store()`/service resolution, `st.emit` output pattern, `bindActorFlag` (all visible in `internal/cli/project.go`); `art.Names()`, `art.ByName`, `art.Effective`, `art.For` from Task 1; `SetProjectArtTheme` from Task 5.
- Produces: user-facing command `atm project theme`.

- [ ] **Step 1: Write the failing tests**

Mirror the harness used by `newProjectBoardsShowCmd` tests (exact runner/asserts from `project_test.go`). Cover four cases:

1. `atm project theme ATM` (no pin set) → output contains `"auto"` and the auto-assigned theme name, plus the available theme list.
2. `atm project theme ATM circuit` → sets pin; a following `show` outputs `circuit` marked pinned.
3. `atm project theme ATM auto` → clears pin; `GetProjectConfig` shows empty `ArtTheme`.
4. `atm project theme ATM bogus` → error mentioning the valid names; exit non-zero; config unchanged.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli/ -run TestProjectTheme`
Expected: FAIL — unknown command "theme".

- [ ] **Step 3: Implement**

Register in `newProjectCmd` (after `newProjectBoardsCmd`):

```go
	cmd.AddCommand(newProjectThemeCmd(st))
```

Constructor (adapt the service/emit calls to the exact patterns in this file — e.g. how `newProjectSetNameCmd` resolves the service and emits):

```go
// newProjectThemeCmd shows or pins the project's TUI background art theme.
// Display preference: stored in config.json, no event-log entry.
func newProjectThemeCmd(st *cliState) *cobra.Command {
	return &cobra.Command{
		Use:   "theme <CODE> [<name>|auto]",
		Short: "Show or pin the TUI background art theme",
		Long: "Each project's TUI background art is auto-assigned by hashing its code over the " +
			"built-in themes. Pin a specific theme with a name, or restore auto-assignment with " +
			"'auto'. Available: " + strings.Join(art.Names(), ", ") + ".",
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			code := args[0]
			s := st.service() // ← use the actual accessor from neighboring commands
			cfg, err := s.GetProjectConfig(code)
			if err != nil {
				return err
			}
			pinned := ""
			if cfg != nil {
				pinned = cfg.ArtTheme
			}
			if len(args) == 1 {
				eff := art.Effective(pinned, code)
				mode := "pinned"
				if _, ok := art.ByName(pinned); !ok {
					mode = "auto"
				}
				return st.emit(st.stdout(), map[string]any{
					"project": code, "theme": eff.Name(), "mode": mode, "available": art.Names(),
				}, func() {
					fmt.Fprintf(st.stdout(), "%s theme: %s (%s)\navailable: %s\n",
						code, eff.Name(), mode, strings.Join(art.Names(), ", "))
				})
			}
			want := args[1]
			if want == "auto" {
				want = ""
			} else if _, ok := art.ByName(want); !ok {
				return fmt.Errorf("%w: unknown theme %q (available: %s, or 'auto')",
					core.ErrUsage, want, strings.Join(art.Names(), ", "))
			}
			if err := s.SetProjectArtTheme(code, want, st.actor()); err != nil {
				return err
			}
			eff := art.Effective(want, code)
			mode := "pinned"
			if want == "" {
				mode = "auto"
			}
			return st.emit(st.stdout(), map[string]any{
				"project": code, "theme": eff.Name(), "mode": mode,
			}, func() {
				fmt.Fprintf(st.stdout(), "%s theme: %s (%s)\n", code, eff.Name(), mode)
			})
		},
	}
}
```

Import `atm/internal/tui/art`. The `st.service()` / `st.actor()` calls above are placeholders for THIS FILE's real accessors — copy them from `newProjectSetNameCmd` (`internal/cli/project.go:277`), which does the same service-resolve + actor-stamped mutation + emit dance. Match error wrapping (`core.ErrUsage`) to how sibling commands signal usage errors.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/cli/ -run TestProjectTheme -v`
Expected: PASS (4 subtests).

- [ ] **Step 5: Commit**

```bash
git add internal/cli/project.go internal/cli/project_test.go
git commit -m "feat(ATM-4eae82): atm project theme show/pin/auto CLI"
```

---

### Task 7: Projects pane geometry — 4-way split + fixed 5-row page

**Files:**
- Modify: `internal/tui/projects.go:75-108` (`projectPaneSplitHeights`), `:577-587` (`listPageSize`)
- Test: wherever `projectPaneSplitHeights` is currently tested — `grep -rn "projectPaneSplitHeights" internal/tui/*_test.go`. Update/extend there (create `internal/tui/projects_split_test.go` if untested).

**Interfaces:**
- Consumes: nothing new.
- Produces (used by Task 8): `projectPaneSplitHeights(total int) (listH, artH, eventsH, summaryH int)` — note the NEW second return value; `listPageSize` capped at 5.

- [ ] **Step 1: Find and read existing callers/tests**

Run: `grep -rn "projectPaneSplitHeights\|listPageSize" internal/tui/ | grep -v tasks`
Expected: callers in `renderList` (line ~531) and the `[`/`]` page-jump handler (~248-257), plus any tests. All callers must be updated for the 4-value signature (Task 8 updates `renderList`; if the page-jump handler calls `projectPaneSplitHeights`, update it here to discard `artH`).

- [ ] **Step 2: Write the failing tests**

```go
func TestProjectPaneSplitHeights4Way(t *testing.T) {
	cases := []struct {
		name                            string
		total                           int
		listH, artH, eventsH, summaryH  int
	}{
		// Tall pane: list capped at 9 (5 rows + 4 overhead), events 35%,
		// summary 35%, art absorbs the rest.
		{"tall", 40, 9, 3, 14, 14},
		// Art below 3 lines folds into summary.
		{"art-folds", 30, 9, 0, 10, 11},
		// Scarce: list first, then summary, events collapse under 4.
		{"scarce", 10, 9, 0, 0, 1},
		// Degenerate.
		{"tiny", 5, 5, 0, 0, 0},
		{"one", 1, 1, 0, 0, 0},
		{"zero", 0, 0, 0, 0, 0},
	}
	for _, c := range cases {
		l, a, e, s := projectPaneSplitHeights(c.total)
		if l != c.listH || a != c.artH || e != c.eventsH || s != c.summaryH {
			t.Errorf("%s(%d): got (%d,%d,%d,%d), want (%d,%d,%d,%d)",
				c.name, c.total, l, a, e, s, c.listH, c.artH, c.eventsH, c.summaryH)
		}
	}
}

func TestProjectPaneSplitAlwaysSumsToTotal(t *testing.T) {
	for total := 0; total <= 120; total++ {
		l, a, e, s := projectPaneSplitHeights(total)
		if l+a+e+s != total {
			t.Fatalf("total %d: sections sum to %d", total, l+a+e+s)
		}
		if a != 0 && a < 3 {
			t.Fatalf("total %d: art region %d below minimum 3", total, a)
		}
	}
}

func TestListPageSizeCappedAtFive(t *testing.T) {
	p := &projectsModel{}
	if got := p.listPageSize(9); got != 5 {
		t.Fatalf("listPageSize(9) = %d, want 5", got)
	}
	if got := p.listPageSize(100); got != 5 {
		t.Fatalf("listPageSize(100) = %d, want 5 (fixed page)", got)
	}
	if got := p.listPageSize(7); got != 3 {
		t.Fatalf("listPageSize(7) = %d, want 3 (short pane degrades)", got)
	}
	if got := p.listPageSize(2); got != 1 {
		t.Fatalf("listPageSize(2) = %d, want 1 (floor)", got)
	}
}
```

The expected values in `TestProjectPaneSplitHeights4Way` were computed by hand against the implementation below (e.g. total 40: list 9, events 40·35% = 14, summary 14, art 40−9−14−14 = 3). On mismatch, fix the implementation, not the invariant tests.

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test ./internal/tui/ -run 'TestProjectPaneSplit|TestListPageSize'`
Expected: FAIL — wrong arity / old behavior.

- [ ] **Step 4: Implement**

Replace `projectPaneSplitHeights`:

```go
// projectPaneSplitHeights allocates the list view's vertical space four
// ways, top to bottom: project list (fixed page of 5 rows = 9 lines with
// caption/header/rule/footer), background art (absorbs the spare height),
// recent-events feed (~35%, collapses under 4 — see the boxed/compact frame
// note on renderEventsFeed), and summary (~35%, keeps the bottom). An art
// slot under 3 lines is not worth drawing (art.MinH) and folds into summary,
// restoring the pre-art layout on short panes.
func projectPaneSplitHeights(total int) (listH, artH, eventsH, summaryH int) {
	if total <= 0 {
		return 0, 0, 0, 0
	}
	listH = 9 // caption + header + rule + 5 rows + footer
	if listH > total {
		return total, 0, 0, 0
	}
	remaining := total - listH
	eventsH = total * 35 / 100
	if eventsH > remaining {
		eventsH = remaining
	}
	if eventsH < 4 {
		eventsH = 0
	}
	remaining -= eventsH
	summaryH = total * 35 / 100
	if summaryH > remaining {
		summaryH = remaining
	}
	artH = remaining - summaryH
	if artH < 3 {
		summaryH += artH
		artH = 0
	}
	return listH, artH, eventsH, summaryH
}
```

Replace `listPageSize`:

```go
// listPageSize returns the project rows per page: fixed at 5 (the list
// section is sized for exactly 5 by projectPaneSplitHeights), degrading
// only when the whole pane is shorter than the fixed list section. Shared
// by rendering and the "[" / "]" page jump so both agree on a page.
func (p *projectsModel) listPageSize(maxRows int) int {
	availableRows := maxRows - 4 // caption + header + rule + footer
	if availableRows < 1 {
		availableRows = 1
	}
	if availableRows > 5 {
		availableRows = 5
	}
	return availableRows
}
```

Update the non-render caller(s) of `projectPaneSplitHeights` found in Step 1 to the 4-value signature (discard `artH` with `_`). `renderList` itself is updated in Task 8 — to keep this task compiling, update its destructuring line now to `listH, _, eventsH, summaryH := projectPaneSplitHeights(p.contentHeight)` without other changes.

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/tui/...`
Expected: PASS — new tests green, existing pane tests still green (fix any test asserting the old 3-way split by updating its expectations to the new geometry, not by weakening invariants).

- [ ] **Step 6: Commit**

```bash
git add internal/tui/projects.go internal/tui/projects_split_test.go
# plus any updated existing test files
git commit -m "feat(ATM-4eae82): 4-way Projects pane split, list fixed at 5 rows"
```

---

### Task 8: Model wiring — art tick, pins map, styles, Projects pane render

**Files:**
- Modify: `internal/tui/app.go` (Model fields ~line 67-138, `Init` ~391, `Update` ~413, `refreshAll` — locate: `grep -n "func (m \*Model) refreshAll" internal/tui/app.go`)
- Modify: `internal/tui/theme.go` (Styles struct ~28, `buildStyles` ~92)
- Modify: `internal/tui/projects.go` (`renderList` ~527)
- Test: `internal/tui/app_test.go` / `internal/tui/projects_test.go` (mirror existing model-test setup — find how tests build a Model: `grep -n "NewModel\|newTestModel" internal/tui/app_test.go | head`)

**Interfaces:**
- Consumes: `art.Effective`, `art.Seed`, `art.Render`, `art.MinH/MinW` (Task 1); `projectPaneSplitHeights` 4-value form (Task 7); `GetProjectConfig` (Task 5).
- Produces (used by Task 9): `Model.artPhase int`, `Model.artPins map[string]string`, `Styles.ArtBase`/`Styles.ArtAccent lipgloss.Style`, helper `(p *projectsModel) renderArt(height int) string`.

- [ ] **Step 1: Write the failing tests**

Using the package's existing model-test harness (adapt constructor/setup names):

```go
func TestArtTickAdvancesPhaseOnWorkspace(t *testing.T) {
	m := newTestModel(t) // ← actual harness helper
	before := m.artPhase
	m.Update(artTickMsg{})
	if m.artPhase != before+1 {
		t.Fatalf("phase = %d, want %d", m.artPhase, before+1)
	}
	// Overlay open: phase freezes.
	m.helpOverlay = helpKeys // any non-zero overlay kind used in this package
	frozen := m.artPhase
	m.Update(artTickMsg{})
	if m.artPhase != frozen {
		t.Fatal("phase must not advance while an overlay is open")
	}
}

func TestArtStylesExistInAllThemes(t *testing.T) {
	for _, name := range themeNames() { // ← reuse the theme enumeration from theme_test.go:13
		st := buildStyles(name)
		if st.ArtBase.GetForeground() == st.ArtAccent.GetForeground() {
			t.Fatalf("theme %v: art base and accent must differ", name)
		}
	}
}

func TestProjectsRenderListIncludesArtRegion(t *testing.T) {
	m := newTestModel(t) // with ≥1 project and a tall pane
	m.projects.SetSize(60, 40)
	out := m.projects.renderList()
	lines := strings.Split(out, "\n")
	if len(lines) != 40 {
		t.Fatalf("renderList height = %d, want 40", len(lines))
	}
	// The art region (rows 9..9+artH) must contain at least one non-blank
	// glyph — the pane is no longer dead padding there.
	_, artH, _, _ := projectPaneSplitHeights(40)
	if artH < 3 {
		t.Skip("pane too short for art in this configuration")
	}
	found := false
	for _, ln := range lines[9 : 9+artH] {
		if strings.TrimSpace(stripANSI(ln)) != "" { // reuse/borrow the package's ANSI stripper if one exists
			found = true
			break
		}
	}
	if !found {
		t.Fatal("art region is blank")
	}
}
```

Check for an existing ANSI-strip helper first (`grep -rn "stripANSI\|ansi" internal/tui/*_test.go | head`); reuse it, or use `github.com/charmbracelet/x/ansi.Strip` only if it is already an existing transitive import — otherwise trim against `lipgloss.Width(ln) > 0` with content heuristics matching how neighboring render tests assert.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/tui/ -run 'TestArt|TestProjectsRenderListIncludesArt'`
Expected: FAIL — `artTickMsg`, `artPhase`, `ArtBase` undefined.

- [ ] **Step 3: Implement**

`internal/tui/theme.go` — Styles struct gains:

```go
	// ArtBase/ArtAccent style the background art: base is the palette's
	// subtle tone so art reads as background; accent is the palette accent,
	// used sparsely by the generators.
	ArtBase   lipgloss.Style
	ArtAccent lipgloss.Style
```

and in `buildStyles`:

```go
		ArtBase:   lipgloss.NewStyle().Foreground(t.Subtle),
		ArtAccent: lipgloss.NewStyle().Foreground(t.Accent),
```

(Verify the high-contrast theme at `theme.go:86` yields differing Subtle/Accent — it does: `240` vs `255`.)

`internal/tui/app.go` — Model fields:

```go
	// artPhase is the background-art animation clock, advanced by artTickMsg
	// only while the plain workspace is visible; artPins caches each listed
	// project's config.json art_theme pin (refreshed by refreshAll) so View
	// never touches the filesystem.
	artPhase int
	artPins  map[string]string
```

Tick plumbing next to `refreshTickCmd` (~line 400):

```go
// artTickMsg advances the background-art animation. The tick always
// reschedules (cheap no-op off-workspace) but the phase only advances while
// the plain workspace is visible, so art freezes under overlays and forms.
type artTickMsg struct{}

const artTickInterval = 600 * time.Millisecond

func artTickCmd() tea.Cmd {
	return tea.Tick(artTickInterval, func(time.Time) tea.Msg { return artTickMsg{} })
}
```

`Init` (line 391) becomes:

```go
func (m *Model) Init() tea.Cmd { return tea.Batch(refreshTickCmd(), artTickCmd()) }
```

In `Update`, next to the `refreshTickMsg` case (line ~416):

```go
	case artTickMsg:
		if m.workspaceIdle() {
			m.artPhase++
		}
		return m, artTickCmd()
```

Add the predicate near `canMutate` (line 384) — verify each zero-value name against the actual type definitions (`grep -n "helpOverlayKind\|confirmAction" internal/tui/*.go | head`):

```go
// workspaceIdle reports whether the plain two-pane workspace is what View
// shows — no overlay, form, or confirm layered over it. Art animates only
// then; anything covering the workspace freezes the phase clock.
func (m *Model) workspaceIdle() bool {
	return m.helpOverlay == helpNone && // ← use the real zero/none constant
		!m.actorsOverlay &&
		m.form == nil &&
		m.confirm == confirmNone && // ← real zero constant
		!m.pluginOverlayOpen() // ← see warning below
}
```

**Warning — `pluginOverlay`'s zero value is 0, not -1** (the field comment at `app.go:126-128` says 0 is *unused-but-not-open* until the first plugin registers). A naive `m.pluginOverlay < 0` check would report an overlay open on every fresh model and freeze art forever. Derive the real "plugin overlay open" predicate from how `View()` decides to render a plugin overlay (grep `pluginOverlay` in `app.go`) and wrap it as `pluginOverlayOpen()` — likely something like `len(m.plugins) > 0 && m.pluginOverlay >= 0 && <whatever flag View checks>`.

**Verify each condition against `View()`'s actual dispatch order** (`grep -n "func (m \*Model) View" internal/tui/app.go` and read it) — the predicate must return false exactly when View draws something other than the workspace. Include detail views? Spec says no detail views: also require `m.projects.view == pViewList` when focused... simpler and spec-faithful: add `&& m.projects.view == pViewList && m.tasks.view == tViewList` using the real view-enum names from `projects.go`/`tasks.go`.

In `refreshAll` (after the projects list refresh), rebuild the pins cache:

```go
	// Refresh the art-pin cache alongside the project list so renderers
	// never read config.json during View.
	pins := make(map[string]string, len(m.projects.list))
	for _, r := range m.projects.list {
		if cfg, err := m.store.GetProjectConfig(r.code); err == nil && cfg != nil {
			pins[r.code] = cfg.ArtTheme
		}
	}
	m.artPins = pins
```

(Adapt `r.code` to the row struct's real field, and place after `m.projects.list` is populated.)

`internal/tui/projects.go` — `renderList` (line 527) becomes the 4-way stack:

```go
	listH, artH, eventsH, summaryH := projectPaneSplitHeights(p.contentHeight)
	if p.m.projectScope == "" {
		eventsH, summaryH = 0, summaryH+eventsH
	}
	boxed := summaryChartsBoxed(summaryH)
	var parts []string
	if listH > 0 {
		parts = append(parts, padToHeight(p.renderListRows(listH), listH))
	}
	if artH > 0 {
		parts = append(parts, padToHeight(p.renderArt(artH), artH))
	}
	if eventsH > 0 {
		parts = append(parts, padToHeight(p.renderEventsFeed(eventsH, boxed), eventsH))
	}
	if summaryH > 0 {
		parts = append(parts, padToHeight(p.renderSummary(summaryH), summaryH))
	}
	return padToHeight(strings.Join(parts, "\n"), p.contentHeight)
```

New renderer at the bottom of the file's render section:

```go
// renderArt draws the background art for the pane's context project: the
// scoped project when one is selected, else the cursor row. Falls back to
// blank lines (padToHeight in the caller) when the region or project list
// can't support art.
func (p *projectsModel) renderArt(height int) string {
	if len(p.list) == 0 {
		return ""
	}
	code := p.m.projectScope
	if code == "" {
		code = p.list[p.cursor].code
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

Import `atm/internal/tui/art` in both files. Guard `p.cursor` bounds the same way `renderListRows` does.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/tui/...`
Expected: PASS — including previously existing renderList/pagination tests (update any that assert exact old layouts; the geometry legitimately changed).

- [ ] **Step 5: See it live**

Run: `go build ./... && go vet ./internal/tui/...`
Expected: clean. (Full interactive check happens in Task 10.)

- [ ] **Step 6: Commit**

```bash
git add internal/tui/app.go internal/tui/theme.go internal/tui/projects.go
# plus test files touched
git commit -m "feat(ATM-4eae82): art tick, pin cache, Projects pane art region"
```

---

### Task 9: Tasks pane — art in the gap above the boards ring

**Files:**
- Modify: `internal/tui/tasks_list.go:226-249` (`renderListWithStrip`)
- Test: `internal/tui/tasks_list_test.go` (or wherever `renderListWithStrip` is tested — `grep -rn "renderListWithStrip" internal/tui/*_test.go`)

**Interfaces:**
- Consumes: `art.Effective/Seed/Render/MinH` (Task 1), `Model.artPins`, `Model.artPhase`, `Styles.ArtBase/ArtAccent` (Task 8).
- Produces: nothing downstream.

- [ ] **Step 1: Write the failing test**

```go
func TestTasksPaneFillsGapWithArt(t *testing.T) {
	m := newTestModel(t) // scoped to a project with FEW tasks, tall pane
	m.tasks.SetSize(70, 46)
	out := m.tasks.renderListWithStrip()
	lines := strings.Split(out, "\n")
	// The rows directly above the boards ring (last stripHeight+pinnedBoxHeight
	// lines) were blank padding before; with few tasks and a tall pane at
	// least one of them must now carry art glyphs.
	top := len(lines) - stripHeight - pinnedBoxHeight
	found := false
	for _, ln := range lines[:top] {
		s := strings.TrimSpace(stripANSI(ln)) // same helper choice as Task 8
		if strings.ContainsAny(s, "~≈·✦*░▒▓─│┌┐└┘╷") {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("no art glyphs in the tasks-pane gap")
	}
	// Height must be unchanged by art injection.
	if len(lines) != 46 {
		t.Fatalf("pane height = %d, want 46", len(lines))
	}
}

func TestTasksPaneNoArtWithoutScope(t *testing.T) {
	m := newTestModel(t)
	m.projectScope = "" // however the harness clears scope
	m.tasks.SetSize(70, 46)
	out := m.tasks.renderListWithStrip()
	for _, ln := range strings.Split(out, "\n") {
		if strings.ContainsAny(stripANSI(ln), "≈✦░▒▓") {
			t.Fatal("art must not render without a project scope")
		}
	}
}
```

(Glyph probes: pick glyphs unique to art — avoid `─`/`│` in the no-scope test since boxes/rules use them.)

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tui/ -run TestTasksPane`
Expected: FAIL — gap is blank.

- [ ] **Step 3: Implement**

In `renderListWithStrip` (line 234), after `listOut := t.renderList()` (and the save/restore), inject:

```go
	listOut = t.fillGapWithArt(listOut)
```

New method below `renderListWithStrip`:

```go
// fillGapWithArt replaces the task table's trailing blank padding (the dead
// space between the last rendered row and the boards ring) with background
// art for the scoped project. The table block keeps its exact height —
// padToHeight pads with empty lines ("" — see padToHeight), so trailing
// lines that trim to empty are exactly the reclaimable gap. Below art.MinH
// blank lines the gap stays as-is (spec collapse threshold).
func (t *tasksModel) fillGapWithArt(listOut string) string {
	code := t.m.projectScope
	if code == "" {
		return listOut
	}
	lines := strings.Split(listOut, "\n")
	gap := 0
	for i := len(lines) - 1; i >= 0 && strings.TrimSpace(lines[i]) == ""; i-- {
		gap++
	}
	if gap < art.MinH {
		return listOut
	}
	theme := art.Effective(t.m.artPins[code], code)
	artLines := art.Render(theme, t.width, gap, art.Seed(code), t.m.artPhase,
		t.m.styles.ArtBase, t.m.styles.ArtAccent)
	if artLines == nil {
		return listOut
	}
	copy(lines[len(lines)-gap:], artLines)
	return strings.Join(lines, "\n")
}
```

Import `atm/internal/tui/art`. Caveat checked in Step 1's second test: the empty-state screen (`renderEmptyState`) centers its message with blank lines above AND below — the scope guard keeps art out of that screen entirely.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/tui/...`
Expected: PASS — both new tests and the existing tasks-list layout tests.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/tasks_list.go internal/tui/tasks_list_test.go
git commit -m "feat(ATM-4eae82): art fills Tasks pane gap above boards ring"
```

---

### Task 10: Full verification, live check, changelog, ledger

**Files:**
- Modify: `CHANGELOG.md` (repo root — follow the existing entry format at the top of the file)

- [ ] **Step 1: Full build + test + vet**

Run: `go build ./... && go vet ./... && go test ./...`
Expected: everything green. Fix regressions before proceeding — do not skip failing packages.

- [ ] **Step 2: Live TUI smoke test**

Run the TUI against a throwaway store (find the launch pattern: `grep -rn "ATM_STORE\|StorePath" internal/cli/env.go | head`, or use the project's run instructions / `atm --help`). Verify by eye, on a terminal ≥ 40 rows:
1. Projects pane shows: 5-row list, art, events, summary — in that order.
2. Art differs between two different project codes (create two projects in the throwaway store).
3. `atm project theme <CODE> circuit` (against the same store) then TUI refresh (wait ≤10s or press the refresh key) → motif switches to circuit.
4. Art animates (~600ms shimmer) and freezes when `?` help overlay opens.
5. Tasks pane: with a few tasks, art sits between table and ring; with no scope, no art.
6. Resize the terminal: art redraws to fit; shrink until art disappears (<3 free lines) with no panics or layout tears.

- [ ] **Step 3: Changelog entry**

Add under the current unreleased/top section, matching the file's existing entry style:

```markdown
- TUI: per-project procedural background art fills the spare space in the
  Projects pane (between the 5-row project list and the events feed) and the
  Tasks pane (between the task table and the boards ring). Five themes
  (waves, starfield, circuit, rain, dunes) auto-assigned per project;
  pin one with `atm project theme <CODE> <name|auto>`. (ATM-4eae82)
```

- [ ] **Step 4: Commit**

```bash
git add CHANGELOG.md
git commit -m "docs(ATM-4eae82): changelog for TUI art backgrounds"
```

- [ ] **Step 5: Ledger update**

Record on ATM-4eae82 (actor `developer@claude:<model>`): implementation complete, commits list, live-check results. Advance the workflow_ai stage per its guide (implement/test rungs as evidence allows). Then request code review per the session's process (superpowers:requesting-code-review).
