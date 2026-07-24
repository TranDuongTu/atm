package art

import "math"

// skyline: a city silhouette at night. Buildings of varying heights sit on
// the baseline, filled solid with a block shade that lightens toward the
// roofline. A clean roof bar caps each building; a window grid sits inside,
// lit by a slow diagonal "lights sweep" wave (lit windows = accent).
type skylineTheme struct{}

func (skylineTheme) Name() string { return "skyline" }

// buildingLayout computes the building rectangles shared by skyline/horizon.
func buildingLayout(f *Frame, seed uint32) []struct{ x0, w, h int } {
	buildings := []struct{ x0, w, h int }{}
	x, bi := 0, 0
	for x < f.W() {
		w := 4 + int(CellHash(bi, 1, seed)%5)
		h := 3 + int(CellHashF(bi, 2, seed)*float64(f.H()-1))
		if h < 3 {
			h = 3
		}
		if h >= f.H() {
			h = f.H() - 1
		}
		if x+w > f.W() {
			w = f.W() - x
		}
		if w < 3 {
			break
		}
		buildings = append(buildings, struct{ x0, w, h int }{x, w, h})
		x += w + 1
		bi++
	}
	return buildings
}

func (skylineTheme) Draw(f *Frame, seed uint32, phase int) {
	ph := float64(phase) * 0.12
	for _, b := range buildingLayout(f, seed) {
		roof := f.H() - b.h
		for y := roof + 1; y < f.H(); y++ {
			depth := float64(y-roof) / float64(b.h)
			lvl := int((1 - depth) * 2)
			shade := '▒'
			switch lvl {
			case 0:
				shade = '░'
			case 1:
				shade = '▒'
			default:
				shade = '▓'
			}
			for xx := b.x0; xx < b.x0+b.w && xx < f.W(); xx++ {
				f.Set(xx, y, shade)
			}
		}
		if roof >= 0 && roof < f.H() {
			for xx := b.x0; xx < b.x0+b.w && xx < f.W(); xx++ {
				f.Set(xx, roof, '▀')
			}
		}
		for wy := roof + 1; wy < f.H()-1; wy++ {
			if (wy-roof)%2 != 1 {
				continue
			}
			for wx := b.x0 + 1; wx < b.x0+b.w-1 && wx < f.W()-1; wx++ {
				if (wx-b.x0)%2 != 1 {
					continue
				}
				lit := math.Sin(float64(wx)*0.4+float64(wy)*0.5+ph) > 0.5
				if lit {
					f.SetAccent(wx, wy, '·')
				} else if CellHash(wx, wy, seed)%4 == 0 {
					f.Set(wx, wy, '·')
				}
			}
		}
	}
}
