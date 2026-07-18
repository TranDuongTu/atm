package eventlog

import (
	"testing"

	"atm/internal/core"
)

// TestChangeSetDirty pins the Dirty contract the facade's reprojection gate
// relies on: a transaction is clean until an event is actually appended;
// idempotent no-ops (SeedLabel on a live label, EnsureLabels with only live
// names) leave it clean.
func TestChangeSetDirty(t *testing.T) {
	e := testEngine(t)
	if err := e.WithProjectBirth("ATM", func() error { return nil }, func(cs core.ChangeSet) error {
		if cs.Dirty() {
			t.Error("fresh birth changeSet reports dirty before any append")
		}
		if err := cs.CreateProject("Acme Task Manager", "developer@claude:test"); err != nil {
			return err
		}
		if !cs.Dirty() {
			t.Error("CreateProject appended the root event but Dirty() is false")
		}
		return nil
	}); err != nil {
		t.Fatalf("WithProjectBirth: %v", err)
	}

	// Seed a label so the no-op paths below have a live target.
	if err := e.WithProjectWrite("ATM", func(cs core.ChangeSet) error {
		if cs.Dirty() {
			t.Error("fresh write changeSet reports dirty before any append")
		}
		if err := cs.SeedLabel("ATM:open-tasks", "open work", "status:open", "developer@claude:test"); err != nil {
			return err
		}
		if !cs.Dirty() {
			t.Error("SeedLabel of an absent label appended but Dirty() is false")
		}
		return nil
	}); err != nil {
		t.Fatalf("seed txn: %v", err)
	}

	// The regression case: no-op paths must stay clean.
	if err := e.WithProjectWrite("ATM", func(cs core.ChangeSet) error {
		if err := cs.SeedLabel("ATM:open-tasks", "different desc", "", "developer@claude:test"); err != nil {
			return err
		}
		if cs.Dirty() {
			t.Error("SeedLabel of a live label is a no-op but Dirty() is true")
		}
		if err := cs.EnsureLabels([]string{"ATM:open-tasks"}, "developer@claude:test"); err != nil {
			return err
		}
		if cs.Dirty() {
			t.Error("EnsureLabels with only live names is a no-op but Dirty() is true")
		}
		if err := cs.EnsureLabels([]string{"ATM:brand-new"}, "developer@claude:test"); err != nil {
			return err
		}
		if !cs.Dirty() {
			t.Error("EnsureLabels registered a new label but Dirty() is false")
		}
		return nil
	}); err != nil {
		t.Fatalf("no-op txn: %v", err)
	}

	// A dirty flag never leaks across transactions.
	if err := e.WithProjectWrite("ATM", func(cs core.ChangeSet) error {
		if cs.Dirty() {
			t.Error("new changeSet inherited dirty state from a previous transaction")
		}
		if err := cs.UpsertLabel("ATM:x", core.LabelFields{}, "developer@claude:test"); err != nil {
			return err
		}
		if !cs.Dirty() {
			t.Error("UpsertLabel appended but Dirty() is false")
		}
		return nil
	}); err != nil {
		t.Fatalf("upsert txn: %v", err)
	}
}
