package store

import (
	"atm/internal/core"
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
	if len(b.Order) != 0 || len(b.Hidden) != 0 || len(b.Pins) != 0 {
		t.Fatalf("absent config not empty: %+v", b)
	}
	want := &core.BoardsConfig{
		Order:  []string{"ATM:all-tasks", "ATM:unmanaged"},
		Hidden: []string{"ATM:context-current"},
		Pins:   []string{"ATM:all-tasks"},
	}
	if err := s.SetProjectBoards("ATM", want, testActor); err != nil {
		t.Fatalf("SetProjectBoards: %v", err)
	}
	got, err := s.GetBoardsConfig("ATM")
	if err != nil {
		t.Fatal(err)
	}
	if got.Order[0] != "ATM:all-tasks" || got.Hidden[0] != "ATM:context-current" || got.Pins[0] != "ATM:all-tasks" {
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
	four := &core.BoardsConfig{Pins: []string{"ATM:a", "ATM:b", "ATM:c", "ATM:d"}}
	if err := s.SetProjectBoards("ATM", four, testActor); !core.IsUsage(err) {
		t.Errorf("4 pins: err = %v, want usage (MaxBoardPins=3)", err)
	}
	if err := s.SetProjectBoards("ATM", &core.BoardsConfig{}, ""); !core.IsUsage(err) {
		t.Errorf("missing actor: err = %v, want usage", err)
	}
}

// TestGetBoardsConfigFoldsLegacyPins is the migration read: boards nil +
// pins.json present -> Pins folded in (capped at 3); first SetProjectBoards
// persists, after which pins.json is ignored.
func TestGetBoardsConfigFoldsLegacyPins(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateProject("ATM", "Agent Tasks Management", testActor); err != nil {
		t.Fatal(err)
	}
	// Task 7 retired WritePins; the fold-in read still consumes a raw
	// pins.json, so the fixture writes one directly under the project dir.
	pinsPath := filepath.Join(s.StorePath(), "projects", "ATM", "pins.json")
	if err := os.WriteFile(pinsPath, []byte(`{"boards":["ATM:open-tasks","ATM:backlog"]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	b, err := s.GetBoardsConfig("ATM")
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Pins) != 2 || b.Pins[0] != "ATM:open-tasks" {
		t.Fatalf("legacy fold-in = %+v, want the pins.json boards", b.Pins)
	}
	// Persist with different pins; pins.json is now dead.
	if err := s.SetProjectBoards("ATM", &core.BoardsConfig{Pins: []string{"ATM:all-tasks"}}, testActor); err != nil {
		t.Fatal(err)
	}
	b2, _ := s.GetBoardsConfig("ATM")
	if len(b2.Pins) != 1 || b2.Pins[0] != "ATM:all-tasks" {
		t.Fatalf("after persist = %+v, pins.json must be ignored", b2.Pins)
	}
}

func TestSetProjectArtTheme(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateProject("ATM", "Agent Tasks Management", testActor); err != nil {
		t.Fatal(err)
	}

	// Set a pin.
	if err := s.SetProjectArtTheme("ATM", "circuit", testActor); err != nil {
		t.Fatal(err)
	}
	c, err := s.GetProjectConfig("ATM")
	if err != nil || c == nil {
		t.Fatalf("config = %v, %v", c, err)
	}
	if c.ArtTheme != "circuit" {
		t.Fatalf("ArtTheme = %q, want circuit", c.ArtTheme)
	}
	if c.UpdatedBy != testActor {
		t.Fatalf("UpdatedBy = %q", c.UpdatedBy)
	}

	// Clearing with empty string removes the pin but keeps the config file
	// readable.
	if err := s.SetProjectArtTheme("ATM", "", testActor); err != nil {
		t.Fatal(err)
	}
	c, err = s.GetProjectConfig("ATM")
	if err != nil {
		t.Fatal(err)
	}
	if c != nil && c.ArtTheme != "" {
		t.Fatalf("ArtTheme not cleared: %q", c.ArtTheme)
	}

	// A config holding ONLY art_theme must not read back as nil (regression
	// guard for GetProjectConfig's emptiness check). Written as a raw JSON
	// fixture straight to config.json, NOT via SetProjectArtTheme: the setter
	// always stamps UpdatedAt, so routing through it would make
	// GetProjectConfig non-nil via the UpdatedAt clause and never actually
	// exercise the ArtTheme clause under test. With every other field left
	// empty, this fixture isolates that one clause -- the assertion below
	// fails if the `&& c.ArtTheme == ""` conjunct is removed from
	// GetProjectConfig. Use a bare second project so nothing else on disk
	// (e.g. ATM's updated_at/updated_by stamps from the writes above) could
	// keep the config non-nil for an unrelated reason.
	if _, err := s.CreateProject("XYZ", "Bare Project", testActor); err != nil {
		t.Fatal(err)
	}
	rawConfigPath := filepath.Join(s.StorePath(), "projects", "XYZ", "config.json")
	if err := os.WriteFile(rawConfigPath, []byte(`{"art_theme":"waves"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err = s.GetProjectConfig("XYZ")
	if err != nil || c == nil {
		t.Fatalf("art-theme-only config must be readable, got %v, %v", c, err)
	}
	if c.ArtTheme != "waves" {
		t.Fatalf("ArtTheme = %q, want waves", c.ArtTheme)
	}

	// Invalid actor is rejected.
	if err := s.SetProjectArtTheme("ATM", "waves", "not-an-actor"); err == nil {
		t.Fatal("invalid actor must be rejected")
	}
}
