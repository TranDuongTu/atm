package eventsync

import (
	"context"
	"errors"
	"testing"

	"atm/libs/eventsource"
)

// fakeStore is a map-backed LocalStore that records whether its mutating
// hooks fired, so tests can assert dry-run and push-only paths touch
// nothing they shouldn't.
type fakeStore struct {
	events          map[string][]*eventsource.Event
	newlyContested  int
	ingestCalled    bool
	bootstrapCalled bool
}

func (s *fakeStore) SyncSnapshot(project string) ([]*eventsource.Event, bool, error) {
	ev, ok := s.events[project]
	return ev, !ok, nil // second value is "absent": true when the project is missing
}

func (s *fakeStore) SyncIngest(project string, incoming []*eventsource.Event) (int, int, error) {
	s.ingestCalled = true
	s.events[project] = append(s.events[project], incoming...)
	return len(incoming), s.newlyContested, nil
}

func (s *fakeStore) SyncBootstrap(project string, incoming []*eventsource.Event) error {
	s.bootstrapCalled = true
	s.events[project] = incoming
	return nil
}

// fakeTarget is a plain (non-Narrowing) SyncTarget that records whether
// Fetch and Publish were called and captures what was published.
type fakeTarget struct {
	snap       *RemoteSnapshot
	published  []RawEvent
	publishErr error
	fetched    bool
}

func (f *fakeTarget) Fetch(ctx context.Context, project string) (*RemoteSnapshot, error) {
	f.fetched = true
	return f.snap, nil
}

func (f *fakeTarget) Publish(ctx context.Context, project string, missing []RawEvent, base *RemoteSnapshot) error {
	if f.publishErr != nil {
		return f.publishErr
	}
	f.published = append(f.published, missing...)
	return nil
}

// narrowFakeTarget adds a Narrowing implementation on top of fakeTarget so
// the digest short-circuit path can be exercised.
type narrowFakeTarget struct {
	fakeTarget
	digest string
	heads  []string
}

func (f *narrowFakeTarget) Frontier(ctx context.Context, project string) (string, []string, error) {
	return f.digest, f.heads, nil
}

func (f *narrowFakeTarget) FetchSince(ctx context.Context, project string, haves []string) ([]RawEvent, error) {
	return nil, nil
}

func TestSyncBidirectional(t *testing.T) {
	clock := fixedClock()
	root := mustProject(t, clock, replicaA, "PRJ")
	e1 := mustTask(t, clock, replicaA, []string{root.ID}, "local task")
	e2 := mustTask(t, clock, replicaB, []string{root.ID}, "remote task")

	store := &fakeStore{events: map[string][]*eventsource.Event{"PRJ": {root, e1}}}
	target := &fakeTarget{snap: &RemoteSnapshot{Events: rawOf(root, e2)}}

	rep, err := Sync(context.Background(), store, target, "PRJ", Options{})
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if rep.Pulled != 1 || rep.Pushed != 1 {
		t.Errorf("Pulled=%d Pushed=%d, want 1,1", rep.Pulled, rep.Pushed)
	}
	// Converged: local now holds all three events; remote received e1.
	if got := len(store.events["PRJ"]); got != 3 {
		t.Errorf("local now has %d events, want 3", got)
	}
	if len(target.published) != 1 || target.published[0].ID != e1.ID {
		t.Errorf("published = %v, want [%s]", rawEventIDs(target.published), e1.ID)
	}
}

func TestSyncPullOnlySkipsPublish(t *testing.T) {
	clock := fixedClock()
	root := mustProject(t, clock, replicaA, "PRJ")
	e1 := mustTask(t, clock, replicaA, []string{root.ID}, "local task")
	e2 := mustTask(t, clock, replicaB, []string{root.ID}, "remote task")

	store := &fakeStore{events: map[string][]*eventsource.Event{"PRJ": {root, e1}}}
	target := &fakeTarget{snap: &RemoteSnapshot{Events: rawOf(root, e2)}}

	rep, err := Sync(context.Background(), store, target, "PRJ", Options{Pull: true})
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if rep.Pulled != 1 || rep.Pushed != 0 {
		t.Errorf("Pulled=%d Pushed=%d, want 1,0", rep.Pulled, rep.Pushed)
	}
	if len(target.published) != 0 {
		t.Errorf("published = %v, want none", rawEventIDs(target.published))
	}
}

func TestSyncPushOnlySkipsIngest(t *testing.T) {
	clock := fixedClock()
	root := mustProject(t, clock, replicaA, "PRJ")
	e1 := mustTask(t, clock, replicaA, []string{root.ID}, "local task")
	e2 := mustTask(t, clock, replicaB, []string{root.ID}, "remote task")

	store := &fakeStore{events: map[string][]*eventsource.Event{"PRJ": {root, e1}}}
	target := &fakeTarget{snap: &RemoteSnapshot{Events: rawOf(root, e2)}}

	rep, err := Sync(context.Background(), store, target, "PRJ", Options{Push: true})
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if rep.Pulled != 0 || rep.Pushed != 1 {
		t.Errorf("Pulled=%d Pushed=%d, want 0,1", rep.Pulled, rep.Pushed)
	}
	if store.ingestCalled {
		t.Errorf("SyncIngest was called, want push-only to skip it")
	}
	if len(target.published) != 1 || target.published[0].ID != e1.ID {
		t.Errorf("published = %v, want [%s]", rawEventIDs(target.published), e1.ID)
	}
}

func TestSyncDryRunTouchesNothing(t *testing.T) {
	clock := fixedClock()
	root := mustProject(t, clock, replicaA, "PRJ")
	e1 := mustTask(t, clock, replicaA, []string{root.ID}, "local task")
	e2 := mustTask(t, clock, replicaB, []string{root.ID}, "remote task")

	store := &fakeStore{events: map[string][]*eventsource.Event{"PRJ": {root, e1}}}
	target := &fakeTarget{snap: &RemoteSnapshot{Events: rawOf(root, e2)}}

	rep, err := Sync(context.Background(), store, target, "PRJ", Options{DryRun: true})
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if !rep.DryRun {
		t.Errorf("Report.DryRun = false, want true")
	}
	if rep.Pulled != 1 || rep.Pushed != 1 {
		t.Errorf("Pulled=%d Pushed=%d, want counts 1,1 reported", rep.Pulled, rep.Pushed)
	}
	if store.ingestCalled || store.bootstrapCalled {
		t.Errorf("dry run mutated local: ingest=%v bootstrap=%v", store.ingestCalled, store.bootstrapCalled)
	}
	if len(target.published) != 0 {
		t.Errorf("dry run published %v, want none", rawEventIDs(target.published))
	}
}

func TestSyncBootstrapsAbsentLocal(t *testing.T) {
	clock := fixedClock()
	root := mustProject(t, clock, replicaA, "PRJ")
	e1 := mustTask(t, clock, replicaA, []string{root.ID}, "remote task")

	store := &fakeStore{events: map[string][]*eventsource.Event{}}
	target := &fakeTarget{snap: &RemoteSnapshot{Events: rawOf(root, e1)}}

	rep, err := Sync(context.Background(), store, target, "PRJ", Options{})
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if !rep.Bootstrapped || rep.Pulled != 2 {
		t.Errorf("Bootstrapped=%v Pulled=%d, want true,2", rep.Bootstrapped, rep.Pulled)
	}
	if !store.bootstrapCalled || store.ingestCalled {
		t.Errorf("bootstrap=%v ingest=%v, want bootstrap only", store.bootstrapCalled, store.ingestCalled)
	}
	if got := len(store.events["PRJ"]); got != 2 {
		t.Errorf("local now has %d events, want 2", got)
	}
}

func TestSyncLocalAbsentRemoteAbsentIsError(t *testing.T) {
	store := &fakeStore{events: map[string][]*eventsource.Event{}}
	target := &fakeTarget{snap: &RemoteSnapshot{Absent: true}}

	rep, err := Sync(context.Background(), store, target, "PRJ", Options{})
	if err == nil {
		t.Fatalf("Sync: want error for nothing-to-sync, got report %+v", rep)
	}
	if rep != nil {
		t.Errorf("Report = %+v, want nil on error", rep)
	}
}

func TestSyncPushFailureReportedNotFatal(t *testing.T) {
	clock := fixedClock()
	root := mustProject(t, clock, replicaA, "PRJ")
	e1 := mustTask(t, clock, replicaA, []string{root.ID}, "local task")
	e2 := mustTask(t, clock, replicaB, []string{root.ID}, "remote task")

	store := &fakeStore{events: map[string][]*eventsource.Event{"PRJ": {root, e1}}}
	boom := errors.New("publish boom")
	target := &fakeTarget{snap: &RemoteSnapshot{Events: rawOf(root, e2)}, publishErr: boom}

	rep, err := Sync(context.Background(), store, target, "PRJ", Options{})
	if err != nil {
		t.Fatalf("Sync: want nil error (push failure is non-fatal here), got %v", err)
	}
	if !errors.Is(rep.PushErr, boom) {
		t.Errorf("PushErr = %v, want %v", rep.PushErr, boom)
	}
	// Pull committed and is reported intact despite the push failure.
	if rep.Pulled != 1 {
		t.Errorf("Pulled = %d, want 1 (pull intact)", rep.Pulled)
	}
	if rep.Pushed != 0 {
		t.Errorf("Pushed = %d, want 0 (push failed)", rep.Pushed)
	}
}

func TestSyncBidirectionalPushFailurePullNothingIsSoft(t *testing.T) {
	clock := fixedClock()
	root := mustProject(t, clock, replicaA, "PRJ")
	e1 := mustTask(t, clock, replicaA, []string{root.ID}, "local task")

	// Local already holds everything the remote has, so pull moves nothing;
	// only the local-only e1 is left to push, and that push fails. Because
	// pull was enabled (bidirectional), this is a legal "pulled OK, push
	// failed" state (L4-10): a non-nil Report with PushErr, nil error.
	store := &fakeStore{events: map[string][]*eventsource.Event{"PRJ": {root, e1}}}
	boom := errors.New("publish boom")
	target := &fakeTarget{snap: &RemoteSnapshot{Events: rawOf(root)}, publishErr: boom}

	rep, err := Sync(context.Background(), store, target, "PRJ", Options{Pull: true, Push: true})
	if err != nil {
		t.Fatalf("Sync: want nil error (bidirectional push failure is soft), got %v", err)
	}
	if rep == nil {
		t.Fatalf("Report = nil, want non-nil with PushErr")
	}
	if !errors.Is(rep.PushErr, boom) {
		t.Errorf("PushErr = %v, want %v", rep.PushErr, boom)
	}
	if rep.Pulled != 0 {
		t.Errorf("Pulled = %d, want 0 (nothing to pull)", rep.Pulled)
	}
	if rep.Pushed != 0 {
		t.Errorf("Pushed = %d, want 0 (push failed)", rep.Pushed)
	}
}

func TestSyncPushOnlyFailureIsFatal(t *testing.T) {
	clock := fixedClock()
	root := mustProject(t, clock, replicaA, "PRJ")
	e1 := mustTask(t, clock, replicaA, []string{root.ID}, "local task")

	store := &fakeStore{events: map[string][]*eventsource.Event{"PRJ": {root, e1}}}
	boom := errors.New("publish boom")
	target := &fakeTarget{snap: &RemoteSnapshot{Events: rawOf(root)}, publishErr: boom}

	rep, err := Sync(context.Background(), store, target, "PRJ", Options{Push: true})
	if err == nil {
		t.Fatalf("Sync: want error (push-only failure accomplished nothing), got report %+v", rep)
	}
	if rep != nil {
		t.Errorf("Report = %+v, want nil on fatal error", rep)
	}
}

func TestSyncDigestShortCircuit(t *testing.T) {
	clock := fixedClock()
	root := mustProject(t, clock, replicaA, "PRJ")
	e1 := mustTask(t, clock, replicaA, []string{root.ID}, "local task")

	store := &fakeStore{events: map[string][]*eventsource.Event{"PRJ": {root, e1}}}
	target := &narrowFakeTarget{
		fakeTarget: fakeTarget{snap: &RemoteSnapshot{Events: rawOf(root, e1)}},
		digest:     SetDigest(eventIDs([]*eventsource.Event{root, e1})),
	}

	rep, err := Sync(context.Background(), store, target, "PRJ", Options{})
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if target.fetched {
		t.Errorf("Fetch was called, want digest short-circuit to skip it")
	}
	if rep.Pulled != 0 || rep.Pushed != 0 {
		t.Errorf("Pulled=%d Pushed=%d, want 0,0 on short-circuit", rep.Pulled, rep.Pushed)
	}
}
