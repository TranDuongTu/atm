package store

import (
	"strings"
	"testing"

	"atm/internal/core"
)

func TestAppendAndReadAskTurns(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateProject("ATM", "Agent Tasks Management", testActor); err != nil {
		t.Fatal(err)
	}
	if err := s.AppendAskTurn("ATM", "sess-1", core.AskTurn{Question: "what blocks release?", Answer: "ATM-1 does"}); err != nil {
		t.Fatalf("AppendAskTurn: %v", err)
	}
	if err := s.AppendAskTurn("ATM", "sess-1", core.AskTurn{Question: "and after that?", Answer: "ATM-2"}); err != nil {
		t.Fatalf("AppendAskTurn: %v", err)
	}
	turns, err := s.ReadAskTurns("ATM", "sess-1")
	if err != nil {
		t.Fatalf("ReadAskTurns: %v", err)
	}
	if len(turns) != 2 || turns[0].Question != "what blocks release?" || turns[1].Answer != "ATM-2" {
		t.Fatalf("turns = %+v, want both in order", turns)
	}
	if turns[0].At == "" {
		t.Error("want a timestamp stamped on append")
	}
}

func TestReadAskTurnsForUnknownSessionIsEmptyNotError(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateProject("ATM", "Agent Tasks Management", testActor); err != nil {
		t.Fatal(err)
	}
	turns, err := s.ReadAskTurns("ATM", "never-used")
	if err != nil {
		t.Fatalf("ReadAskTurns: %v", err)
	}
	if len(turns) != 0 {
		t.Errorf("turns = %+v, want none — a first turn must not have to special-case a missing file", turns)
	}
}

// The id becomes a path component. Reject rather than sanitize: an agent that
// passes a bad id should learn it, not silently get a different session.
func TestAskSessionIDRejectsTraversal(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateProject("ATM", "Agent Tasks Management", testActor); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"../../../etc/passwd", "..", ".", "", "has/slash", strings.Repeat("x", 65)} {
		if err := s.AppendAskTurn("ATM", bad, core.AskTurn{Question: "q", Answer: "a"}); err == nil {
			t.Errorf("AppendAskTurn(%q) = nil, want a usage error", bad)
		}
		if _, err := s.ReadAskTurns("ATM", bad); err == nil {
			t.Errorf("ReadAskTurns(%q) = nil, want a usage error", bad)
		}
	}
}
