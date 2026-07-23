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
