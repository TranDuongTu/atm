package codereview

import (
	"strings"
	"testing"

	"atm/internal/capability"
)

var _ capability.Flow = Cap{}

func TestVocabularySeedsLaneBoardsWithSelfExclusionInbox(t *testing.T) {
	byName := map[string]string{}
	for _, l := range Vocabulary("ATM") {
		byName[l.Name] = l.Expr
	}
	if got := byName["ATM:codereview-inbox"]; got != "NOT codereview:* AND NOT codereview-out:*" {
		t.Fatalf("inbox expr = %q", got)
	}
	if got := byName["ATM:codereview-pipeline"]; got != "codereview:* AND NOT codereview-out:*" {
		t.Fatalf("pipeline expr = %q", got)
	}
	if got := byName["ATM:codereview-out-board"]; got != "codereview-out:*" {
		t.Fatalf("out expr = %q", got)
	}
}

func TestFlowContract(t *testing.T) {
	c := New()
	if c.FinishLabel("ATM").Name != "ATM:codereview:done" {
		t.Fatalf("finish = %q", c.FinishLabel("ATM").Name)
	}
	if c.EvictLabel("ATM").Name != "ATM:codereview-out:*" {
		t.Fatalf("evict = %q", c.EvictLabel("ATM").Name)
	}
	lanes := c.Lanes("ATM")
	if lanes.Inbox != "ATM:codereview-inbox" || lanes.Pipeline != "ATM:codereview-pipeline" || lanes.Out != "ATM:codereview-out-board" {
		t.Fatalf("lanes = %+v", lanes)
	}
}

// The PR gate is the capability's whole warning mechanism, so it is the first
// thing pinned.
func TestAbsorbRefusesWithoutAPullRequest(t *testing.T) {
	s := newTestStore(t)
	r := newRecorder(s)
	tk, _ := s.CreateTask("ATM", "finished work", "", nil, testActor)
	err := r.Absorb(tk.ID, "  ")
	if err == nil || !strings.Contains(err.Error(), "inbox") {
		t.Fatalf("err = %v, want a refusal that explains the inbox is the warning", err)
	}
	if len(labelsOf(t, s, tk.ID)) != 0 {
		t.Fatalf("a refused absorb still stamped labels: %v", labelsOf(t, s, tk.ID))
	}
}

func TestAbsorbSchedulesAndRecordsThePR(t *testing.T) {
	s := newTestStore(t)
	r := newRecorder(s)
	tk, _ := s.CreateTask("ATM", "finished work", "", nil, testActor)
	if err := r.Absorb(tk.ID, "#142"); err != nil {
		t.Fatalf("Absorb: %v", err)
	}
	if !containsString(labelsOf(t, s, tk.ID), "ATM:codereview:scheduled") {
		t.Fatalf("labels = %v", labelsOf(t, s, tk.ID))
	}
	if payloadOf(t, s, tk.ID).PR() != "#142" {
		t.Fatalf("pr = %q", payloadOf(t, s, tk.ID).PR())
	}
}

func TestAbsorbRefusesAnEvictedTask(t *testing.T) {
	s := newTestStore(t)
	r := newRecorder(s)
	tk, _ := s.CreateTask("ATM", "x", "", nil, testActor)
	_ = r.Evict(tk.ID, OutNotWarranted)
	if err := r.Absorb(tk.ID, "#1"); err == nil || !strings.Contains(err.Error(), "release") {
		t.Fatalf("err = %v", err)
	}
}

func TestReviewWalksScheduledToReviewingToDone(t *testing.T) {
	s := newTestStore(t)
	r := newRecorder(s)
	tk, _ := s.CreateTask("ATM", "work", "", nil, testActor)
	_ = r.Absorb(tk.ID, "#142")
	if err := r.Begin(tk.ID); err != nil {
		t.Fatalf("Begin: %v", err)
	}
	if !containsString(labelsOf(t, s, tk.ID), "ATM:codereview:reviewing") {
		t.Fatalf("labels = %v", labelsOf(t, s, tk.ID))
	}
	if err := r.Finish(tk.ID, "docs/reviews/142.md"); err != nil {
		t.Fatalf("Finish: %v", err)
	}
	got := labelsOf(t, s, tk.ID)
	if !containsString(got, "ATM:codereview:done") || containsString(got, "ATM:codereview:reviewing") {
		t.Fatalf("labels = %v", got)
	}
	if payloadOf(t, s, tk.ID).Report() != "docs/reviews/142.md" {
		t.Fatalf("report = %q", payloadOf(t, s, tk.ID).Report())
	}
}

func TestBeginAndFinishRefuseUnclaimedWork(t *testing.T) {
	s := newTestStore(t)
	r := newRecorder(s)
	tk, _ := s.CreateTask("ATM", "loose", "", nil, testActor)
	if err := r.Begin(tk.ID); err == nil || !strings.Contains(err.Error(), "absorb") {
		t.Fatalf("Begin err = %v", err)
	}
	if err := r.Finish(tk.ID, ""); err == nil || !strings.Contains(err.Error(), "absorb") {
		t.Fatalf("Finish err = %v", err)
	}
}

func TestBeginRefusesAFinishedReview(t *testing.T) {
	s := newTestStore(t)
	r := newRecorder(s)
	tk, _ := s.CreateTask("ATM", "work", "", nil, testActor)
	_ = r.Absorb(tk.ID, "#1")
	_ = r.Finish(tk.ID, "")
	if err := r.Begin(tk.ID); err == nil || !strings.Contains(err.Error(), "release") {
		t.Fatalf("err = %v, want a pointer at release", err)
	}
}

func TestEvictSwapsTheAxesExclusively(t *testing.T) {
	s := newTestStore(t)
	r := newRecorder(s)
	tk, _ := s.CreateTask("ATM", "work", "", nil, testActor)
	_ = r.Absorb(tk.ID, "#1")
	if err := r.Evict(tk.ID, OutSuperseded); err != nil {
		t.Fatalf("Evict: %v", err)
	}
	got := labelsOf(t, s, tk.ID)
	if !containsString(got, "ATM:codereview-out:superseded") {
		t.Fatalf("labels = %v", got)
	}
	for _, l := range got {
		if strings.HasPrefix(l, "ATM:codereview:") {
			t.Fatalf("claim label survived evict: %v", got)
		}
	}
}

func TestReleaseClearsCodereviewOnlyAndComments(t *testing.T) {
	s := newTestStore(t)
	r := newRecorder(s)
	tk, _ := s.CreateTask("ATM", "work", "", []string{"ATM:scrum:task"}, testActor)
	_ = r.Absorb(tk.ID, "#142")
	_ = r.Finish(tk.ID, "docs/reviews/142.md")
	if err := r.Release(tk.ID, "changes requested; re-spiral"); err != nil {
		t.Fatalf("Release: %v", err)
	}
	got := labelsOf(t, s, tk.ID)
	for _, l := range got {
		if strings.HasPrefix(l, "ATM:codereview") {
			t.Fatalf("codereview label survived release: %s", l)
		}
	}
	if !containsString(got, "ATM:scrum:task") {
		t.Fatalf("a foreign label was removed: %v", got)
	}
	after, _ := s.GetTask(tk.ID)
	if after.Meta[CapabilityName] != "" {
		t.Fatalf("payload survived release: %q", after.Meta[CapabilityName])
	}
	comments, _ := s.ListComments(tk.ID)
	if len(comments) == 0 || !strings.Contains(comments[len(comments)-1].Body, "re-spiral") {
		t.Fatalf("release reason comment missing: %v", comments)
	}
}

func TestRecorderFailsOnMalformedPayload(t *testing.T) {
	s := newTestStore(t)
	r := newRecorder(s)
	tk, _ := s.CreateTask("ATM", "bad", "", nil, testActor)
	_ = s.SetTaskCapabilityMeta(tk.ID, CapabilityName, "not json", testActor)
	if err := r.Absorb(tk.ID, "#1"); err == nil || !strings.Contains(err.Error(), "hand-repair") {
		t.Fatalf("err = %v", err)
	}
}
