package store

import (
	"atm/internal/core"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

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
	if !core.IsUsage(err) {
		t.Errorf("err = %v, want core.ErrUsage (missing actor)", err)
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
	if !core.IsNotFound(err) {
		t.Errorf("err = %v, want core.ErrNotFound", err)
	}
}

func TestSetProjectRemoteEmptyNameOrURLIsUsage(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateProject("ATM", "Agent Tasks Management", testActor); err != nil {
		t.Fatal(err)
	}
	if err := s.SetProjectRemote("ATM", "", "https://example.com/a", testActor); !core.IsUsage(err) {
		t.Errorf("empty name: err = %v, want core.ErrUsage", err)
	}
	if err := s.SetProjectRemote("ATM", "origin", "", testActor); !core.IsUsage(err) {
		t.Errorf("empty url: err = %v, want core.ErrUsage", err)
	}
}

func TestSetProjectRemoteRequiresActor(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateProject("ATM", "Agent Tasks Management", testActor); err != nil {
		t.Fatal(err)
	}
	err := s.SetProjectRemote("ATM", "origin", "https://example.com/a", "")
	if !core.IsUsage(err) {
		t.Errorf("err = %v, want core.ErrUsage (missing actor)", err)
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

func TestBoardsConfigRoundTripAndMerge(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateProject("ATM", "Agent Tasks Management", testActor); err != nil {
		t.Fatal(err)
	}
	// Absent config: empty (non-nil) BoardsConfig.
	b, err := s.GetBoardsConfig("ATM")
	if err != nil || b == nil {
		t.Fatalf("GetBoardsConfig absent = (%v, %v), want empty non-nil", b, err)
	}
	if len(b.Order) != 0 || len(b.Hidden) != 0 {
		t.Fatalf("absent config not empty: %+v", b)
	}
	want := &core.BoardsConfig{
		Order:  []string{"ATM:all-tasks", "ATM:unmanaged"},
		Hidden: []string{"ATM:context-current"},
	}
	if err := s.SetProjectBoards("ATM", want, testActor); err != nil {
		t.Fatalf("SetProjectBoards: %v", err)
	}
	got, err := s.GetBoardsConfig("ATM")
	if err != nil {
		t.Fatal(err)
	}
	if got.Order[0] != "ATM:all-tasks" || got.Hidden[0] != "ATM:context-current" {
		t.Errorf("round-trip = %+v, want %+v", got, want)
	}
	// Boards write preserves other config fields.
	cfg := EmbeddingConfig{Model: "m1", Endpoint: "http://x", Dim: 4, Threshold: 0.5}
	if err := s.SetEmbeddingConfig("ATM", cfg, testActor); err != nil {
		t.Fatal(err)
	}
	pc, _ := s.GetProjectConfig("ATM")
	if pc.Boards == nil || pc.Embedding == nil {
		t.Errorf("config lost a field after both writes: %+v", pc)
	}
}

// TestGetProjectConfigBoardsOnlyIsNotAbsent guards the emptiness check: a
// config carrying only a boards key must not read as nil.
func TestGetProjectConfigBoardsOnlyIsNotAbsent(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateProject("ATM", "Agent Tasks Management", testActor); err != nil {
		t.Fatal(err)
	}
	if err := s.SetProjectBoards("ATM", &core.BoardsConfig{Hidden: []string{"ATM:backlog"}}, testActor); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetProjectConfig("ATM")
	if err != nil || got == nil || got.Boards == nil {
		t.Fatalf("GetProjectConfig = (%+v, %v), want non-nil with Boards", got, err)
	}
}

func TestSetProjectBoardsValidation(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateProject("ATM", "Agent Tasks Management", testActor); err != nil {
		t.Fatal(err)
	}
	if err := s.SetProjectBoards("ATM", nil, testActor); !core.IsUsage(err) {
		t.Errorf("nil boards: err = %v, want usage", err)
	}
	if err := s.SetProjectBoards("ATM", &core.BoardsConfig{}, ""); !core.IsUsage(err) {
		t.Errorf("missing actor: err = %v, want usage", err)
	}
}

func TestProjectRepoRoundTrip(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateProject("ATM", "Agent Tasks Management", testActor); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := s.SetProjectRepo("ATM", "main", dir, "https://example.com/atm.git", testActor); err != nil {
		t.Fatalf("SetProjectRepo: %v", err)
	}
	repos, err := s.ProjectRepos("ATM")
	if err != nil {
		t.Fatalf("ProjectRepos: %v", err)
	}
	if len(repos) != 1 || repos[0].Name != "main" || repos[0].Path != dir || repos[0].URL != "https://example.com/atm.git" {
		t.Errorf("repos = %+v, want one main -> %s", repos, dir)
	}
	if err := s.RemoveProjectRepo("ATM", "main", testActor); err != nil {
		t.Fatalf("RemoveProjectRepo: %v", err)
	}
	repos, err = s.ProjectRepos("ATM")
	if err != nil {
		t.Fatalf("ProjectRepos after remove: %v", err)
	}
	if len(repos) != 0 {
		t.Errorf("repos = %+v, want empty after remove", repos)
	}
}

func TestSetProjectRepoUpsertUpdatesExisting(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateProject("ATM", "Agent Tasks Management", testActor); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := s.SetProjectRepo("ATM", "main", dir, "https://example.com/a", testActor); err != nil {
		t.Fatal(err)
	}
	dir2 := t.TempDir()
	if err := s.SetProjectRepo("ATM", "main", dir2, "https://example.com/b", testActor); err != nil {
		t.Fatal(err)
	}
	repos, _ := s.ProjectRepos("ATM")
	if len(repos) != 1 || repos[0].Path != dir2 || repos[0].URL != "https://example.com/b" {
		t.Errorf("upsert = %+v, want one main -> %s with url b", repos, dir2)
	}
}

func TestSetProjectRepoMultipleNamesCoexist(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateProject("ATM", "Agent Tasks Management", testActor); err != nil {
		t.Fatal(err)
	}
	d1, d2 := t.TempDir(), t.TempDir()
	if err := s.SetProjectRepo("ATM", "main", d1, "", testActor); err != nil {
		t.Fatal(err)
	}
	if err := s.SetProjectRepo("ATM", "docs", d2, "", testActor); err != nil {
		t.Fatal(err)
	}
	repos, _ := s.ProjectRepos("ATM")
	if len(repos) != 2 || repos[0].Name != "main" || repos[1].Name != "docs" {
		t.Errorf("repos = %+v, want [main docs] in insertion order", repos)
	}
}

func TestRemoveProjectRepoUnknownNameReturnsNotFound(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateProject("ATM", "Agent Tasks Management", testActor); err != nil {
		t.Fatal(err)
	}
	err := s.RemoveProjectRepo("ATM", "nope", testActor)
	if !core.IsNotFound(err) {
		t.Errorf("err = %v, want core.ErrNotFound", err)
	}
}

func TestSetProjectRepoEmptyNameOrPathIsUsage(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateProject("ATM", "Agent Tasks Management", testActor); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := s.SetProjectRepo("ATM", "", dir, "", testActor); !core.IsUsage(err) {
		t.Errorf("empty name: err = %v, want core.ErrUsage", err)
	}
	if err := s.SetProjectRepo("ATM", "main", "", "", testActor); !core.IsUsage(err) {
		t.Errorf("empty path: err = %v, want core.ErrUsage", err)
	}
}

func TestSetProjectRepoRequiresActor(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateProject("ATM", "Agent Tasks Management", testActor); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	err := s.SetProjectRepo("ATM", "main", dir, "", "")
	if !core.IsUsage(err) {
		t.Errorf("err = %v, want core.ErrUsage (missing actor)", err)
	}
}

func TestSetProjectRepoRejectsNonexistentPath(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateProject("ATM", "Agent Tasks Management", testActor); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(t.TempDir(), "does-not-exist")
	if err := s.SetProjectRepo("ATM", "main", missing, "", testActor); err == nil {
		t.Errorf("err = nil, want error for non-existent path %s", missing)
	}
}

func TestSetProjectRepoResolvesRelativeAndTildePath(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateProject("ATM", "Agent Tasks Management", testActor); err != nil {
		t.Fatal(err)
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetProjectRepo("ATM", "main", ".", "", testActor); err != nil {
		t.Fatalf("relative path '.': %v", err)
	}
	repos, _ := s.ProjectRepos("ATM")
	if len(repos) != 1 || repos[0].Path != cwd {
		t.Errorf("path = %q, want resolved absolute %q", repos[0].Path, cwd)
	}
}

func TestSetProjectRepoPreservesOtherConfigFields(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateProject("ATM", "Agent Tasks Management", testActor); err != nil {
		t.Fatal(err)
	}
	if err := s.SetEmbeddingConfig("ATM", EmbeddingConfig{Model: "m", Endpoint: "http://x", Dim: 4, Threshold: 0.5}, testActor); err != nil {
		t.Fatal(err)
	}
	if err := s.SetProjectRemote("ATM", "origin", "https://example.com/sync", testActor); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := s.SetProjectRepo("ATM", "main", dir, "", testActor); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetProjectConfig("ATM")
	if got.Embedding == nil || got.Embedding.Model != "m" {
		t.Errorf("Embedding lost after repo add: %+v", got.Embedding)
	}
	if got.Remotes["origin"] != "https://example.com/sync" {
		t.Errorf("Remotes lost after repo add: %+v", got.Remotes)
	}
	if len(got.Repos) != 1 || got.Repos[0].Path != dir {
		t.Errorf("Repos = %+v, want one main -> %s", got.Repos, dir)
	}
}

// TestGetProjectConfigReposOnlyIsNotAbsent guards the emptiness check: a
// config carrying only a repos key must not read as nil.
func TestGetProjectConfigReposOnlyIsNotAbsent(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateProject("ATM", "Agent Tasks Management", testActor); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := s.SetProjectRepo("ATM", "main", dir, "", testActor); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetProjectConfig("ATM")
	if err != nil || got == nil || len(got.Repos) != 1 {
		t.Fatalf("GetProjectConfig = (%+v, %v), want non-nil with one repo", got, err)
	}
}

func TestProjectReposAbsentConfigReturnsEmptyNoError(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateProject("ATM", "Agent Tasks Management", testActor); err != nil {
		t.Fatal(err)
	}
	repos, err := s.ProjectRepos("ATM")
	if err != nil {
		t.Fatalf("ProjectRepos: %v", err)
	}
	if len(repos) != 0 {
		t.Errorf("repos = %+v, want empty", repos)
	}
}

func TestSetProjectArtOn(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateProject("ATM", "Agent Tasks Management", testActor); err != nil {
		t.Fatal(err)
	}

	// Turn art on with a pinned pair.
	if err := s.SetProjectArtOn("ATM", true, []string{"galaxy", "matrix"}, testActor); err != nil {
		t.Fatal(err)
	}
	c, err := s.GetProjectConfig("ATM")
	if err != nil || c == nil {
		t.Fatalf("config = %v, %v", c, err)
	}
	if !c.ArtOn {
		t.Fatalf("ArtOn = false, want true")
	}
	if len(c.ArtPair) != 2 || c.ArtPair[0] != "galaxy" || c.ArtPair[1] != "matrix" {
		t.Fatalf("ArtPair = %v, want [galaxy matrix]", c.ArtPair)
	}
	if c.UpdatedBy != testActor {
		t.Fatalf("UpdatedBy = %q", c.UpdatedBy)
	}

	// Turning art off clears both the flag and the pinned pair.
	if err := s.SetProjectArtOn("ATM", false, nil, testActor); err != nil {
		t.Fatal(err)
	}
	c, err = s.GetProjectConfig("ATM")
	if err != nil {
		t.Fatal(err)
	}
	if c != nil && c.ArtOn {
		t.Fatalf("ArtOn still true after clear")
	}
	if c != nil && len(c.ArtPair) != 0 {
		t.Fatalf("ArtPair not cleared on off: %v", c.ArtPair)
	}

	// A config holding ONLY art_on must not read back as nil (regression
	// guard for GetProjectConfig's emptiness check). Written as a raw JSON
	// fixture straight to config.json, NOT via SetProjectArtOn: the setter
	// always stamps UpdatedAt, so routing through it would make
	// GetProjectConfig non-nil via the UpdatedAt clause and never actually
	// exercise the ArtOn clause under test. With every other field left
	// empty, this fixture isolates that one clause -- the assertion below
	// fails if the `&& !c.ArtOn` conjunct is removed from
	// GetProjectConfig. Use a bare second project so nothing else on disk
	// (e.g. ATM's updated_at/updated_by stamps from the writes above) could
	// keep the config non-nil for an unrelated reason.
	if _, err := s.CreateProject("XYZ", "Bare Project", testActor); err != nil {
		t.Fatal(err)
	}
	rawConfigPath := filepath.Join(s.StorePath(), "projects", "XYZ", "config.json")
	if err := os.WriteFile(rawConfigPath, []byte(`{"art_on":true}`), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err = s.GetProjectConfig("XYZ")
	if err != nil || c == nil {
		t.Fatalf("art-on-only config must be readable, got %v, %v", c, err)
	}
	if !c.ArtOn {
		t.Fatalf("ArtOn = false, want true")
	}

	// Invalid actor is rejected.
	if err := s.SetProjectArtOn("ATM", true, []string{"galaxy", "matrix"}, "not-an-actor"); err == nil {
		t.Fatal("invalid actor must be rejected")
	}
}

func TestSetChatConfigRoundTrips(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateProject("ATM", "Agent Tasks Management", testActor); err != nil {
		t.Fatal(err)
	}
	cfg := core.ChatConfig{Model: "qwen3:8b", Endpoint: "http://localhost:11434/v1"}
	if err := s.SetChatConfig("ATM", cfg, testActor); err != nil {
		t.Fatalf("SetChatConfig: %v", err)
	}
	got, err := s.GetProjectConfig("ATM")
	if err != nil {
		t.Fatal(err)
	}
	// A chat-only config must not read back as no config at all: the
	// all-empty guard in GetProjectConfig has to know about Chat.
	if got == nil || got.Chat == nil {
		t.Fatalf("config = %+v, want a chat config read back", got)
	}
	if got.Chat.Model != "qwen3:8b" || got.Chat.Endpoint != "http://localhost:11434/v1" {
		t.Errorf("chat = %+v", got.Chat)
	}
}

// The two configs are siblings, not alternatives: setting one must not drop
// the other, since both live in the same config.json.
func TestSetChatConfigKeepsEmbedding(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateProject("ATM", "Agent Tasks Management", testActor); err != nil {
		t.Fatal(err)
	}
	if err := s.SetEmbeddingConfig("ATM", core.EmbeddingConfig{Model: "nomic-embed-text", Endpoint: "http://localhost:11434/v1", Dim: 768}, testActor); err != nil {
		t.Fatal(err)
	}
	if err := s.SetChatConfig("ATM", core.ChatConfig{Model: "qwen3:8b", Endpoint: "http://localhost:11434/v1"}, testActor); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetProjectConfig("ATM")
	if err != nil {
		t.Fatal(err)
	}
	if got.Embedding == nil || got.Embedding.Model != "nomic-embed-text" {
		t.Errorf("embedding = %+v, want it preserved", got.Embedding)
	}
	if got.Chat == nil || got.Chat.Model != "qwen3:8b" {
		t.Errorf("chat = %+v", got.Chat)
	}
}

func TestSetChatConfigRequiresModelAndEndpoint(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateProject("ATM", "Agent Tasks Management", testActor); err != nil {
		t.Fatal(err)
	}
	for _, cfg := range []core.ChatConfig{{Endpoint: "http://x"}, {Model: "m"}} {
		if err := s.SetChatConfig("ATM", cfg, testActor); !errors.Is(err, core.ErrUsage) {
			t.Errorf("SetChatConfig(%+v) err = %v, want ErrUsage", cfg, err)
		}
	}
}

// SetInquiryLog is the third writer into config.json. It must merge, not
// replace: a project that has been configured for search and answers must
// not lose that configuration because someone turned query logging off.
// Both sibling writers (SetEmbeddingConfig, SetChatConfig) carry this same
// guard.
func TestSetInquiryLogKeepsEmbeddingAndChat(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateProject("ATM", "Agent Tasks Management", testActor); err != nil {
		t.Fatal(err)
	}
	if err := s.SetEmbeddingConfig("ATM", core.EmbeddingConfig{Model: "nomic-embed-text", Endpoint: "http://localhost:11434/v1", Dim: 768}, testActor); err != nil {
		t.Fatal(err)
	}
	if err := s.SetChatConfig("ATM", core.ChatConfig{Model: "qwen3:8b", Endpoint: "http://localhost:11434/v1"}, testActor); err != nil {
		t.Fatal(err)
	}
	if err := s.SetInquiryLog("ATM", false, testActor); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetProjectConfig("ATM")
	if err != nil {
		t.Fatal(err)
	}
	if got.Embedding == nil || got.Embedding.Model != "nomic-embed-text" {
		t.Errorf("embedding = %+v, want it preserved", got.Embedding)
	}
	if got.Chat == nil || got.Chat.Model != "qwen3:8b" {
		t.Errorf("chat = %+v, want it preserved", got.Chat)
	}
	if got.InquiryLog == nil || *got.InquiryLog != false {
		t.Errorf("inquiry log = %v, want false", got.InquiryLog)
	}
}
