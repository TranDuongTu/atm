package codereview

import (
	"strings"
	"testing"

	"atm/internal/store"
)

// fixture is the small scaffolding these tests share: a store with the
// vocabulary seeded, a recorder, and a way to make a plain task.
type fixture struct {
	t     *testing.T
	store *store.Store
	rec   *Recorder
	code  string
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	s := newTestStore(t)
	return &fixture{t: t, store: s, rec: newRecorder(s), code: "ATM"}
}

func (f *fixture) task(title string) string {
	f.t.Helper()
	tk, err := f.store.CreateTask(f.code, title, "", nil, testActor)
	if err != nil {
		f.t.Fatal(err)
	}
	return tk.ID
}

// A finding worth tracked action becomes an item on the board that knows
// which review it came from, and a review that knows its items.
func TestFollowUpRecordsTheEdgeAtBothEnds(t *testing.T) {
	f := newFixture(t)
	review := f.task("review the auth rewrite")
	if err := f.rec.Absorb(review, "https://example.invalid/pr/1"); err != nil {
		t.Fatal(err)
	}
	item, err := f.rec.FollowUp(review, "extract the token parser")
	if err != nil {
		t.Fatal(err)
	}
	if item.Title != "extract the token parser" {
		t.Fatalf("item = %+v", item)
	}
	// Born into the pipeline, so the board shows it.
	tk, err := f.store.GetTask(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !hasLabel(tk.Labels, f.code+":"+ClaimNamespace+":"+StateScheduled) {
		t.Fatalf("labels = %v, want the item claimed by codereview", tk.Labels)
	}
	if got := (Cap{}).ParentOf(*tk); got != review {
		t.Fatalf("ParentOf = %q, want the review %q", got, review)
	}
	_, pl, err := f.rec.taskPayload(review)
	if err != nil {
		t.Fatal(err)
	}
	if len(pl.FollowUps()) != 1 || pl.FollowUps()[0] != item.ID {
		t.Fatalf("review roster = %v, want [%s]", pl.FollowUps(), item.ID)
	}
}

// The whole point of a tracked item is that a finding worth fixing need not
// hold the review open. A qa scaffold blocks its original; this must not.
func TestFinishIsNotBlockedByOpenFollowUps(t *testing.T) {
	f := newFixture(t)
	review := f.task("review the auth rewrite")
	if err := f.rec.Absorb(review, "https://example.invalid/pr/1"); err != nil {
		t.Fatal(err)
	}
	if err := f.rec.Begin(review); err != nil {
		t.Fatal(err)
	}
	if _, err := f.rec.FollowUp(review, "extract the token parser"); err != nil {
		t.Fatal(err)
	}
	if err := f.rec.Finish(review, "https://example.invalid/report"); err != nil {
		t.Fatalf("finish refused with an open follow-up: %v", err)
	}
	tk, _ := f.store.GetTask(review)
	if StateOf(tk, f.code) != StateDone {
		t.Fatalf("state = %q, want %q", StateOf(tk, f.code), StateDone)
	}
}

func TestFollowUpsDoNotNest(t *testing.T) {
	f := newFixture(t)
	review := f.task("review the auth rewrite")
	if err := f.rec.Absorb(review, "https://example.invalid/pr/1"); err != nil {
		t.Fatal(err)
	}
	item, err := f.rec.FollowUp(review, "extract the token parser")
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.rec.FollowUp(item.ID, "and another thing")
	if err == nil || !strings.Contains(err.Error(), "do not nest") {
		t.Fatalf("err = %v, want a refusal to nest", err)
	}
}

func TestFollowUpNeedsAClaimedReviewAndATitle(t *testing.T) {
	f := newFixture(t)
	review := f.task("review the auth rewrite")
	if _, err := f.rec.FollowUp(review, "x"); err == nil {
		t.Fatal("accepted a follow-up on a task codereview has not absorbed")
	}
	if err := f.rec.Absorb(review, "https://example.invalid/pr/1"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.rec.FollowUp(review, "  "); err == nil {
		t.Fatal("accepted a follow-up with no title")
	}
}

func hasLabel(labels []string, want string) bool {
	for _, l := range labels {
		if l == want {
			return true
		}
	}
	return false
}
