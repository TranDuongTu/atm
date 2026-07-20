package store

import (
	"os"
	"testing"

	"atm/internal/store/eventlog"
)

// A fresh store has no projects: stats are zero and Version falls back to
// the store's ActiveFormat (v1 on a store.json materialized by Init with
// no explicit active_format).
func TestStoreStatsEmptyStore(t *testing.T) {
	s := newTestStore(t)
	st, err := s.StoreStats("")
	if err != nil {
		t.Fatal(err)
	}
	if st.SizeBytes != 0 || st.EventCount != 0 {
		t.Fatalf("empty store stats = %+v, want zeros", st)
	}
	if st.Version != "v1" {
		t.Fatalf("empty store Version = %q, want v1 (ActiveFormat fallback)", st.Version)
	}
}

// Projects are born v2 on a fresh store: every mutation appends one line to
// events.v2.jsonl, so EventCount is the file's line count and SizeBytes its
// byte size.
func TestStoreStatsCountsV2Events(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateProject("ATM", "x", testActor); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateTask("ATM", "one", "", nil, testActor); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateTask("ATM", "two", "", nil, testActor); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(s.eng.EventsV2Path("ATM"))
	if err != nil {
		t.Fatal(err)
	}
	wantCount := 0
	for _, b := range raw {
		if b == '\n' {
			wantCount++
		}
	}
	if wantCount == 0 {
		t.Fatal("test setup wrote no events")
	}
	st, err := s.StoreStats("")
	if err != nil {
		t.Fatal(err)
	}
	if st.EventCount != wantCount {
		t.Errorf("EventCount = %d, want %d", st.EventCount, wantCount)
	}
	if st.SizeBytes != int64(len(raw)) {
		t.Errorf("SizeBytes = %d, want %d", st.SizeBytes, len(raw))
	}
	if st.Version != "v2" {
		t.Errorf("Version = %q, want v2", st.Version)
	}
}

// Scoping to a project counts only that project's log, and the two scoped
// totals sum to the store-wide total. Version stays store-wide in every
// scope: it describes the store, not the slice being counted.
func TestStoreStatsScopedToProject(t *testing.T) {
	s := newTestStore(t)
	for _, code := range []string{"AAA", "BBB"} {
		if _, err := s.CreateProject(code, code, testActor); err != nil {
			t.Fatal(err)
		}
	}
	// Give AAA strictly more events than BBB so the scopes can't be confused.
	for i := 0; i < 3; i++ {
		if _, err := s.CreateTask("AAA", "a", "", nil, testActor); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.CreateTask("BBB", "b", "", nil, testActor); err != nil {
		t.Fatal(err)
	}
	all, err := s.StoreStats("")
	if err != nil {
		t.Fatal(err)
	}
	aaa, err := s.StoreStats("AAA")
	if err != nil {
		t.Fatal(err)
	}
	bbb, err := s.StoreStats("BBB")
	if err != nil {
		t.Fatal(err)
	}
	if aaa.EventCount+bbb.EventCount != all.EventCount {
		t.Errorf("scoped counts %d+%d != store-wide %d", aaa.EventCount, bbb.EventCount, all.EventCount)
	}
	if aaa.SizeBytes+bbb.SizeBytes != all.SizeBytes {
		t.Errorf("scoped sizes %d+%d != store-wide %d", aaa.SizeBytes, bbb.SizeBytes, all.SizeBytes)
	}
	if aaa.EventCount <= bbb.EventCount {
		t.Errorf("AAA (%d) should out-count BBB (%d)", aaa.EventCount, bbb.EventCount)
	}
	for name, got := range map[string]string{"all": all.Version, "AAA": aaa.Version, "BBB": bbb.Version} {
		if got != "v2" {
			t.Errorf("Version in scope %s = %q, want v2 in every scope", name, got)
		}
	}
}

// An unknown scope matches no project on disk: zero counts, but the
// store-wide Version still resolves.
func TestStoreStatsUnknownProjectScope(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateProject("AAA", "a", testActor); err != nil {
		t.Fatal(err)
	}
	st, err := s.StoreStats("NOPE")
	if err != nil {
		t.Fatal(err)
	}
	if st.EventCount != 0 || st.SizeBytes != 0 {
		t.Errorf("unknown scope = %+v, want zero counts", st)
	}
	if st.Version != "v2" {
		t.Errorf("Version = %q, want v2", st.Version)
	}
}

// Two projects whose effective formats disagree report Version "mixed".
// Flipping BBB's format entry to v1 also exercises the missing-file path:
// BBB has no log.jsonl, which must contribute zero, not an error.
func TestStoreStatsMixedFormats(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateProject("AAA", "a", testActor); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateProject("BBB", "b", testActor); err != nil {
		t.Fatal(err)
	}
	if err := s.eng.SetProjectFormat("BBB", eventlog.StoreFormatV1); err != nil {
		t.Fatal(err)
	}
	st, err := s.StoreStats("")
	if err != nil {
		t.Fatal(err)
	}
	if st.Version != "mixed" {
		t.Errorf("Version = %q, want mixed", st.Version)
	}
	if st.EventCount == 0 || st.SizeBytes == 0 {
		t.Errorf("AAA's v2 events should still count, got %+v", st)
	}
}
