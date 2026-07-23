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
