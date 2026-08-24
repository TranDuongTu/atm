package scrum

import (
	"strings"
	"testing"
)

func findingFor(t *testing.T, rep *ProjectReport, taskID, substr string) bool {
	t.Helper()
	for _, f := range rep.Findings {
		if f.TaskID == taskID && strings.Contains(f.Detail, substr) {
			return true
		}
	}
	return false
}

func TestReportRostersTheThreeLanes(t *testing.T) {
	s := newTestStore(t)
	r := newRecorder(s)
	inbox, _ := s.CreateTask("ATM", "untouched", "", nil, testActor)
	claimed, _ := s.CreateTask("ATM", "building", "", nil, testActor)
	_ = r.Absorb(claimed.ID, TypeTask, StageImplementing)
	evicted, _ := s.CreateTask("ATM", "declined", "", nil, testActor)
	_ = r.Evict(evicted.ID, OutNotWorthIt, "")

	rep, err := (&Reporter{Store: s}).Report("ATM")
	if err != nil {
		t.Fatalf("Report: %v", err)
	}
	if len(rep.Inbox) != 1 || rep.Inbox[0] != inbox.ID {
		t.Fatalf("inbox = %v, want [%s]", rep.Inbox, inbox.ID)
	}
	if len(rep.Pipeline) != 1 || rep.Pipeline[0].TaskID != claimed.ID || rep.Pipeline[0].Type != TypeTask {
		t.Fatalf("pipeline = %+v", rep.Pipeline)
	}
	if len(rep.Out) != 1 || rep.Out[0].TaskID != evicted.ID || rep.Out[0].Reason != OutNotWorthIt {
		t.Fatalf("out = %+v", rep.Out)
	}
}

func TestReportFlagsAParentWhoseChildrenAreAllDone(t *testing.T) {
	s := newTestStore(t)
	r := newRecorder(s)
	story, _ := s.CreateTask("ATM", "story", "", nil, testActor)
	_ = r.Absorb(story.ID, TypeStory, StageImplementing)
	if _, err := r.Add("ATM", "child", TypeTask, story.ID, StageDone); err != nil {
		t.Fatalf("Add: %v", err)
	}
	rep, err := (&Reporter{Store: s}).Report("ATM")
	if err != nil {
		t.Fatalf("Report: %v", err)
	}
	if !findingFor(t, rep, story.ID, "every child is done") {
		t.Fatalf("findings = %+v", rep.Findings)
	}
}

func TestReportFlagsOrphanAndEvictedParents(t *testing.T) {
	s := newTestStore(t)
	r := newRecorder(s)
	orphan, _ := s.CreateTask("ATM", "orphan", "", []string{"ATM:scrum:task"}, testActor)
	if err := s.SetTaskCapabilityMeta(orphan.ID, CapabilityName, `{"v":1,"part_of":"ATM-gone99"}`, testActor); err != nil {
		t.Fatalf("seed: %v", err)
	}
	parent, _ := s.CreateTask("ATM", "parent", "", nil, testActor)
	_ = r.Absorb(parent.ID, TypeStory, StageImplementing)
	child, err := r.Add("ATM", "child", TypeTask, parent.ID, StageImplementing)
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	_ = r.Evict(parent.ID, OutOfScope, "")

	rep, err := (&Reporter{Store: s}).Report("ATM")
	if err != nil {
		t.Fatalf("Report: %v", err)
	}
	if !findingFor(t, rep, orphan.ID, "ATM-gone99") {
		t.Fatalf("missing-parent finding absent: %+v", rep.Findings)
	}
	if !findingFor(t, rep, child.ID, "evicted") {
		t.Fatalf("evicted-parent finding absent: %+v", rep.Findings)
	}
}

func TestReportDerivesBlockingFromDependsOn(t *testing.T) {
	s := newTestStore(t)
	r := newRecorder(s)
	dep, _ := s.CreateTask("ATM", "dep", "", nil, testActor)
	_ = r.Absorb(dep.ID, TypeTask, StageImplementing)
	work, _ := s.CreateTask("ATM", "work", "", nil, testActor)
	_ = r.Absorb(work.ID, TypeTask, StagePlanned)
	if err := r.LinkDependsOn(work.ID, dep.ID); err != nil {
		t.Fatalf("LinkDependsOn: %v", err)
	}
	if err := r.LinkDependsOn(work.ID, "ATM-gone99"); err == nil {
		t.Fatal("linking a missing target must fail")
	}
	rep, err := (&Reporter{Store: s}).Report("ATM")
	if err != nil {
		t.Fatalf("Report: %v", err)
	}
	var found bool
	for _, sum := range rep.Pipeline {
		if sum.TaskID == work.ID {
			found = true
			if len(sum.BlockedBy) != 1 || sum.BlockedBy[0] != dep.ID {
				t.Fatalf("blocked_by = %v, want [%s]", sum.BlockedBy, dep.ID)
			}
		}
	}
	if !found {
		t.Fatalf("work not in pipeline roster: %+v", rep.Pipeline)
	}
	// Once the dependency converges, nothing is blocked.
	if err := r.Stage(dep.ID, StageDone); err != nil {
		t.Fatalf("Stage: %v", err)
	}
	rep, _ = (&Reporter{Store: s}).Report("ATM")
	for _, sum := range rep.Pipeline {
		if sum.TaskID == work.ID && len(sum.BlockedBy) != 0 {
			t.Fatalf("blocked_by = %v, want none", sum.BlockedBy)
		}
	}
}

func TestReportFlagsAClaimWithNoStageAndUnknownVocabulary(t *testing.T) {
	s := newTestStore(t)
	bare, _ := s.CreateTask("ATM", "bare", "", []string{"ATM:scrum:task"}, testActor)
	drift, _ := s.CreateTask("ATM", "drift", "", []string{"ATM:scrum:task", "ATM:scrum-stage:shipping"}, testActor)
	rep, err := (&Reporter{Store: s}).Report("ATM")
	if err != nil {
		t.Fatalf("Report: %v", err)
	}
	if !findingFor(t, rep, bare.ID, "no stage") {
		t.Fatalf("findings = %+v", rep.Findings)
	}
	if !findingFor(t, rep, drift.ID, "scrum-stage:shipping") {
		t.Fatalf("findings = %+v", rep.Findings)
	}
}

func TestReportFlagsAnUnparseablePayload(t *testing.T) {
	s := newTestStore(t)
	bad, _ := s.CreateTask("ATM", "bad", "", []string{"ATM:scrum:task", "ATM:scrum-stage:planned"}, testActor)
	_ = s.SetTaskCapabilityMeta(bad.ID, CapabilityName, "not json", testActor)
	rep, err := (&Reporter{Store: s}).Report("ATM")
	if err != nil {
		t.Fatalf("Report: %v", err)
	}
	if !findingFor(t, rep, bad.ID, "hand-repair") {
		t.Fatalf("findings = %+v", rep.Findings)
	}
}

// The reader must be a reader: the store is byte-identical either side of it.
func TestReportDoesNotTouchTheStore(t *testing.T) {
	s := newTestStore(t)
	r := newRecorder(s)
	story, _ := s.CreateTask("ATM", "story", "", nil, testActor)
	_ = r.Absorb(story.ID, TypeStory, StageImplementing)
	_, _ = r.Add("ATM", "child", TypeTask, story.ID, StageDone)
	bad, _ := s.CreateTask("ATM", "bad", "", []string{"ATM:scrum:task"}, testActor)
	_ = s.SetTaskCapabilityMeta(bad.ID, CapabilityName, "not json", testActor)

	before := ledgerDigest(t, s)
	if _, err := (&Reporter{Store: s}).Report("ATM"); err != nil {
		t.Fatalf("Report: %v", err)
	}
	if _, err := (&Reporter{Store: s}).Links(story.ID); err != nil {
		t.Fatalf("Links: %v", err)
	}
	if after := ledgerDigest(t, s); after != before {
		t.Fatal("the reporter appended to the ledger")
	}
}

func TestLinksDerivesInboundChildrenAndDependents(t *testing.T) {
	s := newTestStore(t)
	r := newRecorder(s)
	story, _ := s.CreateTask("ATM", "story", "", nil, testActor)
	_ = r.Absorb(story.ID, TypeStory, StageImplementing)
	a, _ := r.Add("ATM", "a", TypeTask, story.ID, StageImplementing)
	b, _ := r.Add("ATM", "b", TypeTask, story.ID, StageImplementing)
	_ = r.LinkDependsOn(b.ID, a.ID)

	l, err := (&Reporter{Store: s}).Links(story.ID)
	if err != nil {
		t.Fatalf("Links: %v", err)
	}
	if len(l.Children) != 2 || !containsString(l.Children, a.ID) || !containsString(l.Children, b.ID) {
		t.Fatalf("children = %v", l.Children)
	}
	la, err := (&Reporter{Store: s}).Links(a.ID)
	if err != nil {
		t.Fatalf("Links: %v", err)
	}
	if len(la.Dependents) != 1 || la.Dependents[0] != b.ID {
		t.Fatalf("dependents = %v", la.Dependents)
	}
	if la.PartOf != story.ID {
		t.Fatalf("part_of = %q", la.PartOf)
	}
}
