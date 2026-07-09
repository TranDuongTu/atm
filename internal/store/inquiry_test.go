package store

import "testing"

func TestAppendInquiryAndRead(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateProject("ATM", "Agent Tasks Management", testActor); err != nil {
		t.Fatal(err)
	}
	if err := s.AppendInquiry("ATM", "label conflicts", []string{"ATM-0001", "ATM-0002"}); err != nil {
		t.Fatalf("AppendInquiry: %v", err)
	}
	if err := s.AppendInquiry("ATM", "audit log", []string{"ATM-0003"}); err != nil {
		t.Fatal(err)
	}
	got, err := s.ReadInquiries("ATM")
	if err != nil {
		t.Fatalf("ReadInquiries: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d entries, want 2", len(got))
	}
	if got[0].Query != "label conflicts" || len(got[0].CitedIDs) != 2 {
		t.Errorf("entry 0 = %+v", got[0])
	}
}

func TestReadInquiriesMissing(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateProject("ATM", "Agent Tasks Management", testActor); err != nil {
		t.Fatal(err)
	}
	got, err := s.ReadInquiries("ATM")
	if err != nil {
		t.Fatalf("ReadInquiries missing: %v", err)
	}
	if got != nil {
		t.Errorf("got %v, want nil for missing inquiry log", got)
	}
}
