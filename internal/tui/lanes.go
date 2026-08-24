package tui

import "atm/internal/capability"

// laneKind is a lane's fixed position in pane [2]'s strip. The order is the
// flow contract's order — Inbox -> Pipeline -> Out — and it never varies: a
// user learns where a lane is once, and the capability underneath changes
// what the lanes CONTAIN, never where they sit.
type laneKind int

const (
	laneInbox laneKind = iota
	lanePipeline
	laneOut
)

// laneNames are the display names. The contract names (intake/pipeline/evict)
// stay in code and docs; the user only ever reads these three.
var laneNames = [3]string{"Inbox", "Pipeline", "Out"}

func (k laneKind) String() string {
	if k < laneInbox || k > laneOut {
		return ""
	}
	return laneNames[k]
}

// laneRow is one lane card's state: which seeded board backs it, how much
// work sits in it, and whether that board is unusable.
type laneRow struct {
	Kind      laneKind
	BoardName string // FullName of the seeded lane board
	Count     int
	Broken    bool // board missing or expression invalid — render dim, count "?"
}

// lanesModel is pane [2]'s lane strip state for the current flow capability.
// It replaces the board ring: there is no authoring, no drill-down and no
// ordering here, because the lanes are not the user's to arrange — they are
// the flow's own shape, and the only choice is which one to look at.
type lanesModel struct {
	m        *Model
	lanes    [3]laneRow // fixed positions: [0]=Inbox, [1]=Pipeline, [2]=Out
	selected laneKind   // default lanePipeline on entry/switch
}

func newLanesModel(m *Model) lanesModel {
	l := lanesModel{m: m, selected: lanePipeline}
	l.clear()
	return l
}

// clear resets every lane to an empty row while keeping the fixed kinds, so
// the strip renders three cards even before a flow resolves.
func (l *lanesModel) clear() {
	for i := range l.lanes {
		l.lanes[i] = laneRow{Kind: laneKind(i)}
	}
}

// currentFlow resolves the flow capability pane [2] is scoped to, or nil when
// the scope is a registry capability, the unmanaged pseudo-capability, or no
// project at all. The adapter never names a capability: it asks the registry
// for its flows and matches by the current name.
func (l *lanesModel) currentFlow() capability.Flow {
	scope := l.m.projectScope
	if scope == "" {
		return nil
	}
	cur := l.m.capability.current
	if cur == "" || cur == unmanagedCapability {
		return nil
	}
	for _, f := range l.m.regFor(scope).Flows() {
		if f.Name() == cur {
			return f
		}
	}
	return nil
}

// refresh resolves the current flow's three lane boards and recounts them. A
// lane whose board was never seeded is Broken rather than empty: an empty
// lane and a missing lane look identical in a count, and only one of them is
// the user's problem to fix.
func (l *lanesModel) refresh() {
	l.clear()
	f := l.currentFlow()
	if f == nil {
		return
	}
	scope := l.m.projectScope
	set := f.Lanes(scope)
	stored := map[string]bool{}
	for _, lab := range l.m.store.LabelList(scope, "") {
		stored[lab.Name] = true
	}
	for i, name := range [3]string{set.Inbox, set.Pipeline, set.Out} {
		row := laneRow{Kind: laneKind(i), BoardName: name}
		if name == "" || !stored[name] {
			row.Broken = true
		} else {
			row.Count, row.Broken = l.m.boardCount(name)
		}
		l.lanes[i] = row
	}
}

// move steps the selection one lane and clamps at both edges. No wrap: with
// three fixed positions, wrapping would make [ and ] ambiguous about where
// you land, and the edges are the cheapest possible orientation cue.
func (l *lanesModel) move(dir int) {
	next := laneKind(int(l.selected) + dir)
	if next < laneInbox {
		next = laneInbox
	}
	if next > laneOut {
		next = laneOut
	}
	if next == l.selected {
		return
	}
	l.selected = next
	l.applyFocus()
}

// applyFocus pushes the selected lane's board through the one seam the Tasks
// pane exposes. Every lane is a plain board filter — the focus modes that
// grouped and negated are not part of the lane contract.
func (l *lanesModel) applyFocus() {
	l.m.tasks.setFocus(taskFocus{mode: focusOff}, l.lanes[l.selected].BoardName)
}

// selectDefault lands on Pipeline: on entry and on every capability switch,
// the question the user is most often answering is "what is being built".
func (l *lanesModel) selectDefault() {
	l.selected = lanePipeline
	l.applyFocus()
}
