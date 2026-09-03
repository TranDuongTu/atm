package tui

import (
	"strings"

	"atm/internal/capability"
)

// flowState is where a task stands with respect to ONE flow capability,
// derived from the socket labels it currently carries.
type flowState int

const (
	flowNone flowState = iota
	flowOpen
	flowFinished
	flowEvicted
)

// flowSockets is the momentum chart's only view of a capability: the label
// prefixes that mean claimed, the finish label, and the evict prefix. It is
// built once per (flow, project) from the Flow declarations — the TUI never
// names a capability's vocabulary itself.
type flowSockets struct {
	claimPrefixes []string
	finish        string
	evictPrefix   string
}

// newFlowSockets prefixes the unprefixed ClaimExprs ("scrum:*") with the
// project code and drops the one that is the evict namespace: ClaimExprs
// exist for the pool expression, where evicted-by-me also means not-in-pool,
// but for momentum an eviction is a departure, never an arrival.
func newFlowSockets(f capability.Flow, code string) flowSockets {
	s := flowSockets{
		finish:      f.FinishLabel(code).Name,
		evictPrefix: strings.TrimSuffix(f.EvictLabel(code).Name, "*"),
	}
	for _, expr := range f.ClaimExprs() {
		p := code + ":" + strings.TrimSuffix(expr, "*")
		if p == s.evictPrefix {
			continue
		}
		s.claimPrefixes = append(s.claimPrefixes, p)
	}
	return s
}

func (s flowSockets) isClaim(label string) bool {
	for _, p := range s.claimPrefixes {
		if strings.HasPrefix(label, p) {
			return true
		}
	}
	return false
}

// relevant reports whether a label carries socket meaning for this flow.
func (s flowSockets) relevant(label string) bool {
	return label == s.finish || strings.HasPrefix(label, s.evictPrefix) || s.isClaim(label)
}

// stateOf derives the flow state from the socket labels a task carries.
// Evicted wins over finished (settled out is final), finished wins over
// open (qa's done sits on its claim axis, so a finished task also matches
// a claim prefix).
func (s flowSockets) stateOf(labels map[string]bool) flowState {
	state := flowNone
	for l := range labels {
		switch {
		case strings.HasPrefix(l, s.evictPrefix):
			return flowEvicted
		case l == s.finish:
			state = flowFinished
		case s.isClaim(l) && state == flowNone:
			state = flowOpen
		}
	}
	return state
}
