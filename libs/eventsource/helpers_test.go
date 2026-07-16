package eventsource

import (
	"testing"
	"time"
)

// testClock returns a deterministic Clock advancing one millisecond per
// reading, starting just above start.
func testClock(start int64) *Clock {
	t := start
	return NewClock(func() int64 { t++; return t })
}

// testEvent authors an event for scenario tests, failing the test on error.
func testEvent(t *testing.T, clock *Clock, replica string, parents []string, action string, subj Subject, payload map[string]any) *Event {
	t.Helper()
	e, err := NewEvent(clock, replica, parents, Draft{
		At:      time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC),
		Actor:   "developer@claude:test",
		Action:  action,
		Subject: subj,
		Payload: payload,
	})
	if err != nil {
		t.Fatal(err)
	}
	return e
}

const (
	replicaA = "r_aaaaaaaaaaaaaaaaaaaaaaaaaa"
	replicaB = "r_bbbbbbbbbbbbbbbbbbbbbbbbbb"
)
