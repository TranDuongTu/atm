package art

import "math"

// galaxy: a top-down spiral galaxy. A bright core plus 2 logarithmic spiral
// arms of stars; the whole structure rotates slowly. Stars on the arms
// twinkle; the core is a solid accent blob. Calmer and more iconic than
// starfield -- a single coherent object, not a scatter.
type galaxyTheme struct{}

func (galaxyTheme) Name() string { return "galaxy" }

func (galaxyTheme) Draw(f *Frame, seed uint32, phase int) {
	rot := float64(phase) * 0.03
	cx, cy := float64(f.W())/2, float64(f.H())/2
	coreR := 1.5
	for dy := -2; dy <= 2; dy++ {
		for dx := -2; dx <= 2; dx++ {
			x, y := int(cx)+dx, int(cy)+dy
			if x < 0 || x >= f.W() || y < 0 || y >= f.H() {
				continue
			}
			d := math.Hypot(float64(dx), float64(dy))
			if d <= coreR {
				f.SetAccent(x, y, '☉')
			} else if d <= coreR+1 {
				f.SetAccent(x, y, '·')
			}
		}
	}
	arms := 2
	starsPerArm := f.W() + f.H()
	for a := 0; a < arms; a++ {
		baseAng := float64(a)*math.Pi + rot
		for i := 0; i < starsPerArm; i++ {
			t := float64(i) / float64(starsPerArm)
			r := 2.0 + t*float64(f.W())*0.45
			ang := baseAng + t*4.0*math.Pi
			x := cx + r*math.Cos(ang)
			y := cy + r*math.Sin(ang)*0.5
			xi, yi := int(math.Round(x)), int(math.Round(y))
			if xi < 0 || xi >= f.W() || yi < 0 || yi >= f.H() {
				continue
			}
			h := CellHash(i, a, seed)
			switch (h/7 + uint32(phase)) % 4 {
			case 0:
				f.SetAccent(xi, yi, '·')
			case 1:
				f.Set(xi, yi, '·')
			case 2:
				f.Set(xi, yi, '.')
			case 3:
			}
		}
	}
}
