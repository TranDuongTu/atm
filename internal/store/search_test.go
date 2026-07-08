package store

import "testing"

func TestCosineSimilarity(t *testing.T) {
	if got := cosineSimilarity([]float64{1, 0}, []float64{1, 0}); got < 0.9999 {
		t.Errorf("cosine identical = %v, want ~1", got)
	}
	if got := cosineSimilarity([]float64{1, 0}, []float64{0, 1}); got > 0.0001 {
		t.Errorf("cosine orthogonal = %v, want ~0", got)
	}
	if got := cosineSimilarity([]float64{1, 0}, []float64{-1, 0}); got > -0.9999 {
		t.Errorf("cosine opposite = %v, want ~-1", got)
	}
}

func TestSearchSemanticRankingFromDenormalized(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateProject("ATM", "Agent Tasks Management", "tester"); err != nil {
		t.Fatal(err)
	}
	entries := []VectorEntry{
		{ID: "ATM-0001", Kind: "task", Model: "m", Dim: 2, Vector: []float64{1, 0}, TextHash: "h1", LogSeq: 1, Title: "label resolver refactor", Snippet: "refactor the resolver", Labels: []string{"ATM:type:feature"}},
		{ID: "ATM-0002", Kind: "task", Model: "m", Dim: 2, Vector: []float64{0, 1}, TextHash: "h2", LogSeq: 2, Title: "audit log redesign", Snippet: "redesign the audit log"},
	}
	if err := s.WriteVectorBatch("ATM", "m", entries, 2); err != nil {
		t.Fatal(err)
	}
	hits, fallback, err := s.Search(SearchParams{
		Project: "ATM", Model: "m", QueryVector: []float64{0.95, 0.05}, QueryText: "label resolver",
		K: 5, Threshold: 0.3,
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if fallback {
		t.Errorf("fallback=true, want false (strong semantic hit exists)")
	}
	if len(hits) == 0 || hits[0].ID != "ATM-0001" {
		t.Errorf("hits = %+v, want ATM-0001 first", hits)
	}
	if hits[0].Match != "semantic" {
		t.Errorf("hit.Match = %q, want semantic", hits[0].Match)
	}
	if hits[0].Title != "label resolver refactor" {
		t.Errorf("hit.Title = %q, want denormalized title", hits[0].Title)
	}
}

func TestSearchTextFallbackWhenNoIndex(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateProject("ATM", "Agent Tasks Management", "tester"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateTask("ATM", "label resolver refactor", "hierarchical prefixes", nil, "tester"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateTask("ATM", "audit log redesign", "", nil, "tester"); err != nil {
		t.Fatal(err)
	}
	hits, fallback, err := s.Search(SearchParams{
		Project: "ATM", Model: "m", QueryVector: nil, QueryText: "label resolver",
		K: 5, Threshold: 0.3,
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if !fallback {
		t.Errorf("fallback=false, want true (no index)")
	}
	if len(hits) == 0 {
		t.Fatalf("expected text hits, got none")
	}
	if hits[0].Match != "text" {
		t.Errorf("hit.Match = %q, want text", hits[0].Match)
	}
}

func TestSearchTextFallbackWhenWeakScore(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateProject("ATM", "Agent Tasks Management", "tester"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateTask("ATM", "label resolver", "hierarchical", nil, "tester"); err != nil {
		t.Fatal(err)
	}
	entries := []VectorEntry{
		{ID: "ATM-0001", Kind: "task", Model: "m", Dim: 2, Vector: []float64{0.1, 0.0}, TextHash: "h1", LogSeq: 1, Title: "label resolver", Snippet: "hierarchical"},
	}
	if err := s.WriteVectorBatch("ATM", "m", entries, 1); err != nil {
		t.Fatal(err)
	}
	hits, fallback, err := s.Search(SearchParams{
		Project: "ATM", Model: "m", QueryVector: []float64{0, 1}, QueryText: "label resolver",
		K: 5, Threshold: 0.3,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !fallback {
		t.Errorf("fallback=false, want true (weak score)")
	}
	if len(hits) == 0 || hits[0].Match != "text" {
		t.Errorf("hits = %+v, want text fallback", hits)
	}
}

func TestSearchDimMismatchRejected(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateProject("ATM", "Agent Tasks Management", "tester"); err != nil {
		t.Fatal(err)
	}
	if err := s.WriteVectorBatch("ATM", "m", []VectorEntry{{ID: "ATM-0001", Kind: "task", Model: "m", Dim: 2, Vector: []float64{0.1, 0.2}}}, 1); err != nil {
		t.Fatal(err)
	}
	_, _, err := s.Search(SearchParams{
		Project: "ATM", Model: "m", QueryVector: []float64{0.1, 0.2, 0.3}, QueryText: "q",
		K: 5, Threshold: 0.3,
	})
	if !IsUsage(err) {
		t.Errorf("err = %v, want ErrUsage (dim mismatch)", err)
	}
}

func TestSearchKindFilter(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateProject("ATM", "Agent Tasks Management", "tester"); err != nil {
		t.Fatal(err)
	}
	entries := []VectorEntry{
		{ID: "ATM-0001", Kind: "task", Model: "m", Dim: 2, Vector: []float64{1, 0}, TextHash: "h1", LogSeq: 1, Title: "t"},
		{ID: "ATM-0001-c0001", Kind: "comment", Model: "m", Dim: 2, Vector: []float64{1, 0}, TextHash: "h2", LogSeq: 2, Snippet: "c"},
	}
	if err := s.WriteVectorBatch("ATM", "m", entries, 2); err != nil {
		t.Fatal(err)
	}
	hits, _, err := s.Search(SearchParams{
		Project: "ATM", Model: "m", QueryVector: []float64{1, 0}, QueryText: "t",
		Kind: "task", K: 5, Threshold: 0.3,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range hits {
		if h.Kind != "task" {
			t.Errorf("got kind %q, want only task", h.Kind)
		}
	}
}

func TestSearchDeduplicatesStaleEntries(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateProject("ATM", "Agent Tasks Management", "tester"); err != nil {
		t.Fatal(err)
	}
	if err := s.WriteVectorBatch("ATM", "m", []VectorEntry{{ID: "ATM-0001", Kind: "task", Model: "m", Dim: 2, Vector: []float64{0, 1}, TextHash: "old", LogSeq: 1, Title: "old title", Snippet: "old"}}, 1); err != nil {
		t.Fatal(err)
	}
	if err := s.WriteVectorBatch("ATM", "m", []VectorEntry{{ID: "ATM-0001", Kind: "task", Model: "m", Dim: 2, Vector: []float64{1, 0}, TextHash: "new", LogSeq: 5, Title: "new title", Snippet: "new"}}, 5); err != nil {
		t.Fatal(err)
	}
	hits, _, err := s.Search(SearchParams{Project: "ATM", Model: "m", QueryVector: []float64{1, 0}, QueryText: "new", K: 5, Threshold: 0.3})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 {
		t.Fatalf("got %d hits, want 1 (deduplicated)", len(hits))
	}
	if hits[0].Title != "new title" {
		t.Errorf("hit.Title = %q, want %q (latest entry)", hits[0].Title, "new title")
	}
}
