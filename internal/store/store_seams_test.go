package store

import (
	"atm/internal/store/eventlog"
	"bytes"
	"testing"
	"time"
)

// fixedSeamOpts returns options that pin every entropy/wall-clock input a v2
// author touches: a counter clock (+1ms/tick), a constant replica-id seed,
// and a fixed `at` timestamp.
func fixedSeamOpts() []Option {
	var n int64 = 1_752_480_000_000
	return []Option{
		WithClock(func() int64 { n++; return n }),
		// ensureReplicaForWriteLocked mints both StoreInstanceID and
		// ReplicaID (32 bytes) on first use; pad generously so a fixed
		// reader never runs dry mid-test.
		WithReplicaEntropy(bytes.NewReader(bytes.Repeat([]byte{0xAB}, 256))),
		WithNow(func() time.Time { return time.Date(2026, 7, 14, 9, 12, 3, 0, time.UTC) }),
	}
}

// TestSeamsMakeV2AuthoringReproducible pins that two independently-opened
// stores, seeded with the same fixed clock/replica-entropy/now seams, mint
// the IDENTICAL v2 task alias for the same sequence of authoring calls. That
// reproducibility is the whole point of Task B1: goldens/determinism tests
// need to pin hex aliases, and v2 aliases hash over exactly these three
// wall-clock/random inputs.
func TestSeamsMakeV2AuthoringReproducible(t *testing.T) {
	mk := func() string {
		s, err := Open(t.TempDir(), fixedSeamOpts()...)
		if err != nil {
			t.Fatal(err)
		}
		if err := s.Init(""); err != nil {
			t.Fatal(err)
		}
		if err := s.SetActiveFormat(eventlog.StoreFormatV2); err != nil {
			t.Fatal(err)
		}
		if _, err := s.CreateProject("ATM", "Agent Tasks", testActor); err != nil {
			t.Fatal(err)
		}
		tk, err := s.CreateTask("ATM", "hello", "", nil, testActor)
		if err != nil {
			t.Fatal(err)
		}
		return tk.ID
	}
	a, b := mk(), mk()
	if a != b {
		t.Fatalf("v2 alias not reproducible under fixed seams: %q vs %q", a, b)
	}
}

// mustOpen and mustReplicaID are tiny option-less-Open helpers for
// TestProductionOpenKeepsRandomIdentity below.
func mustOpen(t *testing.T) *Store {
	t.Helper()
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Init(""); err != nil {
		t.Fatal(err)
	}
	return s
}

func mustReplicaID(t *testing.T, s *Store) string {
	t.Helper()
	id, err := s.eng.EnsureReplicaForWriteLocked()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// TestProductionOpenKeepsRandomIdentity guards the Global Constraint: Open
// with NO options must stay byte-for-byte the production behavior (wall
// clock + crypto/rand.Reader). Two option-less Opens must mint DIFFERENT
// random replica ids -- if this ever fails, a seam default got wired wrong
// and production determinism broke.
func TestProductionOpenKeepsRandomIdentity(t *testing.T) {
	r1 := mustReplicaID(t, mustOpen(t))
	r2 := mustReplicaID(t, mustOpen(t))
	if r1 == r2 || r1 == "" {
		t.Fatalf("production Open should mint distinct random replicas, got %q,%q", r1, r2)
	}
}
