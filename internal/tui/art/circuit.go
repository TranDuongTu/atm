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
