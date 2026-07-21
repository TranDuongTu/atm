package workflowai

import (
	"strings"
	"testing"
	"time"

	"atm/internal/store"
)

const testActor = "admin@cli:unset"

func fixedNow() time.Time { return time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC) }

func newRecorder(s *store.Store) *Recorder {
	return &Recorder{Store: s, Actor: testActor, Now: fixedNow}
}

func stageLabelCount(t *testing.T, s *store.Store, id string) int {
	t.Helper()
	tk, err := s.GetTask(id)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	n := 0
	for _, l := range tk.Labels {
		if strings.HasPrefix(l, "ATM:stage:") {
			n++
		}
	}
	return n
}

func TestLadderHappyPath(t *testing.T) {
	s := newTestStore(t)
	tk, _ := s.CreateTask("ATM", "t", "", nil, testActor)
	r := newRecorder(s)

	steps := []struct {
		call               func() (string, error)
		wantPrior, wantNow string
	}{
		{func() (string, error) { return r.Brainstorm(tk.ID) }, StageNew, StageBrainstormed},
		{func() (string, error) { return r.Clarify(tk.ID) }, StageBrainstormed, StageClarified},
		{func() (string, error) { return r.Plan(tk.ID, PlanKindFile, "docs/p.md") }, StageClarified, StagePlanned},
		{func() (string, error) { return r.Ready(tk.ID) }, StagePlanned, StageImplementable},
		{func() (string, error) { return r.Done(tk.ID) }, StageImplementable, StageDone},
	}
	for i, st := range steps {
		prior, err := st.call()
		if err != nil {
			t.Fatalf("step %d: %v", i, err)
		}
		if prior != st.wantPrior {
			t.Errorf("step %d prior = %q, want %q", i, prior, st.wantPrior)
		}
		got, _ := (&Reporter{Store: s}).Stage(tk.ID)
		if got != st.wantNow {
			t.Errorf("step %d stage = %q, want %q", i, got, st.wantNow)
		}
		if n := stageLabelCount(t, s, tk.ID); n != 1 {
			t.Errorf("step %d stage label count = %d, want 1", i, n)
		}
	}
}

func TestLadderRejectsSkippedRungs(t *testing.T) {
	s := newTestStore(t)
	r := newRecorder(s)
	tk, _ := s.CreateTask("ATM", "t", "", nil, testActor)

	cases := []struct {
		name    string
		call    func() (string, error)
		wantMsg string
	}{
		{"clarify from new", func() (string, error) { return r.Clarify(tk.ID) }, "clarify requires brainstormed"},
		{"plan from new", func() (string, error) { return r.Plan(tk.ID, PlanKindFile, "x") }, "plan requires clarified"},
		{"ready from new", func() (string, error) { return r.Ready(tk.ID) }, "no plan recorded"},
		{"done from new", func() (string, error) { return r.Done(tk.ID) }, "done requires implementable"},
	}
	for _, c := range cases {
		if _, err := c.call(); err == nil || !strings.Contains(err.Error(), c.wantMsg) {
			t.Errorf("%s: err = %v, want containing %q", c.name, err, c.wantMsg)
		}
	}
	// And a rung above: brainstorm on a staged task fails.
	tk2, _ := s.CreateTask("ATM", "t2", "", []string{"ATM:stage:clarified"}, testActor)
	if _, err := r.Brainstorm(tk2.ID); err == nil || !strings.Contains(err.Error(), "brainstorm requires a new task") {
		t.Errorf("brainstorm on clarified: err = %v", err)
	}
}

func TestVerbIsIdempotentNoOp(t *testing.T) {
	s := newTestStore(t)
	r := newRecorder(s)
	tk, _ := s.CreateTask("ATM", "t", "", []string{"ATM:stage:brainstormed"}, testActor)
	prior, err := r.Brainstorm(tk.ID)
	if err != nil {
		t.Fatalf("Brainstorm: %v", err)
	}
	if prior != StageBrainstormed {
		t.Errorf("prior = %q, want %q (no-op signals prior == target)", prior, StageBrainstormed)
	}
}

func TestSwapSelfHealsHandEditedMultiStage(t *testing.T) {
	s := newTestStore(t)
	r := newRecorder(s)
	// Hand-edited mess: brainstormed AND clarified at once. clarify's
	// predecessor (brainstormed) is present, so it proceeds and heals.
	tk, _ := s.CreateTask("ATM", "t", "", []string{"ATM:stage:brainstormed", "ATM:stage:clarified"}, testActor)
	if _, err := r.Clarify(tk.ID); err != nil {
		t.Fatalf("Clarify: %v", err)
	}
	if n := stageLabelCount(t, s, tk.ID); n != 1 {
		t.Errorf("stage label count = %d, want 1 (self-healed)", n)
	}
	if got, _ := (&Reporter{Store: s}).Stage(tk.ID); got != StageClarified {
		t.Errorf("stage = %q, want %q", got, StageClarified)
	}
}

func TestPlanRecordsLocatorAndTransitions(t *testing.T) {
	s := newTestStore(t)
	r := newRecorder(s)
	tk, _ := s.CreateTask("ATM", "t", "", []string{"ATM:stage:clarified"}, testActor)
	if _, err := r.Plan(tk.ID, PlanKindFile, "docs/superpowers/plans/x.md"); err != nil {
		t.Fatalf("Plan: %v", err)
	}
	got, _ := s.GetTask(tk.ID)
	pl, err := DecodePayload(got.Meta[CapabilityName])
	if err != nil {
		t.Fatalf("payload: %v", err)
	}
	p := pl.Plan()
	if p == nil || p.Kind != PlanKindFile || p.Ref != "docs/superpowers/plans/x.md" || p.Actor != testActor {
		t.Errorf("plan = %+v", p)
	}
	if p.RecordedAt != "2026-07-21T12:00:00Z" {
		t.Errorf("recorded_at = %q (injectable clock not used?)", p.RecordedAt)
	}
}

func TestPlanUpdatesInPlaceFromPlanned(t *testing.T) {
	s := newTestStore(t)
	r := newRecorder(s)
	tk, _ := s.CreateTask("ATM", "t", "", []string{"ATM:stage:clarified"}, testActor)
	_, _ = r.Plan(tk.ID, PlanKindEphemeral, "session 2026-07-20")
	prior, err := r.Plan(tk.ID, PlanKindFile, "docs/p.md") // re-plan: stage unchanged
	if err != nil {
		t.Fatalf("re-plan: %v", err)
	}
	if prior != StagePlanned {
		t.Errorf("prior = %q, want %q (update-in-place signals current stage)", prior, StagePlanned)
	}
	if got, _ := (&Reporter{Store: s}).Stage(tk.ID); got != StagePlanned {
		t.Errorf("stage = %q, want still planned", got)
	}
	got, _ := s.GetTask(tk.ID)
	pl, _ := DecodePayload(got.Meta[CapabilityName])
	if p := pl.Plan(); p == nil || p.Kind != PlanKindFile {
		t.Errorf("plan not updated: %+v", p)
	}
}

func TestPlanRejectsBadKind(t *testing.T) {
	s := newTestStore(t)
	r := newRecorder(s)
	tk, _ := s.CreateTask("ATM", "t", "", []string{"ATM:stage:clarified"}, testActor)
	if _, err := r.Plan(tk.ID, "url", "https://x"); err == nil || !strings.Contains(err.Error(), "invalid plan kind") {
		t.Errorf("err = %v", err)
	}
}

func TestReadyRequiresPlanRecord(t *testing.T) {
	s := newTestStore(t)
	r := newRecorder(s)
	// Hand-edited to planned WITHOUT a plan record.
	tk, _ := s.CreateTask("ATM", "t", "", []string{"ATM:stage:planned"}, testActor)
	if _, err := r.Ready(tk.ID); err == nil || !strings.Contains(err.Error(), "no plan recorded") {
		t.Errorf("err = %v", err)
	}
}

func TestDemoteClearsStageAndPlanKeepsLinks(t *testing.T) {
	s := newTestStore(t)
	r := newRecorder(s)
	// Task 4: re-enable revision_of + marker assertions once LinkRevisionOf lands.
	// parent, _ := s.CreateTask("ATM", "parent", "", []string{"ATM:stage:planned"}, testActor)
	tk, _ := s.CreateTask("ATM", "t", "", []string{"ATM:stage:clarified"}, testActor)
	_, _ = r.Plan(tk.ID, PlanKindEphemeral, "session x")
	// if err := r.LinkRevisionOf(tk.ID, parent.ID); err != nil {
	// 	t.Fatalf("link: %v", err)
	// }
	prior, err := r.Demote(tk.ID, "plan lost in session cleanup")
	if err != nil {
		t.Fatalf("Demote: %v", err)
	}
	if prior != StagePlanned {
		t.Errorf("prior = %q, want %q", prior, StagePlanned)
	}
	got, _ := s.GetTask(tk.ID)
	if n := stageLabelCount(t, s, tk.ID); n != 0 {
		t.Errorf("stage labels remain: %v", got.Labels)
	}
	pl, _ := DecodePayload(got.Meta[CapabilityName])
	if pl.Plan() != nil {
		t.Error("plan record survived demote")
	}
	// if pl.RevisionOf() != parent.ID {
	// 	t.Error("revision_of link did not survive demote")
	// }
	// if !containsString(got.Labels, "ATM:wfai:revision") {
	// 	t.Error("revision marker did not survive demote")
	// }
	comments, err := s.ListComments(tk.ID)
	if err != nil || len(comments) == 0 || !strings.Contains(comments[len(comments)-1].Body, "plan lost in session cleanup") {
		t.Errorf("demote reason comment missing: %v, %v", comments, err)
	}
}

func TestDemoteRequiresReason(t *testing.T) {
	s := newTestStore(t)
	r := newRecorder(s)
	tk, _ := s.CreateTask("ATM", "t", "", []string{"ATM:stage:brainstormed"}, testActor)
	if _, err := r.Demote(tk.ID, "  "); err == nil || !strings.Contains(err.Error(), "requires --reason") {
		t.Errorf("err = %v", err)
	}
}

func TestDemoteOfNewTaskIsNoOp(t *testing.T) {
	s := newTestStore(t)
	r := newRecorder(s)
	tk, _ := s.CreateTask("ATM", "t", "", nil, testActor)
	prior, err := r.Demote(tk.ID, "why not")
	if err != nil {
		t.Fatalf("Demote: %v", err)
	}
	if prior != StageNew {
		t.Errorf("prior = %q, want StageNew", prior)
	}
	got, _ := s.GetTask(tk.ID)
	if got.Meta[CapabilityName] != "" {
		t.Errorf("no-op demote wrote a payload: %q", got.Meta[CapabilityName])
	}
	if comments, _ := s.ListComments(tk.ID); len(comments) != 0 {
		t.Errorf("no-op demote wrote a comment")
	}
}

func TestVerbFailsOnMalformedPayload(t *testing.T) {
	s := newTestStore(t)
	r := newRecorder(s)
	tk, _ := s.CreateTask("ATM", "t", "", []string{"ATM:stage:clarified"}, testActor)
	if err := s.SetTaskCapabilityMeta(tk.ID, CapabilityName, "not json", testActor); err != nil {
		t.Fatalf("seed malformed payload: %v", err)
	}
	if _, err := r.Plan(tk.ID, PlanKindFile, "docs/p.md"); err == nil || !strings.Contains(err.Error(), "hand-repair") {
		t.Errorf("err = %v, want the hand-repair error (never overwrite unreadable state)", err)
	}
}
