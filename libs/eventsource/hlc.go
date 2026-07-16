package eventsource

import (
	"sync"
	"time"
)

// HLC is a Hybrid Logical Clock stamp (D2): physical milliseconds since
// epoch and a logical counter. It is a tiebreak only — causality comes
// from parents, never from stamps, and nothing ever orders by `at`.
type HLC struct {
	P int64 `json:"p"`
	L int64 `json:"l"`
}

// Compare orders two stamps: -1 if a < b, 0 if equal, +1 if a > b.
func (a HLC) Compare(b HLC) int {
	switch {
	case a.P < b.P:
		return -1
	case a.P > b.P:
		return 1
	case a.L < b.L:
		return -1
	case a.L > b.L:
		return 1
	}
	return 0
}

// Clock is a replica's local HLC state. Tick stamps a locally-authored
// event; Observe folds in a received event's stamp so later local stamps
// sort after it.
type Clock struct {
	mu   sync.Mutex
	now  func() int64
	last HLC
}

// NewClock returns a Clock reading physical time in milliseconds from now.
// A nil now uses the wall clock.
func NewClock(now func() int64) *Clock {
	if now == nil {
		now = func() int64 { return time.Now().UnixMilli() }
	}
	return &Clock{now: now}
}

// Tick returns the stamp for a locally-authored event.
func (c *Clock) Tick() HLC {
	c.mu.Lock()
	defer c.mu.Unlock()
	p := max(c.last.P, c.now())
	l := int64(0)
	if p == c.last.P {
		l = c.last.L + 1
	}
	c.last = HLC{P: p, L: l}
	return c.last
}

// Observe advances the clock past a received event's stamp.
func (c *Clock) Observe(h HLC) {
	c.mu.Lock()
	defer c.mu.Unlock()
	p := max(c.last.P, h.P, c.now())
	var l int64
	switch {
	case p == c.last.P && p == h.P:
		l = max(c.last.L, h.L) + 1
	case p == c.last.P:
		l = c.last.L + 1
	case p == h.P:
		l = h.L + 1
	}
	c.last = HLC{P: p, L: l}
}
