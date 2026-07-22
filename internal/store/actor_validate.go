package store

import (
	"atm/internal/core"
	"fmt"
	"strings"
)

// validateActor enforces the canonical actor form persona@agent:model with a
// registered persona. Called at the top of every mutation, before WithLock.
// Built-in personas resolve from the skills package; custom personas persist
// in the store — no seeding step is required.
func (s *Store) validateActor(raw string) error {
	persona, rest, ok := strings.Cut(raw, "@")
	if !ok {
		return fmt.Errorf("%w: actor must be persona@agent:model (got %q)", core.ErrUsage, raw)
	}
	agent, model, ok := strings.Cut(rest, ":")
	if !ok || persona == "" || agent == "" || model == "" {
		return fmt.Errorf("%w: actor must be persona@agent:model (got %q)", core.ErrUsage, raw)
	}
	if !s.personaExists(persona) {
		return fmt.Errorf("%w: unknown persona %q; create it first with 'atm persona create'", core.ErrUsage, persona)
	}
	return nil
}
