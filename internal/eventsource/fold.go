package eventsource

import (
	"sort"
)

// Slot kinds (L2). Every mutable piece of state is a slot; one rule — the
// maximal-writer rule — governs all of them.
const (
	SlotScalar     = "scalar"
	SlotMembership = "membership"
	SlotExistence  = "existence"
)

// slotKey names one mutable piece of state.
type slotKey struct {
	entity string // entity identity; label name for label entities
	kind   string
	field  string // scalar: field name; membership: label name; existence: ""
}

// slotWrite is one event's write to one slot. value is the written scalar
// value, "add"/"remove" for membership, or "live"/"tombstone" for
// existence.
type slotWrite struct {
	slot  slotKey
	event *Event
	value string
}

// ContestedSlot reports a slot with more than one maximal writer (L2-1).
// Writers are sorted ascending by the HLC total order, so for a scalar
// slot the last writer is the LWW winner. Reported structurally — filtering
// same-outcome noise is board-vocabulary policy, not fold policy (spec
// decision 9). Membership slots of computed labels are inert and never
// reported.
type ContestedSlot struct {
	Entity  string
	Kind    string
	Field   string
	Writers []string
}

// writesOf lists the slot writes an event makes — the L2 action table.
// Unknown actions (including the retired task.meta-changed riding through
// the D6 upgrade) write nothing: they are preserved in the DAG and
// participate in causality, but no rule reads them (D5).
func writesOf(e *Event) []slotWrite {
	str := func(key string) string { s, _ := e.PayloadString(key); return s }
	w := func(entity, kind, field, value string) slotWrite {
		return slotWrite{slot: slotKey{entity, kind, field}, event: e, value: value}
	}
	var out []slotWrite
	switch e.Action {
	case ActionProjectCreated:
		out = append(out, w(e.ID, SlotScalar, "name", str("name")))
	case ActionProjectNameChanged:
		out = append(out, w(e.Subject.ID, SlotScalar, "name", str("name")))
	case ActionTaskCreated:
		out = append(out,
			w(e.ID, SlotScalar, "title", str("title")),
			w(e.ID, SlotScalar, "description", str("description")))
		for _, l := range e.PayloadStringOrList("labels") {
			out = append(out, w(e.ID, SlotMembership, l, "add"))
		}
	case ActionTaskTitleChanged:
		out = append(out, w(e.Subject.ID, SlotScalar, "title", str("title")))
	case ActionTaskDescChanged:
		out = append(out, w(e.Subject.ID, SlotScalar, "description", str("description")))
	case ActionCommentCreated:
		out = append(out, w(e.ID, SlotScalar, "body", str("body")))
		for _, l := range e.PayloadStringOrList("labels") {
			out = append(out, w(e.ID, SlotMembership, l, "add"))
		}
	case ActionCommentBodyChanged:
		out = append(out, w(e.Subject.ID, SlotScalar, "body", str("body")))
	case ActionTaskLabelAdded, ActionCommentLabelAdded:
		for _, l := range e.PayloadStringOrList("label") {
			out = append(out, w(e.Subject.ID, SlotMembership, l, "add"))
		}
	case ActionTaskLabelRemoved, ActionCommentLabelRemoved:
		for _, l := range e.PayloadStringOrList("label") {
			out = append(out, w(e.Subject.ID, SlotMembership, l, "remove"))
		}
	case ActionLabelUpserted:
		name := e.Subject.Name
		fields := e.payloadFields()
		if _, ok := fields["description"]; ok {
			out = append(out, w(name, SlotScalar, "label.description", str("description")))
		}
		if _, ok := fields["expr"]; ok {
			out = append(out, w(name, SlotScalar, "label.expr", str("expr")))
		}
		// An upsert resurrects a removed label (spec decision 6): it
		// writes existence "live", causally dominating any tombstone it
		// observed; concurrent upsert‖remove resolves live (keep beats
		// drop, L2-2).
		out = append(out, w(name, SlotExistence, "", "live"))
	case ActionLabelRemoved:
		out = append(out, w(e.Subject.Name, SlotExistence, "", "tombstone"))
	case ActionProjectRemoved, ActionTaskRemoved, ActionCommentRemoved:
		out = append(out, w(e.Subject.ID, SlotExistence, "", "tombstone"))
	case ActionTaskRestored:
		out = append(out, w(e.Subject.ID, SlotExistence, "", "live"))
	}
	return out
}

// collectWrites groups every event's slot writes by slot, deduplicating a
// same-event double write (e.g. a duplicated payload label) so an event
// can never contest with itself.
func collectWrites(d *DAG) map[slotKey][]slotWrite {
	bySlot := map[slotKey][]slotWrite{}
	seen := map[slotKey]map[string]bool{}
	for _, e := range d.Events() {
		for _, w := range writesOf(e) {
			if w.slot.entity == "" {
				continue // malformed subject: nothing to attach to
			}
			if seen[w.slot] == nil {
				seen[w.slot] = map[string]bool{}
			}
			if seen[w.slot][e.ID] {
				continue
			}
			seen[w.slot][e.ID] = true
			bySlot[w.slot] = append(bySlot[w.slot], w)
		}
	}
	return bySlot
}

// maximalWriters filters a slot's writes to those not causally dominated
// by another write of the same slot — the maximal-writer rule (L2-1). The
// result is sorted ascending by the HLC total order; a slot is contested
// iff more than one write survives.
func maximalWriters(d *DAG, ws []slotWrite) []slotWrite {
	var out []slotWrite
	for i, w := range ws {
		dominated := false
		for j, o := range ws {
			if i != j && d.Reaches(w.event.ID, o.event.ID) {
				dominated = true
				break
			}
		}
		if !dominated {
			out = append(out, w)
		}
	}
	sort.Slice(out, func(i, j int) bool { return CompareEvents(out[i].event, out[j].event) < 0 })
	return out
}
