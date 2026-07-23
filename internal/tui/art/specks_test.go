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
