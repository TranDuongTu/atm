package eventsource

import (
	"sort"
	"strings"
	"time"
)

// Slot kinds (L2). Every mutable piece of state is a slot; one rule — the
// maximal-writer rule — governs all of them.
const (
	SlotScalar     = "scalar"
	SlotMembership = "membership"
	SlotExistence  = "existence"
)

// capabilityFieldPrefix namespaces capability membership slots on the
// project entity away from label membership slots. '!' cannot occur in a
// label name, so the two families can never collide.
const capabilityFieldPrefix = "capability!"

func capabilityField(name string) string  { return capabilityFieldPrefix + name }
func isCapabilityField(field string) bool { return strings.HasPrefix(field, capabilityFieldPrefix) }

// metaFieldPrefix namespaces per-capability metadata scalar slots on the task
// entity away from title/description. Same collision argument as
// capabilityFieldPrefix: '!' cannot occur in the fields it must stay disjoint
// from.
const metaFieldPrefix = "meta!"

func metaField(capability string) string { return metaFieldPrefix + capability }
func isMetaField(field string) bool      { return strings.HasPrefix(field, metaFieldPrefix) }

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
// reported. Because dangling writes are inert rather than errors (spec
// decision 10), Entity may name an entity with no creation event in the
// set and so appear in none of State.Projects/Tasks/Comments/Labels —
// callers must not assume a lookup by Entity succeeds.
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
	case ActionProjectCapabilityEnabled:
		for _, c := range e.PayloadStringOrList("capability") {
			out = append(out, w(e.Subject.ID, SlotMembership, capabilityField(c), "add"))
		}
	case ActionProjectCapabilityDisabled:
		for _, c := range e.PayloadStringOrList("capability") {
			out = append(out, w(e.Subject.ID, SlotMembership, capabilityField(c), "remove"))
		}
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
	case ActionTaskCapabilityMetaSet:
		if c := str("capability"); c != "" {
			out = append(out, w(e.Subject.ID, SlotScalar, metaField(c), str("payload")))
		}
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
			if w.slot.kind == SlotMembership && w.slot.field == "" {
				continue // malformed field: empty label name
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

// State is the fold's output: a pure function of the event set (D4). Two
// replicas holding the same set compute deep-equal State. Nothing below
// consults arrival order, at, or seq.
type State struct {
	Projects  map[string]*ProjectState // by identity
	Tasks     map[string]*TaskState    // by identity
	Comments  map[string]*CommentState // by identity
	Labels    map[string]*LabelState   // by name (a label's name is its identity)
	Contested []ContestedSlot
	Frontier  []string
}

// EntityMeta is the part of an entity every kind shares. Alias is the
// stored display constant from the creation event (L1-2); Tombstoned
// entities remain present so a restore can find them.
type EntityMeta struct {
	ID             string
	Alias          string
	Tombstoned     bool
	CreatedAt      time.Time
	CreatedBy      string
	CreatedHLC     HLC
	CreatedReplica string
	UpdatedAt      time.Time
	UpdatedBy      string
}

type ProjectState struct {
	EntityMeta
	Code string
	Name string
	// Capabilities is the enabled capability set. nil = no capability event
	// was ever recorded (a legacy project — callers treat nil as "all
	// built-ins enabled"); non-nil empty = explicitly none. Sorted.
	Capabilities []string
}

type TaskState struct {
	EntityMeta
	Title       string
	Description string
	Labels      []string
	// Meta maps capability name → opaque payload. A key whose maximal write
	// is the empty string is absent, not empty — clearing is writing "".
	Meta map[string]string
}

type CommentState struct {
	EntityMeta
	TaskRef    string
	ReplyToRef string
	Body       string
	Labels     []string
}

type LabelState struct {
	Name        string
	Description string
	Expr        string
	Tombstoned  bool
	UpdatedAt   time.Time
	UpdatedBy   string
}

// isNamespaceName reports whether a label name denotes a namespace label.
// Namespace-ness is a property of the name alone, so it holds even for a
// label with no stored record. Sole definition of the ":*" rule.
func isNamespaceName(name string) bool {
	return strings.HasSuffix(name, ":*")
}

// IsComputed reports whether membership is derived rather than asserted:
// boards (Expr set) and namespace labels. For a computed label every
// membership slot is inert (L2-6). Sole definition of the L2-6 rule — every
// other site delegates here rather than restating it.
func (l *LabelState) IsComputed() bool {
	return l.Expr != "" || isNamespaceName(l.Name)
}

// FoldEvents builds the DAG and folds it.
func FoldEvents(events []*Event) (*State, error) {
	d, err := BuildDAG(events)
	if err != nil {
		return nil, err
	}
	return Fold(d), nil
}

// Fold derives State from the event set. It never blocks, never prompts,
// and never waits for a human (L2-5): contested slots are surfaced in
// State.Contested, and a losing write stays in the log — LWW selects a
// current value, it does not erase history.
func Fold(d *DAG) *State {
	bySlot := collectWrites(d)
	maximal := make(map[slotKey][]slotWrite, len(bySlot))
	for k, ws := range bySlot {
		maximal[k] = maximalWriters(d, ws)
	}

	st := &State{
		Projects: map[string]*ProjectState{},
		Tasks:    map[string]*TaskState{},
		Comments: map[string]*CommentState{},
		Labels:   map[string]*LabelState{},
		Frontier: d.Frontier(),
	}

	// Pass 1 — materialize entities from their creation events (labels:
	// from their first upsert). Dangling writes stay inert (spec decision 10).
	for _, e := range d.Events() {
		switch e.Action {
		case ActionProjectCreated:
			p := &ProjectState{EntityMeta: metaFor(e)}
			p.Code = p.Alias
			p.Name = scalarValue(maximal[slotKey{e.ID, SlotScalar, "name"}])
			p.Tombstoned = tombstoned(maximal[slotKey{e.ID, SlotExistence, ""}])
			st.Projects[e.ID] = p
		case ActionTaskCreated:
			tk := &TaskState{EntityMeta: metaFor(e)}
			tk.Title = scalarValue(maximal[slotKey{e.ID, SlotScalar, "title"}])
			tk.Description = scalarValue(maximal[slotKey{e.ID, SlotScalar, "description"}])
			tk.Tombstoned = tombstoned(maximal[slotKey{e.ID, SlotExistence, ""}])
			st.Tasks[e.ID] = tk
		case ActionCommentCreated:
			cm := &CommentState{EntityMeta: metaFor(e)}
			cm.TaskRef, _ = e.PayloadString("task_ref")
			cm.ReplyToRef, _ = e.PayloadString("reply_to_ref")
			cm.Body = scalarValue(maximal[slotKey{e.ID, SlotScalar, "body"}])
			cm.Tombstoned = tombstoned(maximal[slotKey{e.ID, SlotExistence, ""}])
			st.Comments[e.ID] = cm
		case ActionLabelUpserted:
			name := e.Subject.Name
			if name == "" || st.Labels[name] != nil {
				continue
			}
			l := &LabelState{Name: name}
			l.Description = scalarValue(maximal[slotKey{name, SlotScalar, "label.description"}])
			l.Expr = scalarValue(maximal[slotKey{name, SlotScalar, "label.expr"}])
			l.Tombstoned = tombstoned(maximal[slotKey{name, SlotExistence, ""}])
			l.UpdatedAt = e.At
			l.UpdatedBy = e.Actor
			st.Labels[name] = l
		}
	}

	// A label may be referenced by a membership slot without ever having been
	// upserted, so there may be no LabelState to ask; fall back to the name.
	computed := func(name string) bool {
		if l := st.Labels[name]; l != nil {
			return l.IsComputed()
		}
		return isNamespaceName(name)
	}

	// Pass 2 — membership and contested, iterating slots in sorted order
	// so output is deterministic. Membership slots of computed labels are
	// inert: skipped for both membership AND contested reporting.
	keys := make([]slotKey, 0, len(maximal))
	for k := range maximal {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		a, b := keys[i], keys[j]
		if a.entity != b.entity {
			return a.entity < b.entity
		}
		if a.kind != b.kind {
			return a.kind < b.kind
		}
		return a.field < b.field
	})
	for _, k := range keys {
		ws := maximal[k]
		if k.kind == SlotMembership && isCapabilityField(k.field) {
			// Capability membership on the project entity. The presence of
			// ANY maximal writer marks the set as explicitly recorded
			// (non-nil), even when the resolution is "not a member".
			if p := st.Projects[k.entity]; p != nil {
				if p.Capabilities == nil {
					p.Capabilities = []string{}
				}
				if member(ws) {
					p.Capabilities = append(p.Capabilities, strings.TrimPrefix(k.field, capabilityFieldPrefix))
				}
			}
		} else if k.kind == SlotMembership {
			if computed(k.field) {
				continue
			}
			if member(ws) {
				switch {
				case st.Tasks[k.entity] != nil:
					st.Tasks[k.entity].Labels = append(st.Tasks[k.entity].Labels, k.field)
				case st.Comments[k.entity] != nil:
					st.Comments[k.entity].Labels = append(st.Comments[k.entity].Labels, k.field)
				}
			}
		} else if k.kind == SlotScalar && isMetaField(k.field) {
			if tk := st.Tasks[k.entity]; tk != nil {
				if v := scalarValue(ws); v != "" {
					if tk.Meta == nil {
						tk.Meta = map[string]string{}
					}
					tk.Meta[strings.TrimPrefix(k.field, metaFieldPrefix)] = v
				}
			}
		}
		if len(ws) > 1 {
			cs := ContestedSlot{Entity: k.entity, Kind: k.kind, Field: k.field}
			for _, w := range ws {
				cs.Writers = append(cs.Writers, w.event.ID)
			}
			st.Contested = append(st.Contested, cs)
		}
	}

	// Pass 3 — UpdatedAt/UpdatedBy from the HLC-greatest maximal writer
	// across each entity's slots (creation included, so it is the floor).
	// Both loops iterate sorted keys — `keys` from Pass 2, then the entity ids
	// — per the global constraint that fold output never reads map order.
	lastWrite := map[string]*Event{}
	for _, k := range keys {
		for _, w := range maximal[k] {
			if cur := lastWrite[k.entity]; cur == nil || CompareEvents(cur, w.event) < 0 {
				lastWrite[k.entity] = w.event
			}
		}
	}
	entities := make([]string, 0, len(lastWrite))
	for entity := range lastWrite {
		entities = append(entities, entity)
	}
	sort.Strings(entities)
	for _, entity := range entities {
		e := lastWrite[entity]
		switch {
		case st.Projects[entity] != nil:
			st.Projects[entity].UpdatedAt, st.Projects[entity].UpdatedBy = e.At, e.Actor
		case st.Tasks[entity] != nil:
			st.Tasks[entity].UpdatedAt, st.Tasks[entity].UpdatedBy = e.At, e.Actor
		case st.Comments[entity] != nil:
			st.Comments[entity].UpdatedAt, st.Comments[entity].UpdatedBy = e.At, e.Actor
		case st.Labels[entity] != nil:
			st.Labels[entity].UpdatedAt, st.Labels[entity].UpdatedBy = e.At, e.Actor
		}
	}

	// Pass 4 — task activity from comment writes (ATM-fe669c). v1 AddComment
	// bumped the parent task via task.meta-changed; ATM-0106 retired that
	// event and the bump went with it, so under v2 a discussion-heavy task
	// looked stale. Restoring a stored bump would re-add whole-record
	// payloads and log noise and create spurious contested slots on
	// concurrent comments. Instead derive activity at read time: a task's
	// effective activity timestamp is the max of its own last write and
	// its live comments' last writes. This is a pure function of the event
	// set (D4-safe) — it reuses the same maximal-writer `lastWrite` map and
	// the HLC total order, so it inherits the determinism of the rest of the
	// fold. Tombstoned comments are inert (D5/decision 10) and do not
	// contribute. Iterating sorted task and comment ids per the global
	// constraint that fold output never reads map order.
	taskIDs := make([]string, 0, len(st.Tasks))
	for id := range st.Tasks {
		taskIDs = append(taskIDs, id)
	}
	sort.Strings(taskIDs)
	for _, taskID := range taskIDs {
		task := st.Tasks[taskID]
		winning := lastWrite[taskID]
		commentIDs := make([]string, 0, len(st.Comments))
		for cid, c := range st.Comments {
			if c.TaskRef == taskID && !c.Tombstoned {
				commentIDs = append(commentIDs, cid)
			}
		}
		sort.Strings(commentIDs)
		for _, cid := range commentIDs {
			if cw := lastWrite[cid]; cw != nil {
				if winning == nil || CompareEvents(winning, cw) < 0 {
					winning = cw
				}
			}
		}
		if winning != nil {
			task.UpdatedAt, task.UpdatedBy = winning.At, winning.Actor
		}
	}
	return st
}

func metaFor(e *Event) EntityMeta {
	alias, _ := e.PayloadString("alias")
	return EntityMeta{
		ID:             e.ID,
		Alias:          alias,
		CreatedAt:      e.At,
		CreatedBy:      e.Actor,
		CreatedHLC:     e.HLC,
		CreatedReplica: e.Replica,
		UpdatedAt:      e.At,
		UpdatedBy:      e.Actor,
	}
}

// scalarValue resolves a scalar slot: highest HLC among maximal writers
// wins (ws is sorted ascending, so the winner is last).
func scalarValue(ws []slotWrite) string {
	if len(ws) == 0 {
		return ""
	}
	return ws[len(ws)-1].value
}

// tombstoned resolves an existence slot: keep beats drop (L2-2) — any
// maximal "live" (task.restored, label.upserted) means live; otherwise any
// maximal tombstone means tombstoned; no writers means live.
func tombstoned(ws []slotWrite) bool {
	if len(ws) == 0 {
		return false
	}
	for _, w := range ws {
		if w.value == "live" {
			return false
		}
	}
	return true
}

// member resolves a membership slot: add-wins (L2-2). Equivalent to the
// OR-Set read "some add is not a causal ancestor of any remove" — an add
// dominated only by adds survives into the maximal set.
func member(ws []slotWrite) bool {
	for _, w := range ws {
		if w.value == "add" {
			return true
		}
	}
	return false
}

func compareCreation(a, b *EntityMeta) int {
	if c := a.CreatedHLC.Compare(b.CreatedHLC); c != 0 {
		return c
	}
	if c := strings.Compare(a.CreatedReplica, b.CreatedReplica); c != 0 {
		return c
	}
	return strings.Compare(a.ID, b.ID)
}

// TasksByCreation returns all tasks (tombstoned included — callers filter)
// in creation order: the HLC creation stamp, which unlike alias order
// stays meaningful after a merge (L1-3).
func (s *State) TasksByCreation() []*TaskState {
	out := make([]*TaskState, 0, len(s.Tasks))
	for _, t := range s.Tasks {
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return compareCreation(&out[i].EntityMeta, &out[j].EntityMeta) < 0 })
	return out
}

// CommentsByCreation returns a task's comments in creation order.
func (s *State) CommentsByCreation(taskRef string) []*CommentState {
	var out []*CommentState
	for _, c := range s.Comments {
		if c.TaskRef == taskRef {
			out = append(out, c)
		}
	}
	sort.Slice(out, func(i, j int) bool { return compareCreation(&out[i].EntityMeta, &out[j].EntityMeta) < 0 })
	return out
}
