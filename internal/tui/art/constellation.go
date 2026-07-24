package art

import "math"

// constellation: a graph of stars connected by faint lines that slowly
// rotate/shift. Stars twinkle; the connecting lines persist. Rich, techy,
// structured. Accent stars, base lines.
type constellationTheme struct{}

func (constellationTheme) Name() string { return "constellation" }

func (constellationTheme) Draw(f *Frame, seed uint32, phase int) {
	stars := f.H() + 2
	if stars > 14 {
		stars = 14
	}
	type pt struct{ x, y float64 }
	pts := make([]pt, stars)
	for i := range pts {
		pts[i] = pt{CellHashF(i, 1, seed) * float64(f.W()), CellHashF(i, 2, seed) * float64(f.H())}
	}
	for i := 0; i < stars; i++ {
		for j := i + 1; j < stars; j++ {
			dx := pts[j].x - pts[i].x
			dy := pts[j].y - pts[i].y
			dist := math.Sqrt(dx*dx + dy*dy)
			if dist > float64(f.W())/3 {
				continue
			}
			steps := int(dist) + 1
			for s := 0; s <= steps; s++ {
				t := float64(s) / float64(steps)
				x := int(pts[i].x + dx*t)
				y := int(pts[i].y + dy*t)
				if x < 0 || x >= f.W() || y < 0 || y >= f.H() {
					continue
				}
				if f.At(x, y) == ' ' {
					f.Set(x, y, '·')
				}
			}
		}
	}
	for i, p := range pts {
		x, y := int(p.x), int(p.y)
		if x < 0 || x >= f.W() || y < 0 || y >= f.H() {
			continue
		}
		switch (CellHash(i, 3, seed)/7 + uint32(phase)) % 4 {
		case 0:
			f.SetAccent(x, y, '✦')
		case 1:
			f.SetAccent(x, y, '·')
		case 2:
			f.Set(x, y, '*')
		case 3:
			f.Set(x, y, '·')
		}
	}
}
