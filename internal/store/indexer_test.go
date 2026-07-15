package store

import (
	"context"
	"errors"
	"fmt"
	"os"
	"regexp"
	"testing"
)

func TestPendingIndexEmptyWhenFresh(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateProject("ATM", "Agent Tasks Management", testActor); err != nil {
		t.Fatal(err)
	}
	created, err := s.CreateTask("ATM", "label resolver", "hierarchical", nil, testActor)
	if err != nil {
		t.Fatal(err)
	}
	realTask, err := s.GetTask(created.ID)
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
	if _, err := s.CreateProject("ATM", "Agent Tasks Management", testActor); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateTask("ATM", "label resolver", "hierarchical", nil, testActor); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateTask("ATM", "audit log", "", nil, testActor); err != nil {
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
	if _, err := s.CreateProject("ATM", "Agent Tasks Management", testActor); err != nil {
		t.Fatal(err)
	}
	task, err := s.CreateTask("ATM", "label resolver", "hierarchical", nil, testActor)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetEmbeddingConfig("ATM", EmbeddingConfig{Model: "m", Endpoint: "http://x", Dim: 2, Threshold: 0.5}, testActor); err != nil {
		t.Fatal(err)
	}
	fake := func(text, role string) ([]float64, error) {
		return []float64{0.1, 0.2}, nil
	}
	res, err := s.ReindexOnce(context.Background(), "ATM", fake, nil)
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

// TestReindexOnceHonorsContextCancellation proves a full re-index can be
// interrupted mid-pass (ATM-17e9cc): the embed loop must observe ctx between
// documents, return context.Canceled promptly, and persist no partial batch.
// Without cancellation the TUI's stopIndexer blocks the Update loop on <-done
// for the entire re-index, freezing the UI on project switch.
func TestReindexOnceHonorsContextCancellation(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateProject("ATM", "Agent Tasks Management", testActor); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		if _, err := s.CreateTask("ATM", fmt.Sprintf("task %d", i), "body", nil, testActor); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.SetEmbeddingConfig("ATM", EmbeddingConfig{Model: "m", Endpoint: "http://x", Dim: 2, Threshold: 0.5}, testActor); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	fake := func(text, role string) ([]float64, error) {
		calls++
		cancel() // cancel after the first embed; the pass must stop before the rest
		return []float64{0.1, 0.2}, nil
	}
	_, err := s.ReindexOnce(ctx, "ATM", fake, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if calls >= 5 {
		t.Errorf("embed called %d times; cancellation did not interrupt the re-index", calls)
	}
	if got, _ := s.ReadVectors("ATM", "m"); len(got) != 0 {
		t.Errorf("wrote %d vector(s); a cancelled pass must not persist a partial batch", len(got))
	}
}

func TestReindexOnceEmbedErrorAborts(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateProject("ATM", "Agent Tasks Management", testActor); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateTask("ATM", "label resolver", "", nil, testActor); err != nil {
		t.Fatal(err)
	}
	if err := s.SetEmbeddingConfig("ATM", EmbeddingConfig{Model: "m", Endpoint: "http://x", Dim: 2, Threshold: 0.5}, testActor); err != nil {
		t.Fatal(err)
	}
	fake := func(text, role string) ([]float64, error) {
		return nil, errors.New("endpoint down")
	}
	if _, err := s.ReindexOnce(context.Background(), "ATM", fake, nil); err == nil {
		t.Fatal("want error on embed failure, got nil")
	}
}

func TestReindexOnceNoConfigErrUsage(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateProject("ATM", "Agent Tasks Management", testActor); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateTask("ATM", "t", "", nil, testActor); err != nil {
		t.Fatal(err)
	}
	fake := func(text, role string) ([]float64, error) { return []float64{0.1}, nil }
	_, err := s.ReindexOnce(context.Background(), "ATM", fake, nil)
	if !IsUsage(err) {
		t.Errorf("err = %v, want ErrUsage (no embedding config)", err)
	}
}

// v2 hash aliases are "<CODE>-" + >=6 hex chars (eventsource.MintTaskAlias),
// never the v1 sequential "ATM-0001" form.
var v2TaskAliasRe = regexp.MustCompile(`^ATM-[0-9a-f]{6,}$`)

// TestReindexOnceOnV2EmbedsAndPinsFreshnessToEventCount pins the COMPOSED
// indexer path on a v2-active project end to end: the task created after
// cutover is discovered, embedded, stored under its hash alias, and the pass
// leaves VectorMeta.LastLogSeq in EVENT-COUNT space -- i.e. `atm index status`
// reports Behind == 0 and Watch does not spin. Without the isV2 watermark
// (indexer.go's `maxSeq = passSeq`), LastLogSeq lands on a creation ordinal and
// the index reports itself permanently behind.
func TestReindexOnceOnV2EmbedsAndPinsFreshnessToEventCount(t *testing.T) {
	s := testStore(t)
	if _, err := s.CreateProject("ATM", "Agent Tasks Management", testActor); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpgradeProjectToV2("ATM"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetEmbeddingConfig("ATM", EmbeddingConfig{Model: "m", Endpoint: "http://x", Dim: 2, Threshold: 0.5}, testActor); err != nil {
		t.Fatal(err)
	}
	tk, err := s.CreateTask("ATM", "embed me after cutover", "body", nil, testActor)
	if err != nil {
		t.Fatal(err)
	}
	if !v2TaskAliasRe.MatchString(tk.ID) {
		t.Fatalf("task id = %q, want a v2 hash alias (test is not exercising v2)", tk.ID)
	}
	fake := func(text, role string) ([]float64, error) { return []float64{0.1, 0.2}, nil }
	res, err := s.ReindexOnce(context.Background(), "ATM", fake, nil)
	if err != nil {
		t.Fatalf("ReindexOnce on v2: %v", err)
	}
	if res.Indexed == 0 {
		t.Fatal("indexed = 0: nothing created after cutover was embedded; semantic search silently rots")
	}
	vecs, err := s.ReadVectors("ATM", "m")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, v := range vecs {
		if v.ID == tk.ID && v.Kind == "task" {
			found = true
		}
	}
	if !found {
		t.Fatalf("vectors = %#v, want one under the task's hash alias %s", vecs, tk.ID)
	}
	count, err := s.v2EventCount("ATM")
	if err != nil {
		t.Fatal(err)
	}
	meta, err := s.VectorMeta("ATM", "m")
	if err != nil || meta == nil {
		t.Fatalf("VectorMeta = %#v, %v", meta, err)
	}
	if meta.LastLogSeq != count {
		t.Fatalf("VectorMeta.LastLogSeq = %d, want the v2 event count %d (index would report itself %d events behind forever)", meta.LastLogSeq, count, count-meta.LastLogSeq)
	}
	if res.LogSeq != count {
		t.Fatalf("IndexResult.LogSeq = %d, want the v2 event count %d (Watch's lastSeq lives in this space)", res.LogSeq, count)
	}
	last, err := s.LastLogSeq("ATM")
	if err != nil {
		t.Fatal(err)
	}
	if behind := last - meta.LastLogSeq; behind != 0 {
		t.Fatalf("behind = %d, want 0 right after a successful pass", behind)
	}
}

// TestReindexOnceOnV2PropagatesFormatLookupError: a failed format lookup must
// never fall back to the v1 watermark path (Minor 4). With store.json unreadable
// the pass has to fail loudly.
func TestReindexOnceOnV2PropagatesFormatLookupError(t *testing.T) {
	s := testStore(t)
	if _, err := s.CreateProject("ATM", "x", testActor); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateTask("ATM", "t", "", nil, testActor); err != nil {
		t.Fatal(err)
	}
	if err := s.SetEmbeddingConfig("ATM", EmbeddingConfig{Model: "m", Endpoint: "http://x", Dim: 2, Threshold: 0.5}, testActor); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.storeMetaPath(), []byte("{ not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	fake := func(text, role string) ([]float64, error) { return []float64{0.1, 0.2}, nil }
	if _, err := s.ReindexOnce(context.Background(), "ATM", fake, nil); err == nil {
		t.Fatal("ReindexOnce swallowed a format-lookup failure and indexed anyway")
	}
}

func TestWatchTriggersOnNewLogAppend(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateProject("ATM", "Agent Tasks Management", testActor); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateTask("ATM", "first task", "", nil, testActor); err != nil {
		t.Fatal(err)
	}
	if err := s.SetEmbeddingConfig("ATM", EmbeddingConfig{Model: "m", Endpoint: "http://x", Dim: 2, Threshold: 0.5}, testActor); err != nil {
		t.Fatal(err)
	}
	fake := func(text, role string) ([]float64, error) { return []float64{0.1, 0.2}, nil }
	calls := 0
	progress := func(msg string) { calls++ }
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.Watch(ctx, "ATM", fake, progress) }()
	if _, err := s.CreateTask("ATM", "second task", "", nil, testActor); err != nil {
		t.Fatal(err)
	}
	cancel()
	<-done
	if calls == 0 {
		t.Error("progress never called; watch loop did not index")
	}
}
