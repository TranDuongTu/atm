package eventsource

import (
	"encoding/json"
	"math/rand"
	"slices"
	"testing"
)

// buildScenario authors a two-replica history exercising every
// ordering-sensitive slot kind: creations, a genuine concurrent scalar
// edit race (contested LWW), a genuine concurrent membership add/remove
// race on the same label (OR-Set add-wins), a plain uncontested add,
// tombstone+causally-later-restore, label upserts (plain and computed),
// a genuine concurrent add/remove race on a COMPUTED label's membership
// slot (must stay inert — never surfaced as membership or Contested,
// L2-6), a membership write against a namespace label that was never
// upserted (exercises the no-LabelState fallback in `computed`), and an
// inert retired action riding through.
//
// Deviation from the brief: the brief's code labeled this block "Add/remove
// race on P:x" but wrote the add to a *different* label (P:y), so P:x's
// only writers were causally ordered (creation dominated by the later
// remove) — not a race at all, and the scenario never exercised the
// add-wins rule for a genuinely concurrent membership conflict. Fixed here
// so the comment's stated intent is what the code actually does. Also
// added: a concurrent race on a computed label's membership slot and a
// namespace-label reference, since the original set never exercised the
// L2-6 "computed membership is inert" skip at all.
func buildScenario(t *testing.T) []*Event {
	t.Helper()
	ca, cb := testClock(1000), testClock(2000)
	var evs []*Event
	add := func(e *Event) *Event { evs = append(evs, e); return e }

	proj := add(testEvent(t, ca, replicaA, nil, ActionProjectCreated,
		Subject{Kind: "project", Code: "P"}, map[string]any{"alias": "P", "name": "proj"}))
	task1 := add(testEvent(t, ca, replicaA, []string{proj.ID}, ActionTaskCreated,
		Subject{Kind: "task"}, map[string]any{"alias": "P-1", "title": "one", "labels": []string{"P:x"}}))
	task2 := add(testEvent(t, cb, replicaB, []string{proj.ID}, ActionTaskCreated,
		Subject{Kind: "task"}, map[string]any{"alias": "P-2", "title": "two"}))
	// Concurrent scalar edits (contested LWW).
	add(testEvent(t, ca, replicaA, []string{task1.ID}, ActionTaskTitleChanged,
		Subject{Kind: "task", ID: task1.ID}, map[string]any{"title": "one-A"}))
	add(testEvent(t, cb, replicaB, []string{task1.ID}, ActionTaskTitleChanged,
		Subject{Kind: "task", ID: task1.ID}, map[string]any{"title": "one-B"}))
	// A plain, uncontested add of a second label.
	add(testEvent(t, ca, replicaA, []string{task1.ID}, ActionTaskLabelAdded,
		Subject{Kind: "task", ID: task1.ID}, map[string]any{"label": "P:y"}))
	// Genuine add/remove race on P:x: both concurrent children of task1's
	// creation, so the creation's own add is dominated and add-wins (L2-2)
	// decides between these two.
	add(testEvent(t, cb, replicaB, []string{task1.ID}, ActionTaskLabelAdded,
		Subject{Kind: "task", ID: task1.ID}, map[string]any{"label": "P:x"}))
	add(testEvent(t, ca, replicaA, []string{task1.ID}, ActionTaskLabelRemoved,
		Subject{Kind: "task", ID: task1.ID}, map[string]any{"label": "P:x"}))
	// Tombstone + causally-later restore on task2.
	rm := add(testEvent(t, ca, replicaA, []string{task2.ID}, ActionTaskRemoved,
		Subject{Kind: "task", ID: task2.ID}, nil))
	add(testEvent(t, cb, replicaB, []string{rm.ID}, ActionTaskRestored,
		Subject{Kind: "task", ID: task2.ID}, nil))
	// Labels: plain + computed, plus a comment.
	up := add(testEvent(t, ca, replicaA, []string{proj.ID}, ActionLabelUpserted,
		Subject{Kind: "label", Name: "P:x"}, map[string]any{"description": "xx", "expr": ""}))
	add(testEvent(t, cb, replicaB, []string{up.ID}, ActionLabelUpserted,
		Subject{Kind: "label", Name: "P:board"}, map[string]any{"description": "", "expr": "x"}))
	// Genuine concurrent add/remove race on a COMPUTED label's membership
	// slot: two maximal writers, yet must never surface as membership or
	// as Contested (L2-6 — inert, skipped before the contested check).
	add(testEvent(t, ca, replicaA, []string{task2.ID}, ActionTaskLabelAdded,
		Subject{Kind: "task", ID: task2.ID}, map[string]any{"label": "P:board"}))
	add(testEvent(t, cb, replicaB, []string{task2.ID}, ActionTaskLabelRemoved,
		Subject{Kind: "task", ID: task2.ID}, map[string]any{"label": "P:board"}))
	// A namespace label (":*" suffix) is computed by name alone even with
	// no LabelState ever upserted for it — exercises the fallback branch
	// in `computed` that consults isNamespaceName when st.Labels[name] is nil.
	add(testEvent(t, ca, replicaA, []string{task2.ID}, ActionTaskLabelAdded,
		Subject{Kind: "task", ID: task2.ID}, map[string]any{"label": "P:ns:*"}))
	cm := add(testEvent(t, cb, replicaB, []string{task1.ID}, ActionCommentCreated,
		Subject{Kind: "comment"}, map[string]any{"alias": "P-1-c1", "task_ref": task1.ID, "body": "hi"}))
	add(testEvent(t, ca, replicaA, []string{cm.ID}, ActionCommentBodyChanged,
		Subject{Kind: "comment", ID: cm.ID}, map[string]any{"body": "edited"}))
	// Retired action riding through: inert but causal.
	add(testEvent(t, ca, replicaA, []string{task1.ID}, "task.meta-changed",
		Subject{Kind: "task", ID: task1.ID}, map[string]any{"next_comment_n": 5}))
	return evs
}

func stateFingerprint(t *testing.T, st *State) string {
	t.Helper()
	// State contains only exported fields and deterministic slices;
	// JSON is a convenient deep-equal witness.
	b, err := json.Marshal(st)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestFoldIsOrderIndependent(t *testing.T) {
	evs := buildScenario(t)
	base, err := FoldEvents(evs)
	if err != nil {
		t.Fatal(err)
	}
	want := stateFingerprint(t, base)

	// Sanity: the scenario actually stresses what it claims to. If any of
	// these fail, the scenario itself is thin/wrong, not the fold.
	if len(base.Contested) < 2 {
		t.Fatalf("scenario should produce >= 2 contested slots (title LWW, P:x membership race), got %d: %+v",
			len(base.Contested), base.Contested)
	}
	for _, cs := range base.Contested {
		if cs.Field == "P:board" {
			t.Fatalf("computed label membership must never be reported as Contested (L2-6), got %+v", cs)
		}
	}
	task2 := base.Tasks[func() string {
		for id, tk := range base.Tasks {
			if tk.Alias == "P-2" {
				return id
			}
		}
		t.Fatal("task P-2 not found")
		return ""
	}()]
	if slices.Contains(task2.Labels, "P:board") || slices.Contains(task2.Labels, "P:ns:*") {
		t.Fatalf("computed/namespace labels must never surface as membership (L2-6), task2.Labels = %v", task2.Labels)
	}

	rng := rand.New(rand.NewSource(1))
	for i := 0; i < 50; i++ {
		shuffled := make([]*Event, len(evs))
		copy(shuffled, evs)
		rng.Shuffle(len(shuffled), func(a, b int) { shuffled[a], shuffled[b] = shuffled[b], shuffled[a] })
		// Inject duplicates: syncing the same event twice must be a no-op.
		shuffled = append(shuffled, shuffled[rng.Intn(len(shuffled))], shuffled[rng.Intn(len(shuffled))])
		st, err := FoldEvents(shuffled)
		if err != nil {
			t.Fatal(err)
		}
		if got := stateFingerprint(t, st); got != want {
			t.Fatalf("permutation %d diverged:\n got %s\nwant %s", i, got, want)
		}
	}
}
