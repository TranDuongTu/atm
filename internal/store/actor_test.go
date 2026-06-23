package store

import (
	"testing"
)

func TestRegisterLazy(t *testing.T) {
	dir := t.TempDir()
	s, _ := Open(dir)
	_ = s.Init("")

	if err := s.Register("agent:claude-1", "Claude 1"); err != nil {
		t.Fatal(err)
	}
	actors := s.List()
	if len(actors) != 1 {
		t.Fatalf("got %d actors want 1", len(actors))
	}
	if actors[0].ID != "agent:claude-1" {
		t.Fatalf("got id %q want agent:claude-1", actors[0].ID)
	}
	if actors[0].Kind != "agent" {
		t.Fatalf("got kind %q want agent", actors[0].Kind)
	}
	if actors[0].Name != "Claude 1" {
		t.Fatalf("got name %q want 'Claude 1'", actors[0].Name)
	}

	if err := s.Register("agent:claude-1", "Claude Renamed"); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get("agent:claude-1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "Claude Renamed" {
		t.Fatalf("name not updated: %q", got.Name)
	}
	if len(s.List()) != 1 {
		t.Fatalf("re-register should not duplicate, got %d", len(s.List()))
	}
}

func TestRegisterInvalidID(t *testing.T) {
	dir := t.TempDir()
	s, _ := Open(dir)
	_ = s.Init("")

	bad := []string{
		"",
		"agent",
		"human",
		"robot:marvin",
		"agent:",
		"human:bad id!",
		"AGENT:upper",
	}
	for _, id := range bad {
		if err := s.Register(id, ""); err == nil {
			t.Errorf("expected error for id %q", id)
		}
	}
}

func TestGetNotFound(t *testing.T) {
	dir := t.TempDir()
	s, _ := Open(dir)
	_ = s.Init("")
	_, err := s.Get("human:ghost")
	if !IsNotFound(err) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestListSorted(t *testing.T) {
	dir := t.TempDir()
	s, _ := Open(dir)
	_ = s.Init("")
	_ = s.Register("human:zoe", "")
	_ = s.Register("agent:alpha", "")
	_ = s.Register("human:alice", "")
	actors := s.List()
	if len(actors) != 3 {
		t.Fatalf("got %d want 3", len(actors))
	}
	if actors[0].ID != "agent:alpha" {
		t.Fatalf("first = %q want agent:alpha", actors[0].ID)
	}
	if actors[1].ID != "human:alice" {
		t.Fatalf("second = %q want human:alice", actors[1].ID)
	}
	if actors[2].ID != "human:zoe" {
		t.Fatalf("third = %q want human:zoe", actors[2].ID)
	}
}
