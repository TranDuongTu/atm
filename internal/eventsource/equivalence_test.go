// Package eventsource_test holds the equivalence capstone: it takes a real v1
// log.jsonl, upgrades it with UpgradeV1, folds it, and asserts the folded
// state matches an independent pure replay of the same log bytes (ReplayV1).
// This is the archaeological record that the v2 core is a faithful superset of
// v1 semantics. It once imported internal/store to drive the live v1 store as
// the oracle; that coupling is now severed — the oracle is eventsource's own
// ReplayV1, and the representative session is captured verbatim in the
// testdata/v1-log.jsonl fixture.
package eventsource_test

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"atm/internal/eventsource"
)

// TestFoldOfUpgradeMatchesReplay is the milestone's central claim: for a real
// v1 log, FoldEvents(UpgradeV1(log)) reproduces a pure ReplayV1(log) exactly on
// every field that survives into v2 — titles, descriptions, bodies, label
// membership, existence, aliases and cross-entity references. Timestamps and
// the retired NextTaskN/NextCommentN counters are out of scope by design.
//
// Known, intentional divergence (not exercised by this fixture, but preserved
// by CompareReplayToFold's computed-label exclusion): if a v1 ledger ever
// assigned a BOARD label (one carrying an expr) or a namespace label to a task,
// ReplayV1 lists it in task.Labels while the v2 fold correctly drops it (L2-6 —
// computed-label membership is derived, never asserted; the raw event is still
// preserved in the log). This fixture only ever assigns plain labels, so it
// never hits that case; CompareReplayToFold excludes computed membership on
// BOTH sides so the two agree regardless.
func TestFoldOfUpgradeMatchesReplay(t *testing.T) {
	logData, err := os.ReadFile(filepath.Join("testdata", "v1-log.jsonl"))
	if err != nil {
		t.Fatal(err)
	}

	// v1 truth — the independent oracle, a pure replay of the log bytes.
	replay, err := eventsource.ReplayV1(logData)
	if err != nil {
		t.Fatalf("ReplayV1: %v", err)
	}

	// v2: upgrade the raw log and fold.
	res, err := eventsource.UpgradeV1(logData)
	if err != nil {
		t.Fatalf("UpgradeV1: %v", err)
	}
	st, err := eventsource.FoldEvents(res.Events)
	if err != nil {
		t.Fatalf("FoldEvents: %v", err)
	}

	// Guard against a vacuous comparison: the oracle must be non-trivial. The
	// fixture creates two tasks (one later removed), two comments (one a reply),
	// exercises task.meta-changed (retired in v2), label add + remove, and a
	// task tombstone.
	if replay.Project == nil {
		t.Fatal("replay produced no project")
	}
	if len(replay.Tasks) != 1 || len(replay.Comments) != 2 {
		t.Fatalf("replay = %d tasks, %d comments; want 1, 2", len(replay.Tasks), len(replay.Comments))
	}
	sawReply := false
	for _, c := range replay.Comments {
		if c.ReplyTo != "" {
			sawReply = true
		}
	}
	if !sawReply {
		t.Fatal("fixture drove a reply but replay carries no reply_to — comparison would be vacuous")
	}

	// The central claim: the fold of the upgrade compares clean against the
	// pure v1 replay, computed-label divergence excluded on both sides.
	if err := eventsource.CompareReplayToFold(replay, st); err != nil {
		t.Fatalf("fold of upgrade diverges from v1 replay: %v", err)
	}

	// ---- Spot-checks against the ORACLE's own values, so a self-consistent
	// snapshot-vs-delta confusion in the upgrade cannot pass unnoticed.
	// ATM-0001 was born ATM:status:open, gained :done, lost :open.
	t1 := replayTask(t, replay, "ATM-0001")
	if !sameSet(t1.Labels, []string{"ATM:status:done"}) {
		t.Fatalf("oracle drifted: task ATM-0001 labels = %v, want [ATM:status:done]", sorted(t1.Labels))
	}

	// ---- The removed task (ATM-0002) is gone from the replay but survives
	// tombstoned in the v2 fold — findable for restore.
	if _, live := liveTask(st, "ATM-0002"); live {
		t.Error("removed task ATM-0002 should not be live in the fold")
	}
	if tk := st.Tasks[res.IdentityByAlias["ATM-0002"]]; tk == nil || !tk.Tombstoned {
		t.Errorf("removed task should remain tombstoned in state: %+v", tk)
	}

	// ---- A linear v1 history can never be contested.
	if len(st.Contested) != 0 {
		t.Errorf("contested on linear history: %+v", st.Contested)
	}

	// ---- Aliases resolve to the identity the upgrade minted.
	for _, alias := range []string{"ATM", "ATM-0001", "ATM-0002", "ATM-0001-c0001", "ATM-0001-c0002"} {
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

func replayTask(t *testing.T, rep *eventsource.ReplayResult, id string) *eventsource.ReplayTask {
	t.Helper()
	for _, tk := range rep.Tasks {
		if tk.ID == id {
			return tk
		}
	}
	t.Fatalf("task %s not in replay", id)
	return nil
}

// liveTask reports whether an alias resolves to a live (non-tombstoned) task in
// the fold.
func liveTask(st *eventsource.State, alias string) (*eventsource.TaskState, bool) {
	for _, tk := range st.Tasks {
		if tk.Alias == alias && !tk.Tombstoned {
			return tk, true
		}
	}
	return nil, false
}
