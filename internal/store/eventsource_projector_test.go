package store

import (
	"testing"

	"atm/internal/eventsource"
)

func TestCacheProjectFromV2StateWritesCompatibilityRows(t *testing.T) {
	s := testStore(t)
	clock := eventsource.NewClock(func() int64 { return 1000 })
	project, err := eventsource.NewEvent(clock, "r_0123456789abcdefghjkmnpqrs", nil, eventsource.Draft{
		Actor:   "admin@cli:unset",
		Action:  "project.created",
		Subject: eventsource.Subject{Kind: "project", Code: "ATM"},
		Payload: map[string]any{"alias": "ATM", "name": "Agent Tasks Management"},
	})
	if err != nil {
		t.Fatal(err)
	}
	task, err := eventsource.NewEvent(clock, "r_0123456789abcdefghjkmnpqrs", []string{project.ID}, eventsource.Draft{
		Actor:   "admin@cli:unset",
		Action:  "task.created",
		Subject: eventsource.Subject{Kind: "task"},
		Payload: map[string]any{"alias": "ATM-abcdef", "title": "First", "description": "Body", "labels": []string{"ATM:status:open"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	state, err := eventsource.FoldEvents([]*eventsource.Event{project, task})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.cacheProjectFromV2State("ATM", state, 2); err != nil {
		t.Fatal(err)
	}
	p, err := s.GetProject("ATM")
	if err != nil {
		t.Fatal(err)
	}
	if p.Code != "ATM" || p.Name != "Agent Tasks Management" {
		t.Fatalf("project = %#v", p)
	}
	db, err := s.cacheDB()
	if err != nil {
		t.Fatal(err)
	}
	tk, ok, err := cacheGetTask(db, "ATM-abcdef")
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("task row missing after projection")
	}
	if tk.Title != "First" || tk.Description != "Body" {
		t.Fatalf("task = %#v", tk)
	}
}

// TestCacheProjectFromV2StateDeletesStaleRows exercises the semantic the L3
// plan called out explicitly: projection is delete-then-insert for the
// project's rows, not upsert-only. An upsert-only projector would leave a
// tombstoned task's row in the cache (nothing "upserts" it away) and would
// also leave a row behind after a re-upgrade discards the branch that
// created it (the new fold never references that task's identity at all,
// so an upsert pass would never touch, let alone delete, its row).
func TestCacheProjectFromV2StateDeletesStaleRows(t *testing.T) {
	s := testStore(t)
	clock := eventsource.NewClock(func() int64 { return 1000 })
	replica := "r_0123456789abcdefghjkmnpqrs"

	project, err := eventsource.NewEvent(clock, replica, nil, eventsource.Draft{
		Actor:   "admin@cli:unset",
		Action:  "project.created",
		Subject: eventsource.Subject{Kind: "project", Code: "ATM"},
		Payload: map[string]any{"alias": "ATM", "name": "Agent Tasks Management"},
	})
	if err != nil {
		t.Fatal(err)
	}
	taskA, err := eventsource.NewEvent(clock, replica, []string{project.ID}, eventsource.Draft{
		Actor:   "admin@cli:unset",
		Action:  "task.created",
		Subject: eventsource.Subject{Kind: "task"},
		Payload: map[string]any{"alias": "ATM-aaaaaa", "title": "Alive", "description": ""},
	})
	if err != nil {
		t.Fatal(err)
	}
	taskB, err := eventsource.NewEvent(clock, replica, []string{project.ID}, eventsource.Draft{
		Actor:   "admin@cli:unset",
		Action:  "task.created",
		Subject: eventsource.Subject{Kind: "task"},
		Payload: map[string]any{"alias": "ATM-bbbbbb", "title": "Removed", "description": ""},
	})
	if err != nil {
		t.Fatal(err)
	}
	removeB, err := eventsource.NewEvent(clock, replica, []string{taskB.ID}, eventsource.Draft{
		Actor:   "admin@cli:unset",
		Action:  "task.removed",
		Subject: eventsource.Subject{Kind: "task", ID: taskB.ID},
	})
	if err != nil {
		t.Fatal(err)
	}

	state, err := eventsource.FoldEvents([]*eventsource.Event{project, taskA, taskB, removeB})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.cacheProjectFromV2State("ATM", state, 4); err != nil {
		t.Fatal(err)
	}

	db, err := s.cacheDB()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok, err := cacheGetTask(db, "ATM-aaaaaa"); err != nil {
		t.Fatal(err)
	} else if !ok {
		t.Fatal("live task ATM-aaaaaa missing after first projection")
	}
	if _, ok, err := cacheGetTask(db, "ATM-bbbbbb"); err != nil {
		t.Fatal(err)
	} else if ok {
		t.Fatal("tombstoned task ATM-bbbbbb present after first projection")
	}

	// Simulate a re-upgrade that discards the branch above and folds a
	// brand-new event set: taskA's identity never appears in this fold at
	// all, so an upsert-only projector would never learn it should be gone.
	freshProject, err := eventsource.NewEvent(clock, replica, nil, eventsource.Draft{
		Actor:   "admin@cli:unset",
		Action:  "project.created",
		Subject: eventsource.Subject{Kind: "project", Code: "ATM"},
		Payload: map[string]any{"alias": "ATM", "name": "Agent Tasks Management"},
	})
	if err != nil {
		t.Fatal(err)
	}
	taskC, err := eventsource.NewEvent(clock, replica, []string{freshProject.ID}, eventsource.Draft{
		Actor:   "admin@cli:unset",
		Action:  "task.created",
		Subject: eventsource.Subject{Kind: "task"},
		Payload: map[string]any{"alias": "ATM-cccccc", "title": "Fresh", "description": ""},
	})
	if err != nil {
		t.Fatal(err)
	}
	freshState, err := eventsource.FoldEvents([]*eventsource.Event{freshProject, taskC})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.cacheProjectFromV2State("ATM", freshState, 2); err != nil {
		t.Fatal(err)
	}

	if _, ok, err := cacheGetTask(db, "ATM-aaaaaa"); err != nil {
		t.Fatal(err)
	} else if ok {
		t.Fatal("stale task ATM-aaaaaa from discarded fold survived re-projection")
	}
	if _, ok, err := cacheGetTask(db, "ATM-cccccc"); err != nil {
		t.Fatal(err)
	} else if !ok {
		t.Fatal("task ATM-cccccc missing after re-projection")
	}
}

// TestCacheProjectFromV2StateDeletesStaleLabels mirrors
// TestCacheProjectFromV2StateDeletesStaleRows for labels: a label live in a
// first projected fold, then absent (not tombstoned) from a disjoint second
// fold — e.g. a re-upgrade that discards the branch that upserted it — must
// not survive re-projection. cacheDeleteProjectRows historically swept only
// comments/tasks/projects and left labels alone ("Labels stay"), relying on
// the Tombstoned branch in the fold loop to delete removed labels; that
// branch only ever visits names present in the CURRENT fold, so a label
// merely absent from the new fold was never visited and its row leaked
// forever. It also pins the scoping: a label outside the project's <CODE>:
// prefix must survive the sweep.
func TestCacheProjectFromV2StateDeletesStaleLabels(t *testing.T) {
	s := testStore(t)
	clock := eventsource.NewClock(func() int64 { return 1000 })
	replica := "r_0123456789abcdefghjkmnpqrs"

	project, err := eventsource.NewEvent(clock, replica, nil, eventsource.Draft{
		Actor:   "admin@cli:unset",
		Action:  "project.created",
		Subject: eventsource.Subject{Kind: "project", Code: "ATM"},
		Payload: map[string]any{"alias": "ATM", "name": "Agent Tasks Management"},
	})
	if err != nil {
		t.Fatal(err)
	}
	labelA, err := eventsource.NewEvent(clock, replica, []string{project.ID}, eventsource.Draft{
		Actor:   "admin@cli:unset",
		Action:  "label.upserted",
		Subject: eventsource.Subject{Kind: "label", Name: "ATM:status:open"},
		Payload: map[string]any{"description": "Open", "expr": ""},
	})
	if err != nil {
		t.Fatal(err)
	}

	state, err := eventsource.FoldEvents([]*eventsource.Event{project, labelA})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.cacheProjectFromV2State("ATM", state, 2); err != nil {
		t.Fatal(err)
	}

	db, err := s.cacheDB()
	if err != nil {
		t.Fatal(err)
	}
	// A label outside the project's <CODE>: prefix, inserted directly, must
	// survive the sweep — it belongs to no project's projection.
	if err := cacheUpsertLabel(db, Label{Name: "OTHER:status:open", Description: "unrelated", LogSeq: 1}); err != nil {
		t.Fatal(err)
	}

	if _, ok, err := cacheGetLabel(db, "ATM:status:open"); err != nil {
		t.Fatal(err)
	} else if !ok {
		t.Fatal("live label ATM:status:open missing after first projection")
	}

	// Simulate a re-upgrade that discards the branch above and folds a
	// brand-new event set: labelA's identity never appears in this fold at
	// all, so a projector that only deletes tombstoned-and-present labels
	// would never learn it should be gone.
	freshProject, err := eventsource.NewEvent(clock, replica, nil, eventsource.Draft{
		Actor:   "admin@cli:unset",
		Action:  "project.created",
		Subject: eventsource.Subject{Kind: "project", Code: "ATM"},
		Payload: map[string]any{"alias": "ATM", "name": "Agent Tasks Management"},
	})
	if err != nil {
		t.Fatal(err)
	}
	labelC, err := eventsource.NewEvent(clock, replica, []string{freshProject.ID}, eventsource.Draft{
		Actor:   "admin@cli:unset",
		Action:  "label.upserted",
		Subject: eventsource.Subject{Kind: "label", Name: "ATM:status:closed"},
		Payload: map[string]any{"description": "Closed", "expr": ""},
	})
	if err != nil {
		t.Fatal(err)
	}
	freshState, err := eventsource.FoldEvents([]*eventsource.Event{freshProject, labelC})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.cacheProjectFromV2State("ATM", freshState, 2); err != nil {
		t.Fatal(err)
	}

	if _, ok, err := cacheGetLabel(db, "ATM:status:open"); err != nil {
		t.Fatal(err)
	} else if ok {
		t.Fatal("stale label ATM:status:open from discarded fold survived re-projection")
	}
	if _, ok, err := cacheGetLabel(db, "ATM:status:closed"); err != nil {
		t.Fatal(err)
	} else if !ok {
		t.Fatal("label ATM:status:closed missing after re-projection")
	}
	if _, ok, err := cacheGetLabel(db, "OTHER:status:open"); err != nil {
		t.Fatal(err)
	} else if !ok {
		t.Fatal("label outside the project's prefix must survive the project-scoped sweep")
	}
}

// TestCacheProjectFromV2StateDropsDanglingReplyToForTombstonedParent proves
// commentAlias does not resolve a tombstoned comment's alias: a reply whose
// ReplyToRef points at a comment that was subsequently removed must project
// with an empty ReplyTo, not a dangling reference to a row that is never
// inserted (tombstoned comments are skipped during projection, but they
// remain present in eventsource.State by design — see fold.go's EntityMeta
// doc — so a lookup with no Tombstoned check still finds and resolves them).
func TestCacheProjectFromV2StateDropsDanglingReplyToForTombstonedParent(t *testing.T) {
	s := testStore(t)
	clock := eventsource.NewClock(func() int64 { return 1000 })
	replica := "r_0123456789abcdefghjkmnpqrs"

	project, err := eventsource.NewEvent(clock, replica, nil, eventsource.Draft{
		Actor:   "admin@cli:unset",
		Action:  "project.created",
		Subject: eventsource.Subject{Kind: "project", Code: "ATM"},
		Payload: map[string]any{"alias": "ATM", "name": "Agent Tasks Management"},
	})
	if err != nil {
		t.Fatal(err)
	}
	task, err := eventsource.NewEvent(clock, replica, []string{project.ID}, eventsource.Draft{
		Actor:   "admin@cli:unset",
		Action:  "task.created",
		Subject: eventsource.Subject{Kind: "task"},
		Payload: map[string]any{"alias": "ATM-abcdef", "title": "First", "description": ""},
	})
	if err != nil {
		t.Fatal(err)
	}
	parent, parentAlias, err := eventsource.NewCommentCreated(clock, replica, []string{task.ID}, eventsource.CommentCreateDraft{
		TaskAlias: "ATM-abcdef",
		TaskRef:   task.ID,
		Actor:     "admin@cli:unset",
		Body:      "Parent",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	_ = parentAlias
	removeParent, err := eventsource.NewEvent(clock, replica, []string{parent.ID}, eventsource.Draft{
		Actor:   "admin@cli:unset",
		Action:  "comment.removed",
		Subject: eventsource.Subject{Kind: "comment", ID: parent.ID},
	})
	if err != nil {
		t.Fatal(err)
	}
	reply, replyAlias, err := eventsource.NewCommentCreated(clock, replica, []string{removeParent.ID}, eventsource.CommentCreateDraft{
		TaskAlias:  "ATM-abcdef",
		TaskRef:    task.ID,
		ReplyToRef: parent.ID,
		Actor:      "admin@cli:unset",
		Body:       "Reply to a since-removed comment",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}

	state, err := eventsource.FoldEvents([]*eventsource.Event{project, task, parent, removeParent, reply})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.cacheProjectFromV2State("ATM", state, 5); err != nil {
		t.Fatal(err)
	}

	db, err := s.cacheDB()
	if err != nil {
		t.Fatal(err)
	}
	c, ok, err := cacheGetComment(db, replyAlias)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatalf("reply comment %s missing after projection", replyAlias)
	}
	if c.ReplyTo != "" {
		t.Fatalf("ReplyTo = %q, want empty: dangling reference to tombstoned parent %s", c.ReplyTo, parentAlias)
	}
}
