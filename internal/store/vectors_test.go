package store

import (
	"os"
	"path/filepath"
	"testing"
)

func TestVectorPaths(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateProject("ATM", "Agent Tasks Management", "tester"); err != nil {
		t.Fatal(err)
	}
	got := s.vectorPath("ATM", "nomic-embed-text")
	want := filepath.Join(s.projectDir("ATM"), "vectors", "nomic-embed-text.jsonl")
	if got != want {
		t.Errorf("vectorPath = %q, want %q", got, want)
	}
	got = s.vectorMetaPath("ATM", "nomic-embed-text")
	want = filepath.Join(s.projectDir("ATM"), "vectors", "nomic-embed-text.meta.json")
	if got != want {
		t.Errorf("vectorMetaPath = %q, want %q", got, want)
	}
}

func TestWriteVectorBatchRoundTripDenormalized(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateProject("ATM", "Agent Tasks Management", "tester"); err != nil {
		t.Fatal(err)
	}
	entries := []VectorEntry{
		{ID: "ATM-0001", Kind: "task", Model: "nomic-embed-text", Dim: 4, Vector: []float64{0.1, 0.2, 0.3, 0.4}, TextHash: "h1", LogSeq: 5, Title: "label resolver", Snippet: "refactor the label resolver", Labels: []string{"ATM:type:feature"}},
		{ID: "ATM-0001-c0001", Kind: "comment", Model: "nomic-embed-text", Dim: 4, Vector: []float64{0.5, 0.6, 0.7, 0.8}, TextHash: "h2", LogSeq: 6, Snippet: "decided to use prefixes", Labels: []string{"ATM:comment:decision"}},
	}
	if err := s.WriteVectorBatch("ATM", "nomic-embed-text", entries, 6); err != nil {
		t.Fatalf("WriteVectorBatch: %v", err)
	}
	got, err := s.ReadVectors("ATM", "nomic-embed-text")
	if err != nil {
		t.Fatalf("ReadVectors: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d entries, want 2", len(got))
	}
	if got[0].ID != "ATM-0001" || got[0].Title != "label resolver" {
		t.Errorf("entry 0 denormalized: %+v", got[0])
	}
	if got[1].ID != "ATM-0001-c0001" || got[1].Snippet != "decided to use prefixes" {
		t.Errorf("entry 1 denormalized: %+v", got[1])
	}
	meta, err := s.VectorMeta("ATM", "nomic-embed-text")
	if err != nil {
		t.Fatalf("VectorMeta: %v", err)
	}
	if meta.LastLogSeq != 6 || meta.Count != 2 || meta.Dim != 4 {
		t.Errorf("meta = %+v, want last_log_seq=6 count=2 dim=4", meta)
	}
}

func TestWriteVectorBatchAppends(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateProject("ATM", "Agent Tasks Management", "tester"); err != nil {
		t.Fatal(err)
	}
	first := []VectorEntry{{ID: "ATM-0001", Kind: "task", Model: "m", Dim: 2, Vector: []float64{0.1, 0.2}, TextHash: "h1", LogSeq: 1}}
	if err := s.WriteVectorBatch("ATM", "m", first, 1); err != nil {
		t.Fatal(err)
	}
	second := []VectorEntry{{ID: "ATM-0002", Kind: "task", Model: "m", Dim: 2, Vector: []float64{0.3, 0.4}, TextHash: "h2", LogSeq: 2}}
	if err := s.WriteVectorBatch("ATM", "m", second, 2); err != nil {
		t.Fatal(err)
	}
	got, err := s.ReadVectors("ATM", "m")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d entries, want 2 (append)", len(got))
	}
	meta, _ := s.VectorMeta("ATM", "m")
	if meta.Count != 2 || meta.LastLogSeq != 2 {
		t.Errorf("meta after append = %+v, want count=2 last_log_seq=2", meta)
	}
}

func TestWriteVectorBatchModelMismatchRejected(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateProject("ATM", "Agent Tasks Management", "tester"); err != nil {
		t.Fatal(err)
	}
	entries := []VectorEntry{{ID: "ATM-0001", Kind: "task", Model: "other", Dim: 2, Vector: []float64{0.1, 0.2}}}
	err := s.WriteVectorBatch("ATM", "m", entries, 1)
	if !IsUsage(err) {
		t.Errorf("err = %v, want ErrUsage", err)
	}
}

func TestWriteVectorBatchDimMismatchRejected(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateProject("ATM", "Agent Tasks Management", "tester"); err != nil {
		t.Fatal(err)
	}
	entries := []VectorEntry{
		{ID: "ATM-0001", Kind: "task", Model: "m", Dim: 2, Vector: []float64{0.1, 0.2}},
		{ID: "ATM-0002", Kind: "task", Model: "m", Dim: 4, Vector: []float64{0.1, 0.2, 0.3, 0.4}},
	}
	err := s.WriteVectorBatch("ATM", "m", entries, 2)
	if !IsUsage(err) {
		t.Errorf("err = %v, want ErrUsage", err)
	}
}

func TestDropVectors(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateProject("ATM", "Agent Tasks Management", "tester"); err != nil {
		t.Fatal(err)
	}
	if err := s.WriteVectorBatch("ATM", "m", []VectorEntry{{ID: "ATM-0001", Kind: "task", Model: "m", Dim: 2, Vector: []float64{0.1, 0.2}}}, 1); err != nil {
		t.Fatal(err)
	}
	if err := s.DropVectors("ATM", "m"); err != nil {
		t.Fatalf("DropVectors: %v", err)
	}
	if got, _ := s.ReadVectors("ATM", "m"); got != nil {
		t.Errorf("ReadVectors after drop: %v, want nil", got)
	}
	err := s.DropVectors("ATM", "m")
	if !IsNotFound(err) {
		t.Errorf("DropVectors missing = %v, want ErrNotFound", err)
	}
}

func TestListVectorModels(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateProject("ATM", "Agent Tasks Management", "tester"); err != nil {
		t.Fatal(err)
	}
	for _, slug := range []string{"nomic-embed-text", "text-embedding-3-small"} {
		if err := s.WriteVectorBatch("ATM", slug, []VectorEntry{{ID: "ATM-0001", Kind: "task", Model: slug, Dim: 2, Vector: []float64{0.1, 0.2}}}, 1); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.ListVectorModels("ATM")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"nomic-embed-text", "text-embedding-3-small"}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("ListVectorModels = %v, want %v", got, want)
	}
}

func TestReadVectorsMalformedLineSkipped(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateProject("ATM", "Agent Tasks Management", "tester"); err != nil {
		t.Fatal(err)
	}
	if err := s.WriteVectorBatch("ATM", "m", []VectorEntry{{ID: "ATM-0001", Kind: "task", Model: "m", Dim: 2, Vector: []float64{0.1, 0.2}}}, 1); err != nil {
		t.Fatal(err)
	}
	path := s.vectorPath("ATM", "m")
	f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	_, _ = f.WriteString("{not valid json\n")
	_ = f.Close()
	got, err := s.ReadVectors("ATM", "m")
	if err != nil {
		t.Fatalf("ReadVectors: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("got %d valid entries, want 1 (malformed skipped)", len(got))
	}
}
