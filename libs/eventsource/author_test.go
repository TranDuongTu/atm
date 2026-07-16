package eventsource

import (
	"strings"
	"testing"
	"time"
)

func fixedClock() *Clock {
	t := int64(1752480000000)
	return NewClock(func() int64 { t++; return t })
}

var testAt = time.Date(2026, 7, 14, 9, 12, 3, 0, time.UTC)

func TestNewEventAuthorsParsableEvent(t *testing.T) {
	e, err := NewEvent(fixedClock(), "r_00000000000000000000000001", nil, Draft{
		At:      testAt,
		Actor:   "developer@claude:test",
		Action:  ActionTaskCreated,
		Subject: Subject{Kind: "task"},
		Payload: map[string]any{"alias": "ATM-7f3a2b", "title": "Fix the cache"},
	})
	if err != nil {
		t.Fatal(err)
	}
	reparsed, err := Parse(e.Raw)
	if err != nil {
		t.Fatal(err)
	}
	if reparsed.ID != e.ID {
		t.Errorf("authored event does not round-trip: %s vs %s", reparsed.ID, e.ID)
	}
	if e.Parents == nil || len(e.Parents) != 0 {
		t.Errorf("root event parents = %#v, want []", e.Parents)
	}
	if got, _ := e.PayloadString("title"); got != "Fix the cache" {
		t.Errorf("payload title = %q", got)
	}
}

func TestNewEventIsDeterministic(t *testing.T) {
	mk := func() *Event {
		e, err := NewEvent(fixedClock(), "r_00000000000000000000000001", []string{"sha256:bbb", "sha256:aaa"}, Draft{
			At:      testAt,
			Actor:   "developer@claude:test",
			Action:  ActionTaskTitleChanged,
			Subject: Subject{Kind: "task", ID: "sha256:eee"},
			Payload: map[string]any{"title": "x"},
		})
		if err != nil {
			t.Fatal(err)
		}
		return e
	}
	a, b := mk(), mk()
	if a.ID != b.ID || string(a.Raw) != string(b.Raw) {
		t.Errorf("same inputs, different events:\n%s\n%s", a.Raw, b.Raw)
	}
}

func TestNewEventSortsAndDedupesParents(t *testing.T) {
	e, err := NewEvent(fixedClock(), "r_00000000000000000000000001", []string{"sha256:bbb", "sha256:aaa", "sha256:bbb"}, Draft{
		At:      testAt,
		Actor:   "a",
		Action:  ActionTaskRemoved,
		Subject: Subject{Kind: "task", ID: "sha256:eee"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(e.Parents) != 2 || e.Parents[0] != "sha256:aaa" || e.Parents[1] != "sha256:bbb" {
		t.Errorf("parents = %v", e.Parents)
	}
}

func TestNewEventOmitsEmptyPayload(t *testing.T) {
	e, err := NewEvent(fixedClock(), "r_00000000000000000000000001", nil, Draft{
		At:      testAt,
		Actor:   "a",
		Action:  ActionTaskRemoved,
		Subject: Subject{Kind: "task", ID: "sha256:eee"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(e.Raw), `"payload"`) {
		t.Errorf("empty payload serialized: %s", e.Raw)
	}
}

func TestNewEventRejectsReservedOrEmptyReplica(t *testing.T) {
	for _, replica := range []string{"", ReplicaV1} {
		_, err := NewEvent(fixedClock(), replica, nil, Draft{
			At: testAt, Actor: "a", Action: ActionTaskRemoved, Subject: Subject{Kind: "task", ID: "sha256:eee"},
		})
		if err == nil {
			t.Errorf("replica %q accepted", replica)
		}
	}
}

func TestNewEventTicksClock(t *testing.T) {
	clock := fixedClock()
	e1, err := NewEvent(clock, "r_00000000000000000000000001", nil, Draft{
		At: testAt, Actor: "a", Action: ActionTaskRemoved, Subject: Subject{Kind: "task", ID: "sha256:eee"},
	})
	if err != nil {
		t.Fatal(err)
	}
	e2, err := NewEvent(clock, "r_00000000000000000000000001", []string{e1.ID}, Draft{
		At: testAt, Actor: "a", Action: ActionTaskRestored, Subject: Subject{Kind: "task", ID: "sha256:eee"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if CompareEvents(e1, e2) >= 0 {
		t.Errorf("later event does not sort after earlier: %v vs %v", e1.HLC, e2.HLC)
	}
}
