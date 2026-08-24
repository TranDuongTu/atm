package scrum

import (
	"strings"
	"testing"

	"atm/internal/store"
)

func countPrefix(t *testing.T, s *store.Store, id, prefix string) int {
	t.Helper()
	n := 0
	for _, l := range labelsOf(t, s, id) {
		if strings.HasPrefix(l, prefix) {
			n++
		}
	}
	return n
}

func TestAbsorbStampsExactlyOneType(t *testing.T) {
	s := newTestStore(t)
	r := newRecorder(s)
	tk, _ := s.CreateTask("ATM", "idea", "", nil, testActor)
	if err := r.Absorb(tk.ID, TypeTask, ""); err != nil {
		t.Fatalf("Absorb: %v", err)
	}
	if !containsString(labelsOf(t, s, tk.ID), "ATM:scrum:task") {
		t.Fatalf("labels = %v", labelsOf(t, s, tk.ID))
	}
	// Re-absorbing as a different type replaces, never accumulates.
	if err := r.Absorb(tk.ID, TypeBug, ""); err != nil {
		t.Fatalf("re-Absorb: %v", err)
	}
	if n := countPrefix(t, s, tk.ID, "ATM:scrum:"); n != 1 {
		t.Fatalf("type labels = %d, want exactly 1 (%v)", n, labelsOf(t, s, tk.ID))
	}
	if !containsString(labelsOf(t, s, tk.ID), "ATM:scrum:bug") {
		t.Fatalf("labels = %v", labelsOf(t, s, tk.ID))
	}
}

func TestAbsorbAtAStageIsTheMigrationPath(t *testing.T) {
	s := newTestStore(t)
	r := newRecorder(s)
	tk, _ := s.CreateTask("ATM", "already built", "", nil, testActor)
	if err := r.Absorb(tk.ID, TypeTask, StageDone); err != nil {
		t.Fatalf("Absorb: %v", err)
	}
	got := labelsOf(t, s, tk.ID)
	if !containsString(got, "ATM:scrum:task") || !containsString(got, "ATM:scrum-stage:done") {
		t.Fatalf("labels = %v", got)
	}
}

func TestAbsorbRefusesAnEvictedTask(t *testing.T) {
	s := newTestStore(t)
	r := newRecorder(s)
	tk, _ := s.CreateTask("ATM", "declined", "", nil, testActor)
	if err := r.Evict(tk.ID, OutNotWorthIt, ""); err != nil {
		t.Fatalf("Evict: %v", err)
	}
	err := r.Absorb(tk.ID, TypeTask, "")
	if err == nil || !strings.Contains(err.Error(), "release") {
		t.Fatalf("Absorb of an evicted task err = %v, want a pointer at release", err)
	}
}

func TestAbsorbRejectsAnUnknownType(t *testing.T) {
	s := newTestStore(t)
	r := newRecorder(s)
	tk, _ := s.CreateTask("ATM", "idea", "", nil, testActor)
	if err := r.Absorb(tk.ID, "epicc", ""); err == nil {
		t.Fatal("Absorb with an unknown type must fail")
	}
}

func TestEvictSwapsTheAxesExclusively(t *testing.T) {
	s := newTestStore(t)
	r := newRecorder(s)
	cover, _ := s.CreateTask("ATM", "cover", "", nil, testActor)
	tk, _ := s.CreateTask("ATM", "dup", "", nil, testActor)
	_ = r.Absorb(tk.ID, TypeTask, StagePlanned)
	if err := r.Evict(tk.ID, OutCoveredBy, cover.ID); err != nil {
		t.Fatalf("Evict: %v", err)
	}
	got := labelsOf(t, s, tk.ID)
	if !containsString(got, "ATM:scrum-out:covered-by") {
		t.Fatalf("labels = %v", got)
	}
	if n := countPrefix(t, s, tk.ID, "ATM:scrum:"); n != 0 {
		t.Fatalf("claim labels survived evict: %v", got)
	}
	if n := countPrefix(t, s, tk.ID, "ATM:scrum-stage:"); n != 0 {
		t.Fatalf("stage labels survived evict: %v", got)
	}
	if payloadOf(t, s, tk.ID).CoveredBy() != cover.ID {
		t.Fatalf("covered_by = %q", payloadOf(t, s, tk.ID).CoveredBy())
	}
}

func TestEvictCoveredByRequiresATarget(t *testing.T) {
	s := newTestStore(t)
	r := newRecorder(s)
	tk, _ := s.CreateTask("ATM", "dup", "", nil, testActor)
	if err := r.Evict(tk.ID, OutCoveredBy, ""); err == nil || !strings.Contains(err.Error(), "covered-by") {
		t.Fatalf("err = %v, want a demand for --covered-by", err)
	}
}

func TestStageDoneOnAStoryWithAnUndoneChildFailsAndNamesIt(t *testing.T) {
	s := newTestStore(t)
	r := newRecorder(s)
	story, _ := s.CreateTask("ATM", "story", "", nil, testActor)
	_ = r.Absorb(story.ID, TypeStory, StageImplementing)
	childDone, err := r.Add("ATM", "done child", TypeTask, story.ID, StageDone)
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	childOpen, err := r.Add("ATM", "open child", TypeTask, story.ID, StageImplementing)
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	err = r.Stage(story.ID, StageDone)
	if err == nil {
		t.Fatal("stage done on a story with an undone child must fail")
	}
	if !strings.Contains(err.Error(), childOpen.ID) {
		t.Fatalf("err = %v, want it to name %s", err, childOpen.ID)
	}
	if strings.Contains(err.Error(), childDone.ID) {
		t.Fatalf("err = %v, must not name the done child %s", err, childDone.ID)
	}
	// Once the last child converges, the parent may be stamped.
	if err := r.Stage(childOpen.ID, StageDone); err != nil {
		t.Fatalf("Stage child: %v", err)
	}
	if err := r.Stage(story.ID, StageDone); err != nil {
		t.Fatalf("Stage story: %v", err)
	}
	if n := countPrefix(t, s, story.ID, "ATM:scrum-stage:"); n != 1 {
		t.Fatalf("stage labels = %d, want 1", n)
	}
}

func TestStageDoneOnATaskNeedsNoChildren(t *testing.T) {
	s := newTestStore(t)
	r := newRecorder(s)
	tk, _ := s.CreateTask("ATM", "unit", "", nil, testActor)
	_ = r.Absorb(tk.ID, TypeTask, StageImplementing)
	if err := r.Stage(tk.ID, StageDone); err != nil {
		t.Fatalf("Stage: %v", err)
	}
}

func TestStageRefusesUnclaimedWork(t *testing.T) {
	s := newTestStore(t)
	r := newRecorder(s)
	tk, _ := s.CreateTask("ATM", "unclaimed", "", nil, testActor)
	if err := r.Stage(tk.ID, StagePlanned); err == nil || !strings.Contains(err.Error(), "absorb") {
		t.Fatalf("err = %v, want a pointer at absorb", err)
	}
}

func TestAddIsBornClaimed(t *testing.T) {
	s := newTestStore(t)
	r := newRecorder(s)
	epic, _ := s.CreateTask("ATM", "epic", "", nil, testActor)
	_ = r.Absorb(epic.ID, TypeEpic, StagePlanned)
	child, err := r.Add("ATM", "a story", TypeStory, epic.ID, "")
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	got := labelsOf(t, s, child.ID)
	if !containsString(got, "ATM:scrum:story") {
		t.Fatalf("child labels = %v", got)
	}
	if payloadOf(t, s, child.ID).PartOf() != epic.ID {
		t.Fatalf("part_of = %q, want %s", payloadOf(t, s, child.ID).PartOf(), epic.ID)
	}
}

func TestAddRefusesAnUnclaimedParent(t *testing.T) {
	s := newTestStore(t)
	r := newRecorder(s)
	loose, _ := s.CreateTask("ATM", "not in scrum", "", nil, testActor)
	if _, err := r.Add("ATM", "child", TypeTask, loose.ID, ""); err == nil {
		t.Fatal("a born-claimed child must hang off a claimed parent")
	}
}

func TestReleaseClearsScrumOnlyAndComments(t *testing.T) {
	s := newTestStore(t)
	r := newRecorder(s)
	tk, _ := s.CreateTask("ATM", "work", "", []string{"ATM:status:open"}, testActor)
	_ = r.Absorb(tk.ID, TypeTask, StagePlanned)
	_ = r.SetPlan(tk.ID, "docs/plans/x.md")
	if err := r.Release(tk.ID, "wrong pipeline"); err != nil {
		t.Fatalf("Release: %v", err)
	}
	got := labelsOf(t, s, tk.ID)
	for _, l := range got {
		if strings.HasPrefix(l, "ATM:scrum") {
			t.Fatalf("scrum label survived release: %s", l)
		}
	}
	if !containsString(got, "ATM:status:open") {
		t.Fatalf("a foreign label was removed: %v", got)
	}
	tkAfter, _ := s.GetTask(tk.ID)
	if tkAfter.Meta[CapabilityName] != "" {
		t.Fatalf("scrum payload survived release: %q", tkAfter.Meta[CapabilityName])
	}
	comments, _ := s.ListComments(tk.ID)
	if len(comments) == 0 || !strings.Contains(comments[len(comments)-1].Body, "wrong pipeline") {
		t.Fatalf("release reason comment missing: %v", comments)
	}
}

func TestReleaseRequiresAReason(t *testing.T) {
	s := newTestStore(t)
	r := newRecorder(s)
	tk, _ := s.CreateTask("ATM", "work", "", nil, testActor)
	_ = r.Absorb(tk.ID, TypeTask, "")
	if err := r.Release(tk.ID, "   "); err == nil {
		t.Fatal("release must demand a reason")
	}
}

func TestReopenSwapsDoneToImplementing(t *testing.T) {
	s := newTestStore(t)
	r := newRecorder(s)
	tk, _ := s.CreateTask("ATM", "work", "", nil, testActor)
	_ = r.Absorb(tk.ID, TypeTask, StageDone)
	if err := r.Reopen(tk.ID, "review found a gap"); err != nil {
		t.Fatalf("Reopen: %v", err)
	}
	got := labelsOf(t, s, tk.ID)
	if containsString(got, "ATM:scrum-stage:done") || !containsString(got, "ATM:scrum-stage:implementing") {
		t.Fatalf("labels = %v", got)
	}
	comments, _ := s.ListComments(tk.ID)
	if len(comments) == 0 || !strings.Contains(comments[len(comments)-1].Body, "review found a gap") {
		t.Fatalf("reopen reason comment missing: %v", comments)
	}
}

func TestReopenRefusesWorkThatIsNotDone(t *testing.T) {
	s := newTestStore(t)
	r := newRecorder(s)
	tk, _ := s.CreateTask("ATM", "work", "", nil, testActor)
	_ = r.Absorb(tk.ID, TypeTask, StageImplementing)
	if err := r.Reopen(tk.ID, "nope"); err == nil {
		t.Fatal("reopen must refuse work that was never finished")
	}
}

func TestLinkDependsOnRefusesSelfCrossProjectAndDirectCycles(t *testing.T) {
	s := newTestStore(t)
	r := newRecorder(s)
	a, _ := s.CreateTask("ATM", "a", "", nil, testActor)
	b, _ := s.CreateTask("ATM", "b", "", nil, testActor)
	if err := r.LinkDependsOn(a.ID, a.ID); err == nil {
		t.Fatal("self-link must fail")
	}
	if err := r.LinkDependsOn(a.ID, b.ID); err != nil {
		t.Fatalf("LinkDependsOn: %v", err)
	}
	if err := r.LinkDependsOn(b.ID, a.ID); err == nil || !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("err = %v, want a cycle refusal", err)
	}
	if err := r.UnlinkDependsOn(a.ID, b.ID); err != nil {
		t.Fatalf("UnlinkDependsOn: %v", err)
	}
	if err := r.UnlinkDependsOn(a.ID, b.ID); err == nil {
		t.Fatal("unlinking an absent link must fail")
	}
}

func TestSpecAndPlanRecordLocators(t *testing.T) {
	s := newTestStore(t)
	r := newRecorder(s)
	tk, _ := s.CreateTask("ATM", "design", "", nil, testActor)
	_ = r.Absorb(tk.ID, TypeDesign, StageBrainstormed)
	if err := r.SetSpec(tk.ID, "docs/specs/x.md"); err != nil {
		t.Fatalf("SetSpec: %v", err)
	}
	if err := r.SetPlan(tk.ID, "docs/plans/x.md"); err != nil {
		t.Fatalf("SetPlan: %v", err)
	}
	pl := payloadOf(t, s, tk.ID)
	if pl.Spec() != "docs/specs/x.md" || pl.Plan() != "docs/plans/x.md" {
		t.Fatalf("locators = %q / %q", pl.Spec(), pl.Plan())
	}
}

func TestRecorderFailsOnMalformedPayload(t *testing.T) {
	s := newTestStore(t)
	r := newRecorder(s)
	tk, _ := s.CreateTask("ATM", "bad", "", nil, testActor)
	if err := s.SetTaskCapabilityMeta(tk.ID, CapabilityName, "not json", testActor); err != nil {
		t.Fatalf("seed malformed payload: %v", err)
	}
	if err := r.Absorb(tk.ID, TypeTask, ""); err == nil || !strings.Contains(err.Error(), "hand-repair") {
		t.Fatalf("err = %v, want hand-repair", err)
	}
}
