package eventsync

import (
	"errors"
	"strings"
	"testing"
	"time"

	"atm/internal/eventsource"
)

const (
	replicaA = "r_aaaaaaaaaaaaaaaaaaaaaaaaaa"
	replicaB = "r_bbbbbbbbbbbbbbbbbbbbbbbbbb"
)

var testAt = time.Date(2026, 7, 14, 9, 12, 3, 0, time.UTC)

func fixedClock() *eventsource.Clock {
	t := int64(1752480000000)
	return eventsource.NewClock(func() int64 { t++; return t })
}

func mustProject(t *testing.T, clock *eventsource.Clock, replica, code string) *eventsource.Event {
	t.Helper()
	ev, _, err := eventsource.NewProjectCreated(clock, replica, nil, eventsource.ProjectCreateDraft{
		Code: code, Name: code, At: testAt, Actor: "developer@claude:test",
	})
	if err != nil {
		t.Fatalf("NewProjectCreated: %v", err)
	}
	return ev
}

func mustTask(t *testing.T, clock *eventsource.Clock, replica string, parents []string, title string) *eventsource.Event {
	t.Helper()
	ev, _, err := eventsource.NewTaskCreated(clock, replica, parents, eventsource.TaskCreateDraft{
		ProjectCode: "PRJ", At: testAt, Actor: "developer@claude:test", Title: title,
	}, nil)
	if err != nil {
		t.Fatalf("NewTaskCreated: %v", err)
	}
	return ev
}

func rawOf(events ...*eventsource.Event) []RawEvent {
	out := make([]RawEvent, len(events))
	for i, e := range events {
		out[i] = RawEvent{ID: e.ID, Raw: e.Raw}
	}
	return out
}

func rawEventIDs(events []RawEvent) []string {
	out := make([]string, len(events))
	for i, e := range events {
		out[i] = e.ID
	}
	return out
}

func TestPlanDisjointSetsDiffBothWays(t *testing.T) {
	clock := fixedClock()
	root := mustProject(t, clock, replicaA, "PRJ")
	e1 := mustTask(t, clock, replicaA, []string{root.ID}, "local task")
	e2 := mustTask(t, clock, replicaB, []string{root.ID}, "remote task")

	local := []*eventsource.Event{root, e1}
	remote := &RemoteSnapshot{Events: rawOf(root, e2)}

	res, err := Plan(local, remote)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if len(res.ToIngest) != 1 || res.ToIngest[0].ID != e2.ID {
		t.Errorf("ToIngest = %v, want [%s]", eventIDs(res.ToIngest), e2.ID)
	}
	if len(res.ToPublish) != 1 || res.ToPublish[0].ID != e1.ID {
		t.Errorf("ToPublish = %v, want [%s]", rawEventIDs(res.ToPublish), e1.ID)
	}
	if res.RemoteAbsent || res.LocalAbsent {
		t.Errorf("RemoteAbsent=%v LocalAbsent=%v, want false,false", res.RemoteAbsent, res.LocalAbsent)
	}
}

func TestPlanIdenticalSetsNoOp(t *testing.T) {
	clock := fixedClock()
	root := mustProject(t, clock, replicaA, "PRJ")
	e1 := mustTask(t, clock, replicaA, []string{root.ID}, "task")

	local := []*eventsource.Event{root, e1}
	remote := &RemoteSnapshot{Events: rawOf(root, e1)}

	res, err := Plan(local, remote)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if len(res.ToIngest) != 0 || len(res.ToPublish) != 0 {
		t.Errorf("expected no-op, got ToIngest=%v ToPublish=%v", eventIDs(res.ToIngest), rawEventIDs(res.ToPublish))
	}
}

func TestPlanRemoteAbsentPublishesAll(t *testing.T) {
	clock := fixedClock()
	root := mustProject(t, clock, replicaA, "PRJ")
	e1 := mustTask(t, clock, replicaA, []string{root.ID}, "task")

	local := []*eventsource.Event{root, e1}
	res, err := Plan(local, &RemoteSnapshot{Absent: true})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if !res.RemoteAbsent {
		t.Errorf("RemoteAbsent = false, want true")
	}
	if len(res.ToPublish) != 2 {
		t.Errorf("ToPublish = %v, want both local events", rawEventIDs(res.ToPublish))
	}
	if len(res.ToIngest) != 0 {
		t.Errorf("ToIngest = %v, want empty", eventIDs(res.ToIngest))
	}
}

func TestPlanLocalAbsentIngestsAll(t *testing.T) {
	clock := fixedClock()
	root := mustProject(t, clock, replicaA, "PRJ")
	e1 := mustTask(t, clock, replicaA, []string{root.ID}, "task")

	remote := &RemoteSnapshot{Events: rawOf(root, e1)}
	res, err := Plan(nil, remote)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if !res.LocalAbsent {
		t.Errorf("LocalAbsent = false, want true")
	}
	if len(res.ToIngest) != 2 || res.ToIngest[0].ID != root.ID || res.ToIngest[1].ID != e1.ID {
		t.Errorf("ToIngest = %v, want topo [root, e1]", eventIDs(res.ToIngest))
	}
	if len(res.ToPublish) != 0 {
		t.Errorf("ToPublish = %v, want empty", rawEventIDs(res.ToPublish))
	}
}

func TestPlanRootMismatchRefused(t *testing.T) {
	clock := fixedClock()
	rootA := mustProject(t, clock, replicaA, "AAA")
	rootB := mustProject(t, clock, replicaB, "BBB")

	local := []*eventsource.Event{rootA}
	remote := &RemoteSnapshot{Events: rawOf(rootB)}

	_, err := Plan(local, remote)
	if !errors.Is(err, ErrRootMismatch) {
		t.Fatalf("err = %v, want ErrRootMismatch", err)
	}
	if !strings.Contains(err.Error(), rootA.ID) || !strings.Contains(err.Error(), rootB.ID) {
		t.Errorf("error %q does not name both roots", err)
	}
}

func TestPlanRootMismatchWithinOneSideRefused(t *testing.T) {
	clock := fixedClock()
	rootA := mustProject(t, clock, replicaA, "AAA")
	rootB := mustProject(t, clock, replicaA, "BBB")

	local := []*eventsource.Event{rootA, rootB}

	_, err := Plan(local, &RemoteSnapshot{Absent: true})
	if !errors.Is(err, ErrRootMismatch) {
		t.Fatalf("err = %v, want ErrRootMismatch", err)
	}
}

func TestPlanRemoteMissingParentRejected(t *testing.T) {
	clock := fixedClock()
	root := mustProject(t, clock, replicaA, "PRJ")
	orphan := mustTask(t, clock, replicaB, []string{"sha256:doesnotexist"}, "orphan")

	local := []*eventsource.Event{root}
	remote := &RemoteSnapshot{Events: rawOf(orphan)}

	_, err := Plan(local, remote)
	if err == nil {
		t.Fatal("Plan: want error for missing parent, got nil")
	}
	if !strings.Contains(err.Error(), orphan.ID) {
		t.Errorf("error %q does not name the offending event", err)
	}
}

func TestPlanRemoteBadLineRejected(t *testing.T) {
	remote := &RemoteSnapshot{Events: []RawEvent{{Raw: []byte("{not json")}}}

	_, err := Plan(nil, remote)
	if err == nil {
		t.Fatal("Plan: want error for unparseable remote line, got nil")
	}
}

func TestPlanIngestOrderTopological(t *testing.T) {
	clock := fixedClock()
	root := mustProject(t, clock, replicaA, "PRJ")
	parent := mustTask(t, clock, replicaA, []string{root.ID}, "parent")
	child := mustTask(t, clock, replicaA, []string{parent.ID}, "child")

	// The remote lists the child before its parent; Plan must still
	// ingest parents-first regardless of wire order.
	remote := &RemoteSnapshot{Events: rawOf(root, child, parent)}

	res, err := Plan(nil, remote)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if len(res.ToIngest) != 3 {
		t.Fatalf("ToIngest = %v, want 3 events", eventIDs(res.ToIngest))
	}
	if res.ToIngest[0].ID != root.ID || res.ToIngest[1].ID != parent.ID || res.ToIngest[2].ID != child.ID {
		t.Errorf("ToIngest = %v, want [root, parent, child]", eventIDs(res.ToIngest))
	}
}

func TestSetDigestOrderIndependent(t *testing.T) {
	set := []string{"sha256:ccc", "sha256:aaa", "sha256:bbb"}
	shuffled := []string{"sha256:bbb", "sha256:ccc", "sha256:aaa"}

	if SetDigest(set) != SetDigest(shuffled) {
		t.Errorf("SetDigest not order-independent: %s vs %s", SetDigest(set), SetDigest(shuffled))
	}
	if !strings.HasPrefix(SetDigest(set), "sha256:") {
		t.Errorf("SetDigest = %q, want sha256: prefix", SetDigest(set))
	}
}
