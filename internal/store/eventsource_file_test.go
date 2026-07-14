package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"atm/internal/eventsource"
)

func testV2Event(t *testing.T, action string) *eventsource.Event {
	t.Helper()
	clock := eventsource.NewClock(func() int64 { return 1000 })
	ev, err := eventsource.NewEvent(clock, "r_0123456789abcdefghjkmnpqrs", nil, eventsource.Draft{
		Actor:   "admin@cli:unset",
		Action:  action,
		Subject: eventsource.Subject{Kind: "project", Code: "ATM"},
		Payload: map[string]any{"alias": "ATM", "name": "Agent Tasks Management"},
	})
	if err != nil {
		t.Fatal(err)
	}
	return ev
}

func TestAppendAndReadV2File(t *testing.T) {
	s := testStore(t)
	ev := testV2Event(t, "project.created")
	if err := s.WithLock("ATM", func() error {
		return s.appendV2EventLineLocked("ATM", ev.Raw)
	}); err != nil {
		t.Fatal(err)
	}
	snap, err := s.readV2File("ATM", false)
	if err != nil {
		t.Fatal(err)
	}
	if snap.EventCount != 1 {
		t.Fatalf("EventCount = %d, want 1", snap.EventCount)
	}
	if snap.Events[0].ID != ev.ID {
		t.Fatalf("event id = %s, want %s", snap.Events[0].ID, ev.ID)
	}
}

func TestReadV2FileTruncatesPartialTailOnlyWhenRepairRequested(t *testing.T) {
	s := testStore(t)
	ev := testV2Event(t, "project.created")
	if err := os.MkdirAll(filepath.Dir(s.eventsV2Path("ATM")), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.eventsV2Path("ATM"), append(append([]byte{}, ev.Raw...), []byte("\n{\"partial\"")...), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := s.readV2File("ATM", false); err == nil {
		t.Fatal("expected integrity error without repairTail")
	}
	snap, err := s.readV2File("ATM", true)
	if err != nil {
		t.Fatal(err)
	}
	if snap.TruncatedBytes == 0 {
		t.Fatal("expected truncated byte count")
	}
	raw, err := os.ReadFile(s.eventsV2Path("ATM"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "partial") {
		t.Fatalf("partial tail not truncated: %s", raw)
	}
}

func TestReadV2FileRejectsMalformedCompleteLine(t *testing.T) {
	s := testStore(t)
	if err := os.MkdirAll(filepath.Dir(s.eventsV2Path("ATM")), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.eventsV2Path("ATM"), []byte("{not-json}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := s.readV2File("ATM", true); err == nil {
		t.Fatal("expected malformed complete line to fail")
	}
}
