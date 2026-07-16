package eventsource

import (
	"fmt"
	"slices"
	"sort"
)

// DAG indexes an event set for causal-ancestry queries (L0). Reachability
// is precomputed as ancestor bitsets: O(1) Reaches after O(V·E/64)
// construction — plenty for ATM-scale logs.
type DAG struct {
	events []*Event       // deterministic topological order
	index  map[string]int // event id → position in events
	anc    [][]uint64     // anc[i] = bitset of events[i]'s ancestors
}

// BuildDAG deduplicates events by id, verifies every parent is present,
// fixes a deterministic topological order (Kahn's algorithm, ready set
// ordered by CompareEvents), and computes ancestor bitsets. A missing
// parent or a cycle (impossible for honest hashes) is an error.
func BuildDAG(events []*Event) (*DAG, error) {
	uniq := make([]*Event, 0, len(events))
	byID := make(map[string]*Event, len(events))
	for _, e := range events {
		if byID[e.ID] == nil {
			byID[e.ID] = e
			uniq = append(uniq, e)
		}
	}
	parentsOf := func(e *Event) []string {
		ps := slices.Clone(e.Parents)
		slices.Sort(ps)
		return slices.Compact(ps) // wire events may repeat a parent
	}
	children := map[string][]string{}
	indeg := make(map[string]int, len(uniq))
	for _, e := range uniq {
		ps := parentsOf(e)
		for _, p := range ps {
			if byID[p] == nil {
				return nil, fmt.Errorf("eventsource: event %s references missing parent %s", e.ID, p)
			}
			children[p] = append(children[p], e.ID)
		}
		indeg[e.ID] = len(ps)
	}
	var ready []*Event
	for _, e := range uniq {
		if indeg[e.ID] == 0 {
			ready = append(ready, e)
		}
	}
	d := &DAG{index: make(map[string]int, len(uniq))}
	for len(ready) > 0 {
		sort.Slice(ready, func(i, j int) bool { return CompareEvents(ready[i], ready[j]) < 0 })
		e := ready[0]
		ready = ready[1:]
		d.index[e.ID] = len(d.events)
		d.events = append(d.events, e)
		for _, cid := range children[e.ID] {
			indeg[cid]--
			if indeg[cid] == 0 {
				ready = append(ready, byID[cid])
			}
		}
	}
	if len(d.events) != len(uniq) {
		return nil, fmt.Errorf("eventsource: parent cycle detected")
	}
	words := (len(d.events) + 63) / 64
	d.anc = make([][]uint64, len(d.events))
	for i, e := range d.events {
		set := make([]uint64, words)
		for _, p := range parentsOf(e) {
			pi := d.index[p]
			set[pi/64] |= 1 << (pi % 64)
			for w, v := range d.anc[pi] {
				set[w] |= v
			}
		}
		d.anc[i] = set
	}
	return d, nil
}

// Reaches reports whether anc happens-before desc: anc is reachable from
// desc by following parents. Strict — an event does not reach itself.
func (d *DAG) Reaches(anc, desc string) bool {
	ai, ok := d.index[anc]
	if !ok {
		return false
	}
	di, ok := d.index[desc]
	if !ok {
		return false
	}
	return d.anc[di][ai/64]&(1<<(ai%64)) != 0
}

// Concurrent reports whether two events are causally unrelated — the only
// definition of concurrency in the suite (L0).
func (d *DAG) Concurrent(a, b string) bool {
	return a != b && !d.Reaches(a, b) && !d.Reaches(b, a)
}

// Frontier returns the ids of events that are not a parent of any held
// event — the parents of the next authored event. Sorted ascending.
func (d *DAG) Frontier() []string {
	isParent := map[string]bool{}
	for _, e := range d.events {
		for _, p := range e.Parents {
			isParent[p] = true
		}
	}
	out := make([]string, 0, 1)
	for _, e := range d.events {
		if !isParent[e.ID] {
			out = append(out, e.ID)
		}
	}
	slices.Sort(out)
	return out
}

// Events returns the events in deterministic topological order.
func (d *DAG) Events() []*Event { return d.events }

// Get returns the event with the given id, or nil.
func (d *DAG) Get(id string) *Event {
	i, ok := d.index[id]
	if !ok {
		return nil
	}
	return d.events[i]
}
