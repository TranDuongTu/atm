package store

import (
	"atm/internal/core"
	"errors"
	"testing"
)

func TestValidateActor(t *testing.T) {
	s := newTestStore(t)
	good := []string{"developer@claude:opus-4.8", "admin@cli:unset", "manager@codex:unset"}
	for _, a := range good {
		if err := s.validateActor(a); err != nil {
			t.Errorf("validateActor(%q) = %v, want nil", a, err)
		}
	}
	bad := []string{"", "developer", "developer@claude", "@claude:x", "developer@:x", "developer@claude:"}
	for _, a := range bad {
		if err := s.validateActor(a); !errors.Is(err, core.ErrUsage) {
			t.Errorf("validateActor(%q) = %v, want core.ErrUsage", a, err)
		}
	}
}

// Actor validation is FORM-ONLY (plan §7): persona records are project
// state and an actor string spans projects, so no per-project record set can
// be its authority. A persona that exists nowhere as a record still stamps.
func TestValidateActorAcceptsUnregisteredPersona(t *testing.T) {
	s := newTestStore(t)
	if err := s.validateActor("ghost@cli:unset"); err != nil {
		t.Errorf("validateActor(ghost) = %v, want nil (form-only)", err)
	}
}

func TestCreateTaskAcceptsUnregisteredPersona(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateProject("ATM", "Demo", "admin@cli:unset"); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	if _, err := s.CreateTask("ATM", "t", "", nil, "ghost@cli:unset"); err != nil {
		t.Errorf("CreateTask with unregistered persona = %v, want nil (form-only)", err)
	}
}
