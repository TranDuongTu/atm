package store

import (
	"bytes"
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

// TestReadV2FileTreatsCompleteButUnterminatedTailAsUncommitted locks in the
// spec's commit-point rule (L3-7): the commit point is the trailing
// newline, not JSON validity. A tail that is a genuinely complete,
// parseable event — but has no terminating '\n' — must still be treated as
// an uncommitted crash remnant: excluded from the snapshot, and truncated
// away when repair is requested. A bufio.Scanner-based repair would accept
// this tail as an ordinary final line (Scanner does not require a trailing
// newline), silently committing an event that was never fsynced as
// complete; only a byte-based split on the last '\n' can tell the
// difference. Both events are authored via eventsource.NewEvent so the tail
// is real, canonical, parent-valid JSON — not a hand-rolled string that
// merely looks like one.
func TestReadV2FileTreatsCompleteButUnterminatedTailAsUncommitted(t *testing.T) {
	s := testStore(t)
	clock := eventsource.NewClock(func() int64 { return 1000 })
	replica := "r_0123456789abcdefghjkmnpqrs"

	committed, err := eventsource.NewEvent(clock, replica, nil, eventsource.Draft{
		Actor:   "admin@cli:unset",
		Action:  "project.created",
		Subject: eventsource.Subject{Kind: "project", Code: "ATM"},
		Payload: map[string]any{"alias": "ATM", "name": "Agent Tasks Management"},
	})
	if err != nil {
		t.Fatal(err)
	}
	tail, err := eventsource.NewEvent(clock, replica, []string{committed.ID}, eventsource.Draft{
		Actor:   "admin@cli:unset",
		Action:  "project.renamed",
		Subject: eventsource.Subject{Kind: "project", Code: "ATM"},
		Payload: map[string]any{"name": "Renamed While Crashing"},
	})
	if err != nil {
		t.Fatal(err)
	}
	// Sanity: the tail is, on its own, a fully valid parseable event — the
	// whole point is that only its missing trailing newline disqualifies it.
	if _, err := eventsource.Parse(tail.Raw); err != nil {
		t.Fatalf("tail event does not parse on its own, test is meaningless: %v", err)
	}

	path := s.eventsV2Path("ATM")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	// committed.Raw + "\n" + tail.Raw, deliberately with NO trailing
	// newline: tail is complete, valid JSON, but uncommitted.
	var buf []byte
	buf = append(buf, committed.Raw...)
	buf = append(buf, '\n')
	buf = append(buf, tail.Raw...)
	if err := os.WriteFile(path, buf, 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := s.readV2File("ATM", false); err == nil {
		t.Fatal("expected integrity error for unterminated tail without repairTail")
	}

	snap, err := s.readV2File("ATM", true)
	if err != nil {
		t.Fatal(err)
	}
	if snap.EventCount != 1 {
		t.Fatalf("EventCount = %d, want 1 (tail must not be counted as committed)", snap.EventCount)
	}
	if snap.Events[0].ID != committed.ID {
		t.Fatalf("snapshot event id = %s, want committed event %s", snap.Events[0].ID, committed.ID)
	}
	for _, ev := range snap.Events {
		if ev.ID == tail.ID {
			t.Fatalf("tail event %s was accepted as committed", tail.ID)
		}
	}
	if snap.TruncatedBytes != len(tail.Raw) {
		t.Fatalf("TruncatedBytes = %d, want %d (exact tail length)", snap.TruncatedBytes, len(tail.Raw))
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := append(append([]byte{}, committed.Raw...), '\n')
	if !bytes.Equal(raw, want) {
		t.Fatalf("on-disk file after repair = %q, want %q (tail bytes must be gone)", raw, want)
	}
}

// TestReadV2FileTreatsSingleUnterminatedCompleteEventAsUncommitted covers the
// degenerate shape of the same rule: a file holding exactly one complete,
// valid event with no trailing newline has zero committed events, not one.
func TestReadV2FileTreatsSingleUnterminatedCompleteEventAsUncommitted(t *testing.T) {
	s := testStore(t)
	ev := testV2Event(t, "project.created")

	path := s.eventsV2Path("ATM")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	// No trailing newline: the entire file is an uncommitted complete event.
	if err := os.WriteFile(path, ev.Raw, 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := s.readV2File("ATM", false); err == nil {
		t.Fatal("expected integrity error for unterminated single event without repairTail")
	}

	snap, err := s.readV2File("ATM", true)
	if err != nil {
		t.Fatal(err)
	}
	if snap.EventCount != 0 {
		t.Fatalf("EventCount = %d, want 0 (sole event was never newline-committed)", snap.EventCount)
	}
	if snap.TruncatedBytes != len(ev.Raw) {
		t.Fatalf("TruncatedBytes = %d, want %d", snap.TruncatedBytes, len(ev.Raw))
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) != 0 {
		t.Fatalf("on-disk file after repair = %q, want empty", raw)
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
