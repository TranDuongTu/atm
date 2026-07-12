package store

import (
	"reflect"
	"testing"
)

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
