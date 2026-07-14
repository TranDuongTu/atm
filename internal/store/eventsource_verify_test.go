package store

import (
	"os"
	"sync"
	"testing"
)

func TestVerifyProjectReportsV2Format(t *testing.T) {
	s := testStore(t)
	_, _ = s.CreateProject("ATM", "x", "admin@cli:unset")
	_, _ = s.CreateTask("ATM", "t", "", nil, "admin@cli:unset")
	_, _ = s.UpgradeProjectToV2("ATM")
	r, err := s.VerifyProject("ATM")
	if err != nil {
		t.Fatal(err)
	}
	if r.Format != StoreFormatV2 {
		t.Fatalf("Format = %q, want v2", r.Format)
	}
	if r.V2Events == 0 {
		t.Fatalf("V2Events = %d, want > 0", r.V2Events)
	}
}

func TestRebuildUsesV2ForV2ActiveProject(t *testing.T) {
	s := testStore(t)
	_, _ = s.CreateProject("ATM", "x", "admin@cli:unset")
	tk, _ := s.CreateTask("ATM", "t", "", nil, "admin@cli:unset")
	_, _ = s.UpgradeProjectToV2("ATM")
	if err := os.Remove(s.cachePath()); err != nil {
		t.Fatal(err)
	}
	s.cacheOnce = sync.Once{}
	s.cacheDBConn = nil
	rep, err := s.Rebuild()
	if err != nil {
		t.Fatal(err)
	}
	if rep.Tasks == 0 {
		t.Fatalf("rebuild report = %#v", rep)
	}
	if _, err := s.GetTask(tk.ID); err != nil {
		t.Fatalf("GetTask after v2 rebuild: %v", err)
	}
}

func TestVerifyProjectV2KeepsVectorAndInquiryReports(t *testing.T) {
	s := testStore(t)
	_, _ = s.CreateProject("ATM", "x", "admin@cli:unset")
	_, _ = s.CreateTask("ATM", "t", "", nil, "admin@cli:unset")
	_, _ = s.UpgradeProjectToV2("ATM")
	// Written AFTER cutover: the cutover itself wipes v1-keyed indexes.
	if err := s.WriteVectorBatch("ATM", "test-model", []VectorEntry{{ID: "ATM-0001", Kind: "task", Model: "test-model", Dim: 2, Vector: []float64{1, 0}, TextHash: "sha256:x", LogSeq: 1}}, 3); err != nil {
		t.Fatal(err)
	}
	r, err := s.VerifyProject("ATM")
	if err != nil {
		t.Fatal(err)
	}
	if len(r.VectorIndexes) != 1 || r.VectorIndexes[0].Model != "test-model" {
		t.Fatalf("VectorIndexes = %#v, want the test-model index reported for a v2 project", r.VectorIndexes)
	}
}
