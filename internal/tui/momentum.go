package tui

import (
	"encoding/json"
	"strings"
	"time"

	"atm/internal/capability"
	"atm/internal/core"
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

// momentumSeries is one window of the momentum chart: three per-bucket
// rates and the open depth sampled at each bucket's end. All four slices
// have exactly spec.buckets entries.
type momentumSeries struct {
	In, Done, Evict, Open []int
}

func (s momentumSeries) totals() (in, done, evict int) {
	for _, v := range s.In {
		in += v
	}
	for _, v := range s.Done {
		done += v
	}
	for _, v := range s.Evict {
		evict += v
	}
	return in, done, evict
}

// momentumPayload is the union of the payload keys the fold reads.
type momentumPayload struct {
	Label  string   `json:"label"`
	Labels []string `json:"labels"`
}

// momentumBuckets folds the project's log into a momentumSeries for one
// flow capability. It walks every entry in Seq order (the slice is already
// in fold order) so the open set is exact at the window's left edge, and
// counts rates only for entries inside the window. State per task is
// derived from the socket labels it currently carries (flowSockets.stateOf),
// so an add/remove pair lands on the same state whichever order it comes in.
func momentumBuckets(entries []core.LogEntry, sockets flowSockets, spec chartRangeSpec, end time.Time) momentumSeries {
	if spec.buckets <= 0 || spec.bucketDays <= 0 {
		return momentumSeries{}
	}
	out := momentumSeries{
		In:    make([]int, spec.buckets),
		Done:  make([]int, spec.buckets),
		Evict: make([]int, spec.buckets),
		Open:  make([]int, spec.buckets),
	}
	start, endDay := chartWindow(spec, end)
	bucketOf := func(day time.Time) (int, bool) {
		if day.Before(start) || day.After(endDay) {
			return 0, false
		}
		b := int(day.Sub(start) / (24 * time.Hour) / time.Duration(spec.bucketDays))
		return b, b >= 0 && b < spec.buckets
	}

	carried := map[string]map[string]bool{} // task id -> socket labels it carries
	state := map[string]flowState{}
	open := 0
	sampled := 0 // buckets whose Open has been written
	sampleThrough := func(bucket int) {
		for ; sampled < bucket && sampled < spec.buckets; sampled++ {
			out.Open[sampled] = open
		}
	}
	apply := func(id string, inWindow bool, bucket int) {
		prev := state[id]
		next := sockets.stateOf(carried[id])
		if next == prev {
			return
		}
		if prev == flowOpen {
			open--
		}
		if next == flowOpen {
			open++
		}
		if inWindow {
			switch {
			case next == flowOpen && prev == flowNone:
				out.In[bucket]++
			case next == flowFinished:
				out.Done[bucket]++
			case next == flowEvicted:
				out.Evict[bucket]++
			}
		}
		state[id] = next
	}

	for _, e := range entries {
		if e.Subject.Kind != "task" || e.Subject.ID == "" || e.At.IsZero() {
			continue
		}
		day := e.At.UTC().Truncate(24 * time.Hour)
		if day.After(endDay) {
			continue // Seq order is the fold order, not strictly chronological: never stop early
		}
		bucket, inWindow := bucketOf(day)
		if inWindow {
			sampleThrough(bucket) // close every bucket that ended before this entry
		}
		id := e.Subject.ID
		var p momentumPayload
		if len(e.Payload) > 0 {
			_ = json.Unmarshal(e.Payload, &p)
		}
		switch e.Action {
		case "task.created":
			for _, l := range p.Labels {
				if sockets.relevant(l) {
					if carried[id] == nil {
						carried[id] = map[string]bool{}
					}
					carried[id][l] = true
				}
			}
		case "task.label-added":
			if !sockets.relevant(p.Label) {
				continue
			}
			if carried[id] == nil {
				carried[id] = map[string]bool{}
			}
			carried[id][p.Label] = true
		case "task.label-removed":
			if !sockets.relevant(p.Label) {
				continue
			}
			delete(carried[id], p.Label)
		case "task.removed":
			delete(carried, id)
		default:
			continue
		}
		apply(id, inWindow, bucket)
	}
	sampleThrough(spec.buckets) // the current bucket and any trailing empty ones
	return out
}
