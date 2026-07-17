package store

import (
	"bytes"
	"errors"
	"os"
	"testing"

	"atm/internal/core"
	"atm/internal/store/eventlog"
	"atm/libs/eventsource"
)

// syncPeer bootstraps a fresh peer store from base (an origin's snapshot at
// some point in time), runs author against it, and returns the events the peer
// authored BEYOND base -- i.e. the "incoming" set an origin store would
// receive over the wire. Because the peer branches from base's frontier, the
// events it returns are a valid DAG continuation of the origin's file, which
// is exactly what SyncIngest requires. Exercising SyncBootstrap here is
// deliberate: it is the only realistic way to manufacture connected incoming
// events, and TestSyncBootstrapCreatesProject asserts bootstrap correctness
// independently.
func syncPeer(t *testing.T, code string, base []*eventsource.Event, opts []Option, author func(*Store)) []*eventsource.Event {
	t.Helper()
	p, err := Open(t.TempDir(), opts...)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Init(""); err != nil {
		t.Fatal(err)
	}
	if err := p.eng.SyncBootstrap(code, base); err != nil {
		t.Fatalf("peer bootstrap: %v", err)
	}
	author(p)
	snap, absent, err := p.eng.SyncSnapshot(code)
	if err != nil || absent {
		t.Fatalf("peer snapshot: absent=%v err=%v", absent, err)
	}
	have := map[string]bool{}
	for _, e := range base {
		have[e.ID] = true
	}
	var out []*eventsource.Event
	for _, e := range snap {
		if !have[e.ID] {
			out = append(out, e)
		}
	}
	return out
}

// countV2Lines returns the number of newline-terminated records in the
// project's events.v2.jsonl -- the on-disk line count SyncIngest grows by
// exactly len(incoming).
func countV2Lines(t *testing.T, s *Store, code string) int {
	t.Helper()
	data, err := os.ReadFile(s.eng.EventsV2Path(code))
	if err != nil {
		if os.IsNotExist(err) {
			return 0
		}
		t.Fatal(err)
	}
	return bytes.Count(data, []byte("\n"))
}

func mustSnapshot(t *testing.T, s *Store, code string) []*eventsource.Event {
	t.Helper()
	snap, absent, err := s.eng.SyncSnapshot(code)
	if err != nil {
		t.Fatalf("snapshot %s: %v", code, err)
	}
	if absent {
		t.Fatalf("snapshot %s: unexpectedly absent", code)
	}
	return snap
}

// TestSyncSnapshotV1Refused: a project whose media is on disk but whose format
// resolves to v1 cannot be snapshotted -- sync is v2-only.
func TestSyncSnapshotV1Refused(t *testing.T) {
	s := testStore(t)
	code := "ATM"
	// Fabricate v1-active media: a log.jsonl with no ProjectFormats entry, so
	// projectFormat falls through to the v1 default. (createProjectV2 always
	// makes v2 projects post-ATM-0127, so v1 media has to be planted directly.)
	if err := os.MkdirAll(s.projectDir(code), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.logPath(code), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, _, err := s.eng.SyncSnapshot(code)
	if !errors.Is(err, eventlog.ErrSyncNeedsV2) {
		t.Fatalf("SyncSnapshot on v1 project = %v, want eventlog.ErrSyncNeedsV2", err)
	}
}

// TestSyncSnapshotAbsentProject: an unknown code reports absent, not an error.
func TestSyncSnapshotAbsentProject(t *testing.T) {
	s := testStore(t)
	events, absent, err := s.eng.SyncSnapshot("ZZZ")
	if err != nil {
		t.Fatalf("SyncSnapshot on absent project = %v, want nil", err)
	}
	if !absent {
		t.Fatal("SyncSnapshot on absent project: absent=false, want true")
	}
	if events != nil {
		t.Fatalf("SyncSnapshot on absent project returned %d events, want nil", len(events))
	}
}

// TestSyncIngestAppendsTopoAndReprojects: ingesting a peer's events appends
// exactly len(incoming) lines and the reprojection makes ListTasks see them.
func TestSyncIngestAppendsTopoAndReprojects(t *testing.T) {
	s := testStore(t)
	code := "ATM"
	if _, err := s.CreateProject(code, "x", testActor); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateTask(code, "origin task", "", nil, testActor); err != nil {
		t.Fatal(err)
	}
	base := mustSnapshot(t, s, code)
	incoming := syncPeer(t, code, base, nil, func(p *Store) {
		if _, err := p.CreateTask(code, "peer task", "", nil, testActor); err != nil {
			t.Fatal(err)
		}
	})
	if len(incoming) == 0 {
		t.Fatal("peer authored no incoming events")
	}

	before := countV2Lines(t, s, code)
	ingested, newly, err := s.eng.SyncIngest(code, incoming)
	if err != nil {
		t.Fatal(err)
	}
	if ingested != len(incoming) {
		t.Fatalf("ingested=%d, want %d", ingested, len(incoming))
	}
	if newly != 0 {
		t.Fatalf("newlyContested=%d, want 0 (no concurrent slot writes)", newly)
	}
	if got := countV2Lines(t, s, code) - before; got != len(incoming) {
		t.Fatalf("file gained %d lines, want %d", got, len(incoming))
	}
	if tasks := s.ListTasks(QueryFilters{Project: code}); len(tasks) != 2 {
		t.Fatalf("ListTasks after ingest = %d, want 2", len(tasks))
	}
}

// TestSyncIngestIdempotent: re-ingesting events already in the file is a no-op.
func TestSyncIngestIdempotent(t *testing.T) {
	s := testStore(t)
	code := "ATM"
	if _, err := s.CreateProject(code, "x", testActor); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateTask(code, "origin task", "", nil, testActor); err != nil {
		t.Fatal(err)
	}
	base := mustSnapshot(t, s, code)
	incoming := syncPeer(t, code, base, nil, func(p *Store) {
		if _, err := p.CreateTask(code, "peer task", "", nil, testActor); err != nil {
			t.Fatal(err)
		}
	})
	if _, _, err := s.eng.SyncIngest(code, incoming); err != nil {
		t.Fatal(err)
	}
	ingested, newly, err := s.eng.SyncIngest(code, incoming)
	if err != nil {
		t.Fatal(err)
	}
	if ingested != 0 || newly != 0 {
		t.Fatalf("second ingest = (%d,%d), want (0,0)", ingested, newly)
	}
}

// TestSyncIngestObservesHLC: after ingesting events stamped far in the future,
// the next locally-authored event's HLC sorts after the ingested maximum --
// the store advanced its clock past what it received.
func TestSyncIngestObservesHLC(t *testing.T) {
	s := testStore(t)
	code := "ATM"
	if _, err := s.CreateProject(code, "x", testActor); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateTask(code, "origin task", "", nil, testActor); err != nil {
		t.Fatal(err)
	}
	base := mustSnapshot(t, s, code)
	// Peer clock pinned ~150 years ahead so its events dominate the origin's
	// wall-clock stamps unambiguously.
	far := func() int64 { return 5_000_000_000_000 }
	incoming := syncPeer(t, code, base, []Option{WithClock(far)}, func(p *Store) {
		if _, err := p.CreateTask(code, "peer task", "", nil, testActor); err != nil {
			t.Fatal(err)
		}
	})
	var maxIn eventsource.HLC
	for _, e := range incoming {
		if e.HLC.Compare(maxIn) > 0 {
			maxIn = e.HLC
		}
	}
	if _, _, err := s.eng.SyncIngest(code, incoming); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateTask(code, "after ingest", "", nil, testActor); err != nil {
		t.Fatal(err)
	}
	snap := mustSnapshot(t, s, code)
	local := snap[len(snap)-1] // the just-authored event is appended last
	if local.HLC.Compare(maxIn) <= 0 {
		t.Fatalf("local HLC %+v does not sort after ingested max %+v", local.HLC, maxIn)
	}
	if local.HLC.P < maxIn.P {
		t.Fatalf("local HLC.P=%d < ingested max P=%d", local.HLC.P, maxIn.P)
	}
}

// TestSyncIngestReportsNewlyContested: ingesting a concurrent edit to a slot
// the origin also just wrote turns that slot contested, and the ingest reports
// exactly one newly-contested slot.
func TestSyncIngestReportsNewlyContested(t *testing.T) {
	s := testStore(t)
	code := "ATM"
	if _, err := s.CreateProject(code, "x", testActor); err != nil {
		t.Fatal(err)
	}
	tk, err := s.CreateTask(code, "shared", "", nil, testActor)
	if err != nil {
		t.Fatal(err)
	}
	// Snapshot BEFORE either replica edits the title, so the two title edits
	// below both branch from the same frontier -- i.e. are concurrent.
	base := mustSnapshot(t, s, code)
	incoming := syncPeer(t, code, base, nil, func(p *Store) {
		if err := p.SetTitle(tk.ID, "peer title", testActor); err != nil {
			t.Fatal(err)
		}
	})
	if err := s.SetTitle(tk.ID, "origin title", testActor); err != nil {
		t.Fatal(err)
	}
	_, newly, err := s.eng.SyncIngest(code, incoming)
	if err != nil {
		t.Fatal(err)
	}
	if newly != 1 {
		t.Fatalf("newlyContested=%d, want 1", newly)
	}
}

// TestSyncBootstrapCreatesProject: bootstrapping into an empty store creates a
// listed, v2-format project whose fold matches the source snapshot.
func TestSyncBootstrapCreatesProject(t *testing.T) {
	origin := testStore(t)
	code := "ATM"
	if _, err := origin.CreateProject(code, "x", testActor); err != nil {
		t.Fatal(err)
	}
	if _, err := origin.CreateTask(code, "t1", "", nil, testActor); err != nil {
		t.Fatal(err)
	}
	if _, err := origin.CreateTask(code, "t2", "", nil, testActor); err != nil {
		t.Fatal(err)
	}
	base := mustSnapshot(t, origin, code)

	dest := testStore(t)
	if err := dest.eng.SyncBootstrap(code, base); err != nil {
		t.Fatal(err)
	}

	// Project is listed.
	listed := false
	for _, p := range dest.ListProjects() {
		if p.Code == code {
			listed = true
		}
	}
	if !listed {
		t.Fatal("bootstrapped project not listed")
	}
	// Format entry is v2.
	if f, err := dest.ProjectFormatForCLI(code); err != nil || f != eventlog.StoreFormatV2 {
		t.Fatalf("format = %q, %v; want v2", f, err)
	}
	// Fold matches source: identical event set and equal live task counts.
	got := mustSnapshot(t, dest, code)
	if len(got) != len(base) {
		t.Fatalf("bootstrapped %d events, source has %d", len(got), len(base))
	}
	have := map[string]bool{}
	for _, e := range base {
		have[e.ID] = true
	}
	for _, e := range got {
		if !have[e.ID] {
			t.Fatalf("bootstrapped file has event %s absent from source", e.ID)
		}
	}
	srcState, err := eventsource.FoldEvents(base)
	if err != nil {
		t.Fatal(err)
	}
	dstState, err := eventsource.FoldEvents(got)
	if err != nil {
		t.Fatal(err)
	}
	if len(srcState.Tasks) != len(dstState.Tasks) {
		t.Fatalf("fold task count %d != source %d", len(dstState.Tasks), len(srcState.Tasks))
	}
	if len(dest.ListTasks(QueryFilters{Project: code})) != len(origin.ListTasks(QueryFilters{Project: code})) {
		t.Fatal("ListTasks count differs from source after bootstrap")
	}
}

// TestSyncBootstrapRefusesExisting: bootstrap must never overwrite a project
// that already has media on disk -- that would silently clobber a live file.
func TestSyncBootstrapRefusesExisting(t *testing.T) {
	s := testStore(t)
	code := "ATM"
	if _, err := s.CreateProject(code, "x", testActor); err != nil {
		t.Fatal(err)
	}
	base := mustSnapshot(t, s, code)
	if err := s.eng.SyncBootstrap(code, base); !errors.Is(err, core.ErrConflict) {
		t.Fatalf("bootstrap over existing project = %v, want core.ErrConflict", err)
	}
}
