// Package eventsource_test holds the equivalence capstone: it drives the
// REAL v1 store through a representative session, upgrades the resulting
// log.jsonl with UpgradeV1, folds it, and asserts the folded state matches
// store.Replay. This is the proof that the v2 core is a faithful superset
// of v1 semantics — and the only file allowed to import both packages.
package eventsource_test

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"atm/internal/eventsource"
	"atm/internal/store"
)

const actor = "admin@cli:test"

// TestFoldOfUpgradeMatchesReplay is the milestone's central claim: for a real
// v1 log, FoldEvents(UpgradeV1(log)) reproduces store.Replay exactly on every
// field that survives into v2 — titles, descriptions, bodies, label membership,
// existence, aliases and cross-entity references. Timestamps and the retired
// NextTaskN/NextCommentN counters are out of scope by design. Known, intentional
// divergence not exercised by this scenario: if a v1 ledger ever assigned a
// BOARD label (one carrying an expr) to a task, store.Replay lists it in
// task.Labels while the v2 fold correctly drops it (L2-6 — computed-label
// membership is derived, never asserted; the raw event is still preserved in
// the log). This scenario only ever assigns plain labels, so it never hits that case.
func TestFoldOfUpgradeMatchesReplay(t *testing.T) {
	root := t.TempDir()
	s, err := store.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Init(""); err != nil {
		t.Fatal(err)
	}

	// A representative session: every mutation kind the store exposes that
	// writes a v2-carried slot — project create + rename, task create /
	// retitle / redescribe, comment create / reply / edit, task AND comment
	// label add + remove, label upsert (create then update), label remove,
	// board (expr) upsert, and task + comment removal.
	if _, err := s.CreateProject("ATM", "Agent Tasks Management", actor); err != nil {
		t.Fatal(err)
	}
	if err := s.SetProjectName("ATM", "Agent Task Management", actor); err != nil {
		t.Fatal(err)
	}
	t1, err := s.CreateTask("ATM", "First task", "with description", []string{"ATM:status:open"}, actor)
	if err != nil {
		t.Fatal(err)
	}
	t2, err := s.CreateTask("ATM", "Second task", "", nil, actor)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetTitle(t1.ID, "First task, retitled", actor); err != nil {
		t.Fatal(err)
	}
	if err := s.SetDescription(t2.ID, "added later", actor); err != nil {
		t.Fatal(err)
	}
	c1, err := s.CreateComment(t1.ID, "a comment", []string{"ATM:comment:progress"}, "", actor)
	if err != nil {
		t.Fatal(err)
	}
	c2, err := s.CreateComment(t1.ID, "a reply", nil, c1.ID, actor)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetCommentBody(c2.ID, "a reply, edited", actor); err != nil {
		t.Fatal(err)
	}
	// Comment membership: add one label, then remove the one it was born with.
	// The v1 *.label-removed payload carries a snapshot of the REMAINING
	// labels; only the fold of the synthesized delta can reproduce this.
	if err := s.CommentLabelAdd(c1.ID, "ATM:comment:decision", actor); err != nil {
		t.Fatal(err)
	}
	if err := s.CommentLabelRemove(c1.ID, "ATM:comment:progress", actor); err != nil {
		t.Fatal(err)
	}
	// Task membership: add + remove.
	if err := s.TaskLabelAdd(t1.ID, "ATM:status:done", actor); err != nil {
		t.Fatal(err)
	}
	if err := s.TaskLabelRemove(t1.ID, "ATM:status:open", actor); err != nil {
		t.Fatal(err)
	}
	// Label upsert: create, then update the description (a second upsert).
	if err := s.LabelAdd("ATM:refactor", "cleanup work", "", actor); err != nil {
		t.Fatal(err)
	}
	if err := s.LabelAdd("ATM:refactor", "cleanup work, revised", "", actor); err != nil {
		t.Fatal(err)
	}
	if err := s.TaskLabelAdd(t2.ID, "ATM:refactor", actor); err != nil {
		t.Fatal(err)
	}
	// A board: a label whose Expr is set. Exercises the label.expr scalar slot.
	if err := s.LabelAdd("ATM:next-sprint", "the sprint board", "status:done OR refactor", actor); err != nil {
		t.Fatal(err)
	}
	// A label that is created and then removed: gone from Replay, tombstoned
	// (but still present) in the fold.
	if err := s.LabelAdd("ATM:doomed-label", "will be removed", "", actor); err != nil {
		t.Fatal(err)
	}
	if _, err := s.LabelRemove("ATM:doomed-label", actor); err != nil {
		t.Fatal(err)
	}
	t3, err := s.CreateTask("ATM", "Doomed task", "", nil, actor)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RemoveTask(t3.ID, actor); err != nil {
		t.Fatal(err)
	}
	c3, err := s.CreateComment(t2.ID, "doomed comment", nil, "", actor)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RemoveComment(c3.ID, actor); err != nil {
		t.Fatal(err)
	}

	// v1 truth — the independent oracle.
	replay, err := s.Replay("ATM")
	if err != nil {
		t.Fatal(err)
	}

	// v2: upgrade the raw log and fold.
	logData, err := os.ReadFile(filepath.Join(root, "projects", "ATM", "log.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	res, err := eventsource.UpgradeV1(logData)
	if err != nil {
		t.Fatal(err)
	}
	st, err := eventsource.FoldEvents(res.Events)
	if err != nil {
		t.Fatal(err)
	}

	// Guard against a vacuous comparison: the oracle must be non-trivial.
	if replay.Project == nil {
		t.Fatal("replay produced no project")
	}
	if len(replay.Tasks) != 2 || len(replay.Comments) != 2 {
		t.Fatalf("replay = %d tasks, %d comments; want 2, 2", len(replay.Tasks), len(replay.Comments))
	}

	// ---- Project: existence, alias/code, name, created_by.
	proj := st.Projects[res.IdentityByAlias["ATM"]]
	if proj == nil {
		t.Fatalf("project %q missing from fold", "ATM")
	}
	if proj.Tombstoned {
		t.Errorf("project tombstoned in fold, live in replay")
	}
	if proj.Code != replay.Project.Code {
		t.Errorf("project code: got %q, want %q", proj.Code, replay.Project.Code)
	}
	if proj.Name != replay.Project.Name {
		t.Errorf("project name: got %q, want %q", proj.Name, replay.Project.Name)
	}
	if proj.CreatedBy != replay.Project.CreatedBy {
		t.Errorf("project created_by: got %q, want %q", proj.CreatedBy, replay.Project.CreatedBy)
	}

	// ---- Tasks: v1 Replay drops removed tasks; v2 keeps them tombstoned.
	liveTasks := map[string]*eventsource.TaskState{}
	for _, tk := range st.Tasks {
		if !tk.Tombstoned {
			liveTasks[tk.Alias] = tk
		}
	}
	if len(liveTasks) != len(replay.Tasks) {
		t.Fatalf("live tasks = %d, replay = %d", len(liveTasks), len(replay.Tasks))
	}
	for _, want := range replay.Tasks {
		got := liveTasks[want.ID]
		if got == nil {
			t.Fatalf("task %s missing from fold", want.ID)
		}
		if got.Title != want.Title {
			t.Errorf("task %s title: got %q, want %q", want.ID, got.Title, want.Title)
		}
		if got.Description != want.Description {
			t.Errorf("task %s description: got %q, want %q", want.ID, got.Description, want.Description)
		}
		if !sameSet(got.Labels, want.Labels) {
			t.Errorf("task %s labels: got %v, want %v", want.ID, sorted(got.Labels), sorted(want.Labels))
		}
		if got.CreatedBy != want.CreatedBy {
			t.Errorf("task %s created_by: got %q, want %q", want.ID, got.CreatedBy, want.CreatedBy)
		}
		if got.ID != res.IdentityByAlias[want.ID] {
			t.Errorf("task %s identity: got %q, want %q", want.ID, got.ID, res.IdentityByAlias[want.ID])
		}
	}
	// The removed task stays in v2 state, tombstoned — findable for restore.
	if tk := st.Tasks[res.IdentityByAlias[t3.ID]]; tk == nil || !tk.Tombstoned {
		t.Errorf("removed task should remain tombstoned in state: %+v", tk)
	}

	// ---- Comments: bodies, labels, task/reply refs, created_by, alias.
	liveComments := map[string]*eventsource.CommentState{}
	for _, cm := range st.Comments {
		if !cm.Tombstoned {
			liveComments[cm.Alias] = cm
		}
	}
	if len(liveComments) != len(replay.Comments) {
		t.Fatalf("live comments = %d, replay = %d", len(liveComments), len(replay.Comments))
	}
	sawReply := false
	for _, want := range replay.Comments {
		got := liveComments[want.ID]
		if got == nil {
			t.Fatalf("comment %s missing from fold", want.ID)
		}
		if got.Body != want.Body {
			t.Errorf("comment %s body: got %q, want %q", want.ID, got.Body, want.Body)
		}
		if got.TaskRef != res.IdentityByAlias[want.TaskID] {
			t.Errorf("comment %s task ref: got %q, want %q (task %s)", want.ID, got.TaskRef, res.IdentityByAlias[want.TaskID], want.TaskID)
		}
		if want.ReplyTo == "" {
			if got.ReplyToRef != "" {
				t.Errorf("comment %s reply ref: got %q, want empty", want.ID, got.ReplyToRef)
			}
		} else {
			sawReply = true
			if got.ReplyToRef != res.IdentityByAlias[want.ReplyTo] {
				t.Errorf("comment %s reply ref: got %q, want %q (reply to %s)", want.ID, got.ReplyToRef, res.IdentityByAlias[want.ReplyTo], want.ReplyTo)
			}
		}
		if !sameSet(got.Labels, want.Labels) {
			t.Errorf("comment %s labels: got %v, want %v", want.ID, sorted(got.Labels), sorted(want.Labels))
		}
		if got.CreatedBy != want.CreatedBy {
			t.Errorf("comment %s created_by: got %q, want %q", want.ID, got.CreatedBy, want.CreatedBy)
		}
		if got.ID != res.IdentityByAlias[want.ID] {
			t.Errorf("comment %s identity: got %q, want %q", want.ID, got.ID, res.IdentityByAlias[want.ID])
		}
	}
	if !sawReply {
		t.Error("history drove a reply but replay carries no reply_to — comparison would be vacuous")
	}
	// The removed comment stays in v2 state, tombstoned.
	if cm := st.Comments[res.IdentityByAlias[c3.ID]]; cm == nil || !cm.Tombstoned {
		t.Errorf("removed comment should remain tombstoned in state: %+v", cm)
	}

	// ---- Membership spot-check against the ORACLE's own values, so a
	// snapshot-vs-delta confusion in the upgrade cannot agree with itself.
	// t1 was born ATM:status:open, gained :done, lost :open. c1 was born
	// ATM:comment:progress, gained :decision, lost :progress.
	if want := replayTask(t, replay, t1.ID); !sameSet(want.Labels, []string{"ATM:status:done"}) {
		t.Fatalf("oracle drifted: task %s labels = %v, want [ATM:status:done]", t1.ID, sorted(want.Labels))
	}
	if want := replayComment(t, replay, c1.ID); !sameSet(want.Labels, []string{"ATM:comment:decision"}) {
		t.Fatalf("oracle drifted: comment %s labels = %v, want [ATM:comment:decision]", c1.ID, sorted(want.Labels))
	}

	// ---- Labels: the live set must match exactly, both directions, with
	// equal description and expr on every one.
	liveLabels := map[string]*eventsource.LabelState{}
	for name, l := range st.Labels {
		if !l.Tombstoned {
			liveLabels[name] = l
		}
	}
	if len(liveLabels) != len(replay.Labels) {
		t.Errorf("live labels = %d, replay = %d (fold %v, replay %v)",
			len(liveLabels), len(replay.Labels), sortedKeys(liveLabels), labelNames(replay.Labels))
	}
	for _, want := range replay.Labels {
		got := liveLabels[want.Name]
		if got == nil {
			t.Errorf("label %s missing or tombstoned in fold", want.Name)
			continue
		}
		if got.Description != want.Description {
			t.Errorf("label %s description: got %q, want %q", want.Name, got.Description, want.Description)
		}
		if got.Expr != want.Expr {
			t.Errorf("label %s expr: got %q, want %q", want.Name, got.Expr, want.Expr)
		}
	}
	inReplay := map[string]bool{}
	for _, l := range replay.Labels {
		inReplay[l.Name] = true
	}
	for name := range liveLabels {
		if !inReplay[name] {
			t.Errorf("label %s live in fold but absent from replay", name)
		}
	}
	// The removed label survives, tombstoned — and it is genuinely gone from v1.
	if l := st.Labels["ATM:doomed-label"]; l == nil || !l.Tombstoned {
		t.Errorf("removed label should remain tombstoned in state: %+v", l)
	}
	if inReplay["ATM:doomed-label"] {
		t.Fatal("oracle drifted: removed label still in replay")
	}
	// The board's expr must have survived the round trip.
	if l := liveLabels["ATM:next-sprint"]; l == nil || l.Expr == "" || !l.IsComputed() {
		t.Errorf("board ATM:next-sprint lost its expr in the fold: %+v", l)
	}

	// ---- A linear v1 history can never be contested.
	if len(st.Contested) != 0 {
		t.Errorf("contested on linear history: %+v", st.Contested)
	}

	// ---- Aliases resolve to the identity the upgrade minted.
	for _, alias := range []string{"ATM", t1.ID, t2.ID, t3.ID, c1.ID, c2.ID, c3.ID} {
		m, err := st.Resolve(alias)
		if err != nil {
			t.Errorf("Resolve(%q): %v", alias, err)
			continue
		}
		if m.ID != res.IdentityByAlias[alias] {
			t.Errorf("Resolve(%q) = %q, want %q", alias, m.ID, res.IdentityByAlias[alias])
		}
	}
}

func sorted(in []string) []string {
	out := slices.Clone(in)
	slices.Sort(out)
	return out
}

func sameSet(a, b []string) bool { return slices.Equal(sorted(a), sorted(b)) }

func sortedKeys(m map[string]*eventsource.LabelState) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

func labelNames(ls []store.Label) []string {
	out := make([]string, 0, len(ls))
	for _, l := range ls {
		out = append(out, l.Name)
	}
	return sorted(out)
}

func replayTask(t *testing.T, st *store.ReplayState, id string) *store.Task {
	t.Helper()
	for _, tk := range st.Tasks {
		if tk.ID == id {
			return tk
		}
	}
	t.Fatalf("task %s not in replay", id)
	return nil
}

func replayComment(t *testing.T, st *store.ReplayState, id string) *store.Comment {
	t.Helper()
	for _, c := range st.Comments {
		if c.ID == id {
			return c
		}
	}
	t.Fatalf("comment %s not in replay", id)
	return nil
}
