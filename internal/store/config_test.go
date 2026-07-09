package store

import "testing"

func TestProjectConfigAbsentReturnsNil(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateProject("ATM", "Agent Tasks Management", "tester"); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetProjectConfig("ATM")
	if err != nil {
		t.Fatalf("GetProjectConfig absent: %v", err)
	}
	if got != nil {
		t.Errorf("got %v, want nil for absent config", got)
	}
}

func TestSetEmbeddingConfigRoundTrip(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateProject("ATM", "Agent Tasks Management", "tester"); err != nil {
		t.Fatal(err)
	}
	cfg := EmbeddingConfig{
		Model:       "nomic-embed-text",
		Endpoint:    "http://localhost:11434/v1",
		QueryPrefix: "search_query: ",
		DocPrefix:   "search_document: ",
		Dim:         768,
		Threshold:   0.55,
	}
	if err := s.SetEmbeddingConfig("ATM", cfg, "tester"); err != nil {
		t.Fatalf("SetEmbeddingConfig: %v", err)
	}
	got, err := s.GetProjectConfig("ATM")
	if err != nil {
		t.Fatalf("GetProjectConfig: %v", err)
	}
	if got.Embedding == nil {
		t.Fatal("Embedding nil after set")
	}
	if got.Embedding.Model != "nomic-embed-text" || got.Embedding.Dim != 768 || got.Embedding.Threshold != 0.55 {
		t.Errorf("Embedding = %+v, want model=nomic dim=768 threshold=0.55", got.Embedding)
	}
	if got.Embedding.QueryPrefix != "search_query: " {
		t.Errorf("QueryPrefix = %q", got.Embedding.QueryPrefix)
	}
}

func TestSetEmbeddingConfigMergesPreservingOtherFields(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateProject("ATM", "Agent Tasks Management", "tester"); err != nil {
		t.Fatal(err)
	}
	cfg := EmbeddingConfig{Model: "m1", Endpoint: "http://x", Dim: 4, Threshold: 0.5}
	if err := s.SetEmbeddingConfig("ATM", cfg, "tester"); err != nil {
		t.Fatal(err)
	}
	cfg2 := EmbeddingConfig{Model: "m2", Endpoint: "http://y", Dim: 8, Threshold: 0.6}
	if err := s.SetEmbeddingConfig("ATM", cfg2, "tester2"); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetProjectConfig("ATM")
	if got.Embedding.Model != "m2" || got.Embedding.Dim != 8 {
		t.Errorf("Embedding not overwritten: %+v", got.Embedding)
	}
}

func TestSetEmbeddingConfigRequiresActor(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateProject("ATM", "Agent Tasks Management", "tester"); err != nil {
		t.Fatal(err)
	}
	err := s.SetEmbeddingConfig("ATM", EmbeddingConfig{Model: "m", Endpoint: "http://x", Dim: 4, Threshold: 0.5}, "")
	if !IsUsage(err) {
		t.Errorf("err = %v, want ErrUsage (missing actor)", err)
	}
}
