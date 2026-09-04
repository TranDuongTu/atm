package store

import (
	"fmt"
	"strings"

	"atm/internal/core"
)

// validateActor enforces the canonical actor form persona@agent:model —
// FORM ONLY. Called at the top of every mutation, before WithLock. Persona
// registration is no longer consulted: persona records are project state
// (plan §7 pruned the machine-global store), an actor string spans projects,
// so no per-project record set can be its authority — and requiring one at
// the top of every mutation was a chicken-and-egg trap (the write that
// would register the persona was itself refused).
func (s *Store) validateActor(raw string) error {
	persona, rest, ok := strings.Cut(raw, "@")
	if !ok {
		return fmt.Errorf("%w: actor must be persona@agent:model (got %q)", core.ErrUsage, raw)
	}
	agent, model, ok := strings.Cut(rest, ":")
	if !ok || persona == "" || agent == "" || model == "" {
		return fmt.Errorf("%w: actor must be persona@agent:model (got %q)", core.ErrUsage, raw)
	}
	return nil
}