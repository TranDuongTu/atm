package eventsource

import (
	"testing"
)

func TestWritesOfActionTable(t *testing.T) {
	c := testClock(1000)
	created := testEvent(t, c, replicaA, nil, ActionTaskCreated, Subject{Kind: "task"},
		map[string]any{"alias": "T-1", "title": "t", "labels": []string{"P:x", "P:y"}})
	ws := writesOf(created)
	if len(ws) != 4 { // title, description, membership x2
		t.Fatalf("task.created writes %d slots, want 4: %+v", len(ws), ws)
	}
	if ws[0].slot != (slotKey{created.ID, SlotScalar, "title"}) || ws[0].value != "t" {
		t.Errorf("title write = %+v", ws[0])
	}
	if ws[1].slot != (slotKey{created.ID, SlotScalar, "description"}) || ws[1].value != "" {
		t.Errorf("description write = %+v", ws[1])
	}
	if ws[2].slot != (slotKey{created.ID, SlotMembership, "P:x"}) || ws[2].value != "add" {
		t.Errorf("membership write = %+v", ws[2])
	}

	title := testEvent(t, c, replicaA, []string{created.ID}, ActionTaskTitleChanged,
		Subject{Kind: "task", ID: created.ID}, map[string]any{"title": "t2"})
	ws = writesOf(title)
	if len(ws) != 1 || ws[0].slot != (slotKey{created.ID, SlotScalar, "title"}) || ws[0].value != "t2" {
		t.Errorf("title-changed writes = %+v", ws)
	}

	// Membership delta reads `label`, never the snapshot `labels`.
	add := testEvent(t, c, replicaA, []string{title.ID}, ActionTaskLabelAdded,
		Subject{Kind: "task", ID: created.ID},
		map[string]any{"label": "P:z", "labels": []string{"P:x", "P:y", "P:z"}})
	ws = writesOf(add)
	if len(ws) != 1 || ws[0].slot != (slotKey{created.ID, SlotMembership, "P:z"}) || ws[0].value != "add" {
		t.Errorf("label-added writes = %+v", ws)
	}

	rm := testEvent(t, c, replicaA, []string{add.ID}, ActionTaskRemoved,
		Subject{Kind: "task", ID: created.ID}, nil)
	ws = writesOf(rm)
	if len(ws) != 1 || ws[0].slot != (slotKey{created.ID, SlotExistence, ""}) || ws[0].value != "tombstone" {
		t.Errorf("removed writes = %+v", ws)
	}

	restore := testEvent(t, c, replicaA, []string{rm.ID}, ActionTaskRestored,
		Subject{Kind: "task", ID: created.ID}, nil)
	ws = writesOf(restore)
	if len(ws) != 1 || ws[0].value != "live" {
		t.Errorf("restored writes = %+v", ws)
	}

	// label.upserted: scalar slots only for keys present, plus existence "live".
	upsert := testEvent(t, c, replicaA, []string{restore.ID}, ActionLabelUpserted,
		Subject{Kind: "label", Name: "P:x"}, map[string]any{"description": "d"})
	ws = writesOf(upsert)
	if len(ws) != 2 {
		t.Fatalf("label.upserted writes = %+v", ws)
	}
	if ws[0].slot != (slotKey{"P:x", SlotScalar, "label.description"}) || ws[0].value != "d" {
		t.Errorf("upsert description write = %+v", ws[0])
	}
	if ws[1].slot != (slotKey{"P:x", SlotExistence, ""}) || ws[1].value != "live" {
		t.Errorf("upsert existence write = %+v", ws[1])
	}

	// Retired/unknown actions are inert.
	meta := testEvent(t, c, replicaA, []string{upsert.ID}, "task.meta-changed",
		Subject{Kind: "task", ID: created.ID}, map[string]any{"next_comment_n": 3})
	if ws := writesOf(meta); len(ws) != 0 {
		t.Errorf("meta-changed should be inert, writes = %+v", ws)
	}
}

func TestMaximalWritersDominatedWriteIsIgnored(t *testing.T) {
	c := testClock(1000)
	created := testEvent(t, c, replicaA, nil, ActionTaskCreated, Subject{Kind: "task"},
		map[string]any{"alias": "T-1", "title": "v0"})
	e1 := testEvent(t, c, replicaA, []string{created.ID}, ActionTaskTitleChanged,
		Subject{Kind: "task", ID: created.ID}, map[string]any{"title": "v1"})
	e2 := testEvent(t, c, replicaA, []string{e1.ID}, ActionTaskTitleChanged,
		Subject{Kind: "task", ID: created.ID}, map[string]any{"title": "v2"})
	d, err := BuildDAG([]*Event{created, e1, e2})
	if err != nil {
		t.Fatal(err)
	}
	ws := collectWrites(d)[slotKey{created.ID, SlotScalar, "title"}]
	if len(ws) != 3 {
		t.Fatalf("collected %d writers, want 3", len(ws))
	}
	m := maximalWriters(d, ws)
	if len(m) != 1 || m[0].event.ID != e2.ID {
		t.Fatalf("maximal writers = %+v, want just e2", m)
	}
}

func TestMaximalWritersConcurrentWritesAreBothMaximal(t *testing.T) {
	ca, cb := testClock(1000), testClock(2000)
	created := testEvent(t, ca, replicaA, nil, ActionTaskCreated, Subject{Kind: "task"},
		map[string]any{"alias": "T-1", "title": "v0"})
	a := testEvent(t, ca, replicaA, []string{created.ID}, ActionTaskTitleChanged,
		Subject{Kind: "task", ID: created.ID}, map[string]any{"title": "from A"})
	b := testEvent(t, cb, replicaB, []string{created.ID}, ActionTaskTitleChanged,
		Subject{Kind: "task", ID: created.ID}, map[string]any{"title": "from B"})
	d, err := BuildDAG([]*Event{created, a, b})
	if err != nil {
		t.Fatal(err)
	}
	m := maximalWriters(d, collectWrites(d)[slotKey{created.ID, SlotScalar, "title"}])
	if len(m) != 2 {
		t.Fatalf("maximal writers = %d, want 2 (contested)", len(m))
	}
	// Sorted ascending by the HLC total order: the LWW winner is last.
	if m[0].event.ID != a.ID || m[1].event.ID != b.ID {
		t.Errorf("order = [%s, %s], want [a, b]", m[0].event.ID[:14], m[1].event.ID[:14])
	}
}

func TestCollectWritesDropsEmptyLabelName(t *testing.T) {
	c := testClock(1000)
	created := testEvent(t, c, replicaA, nil, ActionTaskCreated, Subject{Kind: "task"},
		map[string]any{"alias": "T-1", "title": "t", "labels": []string{""}})
	d, err := BuildDAG([]*Event{created})
	if err != nil {
		t.Fatal(err)
	}
	ws := collectWrites(d)
	if _, ok := ws[slotKey{created.ID, SlotMembership, ""}]; ok {
		t.Error("empty label name should not produce a membership slot")
	}
}

func TestCollectWritesDedupesPerEventAndSlot(t *testing.T) {
	c := testClock(1000)
	created := testEvent(t, c, replicaA, nil, ActionTaskCreated, Subject{Kind: "task"},
		map[string]any{"alias": "T-1", "title": "t", "labels": []string{"P:x", "P:x"}})
	d, err := BuildDAG([]*Event{created})
	if err != nil {
		t.Fatal(err)
	}
	ws := collectWrites(d)[slotKey{created.ID, SlotMembership, "P:x"}]
	if len(ws) != 1 {
		t.Fatalf("duplicate payload label produced %d writes, want 1", len(ws))
	}
}
