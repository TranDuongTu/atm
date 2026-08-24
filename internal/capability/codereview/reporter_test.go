package codereview

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
	inbox, _ := s.CreateTask("ATM", "no PR yet", "", nil, testActor)
	claimed, _ := s.CreateTask("ATM", "under review", "", nil, testActor)
	_ = r.Absorb(claimed.ID, "#142")
	_ = r.Begin(claimed.ID)
	evicted, _ := s.CreateTask("ATM", "declined", "", nil, testActor)
	_ = r.Evict(evicted.ID, OutNotWarranted)

	rep, err := (&Reporter{Store: s}).Report("ATM")
	if err != nil {
		t.Fatalf("Report: %v", err)
	}
	if len(rep.Inbox) != 1 || rep.Inbox[0] != inbox.ID {
		t.Fatalf("inbox = %v, want [%s]", rep.Inbox, inbox.ID)
	}
	if len(rep.Pipeline) != 1 || rep.Pipeline[0].State != StateReviewing || rep.Pipeline[0].PR != "#142" {
		t.Fatalf("pipeline = %+v", rep.Pipeline)
	}
	if len(rep.Out) != 1 || rep.Out[0].Reason != OutNotWarranted {
		t.Fatalf("out = %+v", rep.Out)
	}
	if got := rep.ByState()[StateReviewing]; len(got) != 1 || got[0] != claimed.ID {
		t.Fatalf("ByState = %v", rep.ByState())
	}
}

// absorb cannot produce a claim with no PR, so the reporter surfaces it as
// the hand-assignment it must be.
func TestReportFlagsAClaimWithNoPR(t *testing.T) {
	s := newTestStore(t)
	bare, _ := s.CreateTask("ATM", "bare", "", []string{"ATM:codereview:scheduled"}, testActor)
	rep, _ := (&Reporter{Store: s}).Report("ATM")
	if !findingFor(t, rep, bare.ID, "no pull request recorded") {
		t.Fatalf("findings = %+v", rep.Findings)
	}
}

func TestReportFlagsDriftAndUnparseablePayloads(t *testing.T) {
	s := newTestStore(t)
	drift, _ := s.CreateTask("ATM", "drift", "", []string{"ATM:codereview:approved"}, testActor)
	bad, _ := s.CreateTask("ATM", "bad", "", []string{"ATM:codereview:scheduled"}, testActor)
	_ = s.SetTaskCapabilityMeta(bad.ID, CapabilityName, "not json", testActor)
	rep, _ := (&Reporter{Store: s}).Report("ATM")
	if !findingFor(t, rep, drift.ID, "codereview:approved") {
		t.Fatalf("findings = %+v", rep.Findings)
	}
	if !findingFor(t, rep, bad.ID, "hand-repair") {
		t.Fatalf("findings = %+v", rep.Findings)
	}
}

func TestReportDoesNotTouchTheLedger(t *testing.T) {
	s := newTestStore(t)
	r := newRecorder(s)
	tk, _ := s.CreateTask("ATM", "work", "", nil, testActor)
	_ = r.Absorb(tk.ID, "#142")
	bad, _ := s.CreateTask("ATM", "bad", "", []string{"ATM:codereview:scheduled"}, testActor)
	_ = s.SetTaskCapabilityMeta(bad.ID, CapabilityName, "not json", testActor)

	before := ledgerDigest(t, s)
	if _, err := (&Reporter{Store: s}).Report("ATM"); err != nil {
		t.Fatalf("Report: %v", err)
	}
	if after := ledgerDigest(t, s); after != before {
		t.Fatal("the reporter appended to the ledger")
	}
}
