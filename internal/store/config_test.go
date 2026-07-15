package store

import "testing"

func TestProjectConfigAbsentReturnsNil(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateProject("ATM", "Agent Tasks Management", testActor); err != nil {
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
	if _, err := s.CreateProject("ATM", "Agent Tasks Management", testActor); err != nil {
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
	if err := s.SetEmbeddingConfig("ATM", cfg, testActor); err != nil {
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
	if _, err := s.CreateProject("ATM", "Agent Tasks Management", testActor); err != nil {
		t.Fatal(err)
	}
	cfg := EmbeddingConfig{Model: "m1", Endpoint: "http://x", Dim: 4, Threshold: 0.5}
	if err := s.SetEmbeddingConfig("ATM", cfg, testActor); err != nil {
		t.Fatal(err)
	}
	cfg2 := EmbeddingConfig{Model: "m2", Endpoint: "http://y", Dim: 8, Threshold: 0.6}
	if err := s.SetEmbeddingConfig("ATM", cfg2, testActor); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetProjectConfig("ATM")
	if got.Embedding.Model != "m2" || got.Embedding.Dim != 8 {
		t.Errorf("Embedding not overwritten: %+v", got.Embedding)
	}
}

func TestSetEmbeddingConfigRequiresActor(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateProject("ATM", "Agent Tasks Management", testActor); err != nil {
		t.Fatal(err)
	}
	err := s.SetEmbeddingConfig("ATM", EmbeddingConfig{Model: "m", Endpoint: "http://x", Dim: 4, Threshold: 0.5}, "")
	if !IsUsage(err) {
		t.Errorf("err = %v, want ErrUsage (missing actor)", err)
	}
}

func TestProjectRemoteRoundTrip(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateProject("ATM", "Agent Tasks Management", testActor); err != nil {
		t.Fatal(err)
	}
	if err := s.SetProjectRemote("ATM", "origin", "https://example.com/atm-remote", testActor); err != nil {
		t.Fatalf("SetProjectRemote: %v", err)
	}
	remotes, err := s.ProjectRemotes("ATM")
	if err != nil {
		t.Fatalf("ProjectRemotes: %v", err)
	}
	if remotes["origin"] != "https://example.com/atm-remote" {
		t.Errorf("remotes[origin] = %q, want https://example.com/atm-remote", remotes["origin"])
	}

	if err := s.RemoveProjectRemote("ATM", "origin", testActor); err != nil {
		t.Fatalf("RemoveProjectRemote: %v", err)
	}
	remotes, err = s.ProjectRemotes("ATM")
	if err != nil {
		t.Fatalf("ProjectRemotes after remove: %v", err)
	}
	if _, ok := remotes["origin"]; ok {
		t.Errorf("remotes still contains origin after remove: %+v", remotes)
	}
}

func TestSetProjectRemoteMultipleNamesCoexist(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateProject("ATM", "Agent Tasks Management", testActor); err != nil {
		t.Fatal(err)
	}
	if err := s.SetProjectRemote("ATM", "origin", "https://example.com/a", testActor); err != nil {
		t.Fatal(err)
	}
	if err := s.SetProjectRemote("ATM", "backup", "https://example.com/b", testActor); err != nil {
		t.Fatal(err)
	}
	remotes, err := s.ProjectRemotes("ATM")
	if err != nil {
		t.Fatal(err)
	}
	if len(remotes) != 2 || remotes["origin"] != "https://example.com/a" || remotes["backup"] != "https://example.com/b" {
		t.Errorf("remotes = %+v, want both origin and backup", remotes)
	}
}

func TestRemoveProjectRemoteUnknownNameReturnsNotFound(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateProject("ATM", "Agent Tasks Management", testActor); err != nil {
		t.Fatal(err)
	}
	err := s.RemoveProjectRemote("ATM", "nope", testActor)
	if !IsNotFound(err) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestSetProjectRemoteEmptyNameOrURLIsUsage(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateProject("ATM", "Agent Tasks Management", testActor); err != nil {
		t.Fatal(err)
	}
	if err := s.SetProjectRemote("ATM", "", "https://example.com/a", testActor); !IsUsage(err) {
		t.Errorf("empty name: err = %v, want ErrUsage", err)
	}
	if err := s.SetProjectRemote("ATM", "origin", "", testActor); !IsUsage(err) {
		t.Errorf("empty url: err = %v, want ErrUsage", err)
	}
}

func TestSetProjectRemoteRequiresActor(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateProject("ATM", "Agent Tasks Management", testActor); err != nil {
		t.Fatal(err)
	}
	err := s.SetProjectRemote("ATM", "origin", "https://example.com/a", "")
	if !IsUsage(err) {
		t.Errorf("err = %v, want ErrUsage (missing actor)", err)
	}
}

func TestGetProjectConfigLoadsWithOnlyRemotes(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateProject("ATM", "Agent Tasks Management", testActor); err != nil {
		t.Fatal(err)
	}
	if err := s.SetProjectRemote("ATM", "origin", "https://example.com/a", testActor); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetProjectConfig("ATM")
	if err != nil {
		t.Fatalf("GetProjectConfig: %v", err)
	}
	if got == nil {
		t.Fatal("GetProjectConfig returned nil for config holding only remotes")
	}
	if got.Embedding != nil {
		t.Errorf("Embedding = %+v, want nil", got.Embedding)
	}
	if got.Remotes["origin"] != "https://example.com/a" {
		t.Errorf("Remotes[origin] = %q", got.Remotes["origin"])
	}
}

func TestProjectRemotesAbsentConfigReturnsNilMapNoError(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateProject("ATM", "Agent Tasks Management", testActor); err != nil {
		t.Fatal(err)
	}
	remotes, err := s.ProjectRemotes("ATM")
	if err != nil {
		t.Fatalf("ProjectRemotes: %v", err)
	}
	if len(remotes) != 0 {
		t.Errorf("remotes = %+v, want empty", remotes)
	}
}
