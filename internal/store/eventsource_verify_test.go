package store

import (
	"os"
	"runtime"
	"sync"
	"testing"

	"atm/libs/eventsource"
)

// resetCacheForRebuild deletes cache.db and clears the Store's memoized
// handle to it, forcing the next cacheDB() call to reopen (and Rebuild to
// regenerate) a fresh cache file. Mirrors the pattern already used by
// TestRebuildUsesV2ForV2ActiveProject.
func resetCacheForRebuild(t *testing.T, s *Store) {
	t.Helper()
	if err := os.Remove(s.cachePath()); err != nil {
		t.Fatal(err)
	}
	s.cacheOnce = sync.Once{}
	s.cacheDBConn = nil
}

// appendRawV2LabelEvent appends a label.upserted event with an arbitrary
// name directly to code's live v2 log, bypassing LabelAdd's <CODE>: prefix
// ownership check the same way TestCacheProjectFromV2StateHandlesReupgradeDiscardingLabel
// (eventsource_projector_test.go) does — the eventsource fold layer itself
// places no constraint on which project a label name "belongs to", only
// LabelAdd's caller-facing validation does.
func appendRawV2LabelEvent(t *testing.T, s *Store, code, name string, tick int64) {
	t.Helper()
	snap, err := s.readV2File(code, false)
	if err != nil {
		t.Fatal(err)
	}
	clock := eventsource.NewClock(func() int64 { return tick })
	ev, err := eventsource.NewEvent(clock, "r_0123456789abcdefghjkmnpqrs", snap.Frontier, eventsource.Draft{
		Actor:   "admin@cli:unset",
		Action:  "label.upserted",
		Subject: eventsource.Subject{Kind: "label", Name: name},
		Payload: map[string]any{"description": "shared", "expr": ""},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.WithLock(code, func() error { return s.appendV2EventLineLocked(code, ev.Raw) }); err != nil {
		t.Fatal(err)
	}
}

func TestVerifyProjectReportsV2Format(t *testing.T) {
	s := testStore(t)
	_, _ = s.CreateProject("ATM", "x", "admin@cli:unset")
	_, _ = s.CreateTask("ATM", "t", "", nil, "admin@cli:unset")
	r, err := s.VerifyProject("ATM")
	if err != nil {
		t.Fatal(err)
	}
	if r.Format != string(StoreFormatV2) {
		t.Fatalf("Format = %q, want v2", r.Format)
	}
	if r.V2Events == 0 {
		t.Fatalf("V2Events = %d, want > 0", r.V2Events)
	}
}

func TestRebuildUsesV2ForV2ActiveProject(t *testing.T) {
	s := testStore(t)
	_, _ = s.CreateProject("ATM", "x", "admin@cli:unset")
	tk, _ := s.CreateTask("ATM", "t", "", nil, "admin@cli:unset")
	if err := os.Remove(s.cachePath()); err != nil {
		t.Fatal(err)
	}
	s.cacheOnce = sync.Once{}
	s.cacheDBConn = nil
	rep, err := s.Rebuild()
	if err != nil {
		t.Fatal(err)
	}
	if rep.Tasks == 0 {
		t.Fatalf("rebuild report = %#v", rep)
	}
	if _, err := s.GetTask(tk.ID); err != nil {
		t.Fatalf("GetTask after v2 rebuild: %v", err)
	}
}

func TestVerifyProjectV2KeepsVectorAndInquiryReports(t *testing.T) {
	s := testStore(t)
	_, _ = s.CreateProject("ATM", "x", "admin@cli:unset")
	_, _ = s.CreateTask("ATM", "t", "", nil, "admin@cli:unset")
	// This project is born v2 directly (no v1 log, so no v1-keyed index for
	// an upgrade's cutover step to wipe); the vector batch is written straight
	// into its v2 index.
	if err := s.WriteVectorBatch("ATM", "test-model", []VectorEntry{{ID: "ATM-0001", Kind: "task", Model: "test-model", Dim: 2, Vector: []float64{1, 0}, TextHash: "sha256:x", LogSeq: 1}}, 3); err != nil {
		t.Fatal(err)
	}
	r, err := s.VerifyProject("ATM")
	if err != nil {
		t.Fatal(err)
	}
	if len(r.VectorIndexes) != 1 || r.VectorIndexes[0].Model != "test-model" {
		t.Fatalf("VectorIndexes = %#v, want the test-model index reported for a v2 project", r.VectorIndexes)
	}
}

// TestRebuildToleratesCorruptV2ProjectAndRebuildsHealthyOnes pins Finding 1:
// one project's corrupt events.v2.jsonl must not abort the whole-store
// rebuild. "AAA" sorts before "ZZZ" (projectCodesOnDisk is sorted), so this
// also exercises that processing continues PAST the corrupt project rather
// than merely reaching it last.
func TestRebuildToleratesCorruptV2ProjectAndRebuildsHealthyOnes(t *testing.T) {
	s := testStore(t)
	if _, err := s.CreateProject("AAA", "corrupt-to-be", "admin@cli:unset"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateProject("ZZZ", "healthy", "admin@cli:unset"); err != nil {
		t.Fatal(err)
	}
	tk, err := s.CreateTask("ZZZ", "healthy task", "", nil, "admin@cli:unset")
	if err != nil {
		t.Fatal(err)
	}
	// A complete-but-unparseable line is an integrity error (never a repair
	// target), per readV2FileAt's contract — see
	// TestReadV2FileRejectsMalformedCompleteLine.
	if err := os.WriteFile(s.eventsV2Path("AAA"), []byte("{not-json}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	resetCacheForRebuild(t, s)
	rep, err := s.Rebuild()
	if err != nil {
		t.Fatalf("Rebuild aborted the whole store on AAA's corrupt log: %v", err)
	}
	if rep.Tasks == 0 {
		t.Fatalf("rebuild report = %#v, want ZZZ's healthy task counted", rep)
	}
	if _, err := s.GetTask(tk.ID); err != nil {
		t.Fatalf("GetTask(%s) after rebuild: %v, want ZZZ rebuilt despite AAA's corruption", tk.ID, err)
	}
}

// TestVerifyProjectV2SurfacesNonIntegrityErrorInstead pins Finding 2: a
// plain I/O error reading a v2 project's log (permission denied, not
// malformed content) must propagate as a real error, not be reported as a
// "diverged"/corrupt project. Skipped under root, where a permission bit
// never faults a read.
func TestVerifyProjectV2SurfacesNonIntegrityErrorInstead(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chmod-based permission denial is not meaningful on windows")
	}
	if os.Getuid() == 0 {
		t.Skip("running as root: file permissions do not block reads")
	}
	s := testStore(t)
	if _, err := s.CreateProject("AAA", "x", "admin@cli:unset"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateTask("AAA", "t", "", nil, "admin@cli:unset"); err != nil {
		t.Fatal(err)
	}
	path := s.eventsV2Path("AAA")
	if err := os.Chmod(path, 0o000); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(path, 0o644)
	report, err := s.VerifyProject("AAA")
	if err == nil {
		t.Fatalf("VerifyProject with an unreadable v2 log = (%#v, nil), want a real error", report)
	}
	if IsIntegrity(err) {
		t.Fatalf("VerifyProject wrapped a permission error as an integrity error: %v", err)
	}
	if report != nil {
		t.Fatalf("VerifyProject returned a report alongside a hard error: %#v", report)
	}
}

// TestRebuildDedupsSharedLabelNameAcrossV2Projects pins Finding 3: the
// store-global labels table is keyed by name alone, so two v2 projects that
// (however it happened) both carry a label of the same name must count as
// one in the rebuild report, matching the actual deduped row count in the
// cache.
func TestRebuildDedupsSharedLabelNameAcrossV2Projects(t *testing.T) {
	s := testStore(t)
	if _, err := s.CreateProject("AAA", "x", "admin@cli:unset"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateProject("BBB", "y", "admin@cli:unset"); err != nil {
		t.Fatal(err)
	}
	appendRawV2LabelEvent(t, s, "AAA", "SHARED:tag", 5000)
	appendRawV2LabelEvent(t, s, "BBB", "SHARED:tag", 5001)
	resetCacheForRebuild(t, s)
	rep, err := s.Rebuild()
	if err != nil {
		t.Fatal(err)
	}
	got := len(s.LabelList("", ""))
	if rep.Labels != got {
		t.Fatalf("rep.Labels = %d, want %d (the actual deduped row count) — a shared name was double-counted", rep.Labels, got)
	}
}

// TestRebuildDoesNotWipeVectorIndexForV2Project is the permanent regression
// guard the reviewer asked for: Rebuild's v1 branch clears a project's
// vector index (the index is keyed by v1 log seq, meaningless post-cutover),
// but the v2 branch must NOT — a v2 project's vectors are keyed by v2 event
// count and stay valid across a rebuild.
func TestRebuildDoesNotWipeVectorIndexForV2Project(t *testing.T) {
	s := testStore(t)
	if _, err := s.CreateProject("ATM", "x", "admin@cli:unset"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateTask("ATM", "t", "", nil, "admin@cli:unset"); err != nil {
		t.Fatal(err)
	}
	// This project is born v2 directly (no v1 log, so no v1-keyed index for
	// an upgrade's cutover step to wipe); the vector batch is written straight
	// into its v2 index.
	if err := s.WriteVectorBatch("ATM", "test-model", []VectorEntry{{ID: "ATM-0001", Kind: "task", Model: "test-model", Dim: 2, Vector: []float64{1, 0}, TextHash: "sha256:x", LogSeq: 1}}, 3); err != nil {
		t.Fatal(err)
	}
	resetCacheForRebuild(t, s)
	if _, err := s.Rebuild(); err != nil {
		t.Fatal(err)
	}
	models, err := s.ListVectorModels("ATM")
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 1 || models[0] != "test-model" {
		t.Fatalf("ListVectorModels after Rebuild = %v, want [test-model] (Rebuild must not wipe a v2 project's vector index)", models)
	}
	meta, err := s.VectorMeta("ATM", "test-model")
	if err != nil {
		t.Fatal(err)
	}
	if meta == nil || meta.Count != 1 {
		t.Fatalf("VectorMeta after Rebuild = %#v, want Count=1", meta)
	}
}
