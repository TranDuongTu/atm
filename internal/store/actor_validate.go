package store

import (
	"atm/internal/core"
	"fmt"
	"strings"
)

// validateActor enforces the canonical actor form persona@agent:model with a
// registered persona. Called at the top of every mutation, before WithLock.
// Seeds the built-in personas lazily so a fresh store can validate against
// developer/manager/admin without an explicit seed step.
func (s *Store) validateActor(raw string) error {
	if err := s.ensureBuiltinPersonas(); err != nil {
		return err
	}
	persona, rest, ok := strings.Cut(raw, "@")
	if !ok {
		return fmt.Errorf("%w: actor must be persona@agent:model (got %q)", core.ErrUsage, raw)
	}
	agent, model, ok := strings.Cut(rest, ":")
	if !ok || persona == "" || agent == "" || model == "" {
		return fmt.Errorf("%w: actor must be persona@agent:model (got %q)", core.ErrUsage, raw)
	}
	if _, err := s.GetPersona(persona); err != nil {
		if core.IsNotFound(err) {
			return fmt.Errorf("%w: unknown persona %q; create it first with 'atm persona create'", core.ErrUsage, persona)
		}
		return err
	}
	return nil
}
