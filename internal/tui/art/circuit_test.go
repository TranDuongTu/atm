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
