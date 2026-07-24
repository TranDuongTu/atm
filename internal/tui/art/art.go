// Package art renders per-project procedural background art for the TUI's
// spare vertical space. Themes draw glyphs into a Frame (rune grid + accent
// mask); Render blits the frame through the ntcharts canvas with the two
// palette-derived styles. All drawing is deterministic: layout varies only
// with seed, animation only with phase.
package art

import (
	"hash/fnv"
	"math/rand"

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

// init owns registration order; it is the auto-assign contract. Theme files
// define types only — registration lives here, in spec order.
func init() {
	Register(galaxyTheme{})
	Register(lorenzTheme{})
	Register(matrixTheme{})
	Register(tunnelTheme{})
	Register(skylineTheme{})
	Register(constellationTheme{})
}

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

// Effective resolves a pinned theme name, returning nil when the pin is
// empty or names nothing (renamed theme, typo). There is no hash fallback:
// a missing pin means no art. The code parameter is reserved for future
// callers and is currently unused.
func Effective(pinned, code string) Theme {
	if t, ok := ByName(pinned); ok {
		return t
	}
	return nil
}

// Pair returns two distinct registered themes, deterministic per code and
// stable for a fixed registry (append-only). Requires len(registry) >= 2.
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

// RollPair returns two distinct registered themes drawn at random from the
// live registry using r. Selection is uniform and non-deterministic; the
// caller owns the source (typically time-seeded for a user-triggered
// re-roll). Requires len(registry) >= 2.
func RollPair(r *rand.Rand) [2]Theme {
	n := len(registry)
	i := r.Intn(n)
	j := r.Intn(n - 1)
	if j >= i {
		j++
	}
	return [2]Theme{registry[i], registry[j]}
}

// EffectivePair resolves a pinned two-theme pair by name, falling back to
// Pair(code) when pinned is empty, has the wrong length, or any name does
// not resolve. The fallback keeps the render path simple: callers persist a
// rolled pair and read it back through this single resolver. Always returns
// two distinct registered themes.
func EffectivePair(pinned []string, code string) [2]Theme {
	if len(pinned) == 2 {
		t0, ok0 := ByName(pinned[0])
		t1, ok1 := ByName(pinned[1])
		if ok0 && ok1 && t0.Name() != t1.Name() {
			return [2]Theme{t0, t1}
		}
	}
	return Pair(code)
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
