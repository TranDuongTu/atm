package store

import (
	"path/filepath"
	"testing"
)

func setupPinsStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	root := filepath.Join(dir, "atm")
	s, err := Open(root)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := s.Init(""); err != nil {
		t.Fatalf("init: %v", err)
	}
	if _, err := s.CreateProject("ATM", "Atm", "admin@cli:unset"); err != nil {
		t.Fatalf("create project: %v", err)
	}
	return s
}

func TestGetPinsMissingFileReturnsNil(t *testing.T) {
	s := setupPinsStore(t)
	p, err := s.GetPins("ATM")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if p != nil {
		t.Errorf("got %+v, want nil for missing file", p)
	}
}

func TestWritePinsRoundTrip(t *testing.T) {
	s := setupPinsStore(t)
	in := &Pins{
		Actor:  "admin@cli:unset",
		Boards: []string{"ATM:open-tasks", "ATM:status:*"},
	}
	if err := s.WritePins("ATM", in); err != nil {
		t.Fatalf("write: %v", err)
	}
	out, err := s.GetPins("ATM")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if out == nil {
		t.Fatal("got nil pins after write")
	}
	if len(out.Boards) != 2 || out.Boards[0] != "ATM:open-tasks" || out.Boards[1] != "ATM:status:*" {
		t.Errorf("boards = %v, want [ATM:open-tasks, ATM:status:*]", out.Boards)
	}
	if out.UpdatedAt.IsZero() {
		t.Error("UpdatedAt not stamped")
	}
}

func TestWritePinsValidatesActor(t *testing.T) {
	s := setupPinsStore(t)
	err := s.WritePins("ATM", &Pins{Actor: "bogus"})
	if err == nil {
		t.Fatal("expected actor validation error, got nil")
	}
}
