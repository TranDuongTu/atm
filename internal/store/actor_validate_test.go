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

func TestValidateActorUnregisteredPersona(t *testing.T) {
	s := newTestStore(t)
	if err := s.validateActor("ghost@cli:unset"); !errors.Is(err, core.ErrUsage) {
		t.Errorf("validateActor(ghost) = %v, want core.ErrUsage", err)
	}
}

func TestCreateTaskRejectsUnregisteredPersona(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateProject("ATM", "Demo", "admin@cli:unset"); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	if _, err := s.CreateTask("ATM", "t", "", nil, "ghost@cli:unset"); !errors.Is(err, core.ErrUsage) {
		t.Errorf("CreateTask with unregistered persona = %v, want core.ErrUsage", err)
	}
}
