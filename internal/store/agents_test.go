package store

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const agentsActor = "admin@cli:unset"

func TestAgentsConfigRoundTrip(t *testing.T) {
	s := newTestStore(t)

	got, err := s.GetAgentsConfig()
	if err != nil {
		t.Fatalf("GetAgentsConfig on empty store: %v", err)
	}
	if got.Selected != "" || len(got.Args) != 0 {
		t.Fatalf("expected zero config, got %+v", got)
	}

	if err := s.SetSelectedAgent("ollama:opencode", testActor); err != nil {
		t.Fatalf("SetSelectedAgent: %v", err)
	}
	if err := s.SetAgentArgs("ollama:opencode", []string{"--yolo"}, testActor); err != nil {
		t.Fatalf("SetAgentArgs: %v", err)
	}

	got, err = s.GetAgentsConfig()
	if err != nil {
		t.Fatalf("GetAgentsConfig: %v", err)
	}
	if got.Selected != "ollama:opencode" {
		t.Fatalf("Selected = %q", got.Selected)
	}
	if !reflect.DeepEqual(got.Args["ollama:opencode"], []string{"--yolo"}) {
		t.Fatalf("Args = %+v", got.Args)
	}
	if got.UpdatedBy != testActor {
		t.Fatalf("UpdatedBy = %q", got.UpdatedBy)
	}

	// clearing args removes the entry, leaving Selected untouched
	if err := s.SetAgentArgs("ollama:opencode", nil, testActor); err != nil {
		t.Fatalf("clear SetAgentArgs: %v", err)
	}
	got, _ = s.GetAgentsConfig()
	if _, ok := got.Args["ollama:opencode"]; ok {
		t.Fatalf("expected cleared args, got %+v", got.Args)
	}
	if got.Selected != "ollama:opencode" {
		t.Fatalf("clearing args must not touch Selected; got %q", got.Selected)
	}
}

func TestSetSelectedAgentRejectsBadActor(t *testing.T) {
	s := newTestStore(t)
	if err := s.SetSelectedAgent("opencode", "not-an-actor"); err == nil {
		t.Fatal("expected actor validation error")
	}
}

// A file written by the OLD code (no "models") must load, and writing a model
// must not disturb anything already there. This is the no-migration contract.
func TestAgentModelIsAdditiveToLegacyConfig(t *testing.T) {
	s := newTestStore(t)
	legacy := `{"selected":"ollama:claude","args":{"claude":["--verbose"]}}`
	if err := os.WriteFile(filepath.Join(s.Root, "agents.json"), []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := s.SetAgentModel("ollama:claude", "glm-5.2", agentsActor); err != nil {
		t.Fatal(err)
	}
	cfg, err := s.GetAgentsConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Selected != "ollama:claude" {
		t.Fatalf("selected = %q, want ollama:claude", cfg.Selected)
	}
	if got := strings.Join(cfg.Args["claude"], " "); got != "--verbose" {
		t.Fatalf("args lost: %q", got)
	}
	if cfg.Models["ollama:claude"] != "glm-5.2" {
		t.Fatalf("models = %+v", cfg.Models)
	}
}

// Models are keyed per (agent, launcher) pair, exactly like Args, so the two
// launchers of one agent remember different models.
func TestAgentModelIsPerSelectionKey(t *testing.T) {
	s := newTestStore(t)
	if err := s.SetAgentModel("claude", "opus-5", agentsActor); err != nil {
		t.Fatal(err)
	}
	if err := s.SetAgentModel("ollama:claude", "glm-5.2", agentsActor); err != nil {
		t.Fatal(err)
	}
	cfg, _ := s.GetAgentsConfig()
	if cfg.Models["claude"] != "opus-5" || cfg.Models["ollama:claude"] != "glm-5.2" {
		t.Fatalf("models = %+v", cfg.Models)
	}
}

// An empty model clears the entry rather than storing "", so "no model" has
// exactly one representation.
func TestAgentModelEmptyClears(t *testing.T) {
	s := newTestStore(t)
	if err := s.SetAgentModel("claude", "opus-5", agentsActor); err != nil {
		t.Fatal(err)
	}
	if err := s.SetAgentModel("claude", "", agentsActor); err != nil {
		t.Fatal(err)
	}
	cfg, _ := s.GetAgentsConfig()
	if _, ok := cfg.Models["claude"]; ok {
		t.Fatalf("empty model should clear the entry, got %+v", cfg.Models)
	}
}
