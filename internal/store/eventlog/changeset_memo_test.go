package eventlog

import (
	"testing"

	"atm/internal/core"
)

// TestChangeSetMemoizesFold pins the ATM-40faff contract: within one
// changeSet, operations reuse the first beginAuthorLocked fold; any
// successful append invalidates the memo; the operation after an append
// observes the appended state through a fresh fold.
func TestChangeSetMemoizesFold(t *testing.T) {
	e := testEngine(t)
	if err := e.WithProjectBirth("ATM", func() error { return nil }, func(cs core.ChangeSet) error {
		return cs.CreateProject("Acme Task Manager", "developer@claude:test")
	}); err != nil {
		t.Fatalf("WithProjectBirth: %v", err)
	}
	if err := e.WithProjectWrite("ATM", func(cs core.ChangeSet) error {
		return cs.SeedLabel("ATM:open-tasks", "open work", "status:open", "developer@claude:test")
	}); err != nil {
		t.Fatalf("seed txn: %v", err)
	}

	// Five no-op seeds in ONE transaction fold exactly once.
	before := e.beginFolds.Load()
	if err := e.WithProjectWrite("ATM", func(cs core.ChangeSet) error {
		for i := 0; i < 5; i++ {
			if err := cs.SeedLabel("ATM:open-tasks", "open work", "", "developer@claude:test"); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("no-op txn: %v", err)
	}
	if got := e.beginFolds.Load() - before; got != 1 {
		t.Errorf("5 no-op SeedLabels folded %d times, want 1", got)
	}

	// An append invalidates the memo: a re-seed of the JUST-appended label
	// must see it live (fresh fold) and append nothing — the event count
	// grows by exactly 1 across the transaction.
	countBefore, err := e.ChangeCount("ATM")
	if err != nil {
		t.Fatalf("ChangeCount: %v", err)
	}
	if err := e.WithProjectWrite("ATM", func(cs core.ChangeSet) error {
		if err := cs.SeedLabel("ATM:fresh-board", "fresh", "status:open", "developer@claude:test"); err != nil {
			return err
		}
		if err := cs.SeedLabel("ATM:fresh-board", "fresh", "", "developer@claude:test"); err != nil {
			return err
		}
		if !cs.Dirty() {
			t.Error("dirty seed did not mark the transaction dirty")
		}
		return nil
	}); err != nil {
		t.Fatalf("dirty txn: %v", err)
	}
	countAfter, err := e.ChangeCount("ATM")
	if err != nil {
		t.Fatalf("ChangeCount: %v", err)
	}
	if countAfter-countBefore != 1 {
		t.Errorf("dirty seed + no-op re-seed appended %d events, want 1 (stale memo would double-append)", countAfter-countBefore)
	}
}