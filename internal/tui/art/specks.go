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
