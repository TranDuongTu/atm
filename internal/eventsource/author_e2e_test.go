package eventsource

import (
	"strings"
	"testing"
)

// TestNewTaskCreatedAliasIsPreImagePrefix is the ATM-0125 capstone: it
// authors a task.created event end-to-end (mint → author → fold) and
// asserts the alias/identity relationship that the amended L1 rule
// guarantees. Two claims, both load-bearing:
//
//  1. The alias IS a prefix of the PRE-ALIAS draft's SHA-256 id — the
//     minting rule holds, and a future reader can confirm the alias was
//     honestly derived (from the pre-image, not invented).
//  2. The alias is NOT a prefix of the FINAL event's id — the dropped
//     invariant. Writing the alias into payload.alias changes the
//     payload, which changes the digest, so the final identity cannot
//     share the prefix. Anything that assumed "alias == id[:prefix]" would
//     silently break here.
func TestNewTaskCreatedAliasIsPreImagePrefix(t *testing.T) {
	clock := fixedClock()
	const replica = "r_00000000000000000000000001"
	const code = "ATM"

	// Author through the helper first to obtain the hlc it selected.
	ev, alias, err := NewTaskCreated(clock, replica, nil, TaskCreateDraft{
		ProjectCode: code,
		At:          testAt,
		Actor:       "developer@claude:test",
		Title:       "Fix the cache",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}

	// Re-assemble the pre-alias draft with the helper's hlc; the only
	// difference from the final event is the absence of payload.alias.
	preAlias, err := assembleEvent([]string{}, ev.HLC, replica, testAt,
		"developer@claude:test", ActionTaskCreated, Subject{Kind: "task"},
		map[string]any{"title": "Fix the cache"})
	if err != nil {
		t.Fatal(err)
	}

	// Claim 1: the alias hex is a prefix of the pre-alias draft's id.
	preHex := strings.TrimPrefix(preAlias.ID, "sha256:")
	if !strings.HasPrefix(preHex, strings.TrimPrefix(alias, code+"-")) {
		t.Errorf("alias %q is NOT a prefix of the pre-alias digest %q (minting rule violated)", alias, preAlias.ID)
	}

	// Claim 2: the alias hex is NOT a prefix of the final event's id.
	// This is the dropped invariant — it must not hold.
	finalHex := strings.TrimPrefix(ev.ID, "sha256:")
	aliasHex := strings.TrimPrefix(alias, code+"-")
	if strings.HasPrefix(finalHex, aliasHex) {
		t.Errorf("alias %q IS a prefix of the final id %q — the dropped invariant holds, which is a bug (writing the alias into the payload should have changed the digest)", alias, ev.ID)
	}

	// The final event must differ from the pre-alias draft only by the
	// added payload.alias — sanity check that the two phases share the
	// same envelope except for that one key.
	if ev.HLC != preAlias.HLC || ev.Replica != preAlias.Replica || ev.Action != preAlias.Action {
		t.Errorf("pre-alias draft and final event diverge on envelope fields")
	}
	if got, _ := ev.PayloadString("alias"); got != alias {
		t.Errorf("payload.alias = %q, want %q", got, alias)
	}
	if got, _ := ev.PayloadString("title"); got != "Fix the cache" {
		t.Errorf("payload.title = %q", got)
	}
}

// TestNewTaskCreatedExtendsOnCollision exercises the local disambiguation
// path: a held alias colliding with the minted prefix must extend to the
// shortest unambiguous length.
func TestNewTaskCreatedExtendsOnCollision(t *testing.T) {
	clock := fixedClock()
	const replica = "r_00000000000000000000000001"

	// First author normally to learn what prefix it mints.
	ev1, alias1, err := NewTaskCreated(clock, replica, nil, TaskCreateDraft{
		ProjectCode: "ATM",
		At:          testAt,
		Actor:       "a",
		Title:       "first",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}

	// Re-author with the same inputs and a `taken` that claims alias1 is
	// held: the helper must extend past alias1's length.
	clock2 := fixedClock()
	ev2, alias2, err := NewTaskCreated(clock2, replica, nil, TaskCreateDraft{
		ProjectCode: "ATM",
		At:          testAt,
		Actor:       "a",
		Title:       "first",
	}, func(a string) bool { return a == alias1 })
	if err != nil {
		t.Fatal(err)
	}
	if alias2 == alias1 {
		t.Fatalf("collision not extended: alias2 = %q == alias1", alias2)
	}
	// Same inputs (apart from the taken callback) produce the same
	// pre-image digest, so alias2 must share alias1's prefix and extend it.
	if !strings.HasPrefix(alias2, alias1) {
		t.Errorf("extended alias %q does not share prefix %q", alias2, alias1)
	}
	// Distinct aliases must yield distinct identities.
	if ev1.ID == ev2.ID {
		t.Errorf("two tasks authored with distinct aliases share an identity")
	}
}

// TestNewTaskCreatedFoldsBack exercises the end-to-end path the task
// description calls out: mint, author, fold, and resolve — the exact gap
// that hid the fixed-point bug. The folded state must surface the minted
// alias and round-trip through Resolve.
func TestNewTaskCreatedFoldsBack(t *testing.T) {
	clock := fixedClock()
	const replica = "r_00000000000000000000000001"

	ev, alias, err := NewTaskCreated(clock, replica, nil, TaskCreateDraft{
		ProjectCode: "ATM",
		At:          testAt,
		Actor:       "developer@claude:test",
		Title:       "Fix the cache",
		Description: "the cache is broken",
		Labels:      []string{"ATM:status:open"},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}

	st, err := FoldEvents([]*Event{ev})
	if err != nil {
		t.Fatal(err)
	}
	tk := st.Tasks[ev.ID]
	if tk == nil {
		t.Fatalf("task %s missing from fold", ev.ID)
	}
	if tk.Alias != alias {
		t.Errorf("folded alias = %q, want %q", tk.Alias, alias)
	}
	if tk.Title != "Fix the cache" {
		t.Errorf("folded title = %q", tk.Title)
	}
	if tk.Description != "the cache is broken" {
		t.Errorf("folded description = %q", tk.Description)
	}
	if len(tk.Labels) != 1 || tk.Labels[0] != "ATM:status:open" {
		t.Errorf("folded labels = %v", tk.Labels)
	}

	// Resolve by the minted alias must find this task.
	m, err := st.Resolve(alias)
	if err != nil || m.ID != ev.ID || m.Kind != "task" {
		t.Errorf("Resolve(%q) = %+v, %v", alias, m, err)
	}
	// Resolve by the final identity prefix must also find it — the
	// identity-prefix path is independent of the alias (the dropped
	// invariant would have conflated them).
	hex := strings.TrimPrefix(ev.ID, "sha256:")
	m2, err := st.Resolve(hex[:12])
	if err != nil || m2.ID != ev.ID {
		t.Errorf("Resolve(identity prefix) = %+v, %v", m2, err)
	}
}

// TestNewCommentCreatedAliasIsPreImagePrefix mirrors the task capstone for
// comments: mint → author → fold, and the alias is a prefix of the
// pre-alias digest, not of the final id. The comment references its task by
// identity; the fold must preserve both the alias and the cross-entity
// reference.
func TestNewCommentCreatedAliasIsPreImagePrefix(t *testing.T) {
	clock := fixedClock()
	const replica = "r_00000000000000000000000001"

	// Author the parent task first, so the comment has a real task_ref.
	taskEv, taskAlias, err := NewTaskCreated(clock, replica, nil, TaskCreateDraft{
		ProjectCode: "ATM",
		At:          testAt,
		Actor:       "developer@claude:test",
		Title:       "parent task",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}

	// Author the comment through the helper to obtain the hlc it selected.
	ev, alias, err := NewCommentCreated(clock, replica, []string{taskEv.ID}, CommentCreateDraft{
		TaskAlias: taskAlias,
		TaskRef:   taskEv.ID,
		At:        testAt,
		Actor:     "developer@claude:test",
		Body:      "a comment",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}

	// Re-assemble the pre-alias draft with the helper's hlc; the only
	// difference from the final event is the absence of payload.alias.
	preAlias, err := assembleEvent([]string{taskEv.ID}, ev.HLC, replica, testAt,
		"developer@claude:test", ActionCommentCreated, Subject{Kind: "comment"},
		map[string]any{"task_ref": taskEv.ID, "body": "a comment"})
	if err != nil {
		t.Fatal(err)
	}

	// The alias is a prefix of the pre-alias digest, not the final id.
	preHex := strings.TrimPrefix(preAlias.ID, "sha256:")
	aliasHex := strings.TrimPrefix(alias, taskAlias+"-c")
	if !strings.HasPrefix(preHex, aliasHex) {
		t.Errorf("comment alias %q is NOT a prefix of pre-alias digest %q", alias, preAlias.ID)
	}
	finalHex := strings.TrimPrefix(ev.ID, "sha256:")
	if strings.HasPrefix(finalHex, aliasHex) {
		t.Errorf("comment alias %q IS a prefix of final id %q — dropped invariant holds, bug", alias, ev.ID)
	}

	// The fold surfaces the alias, the task_ref, and the body.
	st, err := FoldEvents([]*Event{taskEv, ev})
	if err != nil {
		t.Fatal(err)
	}
	cm := st.Comments[ev.ID]
	if cm == nil {
		t.Fatalf("comment %s missing from fold", ev.ID)
	}
	if cm.Alias != alias {
		t.Errorf("folded comment alias = %q, want %q", cm.Alias, alias)
	}
	if cm.TaskRef != taskEv.ID {
		t.Errorf("folded task_ref = %q, want %q", cm.TaskRef, taskEv.ID)
	}
	if cm.Body != "a comment" {
		t.Errorf("folded body = %q", cm.Body)
	}
	// Resolve by the comment alias finds the comment.
	m, err := st.Resolve(alias)
	if err != nil || m.ID != ev.ID || m.Kind != "comment" {
		t.Errorf("Resolve(%q) = %+v, %v", alias, m, err)
	}
}

// TestNewCommentCreatedWithReplyAndLabels exercises the optional fields:
// a reply-to reference and creation-time labels both fold through.
func TestNewCommentCreatedWithReplyAndLabels(t *testing.T) {
	clock := fixedClock()
	const replica = "r_00000000000000000000000001"

	taskEv, taskAlias, err := NewTaskCreated(clock, replica, nil, TaskCreateDraft{
		ProjectCode: "ATM", At: testAt, Actor: "a", Title: "parent",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	parent, _, err := NewCommentCreated(clock, replica, []string{taskEv.ID}, CommentCreateDraft{
		TaskAlias: taskAlias, TaskRef: taskEv.ID, At: testAt, Actor: "a", Body: "parent",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	reply, replyAlias, err := NewCommentCreated(clock, replica, []string{parent.ID}, CommentCreateDraft{
		TaskAlias:  taskAlias,
		TaskRef:    taskEv.ID,
		ReplyToRef: parent.ID,
		At:         testAt,
		Actor:      "a",
		Body:       "reply",
		Labels:     []string{"ATM:comment:progress"},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	st, err := FoldEvents([]*Event{taskEv, parent, reply})
	if err != nil {
		t.Fatal(err)
	}
	cm := st.Comments[reply.ID]
	if cm == nil {
		t.Fatalf("reply missing from fold")
	}
	if cm.Alias != replyAlias {
		t.Errorf("alias = %q, want %q", cm.Alias, replyAlias)
	}
	if cm.ReplyToRef != parent.ID {
		t.Errorf("reply_to_ref = %q, want %q", cm.ReplyToRef, parent.ID)
	}
	if len(cm.Labels) != 1 || cm.Labels[0] != "ATM:comment:progress" {
		t.Errorf("labels = %v", cm.Labels)
	}
}

// TestNewProjectCreatedAliasesByCode confirms the project helper: the
// alias is the code (not hash-derived), and the fold surfaces it.
func TestNewProjectCreatedAliasesByCode(t *testing.T) {
	clock := fixedClock()
	const replica = "r_00000000000000000000000001"
	ev, alias, err := NewProjectCreated(clock, replica, nil, ProjectCreateDraft{
		Code: "ATM", Name: "Agent Tasks Management", At: testAt, Actor: "a",
	})
	if err != nil {
		t.Fatal(err)
	}
	if alias != "ATM" {
		t.Errorf("alias = %q, want ATM", alias)
	}
	st, err := FoldEvents([]*Event{ev})
	if err != nil {
		t.Fatal(err)
	}
	p := st.Projects[ev.ID]
	if p == nil {
		t.Fatalf("project missing from fold")
	}
	if p.Alias != "ATM" || p.Code != "ATM" {
		t.Errorf("folded alias/code = %q/%q", p.Alias, p.Code)
	}
	if p.Name != "Agent Tasks Management" {
		t.Errorf("folded name = %q", p.Name)
	}
}

// TestNewTaskCreatedRejectsReservedReplica guards the helper's replica
// validation (mirrors NewEvent's existing guard).
func TestNewTaskCreatedRejectsReservedReplica(t *testing.T) {
	for _, replica := range []string{"", ReplicaV1} {
		_, _, err := NewTaskCreated(fixedClock(), replica, nil, TaskCreateDraft{
			ProjectCode: "ATM", At: testAt, Actor: "a", Title: "t",
		}, nil)
		if err == nil {
			t.Errorf("replica %q accepted", replica)
		}
	}
}

// TestNewTaskCreatedDeterministicForSameInputs guards the determinism
// property inherited from NewEvent: same clock state, same replica, same
// draft, same parents produce the same event bytes and identity. The
// pre-image hash is a pure function of the draft, so two calls with
// identical inputs mint the same alias and author the same final event.
func TestNewTaskCreatedDeterministicForSameInputs(t *testing.T) {
	mk := func() (*Event, string) {
		ev, alias, err := NewTaskCreated(fixedClock(), "r_00000000000000000000000001", nil, TaskCreateDraft{
			ProjectCode: "ATM", At: testAt, Actor: "a", Title: "t",
		}, nil)
		if err != nil {
			t.Fatal(err)
		}
		return ev, alias
	}
	a, aAlias := mk()
	b, bAlias := mk()
	if a.ID != b.ID || aAlias != bAlias || string(a.Raw) != string(b.Raw) {
		t.Errorf("same inputs, different events:\nalias %q vs %q\n%s\n%s", aAlias, bAlias, a.Raw, b.Raw)
	}
}

// TestNewTaskCreatedClockTicksOnce guards that the helper consumes exactly
// one HLC tick (one for the pre-alias draft, reused for the final event),
// so a caller sequencing multiple creations through the same clock sees
// strictly increasing stamps.
func TestNewTaskCreatedClockTicksOnce(t *testing.T) {
	clock := fixedClock()
	const replica = "r_00000000000000000000000001"
	e1, _, err := NewTaskCreated(clock, replica, nil, TaskCreateDraft{
		ProjectCode: "ATM", At: testAt, Actor: "a", Title: "first",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	e2, _, err := NewTaskCreated(clock, replica, []string{e1.ID}, TaskCreateDraft{
		ProjectCode: "ATM", At: testAt, Actor: "a", Title: "second",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if CompareEvents(e1, e2) >= 0 {
		t.Errorf("second task does not sort after first: %v vs %v", e1.HLC, e2.HLC)
	}
}
