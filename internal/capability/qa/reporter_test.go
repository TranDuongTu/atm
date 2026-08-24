package qa

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
	orig, _ := s.CreateTask("ATM", "original", "", nil, testActor)
	_ = r.Absorb(orig.ID)
	sc, _ := r.Scaffold(orig.ID, "staging")
	evicted, _ := s.CreateTask("ATM", "declined", "", nil, testActor)
	_ = r.Evict(evicted.ID, OutNotRelevant, "")

	rep, err := (&Reporter{Store: s}).Report("ATM")
	if err != nil {
		t.Fatalf("Report: %v", err)
	}
	if len(rep.Inbox) != 1 || rep.Inbox[0] != inbox.ID {
		t.Fatalf("inbox = %v, want [%s]", rep.Inbox, inbox.ID)
	}
	if len(rep.Pipeline) != 2 {
		t.Fatalf("pipeline = %+v, want the original and its scaffold", rep.Pipeline)
	}
	for _, sum := range rep.Pipeline {
		if sum.TaskID == orig.ID && (len(sum.Scaffolds) != 1 || len(sum.Live) != 1) {
			t.Fatalf("original summary = %+v", sum)
		}
		if sum.TaskID == sc.ID && sum.PartOf != orig.ID {
			t.Fatalf("scaffold summary = %+v", sum)
		}
	}
	if len(rep.Out) != 1 || rep.Out[0].Reason != OutNotRelevant {
		t.Fatalf("out = %+v", rep.Out)
	}
}

func TestReportFlagsAnOriginalWhoseScaffoldsHaveAllPassed(t *testing.T) {
	s := newTestStore(t)
	r := newRecorder(s)
	orig, _ := s.CreateTask("ATM", "original", "", nil, testActor)
	_ = r.Absorb(orig.ID)
	sc, _ := r.Scaffold(orig.ID, "staging")
	_ = r.Pass(sc.ID)

	rep, err := (&Reporter{Store: s}).Report("ATM")
	if err != nil {
		t.Fatalf("Report: %v", err)
	}
	if !findingFor(t, rep, orig.ID, "every scaffold has passed") {
		t.Fatalf("findings = %+v", rep.Findings)
	}
}

// A passed scaffold is a record, not a problem: it gave up its claim on
// purpose and must not show up as a finding.
func TestReportDoesNotFlagAPassedScaffold(t *testing.T) {
	s := newTestStore(t)
	r := newRecorder(s)
	orig, _ := s.CreateTask("ATM", "original", "", nil, testActor)
	_ = r.Absorb(orig.ID)
	sc, _ := r.Scaffold(orig.ID, "staging")
	_ = r.Pass(sc.ID)
	rep, _ := (&Reporter{Store: s}).Report("ATM")
	for _, f := range rep.Findings {
		if f.TaskID == sc.ID {
			t.Fatalf("passed scaffold flagged: %+v", f)
		}
	}
	for _, sum := range rep.Pipeline {
		if sum.TaskID == sc.ID {
			t.Fatalf("passed scaffold still in the pipeline roster: %+v", sum)
		}
	}
}

// Only a hand-assigned label can put the finish socket on a scaffold, and the
// reporter is the surface that catches it.
func TestReportCatchesAHandStampedScaffoldDone(t *testing.T) {
	s := newTestStore(t)
	r := newRecorder(s)
	orig, _ := s.CreateTask("ATM", "original", "", nil, testActor)
	_ = r.Absorb(orig.ID)
	sc, _ := r.Scaffold(orig.ID, "staging")
	_ = s.TaskLabelAdd(sc.ID, "ATM:qa:done", testActor)
	_ = s.TaskLabelRemove(sc.ID, "ATM:qa:testing", testActor)

	rep, _ := (&Reporter{Store: s}).Report("ATM")
	if !findingFor(t, rep, sc.ID, "finish socket belongs to originals only") {
		t.Fatalf("findings = %+v", rep.Findings)
	}
}

func TestReportFlagsMissingScaffoldsAndOrphans(t *testing.T) {
	s := newTestStore(t)
	orphan, _ := s.CreateTask("ATM", "orphan scaffold", "", []string{"ATM:qa:testing"}, testActor)
	_ = s.SetTaskCapabilityMeta(orphan.ID, CapabilityName, `{"v":1,"part_of":"ATM-gone99"}`, testActor)
	ghost, _ := s.CreateTask("ATM", "original", "", []string{"ATM:qa:testing"}, testActor)
	_ = s.SetTaskCapabilityMeta(ghost.ID, CapabilityName, `{"v":1,"scaffolds":["ATM-gone88"]}`, testActor)

	rep, _ := (&Reporter{Store: s}).Report("ATM")
	if !findingFor(t, rep, orphan.ID, "ATM-gone99") {
		t.Fatalf("findings = %+v", rep.Findings)
	}
	if !findingFor(t, rep, ghost.ID, "ATM-gone88") {
		t.Fatalf("findings = %+v", rep.Findings)
	}
}

func TestReportFlagsAnUnparseablePayload(t *testing.T) {
	s := newTestStore(t)
	bad, _ := s.CreateTask("ATM", "bad", "", []string{"ATM:qa:testing"}, testActor)
	_ = s.SetTaskCapabilityMeta(bad.ID, CapabilityName, "not json", testActor)
	rep, _ := (&Reporter{Store: s}).Report("ATM")
	if !findingFor(t, rep, bad.ID, "hand-repair") {
		t.Fatalf("findings = %+v", rep.Findings)
	}
}

func TestReportDoesNotTouchTheLedger(t *testing.T) {
	s := newTestStore(t)
	r := newRecorder(s)
	orig, _ := s.CreateTask("ATM", "original", "", nil, testActor)
	_ = r.Absorb(orig.ID)
	_, _ = r.Scaffold(orig.ID, "staging")
	bad, _ := s.CreateTask("ATM", "bad", "", []string{"ATM:qa:testing"}, testActor)
	_ = s.SetTaskCapabilityMeta(bad.ID, CapabilityName, "not json", testActor)

	before := ledgerDigest(t, s)
	if _, err := (&Reporter{Store: s}).Report("ATM"); err != nil {
		t.Fatalf("Report: %v", err)
	}
	if after := ledgerDigest(t, s); after != before {
		t.Fatal("the reporter appended to the ledger")
	}
}
