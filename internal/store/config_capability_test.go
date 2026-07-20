package store

import (
	"testing"

	"atm/internal/core"
)

// TestBoardsConfigCapabilityRoundTrip pins the capability-view persistence
// contract: boards.capability survives a SetProjectBoards/GetBoardsConfig
// round trip, and a boards config carrying ONLY Capability still reads back
// non-nil (the GetProjectConfig emptiness check already treats Boards != nil
// as present).
func TestBoardsConfigCapabilityRoundTrip(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateProject("ATM", "Acme", testActor); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	if err := s.SetProjectBoards("ATM", &core.BoardsConfig{Capability: "workflow"}, testActor); err != nil {
		t.Fatalf("SetProjectBoards: %v", err)
	}
	got, err := s.GetBoardsConfig("ATM")
	if err != nil {
		t.Fatalf("GetBoardsConfig: %v", err)
	}
	if got == nil || got.Capability != "workflow" {
		t.Fatalf("Capability = %+v, want workflow", got)
	}
	cfg, err := s.GetProjectConfig("ATM")
	if err != nil || cfg == nil || cfg.Boards == nil {
		t.Fatalf("GetProjectConfig = %+v, %v; want non-nil with Boards", cfg, err)
	}
}
