package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAppendInquiryAndRead(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateProject("ATM", "Agent Tasks Management", testActor); err != nil {
		t.Fatal(err)
	}
	if err := s.AppendInquiry("ATM", "label conflicts", []string{"ATM-0001", "ATM-0002"}, []string{"ATM-0001", "ATM-0002"}, nil); err != nil {
		t.Fatalf("AppendInquiry: %v", err)
	}
	if err := s.AppendInquiry("ATM", "audit log", []string{"ATM-0003"}, []string{"ATM-0003"}, nil); err != nil {
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

func TestAppendInquiryRecordsReturnedAndCited(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateProject("ATM", "Agent Tasks Management", testActor); err != nil {
		t.Fatal(err)
	}
	if err := s.AppendInquiry("ATM", "label conflicts",
		[]string{"ATM-0001", "ATM-0002", "ATM-0003"}, []string{"ATM-0002"}, nil); err != nil {
		t.Fatalf("AppendInquiry: %v", err)
	}
	got, err := s.ReadInquiries("ATM")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d entries, want 1", len(got))
	}
	// recall@k needs the denominator: what came back, not only what was cited.
	if len(got[0].ReturnedIDs) != 3 {
		t.Errorf("ReturnedIDs = %v, want all three", got[0].ReturnedIDs)
	}
	if len(got[0].CitedIDs) != 1 || got[0].CitedIDs[0] != "ATM-0002" {
		t.Errorf("CitedIDs = %v, want [ATM-0002]", got[0].CitedIDs)
	}
}

// The log is append-only and already has lines on disk without the field.
func TestReadInquiriesToleratesEntriesWithoutReturnedIDs(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateProject("ATM", "Agent Tasks Management", testActor); err != nil {
		t.Fatal(err)
	}
	path := s.inquiryLogPath("ATM")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"query":"old","cited_ids":["ATM-1"],"at":"2026-01-01T00:00:00Z"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := s.ReadInquiries("ATM")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ReturnedIDs != nil {
		t.Errorf("got %+v, want the old line read with a nil ReturnedIDs", got)
	}
}

// An empty returned set must survive a round trip as EMPTY, not as absent.
// Absent is reserved for lines written before the field existed, and eval has
// to tell "searched, found nothing" apart from "we do not know".
func TestAppendInquiryDistinguishesEmptyReturnedFromAbsent(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateProject("ATM", "Agent Tasks Management", testActor); err != nil {
		t.Fatal(err)
	}
	if err := s.AppendInquiry("ATM", "nothing matches this", []string{}, []string{}, nil); err != nil {
		t.Fatalf("AppendInquiry: %v", err)
	}
	got, err := s.ReadInquiries("ATM")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d entries, want 1", len(got))
	}
	if got[0].ReturnedIDs == nil {
		t.Error("ReturnedIDs is nil after logging an empty returned set; it must round-trip as empty-but-present, because nil is how a pre-field line reads")
	}
	if len(got[0].ReturnedIDs) != 0 {
		t.Errorf("ReturnedIDs = %v, want empty", got[0].ReturnedIDs)
	}
	// The raw line must carry the key, since that is what makes the two states
	// distinguishable to a reader that is not this struct.
	raw, err := os.ReadFile(s.inquiryLogPath("ATM"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"returned_ids":[]`) {
		t.Errorf("log line = %s, want an explicit empty returned_ids", raw)
	}
}

// A spotlight click-through is a human judgment; a citation is a model's.
// Sub-task 6 (ATM-028a8d) weighs them differently, so the log has to keep them
// apart -- folding an opened ID into CitedIDs would average the strongest
// relevance signal in the corpus into the weaker one.
func TestAppendInquiryRecordsOpenedSeparatelyFromCited(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateProject("ATM", "Agent Tasks Management", testActor); err != nil {
		t.Fatal(err)
	}
	if err := s.AppendInquiry("ATM", "label conflicts",
		[]string{"ATM-0001", "ATM-0002"}, []string{"ATM-0001"}, []string{"ATM-0002"}); err != nil {
		t.Fatalf("AppendInquiry: %v", err)
	}
	got, err := s.ReadInquiries("ATM")
	if err != nil {
		t.Fatalf("ReadInquiries: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d entries, want 1", len(got))
	}
	if len(got[0].OpenedIDs) != 1 || got[0].OpenedIDs[0] != "ATM-0002" {
		t.Errorf("OpenedIDs = %v, want [ATM-0002]", got[0].OpenedIDs)
	}
	if len(got[0].CitedIDs) != 1 || got[0].CitedIDs[0] != "ATM-0001" {
		t.Errorf("CitedIDs = %v, want [ATM-0001] -- opened must not leak into cited", got[0].CitedIDs)
	}
}

// The same reasoning ReturnedIDs documents on itself: a missing key is how a
// line written before the field existed looks, so "opened nothing" and "we do
// not know what was opened" must not serialise identically.
func TestAppendInquiryOpenedIDsSerialisesEmptyNotAbsent(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateProject("ATM", "Agent Tasks Management", testActor); err != nil {
		t.Fatal(err)
	}
	if err := s.AppendInquiry("ATM", "q", []string{"ATM-0001"}, nil, nil); err != nil {
		t.Fatalf("AppendInquiry: %v", err)
	}
	b, err := os.ReadFile(s.inquiryLogPath("ATM"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	// The literal bracket, not just the key: "opened_ids":null unmarshals to
	// the same nil slice an absent key does, which would erase the very
	// distinction this field exists to preserve.
	if !strings.Contains(string(b), `"opened_ids":[]`) {
		t.Errorf("opened_ids must serialise as [], not null or absent:\n%s", b)
	}
	// nil citedIDs goes through the same normalization; pin it so a future
	// refactor of AppendInquiry cannot quietly reintroduce the null case here.
	if !strings.Contains(string(b), `"cited_ids":[]`) {
		t.Errorf("cited_ids must serialise as [], not null, when nil is passed:\n%s", b)
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
