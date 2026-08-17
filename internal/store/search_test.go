package store

import (
	"atm/internal/core"
	"fmt"
	"testing"
)

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
	if _, err := s.CreateProject("ATM", "Agent Tasks Management", testActor); err != nil {
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
	if _, err := s.CreateProject("ATM", "Agent Tasks Management", testActor); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateTask("ATM", "label resolver refactor", "hierarchical prefixes", nil, testActor); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateTask("ATM", "audit log redesign", "", nil, testActor); err != nil {
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
	if _, err := s.CreateProject("ATM", "Agent Tasks Management", testActor); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateTask("ATM", "label resolver", "hierarchical", nil, testActor); err != nil {
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
	if _, err := s.CreateProject("ATM", "Agent Tasks Management", testActor); err != nil {
		t.Fatal(err)
	}
	if err := s.WriteVectorBatch("ATM", "m", []VectorEntry{{ID: "ATM-0001", Kind: "task", Model: "m", Dim: 2, Vector: []float64{0.1, 0.2}}}, 1); err != nil {
		t.Fatal(err)
	}
	_, _, err := s.Search(SearchParams{
		Project: "ATM", Model: "m", QueryVector: []float64{0.1, 0.2, 0.3}, QueryText: "q",
		K: 5, Threshold: 0.3,
	})
	if !core.IsUsage(err) {
		t.Errorf("err = %v, want core.ErrUsage (dim mismatch)", err)
	}
}

func TestSearchKindFilter(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateProject("ATM", "Agent Tasks Management", testActor); err != nil {
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

// A pasted ID must find its entity even though the ID is not part of the
// document text the scorer sees (taskDocumentText is title+description+labels,
// deliberately: the ID is not worth embedding). Text search is where an ID
// lookup has to work, because that is the path a brand-new task takes.
func TestSearchTextFindsATaskByItsID(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateProject("ATM", "Acme", testActor); err != nil {
		t.Fatal(err)
	}
	tk, err := s.CreateTask("ATM", "no keyword here", "", nil, testActor)
	if err != nil {
		t.Fatal(err)
	}

	hits, fallback, err := s.Search(SearchParams{Project: "ATM", QueryText: tk.ID, Kind: "all", K: 10})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if !fallback {
		t.Fatal("a store with no vectors must report the text fallback")
	}
	if len(hits) == 0 || hits[0].ID != tk.ID {
		t.Fatalf("Search(%q) = %+v, want the task itself first", tk.ID, hits)
	}
	if hits[0].Score <= 0 {
		t.Errorf("an ID hit scored %v, want a positive score", hits[0].Score)
	}
}

// An entity the query NAMES outranks one that merely mentions the query's
// words, however many overlap. The fixture puts the ID match LAST in store
// order (creation order) and gives the decoy richer word overlap, so only the
// tier sort can bring the named task forward.
func TestSearchTextRanksIDMatchesAboveBodyMatches(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateProject("ATM", "Acme", testActor); err != nil {
		t.Fatal(err)
	}
	decoy, err := s.CreateTask("ATM", "placeholder one", "", nil, testActor)
	if err != nil {
		t.Fatal(err)
	}
	target, err := s.CreateTask("ATM", "no keyword here", "", nil, testActor)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetTitle(decoy.ID, "decoy mentions "+target.ID+" in its title", testActor); err != nil {
		t.Fatalf("SetTitle: %v", err)
	}

	hits, _, err := s.Search(SearchParams{Project: "ATM", QueryText: target.ID, Kind: "all", K: 10})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) != 2 {
		t.Fatalf("Search(%q) = %d hits, want the target and the decoy", target.ID, len(hits))
	}
	if hits[0].ID != target.ID {
		t.Errorf("first hit = %q, want the named task %q", hits[0].ID, target.ID)
	}
}

// The cap is applied after the ranking, never before it: with more body
// matches than K, the named entity must still survive the cut.
func TestSearchTextAppliesKAfterRanking(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateProject("ATM", "Acme", testActor); err != nil {
		t.Fatal(err)
	}
	var decoys []*Task
	for i := 0; i < 7; i++ {
		d, err := s.CreateTask("ATM", fmt.Sprintf("placeholder %d", i), "", nil, testActor)
		if err != nil {
			t.Fatal(err)
		}
		decoys = append(decoys, d)
	}
	target, err := s.CreateTask("ATM", "no keyword here", "", nil, testActor)
	if err != nil {
		t.Fatal(err)
	}
	for i, d := range decoys {
		if err := s.SetTitle(d.ID, fmt.Sprintf("decoy %d mentions %s", i, target.ID), testActor); err != nil {
			t.Fatalf("SetTitle: %v", err)
		}
	}

	hits, _, err := s.Search(SearchParams{Project: "ATM", QueryText: target.ID, Kind: "all", K: 3})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) != 3 {
		t.Fatalf("Search with K=3 returned %d hits", len(hits))
	}
	if hits[0].ID != target.ID {
		t.Errorf("first hit = %q, want the named task — K must cut after the ranking", hits[0].ID)
	}
}

// A partially typed word finds its entity. The spotlight queries this path on
// a debounce as the user types, so exact-token-only matching would leave the
// results section empty until a whole word was finished — worse than the
// strings.Contains matcher the redesign deletes. Measured before the fix:
// "indexer" found the task, "index" and "ind" found nothing.
func TestSearchTextMatchesAPartialWord(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateProject("ATM", "Acme", testActor); err != nil {
		t.Fatal(err)
	}
	tk, err := s.CreateTask("ATM", "wire the indexer", "", nil, testActor)
	if err != nil {
		t.Fatal(err)
	}

	for _, q := range []string{"indexer", "index", "ind", "i"} {
		hits, _, err := s.Search(SearchParams{Project: "ATM", QueryText: q, Kind: "all", K: 10})
		if err != nil {
			t.Fatalf("Search(%q): %v", q, err)
		}
		if len(hits) != 1 || hits[0].ID != tk.ID {
			t.Errorf("Search(%q) = %+v, want the one task — a prefix of a word must match it", q, hits)
		}
	}
	// A prefix of nothing in the document still matches nothing: the rule is
	// prefix, not fuzzy.
	hits, _, err := s.Search(SearchParams{Project: "ATM", QueryText: "zzz", Kind: "all", K: 10})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) != 0 {
		t.Errorf("Search(\"zzz\") = %+v, want no hits", hits)
	}
}

// Every query token must be carried by the document, prefix-wise — one
// matching token out of two is a weaker hit, not a non-hit, and a query whose
// tokens match nothing is still empty.
func TestSearchTextPrefixMatchingIsPerToken(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateProject("ATM", "Acme", testActor); err != nil {
		t.Fatal(err)
	}
	tk, err := s.CreateTask("ATM", "wire the indexer", "", nil, testActor)
	if err != nil {
		t.Fatal(err)
	}

	hits, _, err := s.Search(SearchParams{Project: "ATM", QueryText: "the index", Kind: "all", K: 10})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) != 1 || hits[0].ID != tk.ID {
		t.Fatalf("Search(\"the index\") = %+v, want the task", hits)
	}
	if hits[0].Score != 2 {
		t.Errorf("Score = %v, want 2 — both query tokens are carried (one exact, one by prefix)", hits[0].Score)
	}
}

func TestSearchDeduplicatesStaleEntries(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateProject("ATM", "Agent Tasks Management", testActor); err != nil {
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
