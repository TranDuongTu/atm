package store

import (
	"context"
	"errors"
	"testing"
)

func TestPendingIndexEmptyWhenFresh(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateProject("ATM", "Agent Tasks Management", "tester"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateTask("ATM", "label resolver", "hierarchical", nil, "tester"); err != nil {
		t.Fatal(err)
	}
	realTask, err := s.GetTask("ATM-0001")
	if err != nil {
		t.Fatal(err)
	}
	realHash := hashText(taskDocumentText(realTask))
	entries := []VectorEntry{{ID: realTask.ID, Kind: "task", Model: "m", Dim: 2, Vector: []float64{0.1, 0.2}, TextHash: realHash, LogSeq: realTask.LogSeq, Title: "label resolver", Snippet: "hierarchical"}}
	if err := s.WriteVectorBatch("ATM", "m", entries, realTask.LogSeq); err != nil {
		t.Fatal(err)
	}
	pending, err := s.PendingIndex("ATM", "m")
	if err != nil {
		t.Fatalf("PendingIndex: %v", err)
	}
	if len(pending) != 0 {
		t.Errorf("pending = %d, want 0 (fresh)", len(pending))
	}
}

func TestPendingIndexDetectsNew(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateProject("ATM", "Agent Tasks Management", "tester"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateTask("ATM", "label resolver", "hierarchical", nil, "tester"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateTask("ATM", "audit log", "", nil, "tester"); err != nil {
		t.Fatal(err)
	}
	pending, err := s.PendingIndex("ATM", "m")
	if err != nil {
		t.Fatalf("PendingIndex: %v", err)
	}
	if len(pending) != 2 {
		t.Errorf("pending = %d, want 2 (no index yet)", len(pending))
	}
}

func TestReindexOnceWithFakeEmbedder(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateProject("ATM", "Agent Tasks Management", "tester"); err != nil {
		t.Fatal(err)
	}
	task, err := s.CreateTask("ATM", "label resolver", "hierarchical", nil, "tester")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetEmbeddingConfig("ATM", EmbeddingConfig{Model: "m", Endpoint: "http://x", Dim: 2, Threshold: 0.5}, "tester"); err != nil {
		t.Fatal(err)
	}
	fake := func(text, role string) ([]float64, error) {
		return []float64{0.1, 0.2}, nil
	}
	res, err := s.ReindexOnce("ATM", fake, nil)
	if err != nil {
		t.Fatalf("ReindexOnce: %v", err)
	}
	if res.Indexed != 1 {
		t.Errorf("indexed = %d, want 1", res.Indexed)
	}
	got, _ := s.ReadVectors("ATM", "m")
	if len(got) != 1 || got[0].ID != task.ID || got[0].Title != "label resolver" {
		t.Errorf("vectors = %+v, want one denormalized task", got)
	}
}

func TestReindexOnceEmbedErrorAborts(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateProject("ATM", "Agent Tasks Management", "tester"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateTask("ATM", "label resolver", "", nil, "tester"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetEmbeddingConfig("ATM", EmbeddingConfig{Model: "m", Endpoint: "http://x", Dim: 2, Threshold: 0.5}, "tester"); err != nil {
		t.Fatal(err)
	}
	fake := func(text, role string) ([]float64, error) {
		return nil, errors.New("endpoint down")
	}
	if _, err := s.ReindexOnce("ATM", fake, nil); err == nil {
		t.Fatal("want error on embed failure, got nil")
	}
}

func TestReindexOnceNoConfigErrUsage(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateProject("ATM", "Agent Tasks Management", "tester"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateTask("ATM", "t", "", nil, "tester"); err != nil {
		t.Fatal(err)
	}
	fake := func(text, role string) ([]float64, error) { return []float64{0.1}, nil }
	_, err := s.ReindexOnce("ATM", fake, nil)
	if !IsUsage(err) {
		t.Errorf("err = %v, want ErrUsage (no embedding config)", err)
	}
}

func TestWatchTriggersOnNewLogAppend(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateProject("ATM", "Agent Tasks Management", "tester"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateTask("ATM", "first task", "", nil, "tester"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetEmbeddingConfig("ATM", EmbeddingConfig{Model: "m", Endpoint: "http://x", Dim: 2, Threshold: 0.5}, "tester"); err != nil {
		t.Fatal(err)
	}
	fake := func(text, role string) ([]float64, error) { return []float64{0.1, 0.2}, nil }
	calls := 0
	progress := func(msg string) { calls++ }
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.Watch(ctx, "ATM", fake, progress) }()
	if _, err := s.CreateTask("ATM", "second task", "", nil, "tester"); err != nil {
		t.Fatal(err)
	}
	cancel()
	<-done
	if calls == 0 {
		t.Error("progress never called; watch loop did not index")
	}
}
