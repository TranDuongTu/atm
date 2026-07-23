package art

import "testing"

func TestCircuitContract(t *testing.T) { assertThemeContract(t, circuitTheme{}) }

// TestCircuitJogsOccur catches the "jog guard is unsatisfiable by a trace's
// own drawing" regression. With a broken guard (f.At(x, y) == '─', checked
// one cell past the last dash a trace itself drew), a jog can only fire when
// some OTHER trace happens to already occupy that exact cell on the exact
// same row -- a coincidence that depends on multiple traces colliding on one
// row. That makes jogs fire on only a minority of seeds, and even then via
// cross-trace corruption rather than the trace's own approach. Once the
// guard correctly checks for a still-blank corner cell, a trace can jog off
// its own dash run, so corners appear reliably on every seed. We assert the
// stronger, distinguishing invariant: EVERY one of a fixed set of seeds
// produces at least one corner glyph -- true after the fix, false (flaky,
// only ~half the seeds) before it.
func TestCircuitJogsOccur(t *testing.T) {
	corners := "┐┌┘└"
	for seed := uint32(1); seed <= 8; seed++ {
		f := frameOf(circuitTheme{}, 44, 8, seed, 0)
		found := false
		for y := 0; y < f.H() && !found; y++ {
			for x := 0; x < f.W(); x++ {
				c := f.At(x, y)
				for _, want := range corners {
					if c == want {
						found = true
						break
					}
				}
				if found {
					break
				}
			}
		}
		if !found {
			t.Errorf("seed %d: no corner glyphs (┐┌┘└) found; jog did not fire", seed)
		}
	}
}

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
