package store

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
	"time"

	"atm/libs/eventsource"
	eventsync "atm/libs/eventsource/sync"
)

// These tests drive the REAL end-to-end sync stack -- eventsync.Sync over an
// eventsync.DirTarget transport into store.Sync{Snapshot,Ingest,Bootstrap} --
// and assert the D4 convergence promise: replicas that author divergent edits
// and then sync in any order settle on the SAME event set and therefore the
// SAME fold. The contract is the event-id SET and the FoldEvents State, NOT
// byte-identical files: an append log records history in author order, so two
// replicas' files legitimately differ in line ORDER while holding the same
// events.
//
// Determinism comes from the store's Open seams: each replica gets a DISTINCT
// deterministic replica-entropy source (so concurrent writes to one slot mint
// distinct event ids instead of colliding) and a pinned HLC/now (so a failing
// run is reproducible). Fold is a pure function of the event set, so once two
// replicas hold the same set their States are reflect.DeepEqual by
// construction -- the real thing under test is that they DO reach the same set.

// e2eStore opens an initialized v2 store with determinism seams pinned. seed
// selects the replica-entropy stream, which must be DISTINCT per replica so
// their minted replica ids -- and hence the ids of concurrent same-slot
// writes -- differ. A math/rand stream never runs out, unlike a fixed byte
// slice, so replica-id minting can draw its 16 bytes freely.
func e2eStore(t *testing.T, seed int64) *Store {
	t.Helper()
	var tick int64
	clock := func() int64 { tick++; return 1_700_000_000_000 + tick }
	now := func() time.Time { return time.Unix(1_700_000_000, 0).UTC() }
	s, err := Open(t.TempDir(),
		WithClock(clock),
		WithReplicaEntropy(rand.New(rand.NewSource(seed))),
		WithNow(now),
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Init(""); err != nil {
		t.Fatal(err)
	}
	return s
}

// eventIDSet returns the set of event ids in a store's current snapshot of
// project code.
func eventIDSet(t *testing.T, s *Store, code string) map[string]bool {
	t.Helper()
	snap := mustSnapshot(t, s, code)
	set := make(map[string]bool, len(snap))
	for _, e := range snap {
		if set[e.ID] {
			t.Fatalf("duplicate event id %s in snapshot of %s", e.ID, code)
		}
		set[e.ID] = true
	}
	return set
}

// foldOf folds a store's current snapshot of project code.
func foldOf(t *testing.T, s *Store, code string) *eventsource.State {
	t.Helper()
	st, err := eventsource.FoldEvents(mustSnapshot(t, s, code))
	if err != nil {
		t.Fatalf("fold %s: %v", code, err)
	}
	return st
}

// assertConverged asserts two stores hold identical event-id sets and
// reflect.DeepEqual folds for project code.
func assertConverged(t *testing.T, a, b *Store, code, label string) {
	t.Helper()
	setA := eventIDSet(t, a, code)
	setB := eventIDSet(t, b, code)
	if !reflect.DeepEqual(setA, setB) {
		t.Fatalf("%s: event-id sets differ: |A|=%d |B|=%d\nonly in A: %v\nonly in B: %v",
			label, len(setA), len(setB), setDiff(setA, setB), setDiff(setB, setA))
	}
	foldA := foldOf(t, a, code)
	foldB := foldOf(t, b, code)
	if !reflect.DeepEqual(foldA, foldB) {
		t.Fatalf("%s: folds differ despite equal id sets (a real fold-determinism bug)", label)
	}
}

func setDiff(a, b map[string]bool) []string {
	var out []string
	for id := range a {
		if !b[id] {
			out = append(out, id)
		}
	}
	sort.Strings(out)
	return out
}

// TestTwoStoreConvergenceByteIdenticalFold: A creates a project with tasks and
// publishes to a dir remote; B bootstraps from that remote; A and B then
// author DIVERGENT edits (a title edit vs a comment add) with distinct replica
// ids; three sync passes (A, B, A) settle both directions; both replicas end
// with identical event-id sets and reflect.DeepEqual folds.
func TestTwoStoreConvergenceByteIdenticalFold(t *testing.T) {
	ctx := context.Background()
	code := "ATM"
	remoteRoot := t.TempDir()
	target := eventsync.NewDirTarget(remoteRoot)

	a := e2eStore(t, 1)
	if _, err := a.CreateProject(code, "convergence", testActor); err != nil {
		t.Fatal(err)
	}
	tk, err := a.CreateTask(code, "shared task", "", nil, testActor)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.CreateTask(code, "second task", "", nil, testActor); err != nil {
		t.Fatal(err)
	}

	// A publishes the initial project to the remote.
	if _, err := eventsync.Sync(ctx, a.eng, target, code, eventsync.Options{}); err != nil {
		t.Fatalf("A initial sync: %v", err)
	}

	// B bootstraps from the remote (project absent locally until this sync).
	b := e2eStore(t, 2)
	rep, err := eventsync.Sync(ctx, b.eng, target, code, eventsync.Options{})
	if err != nil {
		t.Fatalf("B bootstrap sync: %v", err)
	}
	if !rep.Bootstrapped {
		t.Fatalf("B bootstrap sync: Bootstrapped=false, want true (report %+v)", rep)
	}
	assertConverged(t, a, b, code, "after bootstrap")

	// Divergent edits, authored concurrently (before either re-syncs) so they
	// branch from the same frontier: A edits the shared task's title, B adds a
	// comment to it. Distinct replica ids keep the two events distinct.
	if err := a.SetTitle(tk.ID, "A edited title", testActor); err != nil {
		t.Fatal(err)
	}
	if _, err := b.CreateComment(tk.ID, "B added a comment", nil, "", testActor); err != nil {
		t.Fatal(err)
	}

	// Three passes settle both directions: A pushes its edit; B pulls it and
	// pushes its comment; A pulls the comment.
	if _, err := eventsync.Sync(ctx, a.eng, target, code, eventsync.Options{}); err != nil {
		t.Fatalf("A sync pass: %v", err)
	}
	if _, err := eventsync.Sync(ctx, b.eng, target, code, eventsync.Options{}); err != nil {
		t.Fatalf("B sync pass: %v", err)
	}
	if _, err := eventsync.Sync(ctx, a.eng, target, code, eventsync.Options{}); err != nil {
		t.Fatalf("A final sync pass: %v", err)
	}

	assertConverged(t, a, b, code, "after divergent edits")

	// The convergence is real, not empty: the fold reflects both edits. Fold
	// maps are keyed by entity identity (the creation event id), not the
	// display alias, so look the task up by its alias.
	st := foldOf(t, a, code)
	if got := taskTitleByAlias(t, st, tk.ID); got != "A edited title" {
		t.Fatalf("converged title = %q, want %q", got, "A edited title")
	}
	if len(st.Comments) != 1 {
		t.Fatalf("converged comment count = %d, want 1", len(st.Comments))
	}
}

// taskTitleByAlias returns the Title of the folded task whose display alias is
// alias. Fold keys Tasks by entity identity, not alias, so callers holding a
// store-level alias (Task.ID) must resolve through EntityMeta.Alias.
func taskTitleByAlias(t *testing.T, st *eventsource.State, alias string) string {
	t.Helper()
	for _, tk := range st.Tasks {
		if tk.Alias == alias {
			return tk.Title
		}
	}
	t.Fatalf("no folded task with alias %s", alias)
	return ""
}

// commentPresentByAlias reports whether the fold holds a comment with the
// given display alias.
func commentPresentByAlias(st *eventsource.State, alias string) bool {
	for _, cm := range st.Comments {
		if cm.Alias == alias {
			return true
		}
	}
	return false
}

// TestThreeReplicaRandomizedConvergence: three replicas share one dir remote.
// A seeded rand drives 30 iterations that each pick a replica and either
// author a mutation (create task / edit title / add label / add comment) or
// sync it against the remote. Two full sync rounds over every replica settle
// the system; all three end reflect.DeepEqual with equal id sets. The seed is
// logged so a failure is replayable.
func TestThreeReplicaRandomizedConvergence(t *testing.T) {
	ctx := context.Background()
	code := "ATM"
	const seed = int64(20250108)
	t.Logf("randomized convergence seed = %d (pass -run TestThreeReplicaRandomizedConvergence to replay)", seed)
	rng := rand.New(rand.NewSource(seed))

	remoteRoot := t.TempDir()
	target := eventsync.NewDirTarget(remoteRoot)

	// Distinct entropy seeds -> distinct replica ids.
	stores := []*Store{e2eStore(t, 11), e2eStore(t, 12), e2eStore(t, 13)}

	// Replica 0 seeds the project and publishes it; 1 and 2 bootstrap. After
	// this every replica holds the project, so a "create task" mutation on any
	// of them is well-formed.
	if _, err := stores[0].CreateProject(code, "randomized", testActor); err != nil {
		t.Fatal(err)
	}
	for _, s := range stores {
		if _, err := eventsync.Sync(ctx, s.eng, target, code, eventsync.Options{}); err != nil {
			t.Fatalf("initial bootstrap sync: %v", err)
		}
	}

	labelSeq := 0
	for i := 0; i < 30; i++ {
		s := stores[rng.Intn(len(stores))]
		switch rng.Intn(5) {
		case 0: // sync
			if _, err := eventsync.Sync(ctx, s.eng, target, code, eventsync.Options{}); err != nil {
				t.Fatalf("iter %d sync: %v", i, err)
			}
		case 1: // create task
			if _, err := s.CreateTask(code, fmt.Sprintf("task i%d", i), "", nil, testActor); err != nil {
				t.Fatalf("iter %d create task: %v", i, err)
			}
		case 2: // edit a random existing task's title
			if tasks := s.ListTasks(QueryFilters{Project: code}); len(tasks) > 0 {
				tk := tasks[rng.Intn(len(tasks))]
				if err := s.SetTitle(tk.ID, fmt.Sprintf("edited i%d", i), testActor); err != nil {
					t.Fatalf("iter %d set title: %v", i, err)
				}
			}
		case 3: // add a fresh label definition (self-contained, always valid)
			labelSeq++
			name := fmt.Sprintf("%s:lbl-%d", code, labelSeq)
			if err := s.LabelAdd(name, "seeded", "", testActor); err != nil {
				t.Fatalf("iter %d label add: %v", i, err)
			}
		case 4: // add a comment to a random existing task
			if tasks := s.ListTasks(QueryFilters{Project: code}); len(tasks) > 0 {
				tk := tasks[rng.Intn(len(tasks))]
				if _, err := s.CreateComment(tk.ID, fmt.Sprintf("comment i%d", i), nil, "", testActor); err != nil {
					t.Fatalf("iter %d comment: %v", i, err)
				}
			}
		}
	}

	// Two full sync rounds over every replica: round 1 pushes everyone's local
	// work to the remote and pulls what was there before them; round 2 pulls
	// the now-complete union into every replica.
	for round := 0; round < 2; round++ {
		for _, s := range stores {
			if _, err := eventsync.Sync(ctx, s.eng, target, code, eventsync.Options{}); err != nil {
				t.Fatalf("settle round %d sync: %v", round, err)
			}
		}
	}

	assertConverged(t, stores[0], stores[1], code, "three-replica 0 vs 1")
	assertConverged(t, stores[0], stores[2], code, "three-replica 0 vs 2")
}

// TestConflictCopyRecoveryByUnion: a file-sync tool forks the remote log --
// events.v2.jsonl and a sibling events.v2.jsonl.conflict each gain disjoint
// appends -- and the documented recovery runbook concatenates the conflict
// file's lines back onto the main file. That leaves the remote holding the
// shared prefix TWICE plus each fork's tail. Fetch/Plan must dedupe by
// recomputed id so a subsequent sync converges every replica with NO duplicate
// events in the fold.
func TestConflictCopyRecoveryByUnion(t *testing.T) {
	ctx := context.Background()
	code := "ATM"
	remoteRoot := t.TempDir()
	target := eventsync.NewDirTarget(remoteRoot)
	mainPath := filepath.Join(remoteRoot, code, "events.v2.jsonl")
	conflictPath := mainPath + ".conflict"

	// A creates the project and publishes the shared base to the remote.
	a := e2eStore(t, 21)
	if _, err := a.CreateProject(code, "conflict-copy", testActor); err != nil {
		t.Fatal(err)
	}
	tk, err := a.CreateTask(code, "base task", "", nil, testActor)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := eventsync.Sync(ctx, a.eng, target, code, eventsync.Options{}); err != nil {
		t.Fatalf("A publish base: %v", err)
	}
	base := mustSnapshot(t, a, code)

	// The file-sync tool forks the log: copy the main file to a .conflict
	// sibling. Both now hold the identical base prefix.
	baseBytes, err := os.ReadFile(mainPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(conflictPath, baseBytes, 0o644); err != nil {
		t.Fatal(err)
	}

	// The MAIN fork gains an append: A edits the base task's title and pushes.
	if err := a.SetTitle(tk.ID, "A main-fork title", testActor); err != nil {
		t.Fatal(err)
	}
	if _, err := eventsync.Sync(ctx, a.eng, target, code, eventsync.Options{}); err != nil {
		t.Fatalf("A main-fork sync: %v", err)
	}

	// The CONFLICT fork gains a DISJOINT append: B bootstraps from the same
	// base and adds a comment; that event's raw line is appended straight onto
	// the .conflict file, simulating the other side of the file-sync fork.
	b := e2eStore(t, 22)
	if err := b.eng.SyncBootstrap(code, base); err != nil {
		t.Fatalf("B bootstrap: %v", err)
	}
	cm, err := b.CreateComment(tk.ID, "B conflict-fork comment", nil, "", testActor)
	if err != nil {
		t.Fatal(err)
	}
	bEvent := newestEventNotIn(t, b, code, base)
	appendLine(t, conflictPath, bEvent.Raw)

	// The runbook: concatenate the conflict file's lines onto the main file.
	// The main file now holds base(again) + B's comment appended after its own
	// base + A's title edit -- duplicate base lines and all.
	conflictBytes, err := os.ReadFile(conflictPath)
	if err != nil {
		t.Fatal(err)
	}
	appendBytes(t, mainPath, conflictBytes)

	// Sync converges everyone off the deduped union.
	if _, err := eventsync.Sync(ctx, a.eng, target, code, eventsync.Options{}); err != nil {
		t.Fatalf("A recovery sync: %v", err)
	}
	if _, err := eventsync.Sync(ctx, b.eng, target, code, eventsync.Options{}); err != nil {
		t.Fatalf("B recovery sync: %v", err)
	}
	if _, err := eventsync.Sync(ctx, a.eng, target, code, eventsync.Options{}); err != nil {
		t.Fatalf("A final recovery sync: %v", err)
	}

	assertConverged(t, a, b, code, "conflict-copy recovery")

	// No duplicate events survived into either replica's fold, and both edits
	// are present: base task + A's title + B's comment.
	st := foldOf(t, a, code)
	if got := taskTitleByAlias(t, st, tk.ID); got != "A main-fork title" {
		t.Fatalf("recovered title = %q, want %q", got, "A main-fork title")
	}
	if !commentPresentByAlias(st, cm.ID) {
		t.Fatalf("recovered fold missing B's comment %s", cm.ID)
	}
	// eventIDSet (inside assertConverged) already rejects duplicate ids in a
	// snapshot; re-confirm the on-remote duplicate base lines did not inflate
	// the ingested set.
	if n := len(eventIDSet(t, a, code)); n != len(base)+2 {
		t.Fatalf("converged event count = %d, want %d (base %d + title + comment)", n, len(base)+2, len(base))
	}
}

// newestEventNotIn returns the single event in s's snapshot of code that is
// absent from base -- the event the caller just authored.
func newestEventNotIn(t *testing.T, s *Store, code string, base []*eventsource.Event) *eventsource.Event {
	t.Helper()
	have := make(map[string]bool, len(base))
	for _, e := range base {
		have[e.ID] = true
	}
	var found *eventsource.Event
	for _, e := range mustSnapshot(t, s, code) {
		if !have[e.ID] {
			if found != nil {
				t.Fatalf("expected exactly one new event, found >1")
			}
			found = e
		}
	}
	if found == nil {
		t.Fatal("no new event found beyond base")
	}
	return found
}

// appendLine appends a single canonical event line (raw + newline) to path.
func appendLine(t *testing.T, path string, raw []byte) {
	t.Helper()
	appendBytes(t, path, append(append([]byte{}, raw...), '\n'))
}

// appendBytes appends data verbatim to path (O_APPEND), mirroring how a file
// concatenation runbook grows an append log.
func appendBytes(t *testing.T, path string, data []byte) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.Write(data); err != nil {
		t.Fatal(err)
	}
}
