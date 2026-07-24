package art

// matrix: dense falling-glyph columns (matrix rain) with full brightness
// gradient heads and dimmer trailing tails. Heads accent; body base. Taller
// tails and more columns than `rain` for richness.
type matrixTheme struct{}

func (matrixTheme) Name() string { return "matrix" }

func (matrixTheme) Draw(f *Frame, seed uint32, phase int) {
	glyphs := []rune{'0', '1', '2', '3', '4', '5', '6', '7', '8', '9', 'ﾊ', 'ﾐ', 'ﾋ', 'ｰ', 'ｱ', 'ｲ'}
	for x := 0; x < f.W(); x++ {
		h := CellHash(x, 0, seed)
		if h%5 == 0 {
			continue
		}
		speed := 1 + int(h%3)
		tail := 4 + int(h%4)
		head := (int(h%uint32(f.H()+tail)) + phase*speed) % (f.H() + tail)
		for t := 0; t < tail; t++ {
			y := head - t
			if y < 0 || y >= f.H() {
				continue
			}
			g := glyphs[int(CellHash(x, t, seed^uint32(phase)))%len(glyphs)]
			if t == 0 {
				f.SetAccent(x, y, g)
			} else if t == 1 {
				f.Set(x, y, g)
			} else {
				f.Set(x, y, glyphs[int(CellHash(x, t, seed))%len(glyphs)])
			}
		}
	}
}
