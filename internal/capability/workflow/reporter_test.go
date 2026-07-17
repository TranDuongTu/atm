package workflow

import (
	"testing"

	"atm/internal/store"
)

func TestReporterStatusReturnsValue(t *testing.T) {
	s := newTestStore(t)
	tk, err := s.CreateTask("ATM", "t", "", []string{"ATM:status:in-progress"}, "admin@cli:unset")
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	r := &Reporter{Store: s}
	got, err := r.Status(tk.ID)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if got != StatusInProgress {
		t.Errorf("Status = %q, want %q", got, StatusInProgress)
	}
}

func TestReporterStatusUntriagedReturnsEmpty(t *testing.T) {
	s := newTestStore(t)
	tk, _ := s.CreateTask("ATM", "t", "", nil, "admin@cli:unset")
	r := &Reporter{Store: s}
	got, err := r.Status(tk.ID)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if got != "" {
		t.Errorf("Status = %q, want \"\" (untriaged)", got)
	}
}

func TestReporterStatusOnlyNonStatusReturnsEmpty(t *testing.T) {
	s := newTestStore(t)
	tk, _ := s.CreateTask("ATM", "t", "", []string{"ATM:priority:high"}, "admin@cli:unset")
	r := &Reporter{Store: s}
	got, _ := r.Status(tk.ID)
	if got != "" {
		t.Errorf("Status = %q, want \"\" (only non-status label)", got)
	}
}

func TestReporterStatusUnknownTask(t *testing.T) {
	s := newTestStore(t)
	r := &Reporter{Store: s}
	if _, err := r.Status("ATM-deadbeef"); err == nil {
		t.Error("expected error for unknown task id")
	}
}

func TestReporterStatusIsPure(t *testing.T) {
	// Purity: the project log's event count must not advance when the
	// reporter runs. LastLogSeq is the store's staleness probe (see
	// internal/store/log.go) and is the cleanest byte-stable check.
	s := newTestStore(t)
	tk, _ := s.CreateTask("ATM", "t", "", []string{"ATM:status:open"}, "admin@cli:unset")
	code, _, _ := store.ParseTaskID(tk.ID)
	before, err := s.LastLogSeq(code)
	if err != nil {
		t.Fatalf("LastLogSeq before: %v", err)
	}
	r := &Reporter{Store: s}
	if _, err := r.Status(tk.ID); err != nil {
		t.Fatalf("Status: %v", err)
	}
	after, err := s.LastLogSeq(code)
	if err != nil {
		t.Fatalf("LastLogSeq after: %v", err)
	}
	if before != after {
		t.Fatalf("reporter advanced log seq %d -> %d — reporter must be pure", before, after)
	}
}
