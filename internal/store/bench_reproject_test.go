package store

import (
	"fmt"
	"testing"
)

// benchStore seeds a store at roughly the live ATM ledger's scale
// (ATM-d402aa: 156 tasks, 514 comments, 1660 events) so the reprojection
// benchmarks measure the regression's actual regime, not a toy. It returns
// the store plus one existing task alias for the mutation benchmark (aliases
// are engine-minted — never assume a literal like "ATM-0001").
func benchStore(b *testing.B) (*Store, string) {
	b.Helper()
	s, err := Open(b.TempDir())
	if err != nil {
		b.Fatalf("Open: %v", err)
	}
	if err := s.Init(""); err != nil {
		b.Fatalf("Init: %v", err)
	}
	if _, err := s.CreateProject("ATM", "Acme Task Manager", testActor); err != nil {
		b.Fatalf("CreateProject: %v", err)
	}
	if err := s.LabelSeed("ATM:open-tasks", "open work", "status:open", testActor); err != nil {
		b.Fatalf("LabelSeed: %v", err)
	}
	var taskID string
	for i := 0; i < 150; i++ {
		task, err := s.CreateTask("ATM", fmt.Sprintf("task %03d", i), "", []string{"ATM:status:open"}, testActor)
		if err != nil {
			b.Fatalf("CreateTask %d: %v", i, err)
		}
		if taskID == "" {
			taskID = task.ID
		}
		for j := 0; j < 3; j++ {
			if _, err := s.CreateComment(task.ID, fmt.Sprintf("comment %d on %s", j, task.ID), nil, "", testActor); err != nil {
				b.Fatalf("CreateComment %d/%d: %v", i, j, err)
			}
		}
	}
	return s, taskID
}

// BenchmarkNoopLabelSeed is the TUI project-select path (EnsureVocabulary
// runs 4 of these per select). Before ATM-d402aa's fix each iteration paid a
// full fold + full cache rewrite (~3.8s on the live store); after, it is one
// begin-fold with no projection.
func BenchmarkNoopLabelSeed(b *testing.B) {
	s, _ := benchStore(b)
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if err := s.LabelSeed("ATM:open-tasks", "open work", "status:open", testActor); err != nil {
			b.Fatalf("LabelSeed: %v", err)
		}
	}
}

// BenchmarkMutationReproject is one real mutation end-to-end (append + fold +
// one-transaction cache rewrite) — the path ATM-d402aa collapsed from ~1,500
// implicit commits to one.
func BenchmarkMutationReproject(b *testing.B) {
	s, taskID := benchStore(b)
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if err := s.SetTitle(taskID, fmt.Sprintf("title %d", i), testActor); err != nil {
			b.Fatalf("SetTitle: %v", err)
		}
	}
}
