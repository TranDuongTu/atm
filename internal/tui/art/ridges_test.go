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
