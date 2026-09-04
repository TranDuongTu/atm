package store

import (
	"fmt"

	"atm/internal/core"
	"atm/skills"
)

// builtinPersona converts a skills built-in to the core persona shape.
// Built-ins have no audit trail: they ship with the binary.
func builtinPersona(spec skills.PersonaSpec) *core.Persona {
	return &core.Persona{
		Name:            spec.Name,
		Prompt:          spec.Body,
		Description:     spec.Description,
		ProjectOptional: spec.ProjectOptional,
		Launch:          spec.Launch,
		CreatedBy:       "builtin",
		UpdatedBy:       "builtin",
	}
}

// GetPersona resolves a persona by name. The PROJECT record is the primary
// surface (`atm persona set`); the code-side built-ins are the fallback a
// machine runs on before any profile has been applied. The machine-global
// custom-persona store is pruned (plan §7): a persona outside a project is
// imported into it, not onto the machine.
func (s *Store) GetPersona(name string) (*core.Persona, error) {
	if err := core.ValidatePersonaName(name); err != nil {
		return nil, err
	}
	if spec, ok := skills.Persona(name); ok {
		return builtinPersona(spec), nil
	}
	return nil, fmt.Errorf("%w: persona %q — not a built-in; import one into a project with `atm persona set --file`", core.ErrNotFound, name)
}

// ListPersonas returns the personas this binary knows without a project:
// the built-ins, name-sorted. Project records list with `atm persona list
// --project <CODE>`.
func (s *Store) ListPersonas() []*core.Persona {
	var out []*core.Persona
	for _, spec := range skills.Personas() {
		out = append(out, builtinPersona(spec))
	}
	return out
}

// personaExists reports whether name is a persona this binary can resolve
// without a project: the built-ins. validateActor consults it, so a persona
// that exists only as a project record is validated by the record surface's
// own reads, not here.
func (s *Store) personaExists(name string) bool {
	_, ok := skills.Persona(name)
	return ok
}