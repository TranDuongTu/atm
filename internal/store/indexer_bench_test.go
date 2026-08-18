package store

import (
	"os"
	"testing"
)

// benchRealStore opens a real store via ATM_BENCH_STORE -- a copy of a live
// store, per the pattern established in internal/tui/bench_real_store_test.go
// -- and skips when unset. Named apart from bench_reproject_test.go's
// benchStore, which seeds a synthetic fixture at a different scale for a
// different purpose. The cost under test here is proportional to corpus
// size and vector-file size, so a synthetic fixture would answer a question
// nobody asked.
func benchRealStore(b *testing.B) (*Store, string) {
	b.Helper()
	root := os.Getenv("ATM_BENCH_STORE")
	if root == "" {
		b.Skip("ATM_BENCH_STORE not set")
	}
	s, err := Open(root)
	if err != nil {
		b.Fatalf("store.Open: %v", err)
	}
	code := os.Getenv("ATM_BENCH_PROJECT")
	if code == "" {
		code = "ATM"
	}
	if _, err := s.GetProject(code); err != nil {
		b.Skipf("project %s not in the store", code)
	}
	return s, code
}

// BenchmarkPendingIndexCount measures the call answer.Engine makes once per
// question, immediately before it emits Retrieved (engine.go:202). Retrieved
// is the event whose whole purpose is immediacy: the spotlight paints
// SOURCES from it before any generation is attempted, so whatever this
// costs is dead time between the user pressing Tab and seeing anything at
// all.
//
// PendingIndexCount returns len(PendingIndex(...)), and PendingIndex folds
// every live entity, reads the whole vector file into a map, and builds and
// hashes a document for every entity -- allocating the full IndexDoc slice,
// text included, purely to count it.
func BenchmarkPendingIndexCount(b *testing.B) {
	s, code := benchRealStore(b)
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := s.PendingIndexCount(code, "nomic-embed-text"); err != nil {
			b.Fatalf("PendingIndexCount: %v", err)
		}
	}
}
