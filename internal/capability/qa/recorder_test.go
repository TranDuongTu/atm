package qa

import (
	"strings"
	"testing"
)

func TestAbsorbClaimsTheOriginalForTesting(t *testing.T) {
	s := newTestStore(t)
	r := newRecorder(s)
	tk, _ := s.CreateTask("ATM", "finished work", "", nil, testActor)
	if err := r.Absorb(tk.ID); err != nil {
		t.Fatalf("Absorb: %v", err)
	}
	if !containsString(labelsOf(t, s, tk.ID), "ATM:qa:testing") {
		t.Fatalf("labels = %v", labelsOf(t, s, tk.ID))
	}
}

func TestAbsorbRefusesAnEvictedTask(t *testing.T) {
	s := newTestStore(t)
	r := newRecorder(s)
	tk, _ := s.CreateTask("ATM", "declined", "", nil, testActor)
	_ = r.Evict(tk.ID, OutNotRelevant, "")
	if err := r.Absorb(tk.ID); err == nil || !strings.Contains(err.Error(), "release") {
		t.Fatalf("err = %v, want a pointer at release", err)
	}
}

func TestScaffoldIsBornClaimedUnderItsOriginal(t *testing.T) {
	s := newTestStore(t)
	r := newRecorder(s)
	orig, _ := s.CreateTask("ATM", "original", "", nil, testActor)
	_ = r.Absorb(orig.ID)
	sc, err := r.Scaffold(orig.ID, "staging run")
	if err != nil {
		t.Fatalf("Scaffold: %v", err)
	}
	if !containsString(labelsOf(t, s, sc.ID), "ATM:qa:testing") {
		t.Fatalf("scaffold labels = %v", labelsOf(t, s, sc.ID))
	}
	if payloadOf(t, s, sc.ID).PartOf() != orig.ID {
		t.Fatalf("scaffold part_of = %q", payloadOf(t, s, sc.ID).PartOf())
	}
	roster := payloadOf(t, s, orig.ID).Scaffolds()
	if len(roster) != 1 || roster[0] != sc.ID {
		t.Fatalf("original roster = %v", roster)
	}
}

func TestScaffoldRefusesAnUnclaimedOriginal(t *testing.T) {
	s := newTestStore(t)
	r := newRecorder(s)
	loose, _ := s.CreateTask("ATM", "not in qa", "", nil, testActor)
	if _, err := r.Scaffold(loose.ID, "run"); err == nil {
		t.Fatal("a scaffold must hang off a claimed original")
	}
}

func TestScaffoldRefusesToNestUnderAScaffold(t *testing.T) {
	s := newTestStore(t)
	r := newRecorder(s)
	orig, _ := s.CreateTask("ATM", "original", "", nil, testActor)
	_ = r.Absorb(orig.ID)
	sc, _ := r.Scaffold(orig.ID, "staging")
	if _, err := r.Scaffold(sc.ID, "nested"); err == nil {
		t.Fatal("scaffolds do not nest")
	}
}

// The originals-only finish guarantee: this is what makes qa:done a reliable
// downstream signal, so it gets its own test.
func TestPassNeverStampsDoneOnAScaffold(t *testing.T) {
	s := newTestStore(t)
	r := newRecorder(s)
	orig, _ := s.CreateTask("ATM", "original", "", nil, testActor)
	_ = r.Absorb(orig.ID)
	sc, _ := r.Scaffold(orig.ID, "staging")
	if err := r.Pass(sc.ID); err != nil {
		t.Fatalf("Pass scaffold: %v", err)
	}
	got := labelsOf(t, s, sc.ID)
	if containsString(got, "ATM:qa:done") {
		t.Fatalf("a scaffold was stamped done: %v", got)
	}
	if containsString(got, "ATM:qa:testing") {
		t.Fatalf("scaffold claim survived pass: %v", got)
	}
	if payloadOf(t, s, sc.ID).PartOf() != orig.ID {
		t.Fatal("a passed scaffold must keep its part_of for history")
	}
	comments, _ := s.ListComments(sc.ID)
	if len(comments) == 0 || !strings.Contains(comments[len(comments)-1].Body, "passed") {
		t.Fatalf("scaffold pass comment missing: %v", comments)
	}
}

func TestPassRefusesAnOriginalWithLiveScaffolds(t *testing.T) {
	s := newTestStore(t)
	r := newRecorder(s)
	orig, _ := s.CreateTask("ATM", "original", "", nil, testActor)
	_ = r.Absorb(orig.ID)
	a, _ := r.Scaffold(orig.ID, "staging")
	b, _ := r.Scaffold(orig.ID, "dev")
	_ = r.Pass(a.ID)

	err := r.Pass(orig.ID)
	if err == nil {
		t.Fatal("an original with a live scaffold must not pass")
	}
	if !strings.Contains(err.Error(), b.ID) {
		t.Fatalf("err = %v, want it to name %s", err, b.ID)
	}
	if strings.Contains(err.Error(), a.ID) {
		t.Fatalf("err = %v, must not name the passed scaffold %s", err, a.ID)
	}
	if err := r.Pass(b.ID); err != nil {
		t.Fatalf("Pass b: %v", err)
	}
	if err := r.Pass(orig.ID); err != nil {
		t.Fatalf("Pass original: %v", err)
	}
	got := labelsOf(t, s, orig.ID)
	if !containsString(got, "ATM:qa:done") || containsString(got, "ATM:qa:testing") {
		t.Fatalf("original labels = %v", got)
	}
}

func TestPassRefusesUnclaimedWork(t *testing.T) {
	s := newTestStore(t)
	r := newRecorder(s)
	tk, _ := s.CreateTask("ATM", "loose", "", nil, testActor)
	if err := r.Pass(tk.ID); err == nil || !strings.Contains(err.Error(), "absorb") {
		t.Fatalf("err = %v, want a pointer at absorb", err)
	}
}

func TestEvictSwapsTheAxesExclusively(t *testing.T) {
	s := newTestStore(t)
	r := newRecorder(s)
	tk, _ := s.CreateTask("ATM", "broken", "", nil, testActor)
	_ = r.Absorb(tk.ID)
	if err := r.Evict(tk.ID, OutFailed, ""); err != nil {
		t.Fatalf("Evict: %v", err)
	}
	got := labelsOf(t, s, tk.ID)
	if !containsString(got, "ATM:qa-out:failed") {
		t.Fatalf("labels = %v", got)
	}
	for _, l := range got {
		if strings.HasPrefix(l, "ATM:qa:") {
			t.Fatalf("claim label survived evict: %v", got)
		}
	}
}

func TestEvictCoveredByRequiresATarget(t *testing.T) {
	s := newTestStore(t)
	r := newRecorder(s)
	tk, _ := s.CreateTask("ATM", "x", "", nil, testActor)
	if err := r.Evict(tk.ID, OutCoveredBy, ""); err == nil || !strings.Contains(err.Error(), "covered-by") {
		t.Fatalf("err = %v", err)
	}
}

func TestReleaseClearsQAOnlyAndComments(t *testing.T) {
	s := newTestStore(t)
	r := newRecorder(s)
	orig, _ := s.CreateTask("ATM", "original", "", []string{"ATM:scrum:task"}, testActor)
	_ = r.Absorb(orig.ID)
	_, _ = r.Scaffold(orig.ID, "staging")
	if err := r.Release(orig.ID, "wrong target"); err != nil {
		t.Fatalf("Release: %v", err)
	}
	got := labelsOf(t, s, orig.ID)
	for _, l := range got {
		if strings.HasPrefix(l, "ATM:qa") {
			t.Fatalf("qa label survived release: %s", l)
		}
	}
	if !containsString(got, "ATM:scrum:task") {
		t.Fatalf("a foreign label was removed: %v", got)
	}
	after, _ := s.GetTask(orig.ID)
	if after.Meta[CapabilityName] != "" {
		t.Fatalf("qa payload survived release: %q", after.Meta[CapabilityName])
	}
	comments, _ := s.ListComments(orig.ID)
	if len(comments) == 0 || !strings.Contains(comments[len(comments)-1].Body, "wrong target") {
		t.Fatalf("release reason comment missing: %v", comments)
	}
}

func TestRecorderFailsOnMalformedPayload(t *testing.T) {
	s := newTestStore(t)
	r := newRecorder(s)
	tk, _ := s.CreateTask("ATM", "bad", "", nil, testActor)
	_ = s.SetTaskCapabilityMeta(tk.ID, CapabilityName, "not json", testActor)
	if err := r.Absorb(tk.ID); err == nil || !strings.Contains(err.Error(), "hand-repair") {
		t.Fatalf("err = %v, want hand-repair", err)
	}
}
