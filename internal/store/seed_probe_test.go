package store

// No-op seed benchmark for ATM-40faff regression checks against a COPY of
// a live store. Points at the copy via ATM_BENCH_STORE; skipped otherwise.

import (
	"os"
	"testing"
)

func BenchmarkNoopSeedReal(b *testing.B) {
	root := os.Getenv("ATM_BENCH_STORE")
	if root == "" {
		b.Skip("ATM_BENCH_STORE not set")
	}
	s, err := Open(root)
	if err != nil {
		b.Fatalf("Open: %v", err)
	}
	const actor = "developer@claude:fable-5"
	if err := s.LabelSeed("ATM:status:open", "scrum stage: the unit is under review", "", actor); err != nil {
		b.Fatalf("warm seed: %v", err)
	}
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if err := s.LabelSeed("ATM:status:open", "scrum stage: the unit is under review", "", actor); err != nil {
			b.Fatalf("seed: %v", err)
		}
	}
}