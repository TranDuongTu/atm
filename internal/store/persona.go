package store

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"atm/internal/core"
	"atm/internal/seed"
)

func (s *Store) personasDir() string { return filepath.Join(s.Root, "personas") }
func (s *Store) personaPath(name string) string {
	return filepath.Join(s.personasDir(), name+".json")
}

func (s *Store) CreatePersona(name, prompt, description, actor string) (*Persona, error) {
	if err := core.ValidatePersonaName(name); err != nil {
		return nil, err
	}
	if err := s.validateActor(actor); err != nil {
		return nil, err
	}
	return s.createPersonaLocked(name, prompt, description, actor)
}

// createPersonaLocked writes the persona file. It performs NO actor validation
// and is the path used by SeedPersonas during bootstrap (the built-in personas
// cannot satisfy validateActor until they exist).
func (s *Store) createPersonaLocked(name, prompt, description, actor string) (*Persona, error) {
	var created *Persona
	err := s.WithLock("personas", func() error {
		if _, err := os.Stat(s.personaPath(name)); err == nil {
			return fmt.Errorf("%w: persona %q already exists", core.ErrConflict, name)
		} else if !os.IsNotExist(err) {
			return err
		}
		now := core.Now()
		p := &Persona{
			Name: name, Prompt: prompt, Description: description,
			CreatedAt: now, UpdatedAt: now, CreatedBy: actor, UpdatedBy: actor,
		}
		if err := WriteFileAtomic(s.personaPath(name), p); err != nil {
			return err
		}
		created = p
		return nil
	})
	return created, err
}

func (s *Store) GetPersona(name string) (*Persona, error) {
	if err := core.ValidatePersonaName(name); err != nil {
		return nil, err
	}
	var p Persona
	if err := ReadJSON(s.personaPath(name), &p); err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%w: persona %q", core.ErrNotFound, name)
		}
		return nil, err
	}
	return &p, nil
}

func (s *Store) ListPersonas() []*Persona {
	entries, err := os.ReadDir(s.personasDir())
	if err != nil {
		return nil
	}
	var out []*Persona
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		name := e.Name()[:len(e.Name())-len(".json")]
		if p, err := s.GetPersona(name); err == nil {
			out = append(out, p)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func (s *Store) EditPersona(name string, prompt, description *string, actor string) (*Persona, error) {
	if err := core.ValidatePersonaName(name); err != nil {
		return nil, err
	}
	if err := s.validateActor(actor); err != nil {
		return nil, err
	}
	var updated *Persona
	err := s.WithLock("personas", func() error {
		p, err := s.GetPersona(name)
		if err != nil {
			return err
		}
		if prompt != nil {
			p.Prompt = *prompt
		}
		if description != nil {
			p.Description = *description
		}
		p.UpdatedAt = core.Now()
		p.UpdatedBy = actor
		if err := WriteFileAtomic(s.personaPath(name), p); err != nil {
			return err
		}
		updated = p
		return nil
	})
	return updated, err
}

func (s *Store) RemovePersona(name string) error {
	if err := core.ValidatePersonaName(name); err != nil {
		return err
	}
	for _, b := range seedPersonas() {
		if b == name {
			return fmt.Errorf("%w: cannot remove built-in persona %q", core.ErrUsage, name)
		}
	}
	return s.WithLock("personas", func() error {
		if _, err := s.GetPersona(name); err != nil {
			return err
		}
		return os.Remove(s.personaPath(name))
	})
}

// SeedPersonas creates any built-in persona (seed.Personas) that does not yet
// exist. Idempotent: never overwrites an existing (possibly user-edited) file.
// Returns the names newly created.
func (s *Store) SeedPersonas(actor string) ([]string, error) {
	var added []string
	for _, sp := range seed.Personas {
		_, err := s.createPersonaLocked(sp.Name, sp.Prompt, sp.Description, actor)
		if err == nil {
			added = append(added, sp.Name)
			continue
		}
		if core.IsConflict(err) {
			continue // already exists — leave it untouched
		}
		return added, err
	}
	sort.Strings(added)
	return added, nil
}

// seedPersonas returns the built-in persona names (order-independent).
func seedPersonas() []string {
	names := make([]string, 0, len(seed.Personas))
	for _, sp := range seed.Personas {
		names = append(names, sp.Name)
	}
	return names
}
