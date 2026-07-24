package art

import "math"

// tunnel: a perspective flight through square rings receding to a vanishing
// point. Each ring is drawn as a box that grows from the center outward;
// rings are spaced by phase so the motion is INTO the screen (toward the
// viewer), which is unique in the set -- everything else moves laterally.
// The nearest ring is accent; farther rings fade to dim.
type tunnelTheme struct{}

func (tunnelTheme) Name() string { return "tunnel" }

func (tunnelTheme) Draw(f *Frame, seed uint32, phase int) {
	cx, cy := float64(f.W())/2, float64(f.H())/2
	const rings = 5
	for r := 0; r < rings; r++ {
		d := math.Mod(float64(phase)*0.04+float64(r)/float64(rings), 1.0)
		if d <= 0.02 {
			continue
		}
		hw := d * float64(f.W()) / 2
		hh := d * float64(f.H()) / 2
		x0, y0 := int(cx-hw), int(cy-hh)
		x1, y1 := int(cx+hw), int(cy+hh)
		near := d > 0.6
		box := func(x, y int) {
			if x < 0 || x >= f.W() || y < 0 || y >= f.H() {
				return
			}
			if near {
				f.SetAccent(x, y, '╳')
			} else {
				f.Set(x, y, '·')
			}
		}
		for x := x0; x <= x1; x++ {
			box(x, y0)
			box(x, y1)
		}
		for y := y0; y <= y1; y++ {
			box(x0, y)
			box(x1, y)
		}
	}
	cxi, cyi := int(cx), int(cy)
	if cxi >= 0 && cxi < f.W() && cyi >= 0 && cyi < f.H() {
		f.SetAccent(cxi, cyi, '·')
	}
}
