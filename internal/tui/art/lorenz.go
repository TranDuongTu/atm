package art

// lorenz: the Lorenz strange-attractor butterfly. A single particle is
// integrated along the (scaled, projected) attractor each phase and its
// trail is drawn as a fading phosphor line. Math-art, organic, never
// repeats exactly; the head is accent, the tail fades to dim.
type lorenzTheme struct{}

func (lorenzTheme) Name() string { return "lorenz" }

func (lorenzTheme) Draw(f *Frame, seed uint32, phase int) {
	const (
		sigma = 10.0
		rho   = 28.0
		beta  = 8.0 / 3.0
		dt    = 0.01
	)
	trail := f.W() + f.H()
	if trail > 120 {
		trail = 120
	}
	steps := phase + trail
	x, y, z := 0.1+CellHashF(0, 1, seed), 0.0, 0.0
	type pt struct{ x, y float64 }
	pts := make([]pt, 0, trail)
	for i := 0; i < steps; i++ {
		dx := sigma * (y - x)
		dy := x*(rho-z) - y
		dz := x*y - beta*z
		x += dx * dt
		y += dy * dt
		z += dz * dt
		if i > steps-trail {
			sx := (x/30.0)*0.5 + 0.5
			sy := (z/50.0)*0.5 + 0.2
			pts = append(pts, pt{sx, sy})
		}
	}
	for i, p := range pts {
		px := int(p.x * float64(f.W()))
		py := int(p.y * float64(f.H()))
		if px < 0 || px >= f.W() || py < 0 || py >= f.H() {
			continue
		}
		age := trail - 1 - i
		switch {
		case i == len(pts)-1:
			f.SetAccent(px, py, '●')
		case age < trail/4:
			f.SetAccent(px, py, '·')
		default:
			f.Set(px, py, '·')
		}
	}
}
