package store

import (
	"testing"
)

func TestVerifyCleanStore(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.CreateProject("ATM", "x", testActor)
	tk, _ := s.CreateTask("ATM", "t", "", nil, testActor)
	_ = tk
	report, err := s.VerifyProject("ATM")
	if err != nil {
		t.Fatal(err)
	}
	if report.Diverged {
		t.Fatalf("clean store reports Diverged=true: %+v", report)
	}
	if !report.LogOK {
		t.Errorf("LogOK = false on clean log")
	}
	for _, c := range report.Caches {
		if c.Status != "ok" {
			t.Errorf("cache %s:%s status = %q want ok", c.Kind, c.ID, c.Status)
		}
	}
}

func TestVerifyReportsVectorIndexesInfoLevel(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateProject("ATM", "Agent Tasks Management", testActor); err != nil {
		t.Fatal(err)
	}
	if err := s.WriteVectorBatch("ATM", "m", []VectorEntry{{ID: "ATM-0001", Kind: "task", Model: "m", Dim: 2, Vector: []float64{0.1, 0.2}}}, 1); err != nil {
		t.Fatal(err)
	}
	if err := s.AppendInquiry("ATM", "q", []string{"ATM-0001"}); err != nil {
		t.Fatal(err)
	}
	rep, err := s.VerifyProject("ATM")
	if err != nil {
		t.Fatalf("VerifyProject: %v", err)
	}
	if len(rep.VectorIndexes) != 1 || rep.VectorIndexes[0].Model != "m" || rep.VectorIndexes[0].Count != 1 {
		t.Errorf("VectorIndexes = %+v, want one model=m count=1", rep.VectorIndexes)
	}
	if rep.InquiryCount != 1 {
		t.Errorf("InquiryCount = %d, want 1", rep.InquiryCount)
	}
	if rep.Diverged {
		t.Errorf("Diverged=true for info-level vector presence; want false")
	}
}
