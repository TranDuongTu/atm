package tui

// Temporary real-store benchmark for the ATM-4c476c lag investigation.
// Points at a COPY of a live store via ATM_BENCH_STORE; skipped otherwise.

import (
	"os"
	"testing"

	"atm/internal/capability"
	"atm/internal/capability/scrum"
	"atm/internal/store"
)

func openRealStore(b *testing.B) (*store.Store, *Model) {
	root := os.Getenv("ATM_BENCH_STORE")
	if root == "" {
		b.Skip("ATM_BENCH_STORE not set")
	}
	s, err := store.Open(root)
	if err != nil {
		b.Fatalf("store.Open: %v", err)
	}
	m, err := NewModel(NewModelOpts{Service: s, Actor: benchActor, Registry: capability.NewRegistry(scrum.New())})
	if err != nil {
		b.Fatalf("NewModel: %v", err)
	}
	return s, m
}

func BenchmarkRealRefreshAll(b *testing.B) {
	_, m := openRealStore(b)
	m.projectScope = "ATM"
	m.refreshAll()
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		m.refreshAll()
	}
}

func BenchmarkRealView(b *testing.B) {
	_, m := openRealStore(b)
	m.projectScope = "ATM"
	m.refreshAll()
	m.focused = paneTasks
	m.SetSize(180, 50)
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = m.View()
	}
}

func BenchmarkRealIndexerRefreshStatus(b *testing.B) {
	_, m := openRealStore(b)
	m.projectScope = "ATM"
	im := newIndexerPlugin().model(m)
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		im.refreshStatus()
	}
}

func BenchmarkRealPendingIndex(b *testing.B) {
	s, m := openRealStore(b)
	_ = m
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := s.PendingIndex("ATM", "nomic-embed-text"); err != nil {
			b.Fatalf("PendingIndex: %v", err)
		}
	}
}
