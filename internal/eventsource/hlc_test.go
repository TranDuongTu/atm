package eventsource

import "testing"

func TestHLCCompare(t *testing.T) {
	cases := []struct {
		a, b HLC
		want int
	}{
		{HLC{1, 0}, HLC{2, 0}, -1},
		{HLC{2, 0}, HLC{1, 5}, 1},
		{HLC{1, 1}, HLC{1, 2}, -1},
		{HLC{1, 2}, HLC{1, 2}, 0},
	}
	for _, c := range cases {
		if got := c.a.Compare(c.b); got != c.want {
			t.Errorf("Compare(%v, %v) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestClockTickAdvancesLogicalOnSameMillisecond(t *testing.T) {
	c := NewClock(func() int64 { return 1000 })
	if got := c.Tick(); got != (HLC{1000, 0}) {
		t.Fatalf("first tick = %v", got)
	}
	if got := c.Tick(); got != (HLC{1000, 1}) {
		t.Fatalf("second tick = %v", got)
	}
}

func TestClockTickResetsLogicalOnNewMillisecond(t *testing.T) {
	now := int64(1000)
	c := NewClock(func() int64 { return now })
	c.Tick()
	now = 2000
	if got := c.Tick(); got != (HLC{2000, 0}) {
		t.Fatalf("tick after clock advance = %v", got)
	}
}

func TestClockTickIgnoresWallClockRegression(t *testing.T) {
	now := int64(2000)
	c := NewClock(func() int64 { return now })
	c.Tick()
	now = 1500 // wall clock jumps backwards; stamps must not
	if got := c.Tick(); got != (HLC{2000, 1}) {
		t.Fatalf("tick after regression = %v", got)
	}
}

func TestClockObserve(t *testing.T) {
	// Case p' == local.p == e.p: l = max(l_local, l_e) + 1.
	c := NewClock(func() int64 { return 1000 })
	c.Tick() // local = (1000, 0)
	c.Observe(HLC{1000, 7})
	if got := c.Tick(); got != (HLC{1000, 9}) { // observe set (1000,8); tick bumps to 9
		t.Fatalf("after same-p observe, tick = %v", got)
	}

	// Case p' == e.p (remote ahead): l = e.l + 1.
	c2 := NewClock(func() int64 { return 1000 })
	c2.Observe(HLC{5000, 3})
	if got := c2.Tick(); got != (HLC{5000, 5}) { // observe set (5000,4); tick bumps to 5
		t.Fatalf("after remote-ahead observe, tick = %v", got)
	}

	// Case p' == now (wall clock ahead of both): l = 0.
	c3 := NewClock(func() int64 { return 9000 })
	c3.Observe(HLC{5000, 3})
	if got := c3.Tick(); got != (HLC{9000, 1}) { // observe set (9000,0); tick bumps to 1
		t.Fatalf("after wall-ahead observe, tick = %v", got)
	}
}
